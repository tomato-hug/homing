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
	sessionID string
	source    string
	ch        chan map[string]any
}

var waits = struct {
	mu               sync.Mutex
	bySession        map[string][]*stopHold
	approvals        map[int64]*approvalHold
	settledApprovals map[int64]bool
	settledOrder     []int64
}{
	bySession:        map[string][]*stopHold{},
	approvals:        map[int64]*approvalHold{},
	settledApprovals: map[int64]bool{},
}

func init() {
	api.RegisterAwayEndHook(func() { releaseStopHolds() })
	api.RegisterApprovalSettledChecker(IsApprovalSettled)
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

func drainQueuedReplies(s *store.Store, sessionID string) ([]string, bool) {
	queued := queuedReplies(s, sessionID)
	if len(queued) == 0 {
		return nil, false
	}
	texts := make([]string, 0, len(queued))
	for _, reply := range queued {
		updated, ok := s.UpdateReplyStatus(reply.ID, "held", "已交给挂起中的钩子，Agent 继续执行")
		if ok {
			api.PublishReplyStatus(updated)
		}
		texts = append(texts, filepaths.DeliveryText(reply.Text, reply.Files))
	}
	return texts, true
}

// resumeFromPhone implements away mode for a Stop hook. Outside away mode the
// hook returns immediately; inside away mode a queued phone reply continues the
// turn, otherwise the HTTP request waits for the phone.
func resumeFromPhone(ctx context.Context, d *api.Deps, event store.Event) map[string]any {
	s, err := d.Store()
	if err != nil || !s.Away() {
		return map[string]any{}
	}
	if texts, ok := drainQueuedReplies(s, event.SessionID); ok {
		return blockDecision(texts)
	}

	hold := &stopHold{sessionID: event.SessionID, ch: make(chan *string, 1)}
	waits.mu.Lock()
	waits.bySession[event.SessionID] = append(waits.bySession[event.SessionID], hold)
	waits.mu.Unlock()
	defer removeStopHold(hold)

	if len(queuedReplies(s, event.SessionID)) > 0 && removeStopHoldIfPresent(hold) {
		if texts, ok := drainQueuedReplies(s, event.SessionID); ok {
			return blockDecision(texts)
		}
	}

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
	_ = removeStopHoldIfPresent(target)
}

func removeStopHoldIfPresent(target *stopHold) bool {
	waits.mu.Lock()
	defer waits.mu.Unlock()
	list := waits.bySession[target.sessionID]
	removed := false
	for i, hold := range list {
		if hold == target {
			list = append(list[:i], list[i+1:]...)
			removed = true
			break
		}
	}
	if !removed {
		return false
	}
	if len(list) == 0 {
		delete(waits.bySession, target.sessionID)
	} else {
		waits.bySession[target.sessionID] = list
	}
	return true
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

const maxSettledApprovals = 2048

// HasPendingApprovalForSession reports whether an interactive approval hold
// is currently active for sessionID.
func HasPendingApprovalForSession(sessionID string) bool {
	if strings.TrimSpace(sessionID) == "" {
		return false
	}
	waits.mu.Lock()
	defer waits.mu.Unlock()
	for _, hold := range waits.approvals {
		if hold != nil && hold.sessionID == sessionID {
			return true
		}
	}
	return false
}

// IsApprovalSettled reports whether eventID's interactive approval hold has
// already finished, timed out, or failed as not_pending.
func IsApprovalSettled(eventID int64) bool {
	if eventID <= 0 {
		return false
	}
	waits.mu.Lock()
	defer waits.mu.Unlock()
	return waits.settledApprovals[eventID]
}

func markApprovalSettledLocked(eventID int64) {
	if eventID <= 0 {
		return
	}
	if !waits.settledApprovals[eventID] {
		waits.settledApprovals[eventID] = true
		waits.settledOrder = append(waits.settledOrder, eventID)
		for len(waits.settledOrder) > maxSettledApprovals {
			oldest := waits.settledOrder[0]
			waits.settledOrder = waits.settledOrder[1:]
			delete(waits.settledApprovals, oldest)
		}
	}
}

func markEventSettledInStore(d *api.Deps, event store.Event) {
	if d == nil || event.ID <= 0 {
		return
	}
	s, err := d.Store()
	if err != nil {
		return
	}
	s.MarkRead([]int64{event.ID})
	if event.Source != "" && event.SessionID != "" {
		_ = s.AdvanceSeen(event.Source, event.SessionID, event.ID)
	}
}

func holdApproval(ctx context.Context, event store.Event) map[string]any {
	return holdApprovalWithDeps(ctx, nil, event)
}

func holdApprovalWithDeps(ctx context.Context, d *api.Deps, event store.Event) map[string]any {
	hold := &approvalHold{
		sessionID: event.SessionID,
		source:    event.Source,
		ch:        make(chan map[string]any, 1),
	}
	waits.mu.Lock()
	waits.approvals[event.ID] = hold
	waits.mu.Unlock()
	defer func() {
		waits.mu.Lock()
		if waits.approvals[event.ID] == hold {
			delete(waits.approvals, event.ID)
		}
		markApprovalSettledLocked(event.ID)
		waits.mu.Unlock()
		markEventSettledInStore(d, event)
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

func findFallbackApprovalLocked(s *store.Store, eventID int64) (int64, *approvalHold, store.Event) {
	if s == nil {
		return 0, nil, store.Event{}
	}
	var target store.Event
	for _, ev := range s.Events() {
		if ev.ID == eventID {
			target = ev
			break
		}
	}
	if target.SessionID == "" {
		return 0, nil, target
	}
	var matchedID int64
	var matchedHold *approvalHold
	for id, candidate := range waits.approvals {
		if candidate == nil || candidate.sessionID != target.SessionID {
			continue
		}
		if target.Source != "" && candidate.source != "" && candidate.source != target.Source {
			continue
		}
		if matchedHold != nil {
			// Ambiguous multiple holds for the same session; do not guess.
			return 0, nil, target
		}
		matchedID = id
		matchedHold = candidate
	}
	return matchedID, matchedHold, target
}

func resolveApproval(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !d.RequireScope(w, r, "reply") {
		return
	}
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
	d.Audit(r, "approval", map[string]any{
		"event_id": eventID,
		"decision": decision,
	})

	var s *store.Store
	if st, storeErr := d.Store(); storeErr == nil {
		s = st
	}

	waits.mu.Lock()
	resolvedID := eventID
	hold := waits.approvals[eventID]
	var targetEvent store.Event
	if hold != nil {
		delete(waits.approvals, eventID)
		markApprovalSettledLocked(eventID)
	} else {
		fallbackID, fallbackHold, ev := findFallbackApprovalLocked(s, eventID)
		targetEvent = ev
		if fallbackHold != nil {
			resolvedID = fallbackID
			hold = fallbackHold
			delete(waits.approvals, fallbackID)
			markApprovalSettledLocked(eventID)
			markApprovalSettledLocked(fallbackID)
		} else {
			markApprovalSettledLocked(eventID)
		}
	}
	waits.mu.Unlock()
	if hold == nil {
		if targetEvent.ID == 0 && s != nil {
			for _, ev := range s.Events() {
				if ev.ID == eventID {
					targetEvent = ev
					break
				}
			}
		}
		markEventSettledInStore(d, store.Event{
			ID:        eventID,
			Source:    targetEvent.Source,
			SessionID: targetEvent.SessionID,
		})
		api.WriteJSON(w, http.StatusNotFound, map[string]any{
			"ok": false, "error": "not_pending", "message": "该授权请求已在电脑端处理或已过期",
		})
		return
	}
	reason := "手机批准"
	if decision == "deny" {
		reason = "手机拒绝"
	}
	answer := map[string]any{"decision": decision, "reason": reason}
	if optionID := strings.TrimSpace(stringField(body, "option_id")); optionID != "" {
		answer["option_id"] = optionID
	}
	hold.ch <- answer
	if s != nil {
		ids := []int64{eventID}
		if resolvedID != eventID {
			ids = append(ids, resolvedID)
		}
		s.MarkRead(ids)
		for _, event := range s.Events() {
			if event.ID == resolvedID {
				_ = s.AdvanceSeen(event.Source, event.SessionID, event.ID)
				break
			}
		}
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "event_id": resolvedID, "decision": decision,
		"option_id": strings.TrimSpace(stringField(body, "option_id")),
	})
}
