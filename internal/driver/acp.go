package driver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

const (
	rpcVersion          = "2.0"
	maxProtocolLine     = 16 << 20
	maxStderrTail       = 8 << 10
	shutdownGracePeriod = 1200 * time.Millisecond
)

var (
	ErrPermissionDenied = errors.New("agent requested permission but no interactive approver is attached")
	ErrProcessClosed    = errors.New("agent process closed")
)

// PermissionOption is one ACP permission choice.
type PermissionOption struct {
	OptionID string `json:"option_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// PermissionRequest is an agent's request to run a tool or command.
type PermissionRequest struct {
	SessionID string             `json:"session_id"`
	ToolCall  map[string]any     `json:"tool_call,omitempty"`
	Options   []PermissionOption `json:"options"`
	Raw       map[string]any     `json:"-"`
}

// PermissionDecision selects one option. Cancel answers the protocol with
// "cancelled"; an empty OptionID with Allow=false falls back to reject_once.
type PermissionDecision struct {
	Allow    bool
	OptionID string
	Cancel   bool
}

// Update is one normalised session/update notification.
type Update struct {
	SessionID string         `json:"session_id"`
	Kind      string         `json:"kind"`
	Text      string         `json:"text,omitempty"`
	Raw       map[string]any `json:"-"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("JSON-RPC %d: %s", e.Code, e.Message)
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Client is one ACP agent subprocess. It is safe to use from multiple
// goroutines: writes are serialised and responses are matched by request id.
type Client struct {
	spec Spec
	cwd  string

	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcMessage
	finish  sync.Once
	waitErr error

	stderrMu sync.Mutex
	stderr   bytes.Buffer

	onUpdate     func(Update)
	onPermission func(PermissionRequest) PermissionDecision

	loadSession bool

	permissionMu       sync.Mutex
	pendingPermissions map[string]pendingPermission
}

type pendingPermission struct {
	sessionID string
	requestID json.RawMessage
}

// Start launches the agent process. It does not perform the protocol
// handshake; call Initialize next.
func Start(ctx context.Context, spec Spec, cwd string, onUpdate func(Update), onPermission func(PermissionRequest) PermissionDecision) (*Client, error) {
	if len(spec.Command) == 0 {
		return nil, errors.New("ACP 启动命令为空")
	}
	if strings.TrimSpace(cwd) == "" {
		return nil, errors.New("ACP 工作目录为空")
	}
	binary := platform.ResolveBinary(spec.Command[0])
	if binary == "" {
		return nil, fmt.Errorf("找不到 %s 命令", spec.Command[0])
	}
	args := append([]string(nil), spec.Command[1:]...)
	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = platform.EnvWithAugmentedPATH(spec.Env)
	platform.HideChildWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	client := &Client{
		spec: spec, cwd: cwd, cmd: cmd, stdin: stdin, done: make(chan struct{}),
		pending:            map[int64]chan rpcMessage{},
		onUpdate:           onUpdate,
		onPermission:       onPermission,
		pendingPermissions: map[string]pendingPermission{},
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go client.readStdout(stdout)
	go client.readStderr(stderr)
	go func() {
		err := cmd.Wait()
		client.finishWith(err)
	}()
	select {
	case <-ctx.Done():
		client.Close()
		return nil, ctx.Err()
	default:
		return client, nil
	}
}

// Initialize performs the ACP handshake. We deliberately advertise no file or
// terminal proxying: the agent uses its own tools and Homing only observes the
// protocol.
func (c *Client) Initialize(ctx context.Context) (map[string]any, error) {
	result, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{
				"readTextFile":  false,
				"writeTextFile": false,
			},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "homing", "version": "1"},
	}, 20*time.Second)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if len(result) > 0 {
		_ = json.Unmarshal(result, &out)
	}
	if capabilities, ok := out["agentCapabilities"].(map[string]any); ok {
		value, _ := capabilities["loadSession"].(bool)
		c.mu.Lock()
		c.loadSession = value
		c.mu.Unlock()
	}
	return out, nil
}

// SupportsLoadSession reports the agent capability returned by initialize.
func (c *Client) SupportsLoadSession() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loadSession
}

// NewSession creates a fresh ACP session in cwd.
func (c *Client) NewSession(ctx context.Context, cwd string) (string, map[string]any, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd = c.cwd
	}
	result, err := c.request(ctx, "session/new", map[string]any{
		"cwd": cwd, "mcpServers": []any{},
	}, 30*time.Second)
	if err != nil {
		return "", nil, err
	}
	var out map[string]any
	if len(result) > 0 {
		_ = json.Unmarshal(result, &out)
	}
	sessionID := firstString(out["sessionId"], out["session_id"])
	if sessionID == "" {
		if nested, ok := out["session"].(map[string]any); ok {
			sessionID = firstString(nested["id"], nested["sessionId"])
		}
	}
	if sessionID == "" {
		return "", out, errors.New("session/new 未返回 sessionId")
	}
	return sessionID, out, nil
}

// LoadSession reattaches to an existing session when the agent advertised the
// ACP loadSession capability. The session id stays the agent's own id.
func (c *Client) LoadSession(ctx context.Context, sessionID, cwd string) (map[string]any, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("session/load 缺少 sessionId")
	}
	if !c.SupportsLoadSession() {
		return nil, errors.New("该 Agent 不支持 session/load，不能继续已有会话")
	}
	if strings.TrimSpace(cwd) == "" {
		cwd = c.cwd
	}
	result, err := c.request(ctx, "session/load", map[string]any{
		"sessionId":  sessionID,
		"cwd":        cwd,
		"mcpServers": []any{},
	}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if len(result) > 0 {
		_ = json.Unmarshal(result, &out)
	}
	return out, nil
}

// Prompt sends one text prompt and waits for the turn to finish. Streaming
// updates arrive through the callback passed to Start.
func (c *Client) Prompt(ctx context.Context, sessionID, text string) (map[string]any, error) {
	result, err := c.request(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt": []any{
			map[string]any{"type": "text", "text": text},
		},
	}, 0)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if len(result) > 0 {
		_ = json.Unmarshal(result, &out)
	}
	return out, nil
}

// Cancel asks the agent to stop the current turn. It is a notification, so no
// response is awaited.
func (c *Client) Cancel(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	c.cancelPermissions(sessionID)
	return c.notify("session/cancel", map[string]any{"sessionId": sessionID})
}

// Close asks the agent to exit, then force-kills it after a short grace period.
func (c *Client) Close() {
	c.cancelPermissions("")
	_ = c.stdin.Close()
	select {
	case <-c.done:
		return
	case <-time.After(shutdownGracePeriod):
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// Done closes when the subprocess exits.
func (c *Client) Done() <-chan struct{} { return c.done }

// WaitError is the process exit error, if any.
func (c *Client) WaitError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waitErr
}

// StderrTail is a bounded diagnostic string. It is never included in API
// payloads automatically and callers must redact secrets before logging.
func (c *Client) StderrTail() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	return strings.TrimSpace(c.stderr.String())
}

func (c *Client) request(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	id := atomic.AddInt64(&c.nextID, 1)
	response := make(chan rpcMessage, 1)
	c.mu.Lock()
	if c.waitErr != nil {
		err := c.waitErr
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: %v", ErrProcessClosed, err)
	}
	c.pending[id] = response
	c.mu.Unlock()

	if err := c.write(map[string]any{
		"jsonrpc": rpcVersion, "id": id, "method": method, "params": params,
	}); err != nil {
		c.removePending(id)
		return nil, err
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	select {
	case msg := <-response:
		if msg.Error != nil {
			return nil, msg.Error
		}
		return msg.Result, nil
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-c.done:
		c.removePending(id)
		return nil, ErrProcessClosed
	}
}

func (c *Client) notify(method string, params any) error {
	return c.write(map[string]any{
		"jsonrpc": rpcVersion, "method": method, "params": params,
	})
}

func (c *Client) write(payload any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := c.stdin.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("写入 ACP 失败：%w", err)
	}
	return nil
}

func (c *Client) readStdout(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxProtocolLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			c.appendStderr("协议输出不是 JSON：" + err.Error())
			continue
		}
		if len(msg.ID) > 0 && msg.Method != "" {
			c.handleRequest(msg)
			continue
		}
		if len(msg.ID) > 0 {
			c.handleResponse(msg)
			continue
		}
		if msg.Method != "" {
			c.handleNotification(msg)
		}
	}
	if err := scanner.Err(); err != nil {
		c.appendStderr("读取 ACP 输出失败：" + err.Error())
	}
}

func (c *Client) readStderr(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 8*1024), 256*1024)
	for scanner.Scan() {
		c.appendStderr(scanner.Text())
	}
}

func (c *Client) handleResponse(msg rpcMessage) {
	id, ok := rawInt64(msg.ID)
	if !ok {
		return
	}
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch != nil {
		ch <- msg
	}
}

func (c *Client) handleNotification(msg rpcMessage) {
	if msg.Method != "session/update" {
		return
	}
	var params map[string]any
	if json.Unmarshal(msg.Params, &params) != nil {
		return
	}
	update := parseUpdate(params)
	if update.SessionID == "" {
		return
	}
	if c.onUpdate != nil {
		c.onUpdate(update)
	}
}

func (c *Client) handleRequest(msg rpcMessage) {
	if msg.Method != "session/request_permission" {
		c.writeRPCError(msg.ID, -32601, "client method not implemented: "+msg.Method)
		return
	}
	var params map[string]any
	if json.Unmarshal(msg.Params, &params) != nil {
		c.writeRPCError(msg.ID, -32602, "invalid params")
		return
	}
	request := PermissionRequest{
		SessionID: firstString(params["sessionId"], params["session_id"]),
		Raw:       params,
	}
	if toolCall, ok := params["toolCall"].(map[string]any); ok {
		request.ToolCall = toolCall
	}
	if raw, ok := params["options"].([]any); ok {
		for _, item := range raw {
			option, _ := item.(map[string]any)
			if option == nil {
				continue
			}
			request.Options = append(request.Options, PermissionOption{
				OptionID: firstString(option["optionId"], option["option_id"]),
				Name:     firstString(option["name"]),
				Kind:     firstString(option["kind"]),
			})
		}
	}
	key := permissionKey(msg.ID)
	c.permissionMu.Lock()
	if c.pendingPermissions == nil {
		c.pendingPermissions = map[string]pendingPermission{}
	}
	c.pendingPermissions[key] = pendingPermission{
		sessionID: request.SessionID,
		requestID: append(json.RawMessage(nil), msg.ID...),
	}
	c.permissionMu.Unlock()
	go c.answerPermission(msg.ID, request)
}

func (c *Client) answerPermission(requestID json.RawMessage, request PermissionRequest) {
	decision := PermissionDecision{Cancel: true}
	if c.onPermission != nil {
		decision = c.onPermission(request)
	}
	if decision.Cancel {
		c.respondPermission(requestID, request.SessionID, map[string]any{
			"outcome": map[string]any{"outcome": "cancelled"},
		})
		_ = c.Cancel(request.SessionID)
		return
	}
	if decision.OptionID == "" && decision.Allow {
		decision.OptionID = firstOption(request.Options, "allow_once", "allow_always")
	}
	if decision.OptionID == "" && !decision.Allow {
		decision.OptionID = firstOption(request.Options, "reject_once", "reject_always")
	}
	if decision.OptionID == "" {
		c.respondPermission(requestID, request.SessionID, map[string]any{
			"outcome": map[string]any{"outcome": "cancelled"},
		})
		return
	}
	c.respondPermission(requestID, request.SessionID, map[string]any{
		"outcome": map[string]any{"outcome": "selected", "optionId": decision.OptionID},
	})
}

func permissionKey(id json.RawMessage) string {
	return strings.TrimSpace(string(id))
}

// respondPermission answers at most once. A stop/cancel racing with a late
// phone decision must not emit two JSON-RPC responses for the same request.
func (c *Client) respondPermission(
	requestID json.RawMessage,
	sessionID string,
	result map[string]any,
) {
	key := permissionKey(requestID)
	c.permissionMu.Lock()
	pending, ok := c.pendingPermissions[key]
	if ok && (sessionID == "" || pending.sessionID == sessionID) {
		delete(c.pendingPermissions, key)
	} else {
		ok = false
	}
	c.permissionMu.Unlock()
	if !ok {
		return
	}
	id := pending.requestID
	if len(id) == 0 {
		id = requestID
	}
	_ = c.write(map[string]any{
		"jsonrpc": rpcVersion,
		"id":      json.RawMessage(id),
		"result":  result,
	})
}

// cancelPermissions answers every pending permission request for a session
// with the protocol's cancelled outcome. An empty session id cancels all.
func (c *Client) cancelPermissions(sessionID string) {
	type pendingReply struct {
		id json.RawMessage
	}
	var replies []pendingReply
	c.permissionMu.Lock()
	for key, pending := range c.pendingPermissions {
		if sessionID != "" && pending.sessionID != sessionID {
			continue
		}
		replies = append(replies, pendingReply{id: pending.requestID})
		delete(c.pendingPermissions, key)
	}
	c.permissionMu.Unlock()
	for _, pending := range replies {
		_ = c.write(map[string]any{
			"jsonrpc": rpcVersion,
			"id":      json.RawMessage(pending.id),
			"result": map[string]any{
				"outcome": map[string]any{"outcome": "cancelled"},
			},
		})
	}
}

func (c *Client) writeRPCError(id json.RawMessage, code int, message string) {
	_ = c.write(map[string]any{
		"jsonrpc": rpcVersion, "id": json.RawMessage(id),
		"error": map[string]any{"code": code, "message": message},
	})
}

func (c *Client) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) finishWith(err error) {
	c.finish.Do(func() {
		c.mu.Lock()
		c.waitErr = err
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *Client) appendStderr(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	if c.stderr.Len() > 0 {
		c.stderr.WriteByte('\n')
	}
	c.stderr.WriteString(line)
	if c.stderr.Len() > maxStderrTail {
		raw := c.stderr.Bytes()
		tail := append([]byte(nil), raw[len(raw)-maxStderrTail:]...)
		c.stderr.Reset()
		c.stderr.Write(tail)
	}
}

func parseUpdate(params map[string]any) Update {
	update := Update{
		SessionID: firstString(params["sessionId"], params["session_id"]),
		Raw:       params,
	}
	body := params
	if nested, ok := params["update"].(map[string]any); ok {
		body = nested
	}
	update.Kind = firstString(body["sessionUpdate"], body["session_update"], body["type"], params["type"])
	update.Text = contentText(body["content"])
	if update.Text == "" {
		update.Text = firstString(body["text"])
	}
	return update
}

func contentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		return firstString(typed["text"])
	case []any:
		var b strings.Builder
		for _, item := range typed {
			text := contentText(item)
			if text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(text)
		}
		return b.String()
	}
	return ""
}

func firstString(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return typed
			}
		case fmt.Stringer:
			text := typed.String()
			if strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

func rawInt64(raw json.RawMessage) (int64, bool) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return 0, false
	}
	if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
		return parsed, true
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return 0, false
	}
	if typed, ok := value.(float64); ok {
		return int64(typed), true
	}
	return 0, false
}

func firstOption(options []PermissionOption, kinds ...string) string {
	for _, kind := range kinds {
		for _, option := range options {
			if option.Kind == kind && option.OptionID != "" {
				return option.OptionID
			}
		}
	}
	return ""
}
