package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomato-hug/homing/internal/platform"
)

func TestDevicesKeepNodeTokenHashKey(t *testing.T) {
	dir := t.TempDir()
	paths := platform.NewPaths(dir)
	raw := `{"version":1,"devices":[{"id":"0123456789ab","name":"demo-phone","platform":"android","token_hash":"deadbeef","last_seen_ms":1}],"offers":[]}`
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	devices := s.Devices()
	if len(devices) != 1 || devices[0].TokenSHA256 != "deadbeef" {
		t.Fatalf("devices = %+v", devices)
	}
	if err := s.SaveDevices(devices, nil); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "devices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `"token_hash"`) || strings.Contains(string(saved), `"token_sha256"`) {
		t.Fatalf("devices.json key changed: %s", saved)
	}
}
