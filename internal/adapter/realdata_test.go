package adapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestRealManifests compares the Go reader against the Node relay's numbers on
// this machine. It is skipped unless HOMING_REAL_MANIFESTS points at a directory
// of manifests, because it reads the user's real agent directories:
//
//	HOMING_REAL_MANIFESTS=../../adapters go test ./internal/adapter/ -run Real -v
//
// The expected counts below are what relay/adapters produced on this Mac on
// 2026-11-09 with limit=200 (Node): codebuddy 3, dsh 7, gemini-cli 0, hermes 0,
// openclaw 12. (An earlier note said 3/3/0/0/3 — that was limit=3 truncation.)
func TestRealManifests(t *testing.T) {
	dir := os.Getenv("HOMING_REAL_MANIFESTS")
	if dir == "" {
		t.Skip("设置 HOMING_REAL_MANIFESTS 才跑（会读真实 agent 目录）")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	agents, problems := LoadManifests(abs)
	if len(problems) > 0 {
		t.Fatalf("清单加载失败: %+v", problems)
	}

	want := map[string]int{
		"codebuddy":  3,
		"dsh":        7,
		"gemini-cli": 0,
		"hermes":     0,
		"openclaw":   12,
	}
	got := map[string]int{}
	for _, a := range agents {
		sessions, err := a.ListSessions(context.Background(), 200)
		if err != nil {
			t.Logf("%s: 读取失败 %v", a.ID(), err)
			continue
		}
		got[a.ID()] = len(sessions)
		t.Logf("%-12s sessions=%d", a.ID(), len(sessions))
		if n, ok := want[a.ID()]; ok && n != len(sessions) {
			t.Errorf("%s: Go 读到 %d 条，Node 版是 %d 条", a.ID(), len(sessions), n)
		}
	}
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Errorf("%s: 清单没有加载出来", id)
		}
	}
}
