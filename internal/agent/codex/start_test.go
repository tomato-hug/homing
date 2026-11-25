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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	_, err := startSession(ctx, agent.StartRequest{Cwd: t.TempDir(), Text: "demo startup"}, 40*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("startup deadline not enforced: duration=%v error=%v", time.Since(start), err)
	}
}

func TestStartSessionDeadlineDoesNotKillStartedTurn(t *testing.T) {
	done := filepath.Join(t.TempDir(), "done")
	t.Setenv("HOMING_DEMO_DONE", done)
	installFakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"demo-native-started"}'
sleep 2
printf done > "$HOMING_DEMO_DONE"
`)
	result, err := startSession(context.Background(), agent.StartRequest{Cwd: t.TempDir(), Text: "demo startup"}, time.Second)
	if err != nil || result.SessionID != "demo-native-started" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	deadline := time.Now().Add(3 * time.Second)
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
		result, err := startSession(context.Background(), agent.StartRequest{Cwd: t.TempDir(), Text: "demo startup"}, time.Second)
		if err != nil || result.SessionID != "demo-native-fast" {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	}
}
