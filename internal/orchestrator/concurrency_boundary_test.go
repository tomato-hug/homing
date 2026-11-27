package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/store"
)

type concurrentFakeLauncher struct {
	mu      sync.Mutex
	targets map[string]bool
	started atomic.Int32
	onStart func(ctx context.Context, seq int32, options StartOptions) StartResult
}

func (l *concurrentFakeLauncher) CanDispatch(target string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.targets[target]
}

func (l *concurrentFakeLauncher) Start(ctx context.Context, options StartOptions) StartResult {
	seq := l.started.Add(1)
	if l.onStart != nil {
		return l.onStart(ctx, seq, options)
	}
	return StartResult{
		SessionID: fmt.Sprintf("demo-child-%d", seq),
		Status:    "started",
	}
}

func TestDispatchConcurrentQuotaEnforcement(t *testing.T) {
	t.Run("shared cwd writable limit holds under concurrent Service instances", func(t *testing.T) {
		s := testStore(t)
		cwd := t.TempDir()
		launcher := &concurrentFakeLauncher{
			targets: map[string]bool{"claude": true},
		}

		const workers = 10
		startGate := make(chan struct{})
		var wg sync.WaitGroup
		var okCount atomic.Int32
		var limitCount atomic.Int32
		errs := make(chan error, workers)

		for i := 0; i < workers; i++ {
			wg.Add(1)
			idx := i
			go func() {
				defer wg.Done()
				// Mirror relayapi: each HTTP handler constructs its own Service value over the shared Store.
				svc := Service{
					Store:    s,
					Launcher: launcher,
					NewID:    func() string { return fmt.Sprintf("dsp_shared_race_%d", idx) },
				}
				<-startGate
				_, err := svc.Dispatch(context.Background(), Request{
					ParentSessionID: "demo-parent-shared",
					ParentSource:    "codex",
					TargetAgent:     "claude",
					Cwd:             cwd,
					Text:            fmt.Sprintf("并发写入任务 %d", idx),
					Preset:          "do",
					Workspace:       "shared",
				})
				if err == nil {
					okCount.Add(1)
					return
				}
				var typed *Error
				if errors.As(err, &typed) && typed.Status == 429 && typed.Code == "cwd_dispatch_limit" {
					limitCount.Add(1)
					return
				}
				errs <- err
			}()
		}

		close(startGate)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := okCount.Load(); got != 2 {
			t.Fatalf("succeeded shared writable dispatches = %d, want 2 (limitCount=%d, launched=%d)",
				got, limitCount.Load(), launcher.started.Load())
		}
		if got := limitCount.Load(); got != workers-2 {
			t.Fatalf("cwd_dispatch_limit count = %d, want %d", got, workers-2)
		}
		if got := launcher.started.Load(); got != 2 {
			t.Fatalf("launcher Start invocations = %d, want 2", got)
		}
	})

	t.Run("parent active child limit holds under concurrent worktree dispatches", func(t *testing.T) {
		s := testStore(t)
		cwd := t.TempDir()
		launcher := &concurrentFakeLauncher{
			targets: map[string]bool{"claude": true},
		}

		const workers = 12
		startGate := make(chan struct{})
		var wg sync.WaitGroup
		var okCount atomic.Int32
		var limitCount atomic.Int32
		errs := make(chan error, workers)

		for i := 0; i < workers; i++ {
			wg.Add(1)
			idx := i
			go func() {
				defer wg.Done()
				svc := Service{
					Store:    s,
					Launcher: launcher,
					NewID:    func() string { return fmt.Sprintf("dsp_parent_race_%d", idx) },
				}
				<-startGate
				_, err := svc.Dispatch(context.Background(), Request{
					ParentSessionID: "demo-parent-five",
					ParentSource:    "codex",
					TargetAgent:     "claude",
					Cwd:             cwd,
					Text:            fmt.Sprintf("并发子任务 %d", idx),
					Preset:          "do",
					Workspace:       "worktree",
				})
				if err == nil {
					okCount.Add(1)
					return
				}
				var typed *Error
				if errors.As(err, &typed) && typed.Status == 429 && typed.Code == "parent_dispatch_limit" {
					limitCount.Add(1)
					return
				}
				errs <- err
			}()
		}

		close(startGate)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := okCount.Load(); got != 5 {
			t.Fatalf("succeeded parent dispatches = %d, want 5 (limitCount=%d)", got, limitCount.Load())
		}
		if got := limitCount.Load(); got != workers-5 {
			t.Fatalf("parent_dispatch_limit count = %d, want %d", got, workers-5)
		}
	})
}

func TestDispatchPreservesMidFlightCancelAndHookProgress(t *testing.T) {
	t.Run("cancelled while Launcher.Start blocks stays cancelled", func(t *testing.T) {
		s := testStore(t)
		cwd := t.TempDir()
		enteredStart := make(chan struct{})
		releaseStart := make(chan struct{})

		launcher := &concurrentFakeLauncher{
			targets: map[string]bool{"claude": true},
			onStart: func(_ context.Context, _ int32, _ StartOptions) StartResult {
				close(enteredStart)
				<-releaseStart
				return StartResult{
					SessionID: "demo-late-child-session",
					Status:    "started",
				}
			},
		}
		svc := Service{
			Store:    s,
			Launcher: launcher,
			NewID:    func() string { return "dsp_midflight_cancel" },
		}

		done := make(chan struct {
			dispatch store.Dispatch
			err      error
		}, 1)
		go func() {
			d, err := svc.Dispatch(context.Background(), Request{
				ParentSessionID: "demo-parent",
				ParentSource:    "codex",
				TargetAgent:     "claude",
				Cwd:             cwd,
				Text:            "耗时启动任务",
			})
			done <- struct {
				dispatch store.Dispatch
				err      error
			}{d, err}
		}()

		<-enteredStart
		var current store.Dispatch
		for _, item := range s.Dispatches() {
			if item.ID == "dsp_midflight_cancel" {
				current = item
				break
			}
		}
		if current.Status != "starting" {
			t.Fatalf("in-flight status = %q, want starting", current.Status)
		}

		current.Status = "cancelled"
		current.Message = "已取消"
		if _, err := s.SaveDispatch(current); err != nil {
			t.Fatal(err)
		}
		close(releaseStart)

		outcome := <-done
		if outcome.dispatch.Status != "cancelled" {
			t.Fatalf("returned dispatch status = %q, want cancelled (%+v)", outcome.dispatch.Status, outcome.dispatch)
		}

		reopened, err := store.Open(platform.NewPaths(s.Paths.DataDir))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = reopened.Close() })
		persisted := reopened.Dispatches()
		if len(persisted) != 1 || persisted[0].Status != "cancelled" {
			t.Fatalf("persisted after reopen = %+v, want cancelled", persisted)
		}
	})

	t.Run("early hook completion before Start returns is not regressed", func(t *testing.T) {
		s := testStore(t)
		cwd := t.TempDir()

		launcher := &concurrentFakeLauncher{
			targets: map[string]bool{"claude": true},
			onStart: func(_ context.Context, _ int32, _ StartOptions) StartResult {
				// Simulate a fast CLI whose Stop hook already bound and completed the dispatch
				// before Start returns its RequestID.
				for _, item := range s.Dispatches() {
					if item.ID == "dsp_early_complete" {
						item.TargetSessionID = "demo-fast-hook-session"
						item.Status = "completed"
						item.Message = "协作任务已完成"
						item.Result = &store.DispatchResult{
							Text:       "快速审查结论",
							FinishedAt: "2026-11-26T22:00:00.000Z",
						}
						_, _ = s.SaveDispatch(item)
						break
					}
				}
				return StartResult{
					RequestID: "req_late_1",
					Status:    "started",
				}
			},
		}
		svc := Service{
			Store:    s,
			Launcher: launcher,
			NewID:    func() string { return "dsp_early_complete" },
		}

		got, err := svc.Dispatch(context.Background(), Request{
			ParentSessionID: "demo-parent",
			ParentSource:    "codex",
			TargetAgent:     "claude",
			Cwd:             cwd,
			Text:            "快速审查",
			Preset:          "review",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "completed" || got.TargetSessionID != "demo-fast-hook-session" || got.Result == nil {
			t.Fatalf("got = %+v, want completed with TargetSessionID and Result preserved", got)
		}
	})
}

func TestParseMentionsMultiBacktickAndFenceBoundaries(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantMessage  string
		wantMentions []Mention
	}{
		{
			name:         "double backtick code span does not dispatch mention",
			input:        "請參考 ``@claude:do 刪除舊檔`` 的寫法",
			wantMessage:  "請參考 ``@claude:do 刪除舊檔`` 的寫法",
			wantMentions: nil,
		},
		{
			name:         "double backtick wrapping inner single backtick does not dispatch",
			input:        "範例 `` `@codex:review` `` 僅供說明",
			wantMessage:  "範例 `` `@codex:review` `` 僅供說明",
			wantMentions: nil,
		},
		{
			name:        "unmatched single backtick does not swallow trailing mention",
			input:       "日誌出現單個 ` 符號異常 @claude:ask 幫我排查原因",
			wantMessage: "日誌出現單個 ` 符號異常",
			wantMentions: []Mention{
				{Agent: "claude", Preset: "ask", Text: "幫我排查原因"},
			},
		},
		{
			name:        "closed double backtick span followed by live mention on same line",
			input:       "先對照 ``@codex`` 範例 @claude:review 檢查這段邏輯",
			wantMessage: "先對照 ``@codex`` 範例",
			wantMentions: []Mention{
				{Agent: "claude", Preset: "review", Text: "檢查這段邏輯"},
			},
		},
		{
			name:        "single-line triple backtick code span at line start does not swallow next line",
			input:       "```@claude:do 範例``` 只是行內說明\n@codex:ask 檢查下一行是否生效",
			wantMessage: "```@claude:do 範例``` 只是行內說明",
			wantMentions: []Mention{
				{Agent: "codex", Preset: "ask", Text: "檢查下一行是否生效"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotMsg, gotMentions := ParseMentions(tc.input)
			if gotMsg != tc.wantMessage {
				t.Fatalf("message = %q, want %q", gotMsg, tc.wantMessage)
			}
			if len(gotMentions) != len(tc.wantMentions) {
				t.Fatalf("mentions = %+v, want %+v", gotMentions, tc.wantMentions)
			}
			for i := range tc.wantMentions {
				if gotMentions[i].Agent != tc.wantMentions[i].Agent ||
					gotMentions[i].Preset != tc.wantMentions[i].Preset ||
					gotMentions[i].Text != tc.wantMentions[i].Text {
					t.Fatalf("mention[%d] = %+v, want %+v", i, gotMentions[i], tc.wantMentions[i])
				}
			}
		})
	}
}
