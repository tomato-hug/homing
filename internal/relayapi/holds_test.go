package relayapi

import (
	"context"
	"encoding/json"
	"net/http"
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
