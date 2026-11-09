package api

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/tomato-hug/homing/internal/store"
)

// ReplyInterceptor lets a route package claim a phone reply before the normal
// agent injection path. Hook handlers use it to hand the text to a Stop hook
// that is waiting in away mode. Returning handled=false falls through to the
// agent's Replier.
type ReplyInterceptor func(
	r *http.Request,
	reply store.Reply,
	event *store.Event,
	deliveryText string,
) (status, message string, handled bool)

var (
	replyInterceptorMu sync.RWMutex
	replyInterceptor   ReplyInterceptor
)

// SetReplyInterceptor installs the process-wide reply claim hook.
func SetReplyInterceptor(fn ReplyInterceptor) {
	replyInterceptorMu.Lock()
	replyInterceptor = fn
	replyInterceptorMu.Unlock()
}

func interceptReply(r *http.Request, reply store.Reply, event *store.Event, deliveryText string) (string, string, bool) {
	replyInterceptorMu.RLock()
	fn := replyInterceptor
	replyInterceptorMu.RUnlock()
	if fn == nil {
		return "", "", false
	}
	return fn(r, reply, event, deliveryText)
}

// Authorized exposes the normal Bearer/cookie check to route packages outside
// internal/api (the /p proxy and device heartbeat).
func (d *Deps) Authorized(r *http.Request) bool { return d.authorized(r) }

// FindSummary is the exported lookup used by hook/new-session code that needs
// the full session card (including the request_id completion event).
func (d *Deps) FindSummary(ctx context.Context, sessionID string) (Summary, bool) {
	return d.findSummary(ctx, sessionID)
}

// SourceFor is the exported session -> agent inference used by hooks.
func (d *Deps) SourceFor(ctx context.Context, sessionID string) string {
	return d.sourceFor(ctx, sessionID)
}

// ---- typed SSE events ----------------------------------------------------

type namedMessage struct {
	name      string
	sessionID string
	data      any
}

var namedHub = struct {
	mu   sync.Mutex
	subs map[chan namedMessage]struct{}
}{subs: map[chan namedMessage]struct{}{}}

// PublishNamed writes a typed SSE event (reply_status, session, item, turn,
// notify) to every open stream. sessionID may be empty to broadcast.
func PublishNamed(name, sessionID string, data any) {
	namedHub.mu.Lock()
	defer namedHub.mu.Unlock()
	for ch := range namedHub.subs {
		select {
		case ch <- namedMessage{name: name, sessionID: sessionID, data: data}:
		default:
		}
	}
}

func subscribeNamed() (chan namedMessage, func()) {
	ch := make(chan namedMessage, 32)
	namedHub.mu.Lock()
	namedHub.subs[ch] = struct{}{}
	namedHub.mu.Unlock()
	return ch, func() {
		namedHub.mu.Lock()
		delete(namedHub.subs, ch)
		namedHub.mu.Unlock()
		close(ch)
	}
}

// PublishReplyStatus tells the phone that a bubble changed state.
func PublishReplyStatus(reply store.Reply) {
	var eventID any
	if reply.EventID != nil {
		eventID = *reply.EventID
	}
	message := reply.Message
	if message == "" {
		message = statusMessage(reply.Status)
	}
	PublishNamed("reply_status", reply.SessionID, map[string]any{
		"reply_id":   reply.ID,
		"event_id":   eventID,
		"session_id": reply.SessionID,
		"status":     reply.Status,
		"message":    message,
	})
}

func stringsFrom(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}
