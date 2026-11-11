package service

import (
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/notify"
	"github.com/tomato-hug/homing/internal/store"
)

// IngestStoreEvent feeds an already-shaped event through the same pipeline as
// Service.Ingest: the event is persisted, published on SSE, graded by the
// notifier and handed to the configured push transport. Hook handlers use this
// entry point because they can attach files and session metadata before ingest.
func (s *Service) IngestStoreEvent(ev store.Event) (store.Event, error) {
	if ev.Ts == "" {
		ev.Ts = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if ev.Project == "" {
		ev.Project = projectOf(ev.Cwd)
	}
	if ev.SessionMeta == nil {
		ev.SessionMeta = s.deps.SessionMetaFor(ev.Source, ev.SessionID, ev.Cwd)
	}
	stored, err := s.store.AppendEvent(ev)
	if err != nil {
		return stored, err
	}
	api.Publish(stored)
	for _, reply := range s.store.ConfirmQueuedRepliesForAgentEvent(stored) {
		api.PublishReplyStatus(reply)
	}
	s.notifier.OnEvent(notify.Event{
		ID:         stored.ID,
		Source:     stored.Source,
		SessionID:  stored.SessionID,
		Kind:       stored.Kind,
		DispatchID: sessionMetaString(stored.SessionMeta, "dispatch_id"),
		Title:      stored.Title,
		Text:       stored.Text,
		Cwd:        stored.Cwd,
		Ts:         stored.Ts,
	})
	return stored, nil
}
