package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/store"
)

const (
	questionPollInterval     = 2 * time.Second
	questionDiscoverLookback = 5 * time.Minute
	questionItemLookback     = 3 * time.Minute
	questionDedupWindow      = 45 * time.Second
)

type questionClaim struct {
	at         time.Time
	summary    string
	structured bool
}

var (
	questionMu           sync.Mutex
	seenQuestionItems    = map[string]time.Time{}
	seenQuestionSessions = map[string]questionClaim{}
)

// ClaimInteractiveQuestion records that an interactive question for (source, sessionID)
// has already been ingested via a hook (PreToolUse or Notification), preventing the
// background transcript watcher from emitting a duplicate push.
func ClaimInteractiveQuestion(source, sessionID, itemOrToolUseID, summary string, structured bool) {
	now := time.Now().UTC()
	source = strings.TrimSpace(source)
	sessionID = strings.TrimSpace(sessionID)
	if source == "" || sessionID == "" {
		return
	}

	questionMu.Lock()
	defer questionMu.Unlock()
	pruneQuestionDedupLocked(now)

	sessKey := sessionQuestionKey(source, sessionID)
	prev, existed := seenQuestionSessions[sessKey]
	if !existed || structured || !prev.structured || now.Sub(prev.at) > questionDedupWindow {
		seenQuestionSessions[sessKey] = questionClaim{
			at:         now,
			summary:    strings.TrimSpace(summary),
			structured: structured,
		}
	}
	if rawID := strings.TrimSpace(itemOrToolUseID); rawID != "" {
		seenQuestionItems[itemQuestionKey(source, sessionID, rawID)] = now
		prefixed := source + "-tool-" + rawID
		seenQuestionItems[itemQuestionKey(source, sessionID, prefixed)] = now
	}
}

// HasRecentStructuredQuestion reports whether a structured question prompt was
// recently ingested for the session so a generic fallback Notification can be suppressed.
func HasRecentStructuredQuestion(source, sessionID string, within time.Duration) bool {
	questionMu.Lock()
	defer questionMu.Unlock()
	claim, ok := seenQuestionSessions[sessionQuestionKey(strings.TrimSpace(source), strings.TrimSpace(sessionID))]
	if !ok || !claim.structured {
		return false
	}
	return time.Since(claim.at) <= within
}

// FindPendingQuestionForSession inspects recent transcript items of (source, sessionID)
// and returns the latest unresolved tail interactive question if present.
func FindPendingQuestionForSession(ctx context.Context, source, sessionID string) (string, []agent.ItemQuestion, bool) {
	a, ok := agent.Get(ctx, source)
	if !ok {
		return "", nil, false
	}
	reader, ok := a.(agent.ItemReader)
	if !ok {
		return "", nil, false
	}
	items, err := reader.ReadItems(ctx, sessionID, 30)
	if err != nil || len(items) == 0 {
		return "", nil, false
	}
	item, ok := latestPendingQuestion(items)
	if !ok || len(item.Questions) == 0 {
		return "", nil, false
	}
	return item.ID, item.Questions, true
}

func (s *Service) watchPendingQuestions(ctx context.Context) {
	// Warm up with emit=false so pre-existing unanswered transcripts on disk do not
	// fire stale notifications when Homing starts.
	s.scanPendingQuestions(ctx, false, time.Now().UTC())

	ticker := time.NewTicker(questionPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanPendingQuestions(ctx, true, time.Now().UTC())
		}
	}
}

func (s *Service) scanPendingQuestions(ctx context.Context, emit bool, now time.Time) {
	for _, a := range agent.All(ctx) {
		if ctx.Err() != nil {
			return
		}
		reader, ok := a.(agent.ItemReader)
		if !ok {
			continue
		}
		rows, err := a.ListSessions(ctx, 20)
		if err != nil {
			continue
		}
		source := a.ID()
		label := strings.TrimSpace(a.Name())
		if label == "" {
			label = source
		}
		for _, row := range rows {
			if ctx.Err() != nil {
				return
			}
			if row.SessionID == "" {
				continue
			}
			rowTS, ok := parseQuestionTS(row.LastTS)
			if !ok || now.Sub(rowTS) > questionDiscoverLookback {
				continue
			}
			items, err := reader.ReadItems(ctx, row.SessionID, 30)
			if err != nil || len(items) == 0 {
				continue
			}
			qItem, ok := latestPendingQuestion(items)
			if !ok {
				continue
			}
			if emit {
				if qTS, ok := parseQuestionTS(qItem.TS); ok && now.Sub(qTS) > questionItemLookback {
					continue
				}
			}
			summary := agent.FormatQuestionSummary(qItem.Questions, qItem.Text)
			if summary == "" {
				summary = qItem.Text
			}
			if !claimTranscriptQuestion(source, row.SessionID, qItem.ID, summary, len(qItem.Questions) > 0, emit, now) {
				continue
			}

			title, body := agent.PrimaryQuestionPrompt(qItem.Questions, summary)
			if summary != "" {
				body = summary
			}
			title = scanlib.NormalizeHookSourceBranding(source, label+" · "+title)
			body = scanlib.NormalizeHookSourceBranding(source, body)

			var questionsAny any
			if len(qItem.Questions) > 0 {
				questionsAny = qItem.Questions
			}
			_, _ = s.IngestStoreEvent(store.Event{
				Source:    source,
				Kind:      "needs_input",
				SessionID: row.SessionID,
				Cwd:       row.Cwd,
				Title:     title,
				Text:      body,
				Questions: questionsAny,
			})
		}
	}
}

func latestPendingQuestion(items []agent.Item) (agent.Item, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		switch it.Type {
		case "question":
			if it.Tool != nil && it.Tool.Status != "" && it.Tool.Status != "running" {
				return agent.Item{}, false
			}
			return it, true
		case "user":
			return agent.Item{}, false
		case "assistant":
			if strings.TrimSpace(it.Text) != "" {
				return agent.Item{}, false
			}
		}
	}
	return agent.Item{}, false
}

func claimTranscriptQuestion(source, sessionID, itemID, summary string, structured, emit bool, now time.Time) bool {
	if itemID == "" {
		return false
	}
	questionMu.Lock()
	defer questionMu.Unlock()
	pruneQuestionDedupLocked(now)

	iKey := itemQuestionKey(source, sessionID, itemID)
	if _, seen := seenQuestionItems[iKey]; seen {
		return false
	}
	seenQuestionItems[iKey] = now
	if !emit {
		return false
	}

	sessKey := sessionQuestionKey(source, sessionID)
	if claim, ok := seenQuestionSessions[sessKey]; ok && now.Sub(claim.at) < questionDedupWindow {
		if claim.structured || (summary != "" && claim.summary == strings.TrimSpace(summary)) {
			return false
		}
	}
	seenQuestionSessions[sessKey] = questionClaim{
		at:         now,
		summary:    strings.TrimSpace(summary),
		structured: structured,
	}
	return true
}

func pruneQuestionDedupLocked(now time.Time) {
	if len(seenQuestionItems) > 512 {
		for k, ts := range seenQuestionItems {
			if now.Sub(ts) > 2*time.Hour {
				delete(seenQuestionItems, k)
			}
		}
	}
	if len(seenQuestionSessions) > 512 {
		for k, c := range seenQuestionSessions {
			if now.Sub(c.at) > 2*time.Hour {
				delete(seenQuestionSessions, k)
			}
		}
	}
}

func sessionQuestionKey(source, sessionID string) string {
	return source + "\x00" + sessionID
}

func itemQuestionKey(source, sessionID, itemID string) string {
	return source + "\x00" + sessionID + "\x00" + itemID
}

func parseQuestionTS(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts, true
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts, true
	}
	return time.Time{}, false
}
