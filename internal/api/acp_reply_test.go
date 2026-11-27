package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/driver"
)

// TestACPHelperProcess is a tiny ACP peer for the HTTP-level reply test. It
// deliberately lives in this test binary so the test exercises the real
// Manager/subprocess path without depending on an installed third-party agent.
func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_API_ACP_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	loaded := false
	for scanner.Scan() {
		var message map[string]any
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		id := message["id"]
		switch message["method"] {
		case "initialize":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{
					"protocolVersion": 1,
					"agentCapabilities": map[string]any{
						"loadSession": true,
					},
				},
			})
		case "session/new":
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{"sessionId": "demo-api-acp-session"},
			})
		case "session/load":
			loaded = true
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{},
			})
		case "session/prompt":
			text := "HOMING_ACP_NEW"
			if loaded {
				text = "HOMING_ACP_RESUMED"
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "method": "session/update",
				"params": map[string]any{
					"sessionId": "demo-api-acp-session",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": text},
					},
				},
			})
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{"stopReason": "end_turn"},
			})
		case "session/cancel":
			return
		}
	}
}

func TestReplyResumesACPThroughSessionLoad(t *testing.T) {
	const (
		agentID   = "acp-api-test"
		sessionID = "demo-api-acp-session"
		token     = "demo-token"
	)
	if err := driver.ReplaceDynamicSpecs([]driver.Spec{{
		ID: agentID, Label: "ACP API Test",
		Command:  []string{os.Args[0], "-test.run=TestACPHelperProcess", "--"},
		Env:      map[string]string{"GO_WANT_API_ACP_HELPER": "1"},
		Dispatch: true,
	}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		driver.Default().CloseAll()
		_ = driver.ReplaceDynamicSpecs(nil)
	})

	cwd := t.TempDir()
	dir := t.TempDir()
	events := `{"id":1,"ts":"2026-11-10T10:00:00.000Z","source":"` + agentID +
		`","kind":"reply","session_id":"` + sessionID + `","cwd":"` + cwd +
		`","title":"Demo ACP","text":"示例：上一回合","read":true}` + "\n"
	for name, content := range map[string]string{
		"events.jsonl":       events,
		"session-state.json": `{"version":1,"sessions":{}}`,
		"config.json":        `{"token":"` + token + `"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	deps := &Deps{
		DataDir: dir,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	t.Cleanup(func() { _ = deps.Close() })
	server := httptest.NewServer(NewMux(deps))
	t.Cleanup(server.Close)

	payload, _ := json.Marshal(map[string]any{
		"session_id": sessionID,
		"source":     agentID,
		"text":       "示例：ACP 續聊",
	})
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/reply", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reply status=%d body=%s", response.StatusCode, body)
	}
	var receipt map[string]any
	if err := json.Unmarshal(body, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["status"] != "injected" {
		t.Fatalf("reply status=%v body=%s", receipt["status"], body)
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		for _, item := range driver.Default().Items(sessionID, 100) {
			if strings.Contains(item.Text, "HOMING_ACP_RESUMED") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ACP transcript did not contain the session/load marker: %+v",
		driver.Default().Items(sessionID, 100))
}
