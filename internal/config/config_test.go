package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExampleAndValidation(t *testing.T) {
	c, e := Load("../../configs/weer.yaml")
	if e != nil {
		t.Fatal(e)
	}
	if c.Stations[0].ID != "KFTG" {
		t.Fatal(c)
	}
	c.ProcessingConcurrency = 0
	if c.Validate() == nil {
		t.Fatal("invalid concurrency")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("unknown_key: value"), 0600)
	if _, e = Load(path); e == nil {
		t.Fatal("unknown key accepted")
	}
}
