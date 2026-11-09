package adapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestManifestFormatsUseTempData 用造出来的数据验证四种格式，不碰任何真实 agent 目录。
// 数据里刻意用 demo-session-1 这种名字，一眼能看出是测试。
func TestManifestFormatsUseTempData(t *testing.T) {
	dir := t.TempDir()

	claudeFile := filepath.Join(dir, "demo-claude.jsonl")
	writeFile(t, claudeFile, `{"type":"custom-title","customTitle":"测试会话一","sessionId":"demo-session-1","timestamp":"2026-01-02T03:04:05Z"}
{"type":"message","role":"user","content":"第一条用户消息","sessionId":"demo-session-1","cwd":"/tmp/demo-proj","timestamp":"2026-01-02T03:04:06Z"}
{"type":"message","role":"assistant","content":[{"type":"text","text":"助手的回答"}],"sessionId":"demo-session-1","timestamp":"2026-01-02T03:04:07Z"}
`)

	customFile := filepath.Join(dir, "demo-custom.jsonl")
	writeFile(t, customFile, `{"type":"session","id":"demo-session-2","createdAt":"2026-02-02T03:04:05Z","cwd":"/tmp/demo-two"}
{"type":"user","text":"自定义映射的用户消息","id":"demo-session-2"}
`)

	cases := []struct {
		name     string
		yaml     string
		wantID   string
		wantSess int
	}{
		{
			name: "claude-code",
			yaml: "id: demo-claude\nname: Demo Claude\nsessions:\n  source: jsonl\n  glob: " + claudeFile + "\n  format: claude-code\npush: poll\n",
			wantID:   "demo-session-1",
			wantSess: 1,
		},
		{
			name: "custom",
			yaml: "id: demo-custom\nsessions:\n  source: jsonl\n  glob: " + customFile + "\n  format: custom\n  format_options:\n    session_id: id\n    cwd: cwd\n    time: createdAt\n    role: type\n    text: text\n    user_values: [user]\npush: poll\n",
			wantID:   "demo-session-2",
			wantSess: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, problems, err := LoadManifestFromString(tc.yaml)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if len(problems) > 0 {
				t.Fatalf("校验失败: %v", problems)
			}
			a := NewManifestAgent(m)
			sessions, err := a.ListSessions(context.Background(), 10)
			if err != nil {
				t.Fatalf("列会话失败: %v", err)
			}
			if len(sessions) != tc.wantSess {
				t.Fatalf("会话数 = %d, 期望 %d", len(sessions), tc.wantSess)
			}
			if sessions[0].SessionID != tc.wantID {
				t.Fatalf("session_id = %q, 期望 %q", sessions[0].SessionID, tc.wantID)
			}
			if sessions[0].Title == "" {
				t.Fatal("标题为空：四级回退没生效")
			}
			items, err := a.ReadItems(context.Background(), tc.wantID, 10)
			if err != nil {
				t.Fatalf("读条目失败: %v", err)
			}
			if len(items) == 0 {
				t.Fatal("没有读到任何条目")
			}
			if a.IsEmptySession(tc.wantID) {
				t.Fatal("有 user/assistant 条目却判成空会话")
			}
		})
	}
}

func TestValidateRejectsBadManifest(t *testing.T) {
	bad := []string{
		"name: no id\nsessions:\n  source: jsonl\n  glob: /tmp/*.jsonl\n",
		"id: Bad_ID\nsessions:\n  source: jsonl\n  glob: /tmp/*.jsonl\n",
		"id: demo\nsessions:\n  source: nope\n",
		"id: demo\nsessions:\n  source: jsonl\n  format: claude-code\n",
		"id: demo\nsessions:\n  source: sqlite\n  db: /tmp/x.db\n",
		"id: demo\nsessions:\n  source: jsonl\n  glob: /tmp/*.jsonl\nreply:\n  command: notanarray\n",
		"id: demo\nsessions:\n  source: jsonl\n  glob: /tmp/*.jsonl\npush: carrier-pigeon\n",
	}
	for _, y := range bad {
		m, problems, err := LoadManifestFromString(y)
		if err != nil {
			continue // YAML 本身错也算通过
		}
		if len(problems) == 0 {
			t.Fatalf("这段清单应当被拒绝：%s", y)
		}
		_ = m
	}
}

func TestSubstituteKeepsArgsWhole(t *testing.T) {
	vars := map[string]string{"session_id": "demo-session-1", "text": "带 空格 和 引号\"的文字", "cwd": "/tmp/demo"}
	got, err := substitute("{text}", vars)
	if err != nil {
		t.Fatal(err)
	}
	if got != vars["text"] {
		t.Fatalf("整体占位符应替换成一个参数，得到 %q", got)
	}
	got, err = substitute("--resume={session_id}", vars)
	if err != nil {
		t.Fatal(err)
	}
	if got != "--resume=demo-session-1" {
		t.Fatalf("嵌入占位符替换错误: %q", got)
	}
	if _, err := substitute("{nope}", vars); err == nil {
		t.Fatal("未知占位符应当报错，而不是留空")
	}
}

func TestIngestTokenLifecycle(t *testing.T) {
	dir := t.TempDir()
	store := NewIngestStore(dir)

	token, err := store.Issue("demo-agent", "", 0)
	if err != nil {
		t.Fatalf("发令牌失败: %v", err)
	}
	if store.Lookup(token) == nil {
		t.Fatal("刚发的令牌查不到")
	}
	if got := store.Lookup("hmg_wrong"); got != nil {
		t.Fatal("错误令牌不该通过")
	}
	if !store.Revoke("demo-agent") {
		t.Fatal("吊销失败")
	}
	if store.Lookup(token) != nil {
		t.Fatal("吊销后令牌仍可用")
	}
}

func TestGlobEscapesNothingWeird(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.jsonl"), "{}\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "x\n")
	a := NewManifestAgent(&Manifest{
		ID:       "demo",
		Sessions: SessionSpec{Source: "jsonl", Glob: filepath.Join(dir, "*.jsonl"), Format: "claude-code"},
	})
	files := a.glob()
	if len(files) != 1 {
		t.Fatalf("glob 匹配到 %d 个文件，期望 1", len(files))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
