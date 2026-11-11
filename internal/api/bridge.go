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

// ReplyTransport is a fallback delivery path for agents that do not implement
// agent.Replier. A custom agent registers one through its ingest token; an
// adapter can install a transport that POSTs to that callback. Returning
// handled=false lets the next transport (or the normal agent path) try.
type ReplyTransport func(ctx context.Context, source, sessionID, text string) (status, message string, handled bool)

var (
	replyInterceptorMu sync.RWMutex
	replyInterceptor   ReplyInterceptor
	replyTransportMu   sync.RWMutex
	replyTransports    []ReplyTransport
	awayEndMu          sync.RWMutex
	awayEndHooks       []func()
)

// SetReplyInterceptor installs the process-wide reply claim hook.
func SetReplyInterceptor(fn ReplyInterceptor) {
	replyInterceptorMu.Lock()
	replyInterceptor = fn
	replyInterceptorMu.Unlock()
}

// RegisterReplyTransport adds a fallback reply path. It is intentionally
// additive: multiple route packages can offer independent transports without
// replacing the away-mode interceptor.
func RegisterReplyTransport(fn ReplyTransport) {
	if fn == nil {
		return
	}
	replyTransportMu.Lock()
	replyTransports = append(replyTransports, fn)
	replyTransportMu.Unlock()
}

// RegisterAwayEndHook adds cleanup that must run whenever away mode ends,
// including automatic lock/idle transitions. It is intentionally additive so
// route packages can release their own waiting hooks without importing each
// other.
func RegisterAwayEndHook(fn func()) {
	if fn == nil {
		return
	}
	awayEndMu.Lock()
	awayEndHooks = append(awayEndHooks, fn)
	awayEndMu.Unlock()
}

// RunAwayEndHooks releases resources that only make sense while away.
func RunAwayEndHooks() {
	awayEndMu.RLock()
	hooks := append([]func(){}, awayEndHooks...)
	awayEndMu.RUnlock()
	for _, fn := range hooks {
		fn()
	}
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

// InterceptReply is the exported form for packages that create replies outside
// the normal /api/reply handler (cross-agent compose, forward, continue).
func InterceptReply(
	r *http.Request,
	reply store.Reply,
	event *store.Event,
	deliveryText string,
) (status, message string, handled bool) {
	return interceptReply(r, reply, event, deliveryText)
}

// DeliverReply injects a reply through the agent registry or a fallback
// transport and returns the same status words /api/reply uses.
func (d *Deps) DeliverReply(
	r *http.Request,
	source, sessionID, text, model, permissionMode string,
) (status, message string) {
	return d.deliverReply(r, source, sessionID, text, model, permissionMode)
}

func deliverViaReplyTransport(ctx context.Context, source, sessionID, text string) (string, string, bool) {
	replyTransportMu.RLock()
	transports := append([]ReplyTransport(nil), replyTransports...)
	replyTransportMu.RUnlock()
	for _, fn := range transports {
		if status, message, handled := fn(ctx, source, sessionID, text); handled {
			return status, message, true
		}
	}
	return "", "", false
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

// NamedMessage is one typed SSE event. It is exported so the cloud transport
// can forward the same frames the local stream receives.
type NamedMessage struct {
	Name      string
	SessionID string
	Data      any
}

var namedHub = struct {
	mu   sync.Mutex
	subs map[chan NamedMessage]struct{}
}{subs: map[chan NamedMessage]struct{}{}}

// PublishNamed writes a typed SSE event (reply_status, session, item, turn,
// notify) to every open stream. sessionID may be empty to broadcast.
func PublishNamed(name, sessionID string, data any) {
	data = prepareNamedPayload(name, data)
	namedHub.mu.Lock()
	defer namedHub.mu.Unlock()
	for ch := range namedHub.subs {
		select {
		case ch <- NamedMessage{Name: name, SessionID: sessionID, Data: data}:
		default:
		}
	}
}

func subscribeNamed() (chan NamedMessage, func()) {
	ch := make(chan NamedMessage, 32)
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

// SubscribeNamedEvents exposes the typed SSE fan-out to another transport
// (currently the cloud/LAN event bridge). The returned cancel function must be
// called when the consumer stops.
func SubscribeNamedEvents() (<-chan NamedMessage, func()) {
	ch, cancel := subscribeNamed()
	return ch, cancel
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
