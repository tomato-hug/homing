package relayapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/orchestrator"
	"github.com/tomato-hug/homing/internal/store"
)

var dispatchLauncher orchestrator.Launcher = relayLauncher{}

type relayLauncher struct{}

func (relayLauncher) CanDispatch(target string) bool {
	a, ok := agent.Get(context.Background(), strings.ToLower(strings.TrimSpace(target)))
	if !ok {
		return false
	}
	return effectiveCaps(a).Dispatch
}

func (relayLauncher) Start(ctx context.Context, options orchestrator.StartOptions) orchestrator.StartResult {
	target := strings.ToLower(strings.TrimSpace(options.Target))
	a, ok := agent.Get(ctx, target)
	if !ok {
		return orchestrator.StartResult{Status: "failed", Message: "未知 Agent"}
	}
	started, err := startAgentSession(ctx, a, agent.StartRequest{
		Cwd:            options.Cwd,
		Text:           options.Text,
		PermissionMode: dispatchPermissionMode(a, options.Preset, options.Workspace),
		Origin:         "dispatch",
	})
	if err != nil {
		return orchestrator.StartResult{Status: "failed", Message: err.Error()}
	}
	sessionID := started.SessionID
	requestID := started.RequestID
	if sessionID == "" && requestID == "" {
		requestID = randomHex(8)
		launches.mu.Lock()
		launches.pending[requestID] = launchInfo{
			source: strings.ToLower(strings.TrimSpace(target)),
			cwd:    options.Cwd,
			at:     time.Now(),
		}
		launches.mu.Unlock()
	}
	return orchestrator.StartResult{
		SessionID: sessionID,
		RequestID: requestID,
		Status:    started.Status,
		Message:   started.Message,
	}
}

// dispatchPermissionMode keeps investigation/review presets read-only when the
// target agent can enforce that mode. The preset cards promise this in the UI;
// silently launching with the default mode would make that promise false.
func dispatchPermissionMode(a agent.Agent, preset, workspace string) string {
	preset = strings.ToLower(strings.TrimSpace(preset))
	workspace = strings.ToLower(strings.TrimSpace(workspace))
	if workspace != "readonly" && preset != "ask" && preset != "review" {
		return ""
	}
	for _, mode := range agent.EffectiveCapabilities(a).PermissionModes {
		if strings.EqualFold(strings.TrimSpace(mode), "read_only") {
			return "read_only"
		}
	}
	return ""
}

func dispatchAgent(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	if !d.CheckRate(w, r, "dispatch", 20, time.Minute) {
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "数据目录不可用"})
		return
	}

	parentEventID, err := eventIDField(body)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var parentEvent *store.Event
	if parentEventID != nil {
		for _, event := range s.Events() {
			if event.ID != *parentEventID {
				continue
			}
			found := event
			parentEvent = &found
			break
		}
		if parentEvent == nil {
			api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "父事件不存在"})
			return
		}
	}

	parentSessionID := strings.TrimSpace(stringField(body, "parent_session_id"))
	parentSource := strings.TrimSpace(stringField(body, "parent_source"))
	if parentEvent != nil {
		if parentSessionID != "" && parentSessionID != parentEvent.SessionID {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "父事件与会话不匹配"})
			return
		}
		parentSessionID = parentEvent.SessionID
		parentSource = parentEvent.Source
	}
	if parentSessionID == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "parent_session_id 或 parent_event_id 必须提供一个"})
		return
	}
	if parentSource == "" {
		parentSource = d.SourceFor(r.Context(), parentSessionID)
	}
	parentSummary, found := d.FindSummary(r.Context(), parentSessionID)
	if !found {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "父会话不存在"})
		return
	}
	if parentSummary.Source != "" {
		if parentSource != "" && parentSource != parentSummary.Source {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "parent_source 与父会话不匹配"})
			return
		}
		parentSource = parentSummary.Source
	}

	parentCwd := ""
	if parentEvent != nil {
		parentCwd = strings.TrimSpace(parentEvent.Cwd)
	}
	if parentCwd == "" && parentSummary.Cwd != nil {
		parentCwd = strings.TrimSpace(*parentSummary.Cwd)
	}
	requestedCwd := strings.TrimSpace(stringField(body, "cwd"))
	cwd := requestedCwd
	if parentCwd != "" {
		absoluteParent, parentErr := filepath.Abs(parentCwd)
		if parentErr != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "父会话的 cwd 不可用"})
			return
		}
		if requestedCwd != "" {
			absoluteRequested, requestErr := filepath.Abs(requestedCwd)
			if requestErr != nil || absoluteRequested != absoluteParent {
				api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cwd 必须与父会话一致"})
				return
			}
		}
		cwd = absoluteParent
	}
	if cwd == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "父会话没有可用的 cwd，请显式提供"})
		return
	}
	returnMode := strings.ToLower(strings.TrimSpace(stringField(body, "return")))
	if returnMode == "" {
		returnMode = s.CollabReturn()
	}
	if returnMode != "card" && returnMode != "none" && returnMode != "inject" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "return 只能是 card / none / inject"})
		return
	}
	if returnMode == "inject" && s.CollabReturn() != "inject" {
		api.WriteJSON(w, http.StatusForbidden, map[string]any{
			"ok": false, "code": "collab_inject_disabled",
			"error": "自动转交子 Agent 结论默认关闭，请先在设置中开启",
		})
		return
	}
	preset := strings.ToLower(strings.TrimSpace(stringField(body, "preset")))
	workspace := strings.ToLower(strings.TrimSpace(stringField(body, "workspace")))

	service := orchestrator.Service{Store: s, Launcher: dispatchLauncher}
	request := orchestrator.Request{
		ParentSessionID: parentSessionID,
		ParentSource:    parentSource,
		ParentEventID:   parentEventID,
		TargetAgent:     stringField(body, "target_agent"),
		Cwd:             cwd,
		Text:            stringField(body, "text"),
		Mode:            stringField(body, "mode"),
		Preset:          preset,
		Workspace:       workspace,
		Return:          returnMode,
	}
	if dryRun, _ := body["dry_run"].(bool); dryRun {
		prepared, prepareErr := service.Prepare(request, nil)
		if prepareErr != nil {
			writeDispatchError(w, prepareErr, store.Dispatch{})
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "dry_run": true, "plan": prepared,
		})
		return
	}
	dispatch, dispatchErr := service.Dispatch(r.Context(), request)
	if dispatchErr != nil {
		writeDispatchError(w, dispatchErr, dispatch)
		return
	}
	publishDispatch(d, dispatch)
	d.Audit(r, "dispatch", map[string]any{
		"dispatch_id":       dispatch.ID,
		"parent_session_id": dispatch.ParentSessionID,
		"target":            dispatch.TargetAgent,
	})
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"dispatch_id":       dispatch.ID,
		"parent_session_id": dispatch.ParentSessionID,
		"parent_source":     dispatch.ParentSource,
		"parent_event_id":   dispatch.ParentEventID,
		"target_agent":      dispatch.TargetAgent,
		"target_session_id": dispatch.TargetSessionID,
		"request_id":        dispatch.RequestID,
		"status":            dispatch.Status,
		"message":           dispatch.Message,
		"mode":              dispatch.Mode,
		"dispatch":          dispatch,
	})
}

func writeDispatchError(w http.ResponseWriter, dispatchErr error, dispatch store.Dispatch) {
	status := http.StatusInternalServerError
	code := "dispatch_failed"
	var typed *orchestrator.Error
	if errors.As(dispatchErr, &typed) {
		status = typed.Status
		code = typed.Code
	}
	payload := map[string]any{
		"ok":    false,
		"error": dispatchErr.Error(),
		"code":  code,
	}
	if dispatch.ID != "" {
		payload["dispatch_id"] = dispatch.ID
		payload["dispatch"] = dispatch
	}
	api.WriteJSON(w, status, payload)
}

func listDispatches(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	query := r.URL.Query()
	parentSessionID := strings.TrimSpace(query.Get("parent_session_id"))
	targetAgent := strings.ToLower(strings.TrimSpace(query.Get("target_agent")))
	status := strings.ToLower(strings.TrimSpace(query.Get("status")))
	limit := 100
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil {
			if parsed < 1 {
				parsed = 1
			}
			if parsed > 500 {
				parsed = 500
			}
			limit = parsed
		}
	}

	out := make([]store.Dispatch, 0)
	for _, dispatch := range s.Dispatches() {
		if parentSessionID != "" && dispatch.ParentSessionID != parentSessionID {
			continue
		}
		if targetAgent != "" && dispatch.TargetAgent != targetAgent {
			continue
		}
		if status != "" && dispatch.Status != status {
			continue
		}
		out = append(out, dispatch)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	if len(out) > limit {
		out = out[:limit]
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"dispatches": out,
		"count":      len(out),
	})
}

func publishDispatch(d *api.Deps, dispatch store.Dispatch) {
	if dispatch.ParentSessionID == "" {
		return
	}
	api.PublishNamed("dispatch", dispatch.ParentSessionID, dispatch)
}

func eventIDField(body map[string]any) (*int64, error) {
	raw, present := body["parent_event_id"]
	if !present || raw == nil {
		return nil, nil
	}
	value, ok := raw.(float64)
	if !ok || value <= 0 || math.Trunc(value) != value {
		return nil, errors.New("parent_event_id 必须是正整数")
	}
	id := int64(value)
	return &id, nil
}
