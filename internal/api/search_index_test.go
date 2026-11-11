package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomato-hug/homing/internal/platform"
)

func TestDeleteSearchIndexDisablesAndDoesNotRecreateOnStatus(t *testing.T) {
	d := writeStore(t, demoEvents, demoState)
	path := platform.NewPaths(d.DataDir).Index

	if err := os.WriteFile(path, []byte("demo index"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("index fixture missing: %v", err)
	}

	deleted := serveConsoleRequest(t, d, http.MethodPost, "http://127.0.0.1:18788/api/search/index",
		`{"action":"delete"}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	var deletedPayload map[string]any
	if err := json.Unmarshal(deleted.Body.Bytes(), &deletedPayload); err != nil {
		t.Fatal(err)
	}
	if deletedPayload["ok"] != true || deletedPayload["enabled"] != false {
		t.Fatalf("delete payload = %+v", deletedPayload)
	}

	status := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/search/status", "")
	if status.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status.Code, status.Body.String())
	}
	var payload struct {
		Enabled bool `json:"enabled"`
		Stats   struct {
			Path   string `json:"path"`
			Exists bool   `json:"exists"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Enabled {
		t.Fatal("deleting the index should disable transcript search")
	}
	if payload.Stats.Exists {
		t.Fatal("status reported a deleted index as present")
	}
	if payload.Stats.Path != path {
		t.Fatalf("path = %q, want %q", payload.Stats.Path, path)
	}
	if _, err := os.Stat(filepath.Clean(path)); !os.IsNotExist(err) {
		t.Fatalf("status recreated the deleted index: %v", err)
	}
}
