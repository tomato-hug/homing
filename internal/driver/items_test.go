package driver

import (
	"strings"
	"testing"
	"time"
)

func testMapper() *turnMapper {
	mapper := newTurnMapper("session-1", "fake", 1, 0)
	mapper.now = func() time.Time {
		return time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)
	}
	return mapper
}

func update(kind string, body map[string]any) Update {
	if body == nil {
		body = map[string]any{}
	}
	body["sessionUpdate"] = kind
	return Update{
		SessionID: "session-1",
		Kind:      kind,
		Text:      stringValue(body["text"]),
		Raw: map[string]any{
			"sessionId": "session-1",
			"update":    body,
		},
	}
}

func TestTurnMapperMergesMessageChunks(t *testing.T) {
	mapper := testMapper()

	first := mapper.Map(update("agent_message_chunk", map[string]any{
		"content": map[string]any{"type": "text", "text": "Hello"},
	}))
	second := mapper.Map(update("agent_message_chunk", map[string]any{
		"content": map[string]any{"type": "text", "text": " world"},
	}))

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("chunk maps = %#v / %#v", first, second)
	}
	if first[0].ID != second[0].ID {
		t.Fatalf("chunk ids changed: %q != %q", first[0].ID, second[0].ID)
	}
	if first[0].Seq != second[0].Seq {
		t.Fatalf("chunk seq changed: %d != %d", first[0].Seq, second[0].Seq)
	}
	if second[0].Text != "Hello world" || !second[0].Partial {
		t.Fatalf("merged chunk = %#v", second[0])
	}

	final := mapper.Finish()
	if len(final) != 1 || final[0].Partial || final[0].Text != "Hello world" {
		t.Fatalf("final message = %#v", final)
	}
	if mapper.FinalText() != "Hello world" {
		t.Fatalf("FinalText = %q", mapper.FinalText())
	}
}

func TestTurnMapperMapsToolDiffAndStableStatus(t *testing.T) {
	mapper := testMapper()
	running := mapper.Map(update("tool_call", map[string]any{
		"toolCallId": "call-1",
		"title":      "Edit login.ts",
		"kind":       "edit",
		"status":     "in_progress",
		"content": []any{
			map[string]any{
				"type":    "diff",
				"path":    "src/login.ts",
				"oldText": "const ok = false;\n",
				"newText": "const ok = true;\n",
			},
		},
	}))
	if len(running) != 1 {
		t.Fatalf("running tool = %#v", running)
	}
	item := running[0]
	if item.Type != "file_change" || !item.Partial || len(item.Files) != 1 {
		t.Fatalf("running file change = %#v", item)
	}
	if item.Files[0].Path != "src/login.ts" || item.Files[0].Added != 1 || item.Files[0].Removed != 1 {
		t.Fatalf("diff counts = %#v", item.Files[0])
	}
	if !strings.Contains(item.Files[0].Diff, "-const ok = false;") ||
		!strings.Contains(item.Files[0].Diff, "+const ok = true;") {
		t.Fatalf("unified diff = %q", item.Files[0].Diff)
	}

	completed := mapper.Map(update("tool_call_update", map[string]any{
		"toolCallId": "call-1",
		"status":     "completed",
	}))
	if len(completed) != 1 || completed[0].ID != item.ID || completed[0].Seq != item.Seq {
		t.Fatalf("completed update = %#v", completed)
	}
	if completed[0].Partial {
		t.Fatalf("completed tool still partial: %#v", completed[0])
	}
	if got := mapper.Finish(); len(got) != 0 {
		t.Fatalf("Finish duplicated completed tool: %#v", got)
	}
}

func TestTurnMapperFinishClosesPendingTool(t *testing.T) {
	mapper := testMapper()
	items := mapper.Map(update("tool_call", map[string]any{
		"toolCallId": "call-2",
		"title":      "Run tests",
		"kind":       "execute",
		"status":     "in_progress",
		"rawInput":   map[string]any{"command": "go test ./..."},
	}))
	if len(items) != 1 || !items[0].Partial {
		t.Fatalf("pending tool = %#v", items)
	}
	final := mapper.Finish()
	if len(final) != 1 || final[0].ID != items[0].ID || final[0].Partial {
		t.Fatalf("final pending tool = %#v", final)
	}
	if final[0].Command != "go test ./..." {
		t.Fatalf("command = %q", final[0].Command)
	}
}

func TestTurnMapperMapsPlan(t *testing.T) {
	mapper := testMapper()
	items := mapper.Map(update("plan", map[string]any{
		"entries": []any{
			map[string]any{"content": "Inspect files", "status": "completed"},
			map[string]any{"content": "Write fix", "status": "in_progress"},
		},
	}))
	if len(items) != 1 || items[0].Type != "plan" || len(items[0].Plan) != 2 {
		t.Fatalf("plan = %#v", items)
	}
	if items[0].Plan[0].Step != "Inspect files" || items[0].Plan[1].Status != "in_progress" {
		t.Fatalf("plan steps = %#v", items[0].Plan)
	}
}

func TestParseCommandsAcceptsBothShapes(t *testing.T) {
	commands := parseCommands(map[string]any{
		"availableCommands": []any{
			map[string]any{"name": "/review", "description": "Review changes"},
			map[string]any{"command": "test", "argumentHint": "pattern"},
		},
	})
	if len(commands) != 2 {
		t.Fatalf("commands = %#v", commands)
	}
	if commands[0].Name != "review" || commands[1].Name != "test" || commands[1].ArgumentHint != "pattern" {
		t.Fatalf("parsed commands = %#v", commands)
	}
}
