// Package service wires the pieces of `homing serve` together. It is the one
// place that knows the event pipeline:
//
//	custom agent ──/api/ingest──▶ adapter.EventSink ─┐
//	agent watchers (opencode SSE) ──────────────────┼─▶ store.AppendEvent ─▶ api.Publish (SSE)
//	                                                └─▶ notify.Notifier ─▶ push sink
//
// The wiring hangs off api.Register: building the mux is the moment the server
// knows its data directory, so nobody has to edit cmd/homing beyond the blank
// import. It also registers `homing ls` through internal/cli.
//
// Owner: WorkBuddy.
package service

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/adapter"
	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/cloud"
	"github.com/tomato-hug/homing/internal/notify"
	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/store"
)

// Service is the running event pipeline of one Deps.
type Service struct {
	deps     *api.Deps
	store    *store.Store
	paths    platform.Paths
	notifier *notify.Notifier
	log      *slog.Logger

	mu   sync.RWMutex
	push func(notify.Payload)
}

var (
	mu       sync.Mutex
	services = map[*api.Deps]*Service{}
)

func init() {
	// Order 900: runs after every package that only mounts routes.
	api.Register("service", 900, func(_ *http.ServeMux, d *api.Deps) {
		Start(d)
	})
}

// Start wires the pipeline for d and returns it. Calling it twice for the same
// Deps returns the first instance: building a second mux must not double-deliver.
func Start(d *api.Deps) *Service {
	mu.Lock()
	defer mu.Unlock()
	if existing, ok := services[d]; ok {
		return existing
	}
	st, err := d.Store()
	if err != nil {
		d.Log.Error("事件管道未接通：打不开数据目录", "data", d.DataDir, "err", err)
		return nil
	}
	svc := &Service{
		deps:  d,
		store: st,
		paths: platform.NewPaths(d.DataDir),
		log:   d.Log,
	}
	svc.notifier = notify.New(notify.Options{
		Sink:   svc.deliver,
		Muted:  func(source, sessionID string) bool { return st.SessionState(source, sessionID).Muted },
		Paused: func() bool { return platform.IsPaused(svc.paths) },
		Log:    func(format string, args ...any) { d.Log.Debug("notify: "+format, args...) },
	})
	services[d] = svc
	cloud.AttachPush(d, svc.SetPush)

	adapter.SetEventSink(svc.Ingest)
	go svc.watchAgents(context.Background())
	d.Log.Info("事件管道已接通", "data", d.DataDir)
	return svc
}

// SetPush replaces where notifications go. The default logs them, because the
// local push transport (LAN + APNs) is not part of this package.
func (s *Service) SetPush(fn func(notify.Payload)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.push = fn
}

// Store is the store the whole pipeline shares.
func (s *Service) Store() *store.Store { return s.store }

// Ingest records one event from an agent (custom adapter, watcher, hook) and
// fans it out: inbox (store), live phones (SSE) and notifications.
func (s *Service) Ingest(ev agent.Event) {
	if ev.SessionID == "" || ev.Kind == "" {
		s.log.Debug("事件被丢弃：缺 session_id 或 kind", "source", ev.Source)
		return
	}
	cwd := cwdOf(ev)
	stored, err := s.store.AppendEvent(store.Event{
		Source:      ev.Source,
		Kind:        ev.Kind,
		SessionID:   ev.SessionID,
		Cwd:         cwd,
		Project:     projectOf(cwd),
		Title:       ev.Title,
		Text:        ev.Text,
		Ts:          isoOf(ev.At),
		SessionMeta: s.deps.SessionMetaFor(ev.Source, ev.SessionID, cwd),
	})
	if err != nil {
		s.log.Error("事件写入失败", "source", ev.Source, "session", ev.SessionID, "err", err)
		return
	}
	api.Publish(stored)
	s.notifier.OnEvent(notify.Event{
		ID:        stored.ID,
		Source:    stored.Source,
		SessionID: stored.SessionID,
		Kind:      stored.Kind,
		Title:     stored.Title,
		Text:      stored.Text,
		Cwd:       stored.Cwd,
		Ts:        stored.Ts,
	})
}

// deliver hands a finished notification to the push transport.
func (s *Service) deliver(payload notify.Payload) {
	s.mu.RLock()
	push := s.push
	s.mu.RUnlock()
	if push != nil {
		push(payload)
		return
	}
	s.log.Info("通知",
		"level", payload.Level,
		"source", payload.Source,
		"session", payload.SessionID,
		"title", payload.Title,
		"merged", payload.Merged,
		"count", payload.Count,
	)
}

// Flush pushes out whatever the notifier was still batching (shutdown).
func (s *Service) Flush() { s.notifier.FlushAll() }

// Flush is the package-level form of (*Service).Flush, for the one caller that
// holds a Deps but not a Service: `homing serve` on the way out.
func Flush(d *api.Deps) {
	mu.Lock()
	svc := services[d]
	mu.Unlock()
	if svc != nil {
		svc.Flush()
	}
}

// watchAgents starts every agent that can push events on its own (opencode's
// serve SSE today). Agents without a Watcher are read on demand by the API.
func (s *Service) watchAgents(ctx context.Context) {
	for _, a := range agent.All(ctx) {
		watcher, ok := a.(agent.Watcher)
		if !ok {
			continue
		}
		go func(a agent.Agent, watcher agent.Watcher) {
			for ctx.Err() == nil {
				err := watcher.Watch(ctx, s.Ingest)
				if ctx.Err() != nil {
					return
				}
				// The stream ended (serve went away); try again a bit later.
				s.log.Warn("watcher 退出，5s 后重连", "agent", a.ID(), "err", err)
				if errors.Is(err, agent.ErrUnsupported) {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			}
		}(a, watcher)
	}
}

// cwdOf pulls the working directory out of an agent event's data bag.
func cwdOf(ev agent.Event) string {
	if len(ev.Data) == 0 {
		return ""
	}
	cwd, _ := ev.Data["cwd"].(string)
	return cwd
}

func isoOf(at time.Time) string {
	if at.IsZero() {
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return at.UTC().Format("2006-01-02T15:04:05.000Z")
}

// projectOf is the last path element, or "" when the event carries no cwd.
func projectOf(cwd string) string {
	if cwd == "" {
		return ""
	}
	return scanlib.ProjectFromCwd(cwd)
}
