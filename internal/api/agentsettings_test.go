package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomato-hug/homing/internal/notify"
)

func TestAgentSettingsAndHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)

	settings := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/agents/settings", "")
	if settings.Code != http.StatusOK {
		t.Fatalf("settings status = %d, body = %s", settings.Code, settings.Body.String())
	}
	var payload struct {
		Settings map[string]map[string]any `json:"settings"`
		Order    []string                  `json:"order"`
	}
	if err := json.Unmarshal(settings.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload.Settings["codex"]; !ok {
		t.Fatalf("settings missing codex: %+v", payload.Settings)
	}
	if len(payload.Order) == 0 {
		t.Fatal("empty agent order")
	}

	patched := serveConsoleRequest(t, d, http.MethodPatch, "http://127.0.0.1:18788/api/agents/codex/settings",
		`{"push_to_phone":true,"push_signal":"watch"}`)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patched.Code, patched.Body.String())
	}
	var row map[string]any
	if err := json.Unmarshal(patched.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["connected"] != true || row["push_to_phone"] != true || row["push_signal"] != "watch" {
		t.Fatalf("patched row = %+v", row)
	}
	if _, err := os.Stat(filepath.Join(d.DataDir, "agents", "codex.json")); err != nil {
		t.Fatalf("settings file missing: %v", err)
	}

	ordered := serveConsoleRequest(t, d, http.MethodPut, "http://127.0.0.1:18788/api/agents/order",
		`{"order":["claude","codex","workbuddy"]}`)
	if ordered.Code != http.StatusOK {
		t.Fatalf("order status = %d, body = %s", ordered.Code, ordered.Body.String())
	}

	claudeFile := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claudeFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeFile, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"http","url":"http://127.0.0.1:18787/hook/claude"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	hooks := serveConsoleRequest(t, d, http.MethodPost, "http://127.0.0.1:18788/api/agents/claude/hooks", "")
	if hooks.Code != http.StatusOK {
		t.Fatalf("hooks status = %d, body = %s", hooks.Code, hooks.Body.String())
	}
	raw, err := os.ReadFile(claudeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "127.0.0.1:18788/hook/claude") {
		t.Fatalf("hook was not installed: %s", raw)
	}

	disconnected := serveConsoleRequest(t, d, http.MethodPost, "http://127.0.0.1:18788/api/agents/codex/disconnect", "")
	if disconnected.Code != http.StatusOK {
		t.Fatalf("disconnect status = %d, body = %s", disconnected.Code, disconnected.Body.String())
	}
	var disconnectPayload map[string]any
	if err := json.Unmarshal(disconnected.Body.Bytes(), &disconnectPayload); err != nil {
		t.Fatal(err)
	}
	settingsRow, _ := disconnectPayload["settings"].(map[string]any)
	if settingsRow["connected"] != false || settingsRow["push_to_phone"] != false {
		t.Fatalf("disconnect settings = %+v", disconnectPayload)
	}
}

func TestAgentHooksRequireLoopback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	req := consoleRequest(t, http.MethodPost, "http://127.0.0.1:18788/api/agents/claude/hooks", "")
	req.RemoteAddr = "192.168.1.20:23456"
	rec := httptest.NewRecorder()
	NewMux(d).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAgentTestPushUsesRealDeliverySink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	patched := serveConsoleRequest(t, d, http.MethodPatch, "http://127.0.0.1:18788/api/agents/codex/settings",
		`{"push_to_phone":true}`)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patched.Code, patched.Body.String())
	}
	var delivered notify.Payload
	SetTestPushSink(func(_ context.Context, payload notify.Payload) (int, error) {
		delivered = payload
		return 1, nil
	})
	t.Cleanup(func() { SetTestPushSink(nil) })

	response := serveConsoleRequest(t, d, http.MethodPost, "http://127.0.0.1:18788/api/agents/codex/test-push", "")
	if response.Code != http.StatusOK {
		t.Fatalf("test push status = %d, body = %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true || result["placeholder"] != false || result["delivered"] != float64(1) {
		t.Fatalf("test push result = %+v", result)
	}
	if delivered.Source != "codex" || delivered.Level != "P1" || !delivered.Temp {
		t.Fatalf("delivered payload = %+v", delivered)
	}
}

func TestAgentTestPushReportsMissingTransport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	serveConsoleRequest(t, d, http.MethodPatch, "http://127.0.0.1:18788/api/agents/codex/settings",
		`{"push_to_phone":true}`)
	SetTestPushSink(nil)
	response := serveConsoleRequest(t, d, http.MethodPost, "http://127.0.0.1:18788/api/agents/codex/test-push", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
