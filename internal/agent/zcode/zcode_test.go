package zcode

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestZCodeDetectListAndReadItems(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZCODE_HOME", home)
	dbDir := filepath.Join(home, "cli", "db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "db.sqlite")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
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
		INSERT INTO session_v2 VALUES ('demo-zc-1', 'Demo ZCode Session', '/tmp/demo-zc', 'glm-4.6', '', 1760000010000, 0, 5, 15, 0);
		INSERT INTO session_message VALUES ('z1', 'demo-zc-1', 'user', 1, 1760000009000, '{"text":"测试 ZCode"}');
		INSERT INTO session_message VALUES ('z2', 'demo-zc-1', 'assistant', 2, 1760000010000, '{"content":"ZCode 正常读取"}');
	`)
	_ = db.Close()
	if err != nil {
		t.Fatal(err)
	}

	var a Agent
	ctx := context.Background()
	if !a.Detect(ctx).Installed {
		t.Fatalf("expected ZCode installed")
	}
	sessions, err := a.ListSessions(ctx, 10)
	if err != nil || len(sessions) != 1 || sessions[0].Title != "Demo ZCode Session" {
		t.Fatalf("unexpected sessions: %v %v", sessions, err)
	}
	items, err := a.ReadItems(ctx, "demo-zc-1", 10)
	if err != nil || len(items) != 2 || items[1].Text != "ZCode 正常读取" {
		t.Fatalf("unexpected items: %v %v", items, err)
	}
}
