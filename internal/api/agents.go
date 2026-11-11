package api

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/driver"
)

// displayOrder is the order agent cards appear in. It is the Node relay's
// registry order (relay/scanners/registry.mjs), kept here because the Go
// compiler, not the import block, decides registration order.
var displayOrder = []string{"codex", "claude", "workbuddy", "kimi", "opencode", "zcode"}

// AgentModel is one selectable model in GET /api/agents. The keys are the Node
// relay's modelEntry(): label, provider, is_default (and its older alias
// default) and efforts are always present — the phone's model and effort
// pickers read them.
type AgentModel struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Provider  string   `json:"provider"`
	IsDefault bool     `json:"is_default"`
	Default   bool     `json:"default"`
	Efforts   []string `json:"efforts"`
}

// codexEfforts is CODEX_EFFORTS in the Node relay: only Codex offers an effort choice.
var codexEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "ultra", "max"}

// modelLabel turns an id such as "claude-fable-5-1" into "Claude Fable 5 1":
// the part after the last "/", split on - _ ., words of up to three letters
// upper-cased, longer ones capitalised (relay/server.mjs modelLabel).
func modelLabel(id string) string {
	base := id
	if i := strings.LastIndex(id, "/"); i >= 0 && i+1 < len(id) {
		base = id[i+1:]
	}
	words := strings.FieldsFunc(base, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	for i, w := range words {
		r := []rune(w)
		if len(r) <= 3 {
			words[i] = strings.ToUpper(w)
		} else {
			words[i] = strings.ToUpper(string(r[:1])) + string(r[1:])
		}
	}
	return strings.Join(words, " ")
}

// modelProvider is Node's provider rule: Claude is always "anthropic"; otherwise
// the id's "vendor/" prefix, else a per-agent fallback.
func modelProvider(source, id string) string {
	if source == "claude" {
		return "anthropic"
	}
	if i := strings.Index(id, "/"); i > 0 {
		return id[:i]
	}
	switch source {
	case "codex":
		return "openai"
	case "workbuddy":
		return "workbuddy"
	}
	return "unknown"
}

// hookConfigured reports whether a Codex / Claude settings file already routes
// permission requests to this relay (Node's hookConfigured, without pinning the port).
func hookConfigured(relPath, hookPath string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(home, relPath))
	if err != nil {
		return false
	}
	text := string(raw)
	return strings.Contains(text, hookPath) && strings.Contains(text, "PermissionRequest")
}

// AgentInfo is one card of GET /api/agents.
type AgentInfo struct {
	Source          string          `json:"source"`
	DisplayName     string          `json:"display_name"`
	Available       bool            `json:"available"`
	Capabilities    map[string]bool `json:"capabilities"`
	Models          []AgentModel    `json:"models"`
	DefaultModel    *string         `json:"default_model"`
	Icon            string          `json:"icon"`
	Installed       bool            `json:"installed"`
	Version         *string         `json:"version"`
	PushSignal      string          `json:"push_signal"`
	Status          string          `json:"status"`
	LastEventAt     *string         `json:"last_event_at"`
	PermissionModes []string        `json:"permission_modes"`
	Driver          *AgentDriver    `json:"driver,omitempty"`
}

// AgentDriver reports how the generic drive layer sees one integration. It is
// additive; older clients can ignore it.
type AgentDriver struct {
	Kind         string `json:"kind"`
	Command      string `json:"command,omitempty"`
	Installed    bool   `json:"installed"`
	Ready        bool   `json:"ready"`
	Detail       string `json:"detail,omitempty"`
	Experimental bool   `json:"experimental,omitempty"`
	Verified     bool   `json:"verified"`
}

func init() {
	Register("agents", 10, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/agents", d.guard(d.handleAgents))
	})
}

// sortedAgents returns the built-in agents in card order.
func sortedAgents(ctx context.Context) []agent.Agent {
	all := agent.All(ctx)
	rank := make(map[string]int, len(displayOrder))
	for i, id := range displayOrder {
		rank[id] = i
	}
	out := make([]agent.Agent, 0, len(all))
	for _, id := range displayOrder {
		for _, a := range all {
			if a.ID() == id {
				out = append(out, a)
			}
		}
	}
	for _, a := range all {
		if _, listed := rank[a.ID()]; !listed {
			out = append(out, a)
		}
	}
	return out
}

func (d *Deps) handleAgents(w http.ResponseWriter, r *http.Request) {
	ctx := ctxOf(r)
	lastBySource := map[string]int64{}
	if s, ok := d.store(); ok {
		for _, event := range s.Events() {
			if ts, err := time.Parse(time.RFC3339, event.Ts); err == nil {
				if ts.UnixMilli() > lastBySource[event.Source] {
					lastBySource[event.Source] = ts.UnixMilli()
				}
			}
		}
	}

	now := time.Now()
	agents := make([]AgentInfo, 0, len(displayOrder))
	for _, a := range sortedAgents(ctx) {
		det := a.Detect(ctx)
		caps := agent.EffectiveCapabilities(a)
		var driverInfo *AgentDriver
		if spec, ok := driver.SpecFor(a.ID()); ok {
			readiness := driver.CheckReadiness(spec)
			caps = withDriverCapabilities(caps, spec, readiness)
			driverInfo = &AgentDriver{
				Kind:         "acp",
				Command:      spec.CommandString(),
				Installed:    readiness.Installed,
				Ready:        readiness.Ready,
				Detail:       readiness.Detail,
				Experimental: spec.Experimental,
				Verified:     spec.Verified,
			}
		}

		info := AgentInfo{
			Source:          a.ID(),
			DisplayName:     a.Name(),
			Available:       det.Installed,
			Capabilities:    capabilityMap(a, caps, det.Installed),
			Models:          []AgentModel{},
			Icon:            a.Icon(),
			Installed:       det.Installed,
			Version:         det.Version,
			PushSignal:      string(a.PushSignal()),
			LastEventAt:     nil,
			PermissionModes: append([]string{}, caps.PermissionModes...),
			Driver:          driverInfo,
		}

		if a.ID() == "workbuddy" {
			// Node：WorkBuddy 的 available 指桌面端进程在跑（回复要经它注入），installed 才是装没装。
			info.Available = workbuddyRunning(ctx)
		}

		// Node 对注册表里的扫描型 agent（opencode / zcode …）不列模型；
		// 能力位仍来自 Agent 自己的能力表。
		if caps.Core {
			if lister, ok := a.(agent.ModelLister); ok {
				models, def, err := lister.Models(ctx)
				if err == nil {
					for _, m := range models {
						isDefault := def != "" && m.ID == def
						efforts := []string{}
						if a.ID() == "codex" {
							efforts = codexEfforts
						}
						info.Models = append(info.Models, AgentModel{
							ID:        m.ID,
							Label:     modelLabel(m.ID),
							Provider:  modelProvider(a.ID(), m.ID),
							IsDefault: isDefault,
							Default:   isDefault,
							Efforts:   efforts,
						})
					}
					if def != "" {
						info.DefaultModel = &def
					}
				}
			}
		}

		if lastMillis, ok := lastBySource[a.ID()]; ok && lastMillis > 0 {
			stamp := time.UnixMilli(lastMillis).UTC().Format("2006-01-02T15:04:05.000Z")
			info.LastEventAt = &stamp
			recent := now.Sub(time.UnixMilli(lastMillis)) < 24*time.Hour
			switch {
			case !det.Installed:
				info.Status = "missing"
			case recent:
				info.Status = "connected"
			default:
				info.Status = "pending"
			}
		} else if det.Installed {
			info.Status = "pending"
		} else {
			info.Status = "missing"
		}

		if !caps.Core {
			// Node's registry-only agents (opencode, zcode, …): installed means connected.
			if det.Installed {
				info.Status = "connected"
			} else {
				info.Status = "missing"
			}
		}

		agents = append(agents, info)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"agents": agents, "generated_at": nowISO()})
}

// withDriverCapabilities overlays a verified ACP launch path on top of the
// scanner's static capability table. A configured but unusable drive must not
// leave new_session/stop/dispatch true: the UI would offer a button that fails
// only after the user pressed it.
func withDriverCapabilities(caps agent.Capabilities, spec driver.Spec, readiness driver.Readiness) agent.Capabilities {
	if !spec.Dispatch {
		return caps
	}
	ready := readiness.Ready && readiness.Installed
	caps.NewSession = ready
	caps.Stop = ready
	caps.Approve = ready
	caps.Live = ready
	caps.Dispatch = ready
	return caps
}

func capabilityMap(a agent.Agent, caps agent.Capabilities, installed bool) map[string]bool {
	newSessionReady := caps.NewSession && (installed || caps.NewSessionAny)
	out := map[string]bool{
		"reply":             caps.Reply,
		"new_session":       newSessionReady,
		"new_session_ready": newSessionReady,
		"dispatch":          caps.Dispatch && installed,
		"switch_model":      caps.SwitchModel,
		"effort":            caps.Effort,
		"stop":              caps.Stop,
		"approve":           caps.Approve,
		"live":              caps.Live,
	}
	if !caps.Core {
		// A drive-only ACP manifest has no verified transcript source. Keep the
		// additive sessions flag truthful instead of treating every non-core
		// integration as readable.
		out["sessions"] = caps.Sessions
	}
	if hook, ok := a.(agent.HookApprover); ok {
		settingsPath, endpoint := hook.ApprovalHook()
		out["approve"] = hookConfigured(settingsPath, endpoint)
	}
	return out
}

// PermissionModesFor returns a copy of the modes an agent accepts in
// /api/reply and /api/sessions/new. Callers must reject anything else rather
// than passing an arbitrary string through to the vendor CLI.
func PermissionModesFor(id string) []string {
	a, ok := agent.Get(context.Background(), strings.ToLower(strings.TrimSpace(id)))
	if !ok {
		return nil
	}
	caps := agent.EffectiveCapabilities(a)
	return append([]string(nil), caps.PermissionModes...)
}

// PermissionModeAllowed reports whether mode is a documented capability of the
// agent. The empty string means "use the agent's default" and is always valid.
func PermissionModeAllowed(id, mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return true
	}
	for _, allowed := range PermissionModesFor(id) {
		if mode == allowed {
			return true
		}
	}
	return false
}

// workbuddyRunning is Node's workbuddyRunning(): is the desktop app's process up.
func workbuddyRunning(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	return exec.CommandContext(ctx, "pgrep", "-f", "WorkBuddy.app/Contents/MacOS/Electron").Run() == nil
}
