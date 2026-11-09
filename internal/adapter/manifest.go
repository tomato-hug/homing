package adapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest is one ~/.homing/agents/*.yaml file. Field names and semantics match
// the Node implementation (relay/adapters/manifest.mjs) so the same file works
// with either relay.
type Manifest struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Icon string `yaml:"icon"`

	Detect struct {
		Bin   []string `yaml:"bin"`
		Paths []string `yaml:"paths"`
	} `yaml:"detect"`

	Sessions SessionSpec `yaml:"sessions"`

	Push  string `yaml:"push"`
	Reply *struct {
		Command   []string `yaml:"command"`
		Cwd       string   `yaml:"cwd"`
		TimeoutMS int      `yaml:"timeout_ms"`
	} `yaml:"reply"`

	Models *struct {
		File    string   `yaml:"file"`
		Command []string `yaml:"command"`
	} `yaml:"models"`

	// SourceFile is filled by the loader, not the YAML.
	SourceFile string `yaml:"-"`
}

// SessionSpec says where one agent keeps its sessions and how to read them.
type SessionSpec struct {
	Source        string         `yaml:"source"` // jsonl | json | sqlite | http
	Glob          string         `yaml:"glob"`
	Dir           string         `yaml:"dir"`
	DB            string         `yaml:"db"`
	DBGlob        string         `yaml:"db_glob"` // 目录里每个 .db 一条会话（antigravity）
	Query         string         `yaml:"query"`
	URL           string         `yaml:"url"`
	Format        string         `yaml:"format"`
	FormatOptions map[string]any `yaml:"format_options"`
}

// ManifestDir is where user manifests live:
// ~/.homing/agents on Unix, %APPDATA%\Homing\agents on Windows.
func ManifestDir() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "Homing", "agents")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".homing", "agents")
}

// ScriptDir is where script adapters live: ~/.homing/adapters.
func ScriptDir() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "Homing", "adapters")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".homing", "adapters")
}

func expandHome(p string) string {
	if p == "" {
		return p
	}
	home, _ := os.UserHomeDir()
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

var validFormats = map[string]bool{
	"claude-code":    true,
	"codex":          true,
	"gemini-cli":     true,
	"generic-jsonl":  true,
	"custom":         true,
}

// Validate reports every problem at once so the user can fix the file in one go.
func (m *Manifest) Validate() []string {
	var errs []string
	if strings.TrimSpace(m.ID) == "" {
		errs = append(errs, "缺少 id")
	} else if !validID(m.ID) {
		errs = append(errs, "id 只能用小写字母、数字、点、下划线、连字符（≤64）")
	}
	src := m.Sessions.Source
	switch src {
	case "jsonl", "json":
		if m.Sessions.Glob == "" && m.Sessions.Dir == "" {
			errs = append(errs, "sessions 需要 glob 或 dir")
		}
	case "sqlite":
		if m.Sessions.DB == "" && m.Sessions.DBGlob == "" {
			errs = append(errs, "sessions 需要 db 或 db_glob")
		}
		if m.Sessions.Query == "" {
			errs = append(errs, "sessions 需要 query")
		}
	case "http":
		if m.Sessions.URL == "" {
			errs = append(errs, "sessions 需要 url")
		}
	case "":
		errs = append(errs, "缺少 sessions")
	default:
		errs = append(errs, fmt.Sprintf("sessions.source 必须是 jsonl | json | sqlite | http（收到 %s）", src))
	}
	if src != "sqlite" && src != "http" && src != "" {
		f := m.Sessions.Format
		if f == "" {
			f = "generic-jsonl"
		}
		if !validFormats[f] {
			errs = append(errs, fmt.Sprintf("sessions.format 未知：%s", f))
		}
	}
	if m.Push != "" && m.Push != "watch" && m.Push != "poll" && m.Push != "hook" {
		errs = append(errs, fmt.Sprintf("push 必须是 watch | poll | hook（收到 %s）", m.Push))
	}
	if m.Reply != nil {
		if len(m.Reply.Command) == 0 {
			errs = append(errs, "reply.command 必须是参数数组（不给 shell 拼串）")
		}
	}
	return errs
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// LoadManifest reads and validates one file.
func LoadManifest(path string) (*Manifest, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("YAML 解析失败: %w", err)
	}
	m.SourceFile = path
	if errs := m.Validate(); len(errs) > 0 {
		return &m, errs, nil
	}
	return &m, nil, nil
}

// LoadManifests reads every *.yaml / *.yml in dir. Files that fail validation are
// returned in errs (with their path) instead of aborting the whole load: one bad
// manifest must not hide the others.
func LoadManifests(dir string) (agents []*ManifestAgent, errs []ManifestError) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		path := filepath.Join(dir, n)
		m, problems, err := LoadManifest(path)
		if err != nil {
			errs = append(errs, ManifestError{File: path, ID: n, Problems: []string{err.Error()}})
			continue
		}
		if len(problems) > 0 {
			errs = append(errs, ManifestError{File: path, ID: m.ID, Problems: problems})
			continue
		}
		agents = append(agents, NewManifestAgent(m))
	}
	return agents, errs
}

// ManifestError is one manifest that could not be used.
type ManifestError struct {
	File     string   `json:"file"`
	ID       string   `json:"id"`
	Problems []string `json:"errors"`
}

// ErrNoAgentData marks an agent whose data directory is missing; callers turn it
// into an empty list rather than an error.
var ErrNoAgentData = errors.New("adapter: no agent data")

// yamlUnmarshal is a thin wrapper so callers outside this file do not import yaml.
func yamlUnmarshal(raw []byte, v any) error { return yaml.Unmarshal(raw, v) }
