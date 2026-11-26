package workbuddy

import (
	"context"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/claude"
)

// ReadItems implements agent.ItemReader. WorkBuddy writes transcripts in the same
// record shape as Claude Code, so the parser is shared rather than copied — the
// Node relay reads both through one branch for the same reason.
func (Agent) ReadItems(_ context.Context, sessionID string, limit int) ([]agent.Item, error) {
	file := TranscriptPath(sessionID)
	if file == "" {
		return nil, nil
	}
	return claude.ExtractFileItems(file, id, limit), nil
}
