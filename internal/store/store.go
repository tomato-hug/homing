// Package store keeps homing's own state in the data directory: the event log,
// sent replies, per-session governance (read / pinned / muted / archived),
// paired devices and the local bearer token.
//
// The file formats are the ones the Node relay writes (docs/relay-v2.md), so a
// Go server can be started on a copy of relay/data and answer with the same
// payloads while both run side by side.
//
// Owner: WorkBuddy.
package store

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

// Event is one line of events.jsonl. Attachments and SessionMeta are stored by
// the Node relay too and are returned to the client as they were written.
type Event struct {
	ID        int64  `json:"id"`
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Project   string `json:"project"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Ts        string `json:"ts"`
	Read      bool   `json:"read"`
	// nil means the event never had the key (older lines): it stays omitted.
	Attachments *[]any         `json:"attachments,omitempty"`
	SessionMeta map[string]any `json:"session_meta,omitempty"`
	// LocalhostLinks is derived when the event is served, never stored: the
	// phone offers a "open on this Mac" button for those ports.
	LocalhostLinks []int `json:"localhost_links,omitempty"`
}

// Reply is one line of replies.jsonl. Field names follow what the Node relay
// writes, so a phone reading an old file sees the same keys.
type Reply struct {
	ID             int64    `json:"id"`
	EventID        *int64   `json:"event_id"`
	SessionID      string   `json:"session_id"`
	Source         string   `json:"source,omitempty"`
	Cwd            string   `json:"cwd,omitempty"`
	Model          string   `json:"model,omitempty"`
	Effort         string   `json:"effort,omitempty"`
	PermissionMode string   `json:"permission_mode,omitempty"`
	Text           string   `json:"text"`
	Files          []string `json:"files"`
	Ts             string   `json:"ts"`
	Injected       bool     `json:"injected"`
	Status         string   `json:"status,omitempty"`
	Message        string   `json:"message,omitempty"`
}

// Dispatch is one cross-agent task started from a phone reply. The file is
// append-only like replies: the last line for a dispatch id wins on startup.
// It is a Homing-only format; events.jsonl and replies.jsonl keep their Node
// compatible shapes.
type Dispatch struct {
	ID                  string          `json:"dispatch_id"`
	ParentSessionID     string          `json:"parent_session_id"`
	ParentSource        string          `json:"parent_source"`
	ParentEventID       *int64          `json:"parent_event_id"`
	TargetAgent         string          `json:"target_agent"`
	TargetSessionID     string          `json:"target_session_id,omitempty"`
	RequestID           string          `json:"request_id,omitempty"`
	Text                string          `json:"text"`
	Cwd                 string          `json:"cwd"`
	Mode                string          `json:"mode"`
	Preset              string          `json:"preset,omitempty"`
	Workspace           string          `json:"workspace,omitempty"`
	Thread              string          `json:"thread,omitempty"`
	Branch              string          `json:"branch,omitempty"`
	BaseCommit          string          `json:"base_commit,omitempty"`
	RepoCwd             string          `json:"repo_cwd,omitempty"`
	WorktreeDiscardedAt string          `json:"worktree_discarded_at,omitempty"`
	Return              string          `json:"return,omitempty"`
	Status              string          `json:"status"`
	Message             string          `json:"message,omitempty"`
	Result              *DispatchResult `json:"result,omitempty"`
	CreatedAt           string          `json:"created_at"`
	UpdatedAt           string          `json:"updated_at"`
}

// DispatchResult is the final answer a child task hands back to its parent.
type DispatchResult struct {
	Text         string   `json:"text"`
	FilesChanged []string `json:"files_changed,omitempty"`
	FinishedAt   string   `json:"finished_at"`
	Branch       string   `json:"branch,omitempty"`
	DiffStat     string   `json:"diff_stat,omitempty"`
}

// AwayPeriod is one stretch of "away from the Mac" mode. The file is
// append-only; the last line for an id wins, like dispatches.jsonl.
type AwayPeriod struct {
	ID        string `json:"id"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Source    string `json:"source"`
	DeviceID  string `json:"device_id,omitempty"`
	ClearedAt string `json:"cleared_at,omitempty"`
}

// State is the governance triple plus archived, exactly as session-state.json stores it.
type State struct {
	Read         bool   `json:"read"`
	Pinned       bool   `json:"pinned"`
	Muted        bool   `json:"muted"`
	Archived     bool   `json:"archived"`
	RemindOnDone bool   `json:"remind_on_done,omitempty"`
	SeenEventID  int64  `json:"seen_event_id,omitempty"`
	SeenAt       string `json:"seen_at,omitempty"`
	Title        string `json:"title,omitempty"`
}

type stateRecord struct {
	Read         bool   `json:"read"`
	Pinned       bool   `json:"pinned"`
	Muted        bool   `json:"muted"`
	Archived     bool   `json:"archived"`
	RemindOnDone bool   `json:"remind_on_done,omitempty"`
	SeenEventID  int64  `json:"seen_event_id,omitempty"`
	SeenAt       string `json:"seen_at,omitempty"`
	Title        string `json:"title,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

type stateFile struct {
	Version  int                    `json:"version"`
	Sessions map[string]stateRecord `json:"sessions"`
}

// Config is config.json: the local token plus feature switches.
type Config struct {
	Token string `json:"token"`
	Away  struct {
		Enabled         bool `json:"enabled"`
		AutoLock        bool `json:"auto_lock,omitempty"`
		AutoLockMinutes int  `json:"auto_lock_minutes,omitempty"`
	} `json:"away"`
	Home struct {
		// NewActivity is "away" (default) or "unread".
		NewActivity string `json:"new_activity,omitempty"`
		// CollabReturn is the default for cross-agent results. "inject" must
		// be chosen explicitly in settings; anything else falls back to card.
		CollabReturn string `json:"collab_return,omitempty"`
	} `json:"home"`
	Search struct {
		Disabled bool `json:"disabled,omitempty"`
	} `json:"search"`
	Proxy struct {
		Enabled bool  `json:"enabled"`
		Ports   []int `json:"ports"`
	} `json:"proxy"`
}

// Device is one paired phone.
type Device struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
	CreatedAt  string `json:"created_at"`
	LastSeenMs int64  `json:"last_seen_ms"`
	PairedVia  string `json:"paired_via"`
	LastIP     string `json:"last_ip"`
	// Node writes token_hash; keeping the wire name identical is what lets a
	// phone paired by the Node relay keep working after the switch to Go.
	TokenSHA256 string `json:"token_hash"`
	// Scopes limits what this device may do. An empty list means every scope,
	// which keeps devices paired by older relays working unchanged.
	Scopes []string `json:"scopes,omitempty"`
}

// Offer is a one-shot pairing offer (5 minutes, 6-digit code).
type Offer struct {
	ID              string   `json:"id"`
	Token           string   `json:"token"`
	Code            string   `json:"code"`
	CreatedMs       int64    `json:"created_ms"`
	ExpiresMs       int64    `json:"expires_ms"`
	ClaimedMs       int64    `json:"claimed_ms"`
	ClaimedDeviceID string   `json:"claimed_device_id"`
	Attempts        int      `json:"attempts"`
	LanURL          string   `json:"lan_url"`
	LanURLs         []string `json:"lan_urls"`
	QRPayload       string   `json:"qr_payload"`
}

type devicesFile struct {
	Version int      `json:"version"`
	Devices []Device `json:"devices"`
	Offers  []Offer  `json:"offers"`
}

// Store is safe for concurrent use; the API reads it from many goroutines.
type Store struct {
	Paths platform.Paths

	mu         sync.RWMutex
	events     []Event
	replies    []Reply
	dispatches []Dispatch
	away       []AwayPeriod
	states     map[string]stateRecord
	config     Config
	devices    devicesFile
	history    *historyDB
}

// Open loads everything from disk and creates config.json (with a fresh token)
// when it does not exist yet.
func Open(p platform.Paths) (*Store, error) {
	s := &Store{Paths: p, states: map[string]stateRecord{}}

	if err := s.loadConfig(); err != nil {
		return nil, err
	}
	history, err := openHistory(p.History)
	if err != nil {
		return nil, err
	}
	if err := history.syncJSONL(p); err != nil {
		_ = history.Close()
		return nil, err
	}
	s.history = history
	if s.events, err = history.loadEvents(5000); err != nil {
		_ = history.Close()
		return nil, err
	}
	if s.replies, err = history.loadReplies(); err != nil {
		_ = history.Close()
		return nil, err
	}
	if s.dispatches, err = history.loadDispatches(); err != nil {
		_ = history.Close()
		return nil, err
	}
	if s.away, err = history.loadAwayPeriods(); err != nil {
		_ = history.Close()
		return nil, err
	}
	s.confirmHistoricalQueuedReplies()

	var sf stateFile
	if platform.ReadJSON(p.SessionState, &sf) && sf.Sessions != nil {
		s.states = sf.Sessions
	}
	var df devicesFile
	if platform.ReadJSON(p.Devices, &df) {
		s.devices = df
	}
	if s.devices.Devices == nil {
		s.devices.Devices = []Device{}
	}
	if s.devices.Offers == nil {
		s.devices.Offers = []Offer{}
	}
	s.devices.Version = 1
	return s, nil
}

// Close releases the history database. The JSONL files are already flushed on
// every write, so closing is only needed at process shutdown or in tests.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.history == nil {
		return nil
	}
	history := s.history
	s.history = nil
	return history.Close()
}

// loadConfig reads config.json, creating one with a random 32-char token if missing.
func (s *Store) loadConfig() error {
	if platform.ReadJSON(s.Paths.Config, &s.config) && s.config.Token != "" {
		return nil
	}
	token, err := randomHex(16)
	if err != nil {
		return err
	}
	s.config = Config{Token: token}
	return platform.WriteJSON(s.Paths.Config, s.config)
}

// Token is the local bearer token (never shown in API responses).
func (s *Store) Token() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Token
}

// ---- events --------------------------------------------------------------

// Events returns a copy of the in-memory event log.
func (s *Store) Events() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

// LastEventID is the highest id handed out so far.
func (s *Store) LastEventID() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.events) == 0 {
		return 0
	}
	return s.events[len(s.events)-1].ID
}

// AppendEvent stores an event (assigning the id) and appends it to events.jsonl.
func (s *Store) AppendEvent(e Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == 0 {
		var max int64
		for _, existing := range s.events {
			if existing.ID > max {
				max = existing.ID
			}
		}
		e.ID = max + 1
	}
	if e.Ts == "" {
		e.Ts = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	s.events = append(s.events, e)
	if err := appendJSONL(s.Paths.Events, e); err != nil {
		return e, err
	}
	if s.history != nil {
		if err := s.history.putEvent(e); err != nil {
			return e, err
		}
	}
	// 只在内存里留最近 5000 条，文件不受影响（手机靠 since 拉增量）
	if len(s.events) > 5000 {
		s.events = s.events[len(s.events)-5000:]
	}
	return e, nil
}

// ConfirmQueuedRepliesForAgentEvent marks at most the oldest queued reply as
// delivered when the agent produces a new reply event. The queue can outlive
// the CLI invocation that accepted it (for example when Codex stores the
// message but cannot resume the daemon), so the agent's next visible reply is
// the authoritative signal that the user's message was consumed.
func (s *Store) ConfirmQueuedRepliesForAgentEvent(event Event) []Reply {
	if event.Kind != "reply" || event.SessionID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.replies {
		reply := &s.replies[i]
		if !replyMatchesAgentEvent(*reply, event) {
			continue
		}
		reply.Status = "injected"
		reply.Message = "Agent 已产生后续回复，确认已送达"
		reply.Injected = true
		updated := *reply
		if err := appendJSONL(s.Paths.Replies, updated); err != nil {
			return nil
		}
		if s.history != nil {
			_ = s.history.putReply(updated)
		}
		return []Reply{updated}
	}
	return nil
}

func (s *Store) confirmHistoricalQueuedReplies() {
	claimed := map[int64]bool{}
	for i := range s.replies {
		reply := &s.replies[i]
		if reply.Status != "queued" {
			continue
		}
		for _, event := range s.events {
			if claimed[event.ID] || !replyMatchesAgentEvent(*reply, event) {
				continue
			}
			claimed[event.ID] = true
			reply.Status = "injected"
			reply.Message = "Agent 已产生后续回复，确认已送达"
			reply.Injected = true
			break
		}
	}
}

func replyMatchesAgentEvent(reply Reply, event Event) bool {
	if event.Kind != "reply" || event.SessionID == "" ||
		reply.Status != "queued" || reply.SessionID != event.SessionID {
		return false
	}
	if reply.Source != "" && event.Source != "" && reply.Source != event.Source {
		return false
	}
	return reply.Ts == "" || event.Ts == "" || reply.Ts <= event.Ts
}

// MarkRead flips the read flag on one or more events.
func (s *Store) MarkRead(ids []int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := map[int64]bool{}
	for _, id := range ids {
		set[id] = true
	}
	for i := range s.events {
		if set[s.events[i].ID] {
			s.events[i].Read = true
		}
	}
	if s.history != nil {
		_ = s.history.markRead(ids)
	}
}

// ---- replies -------------------------------------------------------------

// Replies returns a copy of the reply log.
func (s *Store) Replies() []Reply {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Reply, len(s.replies))
	copy(out, s.replies)
	return out
}

// AppendReply records what the phone sent and what happened to it.
func (s *Store) AppendReply(r Reply) (Reply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == 0 {
		var max int64
		for _, existing := range s.replies {
			if existing.ID > max {
				max = existing.ID
			}
		}
		r.ID = max + 1
	}
	if r.Ts == "" {
		r.Ts = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if r.Status == "" {
		r.Status = "queued"
	}
	if r.Files == nil {
		r.Files = []string{}
	}
	s.replies = append(s.replies, r)
	if err := appendJSONL(s.Paths.Replies, r); err != nil {
		return r, err
	}
	if s.history != nil {
		if err := s.history.putReply(r); err != nil {
			return r, err
		}
	}
	return r, nil
}

// SetReplyStatus updates the status/message of an existing reply.
func (s *Store) SetReplyStatus(id int64, status, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.replies {
		if s.replies[i].ID == id {
			if status == "queued" && replyStatusIsDelivered(s.replies[i].Status) {
				return
			}
			s.replies[i].Status = status
			s.replies[i].Message = message
			s.replies[i].Injected = status == "injected" || status == "held"
			if s.history != nil {
				_ = s.history.putReply(s.replies[i])
			}
			return
		}
	}
}

// ---- cross-agent dispatches ---------------------------------------------

// Dispatches returns a copy of the cross-agent dispatch log in insertion order.
func (s *Store) Dispatches() []Dispatch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Dispatch, len(s.dispatches))
	copy(out, s.dispatches)
	return out
}

// SaveDispatch inserts or replaces one dispatch and appends the new revision.
// Callers own the status transitions; CreatedAt is preserved on updates.
func (s *Store) SaveDispatch(dispatch Dispatch) (Dispatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if dispatch.CreatedAt == "" {
		dispatch.CreatedAt = now
	}
	dispatch.UpdatedAt = now
	if dispatch.Mode == "" {
		dispatch.Mode = "parallel"
	}
	if dispatch.Status == "" {
		dispatch.Status = "starting"
	}
	for i := range s.dispatches {
		if s.dispatches[i].ID == dispatch.ID {
			s.dispatches[i] = dispatch
			if err := appendJSONL(s.Paths.Dispatches, dispatch); err != nil {
				return dispatch, err
			}
			if s.history != nil {
				if err := s.history.putDispatch(dispatch); err != nil {
					return dispatch, err
				}
			}
			return dispatch, nil
		}
	}
	s.dispatches = append(s.dispatches, dispatch)
	if err := appendJSONL(s.Paths.Dispatches, dispatch); err != nil {
		return dispatch, err
	}
	if s.history != nil {
		if err := s.history.putDispatch(dispatch); err != nil {
			return dispatch, err
		}
	}
	return dispatch, nil
}

// BindDispatchTarget attaches the session id a launcher only discovered later
// (Claude Code's Stop hook, for example).
func (s *Store) BindDispatchTarget(requestID, targetAgent, targetSessionID string) (Dispatch, bool) {
	if requestID == "" || targetSessionID == "" {
		return Dispatch{}, false
	}
	dispatches := s.Dispatches()
	for _, dispatch := range dispatches {
		if dispatch.RequestID != requestID || dispatch.TargetAgent != targetAgent {
			continue
		}
		dispatch.TargetSessionID = targetSessionID
		dispatch.RequestID = ""
		dispatch.Status = "started"
		dispatch.Message = "已关联到目标会话"
		updated, err := s.SaveDispatch(dispatch)
		return updated, err == nil
	}
	return Dispatch{}, false
}

// ---- session state -------------------------------------------------------

// SessionState returns the governance flags for one session.
//
// Read is computed from the watermark once a session has one. That is what
// makes a new event turn a previously read session unread again; the legacy
// boolean stays in the file so an older relay can still read it.
func (s *Store) SessionState(source, sessionID string) State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.states[source+":"+sessionID]
	out := State{
		Read:         rec.Read,
		Pinned:       rec.Pinned,
		Muted:        rec.Muted,
		Archived:     rec.Archived,
		RemindOnDone: rec.RemindOnDone,
		SeenEventID:  rec.SeenEventID,
		SeenAt:       rec.SeenAt,
		Title:        rec.Title,
	}
	if rec.SeenAt != "" {
		maxID := s.maxEventIDLocked(source, sessionID)
		out.Read = maxID == 0 || rec.SeenEventID >= maxID
	}
	return out
}

// PatchSession updates the given flags and rewrites session-state.json.
func (s *Store) PatchSession(source, sessionID string, patch map[string]bool) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.patchSessionLocked(source, sessionID, patch)
}

// PatchSessionFields updates booleans and the optional custom title. It is the
// single write path used by PATCH /api/sessions/{id}.
func (s *Store) PatchSessionFields(source, sessionID string, patch map[string]bool, title *string) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := source + ":" + sessionID
	rec := s.states[key]
	if title != nil {
		rec.Title = truncateRunes(strings.TrimSpace(*title), 120)
	}
	if err := s.applyPatchLocked(source, sessionID, &rec, patch); err != nil {
		return State{}, err
	}
	rec.UpdatedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	s.states[key] = rec
	err := platform.WriteJSON(s.Paths.SessionState, stateFile{Version: 1, Sessions: s.states})
	return s.statePayloadLocked(source, sessionID, rec), err
}

func (s *Store) patchSessionLocked(source, sessionID string, patch map[string]bool) (State, error) {
	key := source + ":" + sessionID
	rec := s.states[key]
	if err := s.applyPatchLocked(source, sessionID, &rec, patch); err != nil {
		return State{}, err
	}
	rec.UpdatedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	s.states[key] = rec
	err := platform.WriteJSON(s.Paths.SessionState, stateFile{Version: 1, Sessions: s.states})
	return s.statePayloadLocked(source, sessionID, rec), err
}

func (s *Store) applyPatchLocked(source, sessionID string, rec *stateRecord, patch map[string]bool) error {
	for _, field := range []string{"read", "pinned", "muted", "archived", "remind_on_done"} {
		v, ok := patch[field]
		if !ok {
			continue
		}
		switch field {
		case "read":
			rec.Read = v
			if v {
				s.advanceSeenLocked(source, sessionID, rec)
			} else {
				rec.SeenEventID = 0
				rec.SeenAt = ""
			}
		case "pinned":
			rec.Pinned = v
		case "muted":
			rec.Muted = v
		case "archived":
			rec.Archived = v
		case "remind_on_done":
			rec.RemindOnDone = v
		}
	}
	return nil
}

func (s *Store) statePayloadLocked(source, sessionID string, rec stateRecord) State {
	out := State{
		Read:         rec.Read,
		Pinned:       rec.Pinned,
		Muted:        rec.Muted,
		Archived:     rec.Archived,
		RemindOnDone: rec.RemindOnDone,
		SeenEventID:  rec.SeenEventID,
		SeenAt:       rec.SeenAt,
		Title:        rec.Title,
	}
	if rec.SeenAt != "" {
		maxID := s.maxEventIDLocked(source, sessionID)
		out.Read = maxID == 0 || rec.SeenEventID >= maxID
	}
	return out
}

// ConsumeRemindOnDone atomically clears and reports a pending one-shot
// completion reminder. A notification decided before this call can win the
// race, but the reminder will never fire twice.
func (s *Store) ConsumeRemindOnDone(source, sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := source + ":" + sessionID
	rec := s.states[key]
	if !rec.RemindOnDone {
		return false
	}
	rec.RemindOnDone = false
	rec.UpdatedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	s.states[key] = rec
	_ = platform.WriteJSON(s.Paths.SessionState, stateFile{Version: 1, Sessions: s.states})
	return true
}

// AdvanceSeen moves the watermark to at least eventID. It is called when a
// client opens a session or replies, so the next event becomes unread again.
func (s *Store) AdvanceSeen(source, sessionID string, eventID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := source + ":" + sessionID
	rec := s.states[key]
	if eventID > rec.SeenEventID {
		rec.SeenEventID = eventID
	}
	rec.SeenAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	rec.Read = rec.SeenEventID >= s.maxEventIDLocked(source, sessionID)
	rec.UpdatedAt = rec.SeenAt
	s.states[key] = rec
	return platform.WriteJSON(s.Paths.SessionState, stateFile{Version: 1, Sessions: s.states})
}

// MarkAllSeen advances every session that has events to its current tail.
func (s *Store) MarkAllSeen() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	tails := map[string]int64{}
	for _, event := range s.events {
		if event.SessionID == "" {
			continue
		}
		key := event.Source + ":" + event.SessionID
		if event.ID > tails[key] {
			tails[key] = event.ID
		}
	}
	for key, tail := range tails {
		rec := s.states[key]
		rec.SeenEventID = tail
		rec.SeenAt = now
		rec.Read = true
		rec.UpdatedAt = now
		s.states[key] = rec
	}
	return platform.WriteJSON(s.Paths.SessionState, stateFile{Version: 1, Sessions: s.states})
}

func (s *Store) advanceSeenLocked(source, sessionID string, rec *stateRecord) {
	maxID := s.maxEventIDLocked(source, sessionID)
	if maxID > rec.SeenEventID {
		rec.SeenEventID = maxID
	}
	rec.SeenAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	rec.Read = rec.SeenEventID >= maxID
}

func (s *Store) maxEventIDLocked(source, sessionID string) int64 {
	var maxID int64
	for _, event := range s.events {
		if event.Source == source && event.SessionID == sessionID && event.ID > maxID {
			maxID = event.ID
		}
	}
	return maxID
}

// ---- devices -------------------------------------------------------------

// Devices returns the paired phones.
func (s *Store) Devices() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Device, len(s.devices.Devices))
	copy(out, s.devices.Devices)
	return out
}

// Offers returns the pairing offers (expired ones included; the API decides).
func (s *Store) Offers() []Offer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Offer, len(s.devices.Offers))
	copy(out, s.devices.Offers)
	return out
}

// SaveDevices rewrites devices.json with the given devices and offers.
func (s *Store) SaveDevices(devices []Device, offers []Offer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if devices == nil {
		devices = []Device{}
	}
	if offers == nil {
		offers = []Offer{}
	}
	s.devices = devicesFile{Version: 1, Devices: devices, Offers: offers}
	return platform.WriteJSON(s.Paths.Devices, s.devices)
}

// ---- helpers -------------------------------------------------------------

// randomHex makes the 32-character local token that config.json holds.
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func readJSONL[T any](file string) []T {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item T
		// A damaged trailing line must not stop the relay from starting.
		if json.Unmarshal([]byte(line), &item) == nil {
			out = append(out, item)
		}
	}
	return out
}

func appendJSONL(file string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return err
	}
	return f.Sync()
}
