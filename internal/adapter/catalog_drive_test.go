package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tomato-hug/homing/internal/driver"
)

// TestCatalogDriveHelper is the fake ACP process used by
// TestEnableCatalogDriveWritesOnlyAfterHandshake. It is inert unless the wrapper
// script sets the environment variable, so a normal `go test` run ignores it.
func TestCatalogDriveHelper(t *testing.T) {
	if os.Getenv("HOMING_FAKE_ACP") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion":   1,
				"agentCapabilities": map[string]any{"loadSession": true},
				"agentInfo":         map[string]any{"name": "fake-acp"},
			}
		case "session/new":
			result = map[string]any{"sessionId": "demo-acp-session-1"}
		default:
			result = map[string]any{}
		}
		_ = encoder.Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": result,
		})
	}
}

func TestEnableCatalogDriveWritesOnlyAfterHandshake(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake ACP wrapper uses a POSIX shell")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := filepath.Join(home, "Application Support", "Homing")
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	testBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	wrapper := fmt.Sprintf(
		"#!/bin/sh\nHOMING_FAKE_ACP=1 exec %q -test.run='^TestCatalogDriveHelper$'\n",
		testBinary,
	)
	if err := os.WriteFile(filepath.Join(binDir, "hermes"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	driver.ResetReadiness("hermes")
	t.Cleanup(func() { driver.ResetReadiness("hermes") })

	result, err := EnableCatalogDrive(context.Background(), "hermes", dataDir)
	if err != nil {
		t.Fatalf("EnableCatalogDrive: %v", err)
	}
	if !result.OK || result.Probe.SessionID != "demo-acp-session-1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Probe.AgentImplementation == nil || !result.Drive.Enabled {
		t.Fatalf("probe/drive = %+v / %+v", result.Probe, result.Drive)
	}
	raw, err := os.ReadFile(result.File)
	if err != nil {
		t.Fatal(err)
	}
	manifest, problems, err := LoadManifestFromString(string(raw))
	if err != nil || len(problems) > 0 {
		t.Fatalf("manifest err=%v problems=%v", err, problems)
	}
	if manifest.ID != "hermes" || manifest.Drive == nil || manifest.Sessions.Source != "" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.Drive.Command[0] != "hermes" || manifest.Drive.Command[1] != "acp" {
		t.Fatalf("drive = %+v", manifest.Drive)
	}

	// A catalog entry may already have a built-in ACP spec (Hermes does). Its
	// manifest still owns the enabled state, but it must not try to replace the
	// built-in spec and poison /api/adapters with a duplicate-driver error.
	refresh()
	for _, problem := range Errors() {
		if problem.ID == "drive" {
			t.Fatalf("enabling a catalog drive broke the drive registry: %+v", problem)
		}
	}

	removed, err := RemoveCatalogDrive("hermes", dataDir)
	if err != nil {
		t.Fatalf("RemoveCatalogDrive: %v", err)
	}
	if !removed.ManifestRemoved || removed.Trash == "" {
		t.Fatalf("removed = %+v", removed)
	}
	if _, err := os.Stat(result.File); !os.IsNotExist(err) {
		t.Fatalf("drive-only manifest still exists: %v", err)
	}
	if _, err := os.Stat(removed.Trash); err != nil {
		t.Fatalf("trash file missing: %v", err)
	}
}
