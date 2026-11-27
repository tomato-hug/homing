package relayapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/driver"
	filepaths "github.com/tomato-hug/homing/internal/files"
	"github.com/tomato-hug/homing/internal/store"
)

const (
	pendingLaunchTTL   = 10 * time.Minute
	maxPendingLaunches = 128
)

type launchInfo struct {
	source string
	cwd    string
	at     time.Time
}

var launches = struct {
	mu      sync.Mutex
	pending map[string]launchInfo
	procs   map[string]*exec.Cmd
	acp     map[string]acpSession
}{
	pending: map[string]launchInfo{},
	procs:   map[string]*exec.Cmd{},
	acp:     map[string]acpSession{},
}

func recordPendingLaunch(requestID, source, cwd string, now time.Time) {
	launches.mu.Lock()
	defer launches.mu.Unlock()
	prunePendingLaunchesLocked(now)
	launches.pending[requestID] = launchInfo{source: source, cwd: cwd, at: now}
	capPendingLaunchesLocked()
}

func prunePendingLaunchesLocked(now time.Time) {
	for id, info := range launches.pending {
		if now.Sub(info.at) > pendingLaunchTTL {
			delete(launches.pending, id)
		}
	}
	capPendingLaunchesLocked()
}

func capPendingLaunchesLocked() {
	for len(launches.pending) > maxPendingLaunches {
		oldestID := ""
		var oldestAt time.Time
		for id, info := range launches.pending {
			if oldestID == "" || info.at.Before(oldestAt) {
				oldestID = id
				oldestAt = info.at
			}
		}
		if oldestID == "" {
			break
		}
		delete(launches.pending, oldestID)
	}
}

func newSession(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	if !d.CheckRate(w, r, "sessions_new", 20, time.Minute) {
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	source := strings.ToLower(strings.TrimSpace(stringField(body, "source")))
	if !validHookSource(source) {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "source 只能是已接入的 agent id"})
		return
	}
	target, exists := agent.Get(r.Context(), source)
	if !exists {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "未知 Agent"})
		return
	}
	if !effectiveCaps(target).NewSession {
		message := "这个 Agent 目前只能读取或回复，还没有可安全新建独立会话的驱动"
		api.WriteJSON(w, http.StatusConflict, map[string]any{
			"ok": false, "error": message, "message": message,
			"code": "agent_not_driveable",
		})
		return
	}
	cwd := strings.TrimSpace(stringField(body, "cwd"))
	if cwd == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cwd 不能为空"})
		return
	}
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cwd 不存在"})
		return
	}
	info, err := os.Stat(absolute)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cwd 不存在"})
		return
	}
	if !info.IsDir() {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cwd 不是目录"})
		return
	}
	text := strings.TrimSpace(stringField(body, "text"))
	if text == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "text 不能为空"})
		return
	}
	files := filepaths.NormalizeFiles(stringList(body["files"]))
	finalText := filepaths.DeliveryText(text, files)
	model := truncateRunes(strings.TrimSpace(stringField(body, "model")), 80)
	effort := truncateRunes(strings.TrimSpace(stringField(body, "effort")), 32)
	permissionMode := truncateRunes(
		strings.ToLower(strings.TrimSpace(stringField(body, "permission_mode"))), 32,
	)
	if !api.PermissionModeAllowed(source, permissionMode) {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": "permission_mode 不是该 Agent 支持的模式",
			"code": "permission_mode_unsupported",
		})
		return
	}

	started, startErr := startAgentSession(r.Context(), target, agent.StartRequest{
		Cwd:            absolute,
		Text:           finalText,
		Files:          files,
		Model:          model,
		Effort:         effort,
		PermissionMode: permissionMode,
		Origin:         "phone",
	})
	if startErr != nil {
		status := http.StatusBadGateway
		code := "agent_start_failed"
		switch {
		case errors.Is(startErr, driver.ErrACPProcessLimit):
			status = http.StatusTooManyRequests
			code = "acp_process_limit"
		case errors.Is(startErr, agent.ErrUnsupported):
			status = http.StatusConflict
			code = "agent_not_driveable"
		}
		api.WriteJSON(w, status, map[string]any{
			"ok": false, "error": startErr.Error(), "code": code,
		})
		return
	}
	result := map[string]any{"ok": true, "status": firstNonEmpty(started.Status, "started")}
	if started.SessionID != "" {
		result["session_id"] = started.SessionID
	} else {
		result["session_id"] = nil
	}
	if started.RequestID != "" {
		result["request_id"] = started.RequestID
	}
	if started.Message != "" {
		result["warning"] = started.Message
	}
	result["cwd"] = absolute
	if ok, _ := result["ok"].(bool); ok {
		d.Audit(r, "new_session", map[string]any{
			"target":     source,
			"session_id": stringField(result, "session_id"),
			"cwd":        absolute,
		})
	}
	ok, _ := result["ok"].(bool)
	if ok && stringField(result, "session_id") == "" && stringField(result, "request_id") == "" {
		requestID := randomHex(8)
		recordPendingLaunch(requestID, source, absolute, time.Now())
		result["request_id"] = requestID
	}
	status := http.StatusOK
	if !ok {
		status = http.StatusNotImplemented
	}
	api.WriteJSON(w, status, result)
}

func stopSession(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if !requireDrive(d, w, r) {
		return
	}
	sessionID := r.PathValue("id")
	if sessionID == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "session_id 不能为空"})
		return
	}
	d.Audit(r, "stop_session", map[string]any{"session_id": sessionID})
	if via, stopped := stopRegisteredSession(r.Context(), d, sessionID); stopped {
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "session_id": sessionID, "via": via,
		})
		return
	}
	launches.mu.Lock()
	cmd := launches.procs[sessionID]
	if cmd != nil {
		delete(launches.procs, sessionID)
	}
	acp, hasACP := launches.acp[sessionID]
	if hasACP {
		delete(launches.acp, sessionID)
	}
	launches.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Process.Kill()
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID, "via": "codex-exec"})
		return
	}
	if driver.Default().Stop(sessionID) {
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID, "via": "acp"})
		return
	}
	if hasACP {
		acpFire(acp.port, acp.connectionID, map[string]any{
			"jsonrpc": "2.0", "id": 9, "method": "session/cancel",
			"params": map[string]any{"sessionId": sessionID},
		})
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID, "via": "acp-cancel"})
		return
	}
	api.WriteJSON(w, http.StatusConflict, map[string]any{
		"ok": false, "error": "unsupported", "message": "该会话不是由中继启动的进程，无法停止",
	})
}

// stopManagedSession is the shared process/cancel path used by dispatch cancel.
// It reports true when a process or ACP session was actually stopped.
func stopManagedSession(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	if _, stopped := stopRegisteredSession(context.Background(), nil, sessionID); stopped {
		return true
	}
	launches.mu.Lock()
	cmd := launches.procs[sessionID]
	if cmd != nil {
		delete(launches.procs, sessionID)
	}
	acp, hasACP := launches.acp[sessionID]
	if hasACP {
		delete(launches.acp, sessionID)
	}
	launches.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Process.Kill()
		return true
	}
	if driver.Default().Stop(sessionID) {
		return true
	}
	if hasACP {
		acpFire(acp.port, acp.connectionID, map[string]any{
			"jsonrpc": "2.0", "id": 9, "method": "session/cancel",
			"params": map[string]any{"sessionId": sessionID},
		})
		return true
	}
	return false
}

// stopRegisteredSession asks the owning Agent to stop the session. The API
// passes Deps so it can resolve the source cheaply; dispatch cancellation has
// no Deps and falls back to trying every registered Stopper.
func stopRegisteredSession(ctx context.Context, d *api.Deps, sessionID string) (string, bool) {
	if d != nil {
		if source := d.SourceFor(ctx, sessionID); source != "" {
			if a, ok := agent.Get(ctx, source); ok {
				if stopper, supported := a.(agent.Stopper); supported {
					if err := stopper.StopSession(ctx, sessionID); err == nil {
						return a.Name(), true
					}
				}
			}
		}
	}
	for _, a := range agent.All(ctx) {
		stopper, supported := a.(agent.Stopper)
		if !supported {
			continue
		}
		if err := stopper.StopSession(ctx, sessionID); err == nil {
			return a.Name(), true
		}
	}
	return "", false
}

func startCodexSession(cwd, text string) map[string]any {
	return startCodexSessionWithOptions(cwd, text, "", "", "")
}

func startCodexSessionWithOptions(cwd, text, model, effort, permissionMode string) map[string]any {
	binary := resolveBinary("codex")
	if binary == "" {
		return map[string]any{"ok": false, "error": "本机没有 codex 命令"}
	}
	args := []string{"exec", "--json", "--skip-git-repo-check", "-C", cwd}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "-c", `model_reasoning_effort="`+effort+`"`)
	}
	if permissionMode == "read_only" || permissionMode == "plan" {
		args = append(args, "--sandbox", "read-only")
	}
	args = append(args, text)
	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return map[string]any{"ok": false, "error": "无法读取 codex 输出：" + err.Error()}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return map[string]any{"ok": false, "error": "无法启动 codex：" + err.Error()}
	}

	idCh := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(line), &payload) != nil {
				continue
			}
			if stringField(payload, "type") != "thread.started" {
				continue
			}
			if id := firstNonEmpty(stringField(payload, "thread_id"), stringField(payload, "thread-id")); id != "" {
				select {
				case idCh <- id:
				default:
				}
				launches.mu.Lock()
				launches.procs[id] = cmd
				launches.mu.Unlock()
			}
		}
	}()
	go func() {
		done <- cmd.Wait()
		launches.mu.Lock()
		for id, candidate := range launches.procs {
			if candidate == cmd {
				delete(launches.procs, id)
			}
		}
		launches.mu.Unlock()
	}()

	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()
	select {
	case id := <-idCh:
		return map[string]any{"ok": true, "session_id": id, "status": "started"}
	case err := <-done:
		message := strings.TrimSpace(stderr.String())
		if message == "" && err != nil {
			message = err.Error()
		}
		if message == "" {
			message = "codex 未返回 thread id"
		}
		return map[string]any{"ok": false, "error": truncateRunes(message, 300)}
	case <-timer.C:
		_ = cmd.Process.Kill()
		return map[string]any{"ok": false, "error": "启动 Codex 会话超时（未收到 thread id）"}
	}
}

func startClaudeSession(cwd, text string) map[string]any {
	return startClaudeSessionWithOptions(cwd, text, "", "", "")
}

func startClaudeSessionWithOptions(cwd, text, model, effort, permissionMode string) map[string]any {
	binary := resolveBinary("claude")
	if binary == "" {
		return map[string]any{"ok": false, "error": "本机没有 claude 命令"}
	}
	sessionID := ""
	args := []string{"-p"}
	if claudeSupportsSessionID(binary) {
		sessionID = randomUUID()
		args = append(args, "--session-id", sessionID)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	switch permissionMode {
	case "plan", "read_only":
		args = append(args, "--permission-mode", "plan")
	case "accept_edits", "accept-edits":
		args = append(args, "--permission-mode", "acceptEdits")
	case "auto":
		args = append(args, "--permission-mode", "auto")
	}
	args = append(args, text)
	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	if sessionID != "" {
		launches.mu.Lock()
		launches.procs[sessionID] = cmd
		launches.mu.Unlock()
	}
	go func(id string, proc *exec.Cmd) {
		_ = proc.Wait()
		if id == "" {
			return
		}
		launches.mu.Lock()
		if launches.procs[id] == proc {
			delete(launches.procs, id)
		}
		launches.mu.Unlock()
	}(sessionID, cmd)
	if sessionID != "" {
		return map[string]any{"ok": true, "session_id": sessionID, "status": "started"}
	}
	return map[string]any{"ok": true, "session_id": nil, "status": "started"}
}

func configuredClaudeHelpTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("CLAUDE_HELP_TIMEOUT_MS"))
	if raw == "" {
		return 3 * time.Second
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 3 * time.Second
	}
	return time.Duration(value) * time.Millisecond
}

func claudeSupportsSessionID(binary string) bool {
	if cached, ok := claudeSessionIDSupport.Load(binary); ok {
		return cached.(bool)
	}
	ctx, cancel := context.WithTimeout(context.Background(), configuredClaudeHelpTimeout())
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "--help").CombinedOutput()
	if err != nil {
		return false
	}
	supported := strings.Contains(string(out), "--session-id")
	actual, _ := claudeSessionIDSupport.LoadOrStore(binary, supported)
	return actual.(bool)
}

var claudeSessionIDSupport sync.Map

func claimLaunch(d *api.Deps, event store.Event) {
	requestID := ""
	launches.mu.Lock()
	prunePendingLaunchesLocked(time.Now())
	var oldest time.Time
	for id, launch := range launches.pending {
		if launch.source != event.Source {
			continue
		}
		eventCwd, _ := filepath.Abs(event.Cwd)
		launchCwd, _ := filepath.Abs(launch.cwd)
		if eventCwd != launchCwd {
			continue
		}
		if requestID != "" && !launch.at.Before(oldest) {
			continue
		}
		requestID = id
		oldest = launch.at
	}
	if requestID != "" {
		delete(launches.pending, requestID)
	}
	launches.mu.Unlock()
	if requestID == "" {
		return
	}
	bindDispatch(d, requestID, event)
	go func() {
		time.Sleep(300 * time.Millisecond)
		payload := map[string]any{
			"session_id": event.SessionID,
			"source":     event.Source,
			"cwd":        event.Cwd,
			"request_id": requestID,
		}
		if summary, ok := d.FindSummary(context.Background(), event.SessionID); ok {
			if raw, err := json.Marshal(summary); err == nil {
				_ = json.Unmarshal(raw, &payload)
			}
			payload["request_id"] = requestID
		}
		api.PublishNamed("session", event.SessionID, payload)
	}()
}

func bindDispatch(d *api.Deps, requestID string, event store.Event) {
	s, err := d.Store()
	if err != nil {
		return
	}
	dispatch, ok := s.BindDispatchTarget(requestID, event.Source, event.SessionID)
	if !ok {
		return
	}
	publishDispatch(d, dispatch)
}

func resolveBinary(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".npm-global", "bin", name),
		filepath.Join(home, ".local", "bin", name),
		filepath.Join("/opt/homebrew/bin", name),
		filepath.Join("/usr/local/bin", name),
		filepath.Join("/usr/bin", name),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

func stringList(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return out
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

func randomUUID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	raw := hex.EncodeToString(buf[:])
	return raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:32]
}

// ---- WorkBuddy ACP -------------------------------------------------------

type acpSession struct {
	port         int
	connectionID string
}

type acpResult struct {
	ok        bool
	result    map[string]any
	sessionID string
	err       string
}

func startWorkbuddySession(cwd, text string) map[string]any {
	ports := workbuddyACPPorts()
	if len(ports) == 0 {
		return map[string]any{"ok": false, "error": "WorkBuddy 未运行，没有可用的 ACP 端口"}
	}
	for _, port := range ports {
		status, body := postJSON(port, "/api/v1/acp/connect", map[string]any{
			"clientInfo": map[string]any{"name": "homing", "version": "0.1"},
		}, nil, 3*time.Second)
		if status != http.StatusOK {
			continue
		}
		var connected map[string]any
		if json.Unmarshal(body, &connected) != nil {
			continue
		}
		connectionID := stringField(connected, "connectionId")
		if connectionID == "" {
			continue
		}
		initialized := acpCall(port, connectionID, map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]any{
				"protocolVersion": 1,
				"clientInfo":      map[string]any{"name": "homing", "version": "0.1"},
			},
		}, 8*time.Second, true)
		if !initialized.ok {
			continue
		}
		created := acpSessionNew(port, connectionID, cwd, 25*time.Second)
		if !created.ok || created.sessionID == "" {
			continue
		}
		launches.mu.Lock()
		launches.acp[created.sessionID] = acpSession{port: port, connectionID: connectionID}
		launches.mu.Unlock()
		acpFire(port, connectionID, map[string]any{
			"jsonrpc": "2.0", "id": 3, "method": "session/prompt",
			"params": map[string]any{
				"sessionId": created.sessionID,
				"prompt":    []any{map[string]any{"type": "text", "text": text}},
			},
		})
		return map[string]any{
			"ok": true, "session_id": created.sessionID, "status": "reused_active",
			"warning": "WorkBuddy 当前版本不能新建会话，已发到当前活跃会话", "port": port,
		}
	}
	return map[string]any{"ok": false, "error": "未能建立 WorkBuddy ACP 连接"}
}

func workbuddyACPPorts() []int {
	pgrep := exec.Command("pgrep", "-f", "WorkBuddy.app/Contents/MacOS/Electron")
	rawPids, err := pgrep.Output()
	if err != nil || len(bytes.TrimSpace(rawPids)) == 0 {
		return nil
	}
	pids := strings.Join(strings.Fields(string(rawPids)), ",")
	lsof := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-a", "-p", pids)
	output, err := lsof.Output()
	if err != nil {
		return nil
	}
	pattern := regexp.MustCompile(`127\.0\.0\.1:(\d+) \(LISTEN\)`)
	seen := map[int]bool{}
	var ports []int
	for _, line := range strings.Split(string(output), "\n") {
		match := pattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		port, err := strconv.Atoi(match[1])
		if err != nil || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

func postJSON(port int, path string, payload any, headers map[string]string, timeout time.Duration) (int, []byte) {
	raw, _ := json.Marshal(payload)
	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), bytes.NewReader(raw))
	if err != nil {
		return 0, nil
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	return response.StatusCode, body
}

func acpCall(port int, connectionID string, payload any, timeout time.Duration, waitForID bool) acpResult {
	id, _ := payload.(map[string]any)["id"].(float64)
	result := acpResult{err: "ACP 连接结束"}
	err := streamACP(port, connectionID, payload, timeout, func(message map[string]any) bool {
		if waitForID {
			if messageID, _ := message["id"].(float64); messageID == id {
				if body, ok := message["result"].(map[string]any); ok {
					result = acpResult{ok: true, result: body}
				} else {
					result = acpResult{err: acpError(message)}
				}
				return true
			}
		} else if stringField(message, "method") == "session/update" {
			params, _ := message["params"].(map[string]any)
			if sessionID := stringField(params, "sessionId"); sessionID != "" {
				result = acpResult{ok: true, sessionID: sessionID}
				return true
			}
		}
		return false
	})
	if err != nil {
		return acpResult{err: err.Error()}
	}
	return result
}

func acpSessionNew(port int, connectionID, cwd string, timeout time.Duration) acpResult {
	payload := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "session/new",
		"params": map[string]any{"cwd": cwd, "mcpServers": []any{}},
	}
	lastUpdate := ""
	result := acpResult{err: "ACP 连接结束"}
	err := streamACP(port, connectionID, payload, timeout, func(message map[string]any) bool {
		if stringField(message, "method") == "session/update" {
			params, _ := message["params"].(map[string]any)
			if sessionID := stringField(params, "sessionId"); sessionID != "" {
				lastUpdate = sessionID
			}
		}
		if messageID, _ := message["id"].(float64); messageID != 2 {
			return false
		}
		body, _ := message["result"].(map[string]any)
		if body == nil {
			result = acpResult{err: acpError(message)}
			return true
		}
		sessionID := firstNonEmpty(
			stringField(body, "sessionId"),
			stringField(body, "session_id"),
		)
		if nested, ok := body["session"].(map[string]any); ok {
			sessionID = firstNonEmpty(sessionID, stringField(nested, "id"))
		}
		sessionID = firstNonEmpty(sessionID, lastUpdate)
		if sessionID == "" {
			result = acpResult{err: "session/new 未返回 sessionId"}
			return true
		}
		result = acpResult{ok: true, sessionID: sessionID}
		return true
	})
	if err != nil && result.err == "ACP 连接结束" {
		result.err = err.Error()
	}
	if !result.ok && lastUpdate != "" {
		return acpResult{ok: true, sessionID: lastUpdate}
	}
	return result
}

func acpFire(port int, connectionID string, payload any) {
	go func() {
		raw, _ := json.Marshal(payload)
		request, err := http.NewRequest(http.MethodPost,
			fmt.Sprintf("http://127.0.0.1:%d/api/v1/acp", port), bytes.NewReader(raw))
		if err != nil {
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Acp-Connection-Id", connectionID)
		client := &http.Client{Timeout: 2500 * time.Millisecond}
		response, err := client.Do(request)
		if err != nil {
			return
		}
		defer response.Body.Close()
		_, _ = io.Copy(io.Discard, response.Body)
	}()
}

func streamACP(
	port int,
	connectionID string,
	payload any,
	timeout time.Duration,
	onMessage func(map[string]any) bool,
) error {
	raw, _ := json.Marshal(payload)
	request, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/api/v1/acp", port), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Acp-Connection-Id", connectionID)
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("ACP HTTP %d", response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var message map[string]any
		if json.Unmarshal([]byte(line), &message) != nil {
			continue
		}
		if onMessage(message) {
			return nil
		}
	}
	return scanner.Err()
}

func acpError(message map[string]any) string {
	raw, _ := message["error"].(map[string]any)
	if text := stringField(raw, "message"); text != "" {
		return text
	}
	return "ACP error"
}
