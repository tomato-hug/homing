package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

const sseDemoEvent = `{"id":1,"ts":"2026-01-01T10:00:00.000Z","source":"sse-demo","kind":"reply","session_id":"demo-sse-1","cwd":"/tmp/demo-project","project":"demo-project","title":"SSE 測試","text":"demo","read":false}
`

type sseDemoAgent struct {
	mu    sync.Mutex
	items []agent.Item
}

func (a *sseDemoAgent) setItems(items []agent.Item) {
	a.mu.Lock()
	a.items = append([]agent.Item(nil), items...)
	a.mu.Unlock()
}

func (a *sseDemoAgent) ID() string                   { return "sse-demo" }
func (a *sseDemoAgent) Name() string                 { return "SSE Demo" }
func (a *sseDemoAgent) Icon() string                 { return "" }
func (a *sseDemoAgent) PushSignal() agent.PushSignal { return agent.PushHook }
func (a *sseDemoAgent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Reply: true, Sessions: true, Live: true}
}
func (a *sseDemoAgent) Detect(context.Context) agent.Detection {
	return agent.Detection{Installed: true}
}
func (a *sseDemoAgent) ListSessions(context.Context, int) ([]agent.Session, error) {
	return []agent.Session{{
		SessionID: "demo-sse-1",
		Source:    "sse-demo",
		Title:     "SSE Demo",
	}}, nil
}
func (a *sseDemoAgent) Meta(string, string) agent.Meta {
	return agent.Meta{Cwd: "/tmp/demo-project"}
}
func (a *sseDemoAgent) ReadItems(_ context.Context, _ string, limit int) ([]agent.Item, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	items := append([]agent.Item(nil), a.items...)
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

var (
	sseDemoOnce  sync.Once
	sseDemoState = &sseDemoAgent{}
)

func installSSEDemoAgent(t *testing.T) *sseDemoAgent {
	t.Helper()
	sseDemoOnce.Do(func() {
		agent.RegisterProvider("sse-demo-test", func(context.Context) []agent.Agent {
			return []agent.Agent{sseDemoState}
		})
	})
	sseDemoState.setItems(nil)
	t.Cleanup(func() { sseDemoState.setItems(nil) })
	return sseDemoState
}

func openSSEStream(t *testing.T, d *Deps, query, lastEventID string) (*httptest.Server, io.ReadCloser, context.CancelFunc) {
	t.Helper()
	server := httptest.NewServer(NewMux(d))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/stream?"+query, nil)
	if err != nil {
		cancel()
		server.Close()
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer demo-token")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		cancel()
		server.Close()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		server.Close()
		t.Fatalf("stream status = %d: %s", resp.StatusCode, body)
	}
	return server, resp.Body, cancel
}

type sseFrame struct {
	ID    string
	Event string
	Data  []byte
}

func readSSEFrame(t *testing.T, reader *bufio.Scanner) (sseFrame, bool) {
	t.Helper()
	frame := sseFrame{}
	for reader.Scan() {
		line := reader.Text()
		if line == "" {
			if frame.Event != "" || len(frame.Data) > 0 {
				return frame, true
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "id:"):
			frame.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "event:"):
			frame.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			frame.Data = append(frame.Data, []byte(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))...)
		}
	}
	return frame, false
}

func TestSSEResumeBackfillsItemsAfterCursor(t *testing.T) {
	fake := installSSEDemoAgent(t)
	fake.setItems([]agent.Item{
		{ID: "item-old", Seq: 1, Type: "message", Role: "assistant", Text: "old"},
		{ID: "item-new", Seq: 2, Type: "message", Role: "assistant", Text: "new"},
	})
	d := writeStore(t, sseDemoEvent, `{"version":1,"sessions":{}}`)
	server, body, cancel := openSSEStream(t, d, "session_id=demo-sse-1", "e1.i1")
	defer func() {
		cancel()
		body.Close()
		server.Close()
	}()

	scanner := bufio.NewScanner(body)
	frame, ok := readSSEFrame(t, scanner)
	if !ok {
		t.Fatal("stream ended before the backfilled item")
	}
	if frame.Event != "item" || frame.ID != "e1.i2" {
		t.Fatalf("frame = %+v, want item at e1.i2", frame)
	}
	var payload struct {
		SessionID string     `json:"session_id"`
		Item      agent.Item `json:"item"`
	}
	if err := json.Unmarshal(frame.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SessionID != "demo-sse-1" || payload.Item.ID != "item-new" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestSSEHidesReasoningUnlessExplicitlyRequested(t *testing.T) {
	fake := installSSEDemoAgent(t)
	fake.setItems([]agent.Item{
		{ID: "item-reasoning", Seq: 2, Type: "reasoning", Role: "assistant", Text: "內部推理"},
		{ID: "item-answer", Seq: 3, Type: "message", Role: "assistant", Text: "最終答案"},
	})
	d := writeStore(t, sseDemoEvent, `{"version":1,"sessions":{}}`)

	server, body, cancel := openSSEStream(
		t, d, "session_id=demo-sse-1", "e1.i1",
	)
	defer func() {
		cancel()
		body.Close()
		server.Close()
	}()
	scanner := bufio.NewScanner(body)
	frame, ok := readSSEFrame(t, scanner)
	if !ok {
		t.Fatal("stream ended before the final answer")
	}
	var payload struct {
		Item agent.Item `json:"item"`
	}
	if err := json.Unmarshal(frame.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Item.ID != "item-answer" {
		t.Fatalf("default stream item = %+v, want only the final answer", payload.Item)
	}
}

func TestSSECanIncludeReasoningOnRequest(t *testing.T) {
	fake := installSSEDemoAgent(t)
	fake.setItems([]agent.Item{
		{ID: "item-reasoning", Seq: 2, Type: "reasoning", Role: "assistant", Text: "內部推理"},
		{ID: "item-answer", Seq: 3, Type: "message", Role: "assistant", Text: "最終答案"},
	})
	d := writeStore(t, sseDemoEvent, `{"version":1,"sessions":{}}`)

	server, body, cancel := openSSEStream(
		t, d, "session_id=demo-sse-1&include_reasoning=1", "e1.i1",
	)
	defer func() {
		cancel()
		body.Close()
		server.Close()
	}()
	scanner := bufio.NewScanner(body)
	frame, ok := readSSEFrame(t, scanner)
	if !ok {
		t.Fatal("stream ended before the reasoning item")
	}
	var payload struct {
		Item agent.Item `json:"item"`
	}
	if err := json.Unmarshal(frame.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Item.ID != "item-reasoning" {
		t.Fatalf("opted-in stream item = %+v, want reasoning first", payload.Item)
	}
}

func TestSSELargeItemIsTruncatedButItemsEndpointIsFull(t *testing.T) {
	fake := installSSEDemoAgent(t)
	fullText := strings.Repeat("超大輸出-", 20_000)
	largeItem := agent.Item{
		ID: "item-large", Seq: 7, Type: "tool_call", Role: "assistant",
		Tool: &agent.ItemTool{
			Name:         "demo-tool",
			InputSummary: stringPtr("input"),
			OutputTail:   stringPtr(fullText),
		},
		Files: []agent.ItemFile{{
			Path: "demo.txt", Added: 1, Diff: fullText,
		}},
	}
	started := time.Now()
	preview, changed := truncateItemFrame("demo-sse-1", largeItem)
	if !changed || !preview.Truncated || frameSize("demo-sse-1", preview) >= maxSSEFrameBytes {
		t.Fatalf("truncate result = changed:%v truncated:%v size:%d",
			changed, preview.Truncated, frameSize("demo-sse-1", preview))
	}
	t.Logf("truncated in %s: %d -> %d bytes", time.Since(started),
		frameSize("demo-sse-1", largeItem), frameSize("demo-sse-1", preview))

	fake.setItems([]agent.Item{largeItem})
	d := writeStore(t, sseDemoEvent, `{"version":1,"sessions":{}}`)
	server, body, cancel := openSSEStream(t, d, "session_id=demo-sse-1", "e1.i6")
	defer func() {
		cancel()
		body.Close()
		server.Close()
	}()

	scanner := bufio.NewScanner(body)
	frame, ok := readSSEFrame(t, scanner)
	if !ok {
		t.Fatal("stream ended before the truncated item")
	}
	if frame.Event != "item" || frame.ID != "e1.i7" {
		t.Fatalf("frame = %+v, want item at e1.i7", frame)
	}
	if len(frame.Data) >= maxSSEFrameBytes {
		t.Fatalf("SSE item data = %d bytes, want < %d", len(frame.Data), maxSSEFrameBytes)
	}
	var live struct {
		Item agent.Item `json:"item"`
	}
	if err := json.Unmarshal(frame.Data, &live); err != nil {
		t.Fatal(err)
	}
	if !live.Item.Truncated {
		t.Fatal("live item does not set truncated=true")
	}
	if live.Item.Tool == nil || live.Item.Tool.OutputTail == nil {
		t.Fatal("live item lost its tool preview")
	}
	if len(*live.Item.Tool.OutputTail) >= len(fullText) {
		t.Fatal("live tool output was not shortened")
	}

	req, err := http.NewRequest(http.MethodGet,
		server.URL+"/api/sessions/demo-sse-1/items?limit=2000", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer demo-token")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("items status = %d: %s", resp.StatusCode, raw)
	}
	var itemsResponse struct {
		Items []agent.Item `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&itemsResponse); err != nil {
		t.Fatal(err)
	}
	if len(itemsResponse.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(itemsResponse.Items))
	}
	item := itemsResponse.Items[0]
	if item.Truncated || item.Tool == nil || item.Tool.OutputTail == nil ||
		*item.Tool.OutputTail != fullText || len(item.Files) != 1 || item.Files[0].Diff != fullText {
		t.Fatalf("durable item was altered: %+v", item)
	}
}

func TestSSEGlobalStreamKeepsItemSessionAndDoesNotCrossDedupe(t *testing.T) {
	installSSEDemoAgent(t)
	d := writeStore(t, sseDemoEvent, `{"version":1,"sessions":{}}`)
	server, body, cancel := openSSEStream(t, d, "", "")
	defer func() {
		cancel()
		body.Close()
		server.Close()
	}()
	scanner := bufio.NewScanner(body)
	initial, ok := readSSEFrame(t, scanner)
	if !ok || initial.Event != "message" {
		t.Fatalf("initial frame = %+v, want message", initial)
	}

	for _, sessionID := range []string{"demo-session-a", "demo-session-b"} {
		PublishNamed("item", sessionID, map[string]any{
			"session_id": sessionID,
			"item": agent.Item{
				ID: "item-" + sessionID, Seq: 3, Type: "message", Role: "assistant", Text: sessionID,
			},
		})
	}
	seen := map[string]bool{}
	for len(seen) < 2 {
		frame, open := readSSEFrame(t, scanner)
		if !open {
			t.Fatalf("stream ended after %d frames", len(seen))
		}
		if frame.Event != "item" {
			continue
		}
		var payload struct {
			SessionID string     `json:"session_id"`
			Item      agent.Item `json:"item"`
		}
		if err := json.Unmarshal(frame.Data, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.SessionID == "" {
			t.Fatal("global item lost its session_id")
		}
		seen[payload.SessionID] = true
	}
	if !seen["demo-session-a"] || !seen["demo-session-b"] {
		t.Fatalf("sessions = %v", seen)
	}
}

func stringPtr(value string) *string { return &value }

var _ agent.Agent = (*sseDemoAgent)(nil)
var _ agent.ItemReader = (*sseDemoAgent)(nil)
