package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Station struct {
	ID        string  `json:"id" yaml:"id"`
	Name      string  `json:"name" yaml:"name"`
	Latitude  float64 `json:"latitude" yaml:"latitude"`
	Longitude float64 `json:"longitude" yaml:"longitude"`
}
type Config struct {
	Listen                string        `yaml:"listen"`
	DataDir               string        `yaml:"data_dir"`
	Database              string        `yaml:"database"`
	WebDir                string        `yaml:"web_dir"`
	Stations              []Station     `yaml:"stations"`
	Bucket                string        `yaml:"bucket"`
	Region                string        `yaml:"region"`
	PollInterval          time.Duration `yaml:"poll_interval"`
	History               time.Duration `yaml:"history"`
	Retention             time.Duration `yaml:"retention"`
	StaleAfter            time.Duration `yaml:"stale_after"`
	DownloadConcurrency   int           `yaml:"download_concurrency"`
	ProcessingConcurrency int           `yaml:"processing_concurrency"`
	MaxDownloadBytes      int64         `yaml:"max_download_bytes"`
	DownloadTimeout       time.Duration `yaml:"download_timeout"`
	ProcessingTimeout     time.Duration `yaml:"processing_timeout"`
	Python                string        `yaml:"python"`
	Size                  int           `yaml:"render_size"`
	CoverageKM            float64       `yaml:"coverage_km"`
	MaxAttempts           int           `yaml:"max_attempts"`
	BasemapURL            string        `yaml:"basemap_url"`
	BasemapAttribution    string        `yaml:"basemap_attribution"`
}

func Load(path string) (Config, error) {
	c := Config{Listen: "127.0.0.1:8080", DataDir: "data", Database: "data/weer.db", WebDir: "web/dist", Bucket: "unidata-nexrad-level2", Region: "us-east-1", PollInterval: time.Minute, History: 2 * time.Hour, Retention: 6 * time.Hour, StaleAfter: 15 * time.Minute, DownloadConcurrency: 1, ProcessingConcurrency: 1, MaxDownloadBytes: 100 << 20, DownloadTimeout: 2 * time.Minute, ProcessingTimeout: 3 * time.Minute, Python: "python3", Size: 1024, CoverageKM: 230, MaxAttempts: 4}
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	d := yaml.NewDecoder(f)
	d.KnownFields(true)
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if _, _, e := net.SplitHostPort(c.Listen); e != nil {
		return fmt.Errorf("listen must be host:port: %w", e)
	}
	if c.DataDir == "" || c.Database == "" || c.Python == "" || c.WebDir == "" {
		return fmt.Errorf("data_dir, database, python and web_dir are required")
	}
	if c.History <= 0 || c.History > 7*24*time.Hour || c.Retention < c.History || c.Retention > 30*24*time.Hour || c.PollInterval < time.Second || c.StaleAfter <= 0 || c.DownloadTimeout <= 0 || c.ProcessingTimeout <= 0 {
		return fmt.Errorf("invalid durations: history <= 7d, history <= retention <= 30d; timeouts positive")
	}
	if c.DownloadConcurrency < 1 || c.DownloadConcurrency > 8 || c.ProcessingConcurrency < 1 || c.ProcessingConcurrency > 4 || c.MaxAttempts < 1 || c.MaxAttempts > 10 || c.MaxDownloadBytes < 1024 || c.MaxDownloadBytes > 1<<30 || c.Size < 128 || c.Size > 2048 || math.IsNaN(c.CoverageKM) || math.IsInf(c.CoverageKM, 0) || c.CoverageKM < 10 || c.CoverageKM > 460 {
		return fmt.Errorf("invalid concurrency, attempts, download size or render settings")
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`).MatchString(c.Bucket) || !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(c.Region) {
		return fmt.Errorf("invalid S3 bucket or region")
	}
	if len(c.Stations) == 0 {
		return fmt.Errorf("configure at least one station")
	}
	seen := map[string]bool{}
	for _, s := range c.Stations {
		if !regexp.MustCompile(`^[A-Z][A-Z0-9]{3}$`).MatchString(s.ID) || seen[s.ID] || s.Name == "" || math.IsNaN(s.Latitude) || math.IsNaN(s.Longitude) || s.Latitude < -80 || s.Latitude > 80 || s.Longitude < -180 || s.Longitude > 180 {
			return fmt.Errorf("invalid or duplicate station %q", s.ID)
		}
		seen[s.ID] = true
	}
	if c.BasemapURL != "" && !strings.HasPrefix(c.BasemapURL, "https://") {
		return fmt.Errorf("basemap_url must be HTTPS or empty")
	}
	return nil
}
func (c Config) Station(id string) (Station, bool) {
	for _, s := range c.Stations {
		if s.ID == id {
			return s, true
		}
	}
	return Station{}, false
}
func (c Config) Prepare() error {
	for _, p := range []string{c.DataDir, filepath.Dir(c.Database), filepath.Join(c.DataDir, "raw"), filepath.Join(c.DataDir, "frames")} {
		if e := os.MkdirAll(p, 0750); e != nil {
			return e
		}
	}
	return nil
}
