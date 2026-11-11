// Package agentconfig stores the small amount of per-agent state owned by the
// desktop console and installs lifecycle hooks into agents that support them.
//
// The on-disk settings file is the same shape as the Node relay's
// data/agents/<id>.json. Hook installation deliberately only touches Claude
// Code and WorkBuddy settings; Codex's trust state is never modified.
package agentconfig

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

// Settings is one row returned by GET /api/agents/settings.
type Settings struct {
	ID           string  `json:"id"`
	Connected    bool    `json:"connected"`
	PushToPhone  bool    `json:"push_to_phone"`
	ReadSessions bool    `json:"read_sessions"`
	PushSignal   string  `json:"push_signal"`
	ReplyMode    string  `json:"reply_mode"`
	Model        *string `json:"model"`
	Order        *int    `json:"order"`
	UpdatedAt    *string `json:"updated_at"`
}

var pushSignals = map[string]bool{"hook": true, "watch": true, "poll": true}
var replyModes = map[string]bool{"auto": true, "queue": true, "off": true}

var hookFiles = map[string]string{
	"claude":    filepath.Join(".claude", "settings.json"),
	"workbuddy": filepath.Join(".workbuddy", "settings.json"),
	"codex":     filepath.Join(".codex", "hooks.json"),
}

var installedHookPattern = regexp.MustCompile(`127\.0\.0\.1:\d+/hook/`)

// Store reads and writes <dataDir>/agents/<id>.json. Home is only used to
// inspect/install hooks and can be overridden by tests.
type Store struct {
	DataDir string
	Home    string
}

// NewStore uses the current user's home directory.
func NewStore(dataDir string) *Store {
	return &Store{DataDir: dataDir, Home: platform.Home()}
}

// Defaults is the Node relay's defaultsFor(), field for field.
func Defaults(id string) Settings {
	signal := "poll"
	if id == "claude" || id == "workbuddy" {
		signal = "hook"
	}
	return Settings{
		ID:           id,
		ReadSessions: true,
		PushSignal:   signal,
		ReplyMode:    "auto",
	}
}

// ValidID matches the adapter id grammar and is also safe as a filename.
func ValidID(id string) bool {
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

// FileFor returns the settings file for an id. The caller must validate id.
func (s *Store) FileFor(id string) string {
	return filepath.Join(s.DataDir, "agents", id+".json")
}

// ConfigFile is the agent configuration file a hook installer may touch. It is
// informational for GET /api/adapters and disconnect responses.
func (s *Store) ConfigFile(id string) string {
	rel, ok := hookFiles[id]
	if !ok {
		return ""
	}
	return filepath.Join(s.Home, rel)
}

// HookInstalled reports whether an agent's config already points at a local
// Homing hook URL. It deliberately does not pin a port: switching 18787 -> 18788
// must not make the console claim every integration disappeared.
func (s *Store) HookInstalled(id string) bool {
	file := s.ConfigFile(id)
	if file == "" {
		return false
	}
	raw, err := os.ReadFile(file)
	return err == nil && installedHookPattern.Match(raw)
}

// Read loads one settings row, filling every missing key from Defaults. In
// particular, an old file without read_sessions must not turn reading off.
func (s *Store) Read(id string) Settings {
	out := Defaults(id)
	var raw map[string]any
	if !platform.ReadJSON(s.FileFor(id), &raw) {
		return out
	}
	if value, ok := raw["connected"].(bool); ok {
		out.Connected = value
	}
	if value, ok := raw["push_to_phone"].(bool); ok {
		out.PushToPhone = value
	}
	if value, ok := raw["read_sessions"].(bool); ok {
		out.ReadSessions = value
	}
	if value, ok := raw["push_signal"].(string); ok && pushSignals[value] {
		out.PushSignal = value
	}
	if value, ok := raw["reply_mode"].(string); ok && replyModes[value] {
		out.ReplyMode = value
	}
	if value, ok := raw["model"].(string); ok {
		if value != "" {
			out.Model = &value
		}
	}
	if value, ok := integer(raw["order"]); ok {
		out.Order = &value
	}
	if value, ok := raw["updated_at"].(string); ok && value != "" {
		out.UpdatedAt = &value
	}
	out.ID = id
	return out
}

// Patch applies the same subset of fields and coupled booleans as the Node
// relay's agentSettings.patch().
func (s *Store) Patch(id string, changes map[string]any) (Settings, error) {
	next := s.Read(id)

	pushChanged := false
	if value, ok := changes["push_to_phone"].(bool); ok {
		next.PushToPhone = value
		next.Connected = value
		pushChanged = true
	}
	if value, ok := changes["connected"].(bool); ok {
		next.Connected = value
		if !value && !pushChanged {
			next.PushToPhone = false
		}
	}
	if value, ok := changes["read_sessions"].(bool); ok {
		next.ReadSessions = value
	}
	if value, ok := changes["push_signal"].(string); ok && pushSignals[value] {
		next.PushSignal = value
	}
	if value, ok := changes["reply_mode"].(string); ok && replyModes[value] {
		next.ReplyMode = value
	}
	if value, exists := changes["model"]; exists {
		if value == nil {
			next.Model = nil
		} else if text, ok := value.(string); ok {
			if text == "" {
				next.Model = nil
			} else {
				next.Model = &text
			}
		}
	}
	if value, ok := integer(changes["order"]); ok {
		next.Order = &value
	}
	next.ID = id
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	next.UpdatedAt = &stamp
	if err := s.write(id, next); err != nil {
		return next, err
	}
	return next, nil
}

func (s *Store) write(id string, value Settings) error {
	dir := filepath.Dir(s.FileFor(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return platform.WriteJSON(s.FileFor(id), value)
}

// Snapshot merges stored settings with the actual hook state for agents whose
// user has never changed a setting. This keeps upgrades from showing every
// previously configured agent as disconnected.
func (s *Store) Snapshot(ids []string) map[string]Settings {
	out := make(map[string]Settings, len(ids))
	for _, id := range ids {
		row := s.Read(id)
		if row.UpdatedAt == nil {
			installed := s.HookInstalled(id)
			row.Connected = installed
			row.PushToPhone = installed
		}
		out[id] = row
	}
	return out
}

// Ordered returns stored order first, then the registry order for rows without
// an explicit order. This is exactly orderedIds() in the Node relay.
func (s *Store) Ordered(ids []string) []string {
	type orderedID struct {
		id    string
		order int
	}
	var ordered []orderedID
	var rest []string
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		row := s.Read(id)
		if row.Order != nil {
			ordered = append(ordered, orderedID{id: id, order: *row.Order})
		} else {
			rest = append(rest, id)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].order < ordered[j].order })
	out := make([]string, 0, len(ids))
	for _, item := range ordered {
		out = append(out, item.id)
	}
	return append(out, rest...)
}

// SetOrder writes an explicit order for each known id.
func (s *Store) SetOrder(ids []string) ([]string, error) {
	clean := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !ValidID(id) || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	for index, id := range clean {
		row := s.Read(id)
		value := index
		row.Order = &value
		stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		row.UpdatedAt = &stamp
		if err := s.write(id, row); err != nil {
			return clean, err
		}
	}
	return clean, nil
}

func integer(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, true
	case int64:
		return int(number), true
	case float64:
		if number != float64(int(number)) {
			return 0, false
		}
		return int(number), true
	default:
		return 0, false
	}
}
