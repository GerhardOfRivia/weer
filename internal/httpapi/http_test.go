package httpapi

import (
	"encoding/json"
	"github.com/GerhardOfRivia/weer/internal/config"
	"github.com/GerhardOfRivia/weer/internal/store"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestAPIValidationAndOrdering(t *testing.T) {
	c := config.Config{Stations: []config.Station{{ID: "KFTG"}}, Retention: 6 * time.Hour}
	s, e := store.Open(filepath.Join(t.TempDir(), "db"), c.Stations)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, id := range []string{"b", "a"} {
		s.Insert(store.Scan{ID: id, Station: "KFTG", SourceKey: id, Nominal: "2024-05-20T00:00:00.000000000Z", State: "ready", Expires: "9999"})
	}
	h := Handler(c, s)
	for _, q := range []string{"limit=-1", "limit=1001", "from=garbage", "from=2024-01-01T00:00:00Z&to=2025-01-01T00:00:00Z"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/stations/KFTG/scans?"+q, nil))
		if w.Code != 400 {
			t.Fatalf("%s: %d", q, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/stations/KFTG/scans", nil))
	var scans []store.Scan
	if e = json.Unmarshal(w.Body.Bytes(), &scans); e != nil || len(scans) != 2 || scans[0].ID != "a" {
		t.Fatal(w.Body.String(), e)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/stations/KABC/scans", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
