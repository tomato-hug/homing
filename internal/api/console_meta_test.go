package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	consoleweb "github.com/tomato-hug/homing/web/console"
)

func consoleRequest(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req.RemoteAddr = "127.0.0.1:23456"
	req.AddCookie(&http.Cookie{Name: "homing_local", Value: "demo-token"})
	return req
}

func serveConsoleRequest(t *testing.T, d *Deps, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	NewMux(d).ServeHTTP(rec, consoleRequest(t, method, target, body))
	return rec
}

func TestConnectionInfoShape(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)

	unauthorized := httptest.NewRecorder()
	NewMux(d).ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18788/api/connection", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("without cookie status = %d, want 401", unauthorized.Code)
	}

	rec := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/connection", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var info map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info["product"] != "Homing" || info["runtime"] != "go" || info["port"] != float64(18788) {
		t.Fatalf("connection info = %+v", info)
	}
	if info["local_url"] != "http://127.0.0.1:18788" {
		t.Fatalf("local_url = %v", info["local_url"])
	}
	if info["data_dir"] != d.DataDir {
		t.Fatalf("data_dir = %v, want %s", info["data_dir"], d.DataDir)
	}
	if _, ok := info["autostart"].(map[string]any); !ok {
		t.Fatalf("autostart = %#v", info["autostart"])
	}
	security, _ := info["security"].(map[string]any)
	if security["local_auth"] != "cookie" || security["remote_auth"] != "device_token" {
		t.Fatalf("security = %+v", security)
	}
	cloud, _ := info["cloud"].(map[string]any)
	if cloud["configured"] != false || cloud["connected"] != false {
		t.Fatalf("default cloud status = %+v", cloud)
	}
	options, _ := info["options"].(map[string]any)
	lan, _ := options["lan"].(map[string]any)
	if lan["available"] != true || lan["recommended"] != true {
		t.Fatalf("options = %+v", options)
	}
	cloudOption, _ := options["cloud"].(map[string]any)
	selfHosted, _ := options["self_hosted"].(map[string]any)
	if cloudOption["available"] != true || selfHosted["available"] != true {
		t.Fatalf("cloud options = %+v", options)
	}
}

func TestConnectionInfoUsesCloudStatusProvider(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	d.SetCloudStatusProvider(func() CloudStatus {
		return CloudStatus{
			Configured: true,
			Connected:  true,
			Server:     "wss://relay.example.test/v1/ws",
			Peers:      2,
		}
	})

	rec := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/connection", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var info struct {
		Security struct {
			E2E string `json:"e2e"`
		} `json:"security"`
		Cloud CloudStatus `json:"cloud"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Security.E2E != "connected" {
		t.Fatalf("e2e = %q, want connected", info.Security.E2E)
	}
	if !info.Cloud.Configured || !info.Cloud.Connected || info.Cloud.Server != "wss://relay.example.test/v1/ws" || info.Cloud.Peers != 2 {
		t.Fatalf("cloud = %+v", info.Cloud)
	}
}

func TestConsoleMuxServesSpaAndAssets(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	mux := NewMux(d)

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, consoleRequest(t, http.MethodGet, "http://127.0.0.1:18788/console/sessions", ""))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `id="app"`) {
		t.Fatalf("SPA route = %d %s", page.Code, page.Body.String())
	}

	entries, err := consoleweb.Files.ReadDir("assets")
	if err != nil {
		t.Fatal(err)
	}
	assetName := ""
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".js") {
			assetName = entry.Name()
			break
		}
	}
	if assetName == "" {
		t.Fatal("no embedded JavaScript asset")
	}
	asset := httptest.NewRecorder()
	mux.ServeHTTP(asset, consoleRequest(t, http.MethodGet, "http://127.0.0.1:18788/console/assets/"+assetName, ""))
	if asset.Code != http.StatusOK || !strings.HasPrefix(asset.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("asset = %d %q", asset.Code, asset.Header().Get("Content-Type"))
	}
}

func TestStatsAndPauseRoundTrip(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	statsFile := filepath.Join(d.DataDir, "notify-stats.json")
	if err := os.WriteFile(statsFile, []byte(`{"day":"`+todayKeyForTest()+`","pushed":7,"merged":2,"by_level":{"P0":3,"P1":4}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status = %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	notify, _ := payload["notify"].(map[string]any)
	if notify["pushed"] != float64(7) || notify["merged"] != float64(2) {
		t.Fatalf("notify stats = %+v", notify)
	}

	paused := serveConsoleRequest(t, d, http.MethodPost, "http://127.0.0.1:18788/api/push/pause", `{"minutes":30,"reason":"demo"}`)
	if paused.Code != http.StatusOK {
		t.Fatalf("pause status = %d", paused.Code)
	}
	var state map[string]any
	if err := json.Unmarshal(paused.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state["paused"] != true || state["reason"] != "demo" {
		t.Fatalf("pause state = %+v", state)
	}

	resumed := serveConsoleRequest(t, d, http.MethodDelete, "http://127.0.0.1:18788/api/push/pause", "")
	if resumed.Code != http.StatusOK {
		t.Fatalf("resume status = %d", resumed.Code)
	}
	if err := json.Unmarshal(resumed.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state["paused"] != false {
		t.Fatalf("resume state = %+v", state)
	}
}

func todayKeyForTest() string {
	return time.Now().Format("2006-01-02")
}

func TestSetupRoundTrip(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)

	empty := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/setup", "")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"completed":false`) {
		t.Fatalf("empty setup = %d %s", empty.Code, empty.Body.String())
	}

	saved := serveConsoleRequest(t, d, http.MethodPost, "http://127.0.0.1:18788/api/setup",
		`{"agents":["codex","claude"],"method":"lan"}`)
	if saved.Code != http.StatusOK {
		t.Fatalf("save setup = %d %s", saved.Code, saved.Body.String())
	}
	loaded := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/setup", "")
	var payload map[string]any
	if err := json.Unmarshal(loaded.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["completed"] != true {
		t.Fatalf("setup payload = %+v", payload)
	}
	agents, _ := payload["chosen_agents"].([]any)
	if len(agents) != 2 || agents[0] != "codex" || agents[1] != "claude" {
		t.Fatalf("chosen_agents = %+v", agents)
	}
}

func TestAgentPathsAndLogs(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	paths := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/agent-paths", "")
	var pathPayload map[string]any
	if err := json.Unmarshal(paths.Body.Bytes(), &pathPayload); err != nil {
		t.Fatal(err)
	}
	rows, _ := pathPayload["paths"].(map[string]any)
	if _, ok := rows["codex"]; !ok {
		t.Fatalf("agent paths = %+v", pathPayload)
	}

	logFile := filepath.Join(d.DataDir, "homingd.log")
	fakeToken := strings.Repeat("ab", 16)
	fakeDeviceToken := "hm_" + strings.Repeat("demo", 6)
	content := "2026-01-01T10:00:00.000Z [info] 启动\n" +
		"2026-01-01T10:00:01.000Z Token: " + fakeToken + "\n" +
		"2026-01-01T10:00:02.000Z device " + fakeDeviceToken + "\n"
	if err := os.WriteFile(logFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/logs?limit=10", "")
	if logs.Code != http.StatusOK {
		t.Fatalf("logs status = %d", logs.Code)
	}
	if strings.Contains(logs.Body.String(), fakeToken) || strings.Contains(logs.Body.String(), fakeDeviceToken) {
		t.Fatalf("logs leaked a token: %s", logs.Body.String())
	}
	if !strings.Contains(logs.Body.String(), "已隐藏") {
		t.Fatalf("logs were not redacted: %s", logs.Body.String())
	}
}

func TestAutostartIsLoopbackOnly(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	req := consoleRequest(t, http.MethodGet, "http://127.0.0.1:18788/api/autostart", "")
	req.RemoteAddr = "192.168.1.20:5555"
	rec := httptest.NewRecorder()
	NewMux(d).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("remote autostart status = %d, want 403", rec.Code)
	}

	local := serveConsoleRequest(t, d, http.MethodGet, "http://127.0.0.1:18788/api/autostart", "")
	if local.Code != http.StatusOK {
		t.Fatalf("local autostart status = %d", local.Code)
	}
}
