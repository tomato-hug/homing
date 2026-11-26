package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDesktopWindowOpenAndStream(t *testing.T) {
	dir := t.TempDir()
	deps := &Deps{DataDir: dir, Version: "0.11.6-test"}
	defer deps.Close()

	st, err := deps.Store()
	if err != nil {
		t.Fatal(err)
	}
	tok := st.Token()
	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	postWindow := func(path string) (int, map[string]any) {
		body, _ := json.Marshal(map[string]any{"action": "open", "path": path})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/desktop/window", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// 1. Without a subscribed GUI shell, handled_by_shell is false.
	code, res := postWindow("/console/hub")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if handled, _ := res["handled_by_shell"].(bool); handled {
		t.Fatalf("expected handled_by_shell=false when no GUI shell is attached, got %+v", res)
	}

	// 2. Attach simulated GUI shell subscriber on /api/desktop/window/stream.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streamReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/desktop/window/stream", nil)
	streamReq.Header.Set("Authorization", "Bearer "+tok)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()

	gotEvent := make(chan desktopWindowEvent, 1)
	go func() {
		scanner := bufio.NewScanner(streamResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				var ev desktopWindowEvent
				if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &ev) == nil {
					gotEvent <- ev
					return
				}
			}
		}
	}()

	// Wait briefly for stream subscription to register (`: ready` flushed).
	time.Sleep(30 * time.Millisecond)

	code, res = postWindow("/console/sessions?session=demo-session-1")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if handled, _ := res["handled_by_shell"].(bool); !handled {
		t.Fatalf("expected handled_by_shell=true when GUI shell is attached, got %+v", res)
	}

	select {
	case ev := <-gotEvent:
		if ev.Path != "/console/sessions?session=demo-session-1" || ev.Action != "open" {
			t.Fatalf("unexpected window event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for desktop window SSE event")
	}

	// 3. Sanitize external URL attempts back to /console/.
	_, res = postWindow("https://evil.example.com/phish")
	if res["path"] != "/console/" {
		t.Fatalf("expected external path to be sanitized to /console/, got %v", res["path"])
	}
}

func TestDesktopOpenExternalAndRelayProxy(t *testing.T) {
	dir := t.TempDir()
	deps := &Deps{DataDir: dir, Version: "0.11.6-test"}
	defer deps.Close()

	st, err := deps.Store()
	if err != nil {
		t.Fatal(err)
	}
	tok := st.Token()
	srv := httptest.NewServer(NewMux(deps))
	defer srv.Close()

	// Mock OS browser launcher
	prevLauncher := openExternalBrowserFunc
	var launchedURL string
	openExternalBrowserFunc = func(u string) error {
		launchedURL = u
		return nil
	}
	defer func() { openExternalBrowserFunc = prevLauncher }()

	doPost := func(path string, payload map[string]any) (int, map[string]any) {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	// 1. Valid HTTPS URL launches browser
	code, res := doPost("/api/desktop/open-external", map[string]any{"url": "https://relay.example.com/account"})
	if code != http.StatusOK || launchedURL != "https://relay.example.com/account" {
		t.Fatalf("open-external failed: code=%d res=%+v launched=%q", code, res, launchedURL)
	}

	// 2. Reject javascript: or file:// schemes
	code, _ = doPost("/api/desktop/open-external", map[string]any{"url": "javascript:alert(1)"})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for javascript: scheme, got %d", code)
	}

	// 3. Relay proxy forwards /v1/account/config & /v1/admin/overview with Bearer token
	fakeRelay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/account/config":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "auth_mode": "managed", "registration_mode": "open"})
		case "/v1/admin/overview":
			if r.Header.Get("Authorization") != "Bearer demo-admin-tok" {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "admin_unauthorized"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "online_devices": 3})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fakeRelay.Close()

	wsAddr := "ws://" + strings.TrimPrefix(fakeRelay.URL, "http://") + "/v1/ws"
	code, res = doPost("/api/desktop/relay-proxy", map[string]any{
		"server": wsAddr,
		"method": "GET",
		"path":   "/v1/account/config",
	})
	if code != http.StatusOK || res["ok"] != true {
		t.Fatalf("relay-proxy account/config failed: code=%d res=%+v", code, res)
	}

	code, res = doPost("/api/desktop/relay-proxy", map[string]any{
		"server":      wsAddr,
		"method":      "GET",
		"path":        "/v1/admin/overview",
		"admin_token": "demo-admin-tok",
	})
	if code != http.StatusOK || res["ok"] != true {
		t.Fatalf("relay-proxy admin/overview failed: code=%d res=%+v", code, res)
	}

	// 4. Reject disallowed paths
	code, _ = doPost("/api/desktop/relay-proxy", map[string]any{
		"server": wsAddr,
		"method": "GET",
		"path":   "/etc/passwd",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed proxy path, got %d", code)
	}
}
