package searchindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSearchFindsChineseAndEnglish(t *testing.T) {
	ctx := context.Background()
	index, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()

	if err := index.ReplaceSession(ctx, "codex", "demo-session-1", "v1", []Document{
		{ItemID: "item-1", Seq: 1, Role: "user", Body: "修正登入按鈕並重跑完整測試"},
		{ItemID: "item-2", Seq: 2, Role: "assistant", Body: "Fix the Login button and rerun the suite"},
		{ItemID: "tool-1", Seq: 3, Role: "tool", Body: "secret-should-not-be-indexed"},
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		query string
		want  string
	}{
		{"登入", "item-1"},
		{"登入按鈕", "item-1"},
		{"login", "item-2"},
		{"Login button", "item-2"},
	} {
		hits, _, err := index.Search(ctx, test.query, "", 10)
		if err != nil {
			t.Fatalf("%q: %v", test.query, err)
		}
		if len(hits) != 1 || hits[0].ItemID != test.want {
			t.Fatalf("%q hits = %#v", test.query, hits)
		}
	}
}

func TestReplaceSessionRemovesOldRows(t *testing.T) {
	ctx := context.Background()
	index, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	if err := index.ReplaceSession(ctx, "claude", "demo-session-2", "v1", []Document{
		{ItemID: "old", Seq: 1, Role: "user", Body: "舊的內容"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := index.ReplaceSession(ctx, "claude", "demo-session-2", "v2", []Document{
		{ItemID: "new", Seq: 2, Role: "assistant", Body: "新的內容"},
	}); err != nil {
		t.Fatal(err)
	}
	oldHits, _, err := index.Search(ctx, "舊的內容", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldHits) != 0 {
		t.Fatalf("old rows survived replacement: %#v", oldHits)
	}
	newHits, _, err := index.Search(ctx, "新的內容", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(newHits) != 1 || newHits[0].ItemID != "new" {
		t.Fatalf("new hits = %#v", newHits)
	}
}

func TestShortQueryEscapesLikeWildcards(t *testing.T) {
	ctx := context.Background()
	index, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	if err := index.ReplaceSession(ctx, "codex", "demo-session-3", "v1", []Document{
		{ItemID: "a", Seq: 1, Role: "user", Body: "100% complete"},
		{ItemID: "b", Seq: 2, Role: "assistant", Body: "100 complete"},
	}); err != nil {
		t.Fatal(err)
	}
	hits, fallback, err := index.Search(ctx, "0%", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !fallback || len(hits) != 1 || hits[0].ItemID != "a" {
		t.Fatalf("fallback=%v hits=%#v", fallback, hits)
	}
}

func TestDatabaseIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	index, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("mode = %o, want owner-only", info.Mode().Perm())
	}
}
