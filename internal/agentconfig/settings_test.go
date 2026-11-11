package agentconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsDefaultsAndPatch(t *testing.T) {
	dir := t.TempDir()
	store := &Store{DataDir: dir, Home: t.TempDir()}

	claude := store.Read("claude")
	if !claude.ReadSessions {
		t.Fatal("read_sessions should default to true")
	}
	if claude.PushSignal != "hook" || claude.ReplyMode != "auto" {
		t.Fatalf("unexpected defaults: %+v", claude)
	}
	codex := store.Read("codex")
	if codex.PushSignal != "poll" {
		t.Fatalf("codex default signal = %q", codex.PushSignal)
	}

	next, err := store.Patch("codex", map[string]any{
		"push_to_phone": true,
		"push_signal":   "watch",
		"model":         "demo/model-1",
		"order":         float64(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !next.Connected || !next.PushToPhone || next.PushSignal != "watch" {
		t.Fatalf("patch did not apply: %+v", next)
	}
	if next.Model == nil || *next.Model != "demo/model-1" {
		t.Fatalf("model = %#v", next.Model)
	}
	if next.UpdatedAt == nil {
		t.Fatal("updated_at was not set")
	}

	raw, err := os.ReadFile(store.FileFor("codex"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk["push_to_phone"] != true {
		t.Fatalf("disk write: %s", raw)
	}

	off, err := store.Patch("codex", map[string]any{"connected": false})
	if err != nil {
		t.Fatal(err)
	}
	if off.Connected || off.PushToPhone {
		t.Fatalf("disconnect must turn both flags off: %+v", off)
	}
}

func TestSettingsOrderAndSnapshot(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	store := &Store{DataDir: dir, Home: home}
	ids := []string{"codex", "claude", "workbuddy"}

	if _, err := store.SetOrder([]string{"workbuddy", "codex"}); err != nil {
		t.Fatal(err)
	}
	got := store.Ordered(ids)
	want := []string{"workbuddy", "codex", "claude"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}

	// The hook file is in a temp home, never the real ~/.claude.
	settingsFile := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsFile, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"http","url":"http://127.0.0.1:18788/hook/claude"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot([]string{"claude", "codex"})
	if !snapshot["claude"].Connected || snapshot["claude"].UpdatedAt != nil {
		t.Fatalf("hook-derived state = %+v", snapshot["claude"])
	}
}

func TestHookInstallMergesAndBacksUp(t *testing.T) {
	home := t.TempDir()
	store := &Store{DataDir: t.TempDir(), Home: home}
	file := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"theme":"dark","hooks":{"Stop":[{"hooks":[{"type":"http","url":"http://127.0.0.1:9000/existing"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	result := store.InstallHooks("claude", 18788)
	if !result.OK || !result.Changed || result.Backup == "" {
		t.Fatalf("install = %+v", result)
	}
	if _, err := os.Stat(result.Backup); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var merged map[string]any
	if err := json.Unmarshal(raw, &merged); err != nil {
		t.Fatal(err)
	}
	hooks := merged["hooks"].(map[string]any)
	for _, event := range []string{"Stop", "Notification", "PermissionRequest"} {
		list, ok := hooks[event].([]any)
		if !ok || !hookListHasURL(list, "http://127.0.0.1:18788/hook/claude") {
			t.Fatalf("%s not merged: %s", event, raw)
		}
	}
	if merged["theme"] != "dark" {
		t.Fatal("existing settings were not preserved")
	}

	again := store.InstallHooks("claude", 18788)
	if !again.OK || again.Changed {
		t.Fatalf("second install should be a no-op: %+v", again)
	}
}

func TestCodexHookIsManual(t *testing.T) {
	store := &Store{DataDir: t.TempDir(), Home: t.TempDir()}
	result := store.InstallHooks("codex", 18788)
	if result.OK || !result.Manual || result.Snippet == nil {
		t.Fatalf("codex result = %+v", result)
	}
	if result.File != filepath.Join(store.Home, ".codex", "hooks.json") {
		t.Fatalf("file = %q", result.File)
	}
	if _, err := os.Stat(result.File); !os.IsNotExist(err) {
		t.Fatal("codex hooks.json must not be created")
	}
}

func TestEffectiveConnectedDefaultsBuiltinToDetected(t *testing.T) {
	saved := "2026-11-12T00:00:00Z"
	cases := []struct {
		name     string
		row      Settings
		builtin  bool
		detected bool
		want     bool
	}{
		{"builtin detected, never saved", Settings{}, true, true, true},
		{"builtin not installed", Settings{}, true, false, false},
		{"user disconnected explicitly", Settings{UpdatedAt: &saved, Connected: false}, true, true, false},
		{"user connected explicitly", Settings{UpdatedAt: &saved, Connected: true}, false, false, true},
		{"catalog agent without hook", Settings{}, false, true, false},
		{"catalog agent with hook", Settings{Connected: true}, false, true, true},
	}
	for _, tc := range cases {
		if got := EffectiveConnected(tc.row, tc.builtin, tc.detected); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCodexNotifyCountsAsHook(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Store{DataDir: t.TempDir(), Home: home}
	if s.HookInstalled("codex") {
		t.Fatal("no config yet, hook must not be reported")
	}
	config := "model = \"gpt-5\"\nnotify = [\"/Users/me/agent-push-poc/hooks/codex-notify-wrapper.sh\"]\n"
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if !s.HookInstalled("codex") {
		t.Fatal("notify line pointing at codex-notify must count as an installed hook")
	}
	other := "notify = [\"/usr/local/bin/terminal-notifier\"]\n"
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.HookInstalled("codex") {
		t.Fatal("an unrelated notify command is not a Homing hook")
	}
}
