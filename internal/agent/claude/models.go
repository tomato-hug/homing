package claude

import (
	"context"

	"github.com/tomato-hug/homing/internal/agent"
)

// Models implements agent.ModelLister: the models seen in the 30 most recent
// Claude Code sessions, first one as default. Same source and order as the Node
// relay — there is no model file to read for Claude Code.
func (Agent) Models(context.Context) ([]agent.Model, string, error) {
	var out []agent.Model
	seen := map[string]bool{}
	defaultModel := ""

	entries := sessionFiles()
	if len(entries) > 30 {
		entries = entries[:30]
	}
	for _, e := range entries {
		m := fileMetaFor(e).model
		if m == "" || m == "<synthetic>" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, agent.Model{ID: m})
		if defaultModel == "" {
			defaultModel = m
		}
	}
	return out, defaultModel, nil
}
