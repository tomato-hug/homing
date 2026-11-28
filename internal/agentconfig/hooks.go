package agentconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

// HookInstallResult is both the /api/agents/{id}/hooks response and the value
// shown by the setup page. Field names match the Node console.
type HookInstallResult struct {
	OK          bool           `json:"ok"`
	Changed     bool           `json:"changed,omitempty"`
	File        string         `json:"file,omitempty"`
	Backup      string         `json:"backup,omitempty"`
	Message     string         `json:"message,omitempty"`
	Error       string         `json:"error,omitempty"`
	Manual      bool           `json:"manual,omitempty"`
	Unsupported bool           `json:"unsupported,omitempty"`
	Snippet     map[string]any `json:"snippet,omitempty"`
}

// InstallHooks installs the HTTP lifecycle hooks for one built-in agent.
//
// Claude Code and WorkBuddy are merged in place, with a timestamped backup.
// Codex is manual on purpose: writing hooks.json invalidates its trust hashes,
// so Homing only returns the snippet the user can paste and trust themselves.
func (s *Store) InstallHooks(agentID string, port int) HookInstallResult {
	if port <= 0 || port > 65535 {
		return HookInstallResult{Error: "无效的端口，无法生成钩子地址"}
	}
	switch agentID {
	case "claude", "workbuddy":
		file := s.ConfigFile(agentID)
		if file == "" {
			return HookInstallResult{Unsupported: true, Message: "这个 agent 没有可安装的配置文件"}
		}
		if _, err := os.Stat(file); err != nil {
			if agentID == "workbuddy" {
				return HookInstallResult{
					File:   file,
					Manual: true,
					Error:  "没找到 WorkBuddy 的 settings.json，请在 WorkBuddy 设置里按文档手工添加钩子",
				}
			}
			if !errors.Is(err, os.ErrNotExist) {
				return HookInstallResult{File: file, Error: fmt.Sprintf("读取 %s 失败：%v", file, err)}
			}
		}

		settings := map[string]any{}
		if raw, err := os.ReadFile(file); err == nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, &settings); err != nil {
				return HookInstallResult{File: file, Error: fmt.Sprintf("%s 不是合法 JSON（%v），没有改动", file, err)}
			}
			if settings == nil {
				settings = map[string]any{}
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return HookInstallResult{File: file, Error: fmt.Sprintf("读取 %s 失败：%v", file, err)}
		}

		url := fmt.Sprintf("http://127.0.0.1:%d/hook/%s", port, agentID)
		next, changed := mergeClaudeHooks(settings, url)
		if !changed {
			return HookInstallResult{OK: true, File: file, Message: "钩子已经装好了"}
		}
		backup, err := backupThenWrite(file, next)
		if err != nil {
			return HookInstallResult{File: file, Error: err.Error()}
		}
		return HookInstallResult{
			OK:      true,
			Changed: true,
			File:    file,
			Backup:  backup,
			Message: "已安装钩子，重启对应的 agent 后生效",
		}

	case "codex":
		file := s.ConfigFile("codex")
		url := fmt.Sprintf("http://127.0.0.1:%d/hook/codex", port)
		return HookInstallResult{
			File:    file,
			Manual:  true,
			Message: "Codex 的 PermissionRequest 钩子需要你在 hooks.json 里自行添加并在 Codex 里确认信任；Homing 不会改动它的信任状态",
			Snippet: map[string]any{
				"hooks": map[string]any{
					"PermissionRequest": []any{
						map[string]any{
							"hooks": []any{
								map[string]any{"type": "http", "url": url, "timeout": 660},
							},
						},
					},
				},
			},
		}
	default:
		return HookInstallResult{Unsupported: true, Message: "这个 agent 走轮询读取，不需要钩子"}
	}
}

func claudeHooksSpec(url string) map[string]any {
	return map[string]any{
		"Stop": []any{
			map[string]any{"hooks": []any{
				map[string]any{"type": "http", "url": url, "timeout": 660},
			}},
		},
		"Notification": []any{
			map[string]any{"hooks": []any{
				map[string]any{"type": "http", "url": url, "timeout": 5},
			}},
		},
		"PermissionRequest": []any{
			map[string]any{"hooks": []any{
				map[string]any{"type": "http", "url": url, "timeout": 660},
			}},
		},
		"PreToolUse": []any{
			map[string]any{
				"matcher": "AskUserQuestion|ExitPlanMode",
				"hooks": []any{
					map[string]any{"type": "http", "url": url, "timeout": 5},
				},
			},
		},
	}
}

func mergeClaudeHooks(settings map[string]any, url string) (map[string]any, bool) {
	next := cloneMap(settings)
	hooks, _ := next["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	changed := false
	for event, entries := range claudeHooksSpec(url) {
		existing, _ := hooks[event].([]any)
		if hookListHasURL(existing, url) {
			continue
		}
		newEntries, _ := entries.([]any)
		hooks[event] = append(append([]any(nil), existing...), newEntries...)
		changed = true
	}
	next["hooks"] = hooks
	return next, changed
}

func hookListHasURL(entries []any, url string) bool {
	for _, entry := range entries {
		group, _ := entry.(map[string]any)
		list, _ := group["hooks"].([]any)
		for _, item := range list {
			hook, _ := item.(map[string]any)
			if hookURL, _ := hook["url"].(string); hookURL == url {
				return true
			}
		}
	}
	return false
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func backupThenWrite(file string, value map[string]any) (string, error) {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", fmt.Errorf("创建配置目录失败：%w", err)
	}
	backup := ""
	if raw, err := os.ReadFile(file); err == nil {
		backup = fmt.Sprintf("%s.bak-homing-%s", file, time.Now().Format("20060102T150405"))
		if err := platform.WriteFile(backup, raw); err != nil {
			return "", fmt.Errorf("备份 %s 失败：%w", filepath.Base(file), err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("读取 %s 失败：%w", filepath.Base(file), err)
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("生成配置失败：%w", err)
	}
	body = append(body, '\n')
	if err := platform.WriteFile(file, body); err != nil {
		return "", fmt.Errorf("写入 %s 失败：%w", filepath.Base(file), err)
	}
	return backup, nil
}
