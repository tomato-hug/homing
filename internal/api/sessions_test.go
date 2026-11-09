package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomato-hug/homing/internal/platform"
)

// writeStore lays down a data directory with hand-written event log and
// governance file. Everything here is obviously made up (demo-session-*), so a
// failure can never be mistaken for real user data.
func writeStore(t *testing.T, events, state string) *Deps {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session-state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"token":"demo-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Deps{DataDir: dir}
}

const demoEvents = `{"id":1,"ts":"2026-01-01T10:00:00.000Z","source":"codex","kind":"reply","session_id":"demo-session-1","cwd":"/tmp/demo-project","project":"demo-project","title":"Codex 回复","text":"第一行\n第二行","read":false}
{"id":2,"ts":"2026-01-01T10:05:00.000Z","source":"codex","kind":"needs_input","session_id":"demo-session-1","cwd":"/tmp/demo-project","project":"demo-project","title":"Codex 等待输入","text":"要选哪一个？","read":false}
{"id":3,"ts":"2026-01-01T09:00:00.000Z","source":"claude","kind":"permission","session_id":"demo-session-2","cwd":"/tmp/demo-project","project":"demo-project","title":"Claude Code 请求授权","text":"Bash：ls","read":true}
`

const demoState = `{"version":1,"sessions":{"codex:demo-session-1":{"pinned":true}}}`

// TestSummariesFoldEvents checks the event log becomes one row per session with
// the unread count, preview and pinned flag the phone expects.
func TestSummariesFoldEvents(t *testing.T) {
	d := writeStore(t, demoEvents, demoState)
	sessions := d.summaries(context.Background(), 50, "", "")
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(sessions))
	}
	// pinned first, even though demo-session-2 sorts by its own event.
	first := sessions[0]
	if first.SessionID != "demo-session-1" || !first.Pinned {
		t.Fatalf("first session = %+v, want pinned demo-session-1", first)
	}
	if first.Unread != 2 || first.EventCount != 2 {
		t.Errorf("unread/event_count = %d/%d, want 2/2", first.Unread, first.EventCount)
	}
	if first.Status != "needs_input" {
		t.Errorf("status = %q, want needs_input", first.Status)
	}
	if got := deref(first.LastPreview); got != "第一行" {
		t.Errorf("last_preview = %q, want 第一行", got)
	}
	// 标题来自事件里的 title… 但 session meta 走 agent，本地没有 codex 会话时
	// 回落到 first_prompt / project，这里只要求非空。
	if first.Title == "" {
		t.Error("title is empty")
	}
}

// TestSummariesFilterBySourceAndStatus covers the query parameters the console uses.
func TestSummariesFilterBySourceAndStatus(t *testing.T) {
	d := writeStore(t, demoEvents, demoState)
	bySource := d.summaries(context.Background(), 50, "claude", "")
	if len(bySource) != 1 || bySource[0].SessionID != "demo-session-2" {
		t.Fatalf("source filter = %+v", bySource)
	}
	byStatus := d.summaries(context.Background(), 50, "", "needs_input")
	if len(byStatus) != 1 || byStatus[0].SessionID != "demo-session-1" {
		t.Fatalf("status filter = %+v", byStatus)
	}
}

// TestInboxBuckets checks the three buckets and the kind mapping.
func TestInboxBuckets(t *testing.T) {
	d := writeStore(t, demoEvents, demoState)
	inbox := d.buildInbox(httptest.NewRequest(http.MethodGet, "/api/inbox", nil))
	if len(inbox.Waiting) != 2 {
		t.Fatalf("waiting = %d, want 2 (needs_reply + approval)", len(inbox.Waiting))
	}
	kinds := map[string]bool{}
	for _, item := range inbox.Waiting {
		kinds[item.Kind] = true
	}
	if !kinds["needs_reply"] || !kinds["approval"] {
		t.Fatalf("waiting kinds = %v, want needs_reply and approval", kinds)
	}
}

// TestLocalhostPorts keeps the "open on this Mac" hint honest.
func TestLocalhostPorts(t *testing.T) {
	got := localhostPorts("看 http://localhost:3456/a 和 127.0.0.1:5173 ，还有 localhost:3456 。")
	if len(got) != 2 || got[0] != 3456 || got[1] != 5173 {
		t.Fatalf("ports = %v, want [3456 5173]", got)
	}
}

// TestClampInt guards the limit bounds of /api/sessions.
func TestClampInt(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 100}, {"0", 1}, {"-5", 1}, {"9999", 500}, {"42", 42}, {"abc", 100},
	}
	for _, c := range cases {
		if got := clampInt(c.raw, 100, 1, 500); got != c.want {
			t.Errorf("clampInt(%q) = %d, want %d", c.raw, got, c.want)
		}
	}
}

// TestPathsAreInsideDataDir makes sure the store never walks out of the data dir.
func TestPathsAreInsideDataDir(t *testing.T) {
	dir := t.TempDir()
	paths := platform.NewPaths(dir)
	if filepath.Dir(paths.Events) != dir {
		t.Fatalf("events path %q is not inside %q", paths.Events, dir)
	}
}
