package api

import (
	"context"
	"net/http"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

// displayOrder is the order agent cards appear in. It is the Node relay's
// registry order (relay/scanners/registry.mjs), kept here because the Go
// compiler, not the import block, decides registration order.
var displayOrder = []string{"codex", "claude", "workbuddy", "opencode", "zcode"}

// AgentModel is one selectable model in GET /api/agents.
type AgentModel struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider,omitempty"`
	Default  bool   `json:"default,omitempty"`
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
				"sessions":     caps.Sessions,
				"models":       caps.Models,
				"new_session":  false, // 新建会话还没搬到 Go
				"switch_model": caps.Reply && caps.Models,
				"effort":       false,
				"stop":         canReply,
				"approve":      false, // 授权挂起还没搬到 Go
				"live":         a.PushSignal() != agent.PushPoll,
			},
			Models:      []AgentModel{},
			Icon:        a.Icon(),
			Installed:   det.Installed,
			Version:     det.Version,
			PushSignal:  string(a.PushSignal()),
			LastEventAt: nil,
		}

		if lister, ok := a.(agent.ModelLister); ok {
			models, def, err := lister.Models(ctx)
			if err == nil {
				for _, m := range models {
					info.Models = append(info.Models, AgentModel{
						ID:      m.ID,
						Label:   firstNonEmpty(m.Name, m.ID),
						Default: def != "" && m.ID == def,
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

		agents = append(agents, info)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"agents": agents, "generated_at": nowISO()})
}
