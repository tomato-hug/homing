package driver

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckReadinessRequiresAdvertisedACPFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake probe uses a POSIX shell")
	}
	bin := t.TempDir()
	writeProbe := func(name, help string) {
		script := "#!/bin/sh\nprintf '%s\\n' '" + help + "'\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeProbe("fake-acp-yes", "usage: fake --acp")
	writeProbe("fake-acp-no", "usage: fake --serve")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	spec := Spec{
		ID: "fake-acp-yes", Command: []string{"fake-acp-yes"},
		Dispatch: true, Check: []string{"fake-acp-yes", "--help"},
		CheckRequire: []string{"--acp"},
	}
	ResetReadiness(spec.ID)
	t.Cleanup(func() { ResetReadiness(spec.ID) })
	if got := CheckReadiness(spec); !got.Ready {
		t.Fatalf("ready = false: %+v", got)
	}

	spec.ID = "fake-acp-no"
	spec.Command = []string{"fake-acp-no"}
	spec.Check = []string{"fake-acp-no", "--help"}
	ResetReadiness(spec.ID)
	t.Cleanup(func() { ResetReadiness(spec.ID) })
	if got := CheckReadiness(spec); got.Ready {
		t.Fatalf("missing flag readiness = %+v", got)
	}
}

func TestBuiltInSpecsIncludeBroadACPAgents(t *testing.T) {
	want := []string{
		"hermes", "openclaw", "kimi", "dsh",
		"gemini-cli", "opencode", "cline", "copilot-cli",
		"kiro", "openhands", "goose",
	}
	seen := map[string]bool{}
	for _, spec := range Specs() {
		seen[spec.ID] = true
		if len(spec.Command) == 0 {
			t.Errorf("%s has no argv command", spec.ID)
		}
		if spec.Dispatch && !spec.Verified && !spec.Experimental {
			t.Errorf("%s is unverified but not marked experimental", spec.ID)
		}
	}
	for _, id := range want {
		if !seen[id] {
			t.Errorf("missing ACP spec %q", id)
		}
	}
}

func TestReplaceDynamicSpecsKeepsBuiltinReadiness(t *testing.T) {
	const id = "hermes"
	MarkReadiness(Spec{ID: id, Command: []string{"sh"}}, true, "")
	t.Cleanup(func() { ResetReadiness(id) })

	if err := ReplaceDynamicSpecs(nil); err != nil {
		t.Fatalf("ReplaceDynamicSpecs: %v", err)
	}
	readinessCache.Lock()
	entry, ok := readinessCache.bySpec[id]
	readinessCache.Unlock()
	if !ok || !entry.val.Ready {
		t.Fatalf("built-in readiness was cleared: %+v (present=%v)", entry.val, ok)
	}
}
