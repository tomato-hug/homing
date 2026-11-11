package relayapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tomato-hug/homing/internal/orchestrator"
	"github.com/tomato-hug/homing/internal/store"
)

func TestComposeDryRunShowsPlanWithoutSideEffects(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  orchestrator.StartResult{SessionID: "demo-claude-child", Status: "started"},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	parent, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-compose-parent", Cwd: cwd,
		Title: "父会话", Text: "示例父会话",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-compose-parent",
		"source":"codex",
		"text":"先修正登录按钮\n@claude:review 检查登录流程并补测试",
		"dry_run":true
	}`)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	var result struct {
		OK   bool `json:"ok"`
		Plan struct {
			Message  string `json:"message"`
			Mentions []struct {
				Agent        string `json:"agent"`
				Preset       string `json:"preset"`
				Text         string `json:"text"`
				Return       string `json:"return"`
				Dispatchable bool   `json:"dispatchable"`
			} `json:"mentions"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Plan.Message != "先修正登录按钮" {
		t.Fatalf("plan=%+v", result.Plan)
	}
	if len(result.Plan.Mentions) != 1 || result.Plan.Mentions[0].Agent != "claude" ||
		result.Plan.Mentions[0].Preset != "review" || !result.Plan.Mentions[0].Dispatchable {
		t.Fatalf("mentions=%+v", result.Plan.Mentions)
	}
	if result.Plan.Mentions[0].Return != "card" {
		t.Fatalf("review return = %q", result.Plan.Mentions[0].Return)
	}
	if len(relay.store.Dispatches()) != 0 {
		t.Fatal("dry run must not create dispatches")
	}
	if len(relay.store.Replies()) != 0 {
		t.Fatal("dry run must not create replies")
	}
	if len(relay.store.Events()) != 1 || relay.store.Events()[0].ID != parent.ID {
		t.Fatal("dry run must not append events")
	}
}

func TestComposeHandoffAlwaysUsesNoResultAndMarksDispatch(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  orchestrator.StartResult{SessionID: "demo-claude-handoff", Status: "started"},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-handoff-parent", Cwd: cwd,
		Title: "父会话",
	}); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-handoff-parent",
		"source":"codex",
		"text":"@claude:handoff 接手修复登录流程",
		"cwd":%q,
		"collab":{"return":"card"}
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	dispatches := relay.store.Dispatches()
	if len(dispatches) != 1 {
		t.Fatalf("dispatches=%+v", dispatches)
	}
	if dispatches[0].Return != "none" {
		t.Fatalf("handoff return = %q", dispatches[0].Return)
	}
	if !strings.Contains(dispatches[0].Message, "移交") {
		t.Fatalf("handoff marker = %q", dispatches[0].Message)
	}
}

func TestComposeDryRunAppliesPerMentionOverrides(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true, "codex": true},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-compose-parent", Cwd: cwd,
		Title: "父会话",
	}); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-compose-parent",
		"source":"codex",
		"text":"先确认范围\n@claude:review 检查登录流程\n@codex:do 修正按钮",
		"cwd":%q,
		"dry_run":true,
		"collab":{"context":["recent","files"]},
		"mentions":[
			{"agent":"claude","target":"new","context":["diff"],"workspace":"readonly"},
			{"agent":"codex","target":"new","context":[],"workspace":"shared"}
		]
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	var result struct {
		Plan struct {
			Mentions []struct {
				Agent     string   `json:"agent"`
				Target    string   `json:"target"`
				Context   []string `json:"context"`
				Workspace string   `json:"workspace"`
			} `json:"mentions"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Plan.Mentions) != 2 {
		t.Fatalf("mentions=%+v", result.Plan.Mentions)
	}
	claude := result.Plan.Mentions[0]
	if claude.Agent != "claude" || claude.Target != "new" ||
		claude.Workspace != "readonly" ||
		len(claude.Context) != 1 || claude.Context[0] != "diff" {
		t.Fatalf("claude override=%+v", claude)
	}
	codex := result.Plan.Mentions[1]
	if codex.Agent != "codex" || codex.Workspace != "shared" ||
		len(codex.Context) != 0 {
		t.Fatalf("codex override=%+v", codex)
	}
	if len(relay.store.Dispatches()) != 0 || len(relay.store.Replies()) != 0 {
		t.Fatal("dry run must not create dispatches or replies")
	}
}

func TestComposeContinueRequiresExistingDispatch(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-compose-parent", Cwd: cwd,
	}); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-compose-parent",
		"text":"父消息\n@claude 继续检查",
		"cwd":%q,
		"dry_run":true,
		"mentions":[{"agent":"claude","target":"continue"}]
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusConflict ||
		!strings.Contains(string(body), `"code":"continue_target_missing"`) {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if len(relay.store.Dispatches()) != 0 || len(relay.store.Replies()) != 0 {
		t.Fatal("missing continuation must not create dispatches or replies")
	}
}

func TestComposeContinueAutoWorkspaceUsesExistingThread(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-compose-parent", Cwd: cwd,
	}); err != nil {
		t.Fatal(err)
	}
	existing, err := relay.store.SaveDispatch(store.Dispatch{
		ID:              "dsp_demo_continue",
		ParentSessionID: "demo-compose-parent",
		ParentSource:    "codex",
		TargetAgent:     "claude",
		TargetSessionID: "demo-claude-child",
		Text:            "先前的审查",
		Cwd:             cwd,
		Mode:            "parallel",
		Preset:          "review",
		Workspace:       "readonly",
		Status:          "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-compose-parent",
		"text":"父消息\n@claude 接着审查",
		"cwd":%q,
		"dry_run":true,
		"mentions":[
			{"agent":"claude","target":"continue","context":["recent"],"workspace":"auto"}
		]
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	var result struct {
		Plan struct {
			Mentions []struct {
				Target    string   `json:"target"`
				ThreadID  string   `json:"thread_id"`
				Workspace string   `json:"workspace"`
				Context   []string `json:"context"`
			} `json:"mentions"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Plan.Mentions) != 1 {
		t.Fatalf("mentions=%+v", result.Plan.Mentions)
	}
	mention := result.Plan.Mentions[0]
	if mention.Target != "continue" || mention.ThreadID != existing.ID ||
		mention.Workspace != "readonly" ||
		len(mention.Context) != 1 || mention.Context[0] != "recent" {
		t.Fatalf("continuation plan=%+v", mention)
	}
	if len(relay.store.Dispatches()) != 1 || len(relay.store.Replies()) != 0 {
		t.Fatal("dry run must not create another dispatch or reply")
	}
}

func TestComposeRejectsInvalidMentionContext(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-compose-parent", Cwd: cwd,
	}); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-compose-parent",
		"text":"父消息\n@claude 检查登录流程",
		"cwd":%q,
		"dry_run":true,
		"mentions":[{"agent":"claude","context":["secrets"]}]
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusBadRequest ||
		!strings.Contains(string(body), `"code":"mention_override_invalid"`) {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if len(relay.store.Dispatches()) != 0 || len(relay.store.Replies()) != 0 {
		t.Fatal("invalid override must not create dispatches or replies")
	}
}

func TestComposeRejectsWholeRequestWhenOneMentionIsNotDispatchable(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{targets: map[string]bool{"claude": true}})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-compose-parent", Cwd: cwd,
	}); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-compose-parent",
		"text":"父消息\n@claude 可以做\n@kimi 这个不行",
		"cwd":%q
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if len(relay.store.Replies()) != 0 || len(relay.store.Dispatches()) != 0 {
		t.Fatal("partial compose must not send anything")
	}
}

func TestComposeStartsChildAndResultReturnsToParent(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  orchestrator.StartResult{SessionID: "demo-claude-child", Status: "started"},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-compose-parent", Cwd: cwd,
		Title: "父会话",
	}); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-compose-parent",
		"text":"父消息\n@claude 检查登录流程",
		"cwd":%q
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if len(relay.store.Dispatches()) != 1 || len(relay.store.Replies()) != 1 {
		t.Fatalf("dispatches=%d replies=%d", len(relay.store.Dispatches()), len(relay.store.Replies()))
	}
	dispatch := relay.store.Dispatches()[0]
	if dispatch.Preset != "ask" || dispatch.TargetSessionID != "demo-claude-child" {
		t.Fatalf("dispatch=%+v", dispatch)
	}

	event, err := relay.store.AppendEvent(store.Event{
		Source: "claude", Kind: "reply", SessionID: "demo-claude-child", Cwd: cwd,
		Text: "结论：登录流程没问题，已补两个测试。",
	})
	if err != nil {
		t.Fatal(err)
	}
	trackDispatchLifecycle(relay.deps, event)
	updated := relay.store.Dispatches()[0]
	if updated.Status != "completed" || updated.Result == nil ||
		updated.Result.Text != "结论：登录流程没问题，已补两个测试。" {
		t.Fatalf("updated=%+v", updated)
	}
	var collab *store.Event
	for _, stored := range relay.store.Events() {
		if stored.Kind == "collab_result" {
			found := stored
			collab = &found
			break
		}
	}
	if collab == nil || collab.SessionID != "demo-compose-parent" {
		t.Fatalf("collab result event = %+v", collab)
	}
}

func TestComposeInjectRequiresSettingAndAutoForwardsResultOnce(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  orchestrator.StartResult{SessionID: "demo-claude-child", Status: "started"},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-inject-parent", Cwd: cwd,
		Title: "父会话",
	}); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-inject-parent",
		"text":"父消息\n@claude 检查登录流程",
		"cwd":%q,
		"collab":{"return":"inject"}
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("disabled inject status=%d body=%s", response.StatusCode, body)
	}
	if len(relay.store.Dispatches()) != 0 || len(relay.store.Replies()) != 0 {
		t.Fatal("disabled inject must not create a dispatch or reply")
	}

	if err := relay.store.SetCollabReturn("inject"); err != nil {
		t.Fatal(err)
	}
	response, body = relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("enabled inject status=%d body=%s", response.StatusCode, body)
	}
	event, err := relay.store.AppendEvent(store.Event{
		Source: "claude", Kind: "reply", SessionID: "demo-claude-child", Cwd: cwd,
		Text: "结论：登录流程没问题。",
	})
	if err != nil {
		t.Fatal(err)
	}
	trackDispatchLifecycle(relay.deps, event)
	parentReplies := 0
	var forwarded string
	for _, reply := range relay.store.Replies() {
		if reply.SessionID != "demo-inject-parent" {
			continue
		}
		parentReplies++
		if strings.Contains(reply.Text, "@claude 的結論") {
			forwarded = reply.Text
		}
	}
	if parentReplies != 2 || !strings.Contains(forwarded, "登录流程没问题") {
		t.Fatalf("parent replies=%d forwarded=%q", parentReplies, forwarded)
	}
	// Completion hooks may repeat (for example reply and stop). The result must
	// still be handed to the parent only once.
	var repeats sync.WaitGroup
	for index := 0; index < 8; index++ {
		repeats.Add(1)
		go func() {
			defer repeats.Done()
			trackDispatchLifecycle(relay.deps, event)
		}()
	}
	repeats.Wait()
	parentReplies = 0
	for _, reply := range relay.store.Replies() {
		if reply.SessionID == "demo-inject-parent" {
			parentReplies++
		}
	}
	if parentReplies != 2 {
		t.Fatalf("duplicate completion forwarded %d parent replies", parentReplies-1)
	}
	auditRaw, err := os.ReadFile(filepath.Join(relay.deps.DataDir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	forwardAudits := 0
	for _, line := range strings.Split(strings.TrimSpace(string(auditRaw)), "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry["action"] == "forward" {
			forwardAudits++
		}
	}
	if forwardAudits != 1 {
		t.Fatalf("auto-forward audit entries = %d, want 1", forwardAudits)
	}
}

func TestComposeRejectsUnsupportedPermissionModeBeforeAnySideEffect(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  orchestrator.StartResult{SessionID: "demo-claude-child", Status: "started"},
	})
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-compose-parent", Cwd: cwd,
	}); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-compose-parent",
		"text":"父消息\n@claude 检查登录流程",
		"cwd":%q,
		"permission_mode":"accept_edits"
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if len(relay.store.Replies()) != 0 || len(relay.store.Dispatches()) != 0 {
		t.Fatal("invalid permission mode must not send anything")
	}
}

func TestReplyAndNewSessionRejectUnsupportedPermissionMode(t *testing.T) {
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-permission-parent", Cwd: cwd,
	}); err != nil {
		t.Fatal(err)
	}
	replyBody := fmt.Sprintf(`{
		"session_id":"demo-permission-parent",
		"source":"codex",
		"text":"测试",
		"permission_mode":"plan"
	}`)
	response, body := relay.do(t, http.MethodPost, "/api/reply", []byte(replyBody), true)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("reply status=%d body=%s", response.StatusCode, body)
	}
	if len(relay.store.Replies()) != 0 {
		t.Fatal("invalid reply permission mode must not append a reply")
	}

	newBody := fmt.Sprintf(`{
		"source":"codex",
		"cwd":%q,
		"text":"测试",
		"permission_mode":"accept_edits"
	}`, cwd)
	response, body = relay.do(t, http.MethodPost, "/api/sessions/new", []byte(newBody), true)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("new session status=%d body=%s", response.StatusCode, body)
	}
}

func TestComposeLaunchFailureDoesNotSendParentOrLeaveWorktree(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"claude": true},
		result:  orchestrator.StartResult{Status: "failed", Message: "示例：启动失败"},
	})
	relay := newTestRelay(t, nil)
	repo := demoGitRepo(t)
	if _, err := relay.store.AppendEvent(store.Event{
		Source: "codex", Kind: "reply", SessionID: "demo-compose-parent", Cwd: repo,
	}); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{
		"session_id":"demo-compose-parent",
		"text":"父消息\n@claude 检查登录流程",
		"cwd":%q,
		"collab":{"workspace":"worktree"}
	}`, repo)
	response, body := relay.do(t, http.MethodPost, "/api/compose", []byte(payload), true)
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if len(relay.store.Replies()) != 0 {
		t.Fatal("failed dispatch must not send the parent reply first")
	}
	worktrees := filepath.Join(relay.deps.DataDir, "worktrees")
	entries, err := os.ReadDir(worktrees)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("worktree left behind: %v", entries)
	}
	out, err := exec.Command("git", "-C", repo, "branch", "--list", "homing/*").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("worktree branch left behind: %s", out)
	}
}

func demoGitRepo(t *testing.T) string {
	t.Helper()
	dir := demoCwd(t)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "demo@example.invalid"},
		{"config", "user.name", "Homing Demo"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-qm", "demo"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}
