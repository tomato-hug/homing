package relayapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/driver"
	filepaths "github.com/tomato-hug/homing/internal/files"
	"github.com/tomato-hug/homing/internal/orchestrator"
	"github.com/tomato-hug/homing/internal/store"
)

type composeMentionPlan struct {
	Agent        string   `json:"agent"`
	Preset       string   `json:"preset"`
	Text         string   `json:"text"`
	Target       string   `json:"target"`
	ThreadID     string   `json:"thread_id,omitempty"`
	Workspace    string   `json:"workspace"`
	Context      []string `json:"context"`
	Return       string   `json:"return"`
	Dispatchable bool     `json:"dispatchable"`
	Reason       string   `json:"reason,omitempty"`
}

type composePlan struct {
	Message  string               `json:"message"`
	Mentions []composeMentionPlan `json:"mentions"`
}

// composeMentionOverride lets a client adjust the plan for one parsed mention
// without inventing a second mention syntax. Entries are aligned by index with
// the mentions parsed from text; Agent is optional but must match when present.
type composeMentionOverride struct {
	Agent     string   `json:"agent,omitempty"`
	Target    string   `json:"target,omitempty"`
	Context   []string `json:"context,omitempty"`
	Workspace string   `json:"workspace,omitempty"`
}

type composeReplyResult struct {
	ReplyID int64  `json:"reply_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

var agentAliases = map[string]string{
	"claude-code":      "claude",
	"claudecode":       "claude",
	"codex-cli":        "codex",
	"deepseek-harness": "dsh",
	"deepseek":         "dsh",
	"kimi-code":        "kimi",
	"kimi-cli":         "kimi",
}

func canonicalMentionAgent(raw string) string {
	agentID := strings.ToLower(strings.TrimSpace(raw))
	if alias, ok := agentAliases[agentID]; ok {
		return alias
	}
	return agentID
}

func composeAgent(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	if !d.CheckRate(w, r, "compose", 20, time.Minute) {
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "数据目录不可用"})
		return
	}
	parentSessionID := strings.TrimSpace(stringField(body, "session_id"))
	if parentSessionID == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "session_id 不能为空"})
		return
	}
	parent, found := d.FindSummary(r.Context(), parentSessionID)
	if !found {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "父会话不存在"})
		return
	}
	parentSource := strings.TrimSpace(stringField(body, "source"))
	if parentSource == "" {
		parentSource = parent.Source
	}
	if parent.Source != "" && parentSource != parent.Source {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "source 与父会话不匹配"})
		return
	}
	if parentSource == "" {
		parentSource = d.SourceFor(r.Context(), parentSessionID)
	}
	cwd := strings.TrimSpace(stringField(body, "cwd"))
	if cwd == "" && parent.Cwd != nil {
		cwd = strings.TrimSpace(*parent.Cwd)
	}
	if cwd == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "父会话没有可用的 cwd"})
		return
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cwd 非法"})
		return
	}
	if info, statErr := os.Stat(cwd); statErr != nil || !info.IsDir() {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cwd 不是可用目录"})
		return
	}

	text := strings.TrimSpace(stringField(body, "text"))
	if text == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "text 不能为空"})
		return
	}
	message, parsed := orchestrator.ParseMentions(text)
	collab, _ := body["collab"].(map[string]any)
	contextParts, contextErr := normalizeCollabContext(stringList(collab["context"]), true)
	if contextErr != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": contextErr.Error(), "code": "context_invalid",
		})
		return
	}
	overrides, overrideErr := parseComposeMentionOverrides(body["mentions"])
	if overrideErr != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": overrideErr.Error(), "code": "mention_override_invalid",
		})
		return
	}
	if len(overrides) > len(parsed) {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": "mentions 覆盖项多于文本中的 @agent",
			"code": "mention_override_invalid",
		})
		return
	}
	returnMode := ""
	if raw, exists := collab["return"]; exists {
		returnMode = strings.ToLower(strings.TrimSpace(anyString(raw, "")))
	}
	if returnMode == "" {
		returnMode = s.CollabReturn()
	}
	if returnMode != "card" && returnMode != "none" && returnMode != "inject" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "collab.return 只能是 card / none / inject"})
		return
	}
	if returnMode == "inject" && s.CollabReturn() != "inject" {
		api.WriteJSON(w, http.StatusForbidden, map[string]any{
			"ok": false, "code": "collab_inject_disabled",
			"error": "自动转交子 Agent 结论默认关闭，请先在设置中开启",
		})
		return
	}

	plan := composePlan{Message: message, Mentions: []composeMentionPlan{}}
	for index, mention := range parsed {
		planItem := composeMentionPlan{
			Agent:   canonicalMentionAgent(mention.Agent),
			Preset:  mention.Preset,
			Text:    mention.Text,
			Target:  "new",
			Context: append([]string(nil), contextParts...),
			Return:  composeReturnForPreset(mention.Preset, returnMode),
		}
		if validationErr := orchestrator.ValidateMention(orchestrator.Mention{
			Agent: planItem.Agent, Preset: planItem.Preset, Text: planItem.Text,
		}); validationErr != nil {
			planItem.Reason = validationErr.Error()
			plan.Mentions = append(plan.Mentions, planItem)
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{
				"ok": false, "error": validationErr.Error(), "code": "mention_invalid", "plan": plan,
			})
			return
		}
		if !dispatchLauncher.CanDispatch(planItem.Agent) {
			planItem.Reason = "该 Agent 尚未提供新会话启动器"
			plan.Mentions = append(plan.Mentions, planItem)
			api.WriteJSON(w, http.StatusConflict, map[string]any{
				"ok": false, "error": "Agent " + planItem.Agent + " 不能接收协作任务",
				"code": "agent_not_dispatchable", "plan": plan,
			})
			return
		}
		var override composeMentionOverride
		if index < len(overrides) {
			override = overrides[index]
			if override.Agent != "" &&
				canonicalMentionAgent(override.Agent) != planItem.Agent {
				api.WriteJSON(w, http.StatusBadRequest, map[string]any{
					"ok": false, "code": "mention_override_invalid",
					"error": fmt.Sprintf("第 %d 个 mention 覆盖项目标是 %s，文本中是 %s",
						index+1, override.Agent, planItem.Agent),
					"plan": plan,
				})
				return
			}
			if override.Context != nil {
				planItem.Context, overrideErr = normalizeCollabContext(override.Context, false)
				if overrideErr != nil {
					api.WriteJSON(w, http.StatusBadRequest, map[string]any{
						"ok": false, "code": "mention_override_invalid",
						"error": fmt.Sprintf("第 %d 个 mention：%s", index+1, overrideErr),
						"plan":  plan,
					})
					return
				}
			}
		}
		thread, hasThread := recentDispatchThread(s, parentSessionID, planItem.Agent, 24*time.Hour)
		targetMode := strings.ToLower(strings.TrimSpace(override.Target))
		switch targetMode {
		case "", "auto":
			if hasThread {
				planItem.Target = "continue"
				planItem.ThreadID = thread.ID
			}
		case "new":
			planItem.Target = "new"
		case "continue":
			if !hasThread {
				planItem.Target = "continue"
				planItem.Reason = "没有可接续的协作线"
				plan.Mentions = append(plan.Mentions, planItem)
				api.WriteJSON(w, http.StatusConflict, map[string]any{
					"ok": false, "code": "continue_target_missing",
					"error": "没有可接续的 " + planItem.Agent + " 协作线，请改成新开或自动",
					"plan":  plan,
				})
				return
			}
			planItem.Target = "continue"
			planItem.ThreadID = thread.ID
		default:
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{
				"ok": false, "code": "mention_override_invalid",
				"error": fmt.Sprintf("第 %d 个 mention 的 target 只能是 auto / new / continue", index+1),
				"plan":  plan,
			})
			return
		}
		workspaceRequest := anyString(collab["workspace"], "auto")
		if override.Workspace != "" {
			workspaceRequest = override.Workspace
		}
		switch strings.ToLower(strings.TrimSpace(workspaceRequest)) {
		case "", "auto", "shared", "readonly", "worktree":
		default:
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{
				"ok": false, "code": "mention_override_invalid",
				"error": fmt.Sprintf(
					"第 %d 个 mention 的 workspace 只能是 auto / shared / readonly / worktree",
					index+1,
				),
				"plan": plan,
			})
			return
		}
		planItem.Workspace, planItem.Dispatchable = resolveWorkspace(
			cwd, planItem.Preset, workspaceRequest,
		)
		if planItem.Target == "continue" && thread.Workspace != "" {
			requestedWorkspace := strings.ToLower(strings.TrimSpace(override.Workspace))
			if requestedWorkspace == "" || requestedWorkspace == "auto" {
				planItem.Workspace = thread.Workspace
			}
		}
		plan.Mentions = append(plan.Mentions, planItem)
	}

	dryRun, _ := body["dry_run"].(bool)
	if dryRun {
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "plan": plan, "dry_run": true})
		return
	}

	files := filepaths.NormalizeFiles(stringList(body["files"]))
	model := truncateRunes(strings.TrimSpace(anyString(body["model"], "")), 80)
	effort := truncateRunes(strings.TrimSpace(anyString(body["effort"], "")), 32)
	permissionMode := truncateRunes(
		strings.ToLower(strings.TrimSpace(anyString(body["permission_mode"], ""))), 32,
	)
	if !api.PermissionModeAllowed(parentSource, permissionMode) {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": "permission_mode 不是父会话 Agent 支持的模式",
			"code": "permission_mode_unsupported", "plan": plan,
		})
		return
	}
	actions, prepareErr := prepareComposeActions(d, r, s, parent, parentSource, parentSessionID, cwd, plan, returnMode)
	if prepareErr != nil {
		status := http.StatusInternalServerError
		code := "dispatch_failed"
		var typed *orchestrator.Error
		if errors.As(prepareErr, &typed) {
			status = typed.Status
			code = typed.Code
		}
		api.WriteJSON(w, status, map[string]any{
			"ok": false, "error": prepareErr.Error(), "code": code, "plan": plan,
		})
		return
	}

	createdWorktrees := make([]composeWorktree, 0, len(actions))
	for i := range actions {
		if actions[i].continuation != nil || actions[i].plan.Workspace != "worktree" {
			continue
		}
		created, branch, baseCommit, worktreeErr := createDispatchWorktree(
			d.DataDir, cwd, actions[i].dispatch.ID,
		)
		if worktreeErr != nil {
			removeComposeWorktrees(cwd, createdWorktrees)
			api.WriteJSON(w, http.StatusConflict, map[string]any{
				"ok": false, "error": "无法创建 worktree：" + worktreeErr.Error(), "plan": plan,
			})
			return
		}
		actions[i].childCwd = created
		actions[i].branch = branch
		actions[i].baseCommit = baseCommit
		actions[i].worktree = &composeWorktree{Path: created, Branch: branch}
		createdWorktrees = append(createdWorktrees, *actions[i].worktree)
	}

	dispatches := make([]store.Dispatch, 0, len(actions))
	for i := range actions {
		action := &actions[i]
		if action.continuation != nil {
			existing := *action.continuation
			_, _, note, replyErr := sendComposeReply(
				d, r, s, existing.TargetAgent, existing.TargetSessionID, existing.Cwd,
				nil, action.plan.Text, nil, "", "", "",
			)
			if replyErr != nil {
				removeComposeWorktrees(cwd, unstartedComposeWorktrees(actions, i))
				api.WriteJSON(w, http.StatusBadGateway, map[string]any{
					"ok": false, "error": replyErr.Error(), "plan": plan, "dispatches": dispatches,
				})
				return
			}
			continuation := existing
			continuation.ID = "dsp_" + randomHex(10)
			continuation.Thread = action.plan.ThreadID
			continuation.Text = action.plan.Text
			continuation.Preset = action.plan.Preset
			continuation.Workspace = action.plan.Workspace
			continuation.Branch = existing.Branch
			continuation.Return = action.plan.Return
			continuation.Status = "running"
			continuation.Message = note
			continuation.Result = nil
			continuation.CreatedAt = ""
			continuation.UpdatedAt = ""
			saved, saveErr := s.SaveDispatch(continuation)
			if saveErr != nil {
				removeComposeWorktrees(cwd, unstartedComposeWorktrees(actions, i+1))
				api.WriteJSON(w, http.StatusInternalServerError, map[string]any{
					"ok": false, "error": "保存协作记录失败", "plan": plan, "dispatches": dispatches,
				})
				return
			}
			dispatches = append(dispatches, saved)
			publishDispatch(d, saved)
			d.Audit(r, "dispatch", map[string]any{
				"dispatch_id":       saved.ID,
				"parent_session_id": saved.ParentSessionID,
				"target":            saved.TargetAgent,
				"preset":            saved.Preset,
			})
			continue
		}

		svc := orchestrator.Service{
			Store:    s,
			Launcher: dispatchLauncher,
			NewID:    func() string { return action.dispatch.ID },
		}
		dispatch, dispatchErr := svc.Dispatch(r.Context(), orchestrator.Request{
			ParentSessionID: parentSessionID,
			ParentSource:    parentSource,
			TargetAgent:     action.plan.Agent,
			Cwd:             action.childCwd,
			Text:            action.plan.Text,
			Preset:          action.plan.Preset,
			Workspace:       action.plan.Workspace,
			Branch:          action.branch,
			BaseCommit:      action.baseCommit,
			RepoCwd:         cwd,
			Return:          action.plan.Return,
			Prompt:          action.prompt,
		})
		if dispatchErr != nil {
			if action.worktree != nil {
				removeComposeWorktrees(cwd, []composeWorktree{*action.worktree})
			}
			removeComposeWorktrees(cwd, unstartedComposeWorktrees(actions, i+1))
			status := http.StatusInternalServerError
			code := "dispatch_failed"
			var typed *orchestrator.Error
			if errors.As(dispatchErr, &typed) {
				status = typed.Status
				code = typed.Code
			}
			payload := map[string]any{
				"ok": false, "error": dispatchErr.Error(), "code": code,
				"plan": plan, "dispatches": dispatches,
			}
			if dispatch.ID != "" {
				payload["dispatch"] = dispatch
			}
			api.WriteJSON(w, status, payload)
			return
		}
		dispatches = append(dispatches, dispatch)
		publishDispatch(d, dispatch)
		d.Audit(r, "dispatch", map[string]any{
			"dispatch_id":       dispatch.ID,
			"parent_session_id": dispatch.ParentSessionID,
			"target":            dispatch.TargetAgent,
			"preset":            dispatch.Preset,
		})
	}

	var reply *composeReplyResult
	if message != "" {
		replyID, status, note, replyErr := sendComposeReply(
			d, r, s, parentSource, parentSessionID, cwd, nil, message, files,
			model, effort, permissionMode,
		)
		if replyErr != nil {
			api.WriteJSON(w, http.StatusBadGateway, map[string]any{
				"ok": false, "error": replyErr.Error(), "plan": plan, "dispatches": dispatches,
			})
			return
		}
		reply = &composeReplyResult{ReplyID: replyID, Status: status, Message: note}
	}

	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "plan": plan, "reply": reply, "dispatches": dispatches,
	})
}

type composeAction struct {
	plan         composeMentionPlan
	continuation *store.Dispatch
	dispatch     store.Dispatch
	childCwd     string
	branch       string
	baseCommit   string
	prompt       string
	worktree     *composeWorktree
}

type composeWorktree struct {
	Path   string
	Branch string
}

func prepareComposeActions(
	d *api.Deps,
	r *http.Request,
	s *store.Store,
	parent api.Summary,
	parentSource, parentSessionID, cwd string,
	plan composePlan,
	returnMode string,
) ([]composeAction, error) {
	actions := make([]composeAction, len(plan.Mentions))
	virtual := make([]store.Dispatch, 0, len(plan.Mentions))
	for i, planItem := range plan.Mentions {
		action := composeAction{plan: planItem, childCwd: cwd}
		if planItem.Target == "continue" && planItem.ThreadID != "" {
			existing, exists := findDispatch(s, planItem.ThreadID)
			if exists && existing.TargetSessionID != "" &&
				existing.Status != "failed" && existing.Status != "cancelled" {
				action.continuation = &existing
				actions[i] = action
				continue
			}
			action.plan.Target = "new"
			action.plan.ThreadID = ""
			plan.Mentions[i].Target = "new"
			plan.Mentions[i].ThreadID = ""
		}
		dispatchID := "dsp_" + randomHex(10)
		prompt := buildContextPack(r.Context(), d, parent, parentSource, cwd, action.plan, action.plan.Context)
		svc := orchestrator.Service{
			Store:    s,
			Launcher: dispatchLauncher,
			NewID:    func() string { return dispatchID },
		}
		prepared, err := svc.Prepare(orchestrator.Request{
			ParentSessionID: parentSessionID,
			ParentSource:    parentSource,
			TargetAgent:     action.plan.Agent,
			Cwd:             cwd,
			Text:            action.plan.Text,
			Preset:          action.plan.Preset,
			Workspace:       action.plan.Workspace,
			Return:          action.plan.Return,
			Prompt:          prompt,
		}, virtual)
		if err != nil {
			return nil, err
		}
		action.dispatch = prepared
		action.prompt = prompt
		virtual = append(virtual, prepared)
		actions[i] = action
	}
	return actions, nil
}

func unstartedComposeWorktrees(actions []composeAction, start int) []composeWorktree {
	out := make([]composeWorktree, 0, len(actions)-start)
	for i := start; i < len(actions); i++ {
		if actions[i].worktree != nil {
			out = append(out, *actions[i].worktree)
		}
	}
	return out
}

func removeComposeWorktrees(repoCwd string, worktrees []composeWorktree) {
	for _, worktree := range worktrees {
		if worktree.Path == "" {
			continue
		}
		if out, err := exec.Command(
			"git", "-C", repoCwd, "worktree", "remove", "--force", worktree.Path,
		).CombinedOutput(); err != nil {
			_ = out
			_ = os.RemoveAll(worktree.Path)
			_, _ = exec.Command("git", "-C", repoCwd, "worktree", "prune").Output()
		}
		if worktree.Branch != "" {
			_, _ = exec.Command("git", "-C", repoCwd, "branch", "-D", worktree.Branch).Output()
		}
	}
}

func recentDispatchThread(s *store.Store, parentSessionID, target string, window time.Duration) (store.Dispatch, bool) {
	cutoff := time.Now().Add(-window).UTC().Format("2006-01-02T15:04:05.000Z")
	var best store.Dispatch
	for _, dispatch := range s.Dispatches() {
		if dispatch.ParentSessionID != parentSessionID || dispatch.TargetAgent != target {
			continue
		}
		if dispatch.Status == "failed" || dispatch.Status == "cancelled" {
			continue
		}
		if dispatch.TargetSessionID == "" || dispatch.CreatedAt < cutoff {
			continue
		}
		if best.ID == "" || dispatch.CreatedAt > best.CreatedAt {
			best = dispatch
		}
	}
	return best, best.ID != ""
}

func resolveWorkspace(cwd, preset, requested string) (string, bool) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" || requested == "auto" {
		switch preset {
		case "ask", "review":
			return "readonly", true
		case "do":
			if isGitRepo(cwd) {
				return "worktree", true
			}
			return "shared", true
		default:
			return "shared", true
		}
	}
	switch requested {
	case "shared", "readonly", "worktree":
		return requested, true
	}
	return "shared", true
}

func composeReturnForPreset(preset, requested string) string {
	if strings.EqualFold(strings.TrimSpace(preset), "handoff") {
		return "none"
	}
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case "none", "inject":
		return strings.ToLower(strings.TrimSpace(requested))
	default:
		return "card"
	}
}

func isGitRepo(cwd string) bool {
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func createDispatchWorktree(dataDir, cwd, dispatchID string) (string, string, string, error) {
	base := filepath.Join(dataDir, "worktrees")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", "", "", err
	}
	baseOut, err := exec.Command("git", "-C", cwd, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", "", "", fmt.Errorf("读取基准 commit 失败")
	}
	path := filepath.Join(base, dispatchID)
	branch := "homing/" + dispatchID
	cmd := exec.Command("git", "-C", cwd, "worktree", "add", "-b", branch, path, "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", "", fmt.Errorf("%s", truncateRunes(strings.TrimSpace(string(out)), 300))
	}
	return path, branch, strings.TrimSpace(string(baseOut)), nil
}

func buildContextPack(
	ctx context.Context,
	d *api.Deps,
	parent api.Summary,
	parentSource, cwd string,
	mention composeMentionPlan,
	parts []string,
) string {
	include := map[string]bool{}
	for _, part := range parts {
		include[strings.ToLower(strings.TrimSpace(part))] = true
	}
	var recentUser, recentAssistant string
	var changed []string
	if a, ok := agent.Get(ctx, parentSource); ok {
		if reader, ok := a.(agent.ItemReader); ok {
			if items, err := reader.ReadItems(ctx, parent.SessionID, 200); err == nil {
				for _, item := range items {
					switch item.Type {
					case "message":
						if item.Role == "user" {
							recentUser = item.Text
						} else if item.Role == "assistant" {
							recentAssistant = item.Text
						}
					case "file_change":
						for _, file := range item.Files {
							if len(changed) < 30 {
								changed = append(changed, fmt.Sprintf("- %s +%d −%d", file.Path, file.Added, file.Removed))
							}
						}
					}
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Homing 协作任务] 来自 %s 的会话「%s」\n", parentSource, parent.Title)
	branch := ""
	if parent.Branch != nil {
		branch = *parent.Branch
	}
	if branch != "" {
		fmt.Fprintf(&b, "工作目录：%s（分支 %s）\n\n", cwd, branch)
	} else {
		fmt.Fprintf(&b, "工作目录：%s\n\n", cwd)
	}
	fmt.Fprintf(&b, "任务：\n%s\n\n--- 背景，仅供参考 ---\n", mention.Text)
	if include["recent"] {
		if strings.TrimSpace(recentUser) != "" {
			fmt.Fprintf(&b, "用户最近的要求：\n%s\n\n", truncateRunes(strings.TrimSpace(recentUser), 1500))
		}
		if strings.TrimSpace(recentAssistant) != "" {
			fmt.Fprintf(&b, "%s 最近的回覆：\n%s\n\n", parentSource,
				truncateRunes(strings.TrimSpace(recentAssistant), 2500))
		}
	}
	if include["files"] && len(changed) > 0 {
		b.WriteString("最近改动的文件：\n")
		b.WriteString(strings.Join(changed, "\n"))
		b.WriteString("\n\n")
	}
	if include["diff"] && isGitRepo(cwd) {
		out, err := exec.Command("git", "-C", cwd, "diff", "--stat").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			b.WriteString("git diff --stat：\n")
			b.WriteString(truncateRunes(strings.TrimSpace(string(out)), 2500))
			b.WriteString("\n\n")
		}
	}
	b.WriteString("--- 背景结束 ---\n\n完成後先用一段話給出結論，再寫細節。")
	if utf8.RuneCountInString(b.String()) > orchestrator.MaxTaskRunes {
		runes := []rune(b.String())
		return string(runes[:orchestrator.MaxTaskRunes])
	}
	return b.String()
}

func sendComposeReply(
	d *api.Deps,
	r *http.Request,
	s *store.Store,
	source, sessionID, cwd string,
	event *store.Event,
	text string,
	files []string,
	model, effort, permissionMode string,
) (int64, string, string, error) {
	if strings.TrimSpace(text) == "" {
		return 0, "", "", errors.New("回复内容不能为空")
	}
	reply, err := s.AppendReply(store.Reply{
		EventID:        eventIDPtr(event),
		SessionID:      sessionID,
		Source:         source,
		Cwd:            cwd,
		Text:           text,
		Model:          model,
		Effort:         effort,
		PermissionMode: permissionMode,
		Files:          files,
		Status:         "queued",
	})
	if err != nil {
		return 0, "", "", err
	}
	if event != nil {
		s.MarkRead([]int64{event.ID})
		_ = s.AdvanceSeen(source, sessionID, event.ID)
	}
	deliveryText := filepaths.DeliveryText(text, files)
	status, note, handled := "", "", false
	if driver.Default().Has(sessionID) {
		if promptErr := driver.Default().Prompt(sessionID, deliveryText); promptErr != nil {
			status, note = "failed", promptErr.Error()
		} else {
			status, note = "injected", "已交给 ACP 会话"
			handled = true
		}
	} else {
		status, note, handled = api.InterceptReply(r, reply, event, deliveryText)
		if !handled {
			status, note = d.DeliverReply(
				r, source, sessionID, deliveryText, model, permissionMode,
			)
		}
	}
	if note == "" {
		note = replyStatusMessage(status)
	}
	updated, _ := s.UpdateReplyStatus(reply.ID, status, note)
	api.PublishReplyStatus(updated)
	if status == "failed" {
		return reply.ID, status, note, errors.New(note)
	}
	return reply.ID, status, note, nil
}

func eventIDPtr(event *store.Event) *int64 {
	if event == nil {
		return nil
	}
	id := event.ID
	return &id
}

func anyString(value any, fallback string) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}

func parseComposeMentionOverrides(raw any) ([]composeMentionOverride, error) {
	if raw == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.New("mentions 覆盖项格式不正确")
	}
	var overrides []composeMentionOverride
	if err := json.Unmarshal(encoded, &overrides); err != nil {
		return nil, errors.New("mentions 覆盖项必须是对象数组")
	}
	if len(overrides) > 20 {
		return nil, errors.New("mentions 覆盖项不能超过 20 个")
	}
	return overrides, nil
}

func normalizeCollabContext(values []string, useDefault bool) ([]string, error) {
	if len(values) == 0 {
		if useDefault {
			return []string{"recent", "files"}, nil
		}
		return []string{}, nil
	}
	allowed := map[string]bool{"recent": true, "files": true, "diff": true}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if !allowed[value] {
			return nil, fmt.Errorf("context 只能包含 recent / files / diff，收到 %q", raw)
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out, nil
}

func replyStatusMessage(status string) string {
	switch status {
	case "injected":
		return "已注入会话"
	case "held":
		return "已交给挂起中的钩子，Agent 继续执行"
	case "queued":
		return "暂无挂起的钩子，已排队"
	case "failed":
		return "投递失败"
	}
	return ""
}

func findDispatch(s *store.Store, id string) (store.Dispatch, bool) {
	for _, dispatch := range s.Dispatches() {
		if dispatch.ID == id {
			return dispatch, true
		}
	}
	return store.Dispatch{}, false
}
