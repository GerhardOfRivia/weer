package ingest

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/GerhardOfRivia/weer/internal/config"
	"github.com/GerhardOfRivia/weer/internal/processor"
	"github.com/GerhardOfRivia/weer/internal/radar"
	"github.com/GerhardOfRivia/weer/internal/store"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Engine struct {
	Config config.Config
	Store  *store.Store
	Source radar.Source
}

func New(c config.Config, s *store.Store) *Engine {
	return &Engine{c, s, radar.Source{BaseURL: "https://" + c.Bucket + ".s3." + c.Region + ".amazonaws.com", Client: &http.Client{Timeout: c.DownloadTimeout}, MaxSize: c.MaxDownloadBytes}}
}
func Lock(dir string) (*os.File, error) {
	f, e := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("data directory is in use; stop weer serve before importing: %w", e)
	}
	return f, nil
}
func ID(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (e *Engine) Discover(ctx context.Context) {
	for _, s := range e.Config.Stations {
		now := time.Now().UTC()
		objects, err := e.Source.Discover(ctx, s.ID, now.Add(-e.Config.History), now)
		if err == nil {
			for _, o := range objects {
				err = e.Store.Insert(store.Scan{ID: ID(e.Config.Bucket + "/" + o.Key), Station: s.ID, SourceKey: o.Key, Size: o.Size, ETag: o.ETag, Nominal: o.Time.Format(store.TimeFormat), State: "discovered", Expires: o.Time.Add(e.Config.Retention).Format(store.TimeFormat)})
				if err != nil {
					break
				}
			}
		}
		e.Store.Check(s.ID, err)
		if err != nil {
			slog.Error("source check failed", "station", s.ID, "error", err)
		}
	}
}
func validFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	b := make([]byte, 8)
	if _, err = io.ReadFull(f, b); err != nil {
		return err
	}
	if !strings.HasPrefix(string(b), "AR2V") && !strings.HasPrefix(string(b), "ARCHIVE2") && !(b[0] == 0x1f && b[1] == 0x8b) {
		return fmt.Errorf("not a supported Level II archive")
	}
	return nil
}
func (e *Engine) copy(ctx context.Context, r io.Reader, id string, expected int64) (string, string, error) {
	path := filepath.Join(e.Config.DataDir, "raw", id)
	f, err := os.CreateTemp(filepath.Dir(path), id+"-*.part")
	if err != nil {
		return "", "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(r, e.Config.MaxDownloadBytes+1))
	if err != nil {
		return "", "", err
	}
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	if n == 0 || n > e.Config.MaxDownloadBytes || (expected > 0 && n != expected) {
		return "", "", fmt.Errorf("download size invalid: got %d expected %d", n, expected)
	}
	if err = f.Sync(); err != nil {
		return "", "", err
	}
	if err = f.Close(); err != nil {
		return "", "", err
	}
	if err = validFile(tmp); err != nil {
		return "", "", err
	}
	hash, err := processor.Fingerprint(tmp)
	if err != nil {
		return "", "", err
	}
	err = os.Rename(tmp, path)
	return path, hash, err
}
func (e *Engine) Download(ctx context.Context, v store.Scan) error {
	req, err := http.NewRequestWithContext(ctx, "GET", e.Source.BaseURL+"/"+v.SourceKey, nil)
	if err != nil {
		return err
	}
	if v.ETag != "" {
		req.Header.Set("If-Match", v.ETag)
	}
	res, err := e.Source.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("download HTTP %d", res.StatusCode)
	}
	if res.ContentLength > e.Config.MaxDownloadBytes {
		return fmt.Errorf("download exceeds maximum size")
	}
	path, hash, err := e.copy(ctx, res.Body, v.ID, v.Size)
	if err != nil {
		return err
	}
	return e.Store.Downloaded(v.ID, path, hash)
}
func (e *Engine) Process(ctx context.Context, v store.Scan) error {
	s, ok := e.Config.Station(v.Station)
	if !ok {
		return fmt.Errorf("station is not configured")
	}
	tmp, err := os.MkdirTemp(filepath.Join(e.Config.DataDir, "frames"), ".processing-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	m, b, err := processor.Run(ctx, e.Config, s, v.Raw, tmp, v.SHA)
	if err != nil {
		return err
	}
	id := processor.FrameID(m)
	dest := filepath.Join(e.Config.DataDir, "frames", id)
	if _, err = os.Stat(dest); os.IsNotExist(err) {
		if err = os.Rename(tmp, dest); err != nil {
			return err
		}
	}
	if err = e.Store.Ready(v.ID, id, dest, b, m.VolumeTime.Start); err != nil {
		return err
	}
	slog.Info("frame ready", "station", v.Station, "scan", v.ID, "job", "processing", "frame", id)
	return nil
}
func (e *Engine) Import(ctx context.Context, path string) error {
	if err := validFile(path); err != nil {
		return err
	}
	hash, err := processor.Fingerprint(path)
	if err != nil {
		return err
	}
	id := ID("local:" + hash)
	if v, err := e.Store.Get(id); err == nil && v.State == "ready" {
		slog.Info("reusing frame", "frame", v.FrameID)
		return nil
	}
	// Read station from the archive header; a gzip wrapper is supported.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 24)
	var reader io.Reader = f
	magic := make([]byte, 2)
	if _, err = io.ReadFull(f, magic); err != nil {
		return err
	}
	f.Seek(0, 0)
	if magic[0] == 0x1f && magic[1] == 0x8b {
		gz, x := gzip.NewReader(f)
		if x != nil {
			return x
		}
		defer gz.Close()
		reader = gz
	}
	_, err = io.ReadFull(reader, header)
	if err != nil {
		return err
	}
	station := string(header[20:24])
	if _, ok := e.Config.Station(station); !ok {
		return fmt.Errorf("archive station %q is not configured", station)
	}
	if _, err = f.Seek(0, 0); err != nil {
		return err
	}
	raw, hash, err := e.copy(ctx, f, id, 0)
	if err != nil {
		return err
	}
	v := store.Scan{ID: id, Station: station, SourceKey: "local:" + hash, Nominal: store.Now(), State: "downloaded", Raw: raw, SHA: hash, Expires: time.Now().Add(e.Config.Retention).UTC().Format(store.TimeFormat)}
	if err = e.Store.Insert(v); err != nil {
		return err
	}
	if err = e.Process(ctx, v); err != nil {
		v.Attempts = 1
		e.Store.DB.Exec("UPDATE scans SET attempts=1 WHERE id=?", v.ID)
		e.Store.Fail(v, "downloaded", err)
	}
	return err
}
func (e *Engine) Cleanup() {
	vs, err := e.Store.Expired()
	if err != nil {
		slog.Error("retention", "error", err)
		return
	}
	for _, v := range vs {
		if v.Raw != "" {
			err = os.Remove(v.Raw)
			if err != nil && !os.IsNotExist(err) {
				continue
			}
		}
		if v.FrameID != "" {
			var refs int
			e.Store.DB.QueryRow("SELECT COUNT(*) FROM scans WHERE frame_id=? AND id!=?", v.FrameID, v.ID).Scan(&refs)
			if refs > 0 {
				if err = e.Store.Delete(v.ID); err != nil {
					slog.Error("retention delete", "error", err)
				}
				continue
			}
			_, dir, x := e.Store.Frame(v.FrameID)
			if x == nil {
				if err = os.RemoveAll(dir); err != nil {
					continue
				}
			}
		}
		if err = e.Store.Delete(v.ID); err != nil {
			slog.Error("retention delete", "scan", v.ID, "error", err)
		}
	}
}
func (e *Engine) Run(ctx context.Context) {
	e.Discover(ctx)
	var wg sync.WaitGroup
	for _, spec := range []struct {
		count    int
		from, to string
		fn       func(context.Context, store.Scan) error
	}{{e.Config.DownloadConcurrency, "discovered", "downloading", e.Download}, {e.Config.ProcessingConcurrency, "downloaded", "processing", e.Process}} {
		for i := 0; i < spec.count; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ctx.Err() == nil {
					v, err := e.Store.Claim(spec.from, spec.to, e.Config.MaxAttempts)
					if err == sql.ErrNoRows {
						select {
						case <-ctx.Done():
							return
						case <-time.After(500 * time.Millisecond):
						}
						continue
					}
					if err != nil {
						slog.Error("claim", "error", err)
						return
					}
					slog.Info("job started", "station", v.Station, "scan", v.ID, "job", spec.to, "attempt", v.Attempts)
					err = spec.fn(ctx, v)
					if err != nil && ctx.Err() != nil {
						return
					} // Recovery resumes interrupted work without consuming a failed attempt.
					if err != nil {
						slog.Error("job failed", "station", v.Station, "scan", v.ID, "job", spec.to, "error", err)
						if x := e.Store.Fail(v, spec.from, err); x != nil {
							slog.Error("persist failure", "error", x)
						}
					}
				}
			}()
		}
	}
	ticker := time.NewTicker(e.Config.PollInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-ticker.C:
			e.Discover(ctx)
			e.Cleanup()
		}
	}
	wg.Wait()
}

// Recover is called under the exclusive data-directory lock, before workers start.
func (e *Engine) Recover() error {
	if err := e.Store.Recover(); err != nil {
		return err
	}
	for _, station := range e.Config.Stations {
		scans, err := e.Store.List(station.ID, "", "9999", 100000)
		if err != nil {
			return err
		}
		for _, scan := range scans {
			if scan.State != "ready" {
				continue
			}
			b, dir, err := e.Store.Frame(scan.FrameID)
			_, imageErr := os.Stat(filepath.Join(dir, "image.png"))
			_, manifestErr := os.Stat(filepath.Join(dir, "manifest.json"))
			var m processor.Manifest
			if err != nil || imageErr != nil || manifestErr != nil || json.Unmarshal(b, &m) != nil || m.ConfigHash != processor.ConfigHash(e.Config, station) || m.ProcessorVersion != processor.Version {
				if _, err = e.Store.DB.Exec("UPDATE scans SET state='downloaded',frame_id=NULL,observation_time=NULL,attempts=0,next_retry='',error='' WHERE id=?", scan.ID); err != nil {
					return err
				}
			}
		}
	}
	for _, pattern := range []string{"raw/*.part", "frames/.processing-*"} {
		paths, _ := filepath.Glob(filepath.Join(e.Config.DataDir, pattern))
		for _, p := range paths {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
	}
	// Remove complete orphan frames left by a crash between filesystem and SQL publication.
	dirs, err := os.ReadDir(filepath.Join(e.Config.DataDir, "frames"))
	if err != nil {
		return err
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		var count int
		if err = e.Store.DB.QueryRow("SELECT COUNT(*) FROM scans WHERE frame_id=?", d.Name()).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if err = os.RemoveAll(filepath.Join(e.Config.DataDir, "frames", d.Name())); err != nil {
				return err
			}
			e.Store.DB.Exec("DELETE FROM frames WHERE id=?", d.Name())
		}
	}
	e.Cleanup()
	return nil
}
