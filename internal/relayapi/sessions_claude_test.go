package relayapi

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestStartClaudeSessionProvidesExplicitSessionID(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(root, "args.txt")
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then
  echo "  --session-id <uuid>"
  exit 0
fi
printf '%s\n' "$@" > "$HOMING_TEST_ARGS_FILE"
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOMING_TEST_ARGS_FILE", argsFile)

	result := startClaudeSession(root, "demo task")
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("result=%v", result)
	}
	sessionID, _ := result["session_id"].(string)
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(sessionID) {
		t.Fatalf("session_id=%q", sessionID)
	}

	var raw []byte
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		var err error
		raw, err = os.ReadFile(argsFile)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{"-p", "--session-id", sessionID, "demo task"}
	if len(args) != len(want) {
		t.Fatalf("args=%q want=%q", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args=%q want=%q", args, want)
		}
	}
}

func TestStartClaudeSessionFallsBackWithoutSessionIDFlag(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then
  echo "Claude Code"
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := startClaudeSession(root, "demo task")
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("result=%v", result)
	}
	if result["session_id"] != nil {
		t.Fatalf("session_id=%v, want nil", result["session_id"])
	}
}
