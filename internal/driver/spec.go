// Package driver runs agents through stable, process-based protocols. Codex
// and Claude keep their existing CLI launchers; the ACP driver here covers
// agents that expose Agent Client Protocol over stdio, so adding another one
// does not require another switch statement in the HTTP layer.
package driver

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

// Spec describes one verified non-shell command that starts an agent in ACP
// mode. Command is an argv vector; it is never passed through a shell.
type Spec struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	Command      []string          `json:"command"`
	Env          map[string]string `json:"env,omitempty"`
	Dispatch     bool              `json:"dispatch"`
	Verified     bool              `json:"verified"`
	Experimental bool              `json:"experimental,omitempty"`
	SetupHint    string            `json:"setup_hint,omitempty"`
	Notes        string            `json:"notes,omitempty"`
	// Check is an optional argv vector that verifies a usable ACP adapter
	// without entering a long-lived protocol session.
	Check []string `json:"check,omitempty"`
	// CheckFail contains case-insensitive output fragments that mean the binary
	// exists but its ACP dependencies are not usable.
	CheckFail []string `json:"check_fail,omitempty"`
	// CheckRequire contains fragments the check output must contain. It keeps a
	// generic `--help` probe from claiming ACP support when the installed build
	// has no ACP flag yet.
	CheckRequire []string `json:"check_require,omitempty"`
}

// CommandString is display-only. Callers must use Command for execution.
func (s Spec) CommandString() string {
	return strings.Join(s.Command, " ")
}

// Installed reports whether the first argv element exists on PATH or user bin dirs.
func (s Spec) Installed() bool {
	if len(s.Command) == 0 || strings.TrimSpace(s.Command[0]) == "" {
		return false
	}
	return platform.ResolveBinary(s.Command[0]) != ""
}

// specs intentionally contains only commands verified from official agent
// documentation. A missing binary is not an error: capabilities are reported
// as false until the user installs that agent.
var specs = []Spec{
	{
		ID: "hermes", Label: "Hermes Agent", Command: []string{"hermes", "acp"},
		Dispatch: true, Verified: true,
		Check: []string{"hermes", "acp", "--check"},
		CheckFail: []string{
			"acp dependencies not installed",
			"install them with",
		},
		Notes: "Hermes ACP mode over stdio.",
	},
	{
		ID: "openclaw", Label: "OpenClaw", Command: []string{"openclaw", "acp"},
		Dispatch: true, Verified: true,
		Check: []string{"openclaw", "acp", "--help"},
		Notes: "OpenClaw's ACP bridge forwards prompts to its Gateway.",
	},
	{
		ID: "kimi", Label: "Kimi Code", Command: []string{"kimi", "acp"},
		Dispatch: true, Verified: true,
		Check: []string{"kimi", "acp", "--help"},
		Notes: "Kimi Code exposes ACP over stdin/stdout.",
	},
	{
		ID: "dsh", Label: "DeepSeek Harness", Command: []string{"dsh", "--profile", "acp"},
		Dispatch: true, Verified: true, Experimental: true,
		Check:     []string{"dsh", "--profile", "acp", "--help"},
		SetupHint: "先安裝 @dsh-enhanced/acp 並建立 acp profile；官方 ACP profile 尚未全域內建。",
		Notes:     "DSH 的 ACP 能力由 @dsh-enhanced/acp profile plugin 提供。",
	},
	{
		ID: "gemini-cli", Label: "Gemini CLI", Command: []string{"gemini", "--acp"},
		Dispatch: true, Experimental: true,
		Check:        []string{"gemini", "--help"},
		CheckRequire: []string{"acp"},
		SetupHint:    "需要已登入的 Gemini CLI；ACP 模式由 --acp 啟動。",
		Notes:        "Gemini CLI 的 ACP 入口；不同版本若沒有 --acp，自檢會失敗而不是假裝可用。",
	},
	{
		ID: "opencode", Label: "OpenCode", Command: []string{"opencode", "acp"},
		Dispatch: true, Experimental: true,
		Check:     []string{"opencode", "acp", "--help"},
		SetupHint: "OpenCode 需先安裝並完成 provider 設定。",
		Notes:     "OpenCode 的原生 ACP 子命令；內置掃描器仍負責讀取本地會話。",
	},
	{
		ID: "cline", Label: "Cline", Command: []string{"cline", "--acp"},
		Dispatch: true, Experimental: true,
		Check:        []string{"cline", "--help"},
		CheckRequire: []string{"--acp"},
		SetupHint:    "需要 Cline CLI；擴展版本身不能作為 stdio ACP 子進程。",
		Notes:        "Cline CLI 的 ACP 模式。",
	},
	{
		ID: "copilot-cli", Label: "GitHub Copilot CLI", Command: []string{"copilot", "--acp", "--stdio"},
		Dispatch: true, Experimental: true,
		Check:        []string{"copilot", "help"},
		CheckRequire: []string{"--acp", "--stdio"},
		SetupHint:    "ACP 支援目前是 public preview；需先登入 Copilot CLI，並使用 --stdio 傳輸。",
		Notes:        "Copilot CLI 的 ACP stdio 模式；沒有 --acp 或 --stdio 的版本會自檢失敗。",
	},
	{
		ID: "kiro", Label: "Kiro CLI", Command: []string{"kiro-cli-chat", "acp"},
		Dispatch: true, Experimental: true,
		Check:     []string{"kiro-cli-chat", "acp", "--help"},
		SetupHint: "Kiro CLI 需先登入；ACP 使用 kiro-cli-chat acp。",
		Notes:     "Kiro CLI 的原生 ACP 伺服器；舊版 kiro-cli 名稱不冒充可用。",
	},
	{
		ID: "openhands", Label: "OpenHands", Command: []string{"openhands", "acp"},
		Dispatch: true, Experimental: true,
		Check:     []string{"openhands", "acp", "--help"},
		SetupHint: "OpenHands CLI 已停止主動維護；ACP 仍為實驗性，需先完成 /settings。",
		Notes:     "OpenHands CLI 的 ACP 模式（實驗性）。",
	},
	{
		ID: "goose", Label: "Goose", Command: []string{"goose", "acp"},
		Dispatch: true, Experimental: true,
		Check:     []string{"goose", "acp", "--help"},
		SetupHint: "Goose 需先完成 provider 設定並安裝 ACP 支援版本。",
		Notes:     "Goose 的 ACP 子命令；不同發行版的可用性由握手結果決定。",
	},
	{
		ID: "cursor", Label: "Cursor", Command: []string{"cursor-agent", "acp"},
		Dispatch: true, Experimental: true,
		Check:     []string{"cursor-agent", "acp", "--help"},
		SetupHint: "需要已登入的 Cursor CLI（cursor-agent）；IDE 擴展本身不是 ACP 子進程。",
		Notes:     "Cursor CLI 的 ACP 模式。",
	},
	{
		ID: "qwen-code", Label: "Qwen Code", Command: []string{"qwen", "--acp"},
		Dispatch: true, Experimental: true,
		Check:        []string{"qwen", "--help"},
		CheckRequire: []string{"acp"},
		SetupHint:    "需要已登入的 Qwen Code CLI；ACP 模式由 --acp 啟動。",
		Notes:        "Qwen Code 的 ACP 入口；舊版沒有 --acp 時會自檢失敗。",
	},
	{
		ID: "droid", Label: "Factory Droid", Command: []string{"droid", "exec", "--output-format", "acp"},
		Dispatch: true, Experimental: true,
		Check:        []string{"droid", "exec", "--help"},
		CheckRequire: []string{"acp"},
		SetupHint:    "需要 Factory Droid CLI 與已完成的登入設定。",
		Notes:        "Droid exec 的 ACP 輸出模式。",
	},
	{
		ID: "qoder", Label: "Qoder", Command: []string{"qodercli", "--acp"},
		Dispatch: true, Experimental: true,
		Check:        []string{"qodercli", "--help"},
		CheckRequire: []string{"acp"},
		SetupHint:    "需要 Qoder CLI（qodercli）並完成登入；IDE 本身不作為 ACP 子進程。",
		Notes:        "Qoder CLI 的 ACP 模式。",
	},
	{
		ID: "trae", Label: "Trae", Command: []string{"traecli", "acp", "serve"},
		Dispatch: true, Experimental: true,
		Check:     []string{"traecli", "acp", "--help"},
		SetupHint: "需要 Trae CLI（traecli）；不同發行版的 acp serve 參數可能不同。",
		Notes:     "Trae CLI 的 ACP 模式。",
	},
}

// Readiness is the cached result of checking whether an ACP spec can actually
// be launched. Installed alone is not enough: Hermes can exist on PATH while
// its optional ACP dependencies are missing.
type Readiness struct {
	Installed bool   `json:"installed"`
	Ready     bool   `json:"ready"`
	Checked   bool   `json:"checked"`
	Detail    string `json:"detail,omitempty"`
	CheckedAt string `json:"checked_at,omitempty"`
}

type readinessEntry struct {
	at  time.Time
	val Readiness
}

var readinessCache = struct {
	sync.Mutex
	bySpec map[string]readinessEntry
}{
	bySpec: map[string]readinessEntry{},
}

const readinessTTL = 30 * time.Second

// Specs returns a copy of all known ACP drive specs.
func Specs() []Spec {
	registryMu.RLock()
	dynamic := make([]Spec, 0, len(dynamicSpecs))
	for _, spec := range dynamicSpecs {
		dynamic = append(dynamic, spec)
	}
	registryMu.RUnlock()
	out := make([]Spec, len(specs))
	copy(out, specs)
	for _, spec := range dynamic {
		out = append(out, spec)
	}
	return out
}

// SpecFor resolves one agent id.
func SpecFor(id string) (Spec, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	if spec, ok := BuiltinSpecFor(id); ok {
		return spec, true
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, spec := range dynamicSpecs {
		if spec.ID == id {
			return spec, true
		}
	}
	return Spec{}, false
}

// BuiltinSpecFor resolves a drive command shipped with Homing. A manifest may
// persist the enabled state for the same id, but it must not replace the
// built-in command; keeping this distinction lets catalog enablement coexist
// with a drive-only manifest without producing a duplicate-spec error.
func BuiltinSpecFor(id string) (Spec, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, spec := range specs {
		if spec.ID == id {
			return spec, true
		}
	}
	return Spec{}, false
}

// Dispatchable reports whether this machine can currently launch the agent.
func Dispatchable(id string) bool {
	spec, ok := SpecFor(id)
	return ok && spec.Dispatch && CheckReadiness(spec).Ready
}

// CheckReadiness runs the spec's short check command, caching the result for
// thirty seconds. Without a check command it falls back to "binary installed".
func CheckReadiness(spec Spec) Readiness {
	if !spec.Installed() {
		return Readiness{Detail: "找不到 " + firstCommand(spec)}
	}
	readinessCache.Lock()
	if cached, ok := readinessCache.bySpec[spec.ID]; ok && time.Since(cached.at) < readinessTTL {
		readinessCache.Unlock()
		return cached.val
	}
	readinessCache.Unlock()

	value := Readiness{Installed: true, Ready: true, Checked: true}
	value.CheckedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if len(spec.Check) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		checkBin := platform.ResolveBinary(spec.Check[0])
		if checkBin == "" {
			checkBin = spec.Check[0]
		}
		cmd := exec.CommandContext(ctx, checkBin, spec.Check[1:]...)
		cmd.Env = platform.EnvWithAugmentedPATH(spec.Env)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		err := cmd.Run()
		text := strings.TrimSpace(output.String())
		lower := strings.ToLower(text)
		switch {
		case ctx.Err() != nil:
			value.Ready = false
			value.Detail = "ACP 檢查逾時"
		case err != nil:
			value.Ready = false
			value.Detail = truncateOneLine(text, 240)
			if value.Detail == "" {
				value.Detail = err.Error()
			}
		default:
			missing := ""
			for _, fragment := range spec.CheckRequire {
				if !strings.Contains(lower, strings.ToLower(fragment)) {
					missing = fragment
					break
				}
			}
			if missing != "" {
				value.Ready = false
				value.Detail = "ACP 自檢輸出缺少 " + missing
				break
			}
			for _, fragment := range spec.CheckFail {
				if strings.Contains(lower, strings.ToLower(fragment)) {
					value.Ready = false
					value.Detail = truncateOneLine(text, 240)
					break
				}
			}
		}
		cancel()
	}
	readinessCache.Lock()
	readinessCache.bySpec[spec.ID] = readinessEntry{at: time.Now(), val: value}
	readinessCache.Unlock()
	return value
}

// PeekReadiness reports an installed binary and any fresh cached check without
// spawning the agent. Catalog pages call this for every discovered entry; only
// an explicit enable action should pay the cost of a real health check.
func PeekReadiness(spec Spec) Readiness {
	installed := spec.Installed()
	readinessCache.Lock()
	cached, ok := readinessCache.bySpec[spec.ID]
	readinessCache.Unlock()
	if ok && time.Since(cached.at) < readinessTTL {
		cached.val.Installed = installed
		if !installed {
			cached.val.Ready = false
		}
		return cached.val
	}
	return Readiness{Installed: installed}
}

// MarkReadiness lets a real launch immediately invalidate or confirm the cache.
func MarkReadiness(spec Spec, ready bool, detail string) {
	value := Readiness{
		Installed: spec.Installed(),
		Ready:     ready,
		Checked:   true,
		Detail:    truncateOneLine(detail, 240),
	}
	value.CheckedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	readinessCache.Lock()
	readinessCache.bySpec[spec.ID] = readinessEntry{at: time.Now(), val: value}
	readinessCache.Unlock()
}

// ResetReadiness is used when a user manifest changes on disk.
func ResetReadiness(id string) {
	readinessCache.Lock()
	delete(readinessCache.bySpec, id)
	readinessCache.Unlock()
}

var (
	registryMu   sync.RWMutex
	dynamicSpecs = map[string]Spec{}
)

// RegisterSpec adds or replaces a drive spec supplied by a user manifest.
// Built-in specs cannot be replaced, so a manifest cannot shadow a verified
// command.
func RegisterSpec(spec Spec) error {
	if !validSpecID(spec.ID) || len(spec.Command) == 0 {
		return errors.New("無效的 drive spec")
	}
	for _, builtin := range specs {
		if builtin.ID == spec.ID {
			return errors.New("不能覆寫內建 ACP 驅動")
		}
	}
	copySpec := spec
	copySpec.Command = append([]string(nil), spec.Command...)
	copySpec.Check = append([]string(nil), spec.Check...)
	copySpec.CheckFail = append([]string(nil), spec.CheckFail...)
	copySpec.Env = map[string]string{}
	for key, value := range spec.Env {
		copySpec.Env[key] = value
	}
	registryMu.Lock()
	dynamicSpecs[copySpec.ID] = copySpec
	registryMu.Unlock()
	ResetReadiness(copySpec.ID)
	return nil
}

// ReplaceDynamicSpecs swaps the complete set of manifest-provided drive specs.
// Removing a manifest therefore removes its capability on the next refresh.
func ReplaceDynamicSpecs(next []Spec) error {
	clean := map[string]Spec{}
	for _, spec := range next {
		if !validSpecID(spec.ID) || len(spec.Command) == 0 {
			return errors.New("無效的 drive spec")
		}
		for _, builtin := range specs {
			if builtin.ID == spec.ID {
				return errors.New("不能覆寫內建 ACP 驅動")
			}
		}
		copySpec := spec
		copySpec.Command = append([]string(nil), spec.Command...)
		copySpec.Check = append([]string(nil), spec.Check...)
		copySpec.CheckFail = append([]string(nil), spec.CheckFail...)
		copySpec.Env = map[string]string{}
		for key, value := range spec.Env {
			copySpec.Env[key] = value
		}
		clean[copySpec.ID] = copySpec
	}
	registryMu.Lock()
	dynamicSpecs = clean
	registryMu.Unlock()
	readinessCache.Lock()
	for id := range readinessCache.bySpec {
		if _, builtin := BuiltinSpecFor(id); builtin {
			// Replacing the dynamic registry says nothing about a built-in
			// command's health. Keep the successful handshake cache so the
			// catalog does not flip back to "check failed, unknown reason"
			// immediately after the user removes a drive-only manifest.
			continue
		}
		if _, ok := clean[id]; !ok {
			delete(readinessCache.bySpec, id)
		}
	}
	readinessCache.Unlock()
	return nil
}

func validSpecID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for index, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func firstCommand(spec Spec) string {
	if len(spec.Command) == 0 {
		return "命令"
	}
	return spec.Command[0]
}

func truncateOneLine(value string, limit int) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
