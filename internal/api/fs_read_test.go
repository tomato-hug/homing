package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomato-hug/homing/internal/store"
)

func TestFSReadServesProjectTextAndRejectsUnsafePaths(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "src", "demo.go"), []byte("package demo\n\nfunc Hello() string { return \"hi\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("DEMO_SECRET=not-real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "blob.bin"), []byte{0x00, 0x01, 0x02, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "other.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "other.txt"), filepath.Join(project, "escape.txt")); err != nil {
		t.Fatal(err)
	}

	deps := &Deps{DataDir: t.TempDir(), Version: "test"}
	t.Cleanup(func() { _ = deps.Close() })
	s, err := deps.Store()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-fs-read-1", Cwd: project, Title: "demo", Text: "demo",
	}); err != nil {
		t.Fatal(err)
	}
	mux := NewMux(deps)
	get := func(path, cwd string) (int, map[string]any) {
		q := url.Values{"path": {path}}
		if cwd != "" {
			q.Set("cwd", cwd)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/fs/read?"+q.Encode(), nil)
		req.Header.Set("Authorization", "Bearer "+s.Token())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}

	code, body := get("src/demo.go:3", project)
	if code != http.StatusOK {
		t.Fatalf("relative read status=%d body=%v", code, body)
	}
	if body["name"] != "demo.go" || body["language"] != "go" || body["line"].(float64) != 3 {
		t.Fatalf("unexpected read body: %v", body)
	}
	if code, _ := get(filepath.Join(project, "src", "demo.go"), ""); code != http.StatusOK {
		t.Fatalf("absolute read status=%d", code)
	}
	if code, _ := get(filepath.Join(outside, "other.txt"), ""); code != http.StatusForbidden {
		t.Fatalf("outside read status=%d, want 403", code)
	}
	if code, _ := get("escape.txt", project); code != http.StatusForbidden {
		t.Fatalf("symlink escape status=%d, want 403", code)
	}
	if code, _ := get("../"+filepath.Base(outside)+"/other.txt", project); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("dot-dot escape status=%d, want 403/404", code)
	}
	if code, _ := get(".env", project); code != http.StatusForbidden {
		t.Fatalf(".env status=%d, want 403", code)
	}
	if code, _ := get("blob.bin", project); code != http.StatusUnsupportedMediaType {
		t.Fatalf("binary status=%d, want 415", code)
	}
}

func TestSplitPathLineSuffix(t *testing.T) {
	cases := map[string]struct {
		path string
		line int
	}{
		"src/a.go":      {"src/a.go", 0},
		"src/a.go:12":   {"src/a.go", 12},
		"src/a.go:12:4": {"src/a.go", 12},
		"C:/demo/a.go":  {"C:/demo/a.go", 0},
		"README.md:0":   {"README.md:0", 0},
	}
	for input, want := range cases {
		path, line := splitPathLineSuffix(input)
		if path != want.path || line != want.line {
			t.Fatalf("splitPathLineSuffix(%q) = (%q,%d), want (%q,%d)", input, path, line, want.path, want.line)
		}
	}
}
