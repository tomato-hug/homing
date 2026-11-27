// Package orchestrator creates cross-agent child tasks without moving the
// parent conversation. It validates the request, records the relationship, and
// delegates the actual process launch to a Launcher supplied by the relay.
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tomato-hug/homing/internal/store"
)

const MaxTaskRunes = 12000

type storeDispatchLock struct {
	mu   sync.Mutex
	refs int
}

var storeDispatchLocks = struct {
	mu    sync.Mutex
	locks map[string]*storeDispatchLock
}{locks: map[string]*storeDispatchLock{}}

func lockStoreDispatches(st *store.Store) func() {
	key := filepath.Clean(st.Paths.DataDir)
	if key == "" || key == "." {
		key = fmt.Sprintf("%p", st)
	}
	storeDispatchLocks.mu.Lock()
	entry := storeDispatchLocks.locks[key]
	if entry == nil {
		entry = &storeDispatchLock{}
		storeDispatchLocks.locks[key] = entry
	}
	entry.refs++
	storeDispatchLocks.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		storeDispatchLocks.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(storeDispatchLocks.locks, key)
		}
		storeDispatchLocks.mu.Unlock()
	}
}

// StartResult is what an agent-specific session launcher reports.
type StartResult struct {
	SessionID string
	RequestID string
	Status    string
	Message   string
}

// StartOptions is the provider-neutral launch description for a child task.
// Preset and Workspace are kept separate so the launcher can choose a
// read-only permission mode without changing the user's task text.
type StartOptions struct {
	Target    string
	Cwd       string
	Text      string
	Preset    string
	Workspace string
}

// Launcher starts a fresh target-agent session. Implementations must not
// pretend success when the executable or service is unavailable.
type Launcher interface {
	CanDispatch(target string) bool
	Start(ctx context.Context, options StartOptions) StartResult
}

// Request is the validated hand-off described by the phone.
type Request struct {
	ParentSessionID string
	ParentSource    string
	ParentEventID   *int64
	TargetAgent     string
	Cwd             string
	Text            string
	Mode            string
	Preset          string
	Workspace       string
	Thread          string
	Branch          string
	BaseCommit      string
	RepoCwd         string
	Return          string
	// Prompt overrides what the child process receives. Text stays the user's
	// task as shown in the card; Prompt adds the context pack.
	Prompt string
}

// Error carries the HTTP-facing reason and, when available, the failed record
// so the phone can still show the dispatch card.
type Error struct {
	Status   int
	Code     string
	Message  string
	Dispatch *store.Dispatch
}

func (e *Error) Error() string { return e.Message }

// Service is the application service behind POST /api/dispatch.
type Service struct {
	Store    *store.Store
	Launcher Launcher
	NewID    func() string
}

// Prepare validates one child task and returns the record that Dispatch would
// create. It has no side effects. Compose uses it to validate every mention
// before sending the parent half of a multi-action request.
func (s *Service) Prepare(req Request, extra []store.Dispatch) (store.Dispatch, error) {
	return s.prepare(req, extra)
}

// Dispatch validates, records, and starts one child task.
func (s *Service) Dispatch(ctx context.Context, req Request) (store.Dispatch, error) {
	if s == nil || s.Store == nil {
		return store.Dispatch{}, &Error{Status: 500, Code: "store_unavailable", Message: "数据目录不可用"}
	}
	if s.Launcher == nil {
		return store.Dispatch{}, &Error{Status: 503, Code: "dispatcher_unavailable", Message: "跨 Agent 调度器未启用"}
	}

	unlockReserve := lockStoreDispatches(s.Store)
	dispatch, err := s.prepare(req, nil)
	if err != nil {
		unlockReserve()
		return store.Dispatch{}, err
	}
	saved, err := s.Store.SaveDispatch(dispatch)
	unlockReserve()
	if err != nil {
		return saved, &Error{Status: 500, Code: "dispatch_store_failed", Message: "保存调度记录失败", Dispatch: &saved}
	}

	launchText := strings.TrimSpace(req.Prompt)
	if launchText == "" {
		launchText = dispatch.Text
	}
	if utf8.RuneCountInString(launchText) > MaxTaskRunes {
		launchText = truncateRunes(launchText, MaxTaskRunes)
	}
	result := s.Launcher.Start(ctx, StartOptions{
		Target:    dispatch.TargetAgent,
		Cwd:       dispatch.Cwd,
		Text:      launchText,
		Preset:    dispatch.Preset,
		Workspace: dispatch.Workspace,
	})
	if result.Status == "" {
		result.Status = "started"
	}

	unlockFinalize := lockStoreDispatches(s.Store)
	defer unlockFinalize()

	latest := saved
	for _, existing := range s.Store.Dispatches() {
		if existing.ID == saved.ID {
			latest = existing
			break
		}
	}
	switch latest.Status {
	case "cancelled", "completed", "failed":
		return latest, nil
	}

	sessionID := strings.TrimSpace(result.SessionID)
	requestID := strings.TrimSpace(result.RequestID)
	message := strings.TrimSpace(result.Message)

	if latest.TargetSessionID == "" && sessionID != "" {
		latest.TargetSessionID = sessionID
	}
	if latest.TargetSessionID != "" {
		latest.RequestID = ""
		if latest.Status == "starting" || latest.Status == "waiting_session" {
			latest.Status = "started"
		}
	} else if requestID != "" {
		latest.RequestID = requestID
		if latest.Status == "starting" {
			latest.Status = "waiting_session"
		}
	} else if latest.Status == "starting" || latest.Status == "waiting_session" {
		latest.Status = normalizeStatus(result.Status)
	}

	if message != "" && (latest.Message == "" || latest.Status == "failed") {
		latest.Message = message
	}
	if latest.Status == "failed" {
		if latest.Message == "" {
			latest.Message = "目标会话启动失败"
		}
		latest, _ = s.Store.SaveDispatch(latest)
		return latest, &Error{Status: 502, Code: "launch_failed", Message: latest.Message, Dispatch: &latest}
	}
	if latest.Preset == "handoff" && strings.TrimSpace(latest.Message) == "" {
		latest.Message = "已移交給 @" + latest.TargetAgent + "，結果不會自動回到父會話"
	}
	latest, err = s.Store.SaveDispatch(latest)
	if err != nil {
		return latest, &Error{Status: 500, Code: "dispatch_store_failed", Message: "保存调度状态失败", Dispatch: &latest}
	}
	return latest, nil
}

func (s *Service) prepare(req Request, extra []store.Dispatch) (store.Dispatch, error) {
	if s == nil || s.Store == nil {
		return store.Dispatch{}, &Error{Status: 500, Code: "store_unavailable", Message: "数据目录不可用"}
	}
	if s.Launcher == nil {
		return store.Dispatch{}, &Error{Status: 503, Code: "dispatcher_unavailable", Message: "跨 Agent 调度器未启用"}
	}

	target := strings.ToLower(strings.TrimSpace(req.TargetAgent))
	if target == "" {
		return store.Dispatch{}, &Error{Status: 400, Code: "target_required", Message: "target_agent 不能为空"}
	}
	if !s.Launcher.CanDispatch(target) {
		return store.Dispatch{}, &Error{
			Status:  409,
			Code:    "agent_not_dispatchable",
			Message: "该 Agent 尚未提供新会话启动器，不能跨 Agent 调度",
		}
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		return store.Dispatch{}, &Error{Status: 400, Code: "text_required", Message: "调度任务不能为空"}
	}
	if utf8.RuneCountInString(text) > MaxTaskRunes {
		return store.Dispatch{}, &Error{Status: 413, Code: "text_too_long", Message: "调度任务过长"}
	}

	parentSession := strings.TrimSpace(req.ParentSessionID)
	if parentSession == "" {
		return store.Dispatch{}, &Error{
			Status:  400,
			Code:    "parent_required",
			Message: "parent_session_id 或 parent_event_id 必须提供一个",
		}
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "parallel"
	}
	if mode != "parallel" {
		return store.Dispatch{}, &Error{Status: 400, Code: "mode_unsupported", Message: "首版只支持 parallel 模式"}
	}
	preset := strings.ToLower(strings.TrimSpace(req.Preset))
	if preset == "" {
		if mode == "parallel" {
			preset = "do"
		} else {
			preset = "ask"
		}
	}
	if !Presets[preset] {
		return store.Dispatch{}, &Error{Status: 400, Code: "preset_unsupported", Message: "未知协作预设"}
	}
	workspace := strings.ToLower(strings.TrimSpace(req.Workspace))
	if workspace == "" {
		workspace = "auto"
	}
	switch workspace {
	case "auto", "shared", "readonly", "worktree":
	default:
		return store.Dispatch{}, &Error{Status: 400, Code: "workspace_unsupported", Message: "未知工作区模式"}
	}
	if strings.TrimSpace(req.Cwd) == "" {
		return store.Dispatch{}, &Error{Status: 400, Code: "cwd_required", Message: "cwd 不能为空"}
	}
	cwd, err := filepath.Abs(strings.TrimSpace(req.Cwd))
	if err != nil {
		return store.Dispatch{}, &Error{Status: 400, Code: "cwd_invalid", Message: "cwd 不存在"}
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return store.Dispatch{}, &Error{Status: 400, Code: "cwd_invalid", Message: "cwd 不是可用目录"}
	}
	if err := s.checkCollabLimits(dispatchLimitsRequest{
		ParentSessionID: parentSession,
		Cwd:             cwd,
		Preset:          preset,
		Workspace:       workspace,
		Continuation:    strings.TrimSpace(req.Thread) != "",
	}, extra); err != nil {
		return store.Dispatch{}, err
	}
	returnMode := normalizeReturn(req.Return)
	if preset == "handoff" {
		// A handoff transfers ownership to the target. Returning a result card,
		// and especially auto-injecting it, would undo that transfer and could
		// feed one agent's output back into the original conversation.
		returnMode = "none"
	}

	return store.Dispatch{
		ID:              s.newID(),
		ParentSessionID: parentSession,
		ParentSource:    strings.TrimSpace(req.ParentSource),
		ParentEventID:   req.ParentEventID,
		TargetAgent:     target,
		Text:            text,
		Cwd:             cwd,
		Mode:            mode,
		Preset:          preset,
		Workspace:       workspace,
		Thread:          strings.TrimSpace(req.Thread),
		Branch:          strings.TrimSpace(req.Branch),
		BaseCommit:      strings.TrimSpace(req.BaseCommit),
		RepoCwd:         strings.TrimSpace(req.RepoCwd),
		Return:          returnMode,
		Status:          "starting",
	}, nil
}

type dispatchLimitsRequest struct {
	ParentSessionID string
	Cwd             string
	Preset          string
	Workspace       string
	Continuation    bool
}

func (s *Service) checkCollabLimits(req dispatchLimitsRequest, extra []store.Dispatch) error {
	if req.Continuation {
		return nil
	}
	active := func(status string) bool {
		switch status {
		case "starting", "waiting_session", "started", "running", "needs_you":
			return true
		}
		return false
	}
	parentChildren := 0
	sharedWriters := 0
	records := append([]store.Dispatch(nil), s.Store.Dispatches()...)
	records = append(records, extra...)
	for _, existing := range records {
		if existing.TargetSessionID == req.ParentSessionID && existing.Status != "cancelled" {
			return &Error{
				Status: 429, Code: "nested_dispatch_unsupported",
				Message: "子任務不能再次派發協作任務；請回到父會話繼續",
			}
		}
		if existing.ParentSessionID != req.ParentSessionID || !active(existing.Status) {
			continue
		}
		parentChildren++
		if existing.Cwd == req.Cwd && dispatchWritesShared(existing.Workspace, existing.Preset) {
			sharedWriters++
		}
	}
	if parentChildren >= 5 {
		return &Error{
			Status: 429, Code: "parent_dispatch_limit",
			Message: "每个父会话最多同时有 5 个协作子任务",
		}
	}
	if dispatchWritesShared(req.Workspace, req.Preset) && sharedWriters >= 2 {
		return &Error{
			Status: 429, Code: "cwd_dispatch_limit",
			Message: "同一目录最多同时有 2 个可写协作子任务；请改用 worktree 或等待完成",
		}
	}
	return nil
}

func dispatchWritesShared(workspace, preset string) bool {
	switch preset {
	case "ask", "review":
		return false
	}
	switch workspace {
	case "shared", "auto":
		return true
	}
	return false
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("dsp_%d", time.Now().UnixNano())
	}
	return "dsp_" + hex.EncodeToString(buf)
}

func normalizeStatus(status string) string {
	switch status {
	case "started", "waiting_session", "failed":
		return status
	default:
		return "started"
	}
}

func normalizeReturn(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none", "inject":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "card"
	}
}
