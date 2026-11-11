package store

import (
	"testing"

	"github.com/tomato-hug/homing/internal/platform"
)

func TestSeenWatermarkBecomesUnreadOnNewEvent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(platform.NewPaths(dir))
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.AppendEvent(Event{Source: "codex", SessionID: "demo-session-1", Kind: "reply"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchSession("codex", "demo-session-1", map[string]bool{"read": true}); err != nil {
		t.Fatal(err)
	}
	state := s.SessionState("codex", "demo-session-1")
	if !state.Read || state.SeenEventID != first.ID {
		t.Fatalf("state after read = %+v, want read at %d", state, first.ID)
	}
	second, err := s.AppendEvent(Event{Source: "codex", SessionID: "demo-session-1", Kind: "needs_input"})
	if err != nil {
		t.Fatal(err)
	}
	state = s.SessionState("codex", "demo-session-1")
	if state.Read {
		t.Fatalf("new event %d should make the session unread again", second.ID)
	}
	if err := s.AdvanceSeen("codex", "demo-session-1", second.ID); err != nil {
		t.Fatal(err)
	}
	if state = s.SessionState("codex", "demo-session-1"); !state.Read {
		t.Fatalf("state after advance = %+v, want read", state)
	}
}

func TestAwayPeriodsAppendAndClear(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(platform.NewPaths(dir))
	if err != nil {
		t.Fatal(err)
	}
	period, err := s.StartAway("manual", "demo-device")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Away() {
		t.Fatal("away config should be enabled")
	}
	if current, ok := s.CurrentAwayPeriod(); !ok || current.ID != period.ID {
		t.Fatalf("current = %+v, %v", current, ok)
	}
	// Starting again must not create a second period.
	again, err := s.StartAway("manual", "demo-device")
	if err != nil || again.ID != period.ID {
		t.Fatalf("second start = %+v, %v", again, err)
	}
	ended, ok, err := s.EndAway()
	if err != nil || !ok || ended.EndedAt == "" {
		t.Fatalf("end = %+v, %v, %v", ended, ok, err)
	}
	if s.Away() {
		t.Fatal("away config should be disabled")
	}
	cleared, ok, err := s.ClearAwayPeriod(period.ID)
	if err != nil || !ok || cleared.ClearedAt == "" {
		t.Fatalf("clear = %+v, %v, %v", cleared, ok, err)
	}

	reopened, err := Open(platform.NewPaths(dir))
	if err != nil {
		t.Fatal(err)
	}
	periods := reopened.AwayPeriods()
	if len(periods) != 1 || periods[0].EndedAt == "" || periods[0].ClearedAt == "" {
		t.Fatalf("reloaded periods = %+v", periods)
	}
}

func TestRemindOnDoneIsPersistedAndConsumedOnce(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(platform.NewPaths(dir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(Event{
		Source: "codex", SessionID: "demo-session-1", Kind: "reply",
	}); err != nil {
		t.Fatal(err)
	}
	state, err := s.PatchSession(
		"codex", "demo-session-1", map[string]bool{"remind_on_done": true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !state.RemindOnDone {
		t.Fatalf("state = %+v, want reminder enabled", state)
	}

	reopened, err := Open(platform.NewPaths(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.SessionState("codex", "demo-session-1").RemindOnDone {
		t.Fatal("reminder did not survive reopening the store")
	}
	if !reopened.ConsumeRemindOnDone("codex", "demo-session-1") {
		t.Fatal("first consume should return true")
	}
	if reopened.ConsumeRemindOnDone("codex", "demo-session-1") {
		t.Fatal("second consume should return false")
	}
	if reopened.SessionState("codex", "demo-session-1").RemindOnDone {
		t.Fatal("consumed reminder should be cleared in state")
	}
}

func TestCollabReturnDefaultsToCardAndPersistsOptIn(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(platform.NewPaths(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.CollabReturn(); got != "card" {
		t.Fatalf("default collab return = %q, want card", got)
	}
	if err := s.SetCollabReturn("anything"); err == nil {
		t.Fatal("invalid return mode should be rejected")
	}
	if err := s.SetCollabReturn("inject"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(platform.NewPaths(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.CollabReturn(); got != "inject" {
		t.Fatalf("reloaded collab return = %q, want inject", got)
	}
	if err := reopened.SetCollabReturn("card"); err != nil {
		t.Fatal(err)
	}
	if got := reopened.CollabReturn(); got != "card" {
		t.Fatalf("collab return after opt-out = %q, want card", got)
	}
}
