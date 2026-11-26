package opencode

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
	"github.com/tomato-hug/homing/internal/agent"
)

func createSyntheticOpenCodeDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE session_v2 (
			id TEXT PRIMARY KEY,
			title TEXT,
			directory TEXT,
			model TEXT,
			parent_id TEXT,
			time_updated INTEGER,
			time_archived INTEGER,
			tokens_input INTEGER,
			tokens_output INTEGER,
			cost REAL
		);
		CREATE TABLE session_message (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			type TEXT,
			seq INTEGER,
			time_created INTEGER,
			data TEXT
		);
		CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO session_v2 VALUES ('demo-oc-1', 'Demo OpenCode Session', '/tmp/demo-oc', 'openai/gpt-5', '', 1760000002000, 0, 10, 20, 0.01);
		INSERT INTO session_message VALUES ('m1', 'demo-oc-1', 'user', 1, 1760000001000, '{"text":"请检查构建"}');
		INSERT INTO session_message VALUES ('m2', 'demo-oc-1', 'assistant', 2, 1760000002000, '{"content":[{"type":"text","text":"构建全部通过"}]}');
	`)
	if err != nil {
		t.Fatalf("seed sqlite: %v", err)
	}
}

func TestOpenCodeSessionsReadItemsAndServeReply(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")
	createSyntheticOpenCodeDB(t, dbPath)
	t.Setenv("OPENCODE_DB", dbPath)

	var gotPrompt bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/global/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"0.2.0-test"}`))
		case "/session/demo-oc-1/prompt_async":
			gotPrompt = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("OPENCODE_SERVER_URL", srv.URL)
	probeServe(true)
	t.Cleanup(func() {
		t.Setenv("OPENCODE_SERVER_URL", "http://127.0.0.1:1")
		probeServe(true)
	})

	var a Agent
	ctx := context.Background()
	det := a.Detect(ctx)
	if !det.Installed {
		t.Fatalf("expected OpenCode installed")
	}
	if !a.Capabilities().Reply {
		t.Fatalf("expected OpenCode Reply capability true when serve is healthy")
	}
	sessions, err := a.ListSessions(ctx, 10)
	if err != nil || len(sessions) != 1 || sessions[0].SessionID != "demo-oc-1" {
		t.Fatalf("unexpected sessions: %v %v", sessions, err)
	}
	items, err := a.ReadItems(ctx, "demo-oc-1", 10)
	if err != nil || len(items) != 2 || items[1].Text != "构建全部通过" {
		t.Fatalf("unexpected items: %v %v", items, err)
	}
	if err := a.Reply(ctx, "demo-oc-1", "继续下一步", agent.ReplyOpts{}); err != nil {
		t.Fatalf("Reply failed: %v", err)
	}
	if !gotPrompt {
		t.Fatalf("expected prompt_async endpoint to be called")
	}
}
