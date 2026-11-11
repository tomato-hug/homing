package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/api"
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

	jsonFile := filepath.Join(dir, "demo-session-3.json")
	writeFile(t, jsonFile, `{
  "sessionId": "demo-session-3",
  "cwd": "/tmp/demo-three",
  "updatedAt": "2026-03-02T03:04:05Z",
  "messages": [
    {"role": "user", "content": "JSON 数组里的用户消息", "timestamp": "2026-03-02T03:04:05Z"},
    {"role": "assistant", "content": "JSON 数组里的助手消息", "timestamp": "2026-03-02T03:04:06Z"}
  ]
}`)

	cases := []struct {
		name     string
		yaml     string
		wantID   string
		wantSess int
	}{
		{
			name:     "claude-code",
			yaml:     "id: demo-claude\nname: Demo Claude\nsessions:\n  source: jsonl\n  glob: " + claudeFile + "\n  format: claude-code\npush: poll\n",
			wantID:   "demo-session-1",
			wantSess: 1,
		},
		{
			name:     "custom",
			yaml:     "id: demo-custom\nsessions:\n  source: jsonl\n  glob: " + customFile + "\n  format: custom\n  format_options:\n    session_id: id\n    cwd: cwd\n    time: createdAt\n    role: type\n    text: text\n    user_values: [user]\npush: poll\n",
			wantID:   "demo-session-2",
			wantSess: 1,
		},
		{
			name:     "generic-json",
			yaml:     "id: demo-json\nsessions:\n  source: json\n  glob: " + jsonFile + "\n  format: generic-json\npush: poll\n",
			wantID:   "demo-session-3",
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

func TestNestedAgentFormats(t *testing.T) {
	dir := t.TempDir()

	dshFile := filepath.Join(dir, "session.v4.jsonl")
	writeFile(t, dshFile, `{"type":"session","id":"demo-session-dsh","createdAt":1790000000000,"cwd":"/tmp/demo-dsh"}
{"type":"user/message","time":1790000001000,"data":{"role":"user","source":{"kind":"runtime-context"},"content":[{"type":"text","text":"Current runtime context. Ignore me."}]}}
{"type":"user/message","time":1790000002000,"data":{"role":"user","source":{"kind":"user"},"content":[{"type":"text","text":"请检查发布配置"}]}}
{"type":"session/title","time":1790000002001,"data":{"title":"发布配置检查","source":{"kind":"provider"}}}
{"type":"assistant/message","time":1790000003000,"data":{"message":{"role":"assistant","source":{"model":"deepseek-v4"},"content":[{"type":"reasoning","text":"hidden"},{"type":"text","text":"检查完成，没有问题。"}]}}}
`)

	openclawFile := filepath.Join(dir, "demo-openclaw.jsonl")
	writeFile(t, openclawFile, `{"type":"session","version":3,"id":"demo-session-openclaw","timestamp":"2026-07-28T14:01:50.145Z","cwd":"/tmp/demo-openclaw"}
{"type":"model_change","timestamp":"2026-07-28T14:01:50.149Z","provider":"demo","modelId":"gpt-demo"}
{"type":"message","id":"m1","timestamp":"2026-07-28T14:02:00.000Z","message":{"role":"user","content":[{"type":"text","text":"[Tue 2026-07-28 22:01 GMT+8] 只回复收到"}]}}
{"type":"message","id":"m2","timestamp":"2026-07-28T14:02:01.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"收到。"}]}}
`)

	cases := []struct {
		name   string
		format string
		file   string
	}{
		{name: "dsh", format: "dsh", file: dshFile},
		{name: "openclaw", format: "openclaw", file: openclawFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := FormatFor(tc.format, nil)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := f.ListFiles([]string{tc.file})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 {
				t.Fatalf("rows = %d", len(rows))
			}
			if rows[0].Cwd == "" || rows[0].LastTS == "" {
				t.Fatalf("missing metadata: %+v", rows[0])
			}
			items, err := f.ReadItems(tc.file, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 2 {
				t.Fatalf("items = %#v", items)
			}
			if items[0].Role != "user" || items[1].Role != "assistant" {
				t.Fatalf("roles = %q / %q", items[0].Role, items[1].Role)
			}
			if items[0].Text == "" || items[1].Text == "" {
				t.Fatalf("empty text: %#v", items)
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

func TestIngestReplyCallback(t *testing.T) {
	received := make(chan map[string]string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]string
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			t.Errorf("callback body: %v", err)
			return
		}
		received <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	store := NewIngestStore(t.TempDir())
	if _, err := store.Issue("demo-agent", server.URL, 1000); err != nil {
		t.Fatal(err)
	}
	channel := store.ReplyChannel("demo-agent")
	if channel == nil {
		t.Fatal("reply channel not registered")
	}
	status, message, handled := deliverIngestReply(
		context.Background(), channel, "demo-agent", "demo-session-1", "示例：继续",
	)
	if !handled || status != "injected" || message == "" {
		t.Fatalf("status=%q message=%q handled=%v", status, message, handled)
	}
	select {
	case body := <-received:
		if body["agent"] != "demo-agent" || body["session_id"] != "demo-session-1" ||
			body["text"] != "示例：继续" {
			t.Fatalf("callback body = %#v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback was not delivered")
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

func TestConsoleAdapterContractAndObjectSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := t.TempDir()
	writeFile(t, filepath.Join(dataDir, "config.json"), `{"token":"demo-token"}`)
	sessionFile := filepath.Join(t.TempDir(), "demo-session.jsonl")
	writeFile(t, sessionFile, `{"type":"message","role":"user","content":"demo","sessionId":"demo-session-1"}`+"\n")
	invalidate()

	mux := api.NewMux(&api.Deps{DataDir: dataDir})
	get := func(target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, bytes.NewReader([]byte(body)))
		req.RemoteAddr = "127.0.0.1:23456"
		req.Header.Set("Authorization", "Bearer demo-token")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	post := func(target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader([]byte(body)))
		req.RemoteAddr = "127.0.0.1:23456"
		req.Header.Set("Authorization", "Bearer demo-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	unauthorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18788/api/adapters", nil)
	req.RemoteAddr = "127.0.0.1:23456"
	mux.ServeHTTP(unauthorized, req)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	rec := get("http://127.0.0.1:18788/api/adapters", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("adapters status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	builtin, _ := payload["builtin"].([]any)
	if len(builtin) != 5 {
		t.Fatalf("builtin = %#v", payload["builtin"])
	}
	if payload["placeholder"] != false {
		t.Fatalf("placeholder = %#v", payload["placeholder"])
	}
	formats, _ := payload["formats"].([]any)
	formatSet := map[string]bool{}
	for _, format := range formats {
		formatSet[fmt.Sprint(format)] = true
	}
	if !formatSet["jsonl"] || !formatSet["json"] || !formatSet["sqlite"] {
		t.Fatalf("formats = %#v", payload["formats"])
	}
	if formatSet["markdown"] || formatSet["log"] {
		t.Fatalf("formats advertises an unsupported reader: %#v", payload["formats"])
	}

	body, _ := json.Marshal(map[string]any{
		"adapter": map[string]any{
			"id": "demo-agent", "name": "Demo Agent",
			"source": map[string]any{"kind": "file", "path": sessionFile, "format": "jsonl"},
			"push":   map[string]any{"method": "poll"},
			"reply":  map[string]any{"mode": "none", "command": ""},
		},
	})
	saved := post("http://127.0.0.1:18788/api/adapters", string(body))
	if saved.Code != http.StatusOK {
		t.Fatalf("save status = %d, body = %s", saved.Code, saved.Body.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".homing", "agents", "demo-agent.yaml")); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}

	testRead := post("http://127.0.0.1:18788/api/adapters/test",
		`{"path":`+jsonString(sessionFile)+`,"format":"jsonl"}`)
	if testRead.Code != http.StatusOK {
		t.Fatalf("test status = %d, body = %s", testRead.Code, testRead.Body.String())
	}
	var sample map[string]any
	if err := json.Unmarshal(testRead.Body.Bytes(), &sample); err != nil {
		t.Fatal(err)
	}
	if sample["ok"] != true || sample["file_count"] != float64(1) {
		t.Fatalf("test read = %+v", sample)
	}
}

func jsonString(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
