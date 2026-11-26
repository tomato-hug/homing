package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/store"
)

func TestAgentProcessParserAndWakeRoutes(t *testing.T) {
	// 1. Direct unit test of single-snapshot PS parser (desktop .app + CLI + runtime wrappers)
	parsed := parseRunningAgentsFromPS([]string{
		"/Applications/WorkBuddy.app/Contents/MacOS/Electron /Applications/WorkBuddy.app/Contents/MacOS/Electron --type=gpu-process",
		"/opt/homebrew/bin/codex codex --full-auto",
		"node node /usr/local/bin/gemini.js prompt",
		"/usr/local/bin/homing homing serve --addr 127.0.0.1:18787",
	})
	if !parsed["workbuddy"] || !parsed["codex"] || !parsed["gemini-cli"] {
		t.Fatalf("unexpected parsed running agents: %+v", parsed)
	}
	if parsed["homing"] {
		t.Fatalf("homing daemon itself must never be parsed as an agent: %+v", parsed)
	}

	// 2. Wake state evaluation
	restore := InstallWakeTestHooksForTesting(
		func(_ context.Context) ([]string, error) {
			return []string{
				"/Applications/WorkBuddy.app/Contents/MacOS/Electron",
			}, nil
		},
		func(agentID string) string {
			switch agentID {
			case "workbuddy":
				return "/Applications/WorkBuddy.app"
			case "cursor":
				return "/Applications/Cursor.app"
			default:
				return ""
			}
		},
	)
	defer restore()

	dataDir := t.TempDir()
	now := time.Now()
	procMap := liveAgentProcessMap(context.Background())
	procRunning, activeOnline, canWake, wakeMode := evaluateAgentWakeState(
		dataDir, "workbuddy", true, nil, procMap, 0, now,
	)
	if !procRunning || !activeOnline || !canWake || wakeMode != "app" {
		t.Fatalf("unexpected workbuddy state: proc=%v active=%v canWake=%v mode=%s", procRunning, activeOnline, canWake, wakeMode)
	}
	cProc, cActive, cCanWake, cMode := evaluateAgentWakeState(
		dataDir, "cursor", false, nil, procMap, 0, now,
	)
	if cProc || cActive || !cCanWake || cMode != "app" {
		t.Fatalf("unexpected idle cursor state: proc=%v active=%v canWake=%v mode=%s", cProc, cActive, cCanWake, cMode)
	}

	// 3. HTTP route scope & whitelist checks
	deps := &Deps{DataDir: dataDir, Version: "0.11.6-test"}
	st, err := deps.Store()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ownerToken := st.Token()

	readOnlyRaw := "hm_demo_readonly_device_token_123456"
	sum := sha256.Sum256([]byte(readOnlyRaw))
	if err := st.SaveDevices([]store.Device{{
		ID:          "demo-readonly-phone",
		Name:        "Demo ReadOnly Phone",
		TokenSHA256: hex.EncodeToString(sum[:]),
		Scopes:      []string{"read"},
	}}, nil); err != nil {
		t.Fatalf("save devices: %v", err)
	}

	mux := NewMux(deps)

	// Read-only device token cannot invoke POST /api/agents/cursor/wake (must require drive scope)
	forbiddenReq := httptest.NewRequest(http.MethodPost, "/api/agents/cursor/wake", nil)
	forbiddenReq.Header.Set("Authorization", "Bearer "+readOnlyRaw)
	forbiddenRec := httptest.NewRecorder()
	mux.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for read-only token on wake, got %d (%s)", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	// Owner/drive token wakes cursor (desktop app bundle stubbed) without arbitrary shell execution
	wakeReq := httptest.NewRequest(http.MethodPost, "/api/agents/cursor/wake", nil)
	wakeReq.Header.Set("Authorization", "Bearer "+ownerToken)
	wakeRec := httptest.NewRecorder()
	mux.ServeHTTP(wakeRec, wakeReq)
	if wakeRec.Code != http.StatusOK {
		t.Fatalf("POST /api/agents/cursor/wake status = %d, body = %s", wakeRec.Code, wakeRec.Body.String())
	}
	var wakeResp map[string]any
	if err := json.Unmarshal(wakeRec.Body.Bytes(), &wakeResp); err != nil {
		t.Fatalf("decode wake response: %v", err)
	}
	if wakeResp["ok"] != true || wakeResp["active_online"] != true || wakeResp["wake_mode"] != "app" {
		t.Fatalf("unexpected wake response: %+v", wakeResp)
	}

	// Invalid agent ID with shell metacharacters is rejected with 400
	badReq := httptest.NewRequest(http.MethodPost, "/api/agents/bad;id/wake", nil)
	badReq.Header.Set("Authorization", "Bearer "+ownerToken)
	badRec := httptest.NewRecorder()
	mux.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid agent id, got %d", badRec.Code)
	}
}
