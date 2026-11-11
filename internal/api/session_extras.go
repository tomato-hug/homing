package api

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/driver"
)

// SessionCommand is one row of the composer's "/" palette.
type SessionCommand struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArgumentHint string `json:"argument_hint,omitempty"`
}

var staticCommands = map[string][]SessionCommand{
	"codex": {
		{Name: "status", Description: "查看当前会话与权限状态"},
		{Name: "model", Description: "查看或切换模型", ArgumentHint: "模型名"},
		{Name: "approvals", Description: "查看当前审批策略"},
		{Name: "diff", Description: "查看本次会话的文件改动"},
	},
	"claude": {
		{Name: "status", Description: "查看当前会话状态"},
		{Name: "model", Description: "查看或切换模型", ArgumentHint: "模型名"},
		{Name: "compact", Description: "压缩上下文", ArgumentHint: "保留重点"},
		{Name: "cost", Description: "查看本次会话用量"},
		{Name: "review", Description: "审查当前改动", ArgumentHint: "范围"},
	},
}

func (d *Deps) handleSessionCommands(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathParam(w, r)
	if !ok {
		return
	}
	source := d.sourceFor(ctxOf(r), sessionID)
	commands := append([]SessionCommand(nil), staticCommands[source]...)
	for _, command := range driver.Default().Commands(sessionID) {
		seen := false
		for _, existing := range commands {
			if existing.Name == command.Name {
				seen = true
				break
			}
		}
		if !seen {
			commands = append(commands, SessionCommand{
				Name: command.Name, Description: command.Description,
				ArgumentHint: command.ArgumentHint,
			})
		}
	}
	if commands == nil {
		commands = []SessionCommand{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"source":     source,
		"commands":   commands,
		"count":      len(commands),
	})
}

func (d *Deps) handleSessionExport(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathParam(w, r)
	if !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "md"
	}
	if format != "md" && format != "markdown" {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "目前只支持 format=md"})
		return
	}
	summary, found := d.findSummary(ctxOf(r), sessionID)
	if !found {
		WriteJSON(w, http.StatusNotFound, map[string]any{"error": "会话不存在"})
		return
	}
	items := []agent.Item{}
	if a, ok := agent.Get(ctxOf(r), summary.Source); ok {
		if reader, ok := a.(agent.ItemReader); ok {
			if read, err := reader.ReadItems(ctxOf(r), sessionID, 1000); err == nil {
				items = read
			}
		}
	}
	items = filterReasoningItems(items, queryBool(r, "include_reasoning"))
	body := sessionMarkdown(summary, items)
	filename := safeExportName(firstNonEmpty(summary.Title, sessionID)) + ".md"
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func sessionMarkdown(summary Summary, items []agent.Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", firstNonEmpty(summary.Title, summary.SessionID))
	fmt.Fprintf(&b, "- Agent：%s\n", summary.Source)
	if summary.Project != nil {
		fmt.Fprintf(&b, "- 项目：%s\n", *summary.Project)
	}
	if summary.Cwd != nil {
		fmt.Fprintf(&b, "- 目录：`%s`\n", *summary.Cwd)
	}
	if summary.Branch != nil {
		fmt.Fprintf(&b, "- 分支：`%s`\n", *summary.Branch)
	}
	if summary.Model != nil {
		fmt.Fprintf(&b, "- 模型：%s\n", *summary.Model)
	}
	if summary.Effort != nil {
		fmt.Fprintf(&b, "- 思考强度：%s\n", *summary.Effort)
	}
	if summary.PermissionMode != nil {
		fmt.Fprintf(&b, "- 权限模式：%s\n", *summary.PermissionMode)
	}
	fmt.Fprintf(&b, "- Session：`%s`\n", summary.SessionID)
	fmt.Fprintf(&b, "- 导出时间：%s\n\n", time.Now().UTC().Format(time.RFC3339))
	b.WriteString("---\n\n")
	if len(items) == 0 {
		b.WriteString("_该 Agent 暂未提供可读取的对话条目。_\n")
		return b.String()
	}
	for _, item := range items {
		switch item.Type {
		case "message":
			role := "助手"
			if item.Role == "user" {
				role = "用户"
			}
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", role, strings.TrimSpace(item.Text))
		case "reasoning":
			if strings.TrimSpace(item.Text) != "" {
				fmt.Fprintf(&b, "> 思考：%s\n\n", strings.ReplaceAll(strings.TrimSpace(item.Text), "\n", "\n> "))
			}
		case "tool_call":
			name := "工具"
			if item.Tool != nil && item.Tool.Name != "" {
				name = item.Tool.Name
			}
			fmt.Fprintf(&b, "### 工具调用 · %s\n\n", name)
			if item.Command != "" {
				fmt.Fprintf(&b, "```sh\n%s\n```\n\n", strings.TrimSpace(item.Command))
			}
			if item.Tool != nil && item.Tool.OutputTail != nil && strings.TrimSpace(*item.Tool.OutputTail) != "" {
				fmt.Fprintf(&b, "```text\n%s\n```\n\n", strings.TrimSpace(*item.Tool.OutputTail))
			}
		case "file_change":
			b.WriteString("### 文件改动\n\n")
			for _, file := range item.Files {
				fmt.Fprintf(&b, "- `%s` +%d −%d\n", file.Path, file.Added, file.Removed)
				if strings.TrimSpace(file.Diff) != "" {
					fmt.Fprintf(&b, "\n```diff\n%s\n```\n\n", strings.TrimSpace(file.Diff))
				}
			}
			b.WriteString("\n")
		case "plan":
			b.WriteString("### 计划\n\n")
			for _, step := range item.Plan {
				box := " "
				if step.Status == "completed" || step.Status == "done" {
					box = "x"
				}
				fmt.Fprintf(&b, "- [%s] %s\n", box, step.Step)
			}
			b.WriteString("\n")
		case "error":
			fmt.Fprintf(&b, "### 错误\n\n%s\n\n", strings.TrimSpace(firstNonEmpty(item.Error, item.Text)))
		}
	}
	return b.String()
}

func safeExportName(value string) string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		clean = "session"
	}
	var b strings.Builder
	for _, r := range clean {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == ' ', r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), ".-")
	if out == "" {
		return "session"
	}
	runes := []rune(out)
	if len(runes) > 60 {
		out = string(runes[:60])
	}
	return out
}

// handleFSComplete powers @file completion inside a known project. It never
// walks outside the cwd recorded by a session.
func (d *Deps) handleFSComplete(w http.ResponseWriter, r *http.Request) {
	rawCwd := strings.TrimSpace(r.URL.Query().Get("cwd"))
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	if rawCwd == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "cwd 不能为空"})
		return
	}
	cwd, err := filepath.Abs(rawCwd)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "cwd 非法"})
		return
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "cwd 不存在"})
		return
	}
	if prefix != "" {
		clean := filepath.Clean(prefix)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "prefix 不能离开 cwd"})
			return
		}
		prefix = clean
	}
	if !d.cwdAllowed(ctxOf(r), cwd) {
		WriteJSON(w, http.StatusForbidden, map[string]any{"error": "cwd 不在已知项目内"})
		return
	}

	items, truncated := completePaths(ctxOf(r), cwd, prefix, 50)
	WriteJSON(w, http.StatusOK, map[string]any{
		"cwd":       cwd,
		"prefix":    prefix,
		"items":     items,
		"count":     len(items),
		"truncated": truncated,
	})
}

type completedPath struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

func (d *Deps) cwdAllowed(ctx context.Context, cwd string) bool {
	s, ok := d.store()
	if !ok {
		return false
	}
	for _, event := range s.Events() {
		if event.Cwd == "" {
			continue
		}
		if candidate, err := filepath.Abs(event.Cwd); err == nil && candidate == cwd {
			return true
		}
	}
	return false
}

func completePaths(ctx context.Context, cwd, prefix string, limit int) ([]completedPath, bool) {
	lowerPrefix := strings.ToLower(prefix)
	if top := strings.SplitN(prefix, string(filepath.Separator), 2); len(top) > 0 && strings.HasPrefix(top[0], ".") && top[0] == "." {
		return []completedPath{}, false
	}
	if paths, ok := gitFiles(ctx, cwd); ok {
		out := make([]completedPath, 0, limit)
		truncated := false
		for _, rel := range paths {
			if !strings.HasPrefix(strings.ToLower(rel), lowerPrefix) {
				continue
			}
			if len(out) == limit {
				truncated = true
				break
			}
			out = append(out, completedPath{Path: rel, Type: "file"})
		}
		return out, truncated
	}

	out := make([]completedPath, 0, limit)
	visited := 0
	truncated := false
	_ = filepath.WalkDir(cwd, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == cwd {
			return nil
		}
		rel, relErr := filepath.Rel(cwd, path)
		if relErr != nil {
			return nil
		}
		base := entry.Name()
		if entry.IsDir() && (strings.HasPrefix(base, ".") || base == "node_modules" || base == "build" || base == "dist") {
			return filepath.SkipDir
		}
		visited++
		if visited > 4000 {
			truncated = true
			return fs.SkipAll
		}
		if !strings.HasPrefix(strings.ToLower(rel), lowerPrefix) {
			return nil
		}
		if len(out) == limit {
			truncated = true
			return fs.SkipAll
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		out = append(out, completedPath{Path: rel, Type: kind})
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, truncated
}

func gitFiles(ctx context.Context, cwd string) ([]string, bool) {
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	parts := strings.Split(string(out), "\x00")
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if clean := strings.TrimSpace(part); clean != "" {
			files = append(files, filepath.ToSlash(clean))
		}
	}
	sort.Strings(files)
	return files, true
}
