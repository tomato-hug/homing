package adapter

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/agentconfig"
	"github.com/tomato-hug/homing/internal/api"
)

//go:embed catalogdata/*.yaml
var catalogYAMLFS embed.FS

// Readiness says how much of an agent Homing can actually use today. It is
// deliberately conservative: a generic event hook is not the same thing as a
// session scanner, and it must never be presented as one.
const (
	ReadinessBuiltin    = "builtin"    // compiled-in scanner
	ReadinessAdapter    = "adapter"    // verified YAML scanner, one-click install
	ReadinessHook       = "hook"       // event notifications only
	ReadinessUnverified = "unverified" // detected/popular, format not verified yet
)

type catalogSpec struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Vendor          string           `json:"vendor,omitempty"`
	Category        string           `json:"category,omitempty"`
	Readiness       string           `json:"readiness"`
	Icon            string           `json:"icon,omitempty"`
	DetectBins      []string         `json:"detect_bins,omitempty"`
	DetectPaths     []string         `json:"detect_paths,omitempty"`
	PushSignals     []string         `json:"push_signals,omitempty"`
	Sessions        bool             `json:"sessions,omitempty"`
	Reply           bool             `json:"reply,omitempty"`
	Models          bool             `json:"models,omitempty"`
	HookSupport     string           `json:"hook_support,omitempty"` // native | generic | none
	HookInstall     string           `json:"hook_install,omitempty"` // auto | snippet | manual | generic | none
	HookFile        string           `json:"hook_file,omitempty"`
	HookHint        string           `json:"hook_hint,omitempty"`
	Manifest        string           `json:"manifest,omitempty"` // embedded YAML filename under catalogdata/
	Notes           string           `json:"notes,omitempty"`
	Tagline         string           `json:"tagline,omitempty"`
	Homepage        string           `json:"homepage,omitempty"`
	DocsURL         string           `json:"docs_url,omitempty"`
	ConfigFile      string           `json:"config_file,omitempty"`
	Platforms       []string         `json:"platforms,omitempty"`
	CatalogSource   string           `json:"catalog_source,omitempty"` // homing | magpie | magpie-community
	SourceURL       string           `json:"source_url,omitempty"`
	SourcePackage   string           `json:"source_package,omitempty"`
	InstallCommands []CatalogCommand `json:"install_commands,omitempty"`
	// DownloadURL is the official download page for desktop apps that have no
	// command-line installer. The console opens it in the browser.
	DownloadURL string `json:"download_url,omitempty"`
}

// CatalogCommand is a way to install the agent itself. The console shows it
// and, after the user confirms, POST /api/agent-catalog/{id}/setup runs it on
// this computer (console only, never from a phone).
type CatalogCommand struct {
	Label   string `json:"label"`
	Command string `json:"command"`
	// Runnable is filled per request: the command's program (curl, brew,
	// npm…) exists on this computer, so the console can run it.
	Runnable bool `json:"runnable,omitempty"`
}

// CatalogIntegration describes the concrete ways Homing can connect to one
// agent. "ready" means the capability is already implemented, "available"
// means a generated kit can be used, and "draft" means the user still has to
// verify a local data format.
type CatalogIntegration struct {
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Description string `json:"description"`
	State       string `json:"state"`
	Path        string `json:"path,omitempty"`
	Command     string `json:"command,omitempty"`
}

// catalogSpecs is the product-facing agent list. Built-in scanners come first,
// then the verified manifests, then integrations that can only send events.
// Entries marked unverified are intentionally not installable.
var catalogSpecs = []catalogSpec{
	// ---- built-in scanners -------------------------------------------------
	{
		ID: "codex", Name: "Codex", Vendor: "OpenAI", Category: "CLI",
		Readiness: ReadinessBuiltin, Icon: "codex",
		DetectBins: []string{"codex"}, DetectPaths: []string{"~/.codex"},
		PushSignals: []string{"hook", "poll"}, Sessions: true, Reply: true, Models: true,
		HookSupport: "native", HookInstall: "snippet", HookFile: "~/.codex/hooks.json",
		Homepage: "https://github.com/openai/codex",
		Tagline:  "OpenAI 的终端编程 Agent；手机上可以直接发起和回复会话。",
		InstallCommands: []CatalogCommand{
			{Label: "安装脚本", Command: "curl -fsSL https://chatgpt.com/codex/install.sh | sh"},
			{Label: "Homebrew", Command: "brew install --cask codex"},
			{Label: "npm", Command: "npm install -g @openai/codex"},
		},
		Notes: "PermissionRequest 钩子需要你在 hooks.json 里手工添加，并在 Codex 里确认信任。",
	},
	{
		ID: "claude", Name: "Claude Code", Vendor: "Anthropic", Category: "CLI",
		Readiness: ReadinessBuiltin, Icon: "claude",
		DetectBins: []string{"claude"}, DetectPaths: []string{"~/.claude"},
		PushSignals: []string{"hook", "poll"}, Sessions: true, Reply: true, Models: true,
		HookSupport: "native", HookInstall: "auto", HookFile: "~/.claude/settings.json",
		Homepage: "https://docs.anthropic.com/en/docs/claude-code/overview",
		Tagline:  "Anthropic 的终端编程 Agent；钩子可一键安装，审批能推到手机。",
		InstallCommands: []CatalogCommand{
			{Label: "安装脚本", Command: "curl -fsSL https://claude.ai/install.sh | bash"},
			{Label: "npm", Command: "npm install -g @anthropic-ai/claude-code"},
		},
		Notes: "Stop / Notification / PermissionRequest 钩子可以在控制台一键安装。",
	},
	{
		ID: "workbuddy", Name: "WorkBuddy", Vendor: "WorkBuddy", Category: "桌面端",
		Readiness: ReadinessBuiltin, Icon: "workbuddy",
		DetectBins: []string{"workbuddy"}, DetectPaths: []string{"~/.workbuddy"},
		PushSignals: []string{"hook", "poll"}, Sessions: true, Reply: true, Models: true,
		HookSupport: "native", HookInstall: "auto", HookFile: "~/.workbuddy/settings.json",
		Homepage:    "https://www.codebuddy.cn/work/",
		DownloadURL: "https://www.codebuddy.cn/work/",
		Tagline:     "腾讯的桌面办公 Agent；装好桌面端后自动接入。",
		Notes:       "回复要经桌面端注入；电脑上没开 WorkBuddy 时只能收到事件。",
	},
	{
		ID: "kimi", Name: "Kimi Code", Vendor: "Moonshot AI", Category: "CLI",
		Readiness: ReadinessBuiltin, Icon: "kimi",
		DetectBins: []string{"kimi"}, DetectPaths: []string{"~/.kimi-code"},
		PushSignals: []string{"poll"}, Sessions: true, Reply: false, Models: false,
		HookSupport: "native", HookInstall: "manual", HookFile: "~/.kimi-code/config.toml",
		Homepage:   "https://www.kimi.com/zh-hans/resources/kimi-code-introduction",
		ConfigFile: "~/.kimi-code/config.toml",
		Tagline:    "Kimi 官方终端 Agent，本地会话可直接读取。",
		InstallCommands: []CatalogCommand{
			{Label: "安装脚本", Command: "curl -fsSL https://code.kimi.com/kimi-code/install.sh | bash"},
			{Label: "npm", Command: "npm install -g @moonshot-ai/kimi-code"},
		},
		Notes: "读取 ~/.kimi-code 的会话；config.toml 有生命周期钩子，目前由轮询读取，钩子要手工接。",
	},
	{
		ID: "opencode", Name: "OpenCode", Vendor: "SST", Category: "CLI",
		Readiness: ReadinessBuiltin, Icon: "opencode",
		DetectBins: []string{"opencode"}, DetectPaths: []string{"~/.local/share/opencode", "~/.config/opencode"},
		PushSignals: []string{"watch", "poll"}, Sessions: true, Reply: false, Models: true,
		HookSupport: "none", HookInstall: "none",
		Tagline: "终端与本地 serve 双形态；有 serve 时可直接对话。",
		InstallCommands: []CatalogCommand{
			{Label: "安装脚本", Command: "curl -fsSL https://opencode.ai/v2/install | bash"},
			{Label: "npm", Command: "npm install -g --allow-scripts=@opencode/cli @opencode/cli"},
		},
		Notes: "有本地 serve 时可以回复；没有 serve 时按文件轮询读取，只读。",
	},
	{
		ID: "zcode", Name: "ZCode", Vendor: "ZCode", Category: "CLI",
		Readiness: ReadinessBuiltin, Icon: "zcode",
		DetectBins: []string{"zcode"}, DetectPaths: []string{"~/.zcode"},
		PushSignals: []string{"poll"}, Sessions: true, Reply: false, Models: true,
		HookSupport: "none", HookInstall: "none",
		Notes: "读取 ZCode 的 SQLite 会话库，目前只读。",
	},

	// ---- verified manifests (one-click install) ----------------------------
	{
		ID: "dsh", Name: "DeepSeek Harness", Vendor: "DeepSeek", Category: "CLI",
		Readiness: ReadinessAdapter, Icon: "dsh", Manifest: "deepseek-harness.yaml",
		DetectBins: []string{"dsh"}, DetectPaths: []string{"~/.dsh"},
		PushSignals: []string{"poll"}, Sessions: true, Reply: false, Models: false,
		HookSupport: "generic", HookInstall: "generic",
		Homepage:   "https://github.com/deepseek-ai/deepseek-harness",
		ConfigFile: "~/.dsh/settings.yaml",
		Tagline:    "DeepSeek 官方 Harness；Homing 有专用 zstd JSONL 解析器。",
		InstallCommands: []CatalogCommand{
			{Label: "npm", Command: "npm install -g @deepseek-ai/dsh"},
			{Label: "直接运行", Command: "npx @deepseek-ai/dsh web"},
		},
		Notes: "专用解析器读取 zstd 压缩的 JSONL，跳过 runtime-context / skill-catalog 注入消息。",
	},
	{
		ID: "openclaw", Name: "OpenClaw", Vendor: "OpenClaw", Category: "CLI",
		Readiness: ReadinessAdapter, Icon: "openclaw", Manifest: "openclaw.yaml",
		DetectBins: []string{"openclaw"}, DetectPaths: []string{"~/.openclaw"},
		PushSignals: []string{"poll"}, Sessions: true, Reply: false, Models: false,
		HookSupport: "generic", HookInstall: "generic",
		Notes: "专用解析器读取 message.role / content 嵌套结构，thinking 块不会进正文。",
	},
	{
		ID: "codebuddy", Name: "CodeBuddy Code", Vendor: "腾讯", Category: "CLI",
		Readiness: ReadinessAdapter, Icon: "codebuddy", Manifest: "codebuddy.yaml",
		DetectBins: []string{"codebuddy", "cbc", "codebuddy-code"}, DetectPaths: []string{"~/.codebuddy"},
		PushSignals: []string{"poll"}, Sessions: true, Reply: false, Models: true,
		HookSupport: "generic", HookInstall: "generic",
		InstallCommands: []CatalogCommand{
			{Label: "npm", Command: "npm install -g @tencent-ai/codebuddy-code"},
		},
		Notes: "与 Claude Code 同族的 JSONL；本机没有 codebuddy CLI，所以没有启用回复命令。",
	},
	{
		ID: "gemini-cli", Name: "Gemini CLI", Vendor: "Google", Category: "CLI",
		Readiness: ReadinessAdapter, Icon: "gemini", Manifest: "gemini-cli.yaml",
		DetectBins: []string{"gemini"}, DetectPaths: []string{"~/.gemini"},
		PushSignals: []string{"poll"}, Sessions: true, Reply: false, Models: false,
		HookSupport: "generic", HookInstall: "generic",
		InstallCommands: []CatalogCommand{
			{Label: "Homebrew", Command: "brew install gemini-cli"},
			{Label: "npm", Command: "npm install -g @google/gemini-cli"},
		},
		Notes: "一个会话一个 JSON；本机目录存在但还没有跑过会话时，读出来会是 0 条。",
	},
	{
		ID: "hermes", Name: "Hermes Agent", Vendor: "Nous Research", Category: "CLI",
		Readiness: ReadinessAdapter, Icon: "hermes", Manifest: "hermes.yaml",
		DetectBins: []string{"hermes"}, DetectPaths: []string{"~/.hermes"},
		PushSignals: []string{"poll"}, Sessions: true, Reply: false, Models: false,
		HookSupport: "generic", HookInstall: "generic",
		Notes: "读 SQLite 的 sessions 表；库里没有 title / cwd 列时标题回落到会话 id。",
	},

	// ---- event-hook integrations ------------------------------------------
	{
		ID: "cursor", Name: "Cursor", Vendor: "Anysphere", Category: "IDE",
		DownloadURL: "https://cursor.com/download",
		Readiness:   ReadinessHook, Icon: "cursor",
		DetectBins:  []string{"cursor-agent"},
		DetectPaths: []string{"~/.cursor", "~/Library/Application Support/Cursor", "/Applications/Cursor.app"},
		PushSignals: []string{"hook"}, HookSupport: "native", HookInstall: "manual",
		HookFile: "~/.cursor/hooks.json",
		Notes:    "官方 hooks.json 可以执行命令；把 JSON 转给 /hook/generic 就能进收件箱。会话库尚未解析。",
	},
	{
		ID: "goose", Name: "Goose", Vendor: "Block", Category: "CLI",
		Readiness: ReadinessHook, Icon: "goose",
		DetectBins: []string{"goose"}, DetectPaths: []string{"~/.config/goose", "~/Library/Application Support/Block/goose"},
		PushSignals: []string{"hook"}, HookSupport: "native", HookInstall: "manual",
		Notes: "有插件钩子，可以把事件转给 Homing；会话读取还需要核实 Goose 的本地记录格式。",
	},
	{
		ID: "zed", Name: "Zed", Vendor: "Zed Industries", Category: "编辑器",
		DownloadURL: "https://zed.dev/download",
		Readiness:   ReadinessHook, Icon: "zed",
		DetectBins: []string{"zed"}, DetectPaths: []string{"~/.config/zed", "~/Library/Application Support/Zed", "/Applications/Zed.app"},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		Notes: "可先用通用钩子收事件，或用自定义清单接它的 assistant 记录；原生会话格式尚未核实。",
	},
	{
		ID: "aider", Name: "Aider", Vendor: "Aider", Category: "CLI",
		Readiness: ReadinessHook, Icon: "aider",
		DetectBins:  []string{"aider"},
		DetectPaths: []string{"~/.aider.conf.yml"},
		PushSignals: []string{"hook"}, HookSupport: "native", HookInstall: "manual",
		HookFile:   "~/.aider.conf.yml",
		HookHint:   "Aider 自带 --notifications-command。把接入套件里的 hook.sh 完整路径写进 notifications-command，并开启 notifications: true；Aider 生成回复后会调用一次。",
		ConfigFile: "~/.aider.conf.yml",
		Homepage:   "https://aider.chat",
		Notes:      "原生通知钩子可送完成事件；.aider.chat.history.md 的会话解析器尚未实现，先不冒充可读会话。",
	},
	{
		ID: "continue", Name: "Continue", Vendor: "Continue", Category: "IDE / CLI",
		Readiness: ReadinessHook, Icon: "continue",
		DetectBins: []string{"cn"}, DetectPaths: []string{"~/.continue"},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		Notes: "~/.continue/sessions 可先用自定义清单；事件可以用通用钩子送进来。",
	},
	{
		ID: "qwen-code", Name: "Qwen Code", Vendor: "阿里云", Category: "CLI",
		Readiness: ReadinessHook, Icon: "qwen",
		DetectBins: []string{"qwen"}, DetectPaths: []string{"~/.qwen"},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		Notes: "Gemini CLI 同族的 CLI；会话格式与钩子事件名还没逐项核实，先走通用钩子。",
	},
	{
		ID: "cline", Name: "Cline", Vendor: "Cline", Category: "VS Code 扩展",
		Readiness: ReadinessHook, Icon: "cline",
		DetectBins: []string{"cline"},
		DetectPaths: []string{
			"~/Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev",
			"~/.config/Code/User/globalStorage/saoudrizwan.claude-dev",
		},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		InstallCommands: []CatalogCommand{
			{Label: "npm", Command: "npm install -g --allow-scripts=cline cline"},
		},
		Notes: "扩展内的任务记录还没核实；能执行命令的版本可把事件转给通用钩子。",
	},
	{
		ID: "roo-code", Name: "Roo Code", Vendor: "Roo", Category: "VS Code 扩展",
		Readiness: ReadinessHook, Icon: "roo",
		DetectPaths: []string{
			"~/Library/Application Support/Code/User/globalStorage/rooveterinaryinc.roo-cline",
			"~/.config/Code/User/globalStorage/rooveterinaryinc.roo-cline",
		},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		Notes: "与 Cline 同源的扩展；会话记录格式未核实，先只做事件通知。",
	},
	{
		ID: "crush", Name: "Crush", Vendor: "Charm", Category: "CLI",
		Readiness: ReadinessHook, Icon: "crush",
		DetectBins: []string{"crush"}, DetectPaths: []string{"~/.config/crush", "~/.local/share/crush"},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		InstallCommands: []CatalogCommand{
			{Label: "npm", Command: "npm install -g --allow-scripts=@charmland/crush @charmland/crush"},
		},
		Notes: "可以包一层启动脚本，把完成/报错事件转给通用钩子；会话文件格式待核实。",
	},
	{
		ID: "amp", Name: "Amp", Vendor: "Sourcegraph", Category: "CLI",
		Readiness: ReadinessHook, Icon: "amp",
		DetectBins: []string{"amp"}, DetectPaths: []string{"~/.amp"},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		Notes: "先通过通用钩子接收事件；Amp 的本地会话格式没有在本机核实。",
	},
	{
		ID: "droid", Name: "Droid", Vendor: "Factory", Category: "CLI",
		Readiness: ReadinessHook, Icon: "droid",
		DetectBins: []string{"droid"}, DetectPaths: []string{"~/.factory"},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		Notes: "CLI 可以把结束事件转给 Homing；会话记录格式尚未核实。",
	},
	{
		ID: "copilot-cli", Name: "GitHub Copilot CLI", Vendor: "GitHub", Category: "CLI",
		Readiness: ReadinessHook, Icon: "copilot",
		DetectBins: []string{"copilot"}, DetectPaths: []string{"~/.copilot"},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		InstallCommands: []CatalogCommand{
			{Label: "npm", Command: "npm install -g @github/copilot"},
		},
		Notes: "可以包启动脚本发送事件；Copilot CLI 的会话格式还没核实，不冒充已支持读取。",
	},
	{
		ID: "openhands", Name: "OpenHands", Vendor: "All Hands AI", Category: "CLI",
		Readiness: ReadinessHook, Icon: "openhands",
		DetectBins: []string{"openhands"}, DetectPaths: []string{"~/.openhands"},
		PushSignals: []string{"hook"}, HookSupport: "generic", HookInstall: "generic",
		Notes: "可先用通用钩子或 /api/ingest 收事件；本地会话读取尚未核实。",
	},

	// ---- not verified yet --------------------------------------------------
	{
		ID: "antigravity", Name: "Google Antigravity", Vendor: "Google", Category: "IDE",
		DownloadURL: "https://antigravity.google/download",
		Readiness:   ReadinessUnverified, Icon: "antigravity",
		DetectPaths: []string{"~/.gemini/antigravity", "/Applications/Antigravity.app"},
		HookSupport: "none", HookInstall: "none",
		Notes: "Go 已支持逐会话分库 SQLite（db_glob），但表结构没有样本可核对，所以还没写现成清单。",
	},
	{
		ID: "warp", Name: "Warp", Vendor: "Warp", Category: "终端",
		DownloadURL: "https://www.warp.dev/download",
		Readiness:   ReadinessUnverified, Icon: "warp",
		DetectPaths: []string{"~/.warp", "/Applications/Warp.app"},
		HookSupport: "none", HookInstall: "none",
		Notes: "目前没有核实到可供外部读取的 agent 会话格式；先不提供接入，避免给出假承诺。",
	},
	{
		ID: "trae", Name: "Trae", Vendor: "字节跳动", Category: "IDE",
		Readiness: ReadinessUnverified, Icon: "trae",
		DetectBins:  []string{"traecli"},
		DetectPaths: []string{"~/.trae", "/Applications/Trae.app"},
		HookSupport: "none", HookInstall: "none",
		Notes: "IDE 的本地会话格式未核实；如果要接，建议先提交一份脱敏样本或脚本适配器。",
	},
	{
		ID: "qoder", Name: "Qoder", Vendor: "阿里云", Category: "IDE",
		Readiness: ReadinessUnverified, Icon: "qoder",
		DetectBins:  []string{"qodercli"},
		DetectPaths: []string{"~/.qoder", "/Applications/Qoder.app"},
		HookSupport: "none", HookInstall: "none",
		Notes: "还没有核实它的会话位置与格式，暂不提供一键接入。",
	},
	{
		ID: "windsurf", Name: "Windsurf", Vendor: "Codeium", Category: "IDE",
		DownloadURL: "https://windsurf.com/download",
		Readiness:   ReadinessUnverified, Icon: "windsurf",
		DetectPaths: []string{"~/.codeium/windsurf", "/Applications/Windsurf.app"},
		HookSupport: "none", HookInstall: "none",
		Notes: "会话库格式未核实；可以用自定义清单或脚本适配器先试读。",
	},
	{
		ID: "kiro", Name: "Kiro", Vendor: "AWS", Category: "IDE",
		Readiness: ReadinessUnverified, Icon: "kiro",
		DetectBins:  []string{"kiro-cli-chat", "kiro-cli", "kiro"},
		DetectPaths: []string{"~/.kiro", "/Applications/Kiro.app"},
		HookSupport: "none", HookInstall: "none",
		Notes: "还没有核实本地会话格式，暂不提供接入。",
	},
}

// CatalogEntry is one row of GET /api/agent-catalog. Fields describe what is
// true on this machine right now, not only what the catalog knows about.
type CatalogEntry struct {
	ID                string           `json:"id"`
	Name              string           `json:"name"`
	Vendor            string           `json:"vendor,omitempty"`
	Category          string           `json:"category,omitempty"`
	Icon              string           `json:"icon,omitempty"`
	Readiness         string           `json:"readiness"`
	Detected          bool             `json:"detected"`
	Connected         bool             `json:"connected"`
	Available         bool             `json:"available"`
	ManifestInstalled bool             `json:"manifest_installed"`
	CanInstall        bool             `json:"can_install"`
	CanConnect        bool             `json:"can_connect"`
	CanGenerateKit    bool             `json:"can_generate_kit"`
	InstallKind       string           `json:"install_kind"`
	Sessions          bool             `json:"sessions"`
	Reply             bool             `json:"reply"`
	Models            bool             `json:"models"`
	PushSignals       []string         `json:"push_signals"`
	HookSupport       string           `json:"hook_support"`
	HookInstall       string           `json:"hook_install"`
	HookInstalled     bool             `json:"hook_installed"`
	HookFile          string           `json:"hook_file,omitempty"`
	HookHint          string           `json:"hook_hint,omitempty"`
	DetectBins        []string         `json:"detect_bins,omitempty"`
	DetectPaths       []string         `json:"detect_paths,omitempty"`
	Version           *string          `json:"version"`
	ManifestFile      string           `json:"manifest_file,omitempty"`
	Notes             string           `json:"notes,omitempty"`
	Tagline           string           `json:"tagline,omitempty"`
	Homepage          string           `json:"homepage,omitempty"`
	DocsURL           string           `json:"docs_url,omitempty"`
	ConfigFile        string           `json:"config_file,omitempty"`
	Platforms         []string         `json:"platforms,omitempty"`
	CatalogSource     string           `json:"catalog_source"`
	SourceURL         string           `json:"source_url,omitempty"`
	SourcePackage     string           `json:"source_package,omitempty"`
	InstallCommands   []CatalogCommand `json:"install_commands,omitempty"`
	DownloadURL       string           `json:"download_url,omitempty"`
	// CanSetup is true when the console can install the agent itself on this
	// computer: it is not detected yet and has a command for this platform.
	CanSetup     bool                 `json:"can_setup"`
	Integrations []CatalogIntegration `json:"integrations"`
	KitInstalled bool                 `json:"kit_installed"`
	KitDir       string               `json:"kit_dir,omitempty"`
	Drive        *CatalogDrive        `json:"drive,omitempty"`
}

// CatalogPayload is the GET /api/agent-catalog response.
type CatalogPayload struct {
	Agents      []CatalogEntry `json:"agents"`
	Summary     CatalogSummary `json:"summary"`
	GeneratedAt string         `json:"generated_at"`
}

type CatalogSummary struct {
	Total       int `json:"total"`
	Connected   int `json:"connected"`
	Detected    int `json:"detected"`
	Installable int `json:"installable"`
	HookOnly    int `json:"hook_only"`
	Builtin     int `json:"builtin"`
}

// BuildCatalog merges the static catalog with live scanners and per-agent
// settings. It is exported for tests and for other local front ends.
func BuildCatalog(ctx context.Context, store *agentconfig.Store) CatalogPayload {
	live := agent.All(ctx)
	liveByID := make(map[string]agent.Agent, len(live))
	for _, item := range live {
		liveByID[item.ID()] = item
	}

	ids := make([]string, 0, len(catalogSpecs)+len(live))
	seen := make(map[string]bool, len(catalogSpecs)+len(live))
	for _, spec := range catalogSpecs {
		ids = append(ids, spec.ID)
		seen[spec.ID] = true
	}
	for _, item := range live {
		if !seen[item.ID()] {
			ids = append(ids, item.ID())
			seen[item.ID()] = true
		}
	}
	settings := store.Snapshot(ids)

	entries := make([]CatalogEntry, 0, len(ids))
	for _, spec := range catalogSpecs {
		entries = append(entries, catalogEntryFor(ctx, spec, liveByID[spec.ID], settings[spec.ID]))
	}

	// Manifests and script adapters the user wrote themselves still belong in
	// the list. They are shown as adapter integrations, without an install
	// button (the file is already there).
	for _, item := range live {
		if _, known := catalogSpecByID(item.ID()); known {
			continue
		}
		det := item.Detect(ctx)
		caps := item.Capabilities()
		entries = append(entries, CatalogEntry{
			ID:                item.ID(),
			Name:              item.Name(),
			Vendor:            "自定义",
			Category:          "自定义",
			Icon:              item.Icon(),
			Readiness:         ReadinessAdapter,
			Detected:          det.Installed,
			Connected:         settings[item.ID()].Connected,
			Available:         true,
			ManifestInstalled: true,
			CanInstall:        false,
			CanConnect:        true,
			InstallKind:       "manifest",
			Sessions:          caps.Sessions,
			Reply:             caps.Reply,
			Models:            caps.Models,
			PushSignals:       []string{string(item.PushSignal())},
			HookSupport:       "generic",
			HookInstall:       "generic",
			Version:           det.Version,
			Notes:             "通过你自己的 YAML 清单或脚本适配器接入。",
		})
	}

	payload := CatalogPayload{Agents: entries, GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, entry := range entries {
		payload.Summary.Total++
		if entry.Connected {
			payload.Summary.Connected++
		} else if entry.Detected {
			payload.Summary.Detected++
		}
		if entry.CanInstall {
			payload.Summary.Installable++
		}
		if entry.Readiness == ReadinessHook || entry.Readiness == ReadinessUnverified {
			payload.Summary.HookOnly++
		}
		if entry.Readiness == ReadinessBuiltin {
			payload.Summary.Builtin++
		}
	}
	return payload
}

func catalogEntryFor(ctx context.Context, spec catalogSpec, live agent.Agent, settings agentconfig.Settings) CatalogEntry {
	source := spec.CatalogSource
	if source == "" {
		source = "homing"
	}
	entry := CatalogEntry{
		ID:              spec.ID,
		Name:            spec.Name,
		Vendor:          spec.Vendor,
		Category:        spec.Category,
		Icon:            spec.Icon,
		Readiness:       spec.Readiness,
		CanInstall:      spec.Readiness == ReadinessAdapter,
		CanGenerateKit:  true,
		InstallKind:     "none",
		Sessions:        spec.Sessions,
		Reply:           spec.Reply,
		Models:          spec.Models,
		PushSignals:     append([]string{}, spec.PushSignals...),
		HookSupport:     spec.HookSupport,
		HookInstall:     spec.HookInstall,
		HookFile:        spec.HookFile,
		HookHint:        spec.HookHint,
		DetectBins:      append([]string(nil), spec.DetectBins...),
		DetectPaths:     append([]string(nil), spec.DetectPaths...),
		Notes:           spec.Notes,
		Tagline:         spec.Tagline,
		Homepage:        spec.Homepage,
		DocsURL:         spec.DocsURL,
		ConfigFile:      spec.ConfigFile,
		Platforms:       append([]string(nil), spec.Platforms...),
		CatalogSource:   source,
		SourceURL:       spec.SourceURL,
		SourcePackage:   spec.SourcePackage,
		InstallCommands: append([]CatalogCommand(nil), spec.InstallCommands...),
		DownloadURL:     spec.DownloadURL,
	}
	switch spec.Readiness {
	case ReadinessBuiltin:
		entry.InstallKind = "builtin"
	case ReadinessAdapter:
		entry.InstallKind = "manifest"
		if path := manifestPathFor(spec.ID); path != "" {
			entry.ManifestInstalled = true
			entry.ManifestFile = tildifyPath(path)
		}
	case ReadinessHook:
		entry.InstallKind = "hook"
	}

	detected := false
	if live != nil {
		det := live.Detect(ctx)
		detected = det.Installed
		entry.Version = det.Version
		entry.Available = true
		caps := live.Capabilities()
		entry.Sessions = caps.Sessions
		entry.Reply = caps.Reply
		entry.Models = caps.Models
		if entry.PushSignals == nil {
			entry.PushSignals = []string{string(live.PushSignal())}
		}
	} else if spec.Readiness == ReadinessAdapter && entry.ManifestInstalled {
		// The YAML is on disk but the loader has not picked it up yet (for
		// example immediately after installing it). Detection still comes from
		// the paths declared in the catalog.
		detected = detectCatalogSpec(spec)
	}
	if !detected {
		detected = detectCatalogSpec(spec)
	}

	entry.Detected = detected
	entry.Connected = agentconfig.EffectiveConnected(settings, spec.Readiness == ReadinessBuiltin, detected)
	entry.Available = entry.Available || entry.ManifestInstalled
	entry.CanConnect = entry.Available && detected
	runnable := setupCommandsFor(spec)
	for index := range entry.InstallCommands {
		for _, candidate := range runnable {
			if candidate.Command == entry.InstallCommands[index].Command {
				entry.InstallCommands[index].Runnable = true
			}
		}
	}
	entry.CanSetup = !detected && len(runnable) > 0
	if entry.HookFile != "" {
		entry.HookFile = tildifyPath(expandHome(entry.HookFile))
	}
	if entry.ConfigFile != "" {
		entry.ConfigFile = tildifyPath(expandHome(entry.ConfigFile))
	}
	if spec.Readiness == ReadinessBuiltin || spec.Readiness == ReadinessAdapter {
		entry.HookInstalled = entry.Connected && storeHookInstalled(spec.ID)
	}
	entry.KitInstalled, entry.KitDir = catalogKitState(spec.ID)
	entry.CanConnect = entry.CanConnect || entry.KitInstalled
	var manifest *Manifest
	if manifestAgent, ok := live.(*ManifestAgent); ok {
		manifest = manifestAgent.Manifest()
	}
	entry.Drive = catalogDriveFor(spec.ID, manifest)
	if entry.CanGenerateKit && !containsString(entry.PushSignals, "hook") {
		entry.PushSignals = append(entry.PushSignals, "hook")
	}
	entry.Integrations = catalogIntegrationsFor(entry)
	return entry
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// storeHookInstalled avoids constructing another Store just to read a file.
// The agentconfig package owns the path rules; a Store with no data dir is
// enough because ConfigFile only uses Home.
func storeHookInstalled(id string) bool {
	return agentconfig.NewStore("").HookInstalled(id)
}

func detectCatalogSpec(spec catalogSpec) bool {
	for _, path := range spec.DetectPaths {
		if stat, err := os.Stat(expandHome(path)); err == nil && stat != nil {
			return true
		}
	}
	for _, bin := range spec.DetectBins {
		if scanlib.ResolveBinary(bin) != "" {
			return true
		}
	}
	return false
}

func catalogSpecByID(id string) (catalogSpec, bool) {
	for _, spec := range catalogSpecs {
		if spec.ID == id {
			return spec, true
		}
	}
	return catalogSpec{}, false
}

func manifestPathFor(id string) string {
	for _, ext := range []string{".yaml", ".yml"} {
		path := filepath.Join(ManifestDir(), id+ext)
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path
		}
	}
	return ""
}

var (
	ErrCatalogNotFound        = errors.New("catalog: agent not found")
	ErrCatalogNotInstallable  = errors.New("catalog: agent is not installable")
	ErrCatalogNotInstalled    = errors.New("catalog: manifest is not installed")
	ErrCatalogKitNotInstalled = errors.New("catalog: integration kit is not installed")
)

// CatalogInstallResult is returned by the install endpoint.
type CatalogInstallResult struct {
	OK       bool                 `json:"ok"`
	ID       string               `json:"id"`
	File     string               `json:"file"`
	Backup   string               `json:"backup,omitempty"`
	Settings agentconfig.Settings `json:"settings"`
}

// CatalogRemoveResult is returned by the remove endpoint.
type CatalogRemoveResult struct {
	OK       bool                 `json:"ok"`
	ID       string               `json:"id"`
	Removed  []map[string]string  `json:"removed"`
	Settings agentconfig.Settings `json:"settings"`
}

// CatalogQuickResult is returned by the one-click setup endpoint. It records
// which parts were written without exposing or executing the agent's own
// install commands.
type CatalogQuickResult struct {
	OK                bool                 `json:"ok"`
	ID                string               `json:"id"`
	ManifestInstalled bool                 `json:"manifest_installed"`
	ManifestFile      string               `json:"manifest_file,omitempty"`
	KitInstalled      bool                 `json:"kit_installed"`
	KitExisting       bool                 `json:"kit_existing"`
	KitDir            string               `json:"kit_dir,omitempty"`
	Hook              string               `json:"hook,omitempty"`
	Wrapper           string               `json:"wrapper,omitempty"`
	Settings          agentconfig.Settings `json:"settings"`
}

// CatalogKitRemoveResult is returned after a kit directory is moved aside.
// Kits can contain user edits, so removal never recursively deletes them.
type CatalogKitRemoveResult struct {
	OK    bool   `json:"ok"`
	ID    string `json:"id"`
	Dir   string `json:"dir"`
	Trash string `json:"trash"`
}

// InstallCatalogAgent writes the embedded, verified manifest to
// ~/.homing/agents and turns the agent on in Homing's own settings. It never
// touches the agent's configuration.
func InstallCatalogAgent(id, dataDir string) (CatalogInstallResult, error) {
	spec, ok := catalogSpecByID(id)
	if !ok {
		return CatalogInstallResult{}, ErrCatalogNotFound
	}
	if spec.Readiness != ReadinessAdapter || spec.Manifest == "" {
		return CatalogInstallResult{}, ErrCatalogNotInstallable
	}
	raw, err := catalogYAMLFS.ReadFile("catalogdata/" + spec.Manifest)
	if err != nil {
		return CatalogInstallResult{}, fmt.Errorf("读取内置清单失败: %w", err)
	}
	manifest, problems, err := LoadManifestFromString(string(raw))
	if err != nil {
		return CatalogInstallResult{}, err
	}
	if len(problems) > 0 {
		return CatalogInstallResult{}, fmt.Errorf("内置清单校验未通过: %s", joinProblems(problems))
	}
	if manifest.ID != spec.ID {
		return CatalogInstallResult{}, fmt.Errorf("内置清单 id 不匹配: %s != %s", manifest.ID, spec.ID)
	}

	dir := ManifestDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CatalogInstallResult{}, err
	}
	file := filepath.Join(dir, spec.ID+".yaml")
	backup := ""
	if existing, err := os.ReadFile(file); err == nil {
		backup = fmt.Sprintf("%s.bak-%d", file, time.Now().Unix())
		if err := os.WriteFile(backup, existing, 0o600); err != nil {
			return CatalogInstallResult{}, fmt.Errorf("备份旧清单失败: %w", err)
		}
	}
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		return CatalogInstallResult{}, fmt.Errorf("写入清单失败: %w", err)
	}
	invalidate()

	next, err := agentconfig.NewStore(dataDir).Patch(spec.ID, map[string]any{
		"connected":     true,
		"push_to_phone": true,
	})
	if err != nil {
		return CatalogInstallResult{OK: true, ID: spec.ID, File: file, Backup: backup}, fmt.Errorf("清单已写入，但保存接入设置失败: %w", err)
	}
	return CatalogInstallResult{OK: true, ID: spec.ID, File: file, Backup: backup, Settings: next}, nil
}

// RemoveCatalogAgent renames the installed manifest aside (never deletes it)
// and turns the agent off in Homing's own settings.
func RemoveCatalogAgent(id, dataDir string) (CatalogRemoveResult, error) {
	spec, ok := catalogSpecByID(id)
	if !ok {
		return CatalogRemoveResult{}, ErrCatalogNotFound
	}
	if spec.Readiness != ReadinessAdapter {
		return CatalogRemoveResult{}, ErrCatalogNotInstallable
	}
	var removed []map[string]string
	for _, name := range []string{spec.ID + ".yaml", spec.ID + ".yml"} {
		file := filepath.Join(ManifestDir(), name)
		if _, err := os.Stat(file); err != nil {
			continue
		}
		trash := fmt.Sprintf("%s.deleted-%d", file, time.Now().Unix())
		if err := os.Rename(file, trash); err == nil {
			removed = append(removed, map[string]string{"file": file, "trash": trash})
		}
	}
	if len(removed) == 0 {
		return CatalogRemoveResult{}, ErrCatalogNotInstalled
	}
	invalidate()
	next, err := agentconfig.NewStore(dataDir).Patch(spec.ID, map[string]any{
		"connected":     false,
		"push_to_phone": false,
	})
	if err != nil {
		return CatalogRemoveResult{OK: true, ID: spec.ID, Removed: removed}, fmt.Errorf("清单已移走，但保存接入设置失败: %w", err)
	}
	return CatalogRemoveResult{OK: true, ID: spec.ID, Removed: removed, Settings: next}, nil
}

// QuickConnectCatalogAgent prepares the strongest integration available for
// one catalog entry. Verified adapters get their Homing-owned manifest; every
// entry gets an integration kit if it does not already have one; finally the
// entry is enabled in Homing's own settings.
//
// This never runs a third-party install command and never edits the agent's
// config. Those commands remain copyable instructions in the detail panel.
func QuickConnectCatalogAgent(id, dataDir string) (CatalogQuickResult, error) {
	spec, ok := catalogSpecByID(id)
	if !ok {
		return CatalogQuickResult{}, ErrCatalogNotFound
	}
	result := CatalogQuickResult{OK: true, ID: spec.ID}

	if spec.Readiness == ReadinessAdapter && manifestPathFor(spec.ID) == "" {
		installed, err := InstallCatalogAgent(spec.ID, dataDir)
		if err != nil {
			return CatalogQuickResult{}, err
		}
		result.ManifestInstalled = installed.OK
		result.ManifestFile = installed.File
	}

	kit, err := InstallCatalogKit(spec.ID)
	if err != nil {
		return CatalogQuickResult{}, err
	}
	result.KitInstalled = kit.OK
	result.KitExisting = kit.Existing
	result.KitDir = kit.Dir
	result.Hook = kit.Hook
	result.Wrapper = kit.Wrapper

	settings, err := agentconfig.NewStore(dataDir).Patch(spec.ID, map[string]any{
		"connected":     true,
		"push_to_phone": true,
	})
	if err != nil {
		return CatalogQuickResult{}, fmt.Errorf("接入套件已准备，但保存接入设置失败: %w", err)
	}
	result.Settings = settings
	return result, nil
}

// RemoveCatalogKit moves a generated kit aside. The files remain recoverable
// beside the original directory and can be inspected with "trash".
func RemoveCatalogKit(id string) (CatalogKitRemoveResult, error) {
	if !safeKitID(id) {
		return CatalogKitRemoveResult{}, ErrCatalogNotFound
	}
	if _, ok := catalogSpecByID(id); !ok {
		return CatalogKitRemoveResult{}, ErrCatalogNotFound
	}
	dir := CatalogKitDir(id)
	stat, err := os.Stat(dir)
	if err != nil || !stat.IsDir() {
		return CatalogKitRemoveResult{}, ErrCatalogKitNotInstalled
	}
	trash := fmt.Sprintf("%s.deleted-%d", dir, time.Now().UnixNano())
	if err := os.Rename(dir, trash); err != nil {
		return CatalogKitRemoveResult{}, fmt.Errorf("移除接入套件失败: %w", err)
	}
	return CatalogKitRemoveResult{OK: true, ID: id, Dir: dir, Trash: trash}, nil
}

func joinProblems(problems []string) string {
	out := ""
	for i, problem := range problems {
		if i > 0 {
			out += "；"
		}
		out += problem
	}
	return out
}

// CatalogManifestYAML returns the embedded YAML for an adapter entry. Tests use
// it to keep the catalog copy in sync with adapters/.
func CatalogManifestYAML(id string) ([]byte, bool) {
	spec, ok := catalogSpecByID(id)
	if !ok || spec.Manifest == "" {
		return nil, false
	}
	raw, err := catalogYAMLFS.ReadFile("catalogdata/" + spec.Manifest)
	return raw, err == nil
}

// mountCatalogRoutes is called from mount() in routes.go.
func mountCatalogRoutes(mux *http.ServeMux, d *api.Deps) {
	mux.HandleFunc("GET /api/agent-catalog", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		api.WriteJSON(w, http.StatusOK, BuildCatalog(r.Context(), agentconfig.NewStore(d.DataDir)))
	})

	mux.HandleFunc("POST /api/agent-catalog/{id}/install", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		id := r.PathValue("id")
		if !validID(id) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
			return
		}
		result, err := InstallCatalogAgent(id, d.DataDir)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, ErrCatalogNotFound):
				status = http.StatusNotFound
			case errors.Is(err, ErrCatalogNotInstallable):
				status = http.StatusBadRequest
			}
			api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/agent-catalog/{id}/kit", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		id := r.PathValue("id")
		if !validID(id) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
			return
		}
		result, err := InstallCatalogKit(id)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrCatalogNotFound) {
				status = http.StatusNotFound
			}
			api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/agent-catalog/{id}/quick", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		id := r.PathValue("id")
		if !validID(id) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
			return
		}
		result, err := QuickConnectCatalogAgent(id, d.DataDir)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrCatalogNotFound) {
				status = http.StatusNotFound
			}
			api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("POST /api/agent-catalog/{id}/drive", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		id := r.PathValue("id")
		if !validID(id) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
			return
		}
		result, err := EnableCatalogDrive(r.Context(), id, d.DataDir)
		if err != nil {
			status := http.StatusBadGateway
			switch {
			case errors.Is(err, ErrCatalogNotFound):
				status = http.StatusNotFound
			case errors.Is(err, ErrDriveUnsupported):
				status = http.StatusBadRequest
			case errors.Is(err, ErrDriveNotInstalled):
				status = http.StatusConflict
			}
			api.WriteJSON(w, status, map[string]any{
				"ok": false, "error": err.Error(), "drive": result.Drive,
			})
			return
		}
		api.WriteJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("DELETE /api/agent-catalog/{id}/drive", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		id := r.PathValue("id")
		if !validID(id) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
			return
		}
		result, err := RemoveCatalogDrive(id, d.DataDir)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, ErrCatalogNotFound):
				status = http.StatusNotFound
			case errors.Is(err, ErrDriveNotEnabled):
				status = http.StatusConflict
			}
			api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("DELETE /api/agent-catalog/{id}/kit", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		id := r.PathValue("id")
		if !validID(id) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
			return
		}
		result, err := RemoveCatalogKit(id)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, ErrCatalogNotFound):
				status = http.StatusNotFound
			case errors.Is(err, ErrCatalogKitNotInstalled):
				status = http.StatusNotFound
			}
			api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("DELETE /api/agent-catalog/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		id := r.PathValue("id")
		if !validID(id) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
			return
		}
		result, err := RemoveCatalogAgent(id, d.DataDir)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, ErrCatalogNotFound):
				status = http.StatusNotFound
			case errors.Is(err, ErrCatalogNotInstallable):
				status = http.StatusBadRequest
			case errors.Is(err, ErrCatalogNotInstalled):
				status = http.StatusNotFound
			}
			api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		api.WriteJSON(w, http.StatusOK, result)
	})
}
