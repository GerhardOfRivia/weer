package store

import (
	"database/sql"
	"errors"
	"github.com/GerhardOfRivia/weer/internal/config"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	stations := []config.Station{{ID: "KFTG"}}
	s, e := Open(path, stations)
	if e != nil {
		t.Fatal(e)
	}
	v := Scan{ID: "a", Station: "KFTG", SourceKey: "key", State: "discovered", Nominal: Now(), Expires: time.Now().Add(time.Hour).UTC().Format(TimeFormat)}
	for i := 0; i < 2; i++ {
		if e = s.Insert(v); e != nil {
			t.Fatal(e)
		}
	}
	got, e := s.Claim("discovered", "downloading", 4)
	if e != nil || got.ID != "a" {
		t.Fatal(got, e)
	}
	if _, e = s.Claim("discovered", "downloading", 4); e != sql.ErrNoRows {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path, stations)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Recover(); e != nil {
		t.Fatal(e)
	}
	got, e = s.Claim("discovered", "downloading", 4)
	if e != nil || got.Attempts != 1 {
		t.Fatal(got, e)
	}
	s.Fail(got, "discovered", errors.New("network down"))
	got, _ = s.Get("a")
	if got.Error != "network down" || got.NextRetry == "" {
		t.Fatal(got)
	}
	if _, e = s.Claim("discovered", "downloading", 4); e != sql.ErrNoRows {
		t.Fatal("backoff ignored")
	}
}
func TestRetentionExcludesActive(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "test.db"), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, state := range []string{"ready", "processing", "downloading", "downloaded"} {
		s.Insert(Scan{ID: state, SourceKey: state, State: state, Expires: "2000", Nominal: "2000"})
	}
	v, e := s.Expired()
	if e != nil || len(v) != 2 {
		t.Fatal(v, e)
	}
	for _, x := range v {
		if x.State != "deleting" {
			t.Fatal(x)
		}
	}
}

func TestLocalImportDoesNotInventSourceObservationTime(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"), []config.Station{{ID: "KFTG"}})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.Insert(Scan{ID: "local", Station: "KFTG", SourceKey: "local:hash", State: "downloaded", Nominal: Now(), Expires: "9999"})
	s.Insert(Scan{ID: "source", Station: "KFTG", SourceKey: "2024/archive", State: "discovered", Nominal: "2024-05-20T00:00:00.000000000Z", Expires: "9999"})
	status := s.Status("KFTG")
	if status["latest_discovered_nominal_time"] != "2024-05-20T00:00:00.000000000Z" || status["latest_ready_observation"] != "" {
		t.Fatal(status)
	}
}
