package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomato-hug/homing/internal/agent"
)

// Models implements agent.ModelLister the way the Node relay builds the Codex card:
// the model in config.toml is the default, then whatever the 20 most recent
// rollouts were run with. Codex has no model registry to ask, so the observed set
// is the honest answer.
func (Agent) Models(context.Context) ([]agent.Model, string, error) {
	var out []agent.Model
	seen := map[string]bool{}
	defaultModel := ""

	if raw, err := os.ReadFile(filepath.Join(Agent{}.home(), "config.toml")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "model") && strings.Contains(t, "=") {
				parts := strings.SplitN(t, "=", 2)
				if strings.TrimSpace(parts[0]) != "model" {
					continue
				}
				if v := strings.Trim(strings.TrimSpace(parts[1]), `"`); v != "" {
					defaultModel = v
					break
				}
			}
		}
	}
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, agent.Model{ID: id})
	}
	add(defaultModel)

	for i, file := range rolloutFiles(30) {
		if i >= 20 {
			break
		}
		if m := readRolloutMeta(file).model; m != "" {
			add(m)
		}
	}
	if defaultModel == "" && len(out) > 0 {
		defaultModel = out[0].ID
	}
	return out, defaultModel, nil
}
