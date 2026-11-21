package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tomato-hug/homing/internal/store"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

type historyTestEnvelope struct {
	Items []struct {
		agent.Item
		ItemKey string `json:"item_key"`
	} `json:"items"`
	Version      int     `json:"history_version"`
	Before       *string `json:"before_cursor"`
	Status       string  `json:"history_status"`
	AnchorStatus string  `json:"anchor_status"`
	More         *bool   `json:"has_more_before"`
	Rounds       int     `json:"rounds_returned"`
	Code         string  `json:"code"`
	Reason       string  `json:"history_reason"`
}

func historyFixture(t *testing.T) (*Deps, *sseDemoAgent, []agent.Item) {
	t.Helper()
	historyDemoOnce.Do(func() {
		agent.RegisterProvider("demo-history-test", func(context.Context) []agent.Agent { return []agent.Agent{historyDemoState} })
	})
	fake := &historyDemoState.sseDemoAgent
	fake.setItems(nil)
	t.Cleanup(func() { fake.setItems(nil) })
	var items []agent.Item
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for round := 1; round <= 14; round++ {
		for n, role := range []string{"user", "tool", "assistant"} {
			i := len(items) + 1
			item := agent.Item{ID: fmt.Sprintf("demo-history-i%d", i), Seq: i, TS: base.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano), Type: "message", Role: role, Text: fmt.Sprintf("demo-round-%02d-%s", round, role)}
			if n == 1 {
				item.Type = "tool_call"
				item.Tool = &agent.ItemTool{Name: "demo-tool", Status: "done"}
			}
			items = append(items, item)
		}
	}
	fake.setItems(items)
	event := fmt.Sprintf(`{"id":1,"ts":%q,"source":"demo-history","kind":"done","session_id":"demo-sse-1","text":%q}`+"\n", items[38].TS, items[38].Text)
	d := writeStore(t, event, `{"version":1,"sessions":{}}`)
	t.Cleanup(func() { _ = d.Close() })
	return d, fake, items
}

func historyRequest(t *testing.T, d *Deps, session, query string) (int, historyTestEnvelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+session+"/items?"+query, nil)
	req.SetPathValue("id", session)
	rec := httptest.NewRecorder()
	d.handleSessionItems(rec, req)
	var got historyTestEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return rec.Code, got
}

const historyQuery = "source=demo-history&anchor_event_id=1"

func TestHistoryInitialStartsAtReceivedOutput(t *testing.T) {
	d, _, _ := historyFixture(t)
	status, got := historyRequest(t, d, "demo-sse-1", historyQuery)
	if status != 200 || got.Version != 1 || len(got.Items) != 4 || got.Items[0].Text != "demo-round-13-assistant" || got.AnchorStatus != "matched" || got.Before == nil {
		t.Fatalf("initial: status=%d %+v", status, got)
	}
	for _, item := range got.Items {
		if item.ItemKey == "" {
			t.Fatal("missing stable item key")
		}
	}
}

func TestHistoryLoadsTenWholeRoundsAndOverlapsOnlyAnchorRound(t *testing.T) {
	d, _, _ := historyFixture(t)
	_, initial := historyRequest(t, d, "demo-sse-1", historyQuery)
	if initial.Before == nil {
		t.Fatal("missing cursor")
	}
	status, page := historyRequest(t, d, "demo-sse-1", historyQuery+"&turns=10&before="+url.QueryEscape(*initial.Before))
	if status != 200 || page.Rounds != 10 || len(page.Items) != 30 || page.Items[0].Text != "demo-round-04-user" || page.Items[29].Text != "demo-round-13-assistant" {
		t.Fatalf("history: status=%d %+v", status, page)
	}
	if page.Items[29].ItemKey != initial.Items[0].ItemKey {
		t.Fatal("overlap has a different stable key")
	}
	if page.Before == nil {
		t.Fatal("missing second cursor")
	}
	status, last := historyRequest(t, d, "demo-sse-1", historyQuery+"&turns=10&before="+url.QueryEscape(*page.Before))
	if status != 200 || last.Rounds != 3 || len(last.Items) != 9 || last.More != nil || last.Status != "unknown" {
		t.Fatalf("last retained: status=%d %+v", status, last)
	}
	if last.Before == nil {
		t.Fatal("unknown boundary must remain explicit")
	}
	status, unavailable := historyRequest(t, d, "demo-sse-1", historyQuery+"&turns=10&before="+url.QueryEscape(*last.Before))
	if status != 409 || unavailable.Code != "history_unavailable" {
		t.Fatalf("boundary: %d %+v", status, unavailable)
	}
}

func TestHistoryRefreshAndCursorSurviveTailRenumbering(t *testing.T) {
	d, fake, items := historyFixture(t)
	_, initial := historyRequest(t, d, "demo-sse-1", historyQuery)
	if initial.Before == nil {
		t.Fatal("missing cursor")
	}
	items = append([]agent.Item(nil), items[3:]...)
	for i := range items {
		items[i].Seq = i + 1
		items[i].ID = fmt.Sprintf("demo-history-i%d", i+1)
	}
	fake.setItems(items)
	_, refreshed := historyRequest(t, d, "demo-sse-1", historyQuery)
	if len(refreshed.Items) != 4 || refreshed.Items[0].ItemKey != initial.Items[0].ItemKey {
		t.Fatalf("unstable refresh: %+v", refreshed)
	}
	status, page := historyRequest(t, d, "demo-sse-1", historyQuery+"&turns=10&before="+url.QueryEscape(*initial.Before))
	if status != 200 || page.Rounds != 10 {
		t.Fatalf("renumber: %d %+v", status, page)
	}
}

func TestHistoryRejectsInvalidCrossIdentityAndExpiredCursors(t *testing.T) {
	d, fake, items := historyFixture(t)
	_, initial := historyRequest(t, d, "demo-sse-1", historyQuery)
	if initial.Before == nil {
		t.Fatal("missing cursor")
	}
	for _, tc := range []struct {
		query, session string
		status         int
		code           string
	}{
		{"source=other&anchor_event_id=1", "demo-sse-1", 409, "history_source_mismatch"},
		{historyQuery + "&before=garbage&turns=10", "demo-sse-1", 400, "invalid_history_cursor"},
		{"anchor_event_id=1", "demo-sse-1", 400, "invalid_history_request"},
		{"source=demo-history&anchor_event_id=999", "demo-sse-1", 410, "history_anchor_expired"},
		{historyQuery + "&before=" + url.QueryEscape(*initial.Before), "demo-other", 400, "invalid_history_cursor"},
		{historyQuery + "&turns=11", "demo-sse-1", 400, "invalid_history_request"},
		{historyQuery + "&after_seq=3", "demo-sse-1", 400, "invalid_history_request"},
	} {
		status, got := historyRequest(t, d, tc.session, tc.query)
		if status != tc.status || got.Code != tc.code {
			t.Errorf("%s: %d %+v", tc.query, status, got)
		}
	}
	fake.setItems(items[39:])
	status, got := historyRequest(t, d, "demo-sse-1", historyQuery+"&before="+url.QueryEscape(*initial.Before))
	if status != 409 || got.Code != "history_cursor_expired" {
		t.Fatalf("retention: %d %+v", status, got)
	}
}

func TestHistoryUnresolvedAnchorNeverTransfersEarlierOrUntimedItems(t *testing.T) {
	d, fake, items := historyFixture(t)
	items[38].Text = "demo-output-not-the-notification"
	items = append(items, agent.Item{ID: "demo-untimed", Role: "assistant", Text: "must not leak"})
	fake.setItems(items)
	status, got := historyRequest(t, d, "demo-sse-1", historyQuery)
	if status != 200 || got.AnchorStatus != "timestamp_only" || len(got.Items) != 3 || got.Before == nil || got.Status != "available" {
		t.Fatalf("fallback: %d %+v", status, got)
	}
	for _, item := range got.Items {
		if !strings.HasPrefix(item.Text, "demo-round-14-") {
			t.Fatalf("pre-anchor leaked: %+v", item)
		}
	}
}

func TestHistoryPreservesToolResponsesAndNeverSplitsOversizedRounds(t *testing.T) {
	d, fake, items := historyFixture(t)
	_, initial := historyRequest(t, d, "demo-sse-1", historyQuery)
	if initial.Before == nil {
		t.Fatal("missing cursor")
	}
	items[10].Text = strings.Repeat("demo-tool-output ", 300000)
	fake.setItems(items)
	status, got := historyRequest(t, d, "demo-sse-1", historyQuery+"&before="+url.QueryEscape(*initial.Before)+"&turns=10")
	if status != 413 || got.Code != "history_page_too_large" {
		t.Fatalf("oversized: %d %+v", status, got)
	}
}

func TestHistoryDelayedHookDoesNotExposeAdjacentEarlierPrompt(t *testing.T) {
	d, _, _ := historyFixture(t)
	s, _ := d.store()
	_, err := s.AppendEvent(store.Event{Source: "demo-history", SessionID: "demo-sse-1", Kind: "done", Ts: "2026-01-01T10:00:41Z", Text: "demo-round-13-assistant"})
	if err != nil {
		t.Fatal(err)
	}
	status, got := historyRequest(t, d, "demo-sse-1", "source=demo-history&anchor_event_id=2")
	if status != 200 || len(got.Items) != 2 || got.Items[0].Text != "demo-round-13-assistant" || got.Items[1].Text != "demo-round-14-assistant" {
		t.Fatalf("delayed hook leaked adjacent prompt: %d %+v", status, got)
	}
}

func TestHistoryRejectsPartialRoundWithoutDiscardingToolOutput(t *testing.T) {
	d, fake, items := historyFixture(t)
	_, initial := historyRequest(t, d, "demo-sse-1", historyQuery)
	if initial.Before == nil {
		t.Fatal("missing cursor")
	}
	items[37].Tool.Status = "running"
	fake.setItems(items)
	status, got := historyRequest(t, d, "demo-sse-1", historyQuery+"&before="+url.QueryEscape(*initial.Before))
	if status != 409 || got.Code != "history_round_incomplete" {
		t.Fatalf("partial round: %d %+v", status, got)
	}
}

func TestHistoryAmbiguousDuplicateCursorExpiresAfterRetention(t *testing.T) {
	d, fake, items := historyFixture(t)
	items[38].Text = "demo-other-output"
	items[40] = items[39]
	fake.setItems(items)
	_, initial := historyRequest(t, d, "demo-sse-1", historyQuery)
	if initial.Before == nil || len(initial.Items) != 3 {
		t.Fatalf("no timestamp boundary: %+v", initial)
	}
	if initial.Items[0].ItemKey == initial.Items[1].ItemKey {
		t.Fatal("distinct duplicate rows collapsed")
	}
	fake.setItems(append(items[:39], items[40:]...))
	status, got := historyRequest(t, d, "demo-sse-1", historyQuery+"&before="+url.QueryEscape(*initial.Before))
	if status != 409 || got.Code != "history_cursor_expired" {
		t.Fatalf("ambiguous retained cursor: %d %+v", status, got)
	}
}

func TestHistoryReasoningOptInKeepsBoundaryStable(t *testing.T) {
	d, fake, items := historyFixture(t)
	items[40].Type = "reasoning"
	items[40].Role = "assistant"
	fake.setItems(items)
	_, normal := historyRequest(t, d, "demo-sse-1", historyQuery)
	_, included := historyRequest(t, d, "demo-sse-1", historyQuery+"&include_reasoning=1")
	if len(normal.Items) != 3 || len(included.Items) != 4 || normal.Before == nil || included.Before == nil || *normal.Before != *included.Before {
		t.Fatalf("reasoning filtering changed boundary: %+v %+v", normal, included)
	}
}

func TestHistoryUnresolvedEmptyTranscriptDoesNotClaimEnd(t *testing.T) {
	d, fake, _ := historyFixture(t)
	fake.setItems(nil)
	status, got := historyRequest(t, d, "demo-sse-1", historyQuery)
	if status != 200 || got.Items == nil || len(got.Items) != 0 || got.Before != nil || got.More != nil || got.Status != "unknown" || got.Reason != "anchor_unresolved" {
		t.Fatalf("empty: %d %+v", status, got)
	}
}

// These agents only return synthetic errors and never access user sessions.
type historyUnsupportedAgent struct{}

func (historyUnsupportedAgent) ID() string                             { return "demo-history-unsupported" }
func (historyUnsupportedAgent) Name() string                           { return "Demo history unsupported" }
func (historyUnsupportedAgent) Icon() string                           { return "" }
func (historyUnsupportedAgent) PushSignal() agent.PushSignal           { return agent.PushPoll }
func (historyUnsupportedAgent) Capabilities() agent.Capabilities       { return agent.Capabilities{} }
func (historyUnsupportedAgent) Detect(context.Context) agent.Detection { return agent.Detection{} }
func (historyUnsupportedAgent) ListSessions(context.Context, int) ([]agent.Session, error) {
	return nil, nil
}
func (historyUnsupportedAgent) Meta(string, string) agent.Meta { return agent.Meta{} }

type historyFailedAgent struct{ historyUnsupportedAgent }

func (historyFailedAgent) ID() string { return "demo-history-failed" }
func (historyFailedAgent) ReadItems(context.Context, string, int) ([]agent.Item, error) {
	return nil, errors.New("demo reader failure")
}

var historyErrorsOnce sync.Once

func TestHistoryReaderFailureAndUnsupportedAreExplicit(t *testing.T) {
	historyErrorsOnce.Do(func() {
		agent.RegisterProvider("demo-history-errors", func(context.Context) []agent.Agent {
			return []agent.Agent{historyUnsupportedAgent{}, historyFailedAgent{}}
		})
	})
	for _, tc := range []struct {
		source, code string
		status       int
	}{{"demo-history-unsupported", "history_unsupported", 501}, {"demo-history-failed", "history_read_failed", 502}} {
		d := writeStore(t, fmt.Sprintf(`{"id":1,"ts":"2026-01-01T10:00:00Z","source":%q,"session_id":"demo-sse-1","kind":"done","text":"demo"}`+"\n", tc.source), `{"version":1,"sessions":{}}`)
		t.Cleanup(func() { _ = d.Close() })
		status, got := historyRequest(t, d, "demo-sse-1", "source="+tc.source+"&anchor_event_id=1")
		if status != tc.status || got.Code != tc.code {
			t.Fatalf("%s: %d %+v", tc.source, status, got)
		}
	}
}

func TestHistoryMergesNativeDriverOnlyWithVerifiedOverlap(t *testing.T) {
	native := []agent.Item{{TS: "2026-01-01T10:00:01Z", ID: "demo-history-i1", Role: "user", Type: "message", Text: "demo prompt"}, {TS: "2026-01-01T10:00:02Z", ID: "demo-history-i2", Role: "assistant", Type: "message", Text: "demo answer"}}
	live := []agent.Item{{TS: "2026-01-01T10:00:03Z", ID: "acp-1-user", Role: "user", Type: "message", Text: "demo prompt"}, {TS: "2026-01-01T10:00:04Z", ID: "acp-1-assistant", Role: "assistant", Type: "message", Text: "demo answer"}, {TS: "2026-01-01T10:00:05Z", ID: "acp-2-user", Role: "user", Type: "message", Text: "demo next prompt"}}
	merged, _, code := mergeHistoryWindows(native, live)
	if code != "" || len(merged) != 3 || merged[2].Text != "demo next prompt" {
		t.Fatalf("duplicate mirror: code=%s items=%+v", code, merged)
	}
	live[1].Text = "demo divergent output"
	_, status, code := mergeHistoryWindows(native, live)
	if status != 409 || code != "history_mixed_transcript" {
		t.Fatalf("unverified merge: %d %s", status, code)
	}
}

type historyDemoAgent struct{ sseDemoAgent }

func (*historyDemoAgent) ID() string                                                 { return "demo-history" }
func (*historyDemoAgent) ListSessions(context.Context, int) ([]agent.Session, error) { return nil, nil }

var historyDemoOnce sync.Once
var historyDemoState = &historyDemoAgent{}
