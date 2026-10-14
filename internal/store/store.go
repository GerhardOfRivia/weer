package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/GerhardOfRivia/weer/internal/config"
	_ "modernc.org/sqlite"
	"time"
)

const TimeFormat = "2006-01-02T15:04:05.000000000Z"

type Store struct{ DB *sql.DB }

const migration = `
CREATE TABLE IF NOT EXISTS migrations(version INTEGER PRIMARY KEY);
CREATE TABLE IF NOT EXISTS stations(id TEXT PRIMARY KEY, metadata TEXT NOT NULL, last_check TEXT, source_error TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS scans(id TEXT PRIMARY KEY, station TEXT NOT NULL, source_key TEXT NOT NULL UNIQUE, source_size INTEGER NOT NULL DEFAULT 0, source_etag TEXT NOT NULL DEFAULT '', nominal_time TEXT NOT NULL, observation_time TEXT, state TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, next_retry TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', raw_path TEXT NOT NULL DEFAULT '', sha256 TEXT NOT NULL DEFAULT '', frame_id TEXT, expires TEXT NOT NULL, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS frames(id TEXT PRIMARY KEY, scan_id TEXT NOT NULL, manifest TEXT NOT NULL, directory TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS scans_station_time ON scans(station,observation_time,id);
CREATE INDEX IF NOT EXISTS scans_work ON scans(state,next_retry,nominal_time);
INSERT OR IGNORE INTO migrations VALUES(1);`

func Open(path string, stations []config.Station) (*Store, error) {
	d, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	d.SetMaxOpenConns(1)
	s := &Store{d}
	if _, e = d.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;" + migration); e != nil {
		d.Close()
		return nil, e
	}
	if _, e = d.Exec("UPDATE stations SET enabled=0"); e != nil {
		return nil, e
	}
	for _, st := range stations {
		b, _ := json.Marshal(st)
		if _, e = d.Exec("INSERT INTO stations(id,metadata) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata,enabled=1", st.ID, string(b)); e != nil {
			return nil, e
		}
	}
	return s, nil
}
func Now() string { return time.Now().UTC().Format(TimeFormat) }

type Scan struct {
	ID        string `json:"id"`
	Station   string `json:"station"`
	SourceKey string `json:"source_key"`
	Size      int64  `json:"-"`
	ETag      string `json:"-"`
	Nominal   string `json:"nominal_time"`
	Time      string `json:"observation_time"`
	State     string `json:"state"`
	Attempts  int    `json:"attempts"`
	NextRetry string `json:"next_retry"`
	Error     string `json:"error"`
	Raw       string `json:"-"`
	SHA       string `json:"-"`
	FrameID   string `json:"frame_id"`
	Expires   string `json:"expires"`
}

const cols = "id,station,source_key,source_size,source_etag,nominal_time,COALESCE(observation_time,''),state,attempts,next_retry,error,raw_path,sha256,COALESCE(frame_id,''),expires"

func scan(row interface{ Scan(...any) error }) (v Scan, e error) {
	e = row.Scan(&v.ID, &v.Station, &v.SourceKey, &v.Size, &v.ETag, &v.Nominal, &v.Time, &v.State, &v.Attempts, &v.NextRetry, &v.Error, &v.Raw, &v.SHA, &v.FrameID, &v.Expires)
	return
}
func (s *Store) Insert(v Scan) error {
	_, e := s.DB.Exec(`INSERT OR IGNORE INTO scans(id,station,source_key,source_size,source_etag,nominal_time,state,raw_path,sha256,expires,created) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.Station, v.SourceKey, v.Size, v.ETag, v.Nominal, v.State, v.Raw, v.SHA, v.Expires, Now())
	return e
}
func (s *Store) Get(id string) (Scan, error) {
	return scan(s.DB.QueryRow("SELECT "+cols+" FROM scans WHERE id=?", id))
}
func (s *Store) Recover() error {
	_, e := s.DB.Exec(`UPDATE scans SET state=CASE state WHEN 'downloading' THEN 'discovered' WHEN 'processing' THEN 'downloaded' ELSE state END,attempts=MAX(0,attempts-1) WHERE state IN ('downloading','processing')`)
	return e
}
func (s *Store) Claim(from, to string, max int) (Scan, error) {
	tx, e := s.DB.Begin()
	if e != nil {
		return Scan{}, e
	}
	defer tx.Rollback()
	v, e := scan(tx.QueryRow("SELECT "+cols+" FROM scans WHERE station IN (SELECT id FROM stations WHERE enabled=1) AND state=? AND attempts<? AND next_retry<=? ORDER BY nominal_time DESC,id LIMIT 1", from, max, Now()))
	if e != nil {
		return v, e
	}
	_, e = tx.Exec("UPDATE scans SET state=?,attempts=attempts+1 WHERE id=?", to, v.ID)
	if e != nil {
		return v, e
	}
	e = tx.Commit()
	v.Attempts++
	v.State = to
	return v, e
}
func (s *Store) Fail(v Scan, stage string, e error) error {
	delay := time.Duration(1<<min(v.Attempts, 8)) * 15 * time.Second
	_, err := s.DB.Exec("UPDATE scans SET state=?,error=?,next_retry=? WHERE id=?", stage, e.Error(), time.Now().Add(delay).UTC().Format(TimeFormat), v.ID)
	return err
}
func (s *Store) Downloaded(id, path, hash string) error {
	_, e := s.DB.Exec("UPDATE scans SET state='downloaded',raw_path=?,sha256=?,attempts=0,next_retry='',error='' WHERE id=?", path, hash, id)
	return e
}
func (s *Store) Ready(id, frame, dir string, manifest []byte, observation time.Time) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec("INSERT OR IGNORE INTO frames VALUES(?,?,?,?)", frame, id, string(manifest), dir)
	if e != nil {
		return e
	}
	_, e = tx.Exec("UPDATE scans SET state='ready',frame_id=?,observation_time=?,nominal_time=CASE WHEN source_key LIKE 'local:%' THEN ? ELSE nominal_time END,error='',next_retry='' WHERE id=?", frame, observation.UTC().Format(TimeFormat), observation.UTC().Format(TimeFormat), id)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) List(station, from, to string, limit int) ([]Scan, error) {
	rows, e := s.DB.Query("SELECT "+cols+" FROM (SELECT * FROM scans WHERE station=? AND COALESCE(observation_time,nominal_time)>=? AND COALESCE(observation_time,nominal_time)<=? ORDER BY COALESCE(observation_time,nominal_time) DESC,id DESC LIMIT ?) ORDER BY COALESCE(observation_time,nominal_time),id", station, from, to, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	v := []Scan{}
	for rows.Next() {
		x, e := scan(rows)
		if e != nil {
			return nil, e
		}
		v = append(v, x)
	}
	return v, rows.Err()
}
func (s *Store) Frame(id string) ([]byte, string, error) {
	var b, dir string
	e := s.DB.QueryRow("SELECT manifest,directory FROM frames WHERE id=?", id).Scan(&b, &dir)
	return []byte(b), dir, e
}
func (s *Store) Check(station string, err error) {
	if err != nil {
		s.DB.Exec("UPDATE stations SET source_error=? WHERE id=?", err.Error(), station)
	} else {
		s.DB.Exec("UPDATE stations SET last_check=?,source_error='' WHERE id=?", Now(), station)
	}
}
func (s *Store) Status(id string) map[string]any {
	var check, err, discovered, ready string
	var pending int
	s.DB.QueryRow("SELECT COALESCE(last_check,''),source_error FROM stations WHERE id=?", id).Scan(&check, &err)
	s.DB.QueryRow("SELECT COALESCE(MAX(CASE WHEN source_key NOT LIKE 'local:%' THEN nominal_time END),''),COALESCE(MAX(CASE WHEN state='ready' THEN observation_time END),'') FROM scans WHERE station=?", id).Scan(&discovered, &ready)
	s.DB.QueryRow("SELECT COUNT(*) FROM scans WHERE station=? AND state NOT IN ('ready','deleting')", id).Scan(&pending)
	rows, e := s.DB.Query("SELECT id,state,error,attempts,next_retry FROM scans WHERE station=? AND error!='' ORDER BY created DESC LIMIT 10", id)
	errors := []map[string]any{}
	if e == nil {
		defer rows.Close()
		for rows.Next() {
			var sid, stage, msg, retry string
			var attempts int
			rows.Scan(&sid, &stage, &msg, &attempts, &retry)
			errors = append(errors, map[string]any{"scan_id": sid, "stage": stage, "message": msg, "attempts": attempts, "next_retry": retry})
		}
	}
	return map[string]any{"station": id, "last_successful_source_check": check, "source_error": err, "latest_discovered_nominal_time": discovered, "latest_ready_observation": ready, "pending": pending, "errors": errors}
}
func (s *Store) Expired() (out []Scan, err error) {
	tx, e := s.DB.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	_, e = tx.Exec("UPDATE scans SET state='deleting' WHERE expires<? AND state NOT IN ('downloading','processing')", Now())
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query("SELECT " + cols + " FROM scans WHERE state='deleting'")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		v, e := scan(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	rows.Close()
	return out, tx.Commit()
}
func (s *Store) Delete(id string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM frames WHERE id IN (SELECT frame_id FROM scans WHERE id=?) AND id NOT IN (SELECT frame_id FROM scans WHERE frame_id IS NOT NULL AND id!=?)", id, id); e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM scans WHERE id=? AND state='deleting'", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Close() error   { return s.DB.Close() }
func (s *Store) String() string { return fmt.Sprint("SQLite store") }
