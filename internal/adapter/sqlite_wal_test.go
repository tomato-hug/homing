package adapter

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestManifestSQLiteWALModeSessionsAndItems(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	writer, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	// Keep writer open in WAL mode WITHOUT checkpointing so rows live only in state.db-wal!
	if _, err := writer.Exec(`
		PRAGMA journal_mode = WAL;
		CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT, model TEXT, started_at REAL, message_count INTEGER);
		CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, timestamp REAL);
		INSERT INTO sessions VALUES ('demo-hermes-1', 'WAL 会话标题', 'deepseek-chat', 1760000100, 2);
		INSERT INTO messages VALUES (1, 'demo-hermes-1', 'user', '第一条提问', 1760000101);
		INSERT INTO messages VALUES (2, 'demo-hermes-1', 'assistant', 'WAL 回复正文', 1760000102);
	`); err != nil {
		t.Fatal(err)
	}

	m := &Manifest{
		ID:   "hermes",
		Name: "Hermes Agent",
		Sessions: SessionSpec{
			Source:     "sqlite",
			DB:         dbPath,
			Query:      "select id as session_id, title, model, started_at as last_ts from sessions order by started_at desc",
			ItemsQuery: "select id, role, content as text, timestamp as ts from messages where session_id = ? order by id desc limit ?",
		},
	}
	ma := NewManifestAgent(m)
	ctx := context.Background()
	sessions, err := ma.ListSessions(ctx, 10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("expected 1 WAL session, got %v (err=%v)", sessions, err)
	}
	if sessions[0].Title != "WAL 会话标题" {
		t.Fatalf("expected WAL title, got %q", sessions[0].Title)
	}
	meta := ma.Meta("demo-hermes-1", "")
	if meta.NativeTitle != "WAL 会话标题" {
		t.Fatalf("expected Meta NativeTitle from SQLite, got %+v", meta)
	}
	items, err := ma.ReadItems(ctx, "demo-hermes-1", 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("expected 2 WAL items, got %v (err=%v)", items, err)
	}
	if items[0].Role != "user" || items[1].Text != "WAL 回复正文" {
		t.Fatalf("expected chronological items, got %+v", items)
	}
}
