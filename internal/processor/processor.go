package processor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/GerhardOfRivia/weer/internal/config"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

const Version = "0.1.0"

type Interval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
type Manifest struct {
	SchemaVersion int `json:"schema_version"`
	Source        struct {
		Identifier string `json:"identifier"`
		SHA256     string `json:"sha256"`
	} `json:"source"`
	Station struct {
		ID        string  `json:"id"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Altitude  float64 `json:"altitude"`
	} `json:"station"`
	VolumeTime         Interval          `json:"volume_time"`
	SweepTime          Interval          `json:"sweep_time"`
	Field              string            `json:"field"`
	Units              string            `json:"units"`
	SweepIndex         int               `json:"sweep_index"`
	Elevation          float64           `json:"elevation"`
	AvailableFields    []string          `json:"available_fields"`
	ReflectivitySweeps []json.RawMessage `json:"reflectivity_sweeps"`
	HasValid           bool              `json:"has_valid_measurements"`
	Projection         string            `json:"projection"`
	Extent             []float64         `json:"extent"`
	Width              int               `json:"width"`
	Height             int               `json:"height"`
	Orientation        string            `json:"pixel_orientation"`
	Image              string            `json:"image"`
	ColorScale         struct {
		Units      string    `json:"units"`
		Thresholds []float64 `json:"thresholds"`
		Colors     []string  `json:"colors"`
	} `json:"color_scale"`
	ProcessedAt      time.Time `json:"processed_at"`
	ProcessorVersion string    `json:"processor_version"`
	ConfigHash       string    `json:"render_config_sha256"`
	Warnings         []string  `json:"warnings"`
}

func Fingerprint(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), e
}
func RenderConfig(c config.Config, s config.Station) []byte {
	b, _ := json.Marshal(map[string]any{"size": c.Size, "coverage_km": c.CoverageKM, "station": s.ID, "latitude": s.Latitude, "longitude": s.Longitude})
	return b
}
func ConfigHash(c config.Config, s config.Station) string {
	h := sha256.Sum256(RenderConfig(c, s))
	return hex.EncodeToString(h[:])
}
func (m Manifest) Validate(station, hash, cfg string, size int) error {
	if m.SchemaVersion != 1 || m.Station.ID != station || m.Source.SHA256 != hash || m.ConfigHash != cfg || m.ProcessorVersion != Version || m.Field != "reflectivity" || m.Units != "dBZ" || m.Image != "image.png" || m.Projection != "EPSG:3857" || m.Orientation != "row_0_north" || m.Width != size || m.Height != size || m.SweepIndex < 0 || len(m.ReflectivitySweeps) == 0 {
		return fmt.Errorf("invalid manifest identity or raster contract")
	}
	if len(m.Extent) != 4 {
		return fmt.Errorf("invalid extent")
	}
	for _, v := range m.Extent {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 20037509 {
			return fmt.Errorf("invalid projection coordinate")
		}
	}
	if m.Extent[0] >= m.Extent[2] || m.Extent[1] >= m.Extent[3] || math.Abs(m.Station.Latitude) > 85 || math.Abs(m.Station.Longitude) > 180 {
		return fmt.Errorf("invalid coverage")
	}
	if m.VolumeTime.Start.IsZero() || m.VolumeTime.End.Before(m.VolumeTime.Start) || m.SweepTime.Start.Before(m.VolumeTime.Start) || m.SweepTime.End.After(m.VolumeTime.End) || m.SweepTime.End.Before(m.SweepTime.Start) || m.ProcessedAt.IsZero() {
		return fmt.Errorf("invalid observation intervals")
	}
	if len(m.ColorScale.Colors) != len(m.ColorScale.Thresholds) || len(m.ColorScale.Colors) < 2 || m.ColorScale.Units != "dBZ" {
		return fmt.Errorf("invalid color scale")
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(m.Source.SHA256) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(m.ConfigHash) || math.IsNaN(m.Elevation) || math.IsInf(m.Elevation, 0) || m.Elevation < -10 || m.Elevation > 90 {
		return fmt.Errorf("invalid fingerprint or elevation")
	}
	found := false
	for _, raw := range m.ReflectivitySweeps {
		var sweep struct {
			Index     int       `json:"index"`
			Elevation float64   `json:"elevation"`
			Start     time.Time `json:"start"`
			End       time.Time `json:"end"`
		}
		if json.Unmarshal(raw, &sweep) != nil || sweep.Start.Before(m.VolumeTime.Start) || sweep.End.After(m.VolumeTime.End) || sweep.End.Before(sweep.Start) {
			return fmt.Errorf("invalid sweep metadata")
		}
		if sweep.Index == m.SweepIndex && sweep.Elevation == m.Elevation && sweep.Start.Equal(m.SweepTime.Start) && sweep.End.Equal(m.SweepTime.End) {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("selected sweep absent from catalog")
	}
	for i, color := range m.ColorScale.Colors {
		if !regexp.MustCompile(`^#[a-fA-F0-9]{6}$`).MatchString(color) || (i > 0 && m.ColorScale.Thresholds[i] <= m.ColorScale.Thresholds[i-1]) {
			return fmt.Errorf("invalid color scale")
		}
	}
	return nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 65536 {
		keep := 65536 - b.Len()
		if len(p) > keep {
			p = p[:keep]
		}
		b.Buffer.Write(p)
	}
	return n, nil
}
func Run(ctx context.Context, c config.Config, s config.Station, input, output, hash string) (Manifest, []byte, error) {
	var m Manifest
	ctx, cancel := context.WithTimeout(ctx, c.ProcessingTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Python, "-m", "weer_processor", "--input", input, "--output", output, "--config", string(RenderConfig(c, s)))
	var stdout, stderr boundedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "MPLCONFIGDIR="+filepath.Join(c.DataDir, "mpl-cache"), "OMP_NUM_THREADS=1", "OPENBLAS_NUM_THREADS=1")
	if e := cmd.Run(); e != nil {
		return m, nil, fmt.Errorf("processor: %w; response=%s; stderr=%s", e, stdout.String(), stderr.String())
	}
	var protocol struct {
		OK bool `json:"ok"`
	}
	if e := json.Unmarshal(stdout.Bytes(), &protocol); e != nil || !protocol.OK {
		return m, nil, fmt.Errorf("invalid processor stdout: %s", stdout.String())
	}
	p := filepath.Join(output, "manifest.json")
	info, e := os.Lstat(p)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return m, nil, fmt.Errorf("invalid manifest file")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return m, nil, e
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&m); e != nil {
		return m, nil, e
	}
	if e = m.Validate(s.ID, hash, ConfigHash(c, s), c.Size); e != nil {
		return m, nil, e
	}
	img := filepath.Join(output, m.Image)
	info, e = os.Lstat(img)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 32<<20 {
		return m, nil, fmt.Errorf("invalid image file")
	}
	f, e := os.Open(img)
	if e != nil {
		return m, nil, e
	}
	defer f.Close()
	dimensions, e := png.DecodeConfig(f)
	if e != nil {
		return m, nil, e
	}
	if dimensions.Width != m.Width || dimensions.Height != m.Height {
		return m, nil, fmt.Errorf("PNG dimensions disagree")
	}
	if _, e = f.Seek(0, 0); e != nil {
		return m, nil, e
	}
	im, e := png.Decode(f)
	if e != nil {
		return m, nil, e
	}
	if im.Bounds().Dx() != m.Width || im.Bounds().Dy() != m.Height {
		return m, nil, fmt.Errorf("PNG dimensions disagree")
	}
	return m, b, nil
}
func FrameID(m Manifest) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d:%s:%s", m.Source.SHA256, m.Field, m.SweepIndex, m.ProcessorVersion, m.ConfigHash)))
	return hex.EncodeToString(h[:])
}
