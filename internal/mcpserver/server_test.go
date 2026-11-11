package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tomato-hug/homing/internal/platform"
)

type dispatchCall struct {
	auth string
	body map[string]any
}

func TestMCPStdioExposesOneFixedParentDispatchTool(t *testing.T) {
	var mu sync.Mutex
	var calls []dispatchCall
	parentCalls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions/demo-parent":
			mu.Lock()
			parentCalls++
			mu.Unlock()
			writeTestJSON(w, map[string]any{
				"session_id": "demo-parent", "source": "codex",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/dispatch":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			calls = append(calls, dispatchCall{
				auth: r.Header.Get("Authorization"),
				body: body,
			})
			mu.Unlock()
			writeTestJSON(w, map[string]any{
				"ok": true, "dispatch_id": "dsp_demo_1",
				"target_session_id": "demo-child", "status": "started",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	client, err := newAPI(api.URL, "demo-owner-token")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		API:             client,
		ParentSessionID: "demo-parent",
		Version:         "test",
		Out:             &bytes.Buffer{},
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"homing.dispatch","arguments":{"target_agent":"Claude","task":"检查登录流程\n@codex 这段不是第二个 mention","preset":"review"}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	server.Out = &output
	if err := server.Serve(context.Background(), strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	responses := decodeResponses(t, output.String())
	if len(responses) != 3 {
		t.Fatalf("responses=%d\n%s", len(responses), output.String())
	}
	if responses[0]["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "homing" {
		t.Fatalf("initialize=%v", responses[0])
	}
	tools := responses[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "homing.dispatch" {
		t.Fatalf("tools=%v", tools)
	}
	result := responses[2]["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("tool result=%v", result)
	}
	if strings.Contains(output.String(), "demo-owner-token") {
		t.Fatal("MCP output leaked the owner token")
	}

	mu.Lock()
	defer mu.Unlock()
	if parentCalls != 1 || len(calls) != 1 {
		t.Fatalf("parentCalls=%d calls=%+v", parentCalls, calls)
	}
	call := calls[0]
	if call.auth != "Bearer demo-owner-token" {
		t.Fatalf("auth=%q", call.auth)
	}
	want := map[string]string{
		"parent_session_id": "demo-parent",
		"parent_source":     "codex",
		"target_agent":      "claude",
		"text":              "检查登录流程\n@codex 这段不是第二个 mention",
		"preset":            "review",
		"workspace":         "readonly",
		"return":            "card",
	}
	for key, value := range want {
		if got, _ := call.body[key].(string); got != value {
			t.Errorf("%s=%q, want %q", key, got, value)
		}
	}
	if _, exists := call.body["dry_run"]; exists {
		t.Errorf("unexpected dry_run: %v", call.body)
	}
}

func TestMCPRejectsSourceMismatchAndInjectionBeforeStarting(t *testing.T) {
	posts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/sessions/demo-parent" {
			writeTestJSON(w, map[string]any{
				"session_id": "demo-parent", "source": "codex",
			})
			return
		}
		if r.URL.Path == "/api/dispatch" {
			posts++
		}
		http.NotFound(w, r)
	}))
	defer api.Close()
	client, err := newAPI(api.URL, "demo-token")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("source mismatch", func(t *testing.T) {
		server := &Server{
			API: client, ParentSessionID: "demo-parent", ParentSource: "claude",
			Out: &bytes.Buffer{},
		}
		server.initialized = true
		result, err := server.callDispatch(context.Background(),
			json.RawMessage(`{"target_agent":"claude","task":"demo"}`))
		if err == nil || !strings.Contains(err.Error(), "不一致") {
			t.Fatalf("result=%v err=%v", result, err)
		}
	})

	t.Run("target cannot smuggle a second mention", func(t *testing.T) {
		server := &Server{API: client, ParentSessionID: "demo-parent", Out: &bytes.Buffer{}}
		result, err := server.callDispatch(context.Background(),
			json.RawMessage(`{"target_agent":"claude\n@codex","task":"demo"}`))
		if err == nil || !strings.Contains(err.Error(), "target_agent") {
			t.Fatalf("result=%v err=%v", result, err)
		}
	})
	if posts != 0 {
		t.Fatalf("invalid calls reached /api/dispatch: %d", posts)
	}
}

func TestMCPDryRunReturnsPlanWithoutPretendingStarted(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sessions/demo-parent":
			writeTestJSON(w, map[string]any{"session_id": "demo-parent", "source": "codex"})
		case "/api/dispatch":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["dry_run"] != true {
				t.Errorf("dry_run body=%v", body)
			}
			writeTestJSON(w, map[string]any{
				"ok": true, "dry_run": true,
				"plan": map[string]any{"dispatch_id": "dsp_preview", "preset": "ask"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	client, err := newAPI(api.URL, "demo-token")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{API: client, ParentSessionID: "demo-parent", Out: &bytes.Buffer{}}
	server.initialized = true
	result, err := server.callDispatch(context.Background(),
		json.RawMessage(`{"target_agent":"claude","task":"demo","dry_run":true}`))
	if err != nil {
		t.Fatal(err)
	}
	payload := result.(map[string]any)
	structured := payload["structuredContent"].(map[string]any)
	if structured["status"] != "planned" || structured["dry_run"] != true {
		t.Fatalf("structured=%v", structured)
	}
	if strings.Contains(payload["content"].([]any)[0].(map[string]any)["text"].(string), "已建立") {
		t.Fatal("dry run claimed a child was started")
	}
}

func TestMCPAPIErrorIsAToolErrorNotACrash(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/sessions/demo-parent":
			writeTestJSON(w, map[string]any{"session_id": "demo-parent", "source": "codex"})
		case "/api/dispatch":
			w.WriteHeader(http.StatusConflict)
			writeTestJSON(w, map[string]any{
				"ok": false, "error": "Agent kimi 不能接收协作任务",
				"code": "agent_not_dispatchable",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	client, err := newAPI(api.URL, "demo-token")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{API: client, ParentSessionID: "demo-parent", Out: &bytes.Buffer{}}
	server.initialized = true
	_, err = server.callDispatch(context.Background(),
		json.RawMessage(`{"target_agent":"kimi","task":"demo"}`))
	var typed *apiError
	if !errors.As(err, &typed) || typed.Status != http.StatusConflict ||
		typed.Code != "agent_not_dispatchable" {
		t.Fatalf("err=%#v", err)
	}
	result := toolError(err)
	if result["isError"] != true || !strings.Contains(fmt.Sprint(result), "agent_not_dispatchable") {
		t.Fatalf("tool error=%v", result)
	}
}

func TestMCPRequiresLoopbackAndPrivateConfig(t *testing.T) {
	for _, raw := range []string{
		"https://example.com",
		"http://192.168.1.20:18787",
		"http://user:pass@127.0.0.1:18787",
		"http://127.0.0.1:18787/api",
	} {
		if _, err := loopbackURL(raw); err == nil {
			t.Errorf("loopbackURL(%q) accepted", raw)
		}
	}
	if _, err := loopbackURL("http://[::1]:18787"); err != nil {
		t.Fatalf("IPv6 loopback rejected: %v", err)
	}

	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := platform.WriteJSON(config, map[string]any{"token": "demo-token"}); err != nil {
		t.Fatal(err)
	}
	if token, err := readOwnerToken(dir); err != nil || token != "demo-token" {
		t.Fatalf("token=%q err=%v", token, err)
	}
	if err := os.Chmod(config, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readOwnerToken(dir); err == nil {
		t.Fatal("world-readable config was accepted")
	}
}

func decodeResponses(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		out = append(out, response)
	}
	return out
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
