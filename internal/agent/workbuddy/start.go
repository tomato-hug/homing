package workbuddy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

type managedACP struct {
	port         int
	connectionID string
}

var managed = struct {
	sync.Mutex
	byID map[string]managedACP
}{byID: map[string]managedACP{}}

// StartSession asks the running WorkBuddy desktop bridge for a session. Its
// current bridge reuses the active conversation; the warning is carried in
// StartResult.Message so callers can surface that limitation instead of
// pretending a fresh session was created.
func (Agent) StartSession(ctx context.Context, req agent.StartRequest) (agent.StartResult, error) {
	if err := ctx.Err(); err != nil {
		return agent.StartResult{}, err
	}
	cwd := strings.TrimSpace(req.Cwd)
	text := strings.TrimSpace(req.Text)
	if cwd == "" || text == "" {
		return agent.StartResult{}, errors.New("cwd 和 text 不能为空")
	}
	ports := acpPorts()
	if len(ports) == 0 {
		return agent.StartResult{}, errors.New("WorkBuddy 未运行，没有可用的 ACP 端口")
	}
	for _, port := range ports {
		status, body := postJSON(port, "/api/v1/acp/connect", map[string]any{
			"clientInfo": map[string]any{"name": "homing", "version": "0.1"},
		}, 3*time.Second)
		if status != http.StatusOK {
			continue
		}
		var connected map[string]any
		if json.Unmarshal(body, &connected) != nil {
			continue
		}
		connectionID := stringValue(connected["connectionId"])
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
		managed.Lock()
		managed.byID[created.sessionID] = managedACP{port: port, connectionID: connectionID}
		managed.Unlock()
		acpFire(port, connectionID, map[string]any{
			"jsonrpc": "2.0", "id": 3, "method": "session/prompt",
			"params": map[string]any{
				"sessionId": created.sessionID,
				"prompt":    []any{map[string]any{"type": "text", "text": text}},
			},
		})
		return agent.StartResult{
			SessionID: created.sessionID,
			Status:    "reused_active",
			Message:   "WorkBuddy 当前版本不能新建会话，已发到当前活跃会话",
		}, nil
	}
	return agent.StartResult{}, errors.New("未能建立 WorkBuddy ACP 连接")
}

// StopSession sends session/cancel to the WorkBuddy bridge that owns this
// session. It does not kill the desktop app.
func (Agent) StopSession(_ context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	managed.Lock()
	session, ok := managed.byID[sessionID]
	if ok {
		delete(managed.byID, sessionID)
	}
	managed.Unlock()
	if !ok {
		return agent.ErrUnsupported
	}
	acpFire(session.port, session.connectionID, map[string]any{
		"jsonrpc": "2.0", "id": 9, "method": "session/cancel",
		"params": map[string]any{"sessionId": sessionID},
	})
	return nil
}

type acpResult struct {
	ok        bool
	result    map[string]any
	sessionID string
	err       string
}

func acpPorts() []int {
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

func postJSON(port int, path string, payload any, timeout time.Duration) (int, []byte) {
	raw, _ := json.Marshal(payload)
	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), bytes.NewReader(raw))
	if err != nil {
		return 0, nil
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: timeout}).Do(request)
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
		} else if stringValue(message["method"]) == "session/update" {
			params, _ := message["params"].(map[string]any)
			if sessionID := stringValue(params["sessionId"]); sessionID != "" {
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
		if stringValue(message["method"]) == "session/update" {
			params, _ := message["params"].(map[string]any)
			if sessionID := stringValue(params["sessionId"]); sessionID != "" {
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
		sessionID := firstNonEmpty(stringValue(body["sessionId"]), stringValue(body["session_id"]))
		if nested, ok := body["session"].(map[string]any); ok {
			sessionID = firstNonEmpty(sessionID, stringValue(nested["id"]))
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
		response, err := (&http.Client{Timeout: 2500 * time.Millisecond}).Do(request)
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
	response, err := (&http.Client{Timeout: timeout}).Do(request)
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
	if text := stringValue(raw["message"]); text != "" {
		return text
	}
	return "ACP error"
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
