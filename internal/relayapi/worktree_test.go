package relayapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomato-hug/homing/internal/store"
)

func TestDiscardDispatchWorktreeRemovesOnlyHomingCheckout(t *testing.T) {
	relay := newTestRelay(t, nil)
	dispatch, repo, worktree := demoDispatchWorktree(t, relay, "completed")

	response, body := relay.do(
		t, http.MethodPost, "/api/dispatches/"+dispatch.ID+"/discard",
		[]byte(`{"confirm":true}`), true,
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
	if err := exec.Command(
		"git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/"+dispatch.Branch,
	).Run(); err == nil {
		t.Fatal("collaboration branch still exists")
	}
	saved, ok := findDispatch(relay.store, dispatch.ID)
	if !ok || saved.WorktreeDiscardedAt == "" {
		t.Fatalf("discard marker missing: %+v", saved)
	}
	raw, err := os.ReadFile(filepath.Join(relay.deps.DataDir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry["action"] == "discard_worktree" && entry["dispatch_id"] == dispatch.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("discard was not audited: %s", raw)
	}
}

func TestDiscardDispatchWorktreeRequiresConfirmAndFinishedTask(t *testing.T) {
	relay := newTestRelay(t, nil)
	dispatch, _, worktree := demoDispatchWorktree(t, relay, "completed")

	response, body := relay.do(
		t, http.MethodPost, "/api/dispatches/"+dispatch.ID+"/discard",
		[]byte(`{}`), true,
	)
	if response.StatusCode != http.StatusBadRequest ||
		!strings.Contains(string(body), "confirmation_required") {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("unconfirmed discard removed worktree: %v", err)
	}

	dispatch.Status = "running"
	if _, err := relay.store.SaveDispatch(dispatch); err != nil {
		t.Fatal(err)
	}
	response, body = relay.do(
		t, http.MethodPost, "/api/dispatches/"+dispatch.ID+"/discard",
		[]byte(`{"confirm":true}`), true,
	)
	if response.StatusCode != http.StatusConflict ||
		!strings.Contains(string(body), "dispatch_active") {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("active discard removed worktree: %v", err)
	}
}

func TestDiscardDispatchWorktreeRejectsOutsideHomingDirectory(t *testing.T) {
	relay := newTestRelay(t, nil)
	outside := filepath.Join(t.TempDir(), "not-homing")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	dispatch, err := relay.store.SaveDispatch(store.Dispatch{
		ID:              "dsp_demo_outside",
		ParentSessionID: "demo-parent",
		TargetAgent:     "claude",
		Text:            "示例：不应删除",
		Cwd:             outside,
		Workspace:       "worktree",
		Branch:          "homing/dsp_demo_outside",
		Status:          "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, body := relay.do(
		t, http.MethodPost, "/api/dispatches/"+dispatch.ID+"/discard",
		[]byte(`{"confirm":true}`), true,
	)
	if response.StatusCode != http.StatusConflict ||
		!strings.Contains(string(body), "worktree_invalid") {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside directory was removed: %v", err)
	}
}

func TestDispatchDiffStatRecordsWorktreeChanges(t *testing.T) {
	relay := newTestRelay(t, nil)
	dispatch, repo, worktree := demoDispatchWorktree(t, relay, "running")
	base, err := gitOutput(t.Context(), repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "README.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dispatch.BaseCommit = strings.TrimSpace(base)
	stat := dispatchDiffStat(dispatch)
	if !strings.Contains(stat, "README.md") || !strings.Contains(stat, "1 file changed") {
		t.Fatalf("diff stat = %q", stat)
	}
}

func demoDispatchWorktree(
	t *testing.T,
	relay *testRelay,
	status string,
) (store.Dispatch, string, string) {
	t.Helper()
	repo := demoGitRepo(t)
	dispatchID := fmt.Sprintf("dsp_demo_%s", strings.ReplaceAll(status, "_", "-"))
	branch := "homing/" + dispatchID
	worktree := filepath.Join(relay.deps.DataDir, "worktrees", dispatchID)
	if err := os.MkdirAll(filepath.Dir(worktree), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", branch, worktree, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create worktree: %v %s", err, out)
	}
	dispatch, err := relay.store.SaveDispatch(store.Dispatch{
		ID:              dispatchID,
		ParentSessionID: "demo-parent",
		ParentSource:    "codex",
		TargetAgent:     "claude",
		Text:            "示例：检查登录流程",
		Cwd:             worktree,
		RepoCwd:         repo,
		Workspace:       "worktree",
		Branch:          branch,
		Status:          status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return dispatch, repo, worktree
}
