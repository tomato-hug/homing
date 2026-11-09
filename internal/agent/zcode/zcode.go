// Package zcode reads 智谱 ZCode sessions. ZCode is a fork of OpenCode with the
// same SQLite layout, so the queries are the shared ones in
// internal/agent/opencodedb; only the paths and the config file differ.
//
// Ported from relay/scanners/zcode.mjs. Detection keeps several candidate paths
// so an install shows up without touching this file.
//
// Owner: WorkBuddy.
package zcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/opencodedb"
	"github.com/tomato-hug/homing/internal/platform"
)

const id = "zcode"

// Icon is the monochrome 16×16 glyph shared with the console.
const Icon = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><path d="M3 3.4h10L5.2 12.6H13"/></svg>`

func init() { agent.Register(Agent{}) }

// Agent implements agent.Agent plus ItemReader, EmptyChecker and ModelLister.
// ZCode has no local server, so there is no reply injection and no push signal.
type Agent struct{}

func (Agent) ID() string                   { return id }
func (Agent) Name() string                 { return "ZCode" }
func (Agent) Icon() string                 { return Icon }
func (Agent) PushSignal() agent.PushSignal { return agent.PushPoll }
func (Agent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Sessions: true, Models: true}
}
func (Agent) Meta(sessionID, cwdHint string) agent.Meta { return metaFor(sessionID, cwdHint) }

// Candidates lists every database location ZCode is known to use.
// magpie's zcode.go keeps the database at ~/.zcode/cli/db/db.sqlite even when the
// app data lives elsewhere; the Application Support paths cover macOS installs.
func Candidates() []string {
	roots := []string{}
	if root := os.Getenv("ZCODE_HOME"); root != "" {
		roots = append(roots, root)
	}
	home := platform.Home()
	roots = append(roots, filepath.Join(home, ".zcode"))
	roots = append(roots, filepath.Join(home, "Library", "Application Support", "ZCode"))
	roots = append(roots, filepath.Join(home, "Library", "Application Support", "zcode"))

	seen := map[string]bool{}
	var out []string
	for _, root := range roots {
		for _, rel := range [][]string{
			{"cli", "db", "db.sqlite"},
			{"db", "db.sqlite"},
			{"db.sqlite"},
			{"cli", "db", "zcode.db"},
		} {
			path := filepath.Join(append([]string{root}, rel...)...)
			if !seen[path] {
				seen[path] = true
				out = append(out, path)
			}
		}
	}
	return out
}

// DBPath is the first candidate that exists, or "".
func DBPath() string {
	for _, path := range Candidates() {
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path
		}
	}
	return ""
}

func configCandidates() []string {
	roots := []string{}
	if root := os.Getenv("ZCODE_HOME"); root != "" {
		roots = append(roots, root)
	}
	home := platform.Home()
	roots = append(roots, filepath.Join(home, ".zcode"), filepath.Join(home, ".config", "zcode"))

	var out []string
	for _, root := range roots {
		out = append(out,
			filepath.Join(root, "cli", "config.json"),
			filepath.Join(root, "config.json"),
			filepath.Join(root, "opencode.json"),
		)
	}
	return out
}

// Detect reports the database it found, or how many paths it checked.
func (Agent) Detect(context.Context) agent.Detection {
	path := DBPath()
	ok, sessions := false, 0
	if path != "" {
		ok, sessions = opencodedb.Probe(path)
	}
	det := agent.Detection{Installed: path != "" || ok}
	if path != "" {
		det.Detail = fmt.Sprintf("%d sessions · %s", sessions, path)
	} else {
		det.Detail = fmt.Sprintf("未找到数据库（查过 %d 个路径）", len(Candidates()))
	}
	return det
}

func metaFor(sessionID, cwdHint string) agent.Meta {
	out := agent.Meta{Cwd: cwdHint, TitleSource: "agent"}
	path := DBPath()
	if path == "" {
		return out
	}
	row, ok := opencodedb.OneSession(path, sessionID)
	if !ok {
		return out
	}
	out.Cwd = firstNonEmpty(out.Cwd, row.Cwd)
	out.NativeTitle = row.Title
	out.Model = row.Model
	return out
}

// ListSessions returns the sessions in the ZCode database, newest first.
func (Agent) ListSessions(_ context.Context, limit int) ([]agent.Session, error) {
	path := DBPath()
	if path == "" {
		return nil, nil
	}
	rows := opencodedb.ListSessions(path, limit)
	out := make([]agent.Session, 0, len(rows))
	for _, r := range rows {
		session := agent.Session{
			SessionID:   r.SessionID,
			Source:      id,
			Title:       r.Title,
			TitleSource: "agent",
			LastTS:      r.LastTS,
			Cwd:         r.Cwd,
			Model:       r.Model,
		}
		if r.ArchivedNative {
			session.NativeState = "archived"
		}
		out = append(out, session)
	}
	return out, nil
}

// ReadItems returns the last entries of one session transcript.
func (Agent) ReadItems(_ context.Context, sessionID string, limit int) ([]agent.Item, error) {
	path := DBPath()
	if path == "" {
		return nil, nil
	}
	return opencodedb.ReadItems(path, sessionID, limit), nil
}

// IsEmptySession reports a session with no user or assistant entries.
func (Agent) IsEmptySession(sessionID string) bool {
	path := DBPath()
	if path == "" {
		return false
	}
	return !opencodedb.HasConversation(path, sessionID)
}

// Models reads the provider list from the ZCode config, falling back to the
// models.dev catalog cached in its database. An install-less machine returns an
// empty list rather than invented model ids.
func (Agent) Models(context.Context) ([]agent.Model, string, error) {
	path := DBPath()
	catalog := map[string]any{}
	if path != "" {
		if fromDB := opencodedb.Catalog(path); fromDB != nil {
			catalog = fromDB
		}
	}
	for _, cfgPath := range configCandidates() {
		var cfg struct {
			Provider map[string]struct {
				Models map[string]json.RawMessage `json:"models"`
			} `json:"provider"`
			Model string `json:"model"`
		}
		if !platform.ReadJSON(cfgPath, &cfg) {
			continue
		}
		var out []agent.Model
		for providerID, provider := range cfg.Provider {
			known := make([]string, 0, len(provider.Models))
			for modelID := range provider.Models {
				known = append(known, modelID)
			}
			if len(known) == 0 {
				if entry, ok := catalog[providerID].(map[string]any); ok {
					if models, ok := entry["models"].(map[string]any); ok {
						for modelID := range models {
							known = append(known, modelID)
						}
					}
				}
			}
			for _, modelID := range known {
				out = append(out, agent.Model{ID: providerID + "/" + modelID, Name: modelID})
			}
		}
		if len(out) > 0 || cfg.Model != "" {
			return out, cfg.Model, nil
		}
	}
	return nil, "", nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
