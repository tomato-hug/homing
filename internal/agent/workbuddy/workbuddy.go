// Package workbuddy reads WorkBuddy sessions. The list comes from
// ~/.workbuddy/<uid>/sidebar-list-snapshot.json (title / state / space.name);
// transcripts under ~/.workbuddy/projects/<dir>/<session>.jsonl are the same
// JSONL family as Claude Code, so the claude parser reads them.
//
// Ported from relay/scanners/workbuddy.mjs.
//
// WorkBuddy has no reply injection here on purpose: the phone's message is
// handed to the running turn through the stop-hook hold (see internal/api).
//
// Owner: WorkBuddy.
package workbuddy

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/claude"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/platform"
)

const id = "workbuddy"

// Icon is the monochrome 16×16 glyph shared with the console.
const Icon = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><rect x="2.2" y="3.4" width="11.6" height="9.2" rx="2"/><path d="M5 7h6M5 9.6h3.4"/></svg>`

func init() { agent.Register(Agent{}) }

// Agent implements agent.Agent.
type Agent struct{}

func (Agent) ID() string                   { return id }
func (Agent) Name() string                 { return "WorkBuddy" }
func (Agent) Icon() string                 { return Icon }
func (Agent) PushSignal() agent.PushSignal { return agent.PushHook }
func (Agent) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Core:     true,
		Reply:    true,
		Sessions: true,
		// WorkBuddy 5.9+ 的 app-server 不再暴露旧 /api/v1/acp/connect 桥。
		// 旧桥的 session/new 也会复用当前活跃会话，不能作为独立新会话。
		NewSession:      false,
		Stop:            false,
		Live:            true,
		SwitchModel:     false,
		PermissionModes: []string{"default"},
	}
}
func (Agent) Meta(sessionID, cwdHint string) agent.Meta { return metaFor(sessionID, cwdHint) }

func home() string { return filepath.Join(platform.Home(), ".workbuddy") }

// Detect reports whether ~/.workbuddy exists. WorkBuddy keeps no version file.
func (Agent) Detect(context.Context) agent.Detection {
	return agent.Detection{Installed: dirExists(home())}
}

// snapshotItem is one row of the sidebar snapshot.
type snapshotItem struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	State          string  `json:"state"`
	LastActivityAt float64 `json:"lastActivityAt"` // unix milliseconds
	Space          *struct {
		Name string `json:"name"`
	} `json:"space"`
}

// sidebarSnapshot reads the newest sidebar-list-snapshot.json under ~/.workbuddy.
func sidebarSnapshot() []snapshotItem {
	root := home()
	names, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	best, bestMod := "", time.Time{}
	for _, name := range names {
		if !name.IsDir() {
			continue
		}
		file := filepath.Join(root, name.Name(), "sidebar-list-snapshot.json")
		stat, err := os.Stat(file)
		if err != nil {
			continue
		}
		if best == "" || stat.ModTime().After(bestMod) {
			best, bestMod = file, stat.ModTime()
		}
	}
	if best == "" {
		return nil
	}
	var payload struct {
		Items []snapshotItem `json:"items"`
	}
	if !platform.ReadJSON(best, &payload) {
		return nil
	}
	return payload.Items
}

// snapshotByID indexes the snapshot by session id, cached by file mtime+size.
var snapshotCache = struct {
	key  string
	byID map[string]snapshotItem
}{byID: map[string]snapshotItem{}}

func snapshotIndex() map[string]snapshotItem {
	root := home()
	names, err := os.ReadDir(root)
	if err != nil {
		return map[string]snapshotItem{}
	}
	for _, name := range names {
		if !name.IsDir() {
			continue
		}
		file := filepath.Join(root, name.Name(), "sidebar-list-snapshot.json")
		stat, err := os.Stat(file)
		if err != nil {
			continue
		}
		key := file + "|" + stat.ModTime().String() + "|" + itoa(stat.Size())
		if snapshotCache.key == key {
			return snapshotCache.byID
		}
		var payload struct {
			Items []snapshotItem `json:"items"`
		}
		if !platform.ReadJSON(file, &payload) {
			continue
		}
		byID := make(map[string]snapshotItem, len(payload.Items))
		for _, item := range payload.Items {
			if item.ID != "" {
				byID[item.ID] = item
			}
		}
		snapshotCache = struct {
			key  string
			byID map[string]snapshotItem
		}{key: key, byID: byID}
		return byID
	}
	return map[string]snapshotItem{}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// metaFor is the workbuddy branch of the Node sessionMetaFor.
func metaFor(sessionID, cwdHint string) agent.Meta {
	out := agent.Meta{Cwd: cwdHint, TitleSource: "agent"}
	if item, ok := snapshotIndex()[sessionID]; ok {
		out.NativeTitle = item.Title
		if item.Space != nil && item.Space.Name != "" {
			out.Cwd = firstNonEmpty(out.Cwd, item.Space.Name)
			// Node publishes the space name as the workspace chip verbatim.
			out.Workspace = item.Space.Name
		}
	}
	if recorded, ok := scanlib.TranscriptPath(sessionID); ok {
		m := claude.ReadFileMeta(recorded)
		out.Cwd = firstNonEmpty(out.Cwd, m.Cwd)
		out.FirstPrompt = firstNonEmpty(m.FirstPrompt, out.FirstPrompt)
		out.Model = firstNonEmpty(out.Model, m.Model)
		if out.NativeTitle == "" {
			out.NativeTitle = firstNonEmpty(m.CustomTitle, m.AITitle)
			if m.CustomTitle != "" {
				out.TitleSource = "custom"
			}
		}
	}
	return out
}

// ListSessions returns the sidebar snapshot, newest activity first.
func (Agent) ListSessions(_ context.Context, limit int) ([]agent.Session, error) {
	items := sidebarSnapshot()
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	out := make([]agent.Session, 0, len(items))
	for _, item := range items {
		row := agent.Session{
			SessionID:   item.ID,
			Source:      id,
			Title:       item.Title,
			TitleSource: "agent",
			NativeState: item.State,
		}
		if item.Space != nil {
			row.Cwd = item.Space.Name
		}
		if item.LastActivityAt > 0 {
			row.LastTS = scanlib.ISO(time.UnixMilli(int64(item.LastActivityAt)))
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastTS > out[j].LastTS })
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}

// IsEmptySession follows the Node relay: a session counts as empty when its
// transcript is missing or holds no user/assistant turn. The sidebar snapshot
// alone is not evidence of a conversation.
func (Agent) IsEmptySession(sessionID string) bool {
	path := TranscriptPath(sessionID)
	if path == "" {
		return true
	}
	return !claude.ReadFileMeta(path).HasConversation
}

// TranscriptPath finds a WorkBuddy transcript by session id (projects/<dir>/<id>.jsonl).
func TranscriptPath(sessionID string) string {
	if recorded, ok := scanlib.TranscriptPath(sessionID); ok {
		return recorded
	}
	projects := filepath.Join(home(), "projects")
	dirs, err := os.ReadDir(projects)
	if err != nil {
		return ""
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		candidate := filepath.Join(projects, dir.Name(), sessionID+".jsonl")
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate
		}
	}
	return ""
}
