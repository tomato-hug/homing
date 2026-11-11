package relayapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/store"
)

type worktreeDiscardPlan struct {
	Worktree       string `json:"worktree"`
	Branch         string `json:"branch"`
	Repo           string `json:"repo"`
	WorktreeExists bool   `json:"worktree_exists"`
	BranchExists   bool   `json:"branch_exists"`
	Dirty          bool   `json:"dirty"`
}

// discardDispatchWorktree is the confirmation-gated cleanup for a finished
// collaboration worktree. It only touches paths under the Homing data
// directory and branches created by Homing; it never removes a parent checkout.
func discardDispatchWorktree(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	if !d.CheckRate(w, r, "dispatch_discard", 20, time.Minute) {
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
	dispatch, ok := findDispatch(s, strings.TrimSpace(r.PathValue("id")))
	if !ok {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "协作任务不存在"})
		return
	}
	if dispatch.WorktreeDiscardedAt != "" {
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "already_discarded": true, "dispatch": dispatch,
		})
		return
	}
	if strings.ToLower(strings.TrimSpace(dispatch.Workspace)) != "worktree" ||
		!safeHomingBranch(dispatch.Branch) {
		api.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "code": "not_worktree_dispatch",
			"error": "这条协作任务没有 Homing 管理的独立 worktree",
		})
		return
	}
	if dispatchActive(dispatch.Status) {
		api.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "code": "dispatch_active",
			"error": "协作任务仍在运行；先取消或等待完成，才能丢弃 worktree",
		})
		return
	}

	plan, planErr := inspectDispatchWorktree(r.Context(), d.DataDir, dispatch)
	if planErr != nil {
		api.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "code": "worktree_invalid", "error": planErr.Error(),
		})
		return
	}
	dryRun, _ := boolField(body, "dry_run")
	if dryRun {
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "dry_run": true,
			"would_remove_worktree": plan.WorktreeExists,
			"would_remove_branch":   plan.BranchExists,
			"plan":                  plan,
		})
		return
	}
	confirm, _ := boolField(body, "confirm")
	if !confirm {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "code": "confirmation_required",
			"error": "丢弃 worktree 会删除未合并改动；请带 confirm=true 再次请求",
		})
		return
	}

	if plan.WorktreeExists {
		if out, removeErr := gitOutput(
			r.Context(), plan.Repo, "worktree", "remove", "--force", plan.Worktree,
		); removeErr != nil {
			api.WriteJSON(w, http.StatusConflict, map[string]any{
				"ok": false, "code": "worktree_remove_failed",
				"error": "移除 worktree 失败：" + truncateRunes(strings.TrimSpace(out), 300),
				"plan":  plan,
			})
			return
		}
	}
	if plan.BranchExists {
		if out, removeErr := gitOutput(
			r.Context(), plan.Repo, "branch", "-D", plan.Branch,
		); removeErr != nil {
			api.WriteJSON(w, http.StatusConflict, map[string]any{
				"ok": false, "code": "branch_remove_failed",
				"error": "worktree 已清理，但删除分支失败：" +
					truncateRunes(strings.TrimSpace(out), 300),
				"plan": plan,
			})
			return
		}
	}

	dispatch.WorktreeDiscardedAt = nowISO()
	saved, saveErr := s.SaveDispatch(dispatch)
	if saveErr != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{
			"ok": false, "error": "worktree 已清理，但保存协作记录失败",
		})
		return
	}
	publishDispatch(d, saved)
	d.Audit(r, "discard_worktree", map[string]any{
		"dispatch_id":       saved.ID,
		"parent_session_id": saved.ParentSessionID,
		"target":            saved.TargetAgent,
		"branch":            saved.Branch,
		"cwd":               saved.Cwd,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":                    true,
		"removed_worktree":      plan.WorktreeExists,
		"removed_branch":        plan.BranchExists,
		"dispatch":              saved,
		"worktree_discarded_at": saved.WorktreeDiscardedAt,
	})
}

func dispatchActive(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "starting", "waiting_session", "started", "running", "needs_you":
		return true
	}
	return false
}

func safeHomingBranch(branch string) bool {
	branch = strings.TrimSpace(branch)
	if !strings.HasPrefix(branch, "homing/dsp_") || strings.ContainsAny(branch, " \t\r\n~^:?*[\\") {
		return false
	}
	if strings.Contains(branch, "..") || strings.Contains(branch, "@{") ||
		strings.HasPrefix(branch, "-") || strings.HasSuffix(branch, "/") {
		return false
	}
	return true
}

func inspectDispatchWorktree(
	ctx context.Context,
	dataDir string,
	dispatch store.Dispatch,
) (worktreeDiscardPlan, error) {
	plan := worktreeDiscardPlan{
		Worktree: strings.TrimSpace(dispatch.Cwd),
		Branch:   strings.TrimSpace(dispatch.Branch),
		Repo:     strings.TrimSpace(dispatch.RepoCwd),
	}
	worktreeAbs, err := filepath.Abs(plan.Worktree)
	if err != nil {
		return plan, errors.New("worktree 路径无效")
	}
	plan.Worktree = worktreeAbs
	root := filepath.Join(dataDir, "worktrees")
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return plan, errors.New("Homing worktree 目录无效")
	}
	if !pathWithin(rootAbs, worktreeAbs) {
		return plan, errors.New("拒绝清理 Homing 数据目录之外的 worktree")
	}
	if info, statErr := os.Lstat(worktreeAbs); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return plan, errors.New("拒绝清理符号链接形式的 worktree")
		}
		if info.IsDir() {
			plan.WorktreeExists = true
		}
	} else if !os.IsNotExist(statErr) {
		return plan, fmt.Errorf("检查 worktree 失败：%w", statErr)
	}

	if plan.Repo == "" && plan.WorktreeExists {
		plan.Repo, err = mainWorktreeFor(ctx, plan.Worktree)
		if err != nil {
			return plan, err
		}
	}
	if plan.Repo == "" {
		return plan, errors.New("缺少原始仓库路径，无法安全清理")
	}
	repoAbs, err := filepath.Abs(plan.Repo)
	if err != nil {
		return plan, errors.New("原始仓库路径无效")
	}
	plan.Repo = repoAbs
	if info, statErr := os.Stat(plan.Repo); statErr != nil || !info.IsDir() {
		return plan, errors.New("原始仓库目录不存在")
	}
	if plan.WorktreeExists {
		current, branchErr := gitOutput(ctx, plan.Worktree, "rev-parse", "--abbrev-ref", "HEAD")
		if branchErr != nil {
			return plan, errors.New("worktree 不是可读的 git 工作树")
		}
		if strings.TrimSpace(current) != plan.Branch {
			return plan, errors.New("worktree 当前分支与协作记录不一致")
		}
		status, statusErr := gitOutput(ctx, plan.Worktree, "status", "--porcelain")
		if statusErr != nil {
			return plan, errors.New("无法检查 worktree 状态")
		}
		plan.Dirty = strings.TrimSpace(status) != ""
	}
	if _, branchErr := gitOutput(ctx, plan.Repo, "show-ref", "--verify", "--quiet", "refs/heads/"+plan.Branch); branchErr == nil {
		plan.BranchExists = true
	} else if exitCode(branchErr) != 1 {
		return plan, errors.New("无法检查协作分支")
	}
	if !plan.WorktreeExists && !plan.BranchExists {
		return plan, nil
	}
	return plan, nil
}

func mainWorktreeFor(ctx context.Context, worktree string) (string, error) {
	out, err := gitOutput(ctx, worktree, "worktree", "list", "--porcelain")
	if err != nil {
		return "", errors.New("无法读取 worktree 所属仓库")
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
			if path != "" {
				return path, nil
			}
		}
	}
	return "", errors.New("worktree 所属仓库没有主工作树")
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel)
}
