package adapter

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
)

//go:embed catalogdata/kit/*
var catalogKitFS embed.FS

// CatalogKitResult lists the files a user can point an agent at. Installing a
// kit never edits that agent's own config and never adds an unverified scanner
// to ~/.homing/agents: the manifest and adapter are deliberately .draft files.
type CatalogKitResult struct {
	OK            bool     `json:"ok"`
	ID            string   `json:"id"`
	Dir           string   `json:"dir"`
	Hook          string   `json:"hook"`
	Wrapper       string   `json:"wrapper"`
	ManifestDraft string   `json:"manifest_draft"`
	AdapterDraft  string   `json:"adapter_draft"`
	Readme        string   `json:"readme"`
	NativeFiles   []string `json:"native_files,omitempty"`
	Existing      bool     `json:"existing"`
}

type kitTemplateData struct {
	ID          string
	Name        string
	Category    string
	ConfigFile  string
	DetectPath  string
	HookHint    string
	Homepage    string
	SourceURL   string
	SourceLabel string
}

// CatalogKitDir is the per-agent integration kit folder. Kits are kept apart
// from manifests and script adapters so an unfinished draft can never be
// mistaken for a working scanner on the next refresh.
func CatalogKitDir(id string) string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "Homing", "integrations", id)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".homing", "integrations", id)
}

func catalogKitState(id string) (bool, string) {
	dir := CatalogKitDir(id)
	stat, err := os.Stat(dir)
	installed := err == nil && stat.IsDir()
	if !installed {
		return false, ""
	}
	return true, tildifyPath(dir)
}

func catalogIntegrationsFor(entry CatalogEntry) []CatalogIntegration {
	out := make([]CatalogIntegration, 0, 6)
	if entry.Sessions {
		switch entry.Readiness {
		case ReadinessBuiltin:
			out = append(out, CatalogIntegration{
				Kind: "scanner", Label: "内置扫描", State: "ready",
				Description: "Homing 直接读取本机会话，可同步到手机收件箱。",
			})
		case ReadinessAdapter:
			state := "available"
			if entry.ManifestInstalled {
				state = "ready"
			}
			out = append(out, CatalogIntegration{
				Kind: "manifest", Label: "已验证清单", State: state,
				Description: "写入 Homing 自己的 ~/.homing/agents 清单，不改 agent 配置。",
				Path:        entry.ManifestFile,
			})
		}
	}

	if entry.HookSupport != "" && entry.HookSupport != "none" {
		label := "通用事件钩子"
		description := "接入套件提供 hook.mjs / hook.sh，把完成、出错和待确认事件送到本机。"
		if entry.HookSupport == "native" {
			label = "原生生命周期钩子"
			if entry.HookHint != "" {
				description = entry.HookHint
			}
		}
		state := "available"
		if entry.KitInstalled {
			state = "ready"
		}
		out = append(out, CatalogIntegration{
			Kind: "hook", Label: label, State: state,
			Description: description, Path: entry.HookFile,
			Command: kitCommand(entry.ID, "hook.mjs"),
		})
	} else if entry.CanGenerateKit {
		state := "available"
		if entry.KitInstalled {
			state = "ready"
		}
		out = append(out, CatalogIntegration{
			Kind: "hook", Label: "外部事件桥", State: state,
			Description: "Agent 没有已核实的原生钩子；外部脚本或命令包装器可调用 hook.mjs / hook.sh 发送事件。",
			Command:     kitCommand(entry.ID, "hook.mjs"),
		})
	}

	state := "available"
	if entry.KitInstalled {
		state = "ready"
	}
	out = append(out, CatalogIntegration{
		Kind: "wrapper", Label: "命令包装器", State: state,
		Description: "用 homing-run.sh 包住任意 CLI 启动命令；退出时发送 done 或 error，不读取对话正文。",
		Command:     kitCommand(entry.ID, "homing-run.sh") + " <agent-command>",
	})

	out = append(out, CatalogIntegration{
		Kind: "plugin", Label: "会话插件骨架", State: "draft",
		Description: "生成 .draft.mjs；确认本地 JSON/JSONL/SQLite 结构后再移入 ~/.homing/adapters，避免空适配器冒充支持。",
		Command:     kitCommand(entry.ID, "adapter.draft.mjs"),
	})
	out = append(out, CatalogIntegration{
		Kind: "ingest", Label: "事件 API", State: "available",
		Description: "独立进程或远程开发机可带 Bearer 令牌调用 /api/ingest，并登记回复回调 URL。",
		Command:     "POST /api/ingest",
	})
	return out
}

func kitCommand(id, file string) string {
	return "~/.homing/integrations/" + id + "/" + file
}

// InstallCatalogKit writes a ready-to-use event bridge and editable drafts for
// one catalog entry. It is intentionally idempotent and never overwrites an
// existing generated file.
func InstallCatalogKit(id string) (CatalogKitResult, error) {
	if !safeKitID(id) {
		return CatalogKitResult{}, ErrCatalogNotFound
	}
	spec, ok := catalogSpecByID(id)
	if !ok {
		return CatalogKitResult{}, ErrCatalogNotFound
	}
	dir := CatalogKitDir(id)
	_, statErr := os.Stat(dir)
	existed := statErr == nil
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CatalogKitResult{}, fmt.Errorf("创建接入套件目录失败: %w", err)
	}

	sourceLabel := "Homing"
	switch spec.CatalogSource {
	case "magpie":
		sourceLabel = "Magpie 内置 Agent 目录"
	case "magpie-community":
		sourceLabel = "Magpie 社区 Agent 目录"
	}
	detectPath := ""
	if len(spec.DetectPaths) > 0 {
		detectPath = spec.DetectPaths[0]
	}
	data := kitTemplateData{
		ID:          spec.ID,
		Name:        spec.Name,
		Category:    spec.Category,
		ConfigFile:  spec.ConfigFile,
		DetectPath:  detectPath,
		HookHint:    spec.HookHint,
		Homepage:    spec.Homepage,
		SourceURL:   spec.SourceURL,
		SourceLabel: sourceLabel,
	}
	if data.ConfigFile == "" {
		data.ConfigFile = "未核实"
	}

	files := []struct {
		template string
		name     string
		mode     os.FileMode
	}{
		{"hook.sh.tmpl", "hook.sh", 0o700},
		{"hook.mjs.tmpl", "hook.mjs", 0o700},
		{"run.sh.tmpl", "homing-run.sh", 0o700},
		{"manifest.draft.yaml.tmpl", "manifest.draft.yaml", 0o600},
		{"adapter.draft.mjs.tmpl", "adapter.draft.mjs", 0o600},
		{"README.md.tmpl", "README.md", 0o600},
	}
	if spec.ID == "aider" {
		files = append(files, struct {
			template string
			name     string
			mode     os.FileMode
		}{"aider-notify.sh.tmpl", "aider-notify.sh", 0o700})
	}
	if spec.ID == "kilo" {
		files = append(files, struct {
			template string
			name     string
			mode     os.FileMode
		}{"kilo-plugin.js.tmpl", "kilo-plugin.js", 0o600})
	}

	for _, file := range files {
		path := filepath.Join(dir, file.name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		raw, err := renderKitTemplate(file.template, data)
		if err != nil {
			return CatalogKitResult{}, err
		}
		if err := os.WriteFile(path, raw, file.mode); err != nil {
			return CatalogKitResult{}, fmt.Errorf("写入 %s 失败: %w", file.name, err)
		}
	}

	result := CatalogKitResult{
		OK: true, ID: spec.ID, Dir: dir, Existing: existed,
		Hook:          filepath.Join(dir, "hook.mjs"),
		Wrapper:       filepath.Join(dir, "homing-run.sh"),
		ManifestDraft: filepath.Join(dir, "manifest.draft.yaml"),
		AdapterDraft:  filepath.Join(dir, "adapter.draft.mjs"),
		Readme:        filepath.Join(dir, "README.md"),
	}
	if spec.ID == "aider" {
		result.NativeFiles = append(result.NativeFiles, filepath.Join(dir, "aider-notify.sh"))
	}
	if spec.ID == "kilo" {
		result.NativeFiles = append(result.NativeFiles, filepath.Join(dir, "kilo-plugin.js"))
	}
	return result, nil
}

func renderKitTemplate(name string, data kitTemplateData) ([]byte, error) {
	raw, err := catalogKitFS.ReadFile("catalogdata/kit/" + name)
	if err != nil {
		return nil, fmt.Errorf("读取接入模板 %s 失败: %w", name, err)
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("解析接入模板 %s 失败: %w", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("渲染接入模板 %s 失败: %w", name, err)
	}
	return out.Bytes(), nil
}

func safeKitID(id string) bool {
	return validID(strings.TrimSpace(id))
}
