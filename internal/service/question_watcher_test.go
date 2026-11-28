package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/notify"
	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/store"
)

type stubQuestionAgent struct {
	id    string
	name  string
	mu    sync.Mutex
	rows  []agent.Session
	items map[string][]agent.Item
}

func (a *stubQuestionAgent) ID() string                 { return a.id }
func (a *stubQuestionAgent) Name() string               { return a.name }
func (a *stubQuestionAgent) Icon() string               { return "" }
func (a *stubQuestionAgent) PushSignal() agent.PushSignal { return agent.PushPoll }
func (a *stubQuestionAgent) Detect(context.Context) agent.Detection {
	return agent.Detection{Installed: true}
}
func (a *stubQuestionAgent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Sessions: true}
}
func (a *stubQuestionAgent) ListSessions(context.Context, int) ([]agent.Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]agent.Session(nil), a.rows...), nil
}
func (a *stubQuestionAgent) Meta(string, string) agent.Meta { return agent.Meta{} }
func (a *stubQuestionAgent) ReadItems(_ context.Context, sessionID string, _ int) ([]agent.Item, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]agent.Item(nil), a.items[sessionID]...), nil
}

func resetQuestionDedupForTest() {
	questionMu.Lock()
	defer questionMu.Unlock()
	seenQuestionItems = map[string]time.Time{}
	seenQuestionSessions = map[string]questionClaim{}
}

func TestQuestionWatcherPushesNewPendingQuestionImmediatelyAndDeduplicates(t *testing.T) {
	resetQuestionDedupForTest()

	now := time.Now().UTC()
	oldTS := now.Add(-10 * time.Minute).Format(time.RFC3339)
	freshTS := now.Format(time.RFC3339)

	stub := &stubQuestionAgent{
		id:   "workbuddy-test-qw",
		name: "WorkBuddy Test",
		rows: []agent.Session{
			{SessionID: "old-s1", Source: "workbuddy-test-qw", Cwd: "/tmp/demo", LastTS: oldTS},
			{SessionID: "live-s2", Source: "workbuddy-test-qw", Cwd: "/tmp/demo", LastTS: freshTS},
		},
		items: map[string][]agent.Item{
			// Pre-existing question on startup should be seeded and never pushed.
			"old-s1": {
				{
					TS:        oldTS,
					ID:        "workbuddy-tool-old",
					Type:      "question",
					Questions: []agent.ItemQuestion{{Header: "旧问题", Question: "历史遗留？"}},
					Tool:      &agent.ItemTool{Name: "AskUserQuestion", Status: "running"},
				},
			},
			// Initially idle session with only user text.
			"live-s2": {
				{TS: freshTS, ID: "wb-i1", Type: "user", Role: "user", Text: "帮我规划"},
			},
		},
	}
	agent.RegisterProvider("question-watcher-test", func(context.Context) []agent.Agent {
		return []agent.Agent{stub}
	})

	dataDir := t.TempDir()
	st, err := store.Open(platform.NewPaths(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var pushed []notify.Payload
	svc := &Service{
		store: st,
		deps:  &api.Deps{DataDir: dataDir},
		notifier: notify.New(notify.Options{
			Sink: func(p notify.Payload) { pushed = append(pushed, p) },
		}),
	}

	// Seed pass: old-s1 is ignored/seeded without push.
	svc.scanPendingQuestions(context.Background(), false, now)
	if len(pushed) != 0 {
		t.Fatalf("seed pass must not push, got %+v", pushed)
	}

	// Agent now emits a new AskUserQuestion in live-s2.
	stub.mu.Lock()
	stub.items["live-s2"] = append(stub.items["live-s2"], agent.Item{
		TS:   freshTS,
		ID:   "workbuddy-tool-call-99",
		Type: "question",
		Role: "assistant",
		Questions: []agent.ItemQuestion{
			{
				Header:   "数据库",
				Question: "选择存储方案？",
				Options:  []agent.ItemQuestionOption{{Label: "SQLite"}, {Label: "PostgreSQL"}},
			},
		},
		Tool: &agent.ItemTool{Name: "AskUserQuestion", Status: "running"},
	})
	stub.mu.Unlock()

	svc.scanPendingQuestions(context.Background(), true, now)
	if len(pushed) != 1 {
		t.Fatalf("expected 1 immediate push for live question, got %d (%+v)", len(pushed), pushed)
	}
	if pushed[0].Kind != "needs_input" || pushed[0].Level != "P1" || pushed[0].SessionID != "live-s2" {
		t.Fatalf("unexpected push payload: %+v", pushed[0])
	}

	// Scanning again does not re-emit the same question.
	svc.scanPendingQuestions(context.Background(), true, now.Add(2*time.Second))
	if len(pushed) != 1 {
		t.Fatalf("expected deduplicated push count 1, got %d", len(pushed))
	}

	// Hook claiming before watcher sees a new item suppresses duplicate push.
	ClaimInteractiveQuestion("workbuddy-test-qw", "live-s2", "call-100", "已由钩子处理", true)
	stub.mu.Lock()
	stub.items["live-s2"] = append(stub.items["live-s2"], agent.Item{
		TS:        freshTS,
		ID:        "workbuddy-tool-call-100",
		Type:      "question",
		Questions: []agent.ItemQuestion{{Header: "确认", Question: "继续？"}},
		Tool:      &agent.ItemTool{Name: "AskUserQuestion", Status: "running"},
	})
	stub.mu.Unlock()
	svc.scanPendingQuestions(context.Background(), true, now.Add(4*time.Second))
	if len(pushed) != 1 {
		t.Fatalf("expected PreToolUse claim to suppress duplicate push, got %d", len(pushed))
	}
}
