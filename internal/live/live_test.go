package live

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

type capturedEvent struct {
	name      string
	sessionID string
	data      any
}

func TestManagerPublishesNewItemsAndTurnEvents(t *testing.T) {
	var calls atomic.Int32
	manager := NewManager()
	manager.poll = 2 * time.Millisecond
	manager.read = func(_ context.Context, _, _ string, _ int) ([]agent.Item, error) {
		call := calls.Add(1)
		items := []agent.Item{
			{ID: "item-1", Seq: 1, Type: "message", Text: "one"},
			{ID: "item-2", Seq: 2, Type: "message", Text: "two"},
		}
		if call >= 2 {
			items = append(items, agent.Item{
				ID: "item-3", Seq: 3, Type: "message", Text: "three",
			})
		}
		return items, nil
	}
	events := make(chan capturedEvent, 32)
	manager.SetEmitter(func(name, sessionID string, data any) {
		events <- capturedEvent{name: name, sessionID: sessionID, data: data}
	})

	manager.Track("demo-session-1", "demo-agent")
	defer manager.Untrack("demo-session-1")

	var gotItem bool
	var gotRunning bool
	var gotIdle bool
	deadline := time.After(2 * time.Second)
	for !(gotItem && gotRunning && gotIdle) {
		select {
		case event := <-events:
			if event.sessionID != "demo-session-1" {
				t.Fatalf("unexpected session id %q", event.sessionID)
			}
			switch event.name {
			case "item":
				payload, ok := event.data.(map[string]any)
				if !ok {
					t.Fatalf("item payload = %#v", event.data)
				}
				item, ok := payload["item"].(agent.Item)
				if !ok {
					t.Fatalf("nested item = %#v", payload["item"])
				}
				if item.ID == "item-3" {
					gotItem = true
				}
			case "turn":
				payload, _ := event.data.(map[string]any)
				switch payload["state"] {
				case "running":
					gotRunning = true
				case "idle":
					gotIdle = true
				}
			}
		case <-deadline:
			t.Fatalf("timed out: item=%v running=%v idle=%v", gotItem, gotRunning, gotIdle)
		}
	}
}

func TestTrackSharesOnePollerUntilLastSubscriberLeaves(t *testing.T) {
	manager := NewManager()
	manager.read = func(_ context.Context, _, _ string, _ int) ([]agent.Item, error) {
		return nil, nil
	}
	manager.SetEmitter(func(string, string, any) {})

	manager.Track("demo-session-1", "demo-agent")
	manager.Track("demo-session-1", "demo-agent")
	if got := manager.Tracked(); got != 1 {
		t.Fatalf("tracked = %d, want 1", got)
	}
	manager.Untrack("demo-session-1")
	if got := manager.Tracked(); got != 1 {
		t.Fatalf("tracked after one untrack = %d, want 1", got)
	}
	manager.Untrack("demo-session-1")
	if got := manager.Tracked(); got != 0 {
		t.Fatalf("tracked after last untrack = %d, want 0", got)
	}
}
