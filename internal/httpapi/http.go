package httpapi

import (
	"database/sql"
	"encoding/json"
	"github.com/GerhardOfRivia/weer/internal/config"
	"github.com/GerhardOfRivia/weer/internal/store"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code string) {
	jsonOut(w, status, map[string]any{"error": map[string]string{"code": code, "message": http.StatusText(status)}})
}
func Handler(c config.Config, s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if e := s.DB.PingContext(r.Context()); e != nil {
			fail(w, 503, "database_unavailable")
			return
		}
		jsonOut(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/stations", func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, c.Stations) })
	mux.HandleFunc("GET /api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		status := []map[string]any{}
		for _, st := range c.Stations {
			item := s.Status(st.ID)
			for _, job := range item["errors"].([]map[string]any) {
				job["message"] = "Job failed; inspect service logs using this scan ID for full diagnostics."
				job["retry_eligible"] = job["attempts"].(int) < c.MaxAttempts
			}
			var pending int
			s.DB.QueryRow("SELECT COUNT(*) FROM scans WHERE station=? AND state NOT IN ('ready','deleting') AND attempts<?", st.ID, c.MaxAttempts).Scan(&pending)
			item["pending"] = pending
			status = append(status, item)
		}
		jsonOut(w, 200, map[string]any{"stations": status, "stale_after_seconds": c.StaleAfter.Seconds(), "retention_seconds": c.Retention.Seconds(), "basemap_url": c.BasemapURL, "basemap_attribution": c.BasemapAttribution, "max_attempts": c.MaxAttempts})
	})
	mux.HandleFunc("GET /api/v1/stations/{station}/scans", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("station")
		if _, ok := c.Station(id); !ok {
			fail(w, 404, "station_not_configured")
			return
		}
		to := time.Now().UTC()
		from := to.Add(-c.Retention)
		q := r.URL.Query()
		var e error
		for key, dest := range map[string]*time.Time{"from": &from, "to": &to} {
			if value := q.Get(key); value != "" {
				*dest, e = time.Parse(time.RFC3339Nano, value)
				if e != nil {
					fail(w, 400, "invalid_time")
					return
				}
			}
		}
		limit := 500
		if q.Get("limit") != "" {
			limit, e = strconv.Atoi(q.Get("limit"))
			if e != nil {
				fail(w, 400, "invalid_limit")
				return
			}
		}
		if limit < 1 || limit > 1000 || to.Before(from) || to.Sub(from) > 30*24*time.Hour {
			fail(w, 400, "invalid_range_or_limit")
			return
		}
		start, end := "", "9999"
		if q.Get("from") != "" || q.Get("to") != "" {
			start, end = from.UTC().Format(store.TimeFormat), to.UTC().Format(store.TimeFormat)
		}
		v, e := s.List(id, start, end, limit)
		if e != nil {
			fail(w, 500, "database_error")
			return
		}
		for i := range v {
			if v[i].Error != "" {
				v[i].Error = "Job failed; see service logs for this scan ID."
			}
		}
		jsonOut(w, 200, v)
	})
	mux.HandleFunc("GET /api/v1/scans/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Get(r.PathValue("id"))
		if e != nil {
			if e == sql.ErrNoRows {
				fail(w, 404, "scan_not_found")
			} else {
				fail(w, 500, "database_error")
			}
			return
		}
		if _, ok := c.Station(v.Station); !ok {
			fail(w, 404, "scan_not_found")
			return
		}
		if v.Error != "" {
			v.Error = "Job failed; see service logs for this scan ID."
		}
		jsonOut(w, 200, v)
	})
	for _, kind := range []string{"manifest", "image.png"} {
		mux.HandleFunc("GET /api/v1/frames/{id}/"+kind, func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			b, dir, e := s.Frame(id)
			if e != nil {
				fail(w, 404, "frame_not_found")
				return
			}
			var identity struct {
				Station struct {
					ID string `json:"id"`
				} `json:"station"`
			}
			if json.Unmarshal(b, &identity) != nil {
				fail(w, 500, "manifest_error")
				return
			}
			if _, ok := c.Station(identity.Station.ID); !ok {
				fail(w, 404, "frame_not_found")
				return
			}
			etag := `"` + id + "-" + kind + `"`
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(304)
				return
			}
			if kind == "manifest" {
				w.Header().Set("Content-Type", "application/json")
				w.Write(b)
			} else {
				http.ServeFile(w, r, filepath.Join(dir, "image.png"))
			}
		})
	}
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "route_not_found") })
	files := http.FileServer(http.Dir(c.WebDir))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		if _, e := os.Stat(filepath.Join(c.WebDir, filepath.Clean("/"+r.URL.Path))); e != nil && !strings.Contains(filepath.Base(r.URL.Path), ".") {
			http.ServeFile(w, r, filepath.Join(c.WebDir, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			fail(w, 405, "read_only_api")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		mux.ServeHTTP(w, r)
	})
}
