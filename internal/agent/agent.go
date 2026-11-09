// Package agent defines what every coding agent integration provides, and the
// registry the HTTP API reads from. Built-in agents live in subpackages
// (internal/agent/codex, internal/agent/claude, ...) and register themselves in
// init(); custom agents from YAML manifests and script adapters are added by
// internal/adapter through RegisterProvider.
//
// JSON field names match the Node relay (relay/scanners, docs/relay-v2.md) so the
// existing Android app keeps working against the Go server unchanged. If the Node
// output turns out to differ from a tag here, fix the tag, not the app.
package agent

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// PushSignal says how Homing learns that something happened in an agent.
type PushSignal string

const (
	PushHook       PushSignal = "hook"        // the agent calls our hook script
	PushHTTPEvents PushSignal = "http_events" // the agent runs a local server with an event stream
	PushPoll       PushSignal = "poll"        // we re-read its files on a timer
)

// ErrUnsupported is returned by optional capabilities an agent does not have.
var ErrUnsupported = errors.New("agent: unsupported")

// Detection is the result of looking for an agent on this machine.
type Detection struct {
	Installed bool    `json:"installed"`
	Version   *string `json:"version"`
	Detail    string  `json:"detail,omitempty"`
}

// Capabilities is published as-is in GET /api/agents.
type Capabilities struct {
	Reply    bool `json:"reply"`
	Sessions bool `json:"sessions"`
	Models   bool `json:"models"`
}

// Session is one row of GET /api/sessions before the API layer adds governance
// fields (read / pinned / muted / archived / is_temp / is_empty).
type Session struct {
	SessionID   string `json:"session_id"`
	Source      string `json:"source"`
	Title       string `json:"title"`
	TitleSource string `json:"title_source"`
	LastTS      string `json:"last_ts"`
	NativeState string `json:"native_state"`
	Cwd         string `json:"cwd,omitempty"`
	Model       string `json:"model,omitempty"`
}

// Meta is the extra detail the API asks for when it renders one session.
type Meta struct {
	Cwd         string `json:"cwd,omitempty"`
	NativeTitle string `json:"native_title,omitempty"`
	TitleSource string `json:"title_source,omitempty"`
	FirstPrompt string `json:"first_prompt,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Model       string `json:"model,omitempty"`
}

// Item is one entry of a session transcript.
type Item struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Kind  string `json:"kind"`
	Seq   int    `json:"seq"`
	At    string `json:"at"`
	Text  string `json:"text"`
	Error string `json:"error,omitempty"`
}

// Model is one selectable model.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ReplyOpts carries what the phone sent along with a reply.
type ReplyOpts struct {
	Model  string
	Images []string // local file paths of uploaded attachments
}

// Event is something that happened in an agent session. Kind uses the same words
// as the Node relay's events: message, needs_reply, approval, done, error, turn.
type Event struct {
	Source    string         `json:"source"`
	SessionID string         `json:"session_id"`
	Kind      string         `json:"kind"`
	Title     string         `json:"title,omitempty"`
	Text      string         `json:"text,omitempty"`
	At        time.Time      `json:"at"`
	Data      map[string]any `json:"data,omitempty"`
}

// Agent is the part every integration must implement.
type Agent interface {
	ID() string   // stable id; appears as `source` in the API
	Name() string // display name
	Icon() string // 16×16 monochrome SVG using currentColor
	PushSignal() PushSignal
	Capabilities() Capabilities
	Detect(ctx context.Context) Detection
	ListSessions(ctx context.Context, limit int) ([]Session, error)
	Meta(sessionID, cwdHint string) Meta
}

// Optional capabilities. An agent supports one exactly when it implements it.
type (
	ItemReader interface {
		ReadItems(ctx context.Context, sessionID string, limit int) ([]Item, error)
	}
	EmptyChecker interface {
		IsEmptySession(sessionID string) bool
	}
	Replier interface {
		Reply(ctx context.Context, sessionID, text string, opts ReplyOpts) error
	}
	ModelLister interface {
		Models(ctx context.Context) (models []Model, defaultModel string, err error)
	}
	// Watcher pushes events as they happen (file watch, hook, local event stream).
	// Agents without it are polled by the framework.
	Watcher interface {
		Watch(ctx context.Context, emit func(Event)) error
	}
)

// Provider returns agents that are only known at runtime (custom manifests,
// script adapters). It is called on every All() so edits on disk show up.
type Provider func(ctx context.Context) []Agent

var (
	mu        sync.RWMutex
	builtins  []Agent
	byID      = map[string]Agent{}
	providers []namedProvider
)

type namedProvider struct {
	name string
	fn   Provider
}

// Register adds a built-in agent. Call it from the agent package's init().
// Registering the same id twice panics: it is always a programming error.
func Register(a Agent) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := byID[a.ID()]; dup {
		panic("agent: duplicate id " + a.ID())
	}
	builtins = append(builtins, a)
	byID[a.ID()] = a
}

// RegisterProvider adds a source of runtime agents.
func RegisterProvider(name string, p Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers = append(providers, namedProvider{name, p})
}

// All returns built-in agents in registration order, then provider agents
// sorted by id. A provider agent whose id collides with a built-in is skipped.
func All(ctx context.Context) []Agent {
	mu.RLock()
	out := append([]Agent(nil), builtins...)
	seen := make(map[string]bool, len(byID))
	for id := range byID {
		seen[id] = true
	}
	ps := append([]namedProvider(nil), providers...)
	mu.RUnlock()

	var extra []Agent
	for _, p := range ps {
		for _, a := range p.fn(ctx) {
			if a == nil || seen[a.ID()] {
				continue
			}
			seen[a.ID()] = true
			extra = append(extra, a)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].ID() < extra[j].ID() })
	return append(out, extra...)
}

// Get finds an agent by id among built-ins and providers.
func Get(ctx context.Context, id string) (Agent, bool) {
	mu.RLock()
	a, ok := byID[id]
	mu.RUnlock()
	if ok {
		return a, true
	}
	for _, a := range All(ctx) {
		if a.ID() == id {
			return a, true
		}
	}
	return nil, false
}
