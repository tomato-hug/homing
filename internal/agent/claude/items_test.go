package claude

import (
	"os"
	"path/filepath"
	"testing"
)

// 造数据：demo-session-1 一眼能看出是测试，不含任何真实会话内容。
const demoTranscript = `{"type":"message","role":"user","sessionId":"demo-session-1","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":[{"type":"text","text":"请解释这段代码"}]}}
{"type":"message","role":"assistant","sessionId":"demo-session-1","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","model":"demo-model","content":[{"type":"thinking","thinking":"先看结构"},{"type":"text","text":"这段代码做了三件事"},{"type":"tool_use","id":"call-1","name":"Read","input":{"file_path":"/tmp/demo.go"}}]}}
{"type":"message","role":"user","sessionId":"demo-session-1","timestamp":"2026-01-01T00:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"package demo"}]}}
`

func TestExtractFileItemsDemo(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "demo-session-1.jsonl")
	if err := os.WriteFile(file, []byte(demoTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	items := ExtractFileItems(file, "claude", 50)
	if len(items) < 4 {
		t.Fatalf("条目数 = %d，期望至少 4", len(items))
	}

	kinds := make([]string, 0, len(items))
	for _, it := range items {
		kinds = append(kinds, it.Type)
		if it.ID == "" || it.Seq == 0 {
			t.Fatalf("条目缺少 id/seq: %+v", it)
		}
	}
	want := []string{"user", "reasoning", "assistant", "tool_call"}
	for i, w := range want {
		if kinds[i] != w {
			t.Fatalf("第 %d 条 kind = %q，期望 %q（全部：%v）", i, kinds[i], w, kinds)
		}
	}
	// tool_result 复用 tool_use 的 id，后写覆盖
	last := items[len(items)-1]
	if last.ID != "claude-tool-call-1" || last.Role != "tool" {
		t.Fatalf("tool_result 没有覆盖到同名条目: %+v", last)
	}
}

func TestExtractFileItemsLimit(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "demo-session-2.jsonl")
	if err := os.WriteFile(file, []byte(demoTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	items := ExtractFileItems(file, "claude", 2)
	if len(items) != 2 {
		t.Fatalf("limit=2 时返回 %d 条", len(items))
	}
	// tool_use / tool_result 沿用当前 seq（与 Node 一致），所以这里只验证留下的是尾部
	if items[len(items)-1].ID != "claude-tool-call-1" {
		t.Fatalf("limit=2 应留下最后两条，实际最后一条是 %q", items[len(items)-1].ID)
	}
}
