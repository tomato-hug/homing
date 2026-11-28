package notify

import "testing"

func TestExplicitReminderPromotesOneCompletionEvent(t *testing.T) {
	var pushed []Payload
	reminders := map[string]bool{"codex:demo-session-1": true}
	n := New(Options{
		Sink: func(payload Payload) { pushed = append(pushed, payload) },
		Away: func() bool { return false },
		Reminder: func(source, sessionID string) bool {
			key := source + ":" + sessionID
			if !reminders[key] {
				return false
			}
			delete(reminders, key)
			return true
		},
	})

	n.OnEvent(Event{
		ID: 41, Source: "codex", SessionID: "demo-session-1",
		Kind: "done", Title: "Demo task", Text: "all tests passed",
	})
	if len(pushed) != 0 {
		t.Fatalf("expected notification to wait for P1 merge, got %d push(es)", len(pushed))
	}
	n.FlushAll()
	if len(pushed) != 1 || pushed[0].Level != "P1" {
		t.Fatalf("reminder payload = %+v", pushed)
	}
	if pushed[0].Title != "Demo task" || pushed[0].Kind != "done" {
		t.Fatalf("unexpected reminder payload: %+v", pushed[0])
	}

	n.OnEvent(Event{
		ID: 42, Source: "codex", SessionID: "demo-session-1",
		Kind: "done", Title: "Demo task", Text: "second turn",
	})
	n.FlushAll()
	if len(pushed) != 2 || pushed[1].Level != "P2" {
		t.Fatalf("reminder must be consumed once, pushed=%+v", pushed)
	}
}

func TestPausedOrMutedDoesNotConsumeReminder(t *testing.T) {
	consumed := 0
	n := New(Options{
		Sink:     func(Payload) {},
		Muted:    func(string, string) bool { return true },
		Reminder: func(string, string) bool { consumed++; return true },
	})
	n.OnEvent(Event{Source: "codex", SessionID: "demo-session-1", Kind: "done"})
	if consumed != 0 {
		t.Fatalf("muted event consumed reminder")
	}

	n = New(Options{
		Sink:     func(Payload) {},
		Paused:   func() bool { return true },
		Reminder: func(string, string) bool { consumed++; return true },
	})
	n.OnEvent(Event{Source: "codex", SessionID: "demo-session-1", Kind: "done"})
	if consumed != 0 {
		t.Fatalf("paused event consumed reminder")
	}
}

func TestReminderDoesNotChangeNonCompletionLevel(t *testing.T) {
	var pushed []Payload
	n := New(Options{
		Sink:     func(payload Payload) { pushed = append(pushed, payload) },
		Reminder: func(string, string) bool { return true },
	})
	n.OnEvent(Event{
		Source: "codex", SessionID: "demo-session-1", Kind: "permission",
		Title: "Permission", Text: "allow?",
	})
	if len(pushed) != 1 || pushed[0].Level != "P0" {
		t.Fatalf("permission payload = %+v", pushed)
	}
}

func TestReplyDoesNotConsumeCompletionReminder(t *testing.T) {
	consumed := 0
	n := New(Options{
		Sink: func(Payload) {},
		Reminder: func(string, string) bool {
			consumed++
			return true
		},
	})

	n.OnEvent(Event{
		Source: "codex", SessionID: "demo-session-1",
		Kind: "reply", Title: "Demo task", Text: "still running",
	})
	if consumed != 0 {
		t.Fatalf("reply event consumed completion reminder")
	}

	n.OnEvent(Event{
		Source: "codex", SessionID: "demo-session-1",
		Kind: "done", Title: "Demo task", Text: "finished",
	})
	if consumed != 1 {
		t.Fatalf("completion reminder consumed %d times, want 1", consumed)
	}
}

func TestInteractiveQuestionPushesImmediately(t *testing.T) {
	var pushed []Payload
	n := New(Options{
		Sink: func(payload Payload) { pushed = append(pushed, payload) },
	})
	n.OnEvent(Event{
		Source: "workbuddy", SessionID: "demo-wb-q1",
		Kind: "needs_input", Title: "待确认：样式", Text: "请选择配色方案",
	})
	if len(pushed) != 1 || pushed[0].Level != "P1" || pushed[0].Merged {
		t.Fatalf("expected immediate unmerged P1 push for needs_input, got %+v", pushed)
	}
}
