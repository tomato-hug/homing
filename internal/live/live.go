// Package live publishes incremental transcript items for sessions a client is
// currently watching. The first version polls the agent's ItemReader once a
// second; agents whose files land atomically do not need a platform watcher yet.
package live

import (
	"context"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

const (
	defaultPollInterval = time.Second
	defaultMaxTrackers  = 20
	defaultItemLimit    = 300
)

// Emitter writes one named SSE frame. internal/api installs PublishNamed here,
// which keeps this package independent of the HTTP layer.
type Emitter func(name, sessionID string, data any)

// ReaderFunc reads the tail of a session. It is a field on Manager so tests can
// feed deterministic rows without registering a fake global agent.
type ReaderFunc func(ctx context.Context, source, sessionID string, limit int) ([]agent.Item, error)

// Manager owns the active session tailers.
type Manager struct {
	mu         sync.Mutex
	trackers   map[string]*tracker
	poll       time.Duration
	maxTracked int
	emit       Emitter
	read       ReaderFunc
}

type tracker struct {
	sessionID string
	source    string
	refs      int
	cancel    context.CancelFunc

	lastSeq    int
	baseline   bool
	running    bool
	idleTicks  int
	lastActive time.Time
}

// NewManager builds a manager with the production defaults.
func NewManager() *Manager {
	return &Manager{
		trackers:   map[string]*tracker{},
		poll:       defaultPollInterval,
		maxTracked: defaultMaxTrackers,
		read:       readThroughRegistry,
	}
}

// Default is the process-wide manager used by /api/stream.
var Default = NewManager()

// SetEmitter installs the SSE writer. Call it once during package setup.
func (m *Manager) SetEmitter(emit Emitter) {
	m.mu.Lock()
	m.emit = emit
	m.mu.Unlock()
}

// Track begins tailing a session. Repeated calls from multiple streams share
// one poller and only stop it when the last subscriber leaves.
func (m *Manager) Track(sessionID, source string) {
	if sessionID == "" || source == "" {
		return
	}
	m.mu.Lock()
	if existing := m.trackers[sessionID]; existing != nil {
		existing.refs++
		existing.lastActive = time.Now()
		m.mu.Unlock()
		return
	}
	for len(m.trackers) >= m.maxTracked {
		oldestID := ""
		var oldest time.Time
		for id, candidate := range m.trackers {
			if oldestID == "" || candidate.lastActive.Before(oldest) {
				oldestID = id
				oldest = candidate.lastActive
			}
		}
		if oldestID == "" {
			break
		}
		m.trackers[oldestID].cancel()
		delete(m.trackers, oldestID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	tracked := &tracker{
		sessionID:  sessionID,
		source:     source,
		refs:       1,
		cancel:     cancel,
		lastActive: time.Now(),
	}
	m.trackers[sessionID] = tracked
	m.mu.Unlock()
	go m.run(ctx, tracked)
}

// Untrack releases one stream's interest in a session.
func (m *Manager) Untrack(sessionID string) {
	m.mu.Lock()
	tracked := m.trackers[sessionID]
	if tracked == nil {
		m.mu.Unlock()
		return
	}
	tracked.refs--
	if tracked.refs <= 0 {
		tracked.cancel()
		delete(m.trackers, sessionID)
	}
	m.mu.Unlock()
}

// Tracked reports the active tracker count; used by tests and diagnostics.
func (m *Manager) Tracked() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.trackers)
}

func (m *Manager) run(ctx context.Context, tracked *tracker) {
	m.mu.Lock()
	poll := m.poll
	m.mu.Unlock()
	if poll <= 0 {
		poll = defaultPollInterval
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.pollOnce(ctx, tracked)
		}
	}
}

func (m *Manager) pollOnce(ctx context.Context, tracked *tracker) {
	m.mu.Lock()
	read := m.read
	emit := m.emit
	m.mu.Unlock()
	if read == nil {
		return
	}
	items, err := read(ctx, tracked.source, tracked.sessionID, defaultItemLimit)
	if err != nil {
		return
	}
	maximum := maxSeq(items)
	if !tracked.baseline {
		tracked.baseline = true
		tracked.lastSeq = maximum
		return
	}
	fresh := itemsAfter(items, tracked.lastSeq)
	if len(fresh) == 0 {
		if tracked.running {
			tracked.idleTicks++
			if tracked.idleTicks >= 2 {
				tracked.running = false
				tracked.idleTicks = 0
				if emit != nil {
					emit("turn", tracked.sessionID, map[string]any{
						"session_id": tracked.sessionID,
						"state":      "idle",
						"ts":         isoNow(),
					})
				}
			}
		}
		return
	}
	tracked.lastActive = time.Now()
	tracked.lastSeq = maximum
	tracked.idleTicks = 0
	if emit == nil {
		return
	}
	if !tracked.running {
		tracked.running = true
		emit("turn", tracked.sessionID, map[string]any{
			"session_id": tracked.sessionID,
			"state":      "running",
			"ts":         isoNow(),
		})
	}
	for _, item := range fresh {
		emit("item", tracked.sessionID, map[string]any{
			"session_id": tracked.sessionID,
			"item":       item,
		})
	}
}

func readThroughRegistry(ctx context.Context, source, sessionID string, limit int) ([]agent.Item, error) {
	found, ok := agent.Get(ctx, source)
	if !ok {
		return nil, agent.ErrUnsupported
	}
	reader, ok := found.(agent.ItemReader)
	if !ok {
		return nil, agent.ErrUnsupported
	}
	return reader.ReadItems(ctx, sessionID, limit)
}

func maxSeq(items []agent.Item) int {
	maximum := 0
	for _, item := range items {
		if item.Seq > maximum {
			maximum = item.Seq
		}
	}
	return maximum
}

func itemsAfter(items []agent.Item, sequence int) []agent.Item {
	out := make([]agent.Item, 0, len(items))
	for _, item := range items {
		if item.Seq > sequence {
			out = append(out, item)
		}
	}
	return out
}

func isoNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}
