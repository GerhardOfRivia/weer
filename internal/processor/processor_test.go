package processor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestManifestValidation(t *testing.T) {
	hash := strings.Repeat("a", 64)
	var m Manifest
	m.SchemaVersion = 1
	m.Source.SHA256 = hash
	m.Station.ID = "KFTG"
	m.ConfigHash = hash
	m.ProcessorVersion = Version
	m.Field = "reflectivity"
	m.Units = "dBZ"
	m.Image = "image.png"
	m.Projection = "EPSG:3857"
	m.Orientation = "row_0_north"
	m.Width = 128
	m.Height = 128
	m.Extent = []float64{0, 0, 100, 100}
	start := time.Date(2024, 5, 20, 0, 0, 0, 0, time.UTC)
	m.VolumeTime = Interval{start, start.Add(time.Minute)}
	m.SweepTime = m.VolumeTime
	m.ProcessedAt = start
	m.ColorScale.Units = "dBZ"
	m.ColorScale.Colors = []string{"#000000", "#ffffff"}
	m.ColorScale.Thresholds = []float64{0, 10}
	m.ReflectivitySweeps = []json.RawMessage{json.RawMessage(`{"index":0,"elevation":0,"start":"2024-05-20T00:00:00Z","end":"2024-05-20T00:01:00Z"}`)}
	if e := m.Validate("KFTG", hash, hash, 128); e != nil {
		t.Fatal(e)
	}
	m.Image = "../secret"
	if e := m.Validate("KFTG", hash, hash, 128); e == nil {
		t.Fatal("path traversal accepted")
	}
	m.Image = "image.png"
	m.SweepTime.End = start.Add(2 * time.Minute)
	if e := m.Validate("KFTG", hash, hash, 128); e == nil {
		t.Fatal("invalid time accepted")
	}
}
