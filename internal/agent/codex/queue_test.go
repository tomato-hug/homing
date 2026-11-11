package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomato-hug/homing/internal/agent"
)

func TestCancelQueuedReplyRemovesOnlyRequestedMessage(t *testing.T) {
	threadMu.Lock()
	previous, hadPrevious := threads["demo-queue-session"]
	threads["demo-queue-session"] = &threadState{
		running: true,
		queue: []queuedReply{
			{sessionID: "demo-queue-session", text: "first", replyID: 41},
			{sessionID: "demo-queue-session", text: "second", replyID: 42},
		},
	}
	threadMu.Unlock()
	t.Cleanup(func() {
		threadMu.Lock()
		defer threadMu.Unlock()
		if hadPrevious {
			threads["demo-queue-session"] = previous
		} else {
			delete(threads, "demo-queue-session")
		}
	})

	if err := (Agent{}).CancelQueuedReply(context.Background(), "demo-queue-session", 41); err != nil {
		t.Fatalf("CancelQueuedReply() error = %v", err)
	}
	if err := (Agent{}).CancelQueuedReply(context.Background(), "demo-queue-session", 41); !errors.Is(err, agent.ErrNotQueued) {
		t.Fatalf("second cancel error = %v, want ErrNotQueued", err)
	}

	threadMu.Lock()
	defer threadMu.Unlock()
	state := threads["demo-queue-session"]
	if len(state.queue) != 1 {
		t.Fatalf("queue length = %d, want 1", len(state.queue))
	}
	if state.queue[0].replyID != 42 {
		t.Fatalf("remaining reply = %d, want 42", state.queue[0].replyID)
	}
}

func TestReplyUsesCodexNativeQueue(t *testing.T) {
	argsFile := installFakeCodex(t, `#!/bin/sh
printf '%s\n' "$@" > "$HOMING_CODEX_ARGS"
exit 0
`)

	err := (Agent{}).Reply(
		context.Background(),
		"demo-native-queue-session",
		"fix the layout",
		agent.ReplyOpts{Model: "gpt-test"},
	)
	if err != nil {
		t.Fatalf("Reply() error = %v", err)
	}

	data, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatalf("read fake codex args: %v", readErr)
	}
	args := string(data)
	for _, want := range []string{
		"queue\n",
		"--thread\ndemo-native-queue-session\n",
		"--message\nfix the layout\n",
		"--resume\n",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("codex args missing %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "exec\nresume") {
		t.Fatalf("Reply() used legacy writer path:\n%s", args)
	}
}

func TestReplyReportsQueueFailureInsteadOfPretendingInjected(t *testing.T) {
	installFakeCodex(t, `#!/bin/sh
echo "thread-store conflict: already has an active writer" >&2
exit 1
`)

	err := (Agent{}).Reply(
		context.Background(),
		"demo-queue-conflict-session",
		"hello",
		agent.ReplyOpts{},
	)
	if err == nil {
		t.Fatal("Reply() error = nil, want visible queue failure")
	}
	if !strings.Contains(err.Error(), "active writer") {
		t.Fatalf("Reply() error = %v, want thread-store detail", err)
	}
}

func TestReplyTreatsResumeWarningAsQueued(t *testing.T) {
	installFakeCodex(t, `#!/bin/sh
echo "warning: failed to resume thread on local daemon" >&2
exit 0
`)

	err := (Agent{}).Reply(
		context.Background(),
		"demo-queue-warning-session",
		"hello",
		agent.ReplyOpts{},
	)
	if !errors.Is(err, agent.ErrQueued) {
		t.Fatalf("Reply() error = %v, want ErrQueued", err)
	}
}

func installFakeCodex(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "codex")
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	argsFile := filepath.Join(dir, "args.txt")
	t.Setenv("HOMING_CODEX_ARGS", argsFile)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}
