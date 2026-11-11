package adapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agentconfig"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/platform"
)

type builtinAdapterSpec struct {
	ID            string
	Name          string
	Push          []string
	Reply         string
	DefaultSignal string
}

// Keep this list in sync with internal/api/agents.go and relay/console.mjs.
var builtinAdapters = []builtinAdapterSpec{
	{ID: "codex", Name: "Codex", Push: []string{"hook", "poll"}, Reply: "注入新消息", DefaultSignal: "hook"},
	{ID: "claude", Name: "Claude Code", Push: []string{"hook"}, Reply: "注入新消息", DefaultSignal: "hook"},
	{ID: "workbuddy", Name: "WorkBuddy", Push: []string{"hook"}, Reply: "排队回复", DefaultSignal: "hook"},
	{ID: "opencode", Name: "OpenCode", Push: []string{"poll"}, Reply: "不支持", DefaultSignal: "poll"},
	{ID: "zcode", Name: "ZCode", Push: []string{"poll"}, Reply: "不支持", DefaultSignal: "poll"},
}

// communityAdapters is the Discover tab's list. It is derived from the same
// catalog as the Agent page, so the two screens cannot drift apart.
func communityAdapters() []map[string]string {
	out := make([]map[string]string, 0, 12)
	for _, spec := range catalogSpecs {
		if spec.Readiness != ReadinessHook && spec.Readiness != ReadinessUnverified {
			continue
		}
		out = append(out, map[string]string{
			"id": spec.ID, "name": spec.Name, "owner": "community", "note": spec.Notes,
		})
	}
	return out
}

// consoleAdapterPayload is the full shape expected by console/src/pages/adapters.ts.
// The older items/dirs/counts fields stay for tools that already consume them.
func consoleAdapterPayload(d *api.Deps) map[string]any {
	refresh()
	runtimeMu.Lock()
	ma := append([]*ManifestAgent(nil), manifests...)
	sa := append([]*ScriptAgent(nil), scripts...)
	merrs := append([]ManifestError(nil), manErrs...)
	serrs := append([]ManifestError(nil), scrErrs...)
	runtimeMu.Unlock()

	store := agentconfig.NewStore(d.DataDir)
	ids := make([]string, 0, len(builtinAdapters))
	for _, item := range builtinAdapters {
		ids = append(ids, item.ID)
	}
	settings := store.Snapshot(ids)

	builtin := make([]map[string]any, 0, len(builtinAdapters))
	legacyItems := make([]map[string]any, 0, len(builtinAdapters)+len(ma)+len(sa)+len(merrs)+len(serrs))
	for _, item := range builtinAdapters {
		home := tildifyPath(platform.DescribeHome(item.ID).Path)
		configFile := tildifyPath(store.ConfigFile(item.ID))
		connected := settings[item.ID].Connected
		view := map[string]any{
			"id":             item.ID,
			"name":           item.Name,
			"push":           item.Push,
			"reply":          item.Reply,
			"default_signal": item.DefaultSignal,
			"kind":           "builtin",
			"home":           nullableString(home),
			"config_file":    nullableString(configFile),
			"connected":      connected,
		}
		builtin = append(builtin, view)
		legacyItems = append(legacyItems, map[string]any{
			"kind": "builtin", "id": item.ID, "name": item.Name,
			"push_signal": item.DefaultSignal, "status": "builtin",
		})
	}

	custom := make([]map[string]any, 0, len(ma)+len(merrs)+len(sa)+len(serrs))
	for _, item := range ma {
		view := manifestView(item)
		custom = append(custom, view)
		legacyItems = append(legacyItems, map[string]any{
			"kind": "manifest", "id": item.ID(), "name": item.Name(), "icon": item.Icon(),
			"push_signal": string(item.PushSignal()), "status": "ok", "file": item.m.SourceFile,
		})
	}
	for _, item := range merrs {
		view := brokenManifestView(item)
		custom = append(custom, view)
		legacyItems = append(legacyItems, map[string]any{
			"kind": "manifest", "id": item.ID, "name": item.ID, "icon": "generic",
			"push_signal": "poll", "status": "error", "error": strings.Join(item.Problems, "; "),
			"file": item.File,
		})
	}
	for _, item := range sa {
		view := scriptView(item)
		custom = append(custom, view)
		legacyItems = append(legacyItems, map[string]any{
			"kind": "script", "id": item.ID(), "name": item.Name(), "icon": item.Icon(),
			"push_signal": string(item.PushSignal()), "status": "ok", "file": item.file,
		})
	}
	for _, item := range serrs {
		view := brokenScriptView(item)
		custom = append(custom, view)
		legacyItems = append(legacyItems, map[string]any{
			"kind": "script", "id": item.ID, "name": item.ID, "icon": "generic",
			"push_signal": "poll", "status": "error", "error": strings.Join(item.Problems, "; "),
			"file": item.File,
		})
	}
	sort.SliceStable(custom, func(i, j int) bool {
		return fmt.Sprint(custom[i]["id"]) < fmt.Sprint(custom[j]["id"])
	})

	installed := make([]map[string]any, 0, len(builtin)+len(custom))
	for _, item := range builtin {
		if item["connected"] == true {
			installed = append(installed, item)
		}
	}
	installed = append(installed, custom...)

	payload := map[string]any{
		"builtin":      builtin,
		"installed":    installed,
		"custom":       custom,
		"community":    communityAdapters(),
		"formats":      []string{"jsonl", "json", "sqlite"},
		"push_methods": []string{"hook", "watch", "poll"},
		"root":         tildifyPath(ManifestDir()),
		"placeholder":  false,
		"note":         "内置接入直接读会话；自定义接入可用 YAML 清单、脚本适配器，或让 agent 调用 /hook/generic 和 /api/ingest。",

		// Legacy contract retained for existing tooling.
		"items": legacyItems,
		"dirs": map[string]string{
			"manifests": ManifestDir(),
			"scripts":   ScriptDir(),
		},
		"counts": map[string]int{
			"builtin":  len(builtinAdapters),
			"manifest": len(ma),
			"script":   len(sa),
			"errors":   len(merrs) + len(serrs),
		},
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if ingest != nil {
		payload["ingest"] = ingest.List()
	}
	if bin, err := RunnerAvailable(); err != nil {
		payload["runner"] = map[string]any{"ok": false, "error": err.Error()}
	} else {
		payload["runner"] = map[string]any{"ok": true, "bin": bin}
	}
	return payload
}

func manifestView(item *ManifestAgent) map[string]any {
	m := item.m
	sourcePath := firstText(m.Sessions.Glob, m.Sessions.DBGlob, m.Sessions.DB, m.Sessions.URL, m.Sessions.Dir)
	format := m.Sessions.Format
	if format == "" {
		format = "generic-jsonl"
	}
	push := m.Push
	if push == "" {
		push = "poll"
	}
	replyMode, replyCommand := "none", ""
	if m.Reply != nil {
		replyMode = "command"
		replyCommand = strings.Join(m.Reply.Command, " ")
	}
	raw, _ := os.ReadFile(m.SourceFile)
	createdAt := ""
	if stat, err := os.Stat(m.SourceFile); err == nil {
		createdAt = stat.ModTime().UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"id":   m.ID,
		"name": item.Name(),
		"source": map[string]any{
			"kind":   "file",
			"path":   tildifyPath(sourcePath),
			"format": format,
		},
		"push":       map[string]any{"method": push},
		"reply":      map[string]any{"mode": replyMode, "command": replyCommand},
		"file":       m.SourceFile,
		"yaml":       string(raw),
		"created_at": createdAt,
		"broken":     false,
	}
}

func scriptView(item *ScriptAgent) map[string]any {
	reply := "none"
	if item.Capabilities().Reply {
		reply = "script"
	}
	return map[string]any{
		"id":   item.ID(),
		"name": item.Name(),
		"source": map[string]any{
			"kind": "script", "path": item.file, "format": "mjs",
		},
		"push":   map[string]any{"method": string(item.PushSignal())},
		"reply":  map[string]any{"mode": reply, "command": ""},
		"file":   item.file,
		"broken": false,
	}
}

func brokenManifestView(item ManifestError) map[string]any {
	id := item.ID
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(item.File), filepath.Ext(item.File))
	}
	return map[string]any{
		"id":   id,
		"name": id,
		"source": map[string]any{
			"kind": "file", "path": tildifyPath(item.File), "format": "yaml",
		},
		"push":   map[string]any{"method": "poll"},
		"reply":  map[string]any{"mode": "none", "command": ""},
		"file":   item.File,
		"broken": true,
		"error":  strings.Join(item.Problems, "; "),
	}
}

func brokenScriptView(item ManifestError) map[string]any {
	id := item.ID
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(item.File), filepath.Ext(item.File))
	}
	return map[string]any{
		"id":   id,
		"name": id,
		"source": map[string]any{
			"kind": "script", "path": item.File, "format": "mjs",
		},
		"push":   map[string]any{"method": "poll"},
		"reply":  map[string]any{"mode": "none", "command": ""},
		"file":   item.File,
		"broken": true,
		"error":  strings.Join(item.Problems, "; "),
	}
}

// manifestYAMLFromRequest accepts both the Node/console object form and the
// original {yaml:"..."} form. The object form is intentionally narrow; people
// who need SQLite queries or custom format_options should edit the YAML.
func manifestYAMLFromRequest(body map[string]any) (string, error) {
	if text, _ := body["yaml"].(string); strings.TrimSpace(text) != "" {
		return text, nil
	}
	obj, _ := body["adapter"].(map[string]any)
	if obj == nil {
		if _, hasName := body["name"]; hasName {
			obj = body
		} else if _, hasSource := body["source"]; hasSource {
			obj = body
		}
	}
	if obj == nil {
		return "", errors.New("yaml 不能为空")
	}

	id, _ := obj["id"].(string)
	name, _ := obj["name"].(string)
	if strings.TrimSpace(id) == "" {
		id = slugAdapterID(name)
	}
	if strings.TrimSpace(name) == "" {
		name = id
	}
	source, _ := obj["source"].(map[string]any)
	path, _ := source["path"].(string)
	format, _ := source["format"].(string)
	kind, _ := source["kind"].(string)
	if strings.TrimSpace(path) == "" {
		return "", errors.New("文件位置不能为空")
	}
	if kind != "" && kind != "file" {
		return "", errors.New("图形表单只支持文件来源；进程/HTTP/SQLite 请直接写 YAML")
	}

	sessions := map[string]any{"source": "jsonl", "glob": path}
	switch format {
	case "", "jsonl":
		sessions["format"] = "generic-jsonl"
	case "json":
		sessions["source"] = "json"
		sessions["format"] = "generic-json"
	case "sqlite":
		query, _ := obj["query"].(string)
		if strings.TrimSpace(query) == "" {
			return "", errors.New("SQLite 清单需要 query；请切到 YAML 直接填写")
		}
		sessions = map[string]any{"source": "sqlite", "db": path, "query": query}
	case "markdown", "log":
		return "", fmt.Errorf("%s 解析器尚未实现；可先导出成 JSONL，或写脚本适配器", format)
	default:
		return "", fmt.Errorf("不支持的记录格式：%s", format)
	}

	push := "poll"
	if item, ok := obj["push"].(map[string]any); ok {
		if method, _ := item["method"].(string); method != "" {
			push = method
		}
	}
	manifest := map[string]any{
		"id":       id,
		"name":     name,
		"sessions": sessions,
		"push":     push,
	}
	if reply, ok := obj["reply"].(map[string]any); ok {
		if mode, _ := reply["mode"].(string); mode != "none" {
			command := commandArray(reply["command"])
			if len(command) > 0 {
				manifest["reply"] = map[string]any{"command": command}
			}
		}
	}
	raw, err := yamlMarshal(manifest)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func commandArray(value any) []string {
	switch item := value.(type) {
	case string:
		return strings.Fields(item)
	case []any:
		out := make([]string, 0, len(item))
		for _, part := range item {
			if text, ok := part.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func adapterTestRead(inputPath, format string) map[string]any {
	target := expandHome(strings.TrimSpace(inputPath))
	if target == "" {
		return map[string]any{"ok": false, "error": "先填文件位置"}
	}
	stat, err := os.Stat(target)
	if err != nil {
		return map[string]any{
			"ok": false, "error": "路径不存在：" + tildifyPath(target), "checked": target,
		}
	}
	type fileInfo struct {
		path  string
		size  int64
		mtime time.Time
	}
	files := []fileInfo{}
	collect := func(file string) {
		if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
			files = append(files, fileInfo{path: file, size: info.Size(), mtime: info.ModTime()})
		}
	}
	if stat.IsDir() {
		entries, err := os.ReadDir(target)
		if err != nil {
			return map[string]any{"ok": false, "error": "目录读不了：" + err.Error(), "checked": target}
		}
		for index, entry := range entries {
			if index >= 200 {
				break
			}
			collect(filepath.Join(target, entry.Name()))
		}
	} else {
		collect(target)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime.After(files[j].mtime) })

	var total int64
	for _, file := range files {
		total += file.size
	}
	samples := make([]map[string]any, 0, 3)
	for _, file := range files {
		if len(samples) >= 3 {
			break
		}
		handle, err := os.Open(file.path)
		if err != nil {
			continue
		}
		buffer := make([]byte, 2048)
		n, _ := handle.Read(buffer)
		_ = handle.Close()
		preview := ""
		for _, line := range strings.Split(string(buffer[:n]), "\n") {
			if strings.TrimSpace(line) != "" {
				preview = line
				break
			}
		}
		if len([]rune(preview)) > 220 {
			preview = string([]rune(preview)[:220])
		}
		samples = append(samples, map[string]any{
			"file": tildifyPath(file.path), "size": file.size, "preview": preview,
		})
	}
	out := map[string]any{
		"ok":         len(files) > 0,
		"format":     format,
		"checked":    tildifyPath(target),
		"is_dir":     stat.IsDir(),
		"file_count": len(files),
		"total_size": total,
		"samples":    samples,
		"note":       "只验证路径和文件形状；记录解析由清单适配器完成。",
	}
	if len(files) == 0 {
		out["error"] = "路径在，但里面没有可读文件"
	}
	return out
}

func slugAdapterID(input string) string {
	text := strings.ToLower(strings.TrimSpace(input))
	var b strings.Builder
	lastDash := false
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.':
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "custom-adapter"
	}
	return id
}

func tildifyPath(path string) string {
	if path == "" {
		return ""
	}
	home := platform.Home()
	if path == home {
		return "~"
	}
	prefix := home + string(filepath.Separator)
	if strings.HasPrefix(path, prefix) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
