package relayapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/agent/claude"
	"github.com/tomato-hug/homing/internal/agent/codex"
	"github.com/tomato-hug/homing/internal/orchestrator"
	"github.com/tomato-hug/homing/internal/store"
)

type dispatchFakeLauncher struct {
	targets map[string]bool
	result  orchestrator.StartResult
	starts  []orchestrator.StartOptions
}

func (f dispatchFakeLauncher) CanDispatch(target string) bool {
	return f.targets[target]
}

func (f dispatchFakeLauncher) Start(_ context.Context, options orchestrator.StartOptions) orchestrator.StartResult {
	f.starts = append(f.starts, options)
	return f.result
}

func useDispatchLauncher(t *testing.T, launcher orchestrator.Launcher) {
	t.Helper()
	previous := dispatchLauncher
	dispatchLauncher = launcher
	t.Cleanup(func() { dispatchLauncher = previous })
}

func TestDispatchAPIStartsChildAndListsParentRelationship(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  orchestrator.StartResult{SessionID: "demo-claude-child", Status: "started"},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	parent, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-codex-parent",
		Cwd: cwd, Text: "示例：父会话", Title: "示例：父会话",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"parent_event_id": %d,
		"parent_session_id": "demo-codex-parent",
		"parent_source": "codex",
		"target_agent": "claude",
		"text": "检查登录流程",
		"cwd": %q
	}`, parent.ID, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/dispatch", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result["target_session_id"] != "demo-claude-child" || result["status"] != "started" {
		t.Fatalf("result=%v", result)
	}

	response, body = relay.do(t, http.MethodGet,
		"/api/dispatches?parent_session_id=demo-codex-parent", nil, true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.StatusCode, body)
	}
	var listed struct {
		Dispatches []store.Dispatch `json:"dispatches"`
		Count      int              `json:"count"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Count != 1 || len(listed.Dispatches) != 1 {
		t.Fatalf("listed=%+v", listed)
	}
	got := listed.Dispatches[0]
	if got.ParentSessionID != "demo-codex-parent" ||
		got.ParentEventID == nil || *got.ParentEventID != parent.ID ||
		got.TargetAgent != "claude" || got.TargetSessionID != "demo-claude-child" {
		t.Fatalf("dispatch=%+v", got)
	}
}

func TestDispatchAPIDryRunAndPresetHaveNoSideEffects(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  orchestrator.StartResult{SessionID: "demo-claude-child", Status: "started"},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	parent, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-codex-parent", Cwd: cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"parent_event_id": %d,
		"target_agent": "claude",
		"text": "检查登录流程",
		"preset": "review",
		"workspace": "readonly",
		"dry_run": true
	}`, parent.ID)
	response, body := relay.do(t, http.MethodPost, "/api/dispatch", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	var result struct {
		OK   bool           `json:"ok"`
		Dry  bool           `json:"dry_run"`
		Plan store.Dispatch `json:"plan"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.Dry || result.Plan.Preset != "review" ||
		result.Plan.Workspace != "readonly" {
		t.Fatalf("result=%+v", result)
	}
	if len(relay.store.Dispatches()) != 0 {
		t.Fatalf("dry run created dispatches: %+v", relay.store.Dispatches())
	}
}

func TestDispatchAPIHandoffDoesNotInheritGlobalInject(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  orchestrator.StartResult{SessionID: "demo-claude-child", Status: "started"},
	})
	relay := newTestRelay(t, nil)
	if err := relay.store.SetCollabReturn("inject"); err != nil {
		t.Fatal(err)
	}
	cwd := demoCwd(t)
	parent, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-codex-parent", Cwd: cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"parent_event_id": %d,
		"target_agent": "claude",
		"text": "接手登录修复",
		"preset": "handoff",
		"cwd": %q
	}`, parent.ID, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/dispatch", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	records := relay.store.Dispatches()
	if len(records) != 1 {
		t.Fatalf("dispatches=%+v", records)
	}
	if records[0].Return != "none" {
		t.Fatalf("handoff return=%q, want none", records[0].Return)
	}
	if records[0].Message == "" {
		t.Fatal("handoff should explain that ownership moved")
	}
}

func TestDispatchReadOnlyPresetsMapToSupportedAgents(t *testing.T) {
	for _, target := range []struct {
		name string
		mode string
	}{
		{name: "codex", mode: dispatchPermissionMode(codex.Agent{}, "ask", "readonly")},
		{name: "claude", mode: dispatchPermissionMode(claude.Agent{}, "review", "readonly")},
	} {
		if target.mode != "read_only" {
			t.Errorf("%s read-only mode = %q", target.name, target.mode)
		}
	}
	if mode := dispatchPermissionMode(codex.Agent{}, "do", "auto"); mode != "" {
		t.Fatalf("do mode = %q, want default", mode)
	}
}

func TestDispatchAPIRejectsUnsupportedAgentAndUnauthenticatedCalls(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	parent, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-codex-parent", Cwd: cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"parent_event_id":%d,"target_agent":"kimi","text":"demo","cwd":%q}`,
		parent.ID, cwd)
	response, raw := relay.do(t, http.MethodPost, "/api/dispatch", []byte(body), true)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.StatusCode, raw)
	}
	response, _ = relay.do(t, http.MethodPost, "/api/dispatch", []byte(body), false)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d", response.StatusCode)
	}
	otherCwd := demoCwd(t)
	badCwdBody := fmt.Sprintf(
		`{"parent_event_id":%d,"target_agent":"claude","text":"demo","cwd":%q}`,
		parent.ID, otherCwd,
	)
	response, raw = relay.do(t, http.MethodPost, "/api/dispatch", []byte(badCwdBody), true)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("cwd mismatch status=%d body=%s", response.StatusCode, raw)
	}
	if count := len(relay.store.Dispatches()); count != 0 {
		t.Fatalf("dispatches=%d, want 0", count)
	}
}

func TestClaimLaunchBindsLateClaudeSessionToDispatch(t *testing.T) {
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	dispatch, err := relay.store.SaveDispatch(store.Dispatch{
		ID:              "dsp_demo_late",
		ParentSessionID: "demo-codex-parent",
		ParentSource:    "codex",
		TargetAgent:     "claude",
		RequestID:       "demo-request-late",
		Text:            "检查登录流程",
		Cwd:             cwd,
		Status:          "waiting_session",
	})
	if err != nil {
		t.Fatal(err)
	}
	launches.mu.Lock()
	launches.pending["demo-request-late"] = launchInfo{
		source: "claude", cwd: cwd, at: time.Now(),
	}
	launches.mu.Unlock()
	t.Cleanup(func() {
		launches.mu.Lock()
		delete(launches.pending, "demo-request-late")
		launches.mu.Unlock()
	})

	claimLaunch(relay.deps, store.Event{
		Source: "claude", Kind: "reply", SessionID: "demo-claude-late", Cwd: cwd,
	})
	records := relay.store.Dispatches()
	if len(records) != 1 || records[0].ID != dispatch.ID {
		t.Fatalf("records=%+v", records)
	}
	if records[0].TargetSessionID != "demo-claude-late" || records[0].Status != "started" {
		t.Fatalf("bound=%+v", records[0])
	}
}

func TestClaimLaunchUsesOldestPendingForSameAgentAndCwd(t *testing.T) {
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	for _, record := range []store.Dispatch{
		{
			ID: "dsp_demo_first", ParentSessionID: "demo-parent", ParentSource: "codex",
			TargetAgent: "claude", RequestID: "demo-request-first",
			Text: "第一个任务", Cwd: cwd, Status: "waiting_session",
		},
		{
			ID: "dsp_demo_second", ParentSessionID: "demo-parent", ParentSource: "codex",
			TargetAgent: "claude", RequestID: "demo-request-second",
			Text: "第二个任务", Cwd: cwd, Status: "waiting_session",
		},
	} {
		if _, err := relay.store.SaveDispatch(record); err != nil {
			t.Fatal(err)
		}
	}
	launches.mu.Lock()
	launches.pending["demo-request-second"] = launchInfo{
		source: "claude", cwd: cwd, at: time.Now(),
	}
	launches.pending["demo-request-first"] = launchInfo{
		source: "claude", cwd: cwd, at: time.Now().Add(-time.Minute),
	}
	launches.mu.Unlock()
	t.Cleanup(func() {
		launches.mu.Lock()
		delete(launches.pending, "demo-request-first")
		delete(launches.pending, "demo-request-second")
		launches.mu.Unlock()
	})

	claimLaunch(relay.deps, store.Event{
		Source: "claude", Kind: "reply", SessionID: "demo-claude-first", Cwd: cwd,
	})
	first := dispatchByID(relay.store.Dispatches(), "dsp_demo_first")
	second := dispatchByID(relay.store.Dispatches(), "dsp_demo_second")
	if first.TargetSessionID != "demo-claude-first" || second.TargetSessionID != "" {
		t.Fatalf("after first claim: first=%+v second=%+v", first, second)
	}

	claimLaunch(relay.deps, store.Event{
		Source: "claude", Kind: "reply", SessionID: "demo-claude-second", Cwd: cwd,
	})
	second = dispatchByID(relay.store.Dispatches(), "dsp_demo_second")
	if second.TargetSessionID != "demo-claude-second" {
		t.Fatalf("after second claim: %+v", second)
	}
}

func dispatchByID(records []store.Dispatch, id string) store.Dispatch {
	for _, record := range records {
		if record.ID == id {
			return record
		}
	}
	return store.Dispatch{}
}
