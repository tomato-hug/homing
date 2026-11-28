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

// ErrQueued means the agent was busy with a previous injection and the message
// was queued behind it. The reply is accepted, so this is not a failure.
var ErrQueued = errors.New("agent: queued")

// ErrHeld means the session looks busy inside its own desktop app; the message
// waits for the next stop hook instead of being injected right now.
var ErrHeld = errors.New("agent: held")

// Detection is the result of looking for an agent on this machine.
type Detection struct {
	Installed bool    `json:"installed"`
	Version   *string `json:"version"`
	Detail    string  `json:"detail,omitempty"`
}

// Capabilities is published as-is in GET /api/agents.
type Capabilities struct {
	// Core keeps the legacy /api/agents shape for the three first-party
	// integrations. It is metadata only and never serialized directly.
	Core            bool     `json:"-"`
	Reply           bool     `json:"reply"`
	Sessions        bool     `json:"sessions"`
	Models          bool     `json:"models"`
	NewSession      bool     `json:"new_session,omitempty"`
	NewSessionAny   bool     `json:"-"`
	Stop            bool     `json:"stop,omitempty"`
	Approve         bool     `json:"approve,omitempty"`
	Live            bool     `json:"live,omitempty"`
	Dispatch        bool     `json:"dispatch,omitempty"`
	SwitchModel     bool     `json:"switch_model,omitempty"`
	Effort          bool     `json:"effort,omitempty"`
	PermissionModes []string `json:"permission_modes,omitempty"`
}

// StartRequest is the provider-neutral description of a new session. Agents
// map the fields they understand; unsupported values are rejected by the API
// before they reach the vendor CLI.
type StartRequest struct {
	Cwd            string
	Text           string
	Files          []string
	Model          string
	Effort         string
	PermissionMode string
	Origin         string
}

// StartResult reports a freshly launched session. RequestID is used when the
// agent cannot name the session until its first hook arrives.
type StartResult struct {
	SessionID string
	RequestID string
	Status    string
	Message   string
}

// PermissionDecision is the provider-neutral answer to a permission request.
// OptionID is used by ACP when the agent offered more than allow/deny.
type PermissionDecision struct {
	Allow    bool
	OptionID string
}

// Session is one row of GET /api/sessions before the API layer adds governance
// fields (read / pinned / muted / archived / is_temp / is_empty).
type Session struct {
	SessionID   string `json:"session_id"`
	Source      string `json:"source"`
	Title       string `json:"title"`
	TitleSource string `json:"title_source"`
	LastTS      string `json:"last_ts"`
	// LastUserTS is the last real user-authored turn when the scanner can find
	// it. The home "resume" zone prefers it over LastTS so an agent's later
	// autonomous progress does not outrank a session the user just touched.
	LastUserTS  string `json:"last_user_ts,omitempty"`
	NativeState string `json:"native_state"`
	Cwd         string `json:"cwd,omitempty"`
	Model       string `json:"model,omitempty"`
	LastPreview string `json:"last_preview,omitempty"`
}

// Meta is the extra detail the API asks for when it renders one session. The
// empty string means "unknown"; the API turns it into a JSON null.
type Meta struct {
	Cwd            string `json:"cwd,omitempty"`
	NativeTitle    string `json:"native_title,omitempty"`
	TitleSource    string `json:"title_source,omitempty"`
	FirstPrompt    string `json:"first_prompt,omitempty"`
	Branch         string `json:"branch,omitempty"`
	Model          string `json:"model,omitempty"`
	Effort         string `json:"effort,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	LastUserTS     string `json:"last_user_ts,omitempty"`
	// Workspace overrides the workspace chip derived from cwd (WorkBuddy reports
	// the space name, which is not simply the parent directory).
	Workspace string `json:"workspace,omitempty"`
}

// Item is one entry of a session transcript. The JSON shape is the Node relay's
// extractItems output, key for key (relay/server.mjs): `ts` / `type` rather than
// `at` / `kind`, and the same nested objects.
//
// Fields Node writes as explicit null (tool.input_summary, tool.output_tail,
// tool.exit_code) are pointers without omitempty so they marshal back to null;
// fields Node omits entirely (text, model, command) keep omitempty.
type Item struct {
	TS       string     `json:"ts"`
	ID       string     `json:"id"`
	Seq      int        `json:"seq"`
	Type      string         `json:"type"`
	Role      string         `json:"role"`
	Partial   bool           `json:"partial,omitempty"`
	Title     string         `json:"title,omitempty"`
	Text      string         `json:"text,omitempty"`
	Model    string     `json:"model,omitempty"`
	Command  string     `json:"command,omitempty"`
	ExitCode *int       `json:"exit_code,omitempty"`
	Tool     *ItemTool  `json:"tool,omitempty"`
	Files    []ItemFile `json:"files,omitempty"`
	Plan     []ItemPlan `json:"plan,omitempty"`
	Error    string     `json:"error,omitempty"`
	// Truncated is set on live SSE frames when a very large diff or output had
	// to be shortened. The full item remains available from the items endpoint.
	Truncated bool           `json:"truncated,omitempty"`
	Questions []ItemQuestion `json:"questions,omitempty"`
}

// ItemTool is the tool block of a tool_call / file_change entry.
type ItemTool struct {
	Name         string  `json:"name"`
	InputSummary *string `json:"input_summary"`
	OutputTail   *string `json:"output_tail"`
	ExitCode     *int    `json:"exit_code"`
	Status       string  `json:"status"`
}

// ItemFile is one touched file of a file_change entry.
type ItemFile struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Diff    string `json:"diff"`
}

// ItemPlan is one step of a plan entry.
type ItemPlan struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

// Model is one selectable model.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ReplyOpts carries what the phone sent along with a reply.
type ReplyOpts struct {
	Model          string
	PermissionMode string
	Images         []string // local file paths of uploaded attachments
	ReplyID        int64    // Homing reply id, used to cancel a queued message
}

// Event is something that happened in an agent session. The event log uses the
// relay's internal vocabulary (needs_input, permission, done, error, message);
// public ingest/hook aliases are canonicalised before storage.
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
	// SessionStarter creates an independent session. It is deliberately
	// separate from Replier: remote control of an existing transcript and
	// launching a new process have different safety requirements.
	SessionStarter interface {
		StartSession(ctx context.Context, req StartRequest) (StartResult, error)
	}
	// Stopper stops a turn or process that Homing owns. Agents must return
	// ErrUnsupported when the session was not launched by Homing.
	Stopper interface {
		StopSession(ctx context.Context, sessionID string) error
	}
	// PermissionResponder answers a permission request without going through
	// the legacy Stop-hook hold. ACP implementations use it for option IDs.
	PermissionResponder interface {
		RespondPermission(ctx context.Context, sessionID string, eventID int64, decision PermissionDecision) error
	}
	// HookApprover reports the agent settings file and endpoint that must be
	// configured before permission events can reach Homing.
	HookApprover interface {
		ApprovalHook() (settingsPath, endpoint string)
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

// EffectiveCapabilities overlays optional interface implementations onto the
// static declaration. This keeps /api/agents truthful even when a provider
// agent forgets to fill every redundant flag.
func EffectiveCapabilities(a Agent) Capabilities {
	caps := a.Capabilities()
	// Trust an integration that explicitly reports sessions=false. A drive-only
	// manifest still implements the optional ItemReader method set, but it has
	// no verified transcript source and must not claim L1.
	if a.PushSignal() != PushPoll {
		caps.Live = true
	}
	return caps
}

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
