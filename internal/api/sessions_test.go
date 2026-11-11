package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tomato-hug/homing/internal/agent"
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
	var approval *InboxItem
	for _, item := range inbox.Waiting {
		kinds[item.Kind] = true
		if item.Kind == "approval" {
			candidate := item
			approval = &candidate
		}
	}
	if !kinds["needs_reply"] || !kinds["approval"] {
		t.Fatalf("waiting kinds = %v, want needs_reply and approval", kinds)
	}
	if approval == nil || approval.Approval == nil || len(approval.Approval.Options) != 2 {
		t.Fatalf("approval payload = %+v, want the legacy allow/deny pair", approval)
	}
}

func TestInboxCarriesACPPermissionOptions(t *testing.T) {
	events := `{"id":1,"ts":"2026-01-01T10:00:00.000Z","source":"hermes","kind":"permission","session_id":"demo-session-acp","cwd":"/tmp/demo-project","project":"demo-project","title":"请求授权","text":"写入文件","session_meta":{"acp":{"options":[{"id":"allow_once","label":"仅此次","kind":"allow_once"},{"id":"allow_always","label":"总是允许","kind":"allow_always"},{"id":"reject_once","label":"拒绝","kind":"reject_once"}]}}}
`
	d := writeStore(t, events, `{"version":1,"sessions":{}}`)
	inbox := d.buildInbox(httptest.NewRequest(http.MethodGet, "/api/inbox", nil))
	if len(inbox.Waiting) != 1 {
		t.Fatalf("waiting = %+v, want one approval", inbox.Waiting)
	}
	approval := inbox.Waiting[0].Approval
	if approval == nil || len(approval.Options) != 3 {
		t.Fatalf("approval = %+v, want three ACP options", approval)
	}
	if approval.Options[1]["id"] != "allow_always" || approval.Options[1]["decision"] != "allow" {
		t.Fatalf("allow_always option = %+v", approval.Options[1])
	}
	if approval.Options[2]["id"] != "reject_once" || approval.Options[2]["decision"] != "deny" {
		t.Fatalf("reject_once option = %+v", approval.Options[2])
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

type nilItemsDemoAgent struct{}

func (nilItemsDemoAgent) ID() string                   { return "api-null-items" }
func (nilItemsDemoAgent) Name() string                 { return "Null Items Demo" }
func (nilItemsDemoAgent) Icon() string                 { return "" }
func (nilItemsDemoAgent) PushSignal() agent.PushSignal { return agent.PushPoll }
func (nilItemsDemoAgent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Sessions: true}
}
func (nilItemsDemoAgent) Detect(context.Context) agent.Detection {
	return agent.Detection{Installed: true}
}
func (nilItemsDemoAgent) ListSessions(context.Context, int) ([]agent.Session, error) {
	return []agent.Session{{
		SessionID: "demo-null-items",
		Source:    "api-null-items",
		Title:     "空正文测试",
	}}, nil
}
func (nilItemsDemoAgent) Meta(string, string) agent.Meta { return agent.Meta{} }
func (nilItemsDemoAgent) ReadItems(context.Context, string, int) ([]agent.Item, error) {
	return nil, nil
}

var nilItemsDemoOnce sync.Once

func TestSessionItemsEncodesEmptyArrayInsteadOfNull(t *testing.T) {
	nilItemsDemoOnce.Do(func() {
		agent.RegisterProvider("api-null-items-test", func(context.Context) []agent.Agent {
			return []agent.Agent{nilItemsDemoAgent{}}
		})
	})
	d := writeStore(t, ``, `{"version":1,"sessions":{}}`)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/sessions/demo-null-items/items?limit=10",
		nil,
	)
	request.SetPathValue("id", "demo-null-items")
	recorder := httptest.NewRecorder()
	d.handleSessionItems(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Items []agent.Item `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Items == nil {
		t.Fatalf("items encoded as null: %s", recorder.Body.String())
	}
}

func TestSessionItemsHidesReasoningUnlessExplicitlyRequested(t *testing.T) {
	fake := installSSEDemoAgent(t)
	fake.setItems([]agent.Item{
		{ID: "demo-user", Seq: 1, Type: "message", Role: "user", Text: "做一個示範"},
		{ID: "demo-reasoning", Seq: 2, Type: "reasoning", Role: "assistant", Text: "內部推理"},
		{ID: "demo-answer", Seq: 3, Type: "message", Role: "assistant", Text: "完成"},
	})
	d := writeStore(t, sseDemoEvent, `{"version":1,"sessions":{}}`)

	read := func(query string) []agent.Item {
		t.Helper()
		request := httptest.NewRequest(
			http.MethodGet,
			"/api/sessions/demo-sse-1/items?"+query,
			nil,
		)
		request.SetPathValue("id", "demo-sse-1")
		recorder := httptest.NewRecorder()
		d.handleSessionItems(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d body = %s", recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Items []agent.Item `json:"items"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Items
	}

	defaultItems := read("limit=10")
	if len(defaultItems) != 2 {
		t.Fatalf("default items = %+v, want user + final answer", defaultItems)
	}
	for _, item := range defaultItems {
		if item.Type == "reasoning" {
			t.Fatalf("reasoning leaked without opt-in: %+v", item)
		}
	}

	optedIn := read("limit=10&include_reasoning=1")
	if len(optedIn) != 3 || optedIn[1].Type != "reasoning" {
		t.Fatalf("opted-in items = %+v, want all three with reasoning", optedIn)
	}
}
