package relayapi

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/service"
	"github.com/tomato-hug/homing/internal/store"
)

var dispatchDoneKinds = map[string]bool{
	"reply": true, "done": true, "stop": true, "turn_complete": true, "completed": true,
}

var dispatchWaitingKinds = map[string]bool{
	"permission": true, "needs_input": true, "error": true,
}

// Completion hooks can arrive concurrently (for example reply and stop).
// Serialize the read-modify-write so a result is published and forwarded once.
var dispatchLifecycleMu sync.Mutex

func trackDispatchLifecycle(d *api.Deps, event store.Event) {
	dispatchLifecycleMu.Lock()
	defer dispatchLifecycleMu.Unlock()
	if event.SessionID == "" {
		return
	}
	s, err := d.Store()
	if err != nil {
		return
	}
	var matched *store.Dispatch
	for _, dispatch := range s.Dispatches() {
		if dispatch.TargetSessionID != event.SessionID || dispatch.TargetAgent != event.Source {
			continue
		}
		found := dispatch
		matched = &found
	}
	if matched == nil {
		return
	}
	dispatch := *matched
	switch {
	case dispatchDoneKinds[event.Kind]:
		if dispatch.Result != nil || dispatch.Status == "cancelled" || dispatch.Status == "failed" {
			return
		}
		dispatch.Status = "completed"
		dispatch.Message = "协作任务已完成"
		finishedAt := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
		dispatch.Result = &store.DispatchResult{
			Text:         truncateRunes(strings.TrimSpace(event.Text), 8000),
			FilesChanged: dispatchChangedFiles(d, event.Source, event.SessionID),
			FinishedAt:   finishedAt,
			Branch:       dispatch.Branch,
			DiffStat:     dispatchDiffStat(dispatch),
		}
		saved, saveErr := s.SaveDispatch(dispatch)
		if saveErr != nil {
			d.Log.Error("complete dispatch", "dispatch", dispatch.ID, "err", saveErr)
			return
		}
		publishDispatch(d, saved)
		publishCollabResult(d, s, saved)
		if saved.Return == "inject" {
			autoForwardDispatch(d, s, saved)
		}
	case dispatchWaitingKinds[event.Kind]:
		if dispatch.Status == "completed" || dispatch.Status == "cancelled" {
			return
		}
		dispatch.Status = "needs_you"
		dispatch.Message = "子任务正在等待你处理"
		saved, saveErr := s.SaveDispatch(dispatch)
		if saveErr == nil {
			publishDispatch(d, saved)
		}
	default:
		if dispatch.Status == "starting" || dispatch.Status == "waiting_session" ||
			dispatch.Status == "started" || dispatch.Status == "needs_you" {
			dispatch.Status = "running"
			dispatch.Message = "子任务正在运行"
			saved, saveErr := s.SaveDispatch(dispatch)
			if saveErr == nil {
				publishDispatch(d, saved)
			}
		}
	}
}

func publishCollabResult(d *api.Deps, s *store.Store, dispatch store.Dispatch) {
	if dispatch.Return == "none" || dispatch.Result == nil || dispatch.ParentSessionID == "" {
		return
	}
	meta := map[string]any{
		"dispatch_id":     dispatch.ID,
		"collab_agent":    dispatch.TargetAgent,
		"collab_preset":   dispatch.Preset,
		"collab_status":   dispatch.Status,
		"target_session":  dispatch.TargetSessionID,
		"files_changed":   len(dispatch.Result.FilesChanged),
		"result_finished": dispatch.Result.FinishedAt,
	}
	event := store.Event{
		Source:      firstNonEmpty(dispatch.ParentSource, "unknown"),
		Kind:        "collab_result",
		SessionID:   dispatch.ParentSessionID,
		Cwd:         dispatch.Cwd,
		Title:       "@" + dispatch.TargetAgent + " 完成协作任务",
		Text:        dispatch.Result.Text,
		SessionMeta: meta,
	}
	if svc := service.Start(d); svc != nil {
		if stored, err := svc.IngestStoreEvent(event); err == nil {
			event = stored
		} else {
			d.Log.Error("store collab result", "dispatch", dispatch.ID, "err", err)
		}
	}
	api.PublishNamed("collab_result", dispatch.ParentSessionID, map[string]any{
		"session_id": dispatch.ParentSessionID,
		"dispatch":   dispatch,
		"event":      event,
	})
}

func autoForwardDispatch(d *api.Deps, s *store.Store, dispatch store.Dispatch) {
	if dispatch.Return != "inject" || dispatch.Result == nil ||
		strings.TrimSpace(dispatch.Result.Text) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	parent, found := d.FindSummary(ctx, dispatch.ParentSessionID)
	if !found {
		saveAutoForwardFailure(d, s, dispatch, "父会话不存在")
		return
	}
	cwd := dispatch.Cwd
	if parent.Cwd != nil && strings.TrimSpace(*parent.Cwd) != "" {
		cwd = strings.TrimSpace(*parent.Cwd)
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		"http://homing.local/api/dispatches/"+dispatch.ID+"/forward",
		nil,
	)
	if err != nil {
		saveAutoForwardFailure(d, s, dispatch, err.Error())
		return
	}
	text := "【@" + dispatch.TargetAgent + " 的結論】\n" + dispatch.Result.Text
	_, _, note, err := sendComposeReply(
		d, request, s,
		firstNonEmpty(parent.Source, dispatch.ParentSource), dispatch.ParentSessionID, cwd,
		nil, text, nil, "", "", "",
	)
	if err != nil {
		saveAutoForwardFailure(d, s, dispatch, err.Error())
		return
	}
	d.Audit(nil, "forward", map[string]any{
		"dispatch_id":       dispatch.ID,
		"parent_session_id": dispatch.ParentSessionID,
		"target":            dispatch.TargetAgent,
	})
	dispatch.Message = firstNonEmpty(note, "已完成；结论已自动转交父 Agent")
	if saved, saveErr := s.SaveDispatch(dispatch); saveErr == nil {
		publishDispatch(d, saved)
	}
}

func saveAutoForwardFailure(d *api.Deps, s *store.Store, dispatch store.Dispatch, detail string) {
	d.Audit(nil, "forward_failed", map[string]any{
		"dispatch_id":       dispatch.ID,
		"parent_session_id": dispatch.ParentSessionID,
		"target":            dispatch.TargetAgent,
	})
	dispatch.Message = "已完成；自动转交失败：" + truncateRunes(strings.TrimSpace(detail), 240)
	if saved, err := s.SaveDispatch(dispatch); err == nil {
		publishDispatch(d, saved)
	}
	d.Log.Error("auto-forward dispatch result", "dispatch", dispatch.ID, "err", detail)
}

func dispatchChangedFiles(d *api.Deps, source, sessionID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, ok := agent.Get(ctx, source)
	if !ok {
		return nil
	}
	reader, ok := a.(agent.ItemReader)
	if !ok {
		return nil
	}
	items, err := reader.ReadItems(ctx, sessionID, 200)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var files []string
	for i := len(items) - 1; i >= 0 && len(files) < 20; i-- {
		if items[i].Type != "file_change" {
			continue
		}
		for _, file := range items[i].Files {
			if file.Path == "" || seen[file.Path] {
				continue
			}
			seen[file.Path] = true
			files = append(files, file.Path)
		}
	}
	return files
}

func dispatchDiffStat(dispatch store.Dispatch) string {
	if strings.TrimSpace(dispatch.Cwd) == "" || strings.TrimSpace(dispatch.BaseCommit) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := gitOutput(ctx, dispatch.Cwd, "diff", "--stat", dispatch.BaseCommit)
	if err != nil {
		return ""
	}
	return truncateRunes(strings.TrimSpace(out), 4000)
}

func forwardDispatch(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "数据目录不可用"})
		return
	}
	dispatch, ok := findDispatch(s, strings.TrimSpace(r.PathValue("id")))
	if !ok {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "协作任务不存在"})
		return
	}
	if dispatch.Result == nil || strings.TrimSpace(dispatch.Result.Text) == "" {
		api.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "协作任务还没有结果"})
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	text := strings.TrimSpace(stringField(body, "text"))
	if text == "" {
		text = "【@" + dispatch.TargetAgent + " 的結論】\n" + dispatch.Result.Text
	}
	parent, found := d.FindSummary(r.Context(), dispatch.ParentSessionID)
	if !found {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "父会话不存在"})
		return
	}
	cwd := dispatch.Cwd
	if parent.Cwd != nil {
		cwd = *parent.Cwd
	}
	replyID, status, note, replyErr := sendComposeReply(
		d, r, s, parent.Source, parent.SessionID, cwd, nil, text, nil, "", "", "",
	)
	if replyErr != nil {
		api.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": replyErr.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "dispatch_id": dispatch.ID,
		"reply": map[string]any{"reply_id": replyID, "status": status, "message": note},
	})
	d.Audit(r, "forward", map[string]any{
		"dispatch_id":       dispatch.ID,
		"parent_session_id": dispatch.ParentSessionID,
		"target":            dispatch.TargetAgent,
	})
}

func replyDispatch(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "数据目录不可用"})
		return
	}
	dispatch, ok := findDispatch(s, strings.TrimSpace(r.PathValue("id")))
	if !ok {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "协作任务不存在"})
		return
	}
	if dispatch.TargetSessionID == "" {
		api.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "子会话还没有绑定"})
		return
	}
	if dispatch.Status == "cancelled" {
		api.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "协作任务已取消"})
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	text := strings.TrimSpace(stringField(body, "text"))
	if text == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "text 不能为空"})
		return
	}
	replyID, status, note, replyErr := sendComposeReply(
		d, r, s, dispatch.TargetAgent, dispatch.TargetSessionID, dispatch.Cwd,
		nil, text, nil, "", "", "",
	)
	if replyErr != nil {
		api.WriteJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": replyErr.Error()})
		return
	}
	dispatch.Status = "running"
	dispatch.Message = "已追问，等待子任务继续"
	dispatch.Result = nil
	saved, saveErr := s.SaveDispatch(dispatch)
	if saveErr != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存协作状态失败"})
		return
	}
	publishDispatch(d, saved)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "dispatch": saved,
		"reply": map[string]any{"reply_id": replyID, "status": status, "message": note},
	})
	d.Audit(r, "reply", map[string]any{
		"dispatch_id": saved.ID,
		"target":      saved.TargetAgent,
	})
}

func cancelDispatch(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "数据目录不可用"})
		return
	}
	dispatch, ok := findDispatch(s, strings.TrimSpace(r.PathValue("id")))
	if !ok {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "协作任务不存在"})
		return
	}
	if dispatch.Status == "cancelled" || dispatch.Status == "completed" {
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "dispatch": dispatch})
		return
	}
	stopped := stopManagedSession(dispatch.TargetSessionID)
	dispatch.Status = "cancelled"
	dispatch.Message = "已取消"
	if !stopped {
		dispatch.Message = "已标记取消；该会话不是 Homing 启动的进程"
	}
	saved, saveErr := s.SaveDispatch(dispatch)
	if saveErr != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "保存协作状态失败"})
		return
	}
	publishDispatch(d, saved)
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "dispatch": saved, "stopped": stopped})
	d.Audit(r, "cancel", map[string]any{
		"dispatch_id": saved.ID,
		"target":      saved.TargetAgent,
	})
}
