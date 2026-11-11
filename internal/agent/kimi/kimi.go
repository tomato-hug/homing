// Package kimi reads Kimi Code CLI sessions.
//
// Session layout (Kimi Code >= 2.x):
//
//	~/.kimi-code/sessions/<workdir-bucket>/<session-id>/
//	  session-meta/state.json
//	  agents/main/wire.jsonl
//
// Legacy Kimi CLI sessions use state.json and wire.jsonl at the session root,
// so both layouts are accepted. The wire format wraps each event as
// {timestamp, message:{type,payload}}; timestamps are Unix seconds.
package kimi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/platform"
)

const id = "kimi"

// Icon is a monochrome 16×16 glyph shared with the console.
const Icon = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M10.6 2.4a5.7 5.7 0 1 0 3 9.7 6.4 6.4 0 0 1-3-9.7Z"/><path d="M3.2 12.6 6 9.8"/></svg>`

func init() { agent.Register(Agent{}) }

// Agent implements agent.Agent and agent.ItemReader. Kimi Code has lifecycle
// hooks but no stable local reply endpoint, so replies are not advertised.
type Agent struct{}

func (Agent) ID() string                   { return id }
func (Agent) Name() string                 { return "Kimi Code" }
func (Agent) Icon() string                 { return Icon }
func (Agent) PushSignal() agent.PushSignal { return agent.PushPoll }
func (Agent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Sessions: true}
}
func (Agent) Meta(sessionID, cwdHint string) agent.Meta { return metaFor(sessionID, cwdHint) }

// Home is KIMI_CODE_HOME, or ~/.kimi-code.
func Home() string {
	if root := strings.TrimSpace(os.Getenv("KIMI_CODE_HOME")); root != "" {
		return root
	}
	return filepath.Join(platform.Home(), ".kimi-code")
}

func (Agent) Detect(context.Context) agent.Detection {
	root := Home()
	installed := dirExists(root) || scanlib.ResolveBinary("kimi") != ""
	det := agent.Detection{Installed: installed}
	if n := sessionCount(); n > 0 {
		det.Detail = filepath.Join(root, "sessions")
	}
	return det
}

func sessionCount() int {
	rows := scanSessions()
	return len(rows)
}

type stateFile struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	TitleKind      string `json:"titleKind"`
	CustomTitle    string `json:"custom_title"`
	LastPrompt     string `json:"lastPrompt"`
	Cwd            string `json:"cwd"`
	WorkDir        string `json:"workDir"`
	EnvironmentCwd string `json:"environmentCwd"`
	UpdatedAt      any    `json:"updatedAt"`
	Archived       bool   `json:"archived"`
}

type sessionRow struct {
	dir       string
	wire      string
	id        string
	title     string
	titleFrom string
	first     string
	cwd       string
	lastTS    time.Time
	archived  bool
}

var (
	cacheMu   sync.Mutex
	cacheAt   time.Time
	cacheHome string
	cache     []sessionRow
)

func scanSessions() []sessionRow {
	root := Home()
	base := filepath.Join(root, "sessions")

	cacheMu.Lock()
	if cacheHome == root && len(cache) > 0 && time.Since(cacheAt) < 20*time.Second {
		out := append([]sessionRow(nil), cache...)
		cacheMu.Unlock()
		return out
	}
	cacheMu.Unlock()

	buckets, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	index := sessionIndex()
	var rows []sessionRow
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(base, bucket.Name()))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(base, bucket.Name(), entry.Name())
			row, ok := readSession(dir)
			if !ok {
				continue
			}
			if row.cwd == "" {
				row.cwd = index[row.id]
			}
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].lastTS.After(rows[j].lastTS) })

	cacheMu.Lock()
	cacheAt = time.Now()
	cacheHome = root
	cache = rows
	cacheMu.Unlock()
	return append([]sessionRow(nil), rows...)
}

func readSession(dir string) (sessionRow, bool) {
	row := sessionRow{dir: dir, id: filepath.Base(dir), titleFrom: "first_prompt"}
	for _, rel := range [][]string{
		{"agents", "main", "wire.jsonl"},
		{"wire.jsonl"},
	} {
		path := filepath.Join(append([]string{dir}, rel...)...)
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			row.wire = path
			row.lastTS = stat.ModTime()
			break
		}
	}

	var state stateFile
	for _, path := range []string{
		filepath.Join(dir, "session-meta", "state.json"),
		filepath.Join(dir, "state.json"),
	} {
		if platform.ReadJSON(path, &state) {
			break
		}
	}
	if state.ID != "" {
		row.id = state.ID
	}
	if state.Cwd != "" {
		row.cwd = state.Cwd
	} else if state.EnvironmentCwd != "" {
		row.cwd = state.EnvironmentCwd
	} else if state.WorkDir != "" {
		row.cwd = state.WorkDir
	}
	row.archived = state.Archived
	if ts := millisFromAny(state.UpdatedAt); ts > 0 {
		row.lastTS = time.UnixMilli(ts)
	}
	if row.wire == "" && !fileExists(filepath.Join(dir, "session-meta", "state.json")) && !fileExists(filepath.Join(dir, "state.json")) {
		return sessionRow{}, false
	}

	first, last := readWireSummary(row.wire)
	row.first = first
	if last > 0 {
		row.lastTS = time.UnixMilli(last)
	}

	switch {
	case state.Title != "":
		row.title = state.Title
		if state.TitleKind == "custom" {
			row.titleFrom = "custom"
		} else {
			row.titleFrom = "agent"
		}
	case state.CustomTitle != "":
		row.title, row.titleFrom = state.CustomTitle, "custom"
	case state.LastPrompt != "":
		row.title, row.titleFrom = scanlib.FirstLine(state.LastPrompt, 80), "first_prompt"
	case first != "":
		row.title, row.titleFrom = scanlib.FirstLine(first, 80), "first_prompt"
	}
	if row.id == "" {
		return sessionRow{}, false
	}
	return row, true
}

func readWireSummary(path string) (firstPrompt string, lastMillis int64) {
	if path == "" {
		return "", 0
	}
	for _, line := range scanlib.ReadJSONLines(path) {
		message, payload, at := wireParts(line)
		if at > 0 {
			lastMillis = at
		}
		kind := asString(message["type"])
		if kind == "TurnBegin" && firstPrompt == "" {
			text := scanlib.NormalizeContent(payload["user_input"])
			if text != "" && !scanlib.Noise.MatchString(strings.TrimSpace(text)) {
				firstPrompt = text
			}
		}
	}
	return firstPrompt, lastMillis
}

func (Agent) ListSessions(_ context.Context, limit int) ([]agent.Session, error) {
	rows := scanSessions()
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]agent.Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, agent.Session{
			SessionID:   row.id,
			Source:      id,
			Title:       row.title,
			TitleSource: row.titleFrom,
			LastTS:      scanlib.ISO(row.lastTS),
			Cwd:         row.cwd,
			NativeState: archivedState(row.archived),
		})
	}
	return out, nil
}

func (Agent) ReadItems(_ context.Context, sessionID string, limit int) ([]agent.Item, error) {
	row, ok := findSession(sessionID)
	if !ok || row.wire == "" {
		return nil, nil
	}
	var items []agent.Item
	for _, line := range scanlib.ReadJSONLines(row.wire) {
		message, payload, at := wireParts(line)
		kind := asString(message["type"])
		role, text := "", ""
		switch kind {
		case "TurnBegin":
			text = scanlib.NormalizeContent(payload["user_input"])
			if text != "" && !scanlib.Noise.MatchString(strings.TrimSpace(text)) {
				role = "user"
			}
		case "ContentPart":
			if asString(payload["type"]) == "text" {
				text = asString(payload["text"])
				role = "assistant"
			}
		}
		if role == "" || strings.TrimSpace(text) == "" {
			continue
		}
		stamp := ""
		if at > 0 {
			stamp = time.UnixMilli(at).UTC().Format("2006-01-02T15:04:05.000Z")
		}
		items = append(items, agent.Item{
			TS:   stamp,
			ID:   "kimi-" + itoa(len(items)),
			Seq:  len(items),
			Type: "message",
			Role: role,
			Text: text,
		})
	}
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
		for i := range items {
			items[i].Seq = i
		}
	}
	return items, nil
}

func (Agent) IsEmptySession(sessionID string) bool {
	row, ok := findSession(sessionID)
	if !ok || row.wire == "" {
		return true
	}
	first, _ := readWireSummary(row.wire)
	return first == ""
}

func findSession(sessionID string) (sessionRow, bool) {
	for _, row := range scanSessions() {
		if row.id == sessionID {
			return row, true
		}
	}
	return sessionRow{}, false
}

func metaFor(sessionID, cwdHint string) agent.Meta {
	out := agent.Meta{Cwd: cwdHint, TitleSource: "agent"}
	row, ok := findSession(sessionID)
	if !ok {
		return out
	}
	out.Cwd = firstNonEmpty(cwdHint, row.cwd)
	out.NativeTitle = row.title
	out.TitleSource = row.titleFrom
	out.FirstPrompt = scanlib.FirstLine(row.first, 200)
	return out
}

func wireParts(row map[string]any) (map[string]any, map[string]any, int64) {
	message, _ := row["message"].(map[string]any)
	if message == nil {
		message = row
	}
	payload, _ := message["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	at := int64(0)
	if value, ok := row["timestamp"].(float64); ok && value > 0 {
		at = int64(value * 1000)
	}
	return message, payload, at
}

func millisFromAny(value any) int64 {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return int64(v)
		}
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
	case string:
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return ts.UnixMilli()
		}
	}
	return 0
}

func sessionIndex() map[string]string {
	out := map[string]string{}
	for _, line := range scanlib.ReadJSONLines(filepath.Join(Home(), "session_index.jsonl")) {
		id := asString(line["sessionId"])
		workDir := asString(line["workDir"])
		if id != "" && workDir != "" {
			out[id] = workDir
		}
	}
	return out
}

func archivedState(archived bool) string {
	if archived {
		return "archived"
	}
	return ""
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func itoa(n int) string {
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
