package kimi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestKimiCodeSessionsAndItems(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", root)
	sessionDir := filepath.Join(root, "sessions", "bucket-demo", "ses_demo")
	wireDir := filepath.Join(sessionDir, "agents", "main")
	metaDir := filepath.Join(sessionDir, "session-meta")
	if err := os.MkdirAll(wireDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(metaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := `{
  "id": "ses_demo",
  "title": "核对发布清单",
  "titleKind": "generated",
  "lastPrompt": "核对发布清单",
  "cwd": "/tmp/demo-project",
  "createdAt": 1790000000000,
  "updatedAt": 1790000005000,
  "archived": false
}`
	if err := os.WriteFile(filepath.Join(metaDir, "state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	wire := `{"type":"metadata","protocol_version":"1.10"}
{"timestamp":1790000001.0,"message":{"type":"TurnBegin","payload":{"user_input":"核对发布清单"}}}
{"timestamp":1790000002.0,"message":{"type":"ContentPart","payload":{"type":"think","think":"internal"}}}
{"timestamp":1790000003.0,"message":{"type":"ContentPart","payload":{"type":"text","text":"清单核对完成。"}}}
`
	if err := os.WriteFile(filepath.Join(wireDir, "wire.jsonl"), []byte(wire), 0o600); err != nil {
		t.Fatal(err)
	}

	a := Agent{}
	det := a.Detect(context.Background())
	if !det.Installed {
		t.Fatal("Kimi Code should be detected")
	}
	sessions, err := a.ListSessions(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d", len(sessions))
	}
	if sessions[0].SessionID != "ses_demo" || sessions[0].Title != "核对发布清单" {
		t.Fatalf("session = %+v", sessions[0])
	}
	if sessions[0].Cwd != "/tmp/demo-project" {
		t.Fatalf("cwd = %q", sessions[0].Cwd)
	}
	items, err := a.ReadItems(context.Background(), "ses_demo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Role != "user" || items[1].Role != "assistant" {
		t.Fatalf("items = %#v", items)
	}
	if a.IsEmptySession("ses_demo") {
		t.Fatal("session with a prompt should not be empty")
	}
}

func TestKimiLegacyStateFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", root)
	sessionDir := filepath.Join(root, "sessions", "bucket-demo", "uuid-demo")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), []byte(`{"custom_title":"旧版会话","archived":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "wire.jsonl"), []byte(`{"timestamp":1790000001.0,"message":{"type":"TurnBegin","payload":{"user_input":"旧版问题"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sessions, err := (Agent{}).ListSessions(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Title != "旧版会话" || sessions[0].NativeState != "archived" {
		t.Fatalf("sessions = %+v", sessions)
	}
}
