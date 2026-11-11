package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/store"
)

type cancelDemoAgent struct {
	id         string
	mu         sync.Mutex
	cancelled  bool
	sessionID  string
	replyID    int64
	cancelCall int
}

func (a *cancelDemoAgent) ID() string                   { return a.id }
func (a *cancelDemoAgent) Name() string                 { return "Cancel Demo" }
func (a *cancelDemoAgent) Icon() string                 { return "" }
func (a *cancelDemoAgent) PushSignal() agent.PushSignal { return agent.PushHook }
func (a *cancelDemoAgent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Reply: true, Sessions: true}
}
func (a *cancelDemoAgent) Detect(context.Context) agent.Detection {
	return agent.Detection{Installed: true}
}
func (a *cancelDemoAgent) ListSessions(context.Context, int) ([]agent.Session, error) {
	return nil, nil
}
func (a *cancelDemoAgent) Meta(string, string) agent.Meta { return agent.Meta{} }

func (a *cancelDemoAgent) CancelQueuedReply(_ context.Context, sessionID string, replyID int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancelCall++
	if sessionID != a.sessionID || replyID != a.replyID {
		return agent.ErrNotQueued
	}
	a.cancelled = true
	return nil
}

type cancelUnsupportedDemoAgent struct {
	id string
}

func (a *cancelUnsupportedDemoAgent) ID() string                   { return a.id }
func (a *cancelUnsupportedDemoAgent) Name() string                 { return "Cancel Unsupported Demo" }
func (a *cancelUnsupportedDemoAgent) Icon() string                 { return "" }
func (a *cancelUnsupportedDemoAgent) PushSignal() agent.PushSignal { return agent.PushHook }
func (a *cancelUnsupportedDemoAgent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Reply: true, Sessions: true}
}
func (a *cancelUnsupportedDemoAgent) Detect(context.Context) agent.Detection {
	return agent.Detection{Installed: true}
}
func (a *cancelUnsupportedDemoAgent) ListSessions(context.Context, int) ([]agent.Session, error) {
	return nil, nil
}
func (a *cancelUnsupportedDemoAgent) Meta(string, string) agent.Meta { return agent.Meta{} }

var (
	cancelDemoOnce        sync.Once
	cancelDemoSupported   = &cancelDemoAgent{id: "cancel-demo"}
	cancelDemoUnsupported = &cancelUnsupportedDemoAgent{id: "cancel-demo-unsupported"}
)

func installCancelDemoAgents(t *testing.T) {
	t.Helper()
	cancelDemoOnce.Do(func() {
		agent.RegisterProvider("cancel-demo-test", func(context.Context) []agent.Agent {
			return []agent.Agent{cancelDemoSupported, cancelDemoUnsupported}
		})
	})
	cancelDemoSupported.mu.Lock()
	cancelDemoSupported.cancelled = false
	cancelDemoSupported.sessionID = ""
	cancelDemoSupported.replyID = 0
	cancelDemoSupported.cancelCall = 0
	cancelDemoSupported.mu.Unlock()
}

func appendDemoReply(t *testing.T, d *Deps, source, sessionID, status string) store.Reply {
	t.Helper()
	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	reply, err := s.AppendReply(store.Reply{
		SessionID: sessionID,
		Source:    source,
		Text:      "demo queued reply",
		Status:    status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return reply
}

func cancelReplyRequest(id string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/replies/"+id+"/cancel", nil)
	req.SetPathValue("id", id)
	return WithTrustedLocalRPC(req)
}

func TestCancelQueuedReplyMarksQueuedReplyCancelled(t *testing.T) {
	installCancelDemoAgents(t)
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	reply := appendDemoReply(t, d, cancelDemoSupported.id, "demo-cancel-session", "queued")

	cancelDemoSupported.mu.Lock()
	cancelDemoSupported.sessionID = reply.SessionID
	cancelDemoSupported.replyID = reply.ID
	cancelDemoSupported.mu.Unlock()

	rec := httptest.NewRecorder()
	d.handleCancelReply(rec, cancelReplyRequest(strconv.FormatInt(reply.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "cancelled" {
		t.Fatalf("response status = %#v", body["status"])
	}

	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	replies := s.Replies()
	if len(replies) != 1 || replies[0].Status != "cancelled" {
		t.Fatalf("stored replies = %#v", replies)
	}

	cancelDemoSupported.mu.Lock()
	cancelled := cancelDemoSupported.cancelled
	cancelCall := cancelDemoSupported.cancelCall
	cancelDemoSupported.mu.Unlock()
	if !cancelled || cancelCall != 1 {
		t.Fatalf("cancelled = %v, calls = %d", cancelled, cancelCall)
	}
}

func TestCancelReplyRejectsReplyThatIsNoLongerQueued(t *testing.T) {
	installCancelDemoAgents(t)
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	reply := appendDemoReply(t, d, cancelDemoSupported.id, "demo-running-session", "running")

	rec := httptest.NewRecorder()
	d.handleCancelReply(rec, cancelReplyRequest(strconv.FormatInt(reply.ID, 10)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "reply_not_queued" || body["status"] != "running" {
		t.Fatalf("body = %#v", body)
	}

	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	replies := s.Replies()
	if len(replies) != 1 || replies[0].ID != reply.ID || replies[0].Status != "running" {
		t.Fatalf("stored replies = %#v", replies)
	}
}

func TestCancelReplyRejectsAgentWithoutQueueCancellation(t *testing.T) {
	installCancelDemoAgents(t)
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	reply := appendDemoReply(t, d, cancelDemoUnsupported.id, "demo-unsupported-session", "queued")

	rec := httptest.NewRecorder()
	d.handleCancelReply(rec, cancelReplyRequest(strconv.FormatInt(reply.ID, 10)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "reply_cancel_unsupported" {
		t.Fatalf("body = %#v", body)
	}

	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	replies := s.Replies()
	if len(replies) != 1 || replies[0].ID != reply.ID || replies[0].Status != "queued" {
		t.Fatalf("stored replies = %#v", replies)
	}
}

func TestCancelReplyReportsQueueRaceAsNotQueued(t *testing.T) {
	installCancelDemoAgents(t)
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	reply := appendDemoReply(t, d, cancelDemoSupported.id, "demo-race-session", "queued")

	cancelDemoSupported.mu.Lock()
	cancelDemoSupported.sessionID = "different-session"
	cancelDemoSupported.replyID = reply.ID
	cancelDemoSupported.mu.Unlock()

	rec := httptest.NewRecorder()
	d.handleCancelReply(rec, cancelReplyRequest(strconv.FormatInt(reply.ID, 10)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "reply_not_queued" {
		t.Fatalf("body = %#v", body)
	}

	s, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	replies := s.Replies()
	if len(replies) != 1 || replies[0].Status != "queued" {
		t.Fatalf("stored replies = %#v", replies)
	}
}

func TestCancelReplyAgentLookupUsesRegisteredProvider(t *testing.T) {
	installCancelDemoAgents(t)
	if _, ok := agent.Get(context.Background(), cancelDemoSupported.id); !ok {
		t.Fatal("registered cancellation provider was not found")
	}
	if _, ok := agent.Get(context.Background(), "cancel-demo-missing"); ok {
		t.Fatal("unexpected agent")
	}
}
