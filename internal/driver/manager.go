package driver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

const (
	MaxACPProcesses = 4
	acpIdleLifetime = 10 * time.Minute
	acpItemLimit    = 600
)

var (
	ErrACPProcessLimit = errors.New("ACP 子进程已达上限（4）")
	ErrACPSessionBusy  = errors.New("ACP 会话正在执行上一个回合")
)

// TurnResult is one completed ACP prompt turn.
type TurnResult struct {
	StopReason string
	Text       string
	Error      string
	Cancelled  bool
}

// Observer receives the driver's normalised output. Implementations live in
// relayapi so this package stays independent of HTTP and storage.
type Observer interface {
	Started(sessionID, agentID, cwd string)
	Item(sessionID, agentID string, item agent.Item)
	Turn(sessionID, agentID, state string)
	Permission(ctx context.Context, sessionID, agentID string, request PermissionRequest) PermissionDecision
	Finished(ctx context.Context, sessionID, agentID string, result TurnResult)
}

// ObserverFuncs is convenient for tests and embedders that implement only a
// subset of Observer.
type ObserverFuncs struct {
	OnStarted    func(sessionID, agentID, cwd string)
	OnItem       func(sessionID, agentID string, item agent.Item)
	OnTurn       func(sessionID, agentID, state string)
	OnPermission func(ctx context.Context, sessionID, agentID string, request PermissionRequest) PermissionDecision
	OnFinished   func(ctx context.Context, sessionID, agentID string, result TurnResult)
}

func (o ObserverFuncs) Started(sessionID, agentID, cwd string) {
	if o.OnStarted != nil {
		o.OnStarted(sessionID, agentID, cwd)
	}
}

func (o ObserverFuncs) Item(sessionID, agentID string, item agent.Item) {
	if o.OnItem != nil {
		o.OnItem(sessionID, agentID, item)
	}
}

func (o ObserverFuncs) Turn(sessionID, agentID, state string) {
	if o.OnTurn != nil {
		o.OnTurn(sessionID, agentID, state)
	}
}

func (o ObserverFuncs) Permission(ctx context.Context, sessionID, agentID string, request PermissionRequest) PermissionDecision {
	if o.OnPermission != nil {
		return o.OnPermission(ctx, sessionID, agentID, request)
	}
	return PermissionDecision{Cancel: true}
}

func (o ObserverFuncs) Finished(ctx context.Context, sessionID, agentID string, result TurnResult) {
	if o.OnFinished != nil {
		o.OnFinished(ctx, sessionID, agentID, result)
	}
}

// ManagedSession is one ACP process plus the session it created.
type ManagedSession struct {
	SessionID string
	Agent     string
	Cwd       string

	client *Client
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	busy     bool
	turn     int
	seq      int
	idle     *time.Timer
	items    []agent.Item
	itemByID map[string]int
	commands []SessionCommand
}

// SessionCommand is the small command-palette shape shared with the API.
type SessionCommand struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArgumentHint string `json:"argument_hint,omitempty"`
}

// Manager owns ACP subprocesses so stopping an agent works from the same
// session-id surface as Codex and Claude.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*ManagedSession
	mappers  map[string]*turnMapper
	observer Observer
}

var defaultManager = &Manager{
	sessions: map[string]*ManagedSession{},
	mappers:  map[string]*turnMapper{},
}

// Default returns the process-wide ACP manager.
func Default() *Manager { return defaultManager }

// SetObserver installs the process-wide event receiver.
func (m *Manager) SetObserver(observer Observer) {
	m.mu.Lock()
	m.observer = observer
	m.mu.Unlock()
}

func (m *Manager) getObserver() Observer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.observer
}

// Start launches one fresh session and sends text asynchronously. It returns
// as soon as the ACP session id is known; callers must not hold an HTTP request
// context for the lifetime of the process.
func (m *Manager) Start(ctx context.Context, agentID, cwd, text string, onUpdate func(Update)) (string, error) {
	return m.open(ctx, agentID, cwd, "", text, onUpdate)
}

// Resume reattaches to an existing ACP session through session/load and sends
// one prompt. It returns ErrUnsupportedSession when the agent did not advertise
// the capability, so callers can leave the reply queued instead of guessing.
func (m *Manager) Resume(ctx context.Context, agentID, sessionID, cwd, text string, onUpdate func(Update)) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("ACP 会话 id 为空")
	}
	if m.Has(sessionID) {
		return m.Prompt(sessionID, text)
	}
	got, err := m.open(ctx, agentID, cwd, sessionID, text, onUpdate)
	if err != nil {
		return err
	}
	if got != sessionID {
		m.Stop(got)
		return errors.New("session/load 返回了不同的会话 id")
	}
	return nil
}

func (m *Manager) open(
	ctx context.Context,
	agentID, cwd, existingSessionID, text string,
	onUpdate func(Update),
) (string, error) {
	spec, ok := SpecFor(agentID)
	if !ok || !spec.Dispatch {
		return "", errors.New("该 Agent 没有可用的 ACP 驱动")
	}
	readiness := CheckReadiness(spec)
	if !readiness.Ready {
		return "", fmt.Errorf("%s 的 ACP 驱动不可用：%s", spec.Label, readiness.Detail)
	}
	m.mu.Lock()
	if len(m.sessions) >= MaxACPProcesses {
		m.mu.Unlock()
		return "", ErrACPProcessLimit
	}
	observer := m.observer
	m.mu.Unlock()
	if observer == nil {
		observer = ObserverFuncs{}
	}

	managedCtx, cancel := context.WithCancel(context.Background())
	var session *ManagedSession
	client, err := Start(ctx, spec, cwd, func(update Update) {
		if onUpdate != nil {
			onUpdate(update)
		}
		if session != nil {
			m.handleUpdate(session, update, observer)
		}
	}, func(request PermissionRequest) PermissionDecision {
		return observer.Permission(managedCtx, request.SessionID, spec.ID, request)
	})
	if err != nil {
		cancel()
		MarkReadiness(spec, false, err.Error())
		return "", err
	}
	if _, err := client.Initialize(ctx); err != nil {
		client.Close()
		cancel()
		MarkReadiness(spec, false, client.StderrTail())
		return "", err
	}
	sessionID := existingSessionID
	if sessionID == "" {
		sessionID, _, err = client.NewSession(ctx, cwd)
		if err != nil {
			client.Close()
			cancel()
			MarkReadiness(spec, false, client.StderrTail())
			return "", err
		}
	} else {
		if _, err := client.LoadSession(ctx, sessionID, cwd); err != nil {
			client.Close()
			cancel()
			return "", err
		}
	}
	managed := &ManagedSession{
		SessionID: sessionID,
		Agent:     spec.ID,
		Cwd:       cwd,
		client:    client,
		ctx:       managedCtx,
		cancel:    cancel,
		itemByID:  map[string]int{},
	}
	session = managed
	m.mu.Lock()
	if len(m.sessions) >= MaxACPProcesses {
		m.mu.Unlock()
		client.Close()
		cancel()
		return "", ErrACPProcessLimit
	}
	m.sessions[sessionID] = managed
	m.mu.Unlock()
	MarkReadiness(spec, true, "")
	observer.Started(sessionID, spec.ID, cwd)
	if err := m.prompt(managed, text, observer); err != nil {
		m.Stop(sessionID)
		return "", err
	}
	return sessionID, nil
}

// Prompt sends another user turn to a managed ACP session. It is used by the
// dispatch "ask again" action and by ordinary replies from the phone/console.
func (m *Manager) Prompt(sessionID, text string) error {
	m.mu.Lock()
	managed := m.sessions[strings.TrimSpace(sessionID)]
	observer := m.observer
	m.mu.Unlock()
	if managed == nil {
		return errors.New("ACP 会话不存在或已结束")
	}
	if observer == nil {
		observer = ObserverFuncs{}
	}
	return m.prompt(managed, text, observer)
}

func (m *Manager) prompt(managed *ManagedSession, text string, observer Observer) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("提示词不能为空")
	}
	managed.mu.Lock()
	if managed.busy {
		managed.mu.Unlock()
		return ErrACPSessionBusy
	}
	managed.busy = true
	managed.turn++
	turn := managed.turn
	mapper := newTurnMapper(managed.SessionID, managed.Agent, turn, managed.seq)
	m.mu.Lock()
	if m.mappers == nil {
		m.mappers = map[string]*turnMapper{}
	}
	m.mappers[managed.SessionID] = mapper
	m.mu.Unlock()
	if managed.idle != nil {
		managed.idle.Stop()
		managed.idle = nil
	}
	managed.mu.Unlock()

	observer.Turn(managed.SessionID, managed.Agent, "running")
	go func() {
		result, err := managed.client.Prompt(context.Background(), managed.SessionID, text)
		stopReason := firstNonEmptyString(stringValue(result["stopReason"]), stringValue(result["stop_reason"]))
		turnResult := TurnResult{StopReason: stopReason, Text: mapper.FinalText()}
		if err != nil {
			turnResult.Error = err.Error()
			if managed.ctx.Err() != nil || errors.Is(err, ErrProcessClosed) {
				turnResult.Cancelled = true
			}
		}
		switch stopReason {
		case "refusal", "max_tokens", "max_turn_requests":
			turnResult.Error = firstNonEmptyString(turnResult.Error, "ACP 回合以 "+stopReason+" 结束")
		case "cancelled":
			turnResult.Cancelled = true
		}

		managed.mu.Lock()
		current := mapper
		m.mu.Lock()
		if candidate := m.mappers[managed.SessionID]; candidate != nil {
			current = candidate
		}
		m.mu.Unlock()
		for _, item := range current.Finish() {
			m.storeItemLocked(managed, item)
			observer.Item(managed.SessionID, managed.Agent, item)
		}
		managed.seq = current.Seq()
		managed.busy = false
		managed.mu.Unlock()

		observer.Finished(managed.ctx, managed.SessionID, managed.Agent, turnResult)
		observer.Turn(managed.SessionID, managed.Agent, "idle")

		managed.mu.Lock()
		if managed.ctx.Err() == nil {
			managed.idle = time.AfterFunc(acpIdleLifetime, func() {
				m.Stop(managed.SessionID)
			})
		}
		managed.mu.Unlock()
	}()
	return nil
}

func (m *Manager) handleUpdate(managed *ManagedSession, update Update, observer Observer) {
	body := updateBody(update.Raw)
	if update.Kind == "available_commands_update" {
		managed.mu.Lock()
		managed.commands = parseCommands(body)
		managed.mu.Unlock()
		return
	}
	managed.mu.Lock()
	mapper := m.mapperFor(managed)
	items := mapper.Map(update)
	managed.seq = mapper.Seq()
	for _, item := range items {
		m.storeItemLocked(managed, item)
	}
	managed.mu.Unlock()
	for _, item := range items {
		observer.Item(managed.SessionID, managed.Agent, item)
	}
}

// mappers keeps the chunk aggregation state between update notifications.
// It intentionally lives on Manager rather than in the public session struct.
func (m *Manager) mapperFor(session *ManagedSession) *turnMapper {
	if m.mappers == nil {
		m.mappers = map[string]*turnMapper{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	mapper := m.mappers[session.SessionID]
	if mapper == nil || mapper.turn != session.turn {
		mapper = newTurnMapper(session.SessionID, session.Agent, session.turn, session.seq)
		m.mappers[session.SessionID] = mapper
	}
	return mapper
}

func (m *Manager) storeItemLocked(managed *ManagedSession, item agent.Item) {
	if item.ID == "" {
		return
	}
	if managed.itemByID == nil {
		managed.itemByID = map[string]int{}
	}
	if index, ok := managed.itemByID[item.ID]; ok {
		managed.items[index] = item
		return
	}
	managed.itemByID[item.ID] = len(managed.items)
	managed.items = append(managed.items, item)
	if len(managed.items) > acpItemLimit {
		drop := len(managed.items) - acpItemLimit
		managed.items = append([]agent.Item(nil), managed.items[drop:]...)
		managed.itemByID = map[string]int{}
		for index, kept := range managed.items {
			managed.itemByID[kept.ID] = index
		}
	}
}

// Stop cancels the active turn and closes the subprocess.
func (m *Manager) Stop(sessionID string) bool {
	m.mu.Lock()
	managed := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	if m.mappers != nil {
		delete(m.mappers, sessionID)
	}
	m.mu.Unlock()
	if managed == nil {
		return false
	}
	managed.mu.Lock()
	if managed.idle != nil {
		managed.idle.Stop()
		managed.idle = nil
	}
	managed.mu.Unlock()
	_ = managed.client.Cancel(sessionID)
	managed.cancel()
	managed.client.Close()
	return true
}

// Has reports whether this session is managed by the ACP driver.
func (m *Manager) Has(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[strings.TrimSpace(sessionID)] != nil
}

// Count is the number of live ACP subprocesses.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// CloseAll is used during orderly shutdown and tests.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*ManagedSession, 0, len(m.sessions))
	for _, managed := range m.sessions {
		all = append(all, managed)
	}
	m.sessions = map[string]*ManagedSession{}
	m.mappers = map[string]*turnMapper{}
	m.mu.Unlock()
	for _, managed := range all {
		_ = managed.client.Cancel(managed.SessionID)
		managed.cancel()
		managed.client.Close()
	}
}

// SessionAgent returns the agent id for a managed ACP session.
func (m *Manager) SessionAgent(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if managed := m.sessions[strings.TrimSpace(sessionID)]; managed != nil {
		return managed.Agent
	}
	return ""
}

// SessionCwd returns the working directory of a managed session.
func (m *Manager) SessionCwd(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if managed := m.sessions[strings.TrimSpace(sessionID)]; managed != nil {
		return managed.Cwd
	}
	return ""
}

// Items returns the latest in-memory transcript snapshot for an ACP session.
func (m *Manager) Items(sessionID string, limit int) []agent.Item {
	m.mu.Lock()
	managed := m.sessions[strings.TrimSpace(sessionID)]
	m.mu.Unlock()
	if managed == nil {
		return nil
	}
	managed.mu.Lock()
	defer managed.mu.Unlock()
	if limit <= 0 || limit > len(managed.items) {
		limit = len(managed.items)
	}
	out := make([]agent.Item, limit)
	copy(out, managed.items[len(managed.items)-limit:])
	return out
}

// Commands returns cached ACP available_commands_update entries.
func (m *Manager) Commands(sessionID string) []SessionCommand {
	m.mu.Lock()
	managed := m.sessions[strings.TrimSpace(sessionID)]
	m.mu.Unlock()
	if managed == nil {
		return nil
	}
	managed.mu.Lock()
	defer managed.mu.Unlock()
	return append([]SessionCommand(nil), managed.commands...)
}

func parseCommands(body map[string]any) []SessionCommand {
	value := body["availableCommands"]
	if value == nil {
		value = body["available_commands"]
	}
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]SessionCommand, 0, len(raw))
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		name := firstNonEmptyString(stringValue(entry["name"]), stringValue(entry["command"]))
		if name == "" {
			continue
		}
		out = append(out, SessionCommand{
			Name:         strings.TrimPrefix(name, "/"),
			Description:  firstNonEmptyString(stringValue(entry["description"]), stringValue(entry["title"])),
			ArgumentHint: firstNonEmptyString(stringValue(entry["argumentHint"]), stringValue(entry["argument_hint"])),
		})
	}
	return out
}
