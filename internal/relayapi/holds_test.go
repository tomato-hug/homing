package relayapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/store"
)

func TestHoldApprovalTimeoutReturnsCancelledDecision(t *testing.T) {
	previous := holdTimeout
	holdTimeout = 25 * time.Millisecond
	t.Cleanup(func() { holdTimeout = previous })

	started := time.Now()
	answer := holdApproval(context.Background(), store.Event{
		ID:        991001,
		Source:    "demo-agent",
		SessionID: "demo-session-timeout",
		Kind:      "permission",
	})
	if len(answer) != 0 {
		t.Fatalf("timeout answer = %#v, want empty (caller cancels the ACP request)", answer)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("holdApproval waited %s, want the configured short timeout", elapsed)
	}
}

func TestApprovalEndpointCarriesDecisionAndOption(t *testing.T) {
	relay := newTestRelay(t, nil)
	putHold := func(eventID int64) *approvalHold {
		hold := &approvalHold{ch: make(chan map[string]any, 1)}
		waits.mu.Lock()
		waits.approvals[eventID] = hold
		waits.mu.Unlock()
		t.Cleanup(func() {
			waits.mu.Lock()
			if waits.approvals[eventID] == hold {
				delete(waits.approvals, eventID)
			}
			waits.mu.Unlock()
		})
		return hold
	}

	allow := putHold(910001)
	response, body := relay.do(t, http.MethodPost, "/api/approvals/910001",
		[]byte(`{"decision":"allow","option_id":"allow_always"}`), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("allow status=%d body=%s", response.StatusCode, body)
	}
	var receipt map[string]any
	if err := json.Unmarshal(body, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["option_id"] != "allow_always" {
		t.Fatalf("approval receipt = %#v", receipt)
	}
	decision := <-allow.ch
	if decision["decision"] != "allow" || decision["option_id"] != "allow_always" {
		t.Fatalf("allow decision = %#v", decision)
	}

	deny := putHold(910002)
	response, body = relay.do(t, http.MethodPost, "/api/approvals/910002",
		[]byte(`{"decision":"deny"}`), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("deny status=%d body=%s", response.StatusCode, body)
	}
	decision = <-deny.ch
	if decision["decision"] != "deny" {
		t.Fatalf("deny decision = %#v", decision)
	}
	if _, present := decision["option_id"]; present {
		t.Fatalf("deny without option unexpectedly carried option_id: %#v", decision)
	}
}

func TestApprovalAfterTimeoutIsNotPending(t *testing.T) {
	relay := newTestRelay(t, nil)
	previous := holdTimeout
	holdTimeout = 20 * time.Millisecond
	t.Cleanup(func() { holdTimeout = previous })

	answer := holdApproval(context.Background(), store.Event{
		ID:        910003,
		Source:    "demo-agent",
		SessionID: "demo-approval-timeout",
		Kind:      "permission",
	})
	if len(answer) != 0 {
		t.Fatalf("timed out approval returned %#v", answer)
	}
	response, body := relay.do(t, http.MethodPost, "/api/approvals/910003",
		[]byte(`{"decision":"allow"}`), true)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("late approval status=%d body=%s, want 404", response.StatusCode, body)
	}
}

func TestNotificationPermissionPromptDeduplicatesAndAvoidsDeadHold(t *testing.T) {
	relay := newTestRelay(t, nil)
	cwd := t.TempDir()
	previous := holdTimeout
	holdTimeout = 150 * time.Millisecond
	t.Cleanup(func() { holdTimeout = previous })

	// 1. Standalone Notification(permission_prompt) without PermissionRequest must NOT block or register a hold.
	response, body := relay.do(t, http.MethodPost, "/hook/claude", []byte(`{
		"hook_event_name":"Notification",
		"notification_type":"permission_prompt",
		"session_id":"demo-notif-perm",
		"cwd":"`+cwd+`",
		"message":"Claude needs your permission to use AskUserQuestion"
	}`), false)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("notification status=%d body=%s", response.StatusCode, body)
	}
	event := waitForEvent(t, relay, "demo-notif-perm")
	if event.Kind != "needs_input" {
		t.Fatalf("standalone permission_prompt kind=%q, want needs_input", event.Kind)
	}

	// 2. When PermissionRequest is actively holding, duplicate Notification(permission_prompt) is ignored.
	hookDone := make(chan struct{})
	go func() {
		defer close(hookDone)
		postHookWithTimeout(t, relay, "/hook/claude", `{
			"hook_event_name":"PermissionRequest",
			"session_id":"demo-dedup-perm",
			"cwd":"`+cwd+`",
			"tool_name":"AskUserQuestion"
		}`)
	}()
	permEvent := waitForEvent(t, relay, "demo-dedup-perm")
	dupResp, dupBody := relay.do(t, http.MethodPost, "/hook/claude", []byte(`{
		"hook_event_name":"Notification",
		"notification_type":"permission_prompt",
		"session_id":"demo-dedup-perm",
		"cwd":"`+cwd+`",
		"message":"needs your permission"
	}`), false)
	if dupResp.StatusCode != http.StatusOK || !bytesContains(dupBody, "duplicate_permission_prompt") {
		t.Fatalf("duplicate notification status=%d body=%s", dupResp.StatusCode, dupBody)
	}
	resResp, _ := relay.do(t, http.MethodPost, "/api/approvals/"+strconv.FormatInt(permEvent.ID, 10),
		[]byte(`{"decision":"allow"}`), true)
	if resResp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status=%d", resResp.StatusCode)
	}
	<-hookDone

	// 3. Timed-out hold marks permission settled so session status returns to done.
	timeoutDone := make(chan struct{})
	go func() {
		defer close(timeoutDone)
		postHookWithTimeout(t, relay, "/hook/workbuddy", `{
			"hook_event_name":"PermissionRequest",
			"session_id":"demo-timeout-settled",
			"cwd":"`+cwd+`",
			"tool_name":"AskUserQuestion"
		}`)
	}()
	timeoutEvent := waitForEvent(t, relay, "demo-timeout-settled")
	<-timeoutDone
	if !IsApprovalSettled(timeoutEvent.ID) {
		t.Fatalf("expected timed-out approval %d to be marked settled", timeoutEvent.ID)
	}
	sessResp, sessBody := relay.do(t, http.MethodGet, "/api/sessions/demo-timeout-settled", nil, true)
	if sessResp.StatusCode != http.StatusOK {
		t.Fatalf("session get status=%d body=%s", sessResp.StatusCode, sessBody)
	}
	var summary map[string]any
	if err := json.Unmarshal(sessBody, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["status"] == "permission" {
		t.Fatalf("settled permission session stayed in status=permission: %s", sessBody)
	}
}

func bytesContains(b []byte, sub string) bool {
	return len(sub) == 0 || bytes.Contains(b, []byte(sub))
}
