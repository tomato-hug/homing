// Package notify decides what deserves a phone notification and when.
//
// P0 (permission / error / approval) goes out immediately as a single push.
// P1 (needs_input / question / awaiting_input) is merged per session for 120s.
// P2 (reply / done / stop / turn_complete / completed) is not pushed one by one:
// the newest per session waits 60s and then becomes one "N 个会话已完成" summary.
// Muted sessions and a paused push switch suppress everything.
//
// Ported from relay/notify.mjs.
//
// Owner: WorkBuddy.
package notify

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent/scanlib"
)

// Merge windows, same as the Node relay.
const (
	P1Window = 120 * time.Second
	P2Window = 60 * time.Second
)

var (
	p0Kinds = map[string]bool{"permission": true, "error": true, "approval": true}
	p1Kinds = map[string]bool{
		"needs_input": true, "question": true, "awaiting_input": true, "collab_result": true,
	}
	p2Kinds         = map[string]bool{"reply": true, "done": true, "stop": true, "turn_complete": true, "completed": true}
	completionKinds = map[string]bool{"done": true, "turn_complete": true, "completed": true}
)

// LevelOf maps an event kind to P0 / P1 / P2, or "" when it deserves no push.
func LevelOf(kind string) string {
	switch {
	case p0Kinds[kind]:
		return "P0"
	case p1Kinds[kind]:
		return "P1"
	case p2Kinds[kind]:
		return "P2"
	}
	return ""
}

// Event is what the notifier needs from one stored event.
type Event struct {
	ID         int64
	Source     string
	SessionID  string
	Kind       string
	DispatchID string
	Title      string
	Text       string
	Cwd        string
	Ts         string
}

// Payload is one push body, ready for the transport.
type Payload struct {
	Type       string           `json:"type"`
	Level      string           `json:"level"`
	Kind       string           `json:"kind,omitempty"`
	Source     string           `json:"source,omitempty"`
	SessionID  string           `json:"session_id,omitempty"`
	DispatchID string           `json:"dispatch_id,omitempty"`
	EventID    int64            `json:"event_id,omitempty"`
	Title      string           `json:"title"`
	Summary    string           `json:"summary,omitempty"`
	At         string           `json:"at"`
	Temp       bool             `json:"temp"`
	Merged     bool             `json:"merged"`
	Count      int              `json:"count"`
	Sessions   []PayloadSession `json:"sessions,omitempty"`
}

// PayloadSession is one line of a merged P2 summary.
type PayloadSession struct {
	Source    string `json:"source"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

// Options wires the notifier to the rest of the process.
type Options struct {
	Sink   func(Payload) // where a finished payload goes
	Muted  func(source, sessionID string) bool
	Paused func() bool
	Away   func() bool
	// Reminder consumes a one-shot "notify me when this session finishes"
	// request. It is checked only after paused and muted filtering so a
	// suppressed event does not burn the reminder.
	Reminder func(source, sessionID string) bool
	Log      func(string, ...any)
}

// Notifier merges events into pushes. Safe for concurrent use.
type Notifier struct {
	opts Options

	mu      sync.Mutex
	p1      map[string]*p1Entry
	p2      map[string]p2Entry
	p2Timer *time.Timer
}

type p1Entry struct {
	payload Payload
	count   int
	timer   *time.Timer
}

type p2Entry struct {
	payload Payload
}

// New builds a notifier.
func New(opts Options) *Notifier {
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}
	return &Notifier{opts: opts, p1: map[string]*p1Entry{}, p2: map[string]p2Entry{}}
}

func keyOf(event Event) string { return event.Source + ":" + event.SessionID }

// OnEvent feeds one event in; merges fire on their own timers.
func (n *Notifier) OnEvent(event Event) {
	if event.Kind == "" || event.SessionID == "" {
		return
	}
	level := LevelOf(event.Kind)
	if level == "" {
		return
	}
	if n.opts.Paused != nil && n.opts.Paused() {
		n.opts.Log("notify: 推送已暂停，跳过 %s", keyOf(event))
		return
	}
	if n.opts.Muted != nil && n.opts.Muted(event.Source, event.SessionID) {
		n.opts.Log("notify: %s muted，跳过", keyOf(event))
		return
	}
	// While the user is away, a finished task is exactly the message they are
	// waiting for. An explicit per-session reminder has the same effect even
	// when the user is not in away mode.
	if level == "P2" && completionKinds[event.Kind] {
		if n.opts.Away != nil && n.opts.Away() {
			level = "P1"
		} else if n.opts.Reminder != nil &&
			n.opts.Reminder(event.Source, event.SessionID) {
			level = "P1"
			n.opts.Log("notify: %s 已提升为完成提醒", keyOf(event))
		}
	}

	key := keyOf(event)
	payload := basePayload(event, level)

	n.mu.Lock()
	defer n.mu.Unlock()

	switch level {
	case "P0":
		// Immediate, and it replaces any pending P1 for the same session.
		if pending, ok := n.p1[key]; ok {
			pending.timer.Stop()
			delete(n.p1, key)
		}
		payload.Merged, payload.Count = false, 1
		n.emit(payload)
	case "P1":
		prev := n.p1[key]
		count := 1
		if prev != nil {
			prev.timer.Stop()
			count = prev.count + 1
		}
		entry := &p1Entry{payload: payload, count: count}
		entry.timer = time.AfterFunc(P1Window, func() { n.flushP1(key) })
		n.p1[key] = entry
	case "P2":
		// Keep only the newest per session and wait for the summary window.
		n.p2[key] = p2Entry{payload: payload}
		if n.p2Timer == nil {
			n.p2Timer = time.AfterFunc(P2Window, n.FlushP2)
		}
	}
}

func (n *Notifier) flushP1(key string) {
	n.mu.Lock()
	entry, ok := n.p1[key]
	if ok {
		delete(n.p1, key)
	}
	n.mu.Unlock()
	if !ok {
		return
	}
	payload := entry.payload
	payload.Merged = entry.count > 1
	payload.Count = entry.count
	n.emit(payload)
}

// FlushP2 publishes the pending P2 summary right away.
func (n *Notifier) FlushP2() {
	n.mu.Lock()
	items := make([]Payload, 0, len(n.p2))
	for _, entry := range n.p2 {
		items = append(items, entry.payload)
	}
	n.p2 = map[string]p2Entry{}
	if n.p2Timer != nil {
		n.p2Timer.Stop()
		n.p2Timer = nil
	}
	n.mu.Unlock()

	if len(items) == 0 {
		return
	}
	if len(items) == 1 {
		payload := items[0]
		payload.Level = "P2"
		payload.Merged, payload.Count = false, 1
		n.emit(payload)
		return
	}
	names := make([]string, 0, 3)
	for _, item := range items {
		if len(names) == 3 {
			break
		}
		names = append(names, scanlib.Truncate(item.SessionID, 8))
	}
	summary := strings.Join(names, " · ")
	if len(items) > 3 {
		summary += " …"
	}
	merged := Payload{
		Type:    "notify",
		Level:   "P2",
		Kind:    "done",
		Title:   itoa(len(items)) + " 个会话已完成",
		Summary: summary,
		At:      time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Merged:  true,
		Count:   len(items),
	}
	for _, item := range items {
		merged.Sessions = append(merged.Sessions, PayloadSession{
			Source: item.Source, SessionID: item.SessionID, Title: item.Title,
		})
	}
	n.emit(merged)
}

// FlushAll publishes every pending merge at once (shutdown).
func (n *Notifier) FlushAll() {
	n.mu.Lock()
	keys := make([]string, 0, len(n.p1))
	for key := range n.p1 {
		keys = append(keys, key)
	}
	n.mu.Unlock()
	for _, key := range keys {
		n.flushP1(key)
	}
	n.FlushP2()
}

func (n *Notifier) emit(payload Payload) {
	if n.opts.Sink == nil {
		return
	}
	n.opts.Sink(payload)
}

func basePayload(event Event, level string) Payload {
	title := event.Title
	if title == "" {
		title = event.Source
	}
	if title == "" {
		title = "Agent"
	}
	at := event.Ts
	if at == "" {
		at = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return Payload{
		Type:       "notify",
		Level:      level,
		Kind:       event.Kind,
		Source:     event.Source,
		SessionID:  event.SessionID,
		DispatchID: event.DispatchID,
		EventID:    event.ID,
		Title:      title,
		Summary:    scanlib.Truncate(collapse(event.Text), 160),
		At:         at,
		Temp:       scanlib.TempCwd(event.Cwd),
	}
}

var spaceRun = regexp.MustCompile(`\s+`)

func collapse(text string) string { return strings.TrimSpace(spaceRun.ReplaceAllString(text, " ")) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 12)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
