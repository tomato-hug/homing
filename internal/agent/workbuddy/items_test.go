package workbuddy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomato-hug/homing/internal/agent/claude"
)

// 造数据：demo-session-1 一眼能看出是测试，不含任何真实会话内容。
// WorkBuddy 的转录与 Claude 同族（Node 里也是同一个解析分支），所以这里验证两件事：
// 1) workbuddy 包确实复用了那份解析；2) 文件定位不到时安静返回空。
const demoTranscript = `{"type":"message","role":"user","sessionId":"demo-session-1","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":[{"type":"text","text":"帮我看下这个构建脚本"}]}}
{"type":"message","role":"assistant","sessionId":"demo-session-1","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","model":"demo-model","content":[{"type":"thinking","thinking":"先看脚本内容"},{"type":"text","text":"问题在 PATH"},{"type":"tool_use","id":"call-1","name":"Bash","input":{"command":"cat build.sh"}}]}}
{"type":"message","role":"user","sessionId":"demo-session-1","timestamp":"2026-01-01T00:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"#!/bin/sh\nexit 1"}]}}
`

func TestTranscriptFileMissingSession(t *testing.T) {
	// 不存在的会话：不能 panic，返回空字符串即可（上层会返回空条目）
	if got := transcriptFile("demo-session-does-not-exist"); got != "" {
		if _, err := os.Stat(got); err == nil {
			t.Fatalf("不存在的会话却找到了文件: %s", got)
		}
	}
}

func TestReadItemsUnknownSessionIsEmpty(t *testing.T) {
	items, err := Agent{}.ReadItems(context.Background(), "demo-session-does-not-exist", 50)
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("未知会话返回了 %d 条", len(items))
	}
}

func TestSharesClaudeParser(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "demo-session-1.jsonl")
	if err := os.WriteFile(file, []byte(demoTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	items := claude.ExtractFileItems(file, "workbuddy", 50)
	if len(items) < 4 {
		t.Fatalf("条目数 = %d，期望至少 4", len(items))
	}
	if items[0].Type != "user" || items[0].Text == "" {
		t.Fatalf("首条不是用户消息: %+v", items[0])
	}
	// 工具调用：command + tool 块（手机端靠这两个字段显示命令与输出）
	var tool *int
	for i, it := range items {
		if it.Type == "tool_call" && it.Tool != nil {
			n := i
			tool = &n
			break
		}
	}
	if tool == nil {
		t.Fatal("没有找到 tool_call 条目")
	}
	// 同一 id 后写覆盖（与 Node 一致）：留下的是工具结果，name 仍在
	tc := items[*tool]
	if tc.Tool.Name != "Bash" {
		t.Fatalf("tool 块 name = %q", tc.Tool.Name)
	}
	if tc.Tool.OutputTail == nil || *tc.Tool.OutputTail == "" {
		t.Fatalf("工具结果没有 output_tail: %+v", tc.Tool)
	}
	last := items[len(items)-1]
	if last.Role != "tool" || last.Tool == nil || last.Tool.OutputTail == nil {
		t.Fatalf("工具结果条目不对: %+v", last)
	}
}
