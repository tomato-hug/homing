package relayapi

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	filepaths "github.com/tomato-hug/homing/internal/files"
	"github.com/tomato-hug/homing/internal/store"
)

var holdTimeout = configuredHoldTimeout()

func configuredHoldTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("HOLD_TIMEOUT_MS"))
	if raw == "" {
		return 10 * time.Minute
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 10 * time.Minute
	}
	return time.Duration(value) * time.Millisecond
}

type stopHold struct {
	sessionID string
	ch        chan *string
}

type approvalHold struct {
	ch chan map[string]any
}

var waits = struct {
	mu        sync.Mutex
	bySession map[string][]*stopHold
	approvals map[int64]*approvalHold
}{
	bySession: map[string][]*stopHold{},
	approvals: map[int64]*approvalHold{},
}

func init() {
	api.SetReplyInterceptor(func(
		_ *http.Request,
		reply store.Reply,
		_ *store.Event,
		deliveryText string,
	) (string, string, bool) {
		hold := takeStopHold(reply.SessionID)
		if hold == nil {
			return "", "", false
		}
		text := deliveryText
		hold.ch <- &text
		return "held", "已交给挂起中的钩子，Agent 继续执行", true
	})
}

// resumeFromPhone implements away mode for a Stop hook. Outside away mode the
// hook returns immediately; inside away mode a queued phone reply continues the
// turn, otherwise the HTTP request waits for the phone.
func resumeFromPhone(ctx context.Context, d *api.Deps, event store.Event) map[string]any {
	s, err := d.Store()
	if err != nil || !s.Away() {
		return map[string]any{}
	}
	if queued := queuedReplies(s, event.SessionID); len(queued) > 0 {
		texts := make([]string, 0, len(queued))
		for _, reply := range queued {
			updated, ok := s.UpdateReplyStatus(reply.ID, "held", "已交给挂起中的钩子，Agent 继续执行")
			if ok {
				api.PublishReplyStatus(updated)
			}
			texts = append(texts, filepaths.DeliveryText(reply.Text, reply.Files))
		}
		return blockDecision(texts)
	}

	hold := &stopHold{sessionID: event.SessionID, ch: make(chan *string, 1)}
	waits.mu.Lock()
	waits.bySession[event.SessionID] = append(waits.bySession[event.SessionID], hold)
	waits.mu.Unlock()
	defer removeStopHold(hold)

	timer := time.NewTimer(holdTimeout)
	defer timer.Stop()
	select {
	case text := <-hold.ch:
		if text == nil {
			return map[string]any{}
		}
		return blockDecision([]string{*text})
	case <-ctx.Done():
		return map[string]any{}
	case <-timer.C:
		return map[string]any{}
	}
}

func takeStopHold(sessionID string) *stopHold {
	waits.mu.Lock()
	defer waits.mu.Unlock()
	list := waits.bySession[sessionID]
	if len(list) == 0 {
		return nil
	}
	hold := list[0]
	if len(list) == 1 {
		delete(waits.bySession, sessionID)
	} else {
		waits.bySession[sessionID] = list[1:]
	}
	return hold
}

func removeStopHold(target *stopHold) {
	waits.mu.Lock()
	defer waits.mu.Unlock()
	list := waits.bySession[target.sessionID]
	for i, hold := range list {
		if hold == target {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(waits.bySession, target.sessionID)
	} else {
		waits.bySession[target.sessionID] = list
	}
}

func releaseStopHolds() int {
	waits.mu.Lock()
	defer waits.mu.Unlock()
	count := 0
	for sessionID, list := range waits.bySession {
		for _, hold := range list {
			select {
			case hold.ch <- nil:
				count++
			default:
			}
		}
		delete(waits.bySession, sessionID)
	}
	return count
}

func queuedReplies(s *store.Store, sessionID string) []store.Reply {
	var out []store.Reply
	for _, reply := range s.Replies() {
		if reply.SessionID == sessionID && reply.Status == "queued" {
			out = append(out, reply)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func blockDecision(texts []string) map[string]any {
	cleaned := make([]string, 0, len(texts))
	for _, text := range texts {
		if value := strings.TrimSpace(text); value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return map[string]any{
		"decision": "block",
		"reason":   "用户从手机回复：" + strings.Join(cleaned, "\n"),
	}
}

func holdApproval(ctx context.Context, event store.Event) map[string]any {
	hold := &approvalHold{ch: make(chan map[string]any, 1)}
	waits.mu.Lock()
	waits.approvals[event.ID] = hold
	waits.mu.Unlock()
	defer func() {
		waits.mu.Lock()
		if waits.approvals[event.ID] == hold {
			delete(waits.approvals, event.ID)
		}
		waits.mu.Unlock()
	}()

	timer := time.NewTimer(holdTimeout)
	defer timer.Stop()
	select {
	case decision := <-hold.ch:
		return decision
	case <-ctx.Done():
		return map[string]any{}
	case <-timer.C:
		return map[string]any{}
	}
}

func resolveApproval(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	eventID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || eventID <= 0 {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "event_id 非法"})
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	decision := strings.ToLower(strings.TrimSpace(stringField(body, "decision")))
	if decision != "allow" && decision != "deny" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "decision 只能是 allow 或 deny"})
		return
	}

	waits.mu.Lock()
	hold := waits.approvals[eventID]
	if hold != nil {
		delete(waits.approvals, eventID)
	}
	waits.mu.Unlock()
	if hold == nil {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{
			"ok": false, "error": "not_pending", "message": "该授权请求没有挂起或已处理",
		})
		return
	}
	reason := "手机批准"
	if decision == "deny" {
		reason = "手机拒绝"
	}
	hold.ch <- map[string]any{"decision": decision, "reason": reason}
	if s, err := d.Store(); err == nil {
		s.MarkRead([]int64{eventID})
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "event_id": eventID, "decision": decision,
	})
}
