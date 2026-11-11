package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomato-hug/homing/internal/platform"
)

func TestHistoryMigratesJSONLAndSurvivesWithoutIt(t *testing.T) {
	paths := platform.NewPaths(t.TempDir())
	writeHistoryFixture(t, paths)

	first, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events()) != 2 {
		t.Fatalf("events = %d, want 2", len(first.Events()))
	}
	replies := first.Replies()
	if len(replies) != 1 || replies[0].Status != "injected" {
		t.Fatalf("replies = %#v", replies)
	}
	dispatches := first.Dispatches()
	if len(dispatches) != 1 || dispatches[0].Status != "completed" {
		t.Fatalf("dispatches = %#v", dispatches)
	}
	if away := first.AwayPeriods(); len(away) != 1 || away[0].EndedAt == "" {
		t.Fatalf("away periods = %#v", away)
	}
	first.MarkRead([]int64{1})
	for _, sidecar := range []string{paths.History + "-wal", paths.History + "-shm"} {
		if info, err := os.Stat(sidecar); err == nil {
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("%s mode = %04o, want 0600", filepath.Base(sidecar), got)
			}
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(paths.History)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("history.db mode = %04o, want 0600", info.Mode().Perm())
	}

	// Removing the compatibility files simulates a database-only restart. The
	// same data must be served from history.db, including local read state.
	for _, path := range []string{
		paths.Events, paths.Replies, paths.Dispatches, paths.AwayPeriods,
	} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	second, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	events := second.Events()
	if len(events) != 2 || !events[0].Read {
		t.Fatalf("reopened events = %#v", events)
	}
	if reply := second.Replies(); len(reply) != 1 || reply[0].Status != "injected" {
		t.Fatalf("reopened replies = %#v", reply)
	}
	if dispatch := second.Dispatches(); len(dispatch) != 1 ||
		dispatch[0].Status != "completed" {
		t.Fatalf("reopened dispatches = %#v", dispatch)
	}
	if away := second.AwayPeriods(); len(away) != 1 || away[0].EndedAt == "" {
		t.Fatalf("reopened away periods = %#v", away)
	}
}

func TestHistoryPersistsNewWrites(t *testing.T) {
	paths := platform.NewPaths(t.TempDir())
	first, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	event, err := first.AppendEvent(Event{
		Source: "codex", Kind: "done", SessionID: "demo-history", Text: "done",
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := first.AppendReply(Reply{
		SessionID: "demo-history", Source: "codex", Text: "reply",
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := first.SaveDispatch(Dispatch{
		ID: "dsp_demo", ParentSessionID: "demo-history",
		TargetAgent: "claude", Text: "review", Status: "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	period, err := first.StartAway("manual", "demo-device")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		paths.Events, paths.Replies, paths.Dispatches, paths.AwayPeriods,
	} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	second, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got := second.Events(); len(got) != 1 || got[0].ID != event.ID {
		t.Fatalf("events = %#v", got)
	}
	if got := second.Replies(); len(got) != 1 || got[0].ID != reply.ID {
		t.Fatalf("replies = %#v", got)
	}
	if got := second.Dispatches(); len(got) != 1 || got[0].ID != dispatch.ID {
		t.Fatalf("dispatches = %#v", got)
	}
	if got := second.AwayPeriods(); len(got) != 1 || got[0].ID != period.ID {
		t.Fatalf("away periods = %#v", got)
	}
}

func writeHistoryFixture(t *testing.T, paths platform.Paths) {
	t.Helper()
	writeLines(t, paths.Events,
		`{"id":1,"source":"codex","kind":"done","session_id":"demo-history","text":"first","ts":"2026-11-10T00:00:00.000Z"}`,
		`{"id":2,"source":"claude","kind":"needs_input","session_id":"demo-history-2","text":"second","ts":"2026-11-10T00:00:01.000Z"}`,
	)
	writeLines(t, paths.Replies,
		`{"id":1,"session_id":"demo-history","text":"reply","files":[],"ts":"2026-11-10T00:00:00.000Z","injected":false,"status":"queued"}`,
		`{"id":1,"session_id":"demo-history","text":"reply","files":[],"ts":"2026-11-10T00:00:00.000Z","injected":true,"status":"injected"}`,
	)
	writeLines(t, paths.Dispatches,
		`{"dispatch_id":"dsp_demo","parent_session_id":"demo-history","target_agent":"claude","text":"review","status":"starting"}`,
		`{"dispatch_id":"dsp_demo","parent_session_id":"demo-history","target_agent":"claude","text":"review","status":"completed"}`,
	)
	writeLines(t, paths.AwayPeriods,
		`{"id":"awp_demo","started_at":"2026-11-10T00:00:00.000Z","source":"manual"}`,
		`{"id":"awp_demo","started_at":"2026-11-10T00:00:00.000Z","ended_at":"2026-11-10T01:00:00.000Z","source":"manual"}`,
	)
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if _, err := file.WriteString(line + "\n"); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLatestReplyUsesLastLine(t *testing.T) {
	var first, second Reply
	if err := json.Unmarshal([]byte(`{"id":1,"status":"queued"}`), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"id":1,"status":"injected"}`), &second); err != nil {
		t.Fatal(err)
	}
	got := latestReplies([]Reply{first, second})
	if len(got) != 1 || got[0].Status != "injected" {
		t.Fatalf("latestReplies = %#v", got)
	}
}

func TestLatestReplyDoesNotDowngradeDeliveredStatus(t *testing.T) {
	var queued, injected, stale Reply
	for _, pair := range []struct {
		raw    string
		target *Reply
	}{
		{`{"id":20,"status":"queued","message":"排队中"}`, &queued},
		{`{"id":20,"status":"injected","message":"已交给队列"}`, &injected},
		{`{"id":20,"status":"queued","message":"等待会话恢复"}`, &stale},
	} {
		if err := json.Unmarshal([]byte(pair.raw), pair.target); err != nil {
			t.Fatal(err)
		}
	}

	got := latestReplies([]Reply{queued, injected, stale})
	if len(got) != 1 || got[0].Status != "injected" ||
		got[0].Message != "已交给队列" {
		t.Fatalf("latestReplies = %#v", got)
	}
}

func TestUpdateReplyStatusIgnoresStaleQueuedRevision(t *testing.T) {
	store, err := Open(platform.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reply, err := store.AppendReply(Reply{
		SessionID: "demo-reply-status",
		Text:      "demo",
		Status:    "queued",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.UpdateReplyStatus(reply.ID, "injected", "已交给队列"); !ok {
		t.Fatal("initial status update failed")
	}
	updated, ok := store.UpdateReplyStatus(reply.ID, "queued", "等待会话恢复")
	if !ok {
		t.Fatal("stale status update reported missing reply")
	}
	if updated.Status != "injected" || updated.Message != "已交给队列" {
		t.Fatalf("updated reply = %#v", updated)
	}
}

func TestAgentReplyEventConfirmsOldestQueuedReply(t *testing.T) {
	s, err := Open(platform.NewPaths(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.AppendReply(Reply{
		SessionID: "demo-confirm",
		Text:      "first",
		Status:    "queued",
		Ts:        "2026-11-11T15:32:56.000Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AppendReply(Reply{
		SessionID: "demo-confirm",
		Text:      "second",
		Status:    "queued",
		Ts:        "2026-11-11T15:36:46.000Z",
	})
	if err != nil {
		t.Fatal(err)
	}

	confirmed := s.ConfirmQueuedRepliesForAgentEvent(Event{
		ID:        130,
		Kind:      "reply",
		SessionID: "demo-confirm",
		Ts:        "2026-11-11T15:43:51.000Z",
	})
	if len(confirmed) != 1 || confirmed[0].ID != first.ID ||
		confirmed[0].Status != "injected" {
		t.Fatalf("confirmed = %#v", confirmed)
	}
	for _, reply := range s.Replies() {
		switch reply.ID {
		case first.ID:
			if reply.Status != "injected" {
				t.Fatalf("first reply = %#v", reply)
			}
		case second.ID:
			if reply.Status != "queued" {
				t.Fatalf("second reply = %#v", reply)
			}
		}
	}
}

func TestHistoricalReplyEventsRepairQueuedRepliesOneAtATime(t *testing.T) {
	s := &Store{
		replies: []Reply{
			{ID: 18, SessionID: "demo", Status: "queued", Ts: "2026-11-11T15:32:56.000Z"},
			{ID: 19, SessionID: "demo", Status: "queued", Ts: "2026-11-11T15:36:46.000Z"},
			{ID: 21, SessionID: "demo", Status: "queued", Ts: "2026-11-11T15:41:57.000Z"},
		},
		events: []Event{
			{ID: 129, Kind: "reply", SessionID: "demo", Ts: "2026-11-11T15:33:13.000Z"},
			{ID: 130, Kind: "reply", SessionID: "demo", Ts: "2026-11-11T15:43:51.000Z"},
			{ID: 131, Kind: "reply", SessionID: "demo", Ts: "2026-11-11T15:48:43.000Z"},
		},
	}

	s.confirmHistoricalQueuedReplies()

	for _, reply := range s.replies {
		if reply.Status != "injected" {
			t.Fatalf("reply %d = %#v", reply.ID, reply)
		}
	}
}
