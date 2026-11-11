package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomato-hug/homing/internal/agent"
)

func demoRow(payload map[string]any) map[string]any {
	return map[string]any{
		"timestamp": "2026-01-01T00:00:00Z",
		"payload":   payload,
	}
}

func TestApplyPatchBecomesFileChange(t *testing.T) {
	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: demo/a.go",
		"+package demo",
		"+// hello",
		"*** Update File: demo/b.go",
		"@@",
		"-old line",
		"+new line",
		"*** End Patch",
	}, "\n")
	st := &codexItems{calls: map[string]string{}, byCall: map[string]*codexCall{}}

	items := st.row(demoRow(map[string]any{
		"type":    "custom_tool_call",
		"call_id": "demo-call-1",
		"name":    "apply_patch",
		"input":   patch,
	}))
	if len(items) != 1 {
		t.Fatalf("apply_patch produced %d items, want 1", len(items))
	}
	call := items[0]
	if call.Type != "file_change" || call.Role != "assistant" {
		t.Fatalf("type/role = %q/%q, want file_change/assistant", call.Type, call.Role)
	}
	if call.Tool == nil || call.Tool.Name != "apply_patch" || call.Tool.InputSummary == nil || *call.Tool.InputSummary != "apply_patch" {
		t.Fatalf("tool block = %+v", call.Tool)
	}
	if len(call.Files) != 2 {
		t.Fatalf("files = %+v, want 2", call.Files)
	}
	if call.Files[0].Path != "demo/a.go" || call.Files[0].Added != 2 || call.Files[0].Removed != 0 {
		t.Fatalf("first file = %+v", call.Files[0])
	}
	if call.Files[1].Path != "demo/b.go" || call.Files[1].Added != 1 || call.Files[1].Removed != 1 {
		t.Fatalf("second file = %+v", call.Files[1])
	}
	if strings.Contains(call.Files[1].Diff, "*** End Patch") {
		t.Fatalf("diff kept the end marker: %q", call.Files[1].Diff)
	}

	output := st.row(demoRow(map[string]any{
		"type":    "custom_tool_call_output",
		"call_id": "demo-call-1",
		"output":  "Exit code: 0\nDone",
	}))
	if len(output) != 1 {
		t.Fatalf("apply_patch output produced %d items, want 1", len(output))
	}
	result := output[0]
	if result.Type != "file_change" || len(result.Files) != 2 {
		t.Fatalf("result type/files = %q/%+v", result.Type, result.Files)
	}
	if result.Tool == nil || result.Tool.ExitCode == nil || *result.Tool.ExitCode != 0 || result.Tool.Status != "done" {
		t.Fatalf("result tool = %+v", result.Tool)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("top-level exit_code = %v", result.ExitCode)
	}

	merged := agent.MergeItem(call, result)
	if merged.Type != "file_change" || len(merged.Files) != 2 {
		t.Fatalf("merged type/files lost: %q/%+v", merged.Type, merged.Files)
	}
	if merged.Tool == nil || merged.Tool.InputSummary == nil || *merged.Tool.InputSummary != "apply_patch" {
		t.Fatalf("merged tool lost the call's input summary: %+v", merged.Tool)
	}
}

func TestCustomToolPlanOnlyForUpdatePlan(t *testing.T) {
	input := `{"steps":[{"step":"demo step","status":"pending"}]}`
	st := &codexItems{calls: map[string]string{}, byCall: map[string]*codexCall{}}

	plan := st.row(demoRow(map[string]any{
		"type":    "custom_tool_call",
		"call_id": "demo-plan",
		"name":    "update_plan",
		"input":   input,
	}))
	if len(plan) != 1 || plan[0].Type != "plan" || len(plan[0].Plan) != 1 {
		t.Fatalf("update_plan = %+v", plan)
	}

	tool := st.row(demoRow(map[string]any{
		"type":    "custom_tool_call",
		"call_id": "demo-tool",
		"name":    "mcp__demo",
		"input":   input,
	}))
	if len(tool) != 1 || tool[0].Type != "tool_call" || len(tool[0].Plan) != 0 {
		t.Fatalf("non-plan tool was misclassified: %+v", tool)
	}

	empty := st.row(demoRow(map[string]any{
		"type":    "custom_tool_call",
		"call_id": "demo-empty-plan",
		"name":    "update_plan",
		"input":   "not json",
	}))
	if len(empty) != 1 || empty[0].Type != "plan" {
		t.Fatalf("update_plan with unparsable input = %+v", empty)
	}
}

func TestDeveloperMessagesAreSkipped(t *testing.T) {
	st := &codexItems{calls: map[string]string{}, byCall: map[string]*codexCall{}}
	developer := st.row(demoRow(map[string]any{
		"type":    "message",
		"role":    "developer",
		"content": "You are the demo agent. This is injected context.",
	}))
	if len(developer) != 0 {
		t.Fatalf("developer message was shown: %+v", developer)
	}

	userTag := st.row(demoRow(map[string]any{
		"type":    "message",
		"role":    "user",
		"content": "<demo-tag>a user really typed this</demo-tag>",
	}))
	if len(userTag) != 1 || userTag[0].Type != "user" {
		t.Fatalf("user text starting with a tag was dropped: %+v", userTag)
	}

	agents := st.row(demoRow(map[string]any{
		"type":    "message",
		"role":    "user",
		"content": "# AGENTS.md instructions for /tmp/demo",
	}))
	if len(agents) != 0 {
		t.Fatalf("AGENTS.md instructions were shown: %+v", agents)
	}
}

func TestRolloutMetaTracksLastUserTurn(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rollout-demo.jsonl")
	rows := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:01Z","type":"session_meta","payload":{"cwd":"/tmp/demo-project"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","payload":{"type":"message","role":"user","content":"demo user request"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","payload":{"type":"message","role":"assistant","content":"demo answer"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(file, []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := readRolloutMeta(file)
	if meta.lastUserTS != "2026-01-01T00:00:02Z" {
		t.Fatalf("lastUserTS = %q, want the user turn", meta.lastUserTS)
	}
	if meta.lastTs != "2026-01-01T00:00:03Z" {
		t.Fatalf("lastTs = %q, want the assistant turn", meta.lastTs)
	}
}

func TestRolloutMetaCacheInvalidatesOnFileChange(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rollout-demo-cache.jsonl")
	firstRows := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:01Z","type":"session_meta","payload":{"cwd":"/tmp/demo-project"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","payload":{"type":"message","role":"assistant","content":"first answer"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(file, []byte(firstRows), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readRolloutMetaCached(file); got.lastPreview != "first answer" {
		t.Fatalf("first preview = %q", got.lastPreview)
	}

	secondRows := firstRows +
		`{"timestamp":"2026-01-01T00:00:03Z","payload":{"type":"message","role":"assistant","content":"second answer"}}` + "\n"
	if err := os.WriteFile(file, []byte(secondRows), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readRolloutMetaCached(file); got.lastPreview != "second answer" {
		t.Fatalf("cached preview did not invalidate: %q", got.lastPreview)
	}
}

func TestTitleIndexCacheInvalidatesOnFileChange(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	indexDir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(indexDir, 0o700); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(indexDir, "session_index.jsonl")
	first := `{"id":"demo-thread-1","thread_name":"first title","updated_at":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(index, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}
	titleIndexCache.Lock()
	titleIndexCache.entry = titleIndexEntry{}
	titleIndexCache.Unlock()

	titles, _ := titleIndexFull()
	if titles["demo-thread-1"] != "first title" {
		t.Fatalf("first titles = %#v", titles)
	}
	second := `{"id":"demo-thread-2","thread_name":"second title","updated_at":"2026-01-02T00:00:00Z"}` + "\n"
	if err := os.WriteFile(index, []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	titles, _ = titleIndexFull()
	if titles["demo-thread-2"] != "second title" {
		t.Fatalf("cached title index did not invalidate: %#v", titles)
	}
}
