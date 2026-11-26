package live

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
	manager.Untrack("demo-session-missing")
	manager.Track("", "demo-agent")
	manager.Track("demo-session-1", "")
	if got := manager.Tracked(); got != 0 {
		t.Fatalf("tracked after invalid calls = %d, want 0", got)
	}
}

func TestManagerConcurrentTrackEvictAndPollRace(t *testing.T) {
	var seq atomic.Int64
	manager := NewManager()
	manager.maxTracked = 2
	manager.poll = 100 * time.Microsecond
	manager.read = func(ctx context.Context, _, sessionID string, _ int) ([]agent.Item, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		n := int(seq.Add(1))
		return []agent.Item{{ID: fmt.Sprintf("%s-%d", sessionID, n), Seq: n, Type: "message", Text: "demo"}}, nil
	}
	manager.SetEmitter(func(string, string, any) {})

	// Pin demo-session-0 past its baseline tick so pollOnce actively updates lastActive.
	manager.Track("demo-session-0", "demo-agent")
	time.Sleep(2 * time.Millisecond)

	var wg sync.WaitGroup
	for worker := range 6 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for step := range 80 {
				sess := fmt.Sprintf("demo-session-%d", (id+step)%4)
				manager.Track(sess, "demo-agent")
				time.Sleep(150 * time.Microsecond)
				manager.Untrack(sess)
			}
		}(worker)
	}
	wg.Wait()

	manager.Untrack("demo-session-0")
	for i := range 4 {
		sess := fmt.Sprintf("demo-session-%d", i)
		for range 10 {
			manager.Untrack(sess)
		}
	}
	if got := manager.Tracked(); got != 0 {
		t.Fatalf("tracked after cleanup = %d, want 0", got)
	}
}

func TestManagerMaxTrackedEvictsLRUAndTransitionsIdleWithSynctest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var s1Seq atomic.Int32
		s1Seq.Store(1)

		var emittedMu sync.Mutex
		var emitted []capturedEvent

		manager := NewManager()
		manager.maxTracked = 2
		manager.read = func(_ context.Context, _, sessionID string, _ int) ([]agent.Item, error) {
			switch sessionID {
			case "demo-session-1":
				n := int(s1Seq.Load())
				out := make([]agent.Item, 0, n)
				for i := 1; i <= n; i++ {
					out = append(out, agent.Item{ID: fmt.Sprintf("s1-%d", i), Seq: i, Type: "message"})
				}
				return out, nil
			case "demo-session-2":
				return []agent.Item{{ID: "s2-1", Seq: 1, Type: "message"}}, nil
			default:
				return []agent.Item{{ID: "s3-1", Seq: 1, Type: "message"}}, nil
			}
		}
		manager.SetEmitter(func(name, sessionID string, data any) {
			emittedMu.Lock()
			emitted = append(emitted, capturedEvent{name: name, sessionID: sessionID, data: data})
			emittedMu.Unlock()
		})

		manager.Track("demo-session-1", "demo-agent")
		time.Sleep(10 * time.Millisecond)
		manager.Track("demo-session-2", "demo-agent")
		if got := manager.Tracked(); got != 2 {
			t.Fatalf("tracked = %d, want 2", got)
		}

		// Tick 1 (t=1s): establish baseline on both sessions.
		time.Sleep(time.Second)
		synctest.Wait()

		// Advance session-1 at t=2s so session-1 refreshes lastActive and session-2 becomes LRU.
		s1Seq.Store(2)
		time.Sleep(time.Second)
		synctest.Wait()

		// Tracking session-3 must evict LRU session-2 while keeping recently active session-1.
		manager.Track("demo-session-3", "demo-agent")
		if got := manager.Tracked(); got != 2 {
			t.Fatalf("tracked after eviction = %d, want 2", got)
		}

		emittedMu.Lock()
		emitted = nil
		emittedMu.Unlock()

		// Two idle ticks on session-1 must deterministically transition running -> idle.
		time.Sleep(2 * time.Second)
		synctest.Wait()

		var sawS1Idle bool
		emittedMu.Lock()
		for _, ev := range emitted {
			if ev.sessionID == "demo-session-1" && ev.name == "turn" {
				if payload, ok := ev.data.(map[string]any); ok && payload["state"] == "idle" {
					sawS1Idle = true
				}
			}
		}
		emittedMu.Unlock()
		if !sawS1Idle {
			t.Fatalf("expected demo-session-1 (kept alive over LRU demo-session-2) to emit idle turn")
		}

		manager.Untrack("demo-session-1")
		manager.Untrack("demo-session-3")
		synctest.Wait()
	})
}

func TestManagerGuardBranchesAndUnsupportedRegistry(t *testing.T) {
	if _, err := readThroughRegistry(context.Background(), "demo-unknown-agent", "demo-session-1", 10); err != agent.ErrUnsupported {
		t.Fatalf("readThroughRegistry err = %v, want %v", err, agent.ErrUnsupported)
	}

	synctest.Test(t, func(t *testing.T) {
		manager := NewManager()
		manager.poll = 0 // exercises defaultPollInterval fallback
		manager.read = nil
		manager.Track("demo-nil-read", "demo-agent")
		time.Sleep(time.Second)
		synctest.Wait()

		var step atomic.Int32
		manager.mu.Lock()
		manager.read = func(_ context.Context, _, _ string, _ int) ([]agent.Item, error) {
			switch step.Add(1) {
			case 1:
				return nil, agent.ErrUnsupported
			case 2:
				return []agent.Item{{ID: "x-1", Seq: 1}}, nil
			default:
				return []agent.Item{{ID: "x-1", Seq: 1}, {ID: "x-2", Seq: 2}}, nil
			}
		}
		manager.mu.Unlock()

		// Tick 1: read returns error; Tick 2: establishes baseline; Tick 3: fresh item with nil emitter.
		for range 3 {
			time.Sleep(time.Second)
			synctest.Wait()
		}

		manager.Untrack("demo-nil-read")
		synctest.Wait()
	})
}
