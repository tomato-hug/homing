package agent

import (
	"context"
	"errors"
)

// ErrNotQueued means the reply is no longer waiting in an agent-side queue.
// It may already be running, have completed, or never have been queued.
var ErrNotQueued = errors.New("agent: reply is not queued")

// QueuedReplyCanceller is implemented by agents that keep accepted replies in
// an in-memory queue. The reply id comes from Homing's replies.jsonl log so a
// cancellation targets exactly one queued message, not every message with the
// same text.
type QueuedReplyCanceller interface {
	CancelQueuedReply(ctx context.Context, sessionID string, replyID int64) error
}
