package ingest

import (
	"context"
	"github.com/GerhardOfRivia/weer/internal/config"
	"github.com/GerhardOfRivia/weer/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFailedDownloadAndProcessor(t *testing.T) {
	dir := t.TempDir()
	c := config.Config{DataDir: dir, DownloadTimeout: time.Second, MaxDownloadBytes: 1000, Python: "/does-not-exist", ProcessingTimeout: time.Second, Stations: []config.Station{{ID: "KFTG"}}}
	c.Database = filepath.Join(dir, "db")
	c.Prepare()
	s, err := store.Open(c.Database, c.Stations)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := New(c, s)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	e.Source.BaseURL = srv.URL
	v := store.Scan{ID: "a", Station: "KFTG", SourceKey: "key", State: "discovered", Expires: "9999"}
	s.Insert(v)
	if err = e.Download(context.Background(), v); err == nil {
		t.Fatal("expected HTTP failure")
	}
	if err = e.Process(context.Background(), v); err == nil {
		t.Fatal("expected subprocess failure")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "frames"))
	if len(entries) != 0 {
		t.Fatal("temporary files leaked")
	}
}

func TestDownloadAtomicityAndLimits(t *testing.T) {
	dir := t.TempDir()
	c := config.Config{DataDir: dir, Database: filepath.Join(dir, "db"), MaxDownloadBytes: 32}
	c.Prepare()
	s, err := store.Open(c.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	engine := New(c, s)
	for _, input := range []string{"not radar", "AR2V0006" + strings.Repeat("x", 40)} {
		if _, _, err = engine.copy(context.Background(), strings.NewReader(input), "test", 0); err == nil {
			t.Fatal("bad download accepted")
		}
		if _, err = os.Stat(filepath.Join(dir, "raw", "test")); !os.IsNotExist(err) {
			t.Fatal("partial download published")
		}
	}
	if _, _, err = engine.copy(context.Background(), strings.NewReader("AR2V0006abcdefgh"), "good", 16); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "raw"))
	if len(entries) != 1 {
		t.Fatal("temp leak")
	}
}

func TestCleanupDeletesExpiredFilesOnly(t *testing.T) {
	dir := t.TempDir()
	c := config.Config{DataDir: dir, Database: filepath.Join(dir, "db")}
	c.Prepare()
	s, err := store.Open(c.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := New(c, s)
	for _, state := range []string{"ready", "processing"} {
		raw := filepath.Join(dir, "raw", state)
		os.WriteFile(raw, []byte("radar"), 0600)
		s.Insert(store.Scan{ID: state, SourceKey: state, State: state, Raw: raw, Expires: "2000"})
	}
	e.Cleanup()
	if _, err = os.Stat(filepath.Join(dir, "raw", "ready")); !os.IsNotExist(err) {
		t.Fatal("expired original retained")
	}
	if _, err = os.Stat(filepath.Join(dir, "raw", "processing")); err != nil {
		t.Fatal("active original removed")
	}
}
func TestProcessorTimeout(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "slow")
	os.WriteFile(program, []byte("#!/bin/sh\nexec sleep 10\n"), 0700)
	c := config.Config{DataDir: dir, Database: filepath.Join(dir, "db"), Python: program, ProcessingTimeout: 30 * time.Millisecond, Stations: []config.Station{{ID: "KFTG"}}}
	c.Prepare()
	s, err := store.Open(c.Database, c.Stations)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e := New(c, s)
	start := time.Now()
	if err = e.Process(context.Background(), store.Scan{ID: "x", Station: "KFTG"}); err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout did not bound subprocess")
	}
}
