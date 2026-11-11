package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/driver"
	filepaths "github.com/tomato-hug/homing/internal/files"
	"github.com/tomato-hug/homing/internal/live"
	"github.com/tomato-hug/homing/internal/store"
)

// ---- live event fan-out --------------------------------------------------

// hub fans one appended event out to every open /api/stream connection. It is a
// package-level singleton because a process runs one relay; the hook handlers
// that append events call Publish.
var hub = struct {
	mu   sync.Mutex
	subs map[chan store.Event]struct{}
}{subs: map[chan store.Event]struct{}{}}

// Publish hands a freshly stored event to the open streams.
func Publish(event store.Event) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for ch := range hub.subs {
		select {
		case ch <- event:
		default: // a slow phone must not block the writer
		}
	}
}

func subscribe() (chan store.Event, func()) {
	ch := make(chan store.Event, 32)
	hub.mu.Lock()
	hub.subs[ch] = struct{}{}
	hub.mu.Unlock()
	return ch, func() {
		hub.mu.Lock()
		delete(hub.subs, ch)
		hub.mu.Unlock()
		close(ch)
	}
}

func init() {
	live.Default.SetEmitter(PublishNamed)
	Register("events", 40, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/events", d.guard(d.handleEvents))
		mux.HandleFunc("POST /api/read", d.guard(d.handleRead))
		mux.HandleFunc("GET /api/replies", d.guard(d.handleReplies))
		mux.HandleFunc("POST /api/reply", d.guard(d.handleReply))
		mux.HandleFunc("POST /api/replies/{id}/cancel", d.guard(d.handleCancelReply))
		mux.HandleFunc("GET /api/stream", d.guard(d.handleStream))
	})
}

func (d *Deps) handleEvents(w http.ResponseWriter, r *http.Request) {
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	since, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("since")), 10, 64)
	out := make([]store.Event, 0, 32)
	for _, event := range s.Events() {
		if event.ID <= since {
			continue
		}
		out = append(out, decorateEvent(event))
	}
	WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleRead(w http.ResponseWriter, r *http.Request) {
	body := readBody(r)
	raw, _ := body["ids"].([]any)
	ids := make([]int64, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(float64); ok {
			ids = append(ids, int64(value))
		}
	}
	if s, ok := d.store(); ok {
		s.MarkRead(ids)
		tails := map[string]int64{}
		for _, event := range s.Events() {
			for _, id := range ids {
				if event.ID != id || event.SessionID == "" {
					continue
				}
				key := event.Source + "\x00" + event.SessionID
				if event.ID > tails[key] {
					tails[key] = event.ID
				}
			}
		}
		for key, tail := range tails {
			parts := strings.SplitN(key, "\x00", 2)
			if len(parts) != 2 {
				continue
			}
			_ = s.AdvanceSeen(parts[0], parts[1], tail)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "updated": len(ids)})
}

func (d *Deps) handleReplies(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	type row struct {
		ReplyID   int64  `json:"reply_id"`
		EventID   *int64 `json:"event_id"`
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
		Message   string `json:"message"`
		Ts        string `json:"ts"`
		Text      string `json:"text"`
		Files     []any  `json:"files"`
	}
	out := []row{}
	if s, ok := d.store(); ok {
		for _, reply := range s.Replies() {
			if sessionID != "" && reply.SessionID != sessionID {
				continue
			}
			message := reply.Message
			if message == "" {
				message = statusMessage(reply.Status)
			}
			files := make([]any, 0, len(reply.Files))
			for _, file := range reply.Files {
				files = append(files, file)
			}
			out = append(out, row{
				ReplyID: reply.ID, EventID: reply.EventID, SessionID: reply.SessionID,
				Status: reply.Status, Message: message, Ts: reply.Ts, Text: reply.Text, Files: files,
			})
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"session_id": sessionID, "count": len(out), "replies": out})
}

// statusMessage is the shared wording for a reply status.
func statusMessage(status string) string {
	switch status {
	case "injected":
		return "已注入会话"
	case "held":
		return "已交给挂起中的钩子，Agent 继续执行"
	case "queued":
		return "暂无挂起的钩子，已排队"
	case "failed":
		return "投递失败"
	}
	return ""
}

// handleReply stores what the phone sent and tries to deliver it right away.
func (d *Deps) handleReply(w http.ResponseWriter, r *http.Request) {
	if !d.RequireScope(w, r, "reply") {
		return
	}
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	body := readBody(r)
	text := strings.TrimSpace(stringOr(body["text"], ""))
	if text == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "回复内容不能为空"})
		return
	}

	var event *store.Event
	if raw, present := body["event_id"]; present && raw != nil {
		if id, ok := raw.(float64); ok {
			for _, candidate := range s.Events() {
				if candidate.ID == int64(id) {
					found := candidate
					event = &found
					break
				}
			}
			if event == nil {
				WriteJSON(w, http.StatusNotFound, map[string]any{"error": "事件不存在"})
				return
			}
		}
	}

	sessionID := stringOr(body["session_id"], "")
	if event != nil {
		sessionID = event.SessionID
	}
	if sessionID == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "event_id 或 session_id 必须提供一个"})
		return
	}

	source := ""
	if event != nil {
		source = event.Source
	} else {
		source = stringOr(body["source"], "")
	}
	if source == "" {
		source = d.sourceFor(ctxOf(r), sessionID)
	}
	if source == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "source 只能是已接入的 agent"})
		return
	}

	cwd := ""
	if event != nil {
		cwd = event.Cwd
	}
	if cwd == "" {
		if summary, found := d.findSummary(ctxOf(r), sessionID); found {
			cwd = deref(summary.Cwd)
		}
	}
	model := strings.TrimSpace(stringOr(body["model"], ""))
	if len(model) > 80 {
		model = model[:80]
	}
	files := filepaths.NormalizeFiles(stringsFrom(body["files"]))
	effort := strings.TrimSpace(stringOr(body["effort"], ""))
	if len(effort) > 32 {
		effort = effort[:32]
	}
	permissionMode := strings.ToLower(strings.TrimSpace(stringOr(body["permission_mode"], "")))
	if len(permissionMode) > 32 {
		permissionMode = permissionMode[:32]
	}
	if !PermissionModeAllowed(source, permissionMode) {
		WriteJSON(w, http.StatusBadRequest, map[string]any{
			"error": "permission_mode 不是该 Agent 支持的模式",
			"code":  "permission_mode_unsupported",
		})
		return
	}

	reply, err := s.AppendReply(store.Reply{
		EventID:        eventIDOf(event),
		SessionID:      sessionID,
		Source:         source,
		Cwd:            cwd,
		Text:           text,
		Model:          model,
		Effort:         effort,
		PermissionMode: permissionMode,
		Files:          files,
		Status:         "queued",
	})
	if err != nil {
		d.Log.Error("append reply", "err", err)
	}
	if event != nil {
		s.MarkRead([]int64{event.ID})
		_ = s.AdvanceSeen(source, sessionID, event.ID)
	}

	deliveryText := filepaths.DeliveryText(text, files)

	status, note, handled := interceptReply(r, reply, event, deliveryText)
	if !handled {
		status, note = d.deliverReplyWithID(
			r, source, sessionID, deliveryText, model, permissionMode, reply.ID,
		)
	}
	if note == "" {
		note = statusMessage(status)
	}
	updated, _ := s.UpdateReplyStatus(reply.ID, status, note)
	PublishReplyStatus(updated)
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "reply_id": reply.ID, "status": status, "message": note, "files": files,
	})
}

// handleCancelReply removes a reply that is still waiting in an agent-side
// queue. It never pretends to cancel an in-flight turn; that is /stop's job.
func (d *Deps) handleCancelReply(w http.ResponseWriter, r *http.Request) {
	if !d.RequireScope(w, r, "reply") {
		return
	}
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "reply id 无效"})
		return
	}
	var reply store.Reply
	found := false
	for _, candidate := range s.Replies() {
		if candidate.ID == id {
			reply = candidate
			found = true
			break
		}
	}
	if !found {
		WriteJSON(w, http.StatusNotFound, map[string]any{"error": "回复不存在"})
		return
	}
	if reply.Status != "queued" {
		WriteJSON(w, http.StatusConflict, map[string]any{
			"error":  "只有仍在排队的回复可以撤回",
			"code":   "reply_not_queued",
			"status": reply.Status,
		})
		return
	}
	a, ok := agent.Get(r.Context(), reply.Source)
	if !ok {
		WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "这个 Agent 不在线，无法撤回排队回复",
			"code":  "reply_cancel_unsupported",
		})
		return
	}
	canceller, ok := a.(agent.QueuedReplyCanceller)
	if !ok {
		WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "这个 Agent 暂不支持撤回排队回复",
			"code":  "reply_cancel_unsupported",
		})
		return
	}
	if err := canceller.CancelQueuedReply(r.Context(), reply.SessionID, reply.ID); err != nil {
		status := http.StatusConflict
		if !errors.Is(err, agent.ErrNotQueued) {
			status = http.StatusBadGateway
		}
		WriteJSON(w, status, map[string]any{
			"error": "回复已经不在队列中，可能已开始执行",
			"code":  "reply_not_queued",
		})
		return
	}
	updated, _ := s.UpdateReplyStatus(reply.ID, "cancelled", "已从排队撤回")
	PublishReplyStatus(updated)
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "reply_id": updated.ID, "status": updated.Status, "message": updated.Message,
	})
}

// deliverReply injects the message and maps the agent's answer to a status word.
func (d *Deps) deliverReply(
	r *http.Request,
	source, sessionID, text, model, permissionMode string,
) (string, string) {
	return d.deliverReplyWithID(r, source, sessionID, text, model, permissionMode, 0)
}

func (d *Deps) deliverReplyWithID(
	r *http.Request,
	source, sessionID, text, model, permissionMode string,
	replyID int64,
) (string, string) {
	a, ok := agent.Get(ctxOf(r), source)
	if ok {
		if replier, supported := a.(agent.Replier); supported {
			err := replier.Reply(ctxOf(r), sessionID, text, agent.ReplyOpts{
				Model:          model,
				PermissionMode: permissionMode,
				ReplyID:        replyID,
			})
			switch {
			case err == nil:
				return "injected", "已注入 " + a.Name() + " 会话"
			case errors.Is(err, agent.ErrQueued):
				return "queued", trimPrefix(err.Error(), "agent: queued: ")
			case errors.Is(err, agent.ErrHeld):
				return "held", trimPrefix(err.Error(), "agent: held: ")
			case errors.Is(err, agent.ErrUnsupported):
				// Custom manifests without a reply command fall through to the
				// verified ACP driver below.
			default:
				return "failed", err.Error()
			}
		}
	}
	if spec, supported := driver.SpecFor(source); supported && spec.Dispatch {
		cwd := ""
		if summary, found := d.findSummary(ctxOf(r), sessionID); found {
			cwd = deref(summary.Cwd)
		}
		if driver.Default().Has(sessionID) {
			cwd = driver.Default().SessionCwd(sessionID)
		}
		if strings.TrimSpace(cwd) == "" {
			return "failed", "找不到这个 ACP 会话的工作目录，无法继续"
		}
		err := driver.Default().Resume(ctxOf(r), source, sessionID, cwd, text, nil)
		switch {
		case err == nil:
			return "injected", "已恢复 ACP 会话并发送"
		case errors.Is(err, driver.ErrACPSessionBusy):
			return "queued", "ACP 会话正在执行上一回合"
		default:
			return "failed", err.Error()
		}
	}
	if status, message, handled := deliverViaReplyTransport(ctxOf(r), source, sessionID, text); handled {
		return status, message
	}
	return "queued", statusMessage("queued")
}

func eventIDOf(event *store.Event) *int64 {
	if event == nil {
		return nil
	}
	id := event.ID
	return &id
}

// handleStream is the phone's SSE feed: unread events first, then live ones.
//
// Every durable message and live item frame carries an id that encodes both
// positions. EventSource and the Android transport send that value back as
// Last-Event-ID after a reconnect, so the server can fill the gap before
// resuming the live fan-out.
func (d *Deps) handleStream(w http.ResponseWriter, r *http.Request) {
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sessionFilter := strings.TrimSpace(r.URL.Query().Get("session_id"))
	includeReasoning := queryBool(r, "include_reasoning")
	cursor := parseStreamCursor(r.Header.Get("Last-Event-ID"))
	if cursor.EventID == 0 && cursor.ItemSeq == 0 {
		cursor = parseStreamCursor(r.URL.Query().Get("after_seq"))
	}
	source := ""
	if sessionFilter != "" {
		source = d.sourceFor(ctxOf(r), sessionFilter)
		if source != "" {
			live.Default.Track(sessionFilter, source)
			defer live.Default.Untrack(sessionFilter)
		}
	}

	ch, unsubscribe := subscribe()
	defer unsubscribe()
	namedCh, unsubscribeNamed := subscribeNamed()
	defer unsubscribeNamed()

	writeEvent := func(event store.Event) bool {
		if sessionFilter != "" && event.SessionID != sessionFilter {
			return true
		}
		if event.ID > 0 && event.ID <= cursor.EventID {
			return true
		}
		event = decorateEvent(event)
		payload, err := json.Marshal(event)
		if err != nil {
			return true
		}
		if event.ID > 0 {
			cursor.EventID = event.ID
		}
		if _, err := fmt.Fprintf(w, "id: %s\nevent: message\ndata: %s\n\n",
			cursor.eventIDString(), payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	writeItem := func(sessionID string, item agent.Item) bool {
		if sessionFilter != "" && item.Seq > 0 {
			if item.Seq <= cursor.ItemSeq {
				return true
			}
			cursor.ItemSeq = item.Seq
		}
		if !includeReasoning && itemIsReasoning(item) {
			return true
		}
		item, _ = truncateItemFrame(sessionID, item)
		payload, err := json.Marshal(map[string]any{
			"session_id": sessionID,
			"item":       item,
		})
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "id: %s\nevent: item\ndata: %s\n\n",
			cursor.eventIDString(), payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	writeNamed := func(message NamedMessage) bool {
		if sessionFilter != "" && message.SessionID != "" && message.SessionID != sessionFilter {
			return true
		}
		if message.Name == "item" {
			if payload, ok := message.Data.(map[string]any); ok {
				if item, ok := payload["item"].(agent.Item); ok {
					return writeItem(message.SessionID, item)
				}
			}
		}
		payload, err := json.Marshal(message.Data)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", message.Name, payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// Subscribe above happens before this snapshot. Events emitted while the
	// snapshot is being sent are buffered and deduplicated by writeEvent.
	for _, event := range s.Events() {
		if cursor.EventID == 0 && event.Read {
			continue
		}
		if !writeEvent(event) {
			return
		}
	}
	if sessionFilter != "" && cursor.ItemSeq > 0 {
		for _, item := range streamBackfillItems(
			ctxOf(r), source, sessionFilter, cursor.ItemSeq,
		) {
			if !writeItem(sessionFilter, item) {
				return
			}
		}
	}

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-ch:
			if !open {
				return
			}
			if !writeEvent(event) {
				return
			}
		case message, open := <-namedCh:
			if !open {
				return
			}
			if !writeNamed(message) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ":ping\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// SubscribeEvents exposes the durable event fan-out to another transport such
// as the cloud connector. The cancel function releases the subscription.
func SubscribeEvents() (<-chan store.Event, func()) {
	return subscribe()
}

// streamBackfillItems returns the same item ids and sequence numbers as
// GET /api/sessions/{id}/items. Driver-produced items are merged in so an ACP
// session can resume even when its native transcript has not landed yet.
func streamBackfillItems(
	ctx context.Context,
	source, sessionID string,
	afterSeq int,
) []agent.Item {
	items := []agent.Item{}
	if found, ok := agent.Get(ctx, source); ok {
		if reader, ok := found.(agent.ItemReader); ok {
			if native, err := reader.ReadItems(ctx, sessionID, 2000); err == nil {
				items = native
			}
		}
	}
	items = mergeDriverItems(items, driver.Default().Items(sessionID, 2000))
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Seq == items[j].Seq {
			return items[i].ID < items[j].ID
		}
		return items[i].Seq < items[j].Seq
	})
	out := make([]agent.Item, 0, len(items))
	seenIDs := map[string]bool{}
	for _, item := range items {
		if item.Seq <= afterSeq || item.ID != "" && seenIDs[item.ID] {
			continue
		}
		if item.ID != "" {
			seenIDs[item.ID] = true
		}
		out = append(out, item)
	}
	return out
}

// localhostPorts finds the ports of localhost links in an event text, so the
// phone can offer to open them on this Mac.
func localhostPorts(text string) []int {
	matches := localhostPattern.FindAllStringSubmatch(text, -1)
	seen := map[int]bool{}
	ports := []int{}
	for _, match := range matches {
		port, err := strconv.Atoi(match[1])
		if err != nil || port <= 0 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

var localhostPattern = regexp.MustCompile(`(?i)(?:https?://)?(?:localhost|127\.0\.0\.1|\[::1\]):(\d{2,5})`)

// decorateEvent fills the fields the API computes on the way out. Anything the
// event was stored with is passed through untouched.
func decorateEvent(event store.Event) store.Event {
	event.LocalhostLinks = localhostPorts(event.Text)
	return event
}

func stringOr(value any, fallback string) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}

func trimPrefix(value, prefix string) string {
	if strings.HasPrefix(value, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	return value
}
