package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tomato-hug/homing/internal/store"
)

// The flow recorder turns what already passes through homing (hook events,
// replies, dispatches, meeting-room turns, pushes) into "hops" between nodes:
// the phone, homing itself, each agent and each meeting room. The console's
// 实时流 page draws them as packets moving along edges, the way Magpie's
// routing view shows every request live.
//
// Nothing here stores message bodies: a hop carries at most a short preview,
// and the ring buffer lives only in memory.

const (
	flowRingSize     = 400
	flowPreviewRunes = 90
	flowItemThrottle = 2 * time.Second
)

// FlowHop is one arrow on the 实时流 page.
type FlowHop struct {
	Seq       int64  `json:"seq"`
	At        string `json:"at"`
	Kind      string `json:"kind"`
	From      string `json:"from"`
	To        string `json:"to"`
	Source    string `json:"source,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Title     string `json:"title"`
	Preview   string `json:"preview,omitempty"`
	Status    string `json:"status,omitempty"` // ok | wait | fail
	Via       string `json:"via,omitempty"`    // lan | cloud | local
	Bytes     int    `json:"bytes,omitempty"`
}

type flowRecorder struct {
	mu        sync.Mutex
	seq       int64
	ring      []FlowHop
	subs      map[chan FlowHop]struct{}
	sources   map[string]string // session -> agent
	lastItem  map[string]time.Time
	dispatchs map[string]string // dispatch id -> last status
	rooms     map[string]flowRoomState
	started   bool
}

type flowRoomState struct {
	speaking string
	count    int
	status   string
}

var flow = &flowRecorder{
	subs:      map[chan FlowHop]struct{}{},
	sources:   map[string]string{},
	lastItem:  map[string]time.Time{},
	dispatchs: map[string]string{},
	rooms:     map[string]flowRoomState{},
}

// RecordFlow adds one hop. Exported so other packages can describe moves the
// recorder cannot see on the event buses (for example an ACP permission).
func RecordFlow(hop FlowHop) {
	flow.record(hop)
}

func (f *flowRecorder) record(hop FlowHop) {
	if hop.From == "" || hop.To == "" {
		return
	}
	hop.Preview = flowShort(hop.Preview)
	hop.Title = flowShort(hop.Title)
	if hop.At == "" {
		hop.At = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	f.mu.Lock()
	f.seq++
	hop.Seq = f.seq
	if hop.SessionID != "" && hop.Source != "" {
		f.sources[hop.SessionID] = hop.Source
		if len(f.sources) > 2000 {
			f.sources = map[string]string{hop.SessionID: hop.Source}
		}
	}
	f.ring = append(f.ring, hop)
	if len(f.ring) > flowRingSize {
		f.ring = append([]FlowHop(nil), f.ring[len(f.ring)-flowRingSize:]...)
	}
	for ch := range f.subs {
		select {
		case ch <- hop:
		default:
		}
	}
	f.mu.Unlock()
}

func (f *flowRecorder) sourceOf(sessionID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sources[sessionID]
}

func (f *flowRecorder) since(seq int64) []FlowHop {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FlowHop, 0, len(f.ring))
	for _, hop := range f.ring {
		if hop.Seq > seq {
			out = append(out, hop)
		}
	}
	return out
}

func (f *flowRecorder) subscribe() (chan FlowHop, func()) {
	ch := make(chan FlowHop, 64)
	f.mu.Lock()
	f.subs[ch] = struct{}{}
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.subs, ch)
		f.mu.Unlock()
		close(ch)
	}
}

// start attaches the recorder to both event buses once per process.
func (f *flowRecorder) start() {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return
	}
	f.started = true
	f.mu.Unlock()

	events, _ := subscribe()
	named, _ := subscribeNamed()
	go func() {
		for event := range events {
			f.onEvent(event)
		}
	}()
	go func() {
		for message := range named {
			f.onNamed(message)
		}
	}()
}

func agentNode(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "unknown"
	}
	return "agent:" + source
}

func (f *flowRecorder) onEvent(event store.Event) {
	if event.Source == "" || event.Source == "homing" || event.Source == "phone" {
		return
	}
	status := "ok"
	kind := strings.ToLower(event.Kind)
	if strings.Contains(kind, "permission") || strings.Contains(kind, "approval") || strings.Contains(kind, "waiting") || strings.Contains(kind, "notification") {
		status = "wait"
	}
	if strings.Contains(kind, "error") || strings.Contains(kind, "fail") {
		status = "fail"
	}
	preview := event.Text
	if preview == "" {
		preview = event.Title
	}
	f.record(FlowHop{
		Kind:      "event",
		From:      agentNode(event.Source),
		To:        "homing",
		Source:    event.Source,
		SessionID: event.SessionID,
		Title:     flowEventTitle(event.Kind),
		Preview:   preview,
		Status:    status,
		Via:       "local",
		Bytes:     len(event.Text),
	})
}

func flowEventTitle(kind string) string {
	switch strings.ToLower(kind) {
	case "stop", "done", "turn_end", "complete", "completed":
		return "完成一轮"
	case "permission", "permission_request", "approval":
		return "请求授权"
	case "notification", "waiting", "needs_reply":
		return "等你回复"
	case "error", "failed":
		return "出错"
	case "start", "session_start":
		return "开始会话"
	}
	if kind == "" {
		return "事件"
	}
	return kind
}

func (f *flowRecorder) onNamed(message NamedMessage) {
	data := flowMap(message.Data)
	switch message.Name {
	case "notify":
		count := flowInt(data["count"])
		title := flowString(data["title"])
		if title == "" {
			title = "推送通知"
		}
		if count > 1 {
			title = fmt.Sprintf("%s（合并 %d 条）", title, count)
		}
		f.record(FlowHop{
			Kind:      "notify",
			From:      "homing",
			To:        "phone",
			Source:    flowString(data["source"]),
			SessionID: message.SessionID,
			Title:     title,
			Preview:   flowString(data["summary"]),
			Status:    "ok",
		})
	case "reply_status":
		status := flowString(data["status"])
		sessionID := flowString(data["session_id"])
		source := f.sourceOf(sessionID)
		hop := FlowHop{
			Kind:      "reply",
			From:      "homing",
			To:        agentNode(source),
			Source:    source,
			SessionID: sessionID,
			Preview:   flowString(data["message"]),
		}
		switch status {
		case "delivered", "sent", "injected", "ok", "confirmed":
			hop.Title = "回复已送达"
			hop.Status = "ok"
		case "failed", "rejected", "error":
			hop.Title = "回复失败"
			hop.Status = "fail"
		case "queued", "pending", "waiting":
			hop.Title = "回复排队中"
			hop.Status = "wait"
		default:
			return
		}
		f.record(hop)
	case "dispatch":
		id := flowString(data["dispatch_id"])
		status := flowString(data["status"])
		f.mu.Lock()
		previous, seen := f.dispatchs[id]
		f.dispatchs[id] = status
		if len(f.dispatchs) > 1000 {
			f.dispatchs = map[string]string{id: status}
		}
		f.mu.Unlock()
		if seen {
			return
		}
		_ = previous
		parent := flowString(data["parent_source"])
		from := agentNode(parent)
		if parent == "room" {
			from = "room:" + flowString(data["parent_session_id"])
		} else if parent == "" || parent == "phone" {
			from = "phone"
		}
		target := flowString(data["target_agent"])
		title := "派活"
		switch flowString(data["preset"]) {
		case "ask":
			title = "提问"
		case "review":
			title = "请求审查"
		case "handoff":
			title = "交接"
		case "do":
			title = "派活"
		}
		if parent == "room" {
			title = "请发言"
		}
		f.record(FlowHop{
			Kind:      "dispatch",
			From:      from,
			To:        agentNode(target),
			Source:    target,
			SessionID: flowString(data["target_session_id"]),
			Title:     title,
			Preview:   flowString(data["text"]),
			Status:    "wait",
			Bytes:     len(flowString(data["text"])),
		})
	case "collab_result":
		dispatch := flowMap(data["dispatch"])
		parent := flowString(dispatch["parent_source"])
		if parent == "room" {
			return // the room's own message hop covers it
		}
		to := agentNode(parent)
		if parent == "" || parent == "phone" {
			to = "phone"
		}
		target := flowString(dispatch["target_agent"])
		result := flowMap(dispatch["result"])
		status := "ok"
		if s := flowString(dispatch["status"]); s == "failed" || s == "error" {
			status = "fail"
		}
		f.record(FlowHop{
			Kind:      "result",
			From:      agentNode(target),
			To:        to,
			Source:    target,
			SessionID: flowString(dispatch["parent_session_id"]),
			Title:     "结果回传",
			Preview:   flowString(result["text"]),
			Status:    status,
			Bytes:     len(flowString(result["text"])),
		})
	case "room":
		f.onRoom(data)
	case "item":
		sessionID := flowString(data["session_id"])
		if sessionID == "" {
			sessionID = message.SessionID
		}
		item := flowMap(data["item"])
		role := flowString(item["role"])
		if role != "" && role != "assistant" {
			return
		}
		now := time.Now()
		f.mu.Lock()
		last := f.lastItem[sessionID]
		if now.Sub(last) < flowItemThrottle {
			f.mu.Unlock()
			return
		}
		f.lastItem[sessionID] = now
		if len(f.lastItem) > 1000 {
			f.lastItem = map[string]time.Time{sessionID: now}
		}
		f.mu.Unlock()
		source := flowString(data["source"])
		if source == "" {
			source = f.sourceOf(sessionID)
		}
		text := flowString(item["text"])
		f.record(FlowHop{
			Kind:      "output",
			From:      agentNode(source),
			To:        "homing",
			Source:    source,
			SessionID: sessionID,
			Title:     "输出",
			Preview:   text,
			Status:    "ok",
			Bytes:     len(text),
		})
	case "turn":
		source := flowString(data["source"])
		state := flowString(data["state"])
		title := "开始工作"
		if state != "start" && state != "running" && state != "busy" {
			title = "空闲"
		}
		f.record(FlowHop{
			Kind:      "turn",
			From:      agentNode(source),
			To:        agentNode(source),
			Source:    source,
			SessionID: flowString(data["session_id"]),
			Title:     title,
			Status:    "ok",
		})
	}
}

func (f *flowRecorder) onRoom(data map[string]any) {
	id := flowString(data["id"])
	if id == "" {
		return
	}
	speaking := flowString(data["speaking"])
	count := flowInt(data["message_count"])
	status := flowString(data["status"])
	f.mu.Lock()
	previous := f.rooms[id]
	f.rooms[id] = flowRoomState{speaking: speaking, count: count, status: status}
	f.mu.Unlock()
	node := "room:" + id
	title := flowString(data["title"])
	if count > previous.count && previous.speaking != "" {
		f.record(FlowHop{
			Kind:    "room",
			From:    agentNode(previous.speaking),
			To:      node,
			Source:  previous.speaking,
			Title:   "发言 · " + title,
			Preview: flowString(data["preview"]),
			Status:  "ok",
		})
	}
	if status == "ended" && previous.status != "ended" {
		f.record(FlowHop{
			Kind:   "room",
			From:   node,
			To:     "phone",
			Title:  "纪要完成 · " + title,
			Status: "ok",
		})
	}
}

// flowOrigin names where an audited request came from.
func (d *Deps) flowOrigin(r *http.Request) (string, string) {
	if r == nil {
		return "homing", "local"
	}
	if trusted, _ := r.Context().Value(trustedLocalRPCKey{}).(bool); trusted {
		if r.Header.Get("X-Homing-RPC") != "" {
			return "phone", "cloud"
		}
		if strings.HasPrefix(r.URL.Path, "/api/rooms/") {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/rooms/"), "/")
			return "room:" + parts[0], "local"
		}
		return "homing", "local"
	}
	if device := d.auditDevice(r); device != "local" {
		return "phone", "lan"
	}
	if isLoopback(r.RemoteAddr) {
		return "console", "local"
	}
	return "phone", "lan"
}

// recordAuditFlow is called from Audit: every action a person takes (reply,
// approval, new session, dispatch, room control) becomes a hop into homing.
func (d *Deps) recordAuditFlow(r *http.Request, action string, fields map[string]any) {
	from, via := d.flowOrigin(r)
	if strings.HasPrefix(from, "room:") && action == "reply" {
		return // the dispatch hop already shows the room asking an agent
	}
	title := map[string]string{
		"reply":            "发送回复",
		"approval":         "审批",
		"new_session":      "新建会话",
		"dispatch":         "@ 协作派活",
		"stop_session":     "停止会话",
		"cancel":           "撤回回复",
		"forward":          "转发结果",
		"room_create":      "开会",
		"room_say":         "会议插话",
		"room_pause":       "暂停会议",
		"room_resume":      "继续会议",
		"room_end":         "结束会议",
		"room_extend":      "加一轮",
		"agent_setup":      "安装 Agent",
		"discard_worktree": "丢弃工作树",
	}[action]
	if title == "" {
		if strings.HasPrefix(action, "library_") || strings.HasPrefix(action, "search_index") {
			return
		}
		title = action
	}
	if decision, _ := fields["decision"].(string); decision != "" {
		title += " · " + decision
	}
	sessionID, _ := fields["session_id"].(string)
	to := "homing"
	if roomID, _ := fields["room_id"].(string); roomID != "" {
		to = "room:" + roomID
	}
	source, _ := fields["target"].(string)
	if source == "" && sessionID != "" {
		source = flow.sourceOf(sessionID)
	}
	if from == to {
		return
	}
	flow.record(FlowHop{
		Kind:      "action",
		From:      from,
		To:        to,
		Source:    source,
		SessionID: sessionID,
		Title:     title,
		Status:    "ok",
		Via:       via,
	})
}

func flowMap(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return typed
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func flowString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(typed, 10)
	case int:
		return strconv.Itoa(typed)
	}
	return ""
}

func flowInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	}
	return 0
}

func flowShort(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= flowPreviewRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:flowPreviewRunes]) + "…"
}

// flowNode is one box on the 实时流 page.
type flowNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"` // phone | homing | console | agent | room
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

func (d *Deps) flowNodes() []flowNode {
	nodes := []flowNode{
		{ID: "phone", Kind: "phone", Label: "手机"},
		{ID: "console", Kind: "console", Label: "电脑控制台"},
		{ID: "homing", Kind: "homing", Label: "Homing", Detail: "本机服务"},
	}
	if s, ok := d.store(); ok {
		devices := s.Devices()
		if len(devices) > 0 {
			names := make([]string, 0, len(devices))
			for _, device := range devices {
				if device.Name != "" {
					names = append(names, device.Name)
				}
			}
			nodes[0].Detail = strings.Join(names, "、")
			if len(names) > 1 {
				nodes[0].Label = fmt.Sprintf("手机 ×%d", len(names))
			}
		}
	}
	return nodes
}

func init() {
	Register("flow", 45, func(mux *http.ServeMux, d *Deps) {
		flow.start()
		mux.HandleFunc("GET /api/flow", d.guard(func(w http.ResponseWriter, r *http.Request) {
			since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
			flow.mu.Lock()
			seq := flow.seq
			flow.mu.Unlock()
			WriteJSON(w, http.StatusOK, map[string]any{
				"seq":   seq,
				"hops":  flow.since(since),
				"nodes": d.flowNodes(),
			})
		}))
		mux.HandleFunc("GET /api/flow/stream", d.guard(func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
				return
			}
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, no-transform")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			ch, cancel := flow.subscribe()
			defer cancel()
			keepalive := time.NewTicker(20 * time.Second)
			defer keepalive.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-keepalive.C:
					if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
						return
					}
					flusher.Flush()
				case hop, open := <-ch:
					if !open {
						return
					}
					raw, err := json.Marshal(hop)
					if err != nil {
						continue
					}
					if _, err := fmt.Fprintf(w, "id: %d\nevent: hop\ndata: %s\n\n", hop.Seq, raw); err != nil {
						return
					}
					flusher.Flush()
				}
			}
		}))
	})
}
