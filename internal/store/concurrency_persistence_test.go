package store

import (
	"fmt"
	"sync"
	"testing"

	"github.com/tomato-hug/homing/internal/platform"
)

func TestSetReplyStatusPersistsToJSONLAcrossReopen(t *testing.T) {
	paths := platform.NewPaths(t.TempDir())
	first, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}

	cancelledReply, err := first.AppendReply(Reply{
		SessionID: "demo-reply-cancel",
		Source:    "codex",
		Text:      "demo cancel",
		Status:    "queued",
	})
	if err != nil {
		t.Fatal(err)
	}
	failedReply, err := first.AppendReply(Reply{
		SessionID: "demo-reply-fail",
		Source:    "claude",
		Text:      "demo fail",
		Status:    "queued",
	})
	if err != nil {
		t.Fatal(err)
	}

	first.SetReplyStatus(cancelledReply.ID, "cancelled", "已从排队撤回")
	first.SetReplyStatus(failedReply.ID, "failed", "驱动连接失败")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// Normal restart keeps replies.jsonl on disk and replays syncJSONL into SQLite.
	reopened, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	got := reopened.Replies()
	if len(got) != 2 {
		t.Fatalf("reopened replies count = %d, want 2", len(got))
	}
	if got[0].Status != "cancelled" || got[0].Message != "已从排队撤回" || got[0].Injected {
		t.Fatalf("reopened cancelled reply = %+v, want status=cancelled", got[0])
	}
	if got[1].Status != "failed" || got[1].Message != "驱动连接失败" || got[1].Injected {
		t.Fatalf("reopened failed reply = %+v, want status=failed", got[1])
	}
}

func TestBindDispatchTargetRejectsTerminalDispatchesAndConcurrentCancel(t *testing.T) {
	for _, terminal := range []string{"cancelled", "completed", "failed"} {
		t.Run("terminal-"+terminal, func(t *testing.T) {
			paths := platform.NewPaths(t.TempDir())
			s, err := Open(paths)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			reqID := "demo-req-" + terminal
			d, err := s.SaveDispatch(Dispatch{
				ID:              "dsp_" + terminal,
				ParentSessionID: "demo-parent",
				ParentSource:    "codex",
				TargetAgent:     "claude",
				RequestID:       reqID,
				Text:            "demo review",
				Cwd:             paths.DataDir,
				Status:          "waiting_session",
			})
			if err != nil {
				t.Fatal(err)
			}

			// Simulate cancelDispatch / failure arriving before the child hook binds RequestID.
			d.Status = terminal
			d.Message = "终态已定"
			if _, err := s.SaveDispatch(d); err != nil {
				t.Fatal(err)
			}

			if bound, ok := s.BindDispatchTarget(reqID, "claude", "demo-late-child"); ok {
				t.Fatalf("BindDispatchTarget revived %s dispatch: %+v", terminal, bound)
			}

			dispatches := s.Dispatches()
			if len(dispatches) != 1 || dispatches[0].Status != terminal || dispatches[0].TargetSessionID != "" {
				t.Fatalf("persisted dispatch = %+v, want status=%s without late session bind", dispatches, terminal)
			}
		})
	}

	t.Run("concurrent-bind-and-cancel", func(t *testing.T) {
		paths := platform.NewPaths(t.TempDir())
		s, err := Open(paths)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()

		const total = 30
		for i := range total {
			if _, err := s.SaveDispatch(Dispatch{
				ID:              fmt.Sprintf("dsp_race_%d", i),
				ParentSessionID: "demo-parent",
				ParentSource:    "codex",
				TargetAgent:     "claude",
				RequestID:       fmt.Sprintf("req_race_%d", i),
				Text:            "demo race",
				Cwd:             paths.DataDir,
				Status:          "waiting_session",
			}); err != nil {
				t.Fatal(err)
			}
		}

		var wg sync.WaitGroup
		for i := range total {
			wg.Add(2)
			go func(idx int) {
				defer wg.Done()
				_, _ = s.BindDispatchTarget(
					fmt.Sprintf("req_race_%d", idx),
					"claude",
					fmt.Sprintf("demo-child-%d", idx),
				)
			}(i)
			go func(idx int) {
				defer wg.Done()
				_, _ = s.SaveDispatch(Dispatch{
					ID:              fmt.Sprintf("dsp_race_%d", idx),
					ParentSessionID: "demo-parent",
					ParentSource:    "codex",
					TargetAgent:     "claude",
					RequestID:       fmt.Sprintf("req_race_%d", idx),
					Text:            "demo race",
					Cwd:             paths.DataDir,
					Status:          "cancelled",
					Message:         "已取消",
				})
			}(i)
		}
		wg.Wait()

		for _, d := range s.Dispatches() {
			if d.Status != "cancelled" && d.Status != "started" {
				t.Fatalf("unexpected dispatch state after race: %+v", d)
			}
		}
	})
}

func TestConcurrentAppendEventSlidingWindowMarkReadAndReopen(t *testing.T) {
	paths := platform.NewPaths(t.TempDir())
	s, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 4
	const perWorker = 1280 // 5120 total > 5000 sliding window
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			sess := fmt.Sprintf("demo-sess-%d", workerID)
			for i := range perWorker {
				ev, appendErr := s.AppendEvent(Event{
					Source:    "codex",
					Kind:      "reply",
					SessionID: sess,
					Text:      fmt.Sprintf("msg-%d", i),
				})
				if appendErr != nil {
					t.Errorf("AppendEvent failed: %v", appendErr)
					return
				}
				if i%200 == 0 {
					s.MarkRead([]int64{ev.ID})
					_ = s.Events()
					_ = s.LastEventID()
				}
			}
		}(w)
	}
	wg.Wait()

	memEvents := s.Events()
	if len(memEvents) != 5000 {
		t.Fatalf("in-memory sliding window = %d, want 5000", len(memEvents))
	}
	lastID := s.LastEventID()
	if lastID != workers*perWorker {
		t.Fatalf("LastEventID = %d, want %d", lastID, workers*perWorker)
	}

	// Mark the very last event as read and ensure syncJSONL preserves read=true across restart.
	s.MarkRead([]int64{lastID})
	title := "自定义演示标题"
	if _, err := s.PatchSessionFields("codex", "demo-sess-0", map[string]bool{"pinned": true}, &title); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAllSeen(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	reloaded := reopened.Events()
	if len(reloaded) != 5000 {
		t.Fatalf("reloaded events len = %d, want 5000", len(reloaded))
	}
	tail := reloaded[len(reloaded)-1]
	if tail.ID != lastID || !tail.Read {
		t.Fatalf("tail event after reopen = %+v, want ID=%d Read=true", tail, lastID)
	}
	st := reopened.SessionState("codex", "demo-sess-0")
	if !st.Pinned || !st.Read || st.Title != title {
		t.Fatalf("session state after reopen = %+v", st)
	}
}
