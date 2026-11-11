package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

const maxAPIResponse = 4 << 20

// API is the loopback-only Homing client used by the stdio bridge. It carries
// the owner's local token in memory and never writes it to a tool result, log,
// or process argument.
type API struct {
	baseURL *url.URL
	token   string
	http    *http.Client
}

type sessionInfo struct {
	SessionID string `json:"session_id"`
	Source    string `json:"source"`
}

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Code)
	}
	return e.Message
}

func newAPI(baseURL, token string) (*API, error) {
	u, err := loopbackURL(baseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("本机 Homing 令牌为空")
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !isLoopbackURL(req.URL) {
				return errors.New("拒绝把本机凭据重定向到非回环地址")
			}
			return nil
		},
	}
	return &API{baseURL: u, token: strings.TrimSpace(token), http: client}, nil
}

func loopbackURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "http://127.0.0.1:18787"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("Homing URL 无法解析：%w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("Homing URL 只接受 http 或 https")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return nil, errors.New("Homing URL 只能是本机服务的根地址")
	}
	if !isLoopbackURL(u) {
		return nil, errors.New("homing mcp 只连接回环地址，避免把本机令牌送到网络")
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

func isLoopbackURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.TrimSpace(strings.ToLower(u.Hostname()))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *API) Parent(ctx context.Context, sessionID string) (sessionInfo, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sessionInfo{}, errors.New("parent session id 为空")
	}
	var info sessionInfo
	status, err := a.request(ctx, http.MethodGet,
		"/api/sessions/"+url.PathEscape(sessionID), nil, &info)
	if err != nil {
		return sessionInfo{}, err
	}
	if status != http.StatusOK {
		return sessionInfo{}, &apiError{Status: status, Message: "无法读取父会话"}
	}
	if info.SessionID == "" {
		info.SessionID = sessionID
	}
	return info, nil
}

// Dispatch calls the v1 dispatch endpoint. The endpoint deliberately takes the
// target and task as separate fields, so text supplied by an agent cannot be
// parsed into additional mentions and fan out to unrequested agents.
func (a *API) Dispatch(ctx context.Context, body map[string]any) (map[string]any, error) {
	out := map[string]any{}
	status, err := a.request(ctx, http.MethodPost, "/api/dispatch", body, &out)
	if err != nil {
		return out, err
	}
	if status >= 200 && status < 300 {
		return out, nil
	}
	message, _ := out["error"].(string)
	if strings.TrimSpace(message) == "" {
		message = fmt.Sprintf("Homing 返回 HTTP %d", status)
	}
	code, _ := out["code"].(string)
	return out, &apiError{Status: status, Code: code, Message: message}
}

func (a *API) request(
	ctx context.Context,
	method, path string,
	body any,
	out any,
) (int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	target := *a.baseURL
	target.Path = path
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := a.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("无法连接 Homing 本机服务：%w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponse+1))
	if err != nil {
		return response.StatusCode, err
	}
	if len(raw) > maxAPIResponse {
		return response.StatusCode, errors.New("Homing 响应过大")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return response.StatusCode, nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		if response.StatusCode < 300 {
			return response.StatusCode, fmt.Errorf("Homing 返回了无法解析的响应：%w", err)
		}
	}
	return response.StatusCode, nil
}

// readOwnerToken reads the same config.json the local server uses. Refusing a
// group/world-readable config prevents a token leak caused by an overwritten
// dotfile permission.
func readOwnerToken(dataDir string) (string, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return "", errors.New("Homing 数据目录为空")
	}
	configPath := filepath.Join(dataDir, "config.json")
	info, err := os.Stat(configPath)
	if err != nil {
		return "", fmt.Errorf("读取 Homing 配置失败：%w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("Homing 配置权限过宽（%04o），请先执行 chmod 600 %s",
			info.Mode().Perm(), configPath)
	}
	var config struct {
		Token string `json:"token"`
	}
	if ok := platform.ReadJSON(configPath, &config); !ok || strings.TrimSpace(config.Token) == "" {
		return "", errors.New("Homing 配置中没有本机令牌")
	}
	return config.Token, nil
}
