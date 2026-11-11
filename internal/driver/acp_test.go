package driver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

func TestACPClientHandshakePromptAndPermission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec := Spec{
		ID: "fake", Label: "Fake ACP",
		Command: []string{os.Args[0], "-test.run=TestACPHelperProcess", "--"},
		Env:     map[string]string{"GO_WANT_ACP_HELPER": "1"},
	}
	var kinds []string
	var allowCalled bool
	client, err := Start(ctx, spec, t.TempDir(), func(update Update) {
		kinds = append(kinds, update.Kind)
	}, func(request PermissionRequest) PermissionDecision {
		allowCalled = true
		if len(request.Options) == 0 {
			return PermissionDecision{Allow: false}
		}
		return PermissionDecision{Allow: true}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	initialized, err := client.Initialize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initialized["protocolVersion"] != float64(1) {
		t.Fatalf("protocolVersion = %#v", initialized["protocolVersion"])
	}
	if !client.SupportsLoadSession() {
		t.Fatal("loadSession capability was not recorded")
	}
	sessionID, _, err := client.NewSession(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "acp-session-1" {
		t.Fatalf("session id = %q", sessionID)
	}
	if _, err := client.LoadSession(ctx, sessionID, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	result, err := client.Prompt(ctx, sessionID, "do the harmless test")
	if err != nil {
		t.Fatal(err)
	}
	if result["stopReason"] != "end_turn" {
		t.Fatalf("stopReason = %#v", result["stopReason"])
	}
	if !allowCalled {
		t.Fatal("permission callback was not called")
	}
	deadline := time.Now().Add(time.Second)
	for len(kinds) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(kinds) < 2 || kinds[0] != "agent_message_chunk" || kinds[1] != "agent_thought_chunk" {
		t.Fatalf("updates = %#v", kinds)
	}
}

func TestPermissionDecisionWritesProtocolOutcome(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	client := &Client{
		stdin: writer,
		pendingPermissions: map[string]pendingPermission{
			"99": {sessionID: "demo-session", requestID: json.RawMessage("99")},
		},
	}
	client.onPermission = func(PermissionRequest) PermissionDecision {
		return PermissionDecision{Allow: true}
	}
	client.answerPermission(json.RawMessage("99"), PermissionRequest{
		SessionID: "demo-session",
		Options: []PermissionOption{
			{OptionID: "allow", Kind: "allow_once"},
			{OptionID: "reject", Kind: "reject_once"},
		},
	})
	var response map[string]any
	if err := json.NewDecoder(reader).Decode(&response); err != nil {
		t.Fatal(err)
	}
	outcome, _ := response["result"].(map[string]any)["outcome"].(map[string]any)
	if outcome["outcome"] != "selected" || outcome["optionId"] != "allow" {
		t.Fatalf("permission response = %#v", response)
	}
	if len(client.pendingPermissions) != 0 {
		t.Fatalf("pending permission was not cleared: %#v", client.pendingPermissions)
	}
}

func TestCancelPermissionsAnswersCancelledOnce(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	client := &Client{
		stdin: writer,
		pendingPermissions: map[string]pendingPermission{
			"77": {sessionID: "demo-session", requestID: json.RawMessage("77")},
		},
	}
	client.cancelPermissions("demo-session")
	var response map[string]any
	if err := json.NewDecoder(reader).Decode(&response); err != nil {
		t.Fatal(err)
	}
	outcome, _ := response["result"].(map[string]any)["outcome"].(map[string]any)
	if outcome["outcome"] != "cancelled" {
		t.Fatalf("cancel response = %#v", response)
	}
	if len(client.pendingPermissions) != 0 {
		t.Fatalf("cancelled permission remained pending: %#v", client.pendingPermissions)
	}
}

func TestParseUpdateSupportsNestedAndFlatShapes(t *testing.T) {
	nested := parseUpdate(map[string]any{
		"sessionId": "s1",
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "hello"},
		},
	})
	if nested.Kind != "agent_message_chunk" || nested.Text != "hello" {
		t.Fatalf("nested update = %#v", nested)
	}
	flat := parseUpdate(map[string]any{
		"sessionId": "s2", "type": "tool_call", "text": "run",
	})
	if flat.Kind != "tool_call" || flat.Text != "run" {
		t.Fatalf("flat update = %#v", flat)
	}
}

func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_ACP_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	var promptID any
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
				"result": map[string]any{"sessionId": "acp-session-1"},
			})
		case "session/load":
			loaded = true
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{},
			})
		case "session/prompt":
			promptID = id
			message := "HOMING_ACP_OK"
			if loaded {
				message = "HOMING_ACP_RESUMED"
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "method": "session/update",
				"params": map[string]any{
					"sessionId": "acp-session-1",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": message},
					},
				},
			})
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "method": "session/update",
				"params": map[string]any{
					"sessionId": "acp-session-1",
					"update": map[string]any{
						"sessionUpdate": "agent_thought_chunk",
						"content":       map[string]any{"type": "text", "text": "checking"},
					},
				},
			})
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": 99, "method": "session/request_permission",
				"params": map[string]any{
					"sessionId": "acp-session-1",
					"toolCall":  map[string]any{"title": "test"},
					"options": []any{
						map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
						map[string]any{"optionId": "reject", "name": "Reject", "kind": "reject_once"},
					},
				},
			})
		case "session/cancel":
			return
		}
		if id != nil && fmt.Sprint(id) == "99" {
			if promptID != nil {
				_ = encoder.Encode(map[string]any{
					"jsonrpc": "2.0", "id": promptID,
					"result": map[string]any{"stopReason": "end_turn"},
				})
			}
		}
	}
}

type managerObserver struct {
	mu          sync.Mutex
	started     []string
	items       []agent.Item
	turns       []string
	finished    []TurnResult
	permissions int
}

func (o *managerObserver) Started(sessionID, _, _ string) {
	o.mu.Lock()
	o.started = append(o.started, sessionID)
	o.mu.Unlock()
}

func (o *managerObserver) Item(_, _ string, item agent.Item) {
	o.mu.Lock()
	o.items = append(o.items, item)
	o.mu.Unlock()
}

func (o *managerObserver) Turn(_, _, state string) {
	o.mu.Lock()
	o.turns = append(o.turns, state)
	o.mu.Unlock()
}

func (o *managerObserver) Permission(_ context.Context, _, _ string, request PermissionRequest) PermissionDecision {
	o.mu.Lock()
	o.permissions++
	o.mu.Unlock()
	if len(request.Options) == 0 {
		return PermissionDecision{Cancel: true}
	}
	return PermissionDecision{Allow: true}
}

func (o *managerObserver) Finished(_ context.Context, _, _ string, result TurnResult) {
	o.mu.Lock()
	o.finished = append(o.finished, result)
	o.mu.Unlock()
}

func (o *managerObserver) snapshot() (started int, items int, turns []string, finished []TurnResult, permissions int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.started), len(o.items), append([]string(nil), o.turns...), append([]TurnResult(nil), o.finished...), o.permissions
}

func registerManagerTestSpec(t *testing.T) string {
	t.Helper()
	id := "acp-manager-test"
	if err := RegisterSpec(Spec{
		ID: id, Label: "ACP Manager Test",
		Command:  []string{os.Args[0], "-test.run=TestACPHelperProcess", "--"},
		Env:      map[string]string{"GO_WANT_ACP_HELPER": "1"},
		Dispatch: true, Verified: true,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ReplaceDynamicSpecs(nil) })
	return id
}

func TestManagerRunsMultipleTurnsAndStops(t *testing.T) {
	agentID := registerManagerTestSpec(t)
	manager := &Manager{
		sessions: map[string]*ManagedSession{},
		mappers:  map[string]*turnMapper{},
	}
	observer := &managerObserver{}
	manager.SetObserver(observer)

	sessionID, err := manager.Start(context.Background(), agentID, t.TempDir(), "first turn", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.CloseAll()

	waitFor := func(condition func() bool, label string) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", label)
	}

	waitFor(func() bool {
		_, _, _, finished, _ := observer.snapshot()
		return len(finished) == 1
	}, "first turn")

	if err := manager.Prompt(sessionID, "second turn"); err != nil {
		t.Fatal(err)
	}
	waitFor(func() bool {
		_, _, _, finished, _ := observer.snapshot()
		return len(finished) == 2
	}, "second turn")

	started, items, turns, finished, permissions := observer.snapshot()
	if started != 1 || permissions < 2 || len(finished) != 2 {
		t.Fatalf("observer counts: started=%d permissions=%d finished=%d", started, permissions, len(finished))
	}
	if items == 0 {
		t.Fatal("manager did not publish any items")
	}
	if len(turns) < 4 || turns[0] != "running" || turns[len(turns)-1] != "idle" {
		t.Fatalf("turn states = %#v", turns)
	}
	if got := manager.Items(sessionID, 100); len(got) == 0 {
		t.Fatal("manager transcript is empty")
	}
	if got := manager.Commands(sessionID); len(got) != 0 {
		t.Fatalf("unexpected commands: %#v", got)
	}
	if !manager.Stop(sessionID) {
		t.Fatal("Stop did not find the managed session")
	}
	if manager.Has(sessionID) || manager.Count() != 0 {
		t.Fatal("session remained managed after Stop")
	}
}

func TestManagerResumesWithSessionLoad(t *testing.T) {
	agentID := registerManagerTestSpec(t)
	manager := &Manager{
		sessions: map[string]*ManagedSession{},
		mappers:  map[string]*turnMapper{},
	}
	observer := &managerObserver{}
	manager.SetObserver(observer)
	cwd := t.TempDir()

	sessionID, err := manager.Start(context.Background(), agentID, cwd, "first turn", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForFinished := func(want int) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			_, _, _, finished, _ := observer.snapshot()
			if len(finished) >= want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %d finished turns", want)
	}
	waitForFinished(1)
	if !manager.Stop(sessionID) {
		t.Fatal("Stop did not close the first process")
	}
	if err := manager.Resume(context.Background(), agentID, sessionID, cwd, "resume turn", nil); err != nil {
		t.Fatal(err)
	}
	defer manager.CloseAll()
	waitForFinished(2)

	found := false
	for _, item := range manager.Items(sessionID, 100) {
		if strings.Contains(item.Text, "HOMING_ACP_RESUMED") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("resumed transcript did not contain the session/load marker: %+v", manager.Items(sessionID, 100))
	}
}

func TestManagerRejectsFifthProcess(t *testing.T) {
	agentID := registerManagerTestSpec(t)
	manager := &Manager{
		sessions: map[string]*ManagedSession{},
		mappers:  map[string]*turnMapper{},
	}
	for index := 0; index < MaxACPProcesses; index++ {
		id := "demo-limit-" + fmt.Sprint(index)
		manager.sessions[id] = &ManagedSession{SessionID: id}
	}
	_, err := manager.Start(context.Background(), agentID, t.TempDir(), "demo", nil)
	if !errors.Is(err, ErrACPProcessLimit) {
		t.Fatalf("start error = %v, want ErrACPProcessLimit", err)
	}
}
