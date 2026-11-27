package relayapi

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/store"
)

func newIsolatedDeps(t *testing.T) (*api.Deps, *store.Store) {
	t.Helper()
	deps := &api.Deps{
		Log:     slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1})),
		DataDir: t.TempDir(),
		Version: "test",
	}
	st, err := deps.Store()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = deps.Close() })
	return deps, st
}

func TestStartClaudeSessionRegistersProcAndStopsCleanly(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readyFile := filepath.Join(root, "ready.txt")
	exitedFile := filepath.Join(root, "exited.txt")
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then
  echo "  --session-id <uuid>"
  exit 0
fi
trap 'echo stopped > "$HOMING_TEST_EXITED_FILE"; exit 0' INT TERM
echo ready > "$HOMING_TEST_READY_FILE"
sleep 2
echo natural > "$HOMING_TEST_EXITED_FILE"
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOMING_TEST_READY_FILE", readyFile)
	t.Setenv("HOMING_TEST_EXITED_FILE", exitedFile)

	result := startClaudeSession(root, "demo cancellable task")
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("startClaudeSession result = %v", result)
	}
	sessionID, _ := result["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected non-empty session_id, got %v", result)
	}

	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !stopManagedSession(sessionID) {
		t.Fatalf("stopManagedSession(%q) = false, want true for tracked claude child process", sessionID)
	}

	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if _, err := os.Stat(exitedFile); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	launches.mu.Lock()
	_, stillTracked := launches.procs[sessionID]
	launches.mu.Unlock()
	if stillTracked {
		t.Fatalf("launches.procs still retained stopped session %q", sessionID)
	}
}

func TestClaudeSupportsSessionIDEnforcesTimeout(t *testing.T) {
	root := t.TempDir()
	slowBin := filepath.Join(root, "claude-hang")
	script := `#!/bin/sh
sleep 1
echo "  --session-id <uuid>"
`
	if err := os.WriteFile(slowBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_HELP_TIMEOUT_MS", "60")

	started := time.Now()
	supported := claudeSupportsSessionID(slowBin)
	elapsed := time.Since(started)
	if supported {
		t.Fatal("timed-out --help probe reported supported=true")
	}
	if elapsed > 450*time.Millisecond {
		t.Fatalf("claudeSupportsSessionID hung for %s, want timeout near 60ms", elapsed)
	}
	if _, cached := claudeSessionIDSupport.Load(slowBin); cached {
		t.Fatal("timed-out probe should not permanently cache false in claudeSessionIDSupport")
	}
}

func TestPendingLaunchesExpireAndBoundCapacityWithoutHijacking(t *testing.T) {
	deps, st := newIsolatedDeps(t)
	cwd := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		launches.mu.Lock()
		clear(launches.pending)
		launches.mu.Unlock()
	})

	if _, err := st.SaveDispatch(store.Dispatch{
		ID:              "dsp_stale_1",
		ParentSessionID: "demo-parent",
		ParentSource:    "codex",
		TargetAgent:     "claude",
		RequestID:       "req_stale_2h",
		Cwd:             cwd,
		Text:            "過期派發單",
		Status:          "waiting_session",
	}); err != nil {
		t.Fatal(err)
	}

	launches.mu.Lock()
	clear(launches.pending)
	launches.pending["req_stale_2h"] = launchInfo{
		source: "claude",
		cwd:    cwd,
		at:     time.Now().Add(-2 * time.Hour),
	}
	for i := 0; i < 200; i++ {
		launches.pending["req_bulk_"+strconv.Itoa(i)] = launchInfo{
			source: "codex",
			cwd:    cwd,
			at:     time.Now().Add(time.Duration(i) * time.Millisecond),
		}
	}
	launches.mu.Unlock()

	claimLaunch(deps, store.Event{
		ID:        1,
		Source:    "claude",
		Kind:      "reply",
		SessionID: "demo-manual-fresh",
		Cwd:       cwd,
		Text:      "手動開啟的新會話",
	})

	dispatches := st.Dispatches()
	if len(dispatches) != 1 {
		t.Fatalf("dispatches = %+v", dispatches)
	}
	if dispatches[0].TargetSessionID != "" || dispatches[0].Status != "waiting_session" {
		t.Fatalf("2-hour-old pending launch hijacked manual session: %+v", dispatches[0])
	}

	launches.mu.Lock()
	remaining := len(launches.pending)
	_, stalePresent := launches.pending["req_stale_2h"]
	launches.mu.Unlock()
	if stalePresent {
		t.Fatal("expired pending launch req_stale_2h was not pruned")
	}
	if remaining > 128 {
		t.Fatalf("pending launches count = %d, want bounded at most 128", remaining)
	}
}

func TestResumeFromPhoneRecoversReplyQueuedInRegistrationWindow(t *testing.T) {
	deps, st := newIsolatedDeps(t)
	if err := st.SetAway(true); err != nil {
		t.Fatal(err)
	}
	previous := holdTimeout
	holdTimeout = 140 * time.Millisecond
	t.Cleanup(func() { holdTimeout = previous })

	// Hold waits.mu so resumeFromPhone passes its initial queuedReplies check
	// and pauses right before registering stopHold in waits.bySession.
	waits.mu.Lock()
	done := make(chan map[string]any, 1)
	go func() {
		done <- resumeFromPhone(context.Background(), deps, store.Event{
			ID:        77,
			Source:    "claude",
			Kind:      "reply",
			SessionID: "demo-away-window",
			Text:      "等待指示",
		})
	}()

	time.Sleep(15 * time.Millisecond)
	if _, err := st.AppendReply(store.Reply{
		SessionID: "demo-away-window",
		Source:    "claude",
		Text:      "繼續執行下一步",
		Status:    "queued",
	}); err != nil {
		waits.mu.Unlock()
		t.Fatal(err)
	}
	waits.mu.Unlock()

	decision := <-done
	if decision["decision"] != "block" {
		t.Fatalf("resumeFromPhone decision = %#v, want block (lost queued reply in registration window)", decision)
	}
	reason, _ := decision["reason"].(string)
	if !strings.Contains(reason, "繼續執行下一步") {
		t.Fatalf("reason = %q, want reply text included", reason)
	}
	replies := st.Replies()
	if len(replies) != 1 || replies[0].Status != "held" {
		t.Fatalf("replies = %+v, want status=held", replies)
	}
}
