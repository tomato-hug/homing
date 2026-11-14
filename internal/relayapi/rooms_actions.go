package relayapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/orchestrator"
	"github.com/tomato-hug/homing/internal/store"
)

// RoomAction is a user-approved task derived from a minutes item. Persist the
// launch identity before any external side effects, so retries cannot relaunch.
type RoomAction struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	Agent        string `json:"agent,omitempty"`
	DispatchID   string `json:"dispatch_id,omitempty"`
	Status       string `json:"status"`
	SessionID    string `json:"session_id,omitempty"`
	Result       string `json:"result,omitempty"`
	Error        string `json:"error,omitempty"`
	BaseCommit   string `json:"base_commit,omitempty"`
	AfterEventID int64  `json:"after_event_id,omitempty"`
}

var roomActionLine = regexp.MustCompile(`^\s*(?:[-*+]\s+|[0-9]+[.)]\s+)(?:\[[ xX]\]\s*)?(\S.*)$`)
var roomActionTableSeparator = regexp.MustCompile(`^:?-{3,}:?$`)

// roomTableCells handles escaped pipes without treating them as column breaks.
func roomTableCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return nil
	}
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") {
		line = strings.TrimSuffix(line, "|")
	}
	var cells []string
	var cell strings.Builder
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) && runes[i+1] == '|' {
			cell.WriteRune('|')
			i++
			continue
		}
		if runes[i] == '|' {
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
			continue
		}
		cell.WriteRune(runes[i])
	}
	cells = append(cells, strings.TrimSpace(cell.String()))
	if len(cells) < 2 {
		return nil
	}
	return cells
}

func roomActionTableTaskColumn(headers []string) int {
	for i, header := range headers {
		h := strings.ToLower(strings.Trim(strings.TrimSpace(header), " *_`：:"))
		switch h {
		case "行动", "行动项", "行动内容", "行動", "行動項", "行動內容", "任务", "任务内容", "任務", "任務內容", "事项", "事項", "工作内容", "工作內容", "action", "action item", "action items", "task", "tasks":
			return i
		}
	}
	return -1
}

func proposedRoomActions(minutes string) []RoomAction {
	// List IDs retain their historical ordinal even when a minutes table is
	// present, so already-dispatched list actions never shift to another item.
	lists, tables := []RoomAction{}, []RoomAction{}
	inActions, inCode := false, false
	var headers []string
	taskColumn := -1
	tableReady := false
	for _, line := range strings.Split(minutes, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCode = !inCode
			headers = nil
			tableReady = false
			continue
		}
		if inCode {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			title := strings.Trim(strings.TrimSpace(strings.TrimLeft(trimmed, "#")), " *：:")
			inActions = title == "行动项" || title == "行動項" || strings.EqualFold(title, "action items") || strings.EqualFold(title, "actions")
			headers = nil
			tableReady = false
			continue
		}
		if !inActions {
			continue
		}
		if cells := roomTableCells(trimmed); cells != nil {
			switch {
			case tableReady:
				if len(cells) != len(headers) {
					headers = nil
					tableReady = false
					continue
				}
				task := strings.TrimSpace(cells[taskColumn])
				if task == "" || task == "-" || task == "—" {
					continue
				}
				parts := []string{task}
				for i, value := range cells {
					value = strings.TrimSpace(value)
					if i == taskColumn || value == "" {
						continue
					}
					label := strings.Trim(strings.TrimSpace(headers[i]), " *_`：:")
					if label != "" {
						parts = append(parts, label+"："+value)
					}
				}
				if len(tables) < 30 {
					tables = append(tables, RoomAction{ID: fmt.Sprintf("action-table-%d", len(tables)+1), Text: truncateRunes(strings.Join(parts, "；"), roomUserMessageRunes), Status: "suggested"})
				}
			case headers != nil:
				valid := len(cells) == len(headers)
				for _, cell := range cells {
					if !roomActionTableSeparator.MatchString(strings.TrimSpace(cell)) {
						valid = false
						break
					}
				}
				if valid {
					tableReady = true
				} else {
					headers = nil
				}
			default:
				if column := roomActionTableTaskColumn(cells); column >= 0 {
					headers, taskColumn = cells, column
				}
			}
			continue
		}
		headers = nil
		tableReady = false
		match := roomActionLine.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		text := strings.TrimSpace(match[1])
		if text == "" {
			continue
		}
		lists = append(lists, RoomAction{ID: fmt.Sprintf("action-%d", len(lists)+1), Text: truncateRunes(text, roomUserMessageRunes), Status: "suggested"})
		if len(lists) >= 30 {
			break
		}
	}
	if remaining := 30 - len(lists); len(tables) > remaining {
		tables = tables[:remaining]
	}
	return append(lists, tables...)
}
func roomActionViews(d *api.Deps, room Room) []RoomAction {
	actions := proposedRoomActions(room.Minutes)
	for i := range actions {
		for _, saved := range room.Actions {
			if saved.ID == actions[i].ID {
				actions[i] = saved
				break
			}
		}
	}
	if len(actions) == 0 {
		return actions
	}
	s, err := d.Store()
	if err != nil {
		return actions
	}
	head := roomGitOutput(room.Cwd, "rev-parse", "HEAD")
	for i := range actions {
		a := &actions[i]
		if a.Status == "suggested" || a.Status == "prepare_failed" || a.Status == "preparing" {
			a.BaseCommit = head
		}
		if a.DispatchID == "" {
			continue
		}
		dispatch, ok := findDispatch(s, a.DispatchID)
		if !ok {
			if a.Status == "submitting" || a.Status == "launching" {
				a.Status = "unknown"
			}
			continue
		}
		if dispatch.TargetSessionID != "" && dispatch.Result == nil {
			for _, event := range s.Events() {
				if event.ID <= a.AfterEventID || event.SessionID != dispatch.TargetSessionID || event.Source != dispatch.TargetAgent {
					continue
				}
				if dispatchDoneKinds[event.Kind] || (dispatchWaitingKinds[event.Kind] && dispatch.Status != "needs_you") {
					trackDispatchLifecycle(d, event)
					dispatch, _ = findDispatch(s, a.DispatchID)
					if dispatch.Result != nil {
						break
					}
				}
			}
		}
		if dispatch.TargetAgent == "codex" && dispatch.TargetSessionID != "" && dispatch.Result == nil &&
			(dispatch.Status == "starting" || dispatch.Status == "started" || dispatch.Status == "running" || dispatch.Status == "waiting_session") {
			if text, complete := roomCodexFinished(roomCodexSessionsDir(), dispatch.TargetSessionID, dispatch.Cwd); complete {
				trackDispatchLifecycle(d, store.Event{Source: dispatch.TargetAgent, SessionID: dispatch.TargetSessionID, Kind: "done", Text: text})
				dispatch, _ = findDispatch(s, a.DispatchID)
			}
		}
		a.Status = dispatch.Status
		a.SessionID = dispatch.TargetSessionID
		a.BaseCommit = dispatch.BaseCommit
		if dispatch.Result != nil {
			a.Result = dispatch.Result.Text
		}
		if dispatch.Status == "failed" || dispatch.Status == "cancelled" {
			a.Error = dispatch.Message
		}
	}
	return actions
}

var roomActionWorkers = struct {
	sync.Mutex
	running map[string]bool
}{running: map[string]bool{}}

func roomGitOutput(cwd string, args ...string) string {
	command := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// The path is determined solely by the persisted dispatch ID. A recovered
// preparation can reuse it only if it is a clean worktree of the same repo.
func prepareRoomActionWorktree(dataDir, cwd, dispatchID string) (string, string, string, error) {
	root := roomGitOutput(cwd, "rev-parse", "--show-toplevel")
	if root == "" {
		return "", "", "", errors.New("工作目录不是 Git 仓库")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", "", err
	}
	realCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", "", "", err
	}
	relative, err := filepath.Rel(realRoot, realCwd)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", "", errors.New("工作目录不在仓库中")
	}
	path := filepath.Join(dataDir, "worktrees", dispatchID)
	branch := "homing/" + dispatchID
	base := ""
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		path, branch, base, err = createDispatchWorktree(dataDir, cwd, dispatchID)
		if err != nil {
			return "", "", "", err
		}
	} else if err != nil {
		return "", "", "", err
	} else {
		actualRoot := roomGitOutput(path, "rev-parse", "--show-toplevel")
		common := roomGitOutput(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
		expectedCommon := roomGitOutput(cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
		actualReal, _ := filepath.EvalSymlinks(actualRoot)
		pathReal, _ := filepath.EvalSymlinks(path)
		commonReal, _ := filepath.EvalSymlinks(common)
		expectedReal, _ := filepath.EvalSymlinks(expectedCommon)
		if actualReal == "" || actualReal != pathReal || commonReal == "" || commonReal != expectedReal || roomGitOutput(path, "branch", "--show-current") != branch {
			return "", "", "", errors.New("已存在的工作区无法验证，未继续启动")
		}
		status := exec.Command("git", "-C", path, "status", "--porcelain", "--untracked-files=all")
		output, err := status.Output()
		if err != nil || len(strings.TrimSpace(string(output))) != 0 {
			return "", "", "", errors.New("已存在的工作区有更改或无法检查，未继续启动")
		}
		base = roomGitOutput(path, "rev-parse", "HEAD")
	}
	child := filepath.Join(path, relative)
	info, err := os.Stat(child)
	if err != nil || !info.IsDir() {
		return "", "", "", errors.New("提交中没有原来的工作子目录")
	}
	return child, branch, base, nil
}

func dispatchRoomAction(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	confirmed, _ := body["confirmed"].(bool)
	target := strings.ToLower(strings.TrimSpace(stringField(body, "agent")))
	task := strings.TrimSpace(stringField(body, "text"))
	expectedBase := strings.TrimSpace(stringField(body, "base_commit"))
	if !confirmed || target == "" || task == "" || utf8.RuneCountInString(task) > roomUserMessageRunes {
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "请确认行动项、目标 Agent 和任务内容"})
		return
	}
	id, actionID := strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.PathValue("action"))
	room, exists := loadRoom(d, id)
	if !exists {
		writeRoomError(w, errRoomNotFound)
		return
	}
	if room.Status != roomStatusEnded {
		writeRoomError(w, errors.New("会议结束后才能派发行动项"))
		return
	}
	found, member := false, false
	for _, a := range proposedRoomActions(room.Minutes) {
		if a.ID == actionID {
			found = true
			break
		}
	}
	for _, participant := range room.Participants {
		if participant.Agent == target {
			member = true
			break
		}
	}
	if !found || !member || !dispatchLauncher.CanDispatch(target) {
		api.WriteJSON(w, 400, map[string]any{"ok": false, "error": "行动项或目标 Agent 无效"})
		return
	}
	respondRoom := func() {
		current, _ := loadRoom(d, id)
		current.Actions = roomActionViews(d, current)
		api.WriteJSON(w, 200, map[string]any{"ok": true, "room": current})
	}
	dispatchID := "dsp_" + randomHex(10)
	for _, existing := range room.Actions {
		if existing.ID != actionID {
			continue
		}
		if existing.Status != "prepare_failed" && (existing.Agent != target || existing.Text != task) {
			writeRoomError(w, errors.New("行动项已使用其他内容派发，请先查看已有任务"))
			return
		}
		if existing.Status != "preparing" && existing.Status != "prepare_failed" {
			respondRoom()
			return
		}
		dispatchID = existing.DispatchID
	}
	key := d.DataDir + "\x00" + id + "\x00" + actionID
	roomActionWorkers.Lock()
	if roomActionWorkers.running[key] {
		roomActionWorkers.Unlock()
		respondRoom()
		return
	}
	roomActionWorkers.running[key] = true
	roomActionWorkers.Unlock()
	defer func() { roomActionWorkers.Lock(); delete(roomActionWorkers.running, key); roomActionWorkers.Unlock() }()
	// Re-read after acquiring the worker slot: another request may have finished.
	room, _ = loadRoom(d, id)
	for _, existing := range room.Actions {
		if existing.ID != actionID {
			continue
		}
		if existing.Status != "prepare_failed" && (existing.Agent != target || existing.Text != task) {
			writeRoomError(w, errors.New("行动项已使用其他内容派发，请先查看已有任务"))
			return
		}
		if existing.Status != "preparing" && existing.Status != "prepare_failed" {
			respondRoom()
			return
		}
		dispatchID = existing.DispatchID
	}
	head := roomGitOutput(room.Cwd, "rev-parse", "HEAD")
	if expectedBase != "" && expectedBase != head {
		api.WriteJSON(w, 409, map[string]any{"ok": false, "error": "仓库基准已变化，请重新查看后确认"})
		return
	}
	s, err := d.Store()
	if err != nil {
		writeRoomError(w, fmt.Errorf("%w：数据目录不可用", errRoomPersistence))
		return
	}
	service := orchestrator.Service{Store: s, Launcher: dispatchLauncher, NewID: func() string { return dispatchID }}
	req := orchestrator.Request{ParentSessionID: id, ParentSource: "room", TargetAgent: target, Cwd: room.Cwd, Text: task, Mode: "parallel", Preset: "do", Workspace: "worktree", Return: "none"}
	if _, err = service.Prepare(req, nil); err != nil {
		var typed *orchestrator.Error
		if errors.As(err, &typed) {
			api.WriteJSON(w, typed.Status, map[string]any{"ok": false, "error": typed.Message})
			return
		}
		writeRoomError(w, err)
		return
	}
	var afterID int64
	for _, event := range s.Events() {
		if event.ID > afterID {
			afterID = event.ID
		}
	}
	_, err = mutateRoom(d, id, func(current *Room) error {
		if current.Status != roomStatusEnded {
			return errors.New("会议状态已改变")
		}
		for i := range current.Actions {
			a := &current.Actions[i]
			if a.ID != actionID {
				continue
			}
			if a.Status != "preparing" && a.Status != "prepare_failed" {
				return errors.New("行动项已经派发")
			}
			if a.Status == "preparing" && (a.Text != task || a.Agent != target) {
				return errors.New("行动项正在准备，请先查看")
			}
			*a = RoomAction{ID: actionID, Text: task, Agent: target, DispatchID: dispatchID, Status: "preparing", AfterEventID: afterID}
			return nil
		}
		current.Actions = append(current.Actions, RoomAction{ID: actionID, Text: task, Agent: target, DispatchID: dispatchID, Status: "preparing", AfterEventID: afterID})
		return nil
	})
	if err != nil {
		writeRoomError(w, err)
		return
	}
	child, branch, base, err := prepareRoomActionWorktree(d.DataDir, room.Cwd, dispatchID)
	if err == nil && expectedBase != "" && base != expectedBase {
		err = errors.New("工作区基准与确认时不同，请检查后重新确认")
	}
	if err != nil {
		_, saveErr := mutateRoom(d, id, func(current *Room) error {
			for i := range current.Actions {
				if current.Actions[i].ID == actionID {
					current.Actions[i].Status = "prepare_failed"
					current.Actions[i].Error = "无法准备独立工作区：" + err.Error()
				}
			}
			return nil
		})
		if saveErr != nil {
			writeRoomError(w, saveErr)
			return
		}
		api.WriteJSON(w, 409, map[string]any{"ok": false, "error": "无法准备独立工作区：" + err.Error()})
		return
	}
	// This durable boundary is immediately before the first native launch.
	_, err = mutateRoom(d, id, func(current *Room) error {
		for i := range current.Actions {
			a := &current.Actions[i]
			if a.ID == actionID && a.DispatchID == dispatchID && a.Status == "preparing" {
				a.Status = "launching"
				a.BaseCommit = base
				return nil
			}
		}
		return errors.New("行动项状态已改变")
	})
	if err != nil {
		writeRoomError(w, err)
		return
	}
	req.Cwd = child
	req.Branch = branch
	req.BaseCommit = base
	req.RepoCwd = room.Cwd
	req.Prompt = "会议「" + room.Title + "」的行动项（由用户确认）：\n" + task + "\n\n议题：" + truncateRunes(room.Topic, 1000) + "\n完成后报告结果和验收情况。"
	dispatch, dispatchErr := service.Dispatch(r.Context(), req)
	if dispatch.ID != "" {
		publishDispatch(d, dispatch)
	}
	if dispatchErr != nil {
		// Validation errors before the store record are provably pre-launch.
		var typed *orchestrator.Error
		if dispatch.ID == "" && errors.As(dispatchErr, &typed) && typed.Code != "dispatch_store_failed" && typed.Code != "launch_failed" {
			_, saveErr := mutateRoom(d, id, func(current *Room) error {
				for i := range current.Actions {
					if current.Actions[i].ID == actionID {
						current.Actions[i].Status = "prepare_failed"
						current.Actions[i].Error = typed.Message
					}
				}
				return nil
			})
			if saveErr != nil {
				writeRoomError(w, saveErr)
				return
			}
			api.WriteJSON(w, typed.Status, map[string]any{"ok": false, "error": typed.Message})
			return
		}
		api.WriteJSON(w, 502, map[string]any{"ok": false, "error": "任务启动状态待确认，请查看行动项", "dispatch_id": dispatchID})
		return
	}
	d.Audit(r, "room_action_dispatch", map[string]any{"room_id": id, "action_id": actionID, "dispatch_id": dispatch.ID, "target": target})
	respondRoom()
}
