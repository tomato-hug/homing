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
)

// displayOrder is the order agent cards appear in. It is the Node relay's
// registry order (relay/scanners/registry.mjs), kept here because the Go
// compiler, not the import block, decides registration order.
var displayOrder = []string{"codex", "claude", "workbuddy", "opencode", "zcode"}

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
	Source       string          `json:"source"`
	DisplayName  string          `json:"display_name"`
	Available    bool            `json:"available"`
	Capabilities map[string]bool `json:"capabilities"`
	Models       []AgentModel    `json:"models"`
	DefaultModel *string         `json:"default_model"`
	Icon         string          `json:"icon"`
	Installed    bool            `json:"installed"`
	Version      *string         `json:"version"`
	PushSignal   string          `json:"push_signal"`
	Status       string          `json:"status"`
	LastEventAt  *string         `json:"last_event_at"`
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
		caps := a.Capabilities()
		_, canReply := a.(agent.Replier)

		info := AgentInfo{
			Source:      a.ID(),
			DisplayName: a.Name(),
			Available:   det.Installed,
			Capabilities: map[string]bool{
				"reply":        caps.Reply,
				"new_session":  newSessionFor(a.ID(), det.Installed),
				"switch_model": switchModelFor(a.ID()),
				"effort":       a.ID() == "codex",
				"stop":         canReply,
				"approve":      approveFor(a.ID()),
				"live":         a.PushSignal() != agent.PushPoll,
			},
			Models:      []AgentModel{},
			Icon:        a.Icon(),
			Installed:   det.Installed,
			Version:     det.Version,
			PushSignal:  string(a.PushSignal()),
			LastEventAt: nil,
		}

		if a.ID() == "workbuddy" {
			// Node：WorkBuddy 的 available 指桌面端进程在跑（回复要经它注入），installed 才是装没装。
			info.Available = workbuddyRunning(ctx)
		}

		if !coreAgent(a.ID()) {
			// Node 对注册表里的扫描型 agent（opencode / zcode …）只给 reply + sessions 两个能力位，
			// 不列模型、没有默认模型。
			info.Capabilities = map[string]bool{"reply": caps.Reply, "sessions": true}
		} else if lister, ok := a.(agent.ModelLister); ok {
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

		if !coreAgent(a.ID()) {
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

// newSessionFor / switchModelFor 对 Node 中继的 capability 位：
// relay/server.mjs 的 listAgents() 里每家是写死的（codex/claude 看 bin 是否可用，
// workbuddy 恒为 true；switch_model 只有 codex/claude 有，effort 只有 codex 有）。
func newSessionFor(id string, installed bool) bool {
	switch id {
	case "codex", "claude":
		return installed
	case "workbuddy":
		return true
	}
	return false
}

func switchModelFor(id string) bool {
	switch id {
	case "codex", "claude":
		return true
	}
	return false
}

func coreAgent(id string) bool {
	switch id {
	case "codex", "claude", "workbuddy":
		return true
	}
	return false
}

// approveFor: Codex and Claude can approve from the phone once their hook file
// sends PermissionRequest here; WorkBuddy has no such hook.
func approveFor(id string) bool {
	switch id {
	case "codex":
		return hookConfigured(filepath.Join(".codex", "hooks.json"), "/hook/codex")
	case "claude":
		return hookConfigured(filepath.Join(".claude", "settings.json"), "/hook/claude")
	}
	return false
}

// workbuddyRunning is Node's workbuddyRunning(): is the desktop app's process up.
func workbuddyRunning(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	return exec.CommandContext(ctx, "pgrep", "-f", "WorkBuddy.app/Contents/MacOS/Electron").Run() == nil
}
