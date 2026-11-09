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
	ID        int64    `json:"id"`
	EventID   *int64   `json:"event_id"`
	SessionID string   `json:"session_id"`
	Source    string   `json:"source,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	Model     string   `json:"model,omitempty"`
	Effort    string   `json:"effort,omitempty"`
	Text      string   `json:"text"`
	Files     []string `json:"files"`
	Ts        string   `json:"ts"`
	Injected  bool     `json:"injected"`
	Status    string   `json:"status,omitempty"`
	Message   string   `json:"message,omitempty"`
}

// State is the governance triple plus archived, exactly as session-state.json stores it.
type State struct {
	Read     bool `json:"read"`
	Pinned   bool `json:"pinned"`
	Muted    bool `json:"muted"`
	Archived bool `json:"archived"`
}

type stateRecord struct {
	Read      bool   `json:"read"`
	Pinned    bool   `json:"pinned"`
	Muted     bool   `json:"muted"`
	Archived  bool   `json:"archived"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type stateFile struct {
	Version  int                    `json:"version"`
	Sessions map[string]stateRecord `json:"sessions"`
}

// Config is config.json: the local token plus feature switches.
type Config struct {
	Token string `json:"token"`
	Away  struct {
		Enabled bool `json:"enabled"`
	} `json:"away"`
	Proxy struct {
		Enabled bool  `json:"enabled"`
		Ports   []int `json:"ports"`
	} `json:"proxy"`
}

// Device is one paired phone.
type Device struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	AppVersion  string `json:"app_version"`
	CreatedAt   string `json:"created_at"`
	LastSeenMs  int64  `json:"last_seen_ms"`
	PairedVia   string `json:"paired_via"`
	LastIP      string `json:"last_ip"`
	TokenSHA256 string `json:"token_sha256"`
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

	mu      sync.RWMutex
	events  []Event
	replies []Reply
	states  map[string]stateRecord
	config  Config
	devices devicesFile
}

// Open loads everything from disk and creates config.json (with a fresh token)
// when it does not exist yet.
func Open(p platform.Paths) (*Store, error) {
	s := &Store{Paths: p, states: map[string]stateRecord{}}

	if err := s.loadConfig(); err != nil {
		return nil, err
	}
	s.events = readJSONL[Event](p.Events)
	// replies.jsonl gets a new line every time a reply's status changes, so the
	// last line for an id wins (same as the Node relay's in-memory list).
	byID := map[int64]int{}
	for _, reply := range readJSONL[Reply](p.Replies) {
		if reply.Status == "" {
			reply.Status = "queued"
		}
		if at, seen := byID[reply.ID]; seen {
			s.replies[at] = reply
			continue
		}
		byID[reply.ID] = len(s.replies)
		s.replies = append(s.replies, reply)
	}

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
	// 只在内存里留最近 5000 条，文件不受影响（手机靠 since 拉增量）
	if len(s.events) > 5000 {
		s.events = s.events[len(s.events)-5000:]
	}
	return e, nil
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
	return r, nil
}

// SetReplyStatus updates the status/message of an existing reply.
func (s *Store) SetReplyStatus(id int64, status, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.replies {
		if s.replies[i].ID == id {
			s.replies[i].Status = status
			s.replies[i].Message = message
			s.replies[i].Injected = status == "injected" || status == "held"
			return
		}
	}
}

// ---- session state -------------------------------------------------------

// SessionState returns the governance flags for one session.
func (s *Store) SessionState(source, sessionID string) State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec := s.states[source+":"+sessionID]
	return State{Read: rec.Read, Pinned: rec.Pinned, Muted: rec.Muted, Archived: rec.Archived}
}

// PatchSession updates the given flags and rewrites session-state.json.
func (s *Store) PatchSession(source, sessionID string, patch map[string]bool) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := source + ":" + sessionID
	rec := s.states[key]
	for _, field := range []string{"read", "pinned", "muted", "archived"} {
		v, ok := patch[field]
		if !ok {
			continue
		}
		switch field {
		case "read":
			rec.Read = v
		case "pinned":
			rec.Pinned = v
		case "muted":
			rec.Muted = v
		case "archived":
			rec.Archived = v
		}
	}
	rec.UpdatedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	s.states[key] = rec
	err := platform.WriteJSON(s.Paths.SessionState, stateFile{Version: 1, Sessions: s.states})
	return State{Read: rec.Read, Pinned: rec.Pinned, Muted: rec.Muted, Archived: rec.Archived}, err
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
