package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

func TestStartSessionBoundsSilentStartup(t *testing.T) {
	installFakeCodex(t, "#!/bin/sh\nexec sleep 30\n")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	_, err := startSession(ctx, agent.StartRequest{Cwd: t.TempDir(), Text: "demo startup"}, 40*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("startup deadline not enforced: duration=%v error=%v", time.Since(start), err)
	}
}

func TestStartSessionDeadlineDoesNotKillStartedTurn(t *testing.T) {
	done := filepath.Join(t.TempDir(), "done")
	t.Setenv("HOMING_DEMO_DONE", done)
	installFakeCodex(t, `#!/bin/sh
if [ "${HOMING_DEMO_WARMUP:-0}" = "1" ]; then
  printf '%s\n' '{"type":"thread.started","thread_id":"demo-warmup"}'
  exit 0
fi
printf '%s\n' '{"type":"thread.started","thread_id":"demo-native-started"}'
sleep 1
printf done > "$HOMING_DEMO_DONE"
`)
	// Warm the newly written script vnode once under a generous 8s ceiling so
	// macOS AMFI/syspolicyd first-exec scans during full-repo -race builds do
	// not consume the short startup timer below.
	t.Setenv("HOMING_DEMO_WARMUP", "1")
	if _, err := startSession(context.Background(), agent.StartRequest{Cwd: t.TempDir(), Text: "warmup"}, 8*time.Second); err != nil {
		t.Fatalf("warmup startSession error=%v", err)
	}
	t.Setenv("HOMING_DEMO_WARMUP", "0")

	result, err := startSession(context.Background(), agent.StartRequest{Cwd: t.TempDir(), Text: "demo startup"}, 400*time.Millisecond)
	if err != nil || result.SessionID != "demo-native-started" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(done); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("startup deadline killed a running turn")
}

func TestStartSessionReadsIdentityBeforeFastExit(t *testing.T) {
	installFakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"demo-native-fast"}'
`)
	for i := 0; i < 20; i++ {
		result, err := startSession(context.Background(), agent.StartRequest{Cwd: t.TempDir(), Text: "demo startup"}, 8*time.Second)
		if err != nil || result.SessionID != "demo-native-fast" {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	}
}
