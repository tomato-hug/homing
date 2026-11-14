package relayapi

// 会议室 R1：几个 Agent 按顺序轮流发言的只读讨论，结束时生成会议纪要。
// 设计见 docs/homing-v3/review-2026-11-12.md §4。
//
// 每位参会者在会议里对应一个真实的原生会话：第一次发言用只读的协作派发
// （preset ask、workspace readonly、不回传结果卡）新建子会话，之后每一轮在同一个
// 子会话里追问，只发送它上次发言之后的新内容。一轮发言何时结束沿用协作派发的判断
// （Stop / notify 钩子带回最后一段回答）；没有钩子时，会话条目 45 秒没有变化且
// 末尾是助手回答，也算结束。
//
// 会议状态存在 <数据目录>/rooms/<id>.json，服务重启后进行中的会议变成「已暂停」。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/orchestrator"
	"github.com/tomato-hug/homing/internal/store"
)

const (
	roomMinParticipants   = 2
	roomMaxParticipants   = 4
	roomDefaultRounds     = 2
	roomMaxRounds         = 5
	roomTopicRunes        = 4000
	roomUserMessageRunes  = 4000
	roomStatementRunes    = 6000
	roomTranscriptRunes   = 8000 // 首次发言提示里带的会议记录上限（派发提示总长上限 12000）
	roomTurnTimeout       = 15 * time.Minute
	roomIdleCompletion    = 45 * time.Second
	roomPollInterval      = 2 * time.Second
	roomItemPollInterval  = 5 * time.Second
	roomModeRoundRobin    = "round_robin"
	roomStatusRunning     = "running"
	roomStatusPaused      = "paused"
	roomStatusSummarizing = "summarizing"
	roomStatusEnded       = "ended"
)

// Room 是一场会议。
type Room struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Topic        string            `json:"topic"`
	Cwd          string            `json:"cwd"`
	Mode         string            `json:"mode"`
	Rounds       int               `json:"rounds"`
	Round        int               `json:"round"`
	Status       string            `json:"status"`
	Speaking     string            `json:"speaking,omitempty"`
	NextSpeaker  string            `json:"next_speaker,omitempty"`
	EndRequested bool              `json:"end_requested,omitempty"`
	ActiveTurn   *RoomTurnState    `json:"active_turn,omitempty"`
	Participants []RoomParticipant `json:"participants"`
	Messages     []RoomMessage     `json:"messages"`
	Minutes      string            `json:"minutes,omitempty"`
	Note         string            `json:"note,omitempty"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
}

// RoomTurnState is the durable execution slot. Once submitting is saved, recovery
// only observes the existing dispatch: it must never replay a native operation.
type RoomTurnState struct {
	Agent      string `json:"agent"`
	Round      int    `json:"round"`
	Kind       string `json:"kind"`
	Phase      string `json:"phase"` // claimed (safe to submit), submitting, uncertain
	DispatchID string `json:"dispatch_id,omitempty"`
	Baseline   string `json:"baseline,omitempty"`
}

// RoomParticipant 是一位参会的 Agent。
type RoomParticipant struct {
	Agent      string `json:"agent"`
	Name       string `json:"name"`
	Role       string `json:"role,omitempty"`
	DispatchID string `json:"dispatch_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	// Status：waiting 等待发言 | speaking 发言中 | needs_you 在子会话里等你处理 | failed 无法继续
	Status   string `json:"status"`
	LastSeen int    `json:"last_seen"`
	Spoken   bool   `json:"spoken"`
}

// RoomMessage 是会议记录里的一条。
type RoomMessage struct {
	Seq   int    `json:"seq"`
	From  string `json:"from"` // user | homing | agent id
	Name  string `json:"name"`
	Kind  string `json:"kind"` // statement | user | system | minutes
	Round int    `json:"round"`
	Text  string `json:"text"`
	At    string `json:"at"`
}

// roomTurn 是一次发言的结果。
type roomTurn struct {
	Text       string
	DispatchID string
	SessionID  string
}

// roomSpeaker 让某位参会者发言并等它说完。测试里换成假的。
type roomSpeaker interface {
	Speak(ctx context.Context, room Room, p RoomParticipant, prompt string, onState func(string)) (roomTurn, error)
}

var (
	roomsMu      sync.Mutex
	roomsLoaded  = map[string]bool{} // 按数据目录记录是否已从磁盘加载
	roomsByID    = map[string]*Room{}
	roomEngines  = map[string]context.CancelFunc{}
	roomSpeakers = func(d *api.Deps) roomSpeaker { return dispatchRoomSpeaker{d: d} }
	roomNow      = func() time.Time { return time.Now().UTC() }
)

func init() {
	api.Register("rooms", 61, func(mux *http.ServeMux, d *api.Deps) {
		mux.HandleFunc("GET /api/rooms", guarded(d, listRooms))
		mux.HandleFunc("POST /api/rooms", guarded(d, createRoom))
		mux.HandleFunc("GET /api/rooms/{id}", guarded(d, getRoom))
		mux.HandleFunc("POST /api/rooms/{id}/messages", guarded(d, postRoomMessage))
		mux.HandleFunc("POST /api/rooms/{id}/control", guarded(d, controlRoom))
	})
}

func roomStamp() string { return roomNow().Format("2006-01-02T15:04:05.000Z") }

func roomsDir(d *api.Deps) string { return filepath.Join(d.DataDir, "rooms") }

// ---- 存储 --------------------------------------------------------------------

// ensureRoomsLoaded 在首次访问时读取磁盘上的会议。调用方必须持有 roomsMu。
func ensureRoomsLoaded(d *api.Deps) {
	if roomsLoaded[d.DataDir] {
		return
	}
	roomsLoaded[d.DataDir] = true
	entries, err := os.ReadDir(roomsDir(d))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(roomsDir(d), entry.Name()))
		if err != nil {
			continue
		}
		var room Room
		if json.Unmarshal(raw, &room) != nil || room.ID == "" {
			continue
		}
		if room.Status == roomStatusRunning || room.Status == roomStatusSummarizing {
			if room.Status == roomStatusSummarizing {
				room.EndRequested = true
			}
			// Legacy files did not record whether the native operation was sent.
			// Preserve that uncertainty instead of automatically replaying it.
			if room.ActiveTurn == nil && room.Speaking != "" {
				kind := "statement"
				if room.Status == roomStatusSummarizing {
					kind = "minutes"
				}
				room.ActiveTurn = &RoomTurnState{Agent: room.Speaking, Round: room.Round, Kind: kind, Phase: "uncertain"}
			}
			room.Status = roomStatusPaused
			room.Speaking = ""
			room.Note = "电脑端服务重启过，会议已暂停；点「继续」接着开"
			for i := range room.Participants {
				if room.Participants[i].Status == "speaking" {
					room.Participants[i].Status = "waiting"
				}
			}
			if err := saveRoomLocked(d, &room); err != nil && d.Log != nil {
				d.Log.Warn("save recovered room", "room", room.ID, "err", err)
			}
		}
		copied := room
		roomsByID[room.ID] = &copied
	}
}

func saveRoomLocked(d *api.Deps, room *Room) error {
	if err := os.MkdirAll(roomsDir(d), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(room, "", "  ")
	if err != nil {
		return err
	}
	file := filepath.Join(roomsDir(d), room.ID+".json")
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

func cloneRoom(room *Room) Room {
	out := *room
	if room.ActiveTurn != nil {
		active := *room.ActiveTurn
		out.ActiveTurn = &active
	}
	out.Participants = append([]RoomParticipant(nil), room.Participants...)
	out.Messages = append([]RoomMessage(nil), room.Messages...)
	return out
}

// mutateRoom 在锁内修改一场会议、保存并推送，返回修改后的副本。
func mutateRoom(d *api.Deps, id string, fn func(*Room) error) (Room, error) {
	roomsMu.Lock()
	ensureRoomsLoaded(d)
	room, ok := roomsByID[id]
	if !ok {
		roomsMu.Unlock()
		return Room{}, errRoomNotFound
	}
	candidate := cloneRoom(room)
	if err := fn(&candidate); err != nil {
		roomsMu.Unlock()
		return Room{}, err
	}
	candidate.UpdatedAt = roomStamp()
	if err := saveRoomLocked(d, &candidate); err != nil {
		roomsMu.Unlock()
		return Room{}, fmt.Errorf("%w：%v", errRoomPersistence, err)
	}
	roomsByID[id] = &candidate
	snapshot := cloneRoom(&candidate)
	roomsMu.Unlock()
	api.PublishNamed("room", "", roomSummary(snapshot))
	return snapshot, nil
}

// A worker cannot advance after failing to commit its slot/result. Even when the
// disk remains unavailable, expose a paused state in memory and retain the last
// durable active slot so a restart can collect the original result.
func pauseRoomOnError(d *api.Deps, id string, cause error) {
	roomsMu.Lock()
	room := roomsByID[id]
	if room == nil {
		roomsMu.Unlock()
		return
	}
	candidate := cloneRoom(room)
	candidate.Status = roomStatusPaused
	candidate.Speaking = ""
	candidate.Note = cause.Error() + "；会议已暂停，请检查后继续；投递状态不明时可结束并汇总已有记录"
	if errors.Is(cause, errRoomTurnUncertain) && candidate.ActiveTurn != nil {
		candidate.ActiveTurn.Phase = "uncertain"
	}
	candidate.UpdatedAt = roomStamp()
	_ = saveRoomLocked(d, &candidate)
	roomsByID[id] = &candidate
	snapshot := cloneRoom(&candidate)
	roomsMu.Unlock()
	api.PublishNamed("room", "", roomSummary(snapshot))
}

func loadRoom(d *api.Deps, id string) (Room, bool) {
	roomsMu.Lock()
	defer roomsMu.Unlock()
	ensureRoomsLoaded(d)
	room, ok := roomsByID[id]
	if !ok {
		return Room{}, false
	}
	return cloneRoom(room), true
}

var errRoomNotFound = errors.New("会议不存在")
var errRoomInvalidParticipant = errors.New("点名对象无效")
var errRoomPersistence = errors.New("会议保存失败")
var errRoomTurnUncertain = errors.New("无法确认上次投递状态，未重复发送")
var errRoomIdle = errors.New("会议当前不需要发言")

// ---- 接口 --------------------------------------------------------------------

func listRooms(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	roomsMu.Lock()
	ensureRoomsLoaded(d)
	rooms := make([]Room, 0, len(roomsByID))
	for _, room := range roomsByID {
		rooms = append(rooms, cloneRoom(room))
	}
	roomsMu.Unlock()
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].UpdatedAt > rooms[j].UpdatedAt })
	out := make([]map[string]any, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, roomSummary(room))
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"rooms": out, "agents": roomAgentChoices(r.Context())})
}

func getRoom(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	room, ok := loadRoom(d, strings.TrimSpace(r.PathValue("id")))
	if !ok {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": errRoomNotFound.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "room": room})
}

// roomSummary 是列表里一行：不带完整记录，只带最后一条的摘要。
func roomSummary(room Room) map[string]any {
	preview := ""
	for i := len(room.Messages) - 1; i >= 0; i-- {
		if room.Messages[i].Kind == "statement" || room.Messages[i].Kind == "user" {
			preview = room.Messages[i].Name + "：" + truncateRunes(firstLineOf(room.Messages[i].Text), 80)
			break
		}
	}
	return map[string]any{
		"id":            room.ID,
		"title":         room.Title,
		"topic":         truncateRunes(room.Topic, 200),
		"cwd":           room.Cwd,
		"mode":          room.Mode,
		"rounds":        room.Rounds,
		"round":         room.Round,
		"status":        room.Status,
		"speaking":      room.Speaking,
		"next_speaker":  room.NextSpeaker,
		"end_requested": room.EndRequested,
		"participants":  room.Participants,
		"message_count": len(room.Messages),
		"preview":       preview,
		"has_minutes":   strings.TrimSpace(room.Minutes) != "",
		"note":          room.Note,
		"created_at":    room.CreatedAt,
		"updated_at":    room.UpdatedAt,
	}
}

// roomAgentChoices 列出可以被拉进会议的 Agent：能新建会话并且能被协作派发的。
func roomAgentChoices(ctx context.Context) []map[string]any {
	out := []map[string]any{}
	for _, a := range agent.All(ctx) {
		if !dispatchLauncher.CanDispatch(a.ID()) {
			continue
		}
		det := a.Detect(ctx)
		if !det.Installed {
			continue
		}
		out = append(out, map[string]any{"id": a.ID(), "name": a.Name()})
	}
	return out
}

type createRoomRequest struct {
	Title        string `json:"title"`
	Topic        string `json:"topic"`
	Cwd          string `json:"cwd"`
	Rounds       int    `json:"rounds"`
	Mode         string `json:"mode"`
	Participants []struct {
		Agent string `json:"agent"`
		Role  string `json:"role"`
	} `json:"participants"`
}

func createRoom(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	if !d.CheckRate(w, r, "room", 10, time.Minute) {
		return
	}
	_, raw, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var req createRoomRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求格式不正确"})
		return
	}
	room, code, err := newRoom(r.Context(), req)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": code, "error": err.Error()})
		return
	}

	roomsMu.Lock()
	ensureRoomsLoaded(d)
	saveErr := saveRoomLocked(d, &room)
	if saveErr == nil {
		roomsByID[room.ID] = &room
	}
	snapshot := cloneRoom(&room)
	roomsMu.Unlock()
	if saveErr != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存会议失败"})
		return
	}
	api.PublishNamed("room", "", roomSummary(snapshot))
	startRoomEngine(d, room.ID)
	d.Audit(r, "room_create", map[string]any{"room_id": room.ID, "participants": len(room.Participants)})
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "room": snapshot})
}

// newRoom 校验请求并生成会议（纯函数，便于测试）。
func newRoom(ctx context.Context, req createRoomRequest) (Room, string, error) {
	topic := strings.TrimSpace(req.Topic)
	if topic == "" {
		return Room{}, "topic_required", errors.New("议题不能为空")
	}
	if utf8.RuneCountInString(topic) > roomTopicRunes {
		return Room{}, "topic_too_long", fmt.Errorf("议题不能超过 %d 字", roomTopicRunes)
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = roomModeRoundRobin
	}
	if mode != roomModeRoundRobin {
		return Room{}, "mode_unsupported", errors.New("这一版只支持轮流发言")
	}
	rounds := req.Rounds
	if rounds == 0 {
		rounds = roomDefaultRounds
	}
	if rounds < 1 || rounds > roomMaxRounds {
		return Room{}, "rounds_invalid", fmt.Errorf("轮数只能是 1 到 %d", roomMaxRounds)
	}
	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" {
		return Room{}, "cwd_required", errors.New("请选择一个工作目录，参会的 Agent 会在这里阅读代码")
	}
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return Room{}, "cwd_invalid", errors.New("工作目录不可用")
	}
	if info, err := os.Stat(absolute); err != nil || !info.IsDir() {
		return Room{}, "cwd_invalid", errors.New("工作目录不存在")
	}
	if len(req.Participants) < roomMinParticipants || len(req.Participants) > roomMaxParticipants {
		return Room{}, "participants_invalid", fmt.Errorf("参会 Agent 需要 %d 到 %d 个", roomMinParticipants, roomMaxParticipants)
	}
	seen := map[string]bool{}
	participants := make([]RoomParticipant, 0, len(req.Participants))
	for _, item := range req.Participants {
		id := strings.ToLower(strings.TrimSpace(item.Agent))
		if id == "" || seen[id] {
			return Room{}, "participants_invalid", errors.New("参会 Agent 不能为空或重复")
		}
		seen[id] = true
		a, ok := agent.Get(ctx, id)
		if !ok || !dispatchLauncher.CanDispatch(id) {
			return Room{}, "agent_not_dispatchable", fmt.Errorf("%s 还不能被拉进会议（不能从 Homing 新建会话）", id)
		}
		role := strings.TrimSpace(item.Role)
		if utf8.RuneCountInString(role) > 40 {
			role = truncateRunes(role, 40)
		}
		participants = append(participants, RoomParticipant{
			Agent: id, Name: a.Name(), Role: role, Status: "waiting",
		})
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = truncateRunes(firstLineOf(topic), 30)
	}
	now := roomStamp()
	return Room{
		ID:           "room-" + randomHex(6),
		Title:        title,
		Topic:        topic,
		Cwd:          absolute,
		Mode:         mode,
		Rounds:       rounds,
		Status:       roomStatusRunning,
		Participants: participants,
		Messages:     []RoomMessage{},
		CreatedAt:    now,
		UpdatedAt:    now,
	}, "", nil
}

func postRoomMessage(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	text := strings.TrimSpace(stringField(body, "text"))
	if text == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "发言不能为空"})
		return
	}
	if utf8.RuneCountInString(text) > roomUserMessageRunes {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "发言太长"})
		return
	}
	room, err := mutateRoom(d, strings.TrimSpace(r.PathValue("id")), func(room *Room) error {
		if room.Status == roomStatusEnded || room.Status == roomStatusSummarizing || room.EndRequested {
			return errors.New("会议已经在收尾或已结束，不能再插话")
		}
		appendRoomMessage(room, RoomMessage{From: "user", Name: "你", Kind: "user", Round: room.Round, Text: text})
		return nil
	})
	if err != nil {
		writeRoomError(w, err)
		return
	}
	d.Audit(r, "room_say", map[string]any{"room_id": room.ID})
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "room": room})
}

func controlRoom(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	action := strings.ToLower(strings.TrimSpace(stringField(body, "action")))
	start := false
	room, err := mutateRoom(d, id, func(room *Room) error {
		switch action {
		case "call":
			if (room.Status != roomStatusRunning && room.Status != roomStatusPaused) || room.EndRequested {
				return errors.New("会议已经在收尾或已结束")
			}
			target := strings.ToLower(strings.TrimSpace(stringField(body, "agent")))
			idx := -1
			for i, p := range room.Participants {
				if p.Agent == target {
					idx = i
					break
				}
			}
			if idx < 0 {
				return fmt.Errorf("%w：请选择房内的参会者", errRoomInvalidParticipant)
			}
			if room.Participants[idx].Status == "failed" {
				return errors.New("这位参会者已失败，请查看发言记录中的原因")
			}
			if room.Speaking == target {
				return errors.New("这位参会者正在发言，无需再次点名")
			}
			afterCurrent := cloneRoom(room)
			if room.Speaking != "" {
				afterCurrent.Messages = append(afterCurrent.Messages, RoomMessage{From: room.Speaking, Kind: "statement", Round: room.Round})
			}
			_, round := nextRoomSpeaker(afterCurrent)
			if round == 0 || roomHasSpoken(afterCurrent, target, round) {
				return errors.New("这位参会者在本轮已发言，下一轮再点名")
			}
			room.NextSpeaker = target
		case "pause":
			if room.Status != roomStatusRunning || room.EndRequested {
				return errors.New("会议不在进行中")
			}
			room.Status = roomStatusPaused
			room.Note = "已暂停：当前发言说完后不再请下一位"
		case "resume":
			if room.Status != roomStatusPaused {
				return errors.New("会议没有暂停")
			}
			room.Status = roomStatusRunning
			room.Note = ""
			start = true
		case "end":
			if room.Status == roomStatusEnded || room.Status == roomStatusSummarizing {
				return errors.New("会议已经在收尾或已结束")
			}
			room.EndRequested = true
			room.Note = "正在结束：当前发言说完后整理会议纪要"
			if room.Status == roomStatusPaused {
				room.Status = roomStatusRunning
			}
			start = true
		case "extend":
			if room.Status == roomStatusEnded || room.Status == roomStatusSummarizing || room.EndRequested {
				return errors.New("会议已经在收尾或已结束")
			}
			if room.Rounds >= roomMaxRounds {
				return fmt.Errorf("最多 %d 轮", roomMaxRounds)
			}
			room.Rounds++
		default:
			return errors.New("action 只能是 call / pause / resume / end / extend")
		}
		return nil
	})
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if start {
		startRoomEngine(d, id)
	}
	d.Audit(r, "room_"+action, map[string]any{"room_id": id})
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "room": room})
}

func writeRoomError(w http.ResponseWriter, err error) {
	status := http.StatusConflict
	if errors.Is(err, errRoomNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, errRoomPersistence) {
		status = http.StatusInternalServerError
	} else if errors.Is(err, errRoomInvalidParticipant) {
		status = http.StatusBadRequest
	}
	api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
}

// ---- 会议进程 ------------------------------------------------------------------

func appendRoomMessage(room *Room, msg RoomMessage) RoomMessage {
	next := 1
	if n := len(room.Messages); n > 0 {
		next = room.Messages[n-1].Seq + 1
	}
	msg.Seq = next
	if msg.At == "" {
		msg.At = roomStamp()
	}
	room.Messages = append(room.Messages, msg)
	return msg
}

// startRoomEngine 为一场会议启动后台进程；已经在跑就什么也不做。
func startRoomEngine(d *api.Deps, id string) {
	roomsMu.Lock()
	if _, running := roomEngines[id]; running {
		roomsMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	roomEngines[id] = cancel
	roomsMu.Unlock()
	go func() {
		defer func() {
			finishRoomEngine(ctx, d, id)
			cancel()
		}()
		runRoom(ctx, d, id, roomSpeakers(d))
	}()
}

func finishRoomEngine(ctx context.Context, d *api.Deps, id string) {
	roomsMu.Lock()
	delete(roomEngines, id)
	room := roomsByID[id]
	restart := ctx.Err() == nil && room != nil && room.Status == roomStatusRunning
	roomsMu.Unlock()
	// Resume may have seen our old marker after we observed paused.
	if restart {
		startRoomEngine(d, id)
	}
}

// nextRoomSpeaker 返回下一位发言者的下标和轮次；没有了返回 -1。
// 轮流模式：每一轮里每位（未失败的）参会者按顺序各发言一次。
func nextRoomSpeaker(room Room) (int, int) {
	spoke := map[string]bool{}
	for _, msg := range room.Messages {
		if msg.From != "user" && msg.From != "homing" && (msg.Kind == "statement" || msg.Kind == "system") {
			spoke[fmt.Sprintf("%s#%d", msg.From, msg.Round)] = true
		}
	}
	for round := 1; round <= room.Rounds; round++ {
		first := -1
		for i, p := range room.Participants {
			if p.Status == "failed" {
				continue
			}
			if !spoke[fmt.Sprintf("%s#%d", p.Agent, round)] {
				if p.Agent == room.NextSpeaker {
					return i, round
				}
				if first < 0 {
					first = i
				}
			}
		}
		if first >= 0 {
			return first, round
		}
	}
	return -1, 0
}

func roomHasSpoken(room Room, source string, round int) bool {
	for _, msg := range room.Messages {
		if msg.From == source && msg.Round == round && (msg.Kind == "statement" || msg.Kind == "system") {
			return true
		}
	}
	return false
}

func runRoom(ctx context.Context, d *api.Deps, id string, speaker roomSpeaker) {
	for {
		if ctx.Err() != nil {
			return
		}
		idx, round, finish := -1, 0, false
		// Select and claim the turn under the same lock as pause/call/end.
		room, claimErr := mutateRoom(d, id, func(room *Room) error {
			if room.Status != roomStatusRunning {
				return errRoomIdle
			}
			if active := room.ActiveTurn; active != nil {
				if active.Phase == "uncertain" && room.EndRequested {
					for i := range room.Participants {
						if room.Participants[i].Agent == active.Agent {
							room.Participants[i].Status = "failed"
						}
					}
					appendRoomMessage(room, RoomMessage{From: active.Agent, Name: active.Agent, Kind: "system", Round: active.Round, Text: "上次投递状态不明，结束会议时保留已有记录；未重新发送"})
					room.ActiveTurn = nil
				} else {
					if active.Kind == "minutes" {
						finish = true
						room.Status = roomStatusSummarizing
						room.EndRequested = true
						return nil
					}
					for i := range room.Participants {
						if room.Participants[i].Agent == active.Agent {
							idx = i
							break
						}
					}
					if idx < 0 {
						return errRoomTurnUncertain
					}
					round = active.Round
					room.Round = round
					room.Speaking = active.Agent
					room.Participants[idx].Status = "speaking"
					return nil
				}
			}
			idx, round = nextRoomSpeaker(*room)
			if room.EndRequested || idx < 0 {
				finish = true
				room.Status = roomStatusSummarizing
				room.NextSpeaker = ""
				room.Note = "正在整理会议纪要"
				return nil
			}
			room.ActiveTurn = &RoomTurnState{Agent: room.Participants[idx].Agent, Round: round, Kind: "statement", Phase: "claimed"}
			room.Round = round
			room.Speaking = room.Participants[idx].Agent
			room.Participants[idx].Status = "speaking"
			room.NextSpeaker = ""
			return nil
		})
		if claimErr != nil {
			if !errors.Is(claimErr, errRoomIdle) {
				pauseRoomOnError(d, id, claimErr)
			}
			return
		}
		if finish {
			summarizeRoom(ctx, d, id, speaker)
			return
		}
		participant := room.Participants[idx]
		prompt := buildRoomPrompt(room, idx, round)
		promptSeq := 0
		if n := len(room.Messages); n > 0 {
			promptSeq = room.Messages[n-1].Seq
		}
		var stateErr error
		turn, err := speaker.Speak(ctx, room, participant, prompt, func(state string) {
			_, stateErr = mutateRoom(d, id, func(room *Room) error {
				if idx < len(room.Participants) {
					room.Participants[idx].Status = state
				}
				return nil
			})
		})
		if ctx.Err() != nil {
			return
		}
		if stateErr != nil {
			pauseRoomOnError(d, id, stateErr)
			return
		}
		if errors.Is(err, errRoomPersistence) || errors.Is(err, errRoomTurnUncertain) {
			pauseRoomOnError(d, id, err)
			return
		}
		_, commitErr := mutateRoom(d, id, func(room *Room) error {
			room.ActiveTurn = nil
			p := &room.Participants[idx]
			if turn.DispatchID != "" {
				p.DispatchID = turn.DispatchID
			}
			if turn.SessionID != "" {
				p.SessionID = turn.SessionID
			}
			room.Speaking = ""
			if err != nil {
				appendRoomMessage(room, RoomMessage{
					From: p.Agent, Name: p.Name, Kind: "system", Round: round,
					Text: p.Name + " 这一轮没有发言：" + err.Error(),
				})
				// 第一次就失败（会话没建起来）的参会者不再点名，避免整场卡住。
				if p.DispatchID == "" {
					p.Status = "failed"
				} else {
					p.Status = "waiting"
				}
				p.LastSeen = promptSeq
				return nil
			}
			msg := appendRoomMessage(room, RoomMessage{
				From: p.Agent, Name: p.Name, Kind: "statement", Round: round,
				Text: truncateRunes(strings.TrimSpace(turn.Text), roomStatementRunes),
			})
			p.Status = "waiting"
			p.Spoken = true
			p.LastSeen = msg.Seq
			return nil
		})
		if commitErr != nil {
			pauseRoomOnError(d, id, commitErr)
			return
		}
	}
}

func summarizeRoom(ctx context.Context, d *api.Deps, id string, speaker roomSpeaker) {
	chair := -1
	room, err := mutateRoom(d, id, func(room *Room) error {
		if room.Status == roomStatusEnded || room.Minutes != "" {
			return errRoomIdle
		}
		room.Status = roomStatusSummarizing
		room.EndRequested = true
		room.Note = "正在整理会议纪要"
		for i, p := range room.Participants {
			if room.ActiveTurn != nil {
				if p.Agent == room.ActiveTurn.Agent {
					chair = i
					break
				}
			} else if p.Status != "failed" && p.DispatchID != "" {
				chair = i
				break
			}
		}
		if chair >= 0 {
			if room.ActiveTurn == nil {
				room.ActiveTurn = &RoomTurnState{Agent: room.Participants[chair].Agent, Round: room.Round, Kind: "minutes", Phase: "claimed"}
			}
			room.Speaking = room.Participants[chair].Agent
			room.Participants[chair].Status = "speaking"
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, errRoomIdle) {
			pauseRoomOnError(d, id, err)
		}
		return
	}
	minutes := ""
	if chair >= 0 {
		turn, speakErr := speaker.Speak(ctx, room, room.Participants[chair], buildMinutesPrompt(room), func(string) {})
		if ctx.Err() != nil {
			return
		}
		if errors.Is(speakErr, errRoomPersistence) || errors.Is(speakErr, errRoomTurnUncertain) {
			pauseRoomOnError(d, id, speakErr)
			return
		}
		if speakErr == nil {
			minutes = strings.TrimSpace(turn.Text)
		}
	}
	fallback := minutes == ""
	if fallback {
		minutes = fallbackMinutes(room)
	}
	_, err = mutateRoom(d, id, func(room *Room) error {
		name, from := "Homing", "homing"
		if chair >= 0 && !fallback {
			name = room.Participants[chair].Name
			from = room.Participants[chair].Agent
		}
		for i := range room.Participants {
			if room.Participants[i].Status != "failed" {
				room.Participants[i].Status = "waiting"
			}
		}
		appendRoomMessage(room, RoomMessage{From: from, Name: name, Kind: "minutes", Round: room.Round, Text: minutes})
		room.Minutes = minutes
		room.Status = roomStatusEnded
		room.ActiveTurn = nil
		room.Speaking = ""
		room.EndRequested = false
		room.Note = ""
		if fallback {
			room.Note = "没有拿到 Agent 整理的纪要，下面是按发言顺序的汇总"
		}
		return nil
	})
	if err != nil {
		pauseRoomOnError(d, id, err)
	}
}

// ---- 提示词 --------------------------------------------------------------------

func participantLabel(p RoomParticipant) string {
	if p.Role == "" {
		return p.Name
	}
	return p.Name + "（" + p.Role + "）"
}

func formatRoomMessage(room Room, msg RoomMessage) string {
	name := msg.Name
	for _, p := range room.Participants {
		if p.Agent == msg.From {
			name = participantLabel(p)
		}
	}
	switch msg.Kind {
	case "user":
		return fmt.Sprintf("[第%d轮] 用户：%s", msg.Round, msg.Text)
	case "system":
		return fmt.Sprintf("[第%d轮] （%s）", msg.Round, msg.Text)
	case "minutes":
		return ""
	}
	return fmt.Sprintf("[第%d轮] %s：%s", msg.Round, name, msg.Text)
}

// transcriptTail 拼会议记录，超出上限时只保留最近的部分。
func transcriptTail(room Room, messages []RoomMessage, limit int) string {
	lines := []string{}
	for _, msg := range messages {
		if line := formatRoomMessage(room, msg); line != "" {
			lines = append(lines, line)
		}
	}
	text := strings.Join(lines, "\n\n")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return "（更早的记录已省略）\n…" + string(runes[len(runes)-limit:])
}

func buildRoomPrompt(room Room, idx, round int) string {
	p := room.Participants[idx]
	others := []string{}
	for i, other := range room.Participants {
		if i != idx {
			others = append(others, participantLabel(other))
		}
	}
	rules := "规则：这是只读讨论，不要修改任何文件，不要运行会改变状态的命令；可以阅读工作目录里的代码作为依据。" +
		"每次发言不超过 400 字，先给结论再给理由；同意或反对别人时点名并说明原因；不确定就直接说不确定。只输出你的发言内容。"
	if !p.Spoken && p.DispatchID == "" {
		transcript := transcriptTail(room, room.Messages, roomTranscriptRunes)
		if transcript == "" {
			transcript = "（还没有人发言，你是第一位）"
		}
		return strings.Join([]string{
			"你正在参加一场由 Homing 召集的多 Agent 会议。",
			"议题：" + room.Topic,
			"工作目录：" + room.Cwd,
			"参会者：" + strings.Join(append([]string{participantLabel(p) + "（你）"}, others...), "、") + "，以及用户。",
			fmt.Sprintf("形式：轮流发言，共 %d 轮。", room.Rounds),
			rules,
			"到目前为止的会议记录：\n" + transcript,
			fmt.Sprintf("现在轮到你（%s）发言，第 %d 轮。", participantLabel(p), round),
		}, "\n\n")
	}
	fresh := []RoomMessage{}
	for _, msg := range room.Messages {
		if msg.Seq > p.LastSeen && msg.From != p.Agent {
			fresh = append(fresh, msg)
		}
	}
	delta := transcriptTail(room, fresh, roomTranscriptRunes)
	if delta == "" {
		delta = "（你上次发言之后没有新的内容）"
	}
	return strings.Join([]string{
		fmt.Sprintf("【会议继续 · 第 %d 轮 / 共 %d 轮】议题：%s", round, room.Rounds, truncateRunes(firstLineOf(room.Topic), 120)),
		"你上次发言之后的新内容：\n" + delta,
		rules,
		"现在轮到你发言。",
	}, "\n\n")
}

func buildMinutesPrompt(room Room) string {
	return strings.Join([]string{
		"会议结束了，请你整理会议纪要。",
		"议题：" + room.Topic,
		"要求：用 Markdown；包含「## 结论」「## 各方观点」「## 分歧」「## 行动项」四节；行动项写清负责人和验收标准；" +
			"只根据下面的记录整理，不要加入记录里没有的内容；仍然只读，不要修改任何文件。",
		"完整记录：\n" + transcriptTail(room, room.Messages, roomTranscriptRunes),
	}, "\n\n")
}

func fallbackMinutes(room Room) string {
	var b strings.Builder
	b.WriteString("## 发言汇总\n")
	for _, msg := range room.Messages {
		switch msg.Kind {
		case "statement":
			b.WriteString(fmt.Sprintf("\n- 第%d轮 %s：%s", msg.Round, msg.Name, truncateRunes(firstLineOf(msg.Text), 160)))
		case "user":
			b.WriteString(fmt.Sprintf("\n- 第%d轮 你：%s", msg.Round, truncateRunes(firstLineOf(msg.Text), 160)))
		}
	}
	return b.String()
}

func firstLineOf(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return strings.TrimSpace(text[:i])
	}
	return text
}

// ---- 通过协作派发发言 -------------------------------------------------------------

type dispatchRoomSpeaker struct{ d *api.Deps }

func (sp dispatchRoomSpeaker) Speak(ctx context.Context, room Room, p RoomParticipant, prompt string, onState func(string)) (roomTurn, error) {
	d := sp.d
	s, err := d.Store()
	if err != nil {
		return roomTurn{}, fmt.Errorf("%w：数据目录不可用", errRoomPersistence)
	}
	turn := roomTurn{DispatchID: p.DispatchID, SessionID: p.SessionID}
	if room.ActiveTurn == nil || room.ActiveTurn.Agent != p.Agent {
		return turn, errRoomTurnUncertain
	}
	active := *room.ActiveTurn
	if active.Phase == "uncertain" {
		return turn, errRoomTurnUncertain
	}
	if active.Phase == "claimed" {
		if p.DispatchID == "" {
			active.DispatchID = "dispatch-" + randomHex(12)
		} else {
			dispatch, ok := findDispatch(s, p.DispatchID)
			if !ok || dispatch.TargetSessionID == "" {
				return turn, errors.New("找不到这位参会者的会话")
			}
			active.DispatchID = p.DispatchID
			active.Baseline, _ = roomAssistantTail(ctx, dispatch.TargetAgent, dispatch.TargetSessionID)
			// Clear the previous result before saving the submission boundary.
			// A crash here is still a claimed (never sent) slot.
			dispatch.Status = "running"
			dispatch.Message = "会议室：等待发言"
			dispatch.Result = nil
			if _, err := s.SaveDispatch(dispatch); err != nil {
				return turn, fmt.Errorf("%w：%v", errRoomPersistence, err)
			}
		}
		active.Phase = "submitting"
		if _, err := mutateRoom(d, room.ID, func(current *Room) error { current.ActiveTurn = &active; return nil }); err != nil {
			return turn, err
		}
		if p.DispatchID == "" {
			service := orchestrator.Service{Store: s, Launcher: dispatchLauncher, NewID: func() string { return active.DispatchID }}
			dispatch, dispatchErr := service.Dispatch(ctx, orchestrator.Request{
				ParentSessionID: room.ID, ParentSource: "room", TargetAgent: p.Agent, Cwd: room.Cwd,
				Text: truncateRunes("会议室「"+room.Title+"」发言", 200), Prompt: prompt,
				Mode: "parallel", Preset: "ask", Workspace: "readonly", Return: "none",
			})
			if dispatchErr != nil {
				if dispatch.Status == "failed" {
					return turn, dispatchErr
				}
				return turn, errRoomTurnUncertain
			}
			publishDispatch(d, dispatch)
		} else {
			dispatch, _ := findDispatch(s, p.DispatchID)
			request, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1/api/rooms/"+room.ID, nil)
			if reqErr != nil {
				return turn, errRoomTurnUncertain
			}
			request.RemoteAddr = "127.0.0.1:0"
			_, _, _, replyErr := sendComposeReply(d, api.WithTrustedLocalRPC(request), s,
				dispatch.TargetAgent, dispatch.TargetSessionID, dispatch.Cwd, nil, prompt, nil, "", "", "read_only")
			if replyErr != nil {
				return turn, errRoomTurnUncertain
			}
		}
	}
	// Submitting is a durable one-way boundary. On restart, consume/wait for the
	// original dispatch, even if its result completed before transcript commit.
	turn.DispatchID = active.DispatchID
	dispatch, ok := findDispatch(s, turn.DispatchID)
	if !ok || dispatch.ParentSessionID != room.ID || dispatch.TargetAgent != p.Agent {
		return turn, errRoomTurnUncertain
	}
	if dispatch.TargetSessionID == "" && dispatch.RequestID == "" && dispatch.Status != "completed" && dispatch.Status != "failed" && dispatch.Status != "cancelled" {
		return turn, errRoomTurnUncertain
	}
	turn.SessionID = dispatch.TargetSessionID
	syncSession := func() error {
		_, err := mutateRoom(d, room.ID, func(current *Room) error {
			for i := range current.Participants {
				participant := &current.Participants[i]
				if participant.Agent == p.Agent {
					participant.DispatchID = turn.DispatchID
					if turn.SessionID != "" {
						participant.SessionID = turn.SessionID
					}
					break
				}
			}
			return nil
		})
		return err
	}
	if err := syncSession(); err != nil {
		return turn, err
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var syncErr error
	text, sessionID, err := waitRoomTurn(waitCtx, s, turn.DispatchID, active.Baseline, func(state string) {
		if dispatch, ok := findDispatch(s, turn.DispatchID); ok && dispatch.TargetSessionID != "" {
			turn.SessionID = dispatch.TargetSessionID
		}
		if syncErr = syncSession(); syncErr != nil {
			cancel()
			return
		}
		onState(state)
	})
	if syncErr != nil {
		return turn, syncErr
	}
	if sessionID != "" {
		turn.SessionID = sessionID
	}
	turn.Text = text
	return turn, err
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// waitRoomTurn 等一次发言结束，返回它说的话。
func waitRoomTurn(ctx context.Context, s *store.Store, dispatchID, baseline string, onState func(string)) (string, string, error) {
	deadline := time.Now().Add(roomTurnTimeout)
	ticker := time.NewTicker(roomPollInterval)
	defer ticker.Stop()
	lastSig := ""
	lastChange := time.Now()
	var lastItemPoll time.Time
	needsYou := false
	for {
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-ticker.C:
		}
		dispatch, ok := findDispatch(s, dispatchID)
		if !ok {
			return "", "", errors.New("协作记录丢失")
		}
		switch dispatch.Status {
		case "completed":
			if dispatch.Result != nil && strings.TrimSpace(dispatch.Result.Text) != "" {
				return dispatch.Result.Text, dispatch.TargetSessionID, nil
			}
		case "failed", "cancelled":
			return "", dispatch.TargetSessionID, errors.New(firstNonEmptyString(dispatch.Message, "子会话已停止"))
		case "needs_you":
			if !needsYou {
				needsYou = true
				onState("needs_you")
			}
		default:
			if needsYou {
				needsYou = false
				onState("speaking")
			}
		}
		// 没有钩子时的兜底：会话条目一段时间不再变化，且末尾是新的助手回答。
		if dispatch.TargetSessionID != "" && time.Since(lastItemPoll) >= roomItemPollInterval {
			lastItemPoll = time.Now()
			sig, text := roomAssistantTail(ctx, dispatch.TargetAgent, dispatch.TargetSessionID)
			if sig != lastSig {
				lastSig = sig
				lastChange = time.Now()
			} else if text != "" && sig != baseline && time.Since(lastChange) >= roomIdleCompletion {
				return text, dispatch.TargetSessionID, nil
			}
		}
		if time.Now().After(deadline) {
			return "", dispatch.TargetSessionID, errors.New("超过 15 分钟没有说完，已跳过")
		}
	}
}

// roomAssistantTail 读会话末尾：返回一个随条目变化的签名，以及末尾的助手回答（末尾不是助手回答时为空）。
func roomAssistantTail(ctx context.Context, source, sessionID string) (string, string) {
	a, ok := agent.Get(ctx, source)
	if !ok {
		return "", ""
	}
	reader, ok := a.(agent.ItemReader)
	if !ok {
		return "", ""
	}
	items, err := reader.ReadItems(ctx, sessionID, 40)
	if err != nil || len(items) == 0 {
		return "", ""
	}
	last := items[len(items)-1]
	sig := fmt.Sprintf("%d:%s:%d", len(items), last.ID, utf8.RuneCountInString(last.Text))
	if last.Role == "assistant" && last.Type == "assistant" && strings.TrimSpace(last.Text) != "" {
		return sig, last.Text
	}
	return sig, ""
}
