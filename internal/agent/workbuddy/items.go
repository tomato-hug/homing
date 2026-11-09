package workbuddy

import (
	"context"
	"os"
	"path/filepath"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/claude"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
)

// ReadItems implements agent.ItemReader. WorkBuddy writes transcripts in the same
// record shape as Claude Code, so the parser is shared rather than copied — the
// Node relay reads both through one branch for the same reason.
func (Agent) ReadItems(_ context.Context, sessionID string, limit int) ([]agent.Item, error) {
	file := transcriptFile(sessionID)
	if file == "" {
		return nil, nil
	}
	return claude.ExtractFileItems(file, id, limit), nil
}

// transcriptFile finds the JSONL behind a session: the hook's transcript path when
// one was recorded, else ~/.workbuddy/projects/<slug>/<id>.jsonl.
func transcriptFile(sessionID string) string {
	if p, ok := scanlib.TranscriptPath(sessionID); ok && p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	root := filepath.Join(home(), "projects")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, dir := range entries {
		if !dir.IsDir() {
			continue
		}
		candidate := filepath.Join(root, dir.Name(), sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
