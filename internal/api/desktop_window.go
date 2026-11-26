package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

type desktopWindowEvent struct {
	Action string `json:"action"`
	Path   string `json:"path"`
	Ts     string `json:"ts"`
}

type desktopWindowHub struct {
	mu   sync.Mutex
	subs map[chan desktopWindowEvent]struct{}
}

func (h *desktopWindowHub) subscribe() (chan desktopWindowEvent, func()) {
	ch := make(chan desktopWindowEvent, 8)
	h.mu.Lock()
	if h.subs == nil {
		h.subs = make(map[chan desktopWindowEvent]struct{})
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
		})
	}
}

func (h *desktopWindowHub) publish(ev desktopWindowEvent) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	delivered := 0
	for ch := range h.subs {
		delivered++
		select {
		case ch <- ev:
		default:
		}
	}
	return delivered
}

var (
	windowHubsMu sync.Mutex
	windowHubs   = map[string]*desktopWindowHub{}
)

func windowHubFor(dataDir string) *desktopWindowHub {
	key := strings.TrimSpace(dataDir)
	windowHubsMu.Lock()
	defer windowHubsMu.Unlock()
	hub := windowHubs[key]
	if hub == nil {
		hub = &desktopWindowHub{}
		windowHubs[key] = hub
	}
	return hub
}

func sanitizeConsolePath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" || !strings.HasPrefix(p, "/console") || strings.HasPrefix(p, "//") {
		return "/console/"
	}
	return p
}

func init() {
	Register("desktop-window", 14, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("POST /api/desktop/window", d.guard(d.handleDesktopWindowOpen))
		mux.HandleFunc("GET /api/desktop/window/stream", d.guard(d.handleDesktopWindowStream))
		mux.HandleFunc("POST /api/desktop/open-external", d.guard(d.handleDesktopOpenExternal))
		mux.HandleFunc("POST /api/desktop/relay-proxy", d.guard(d.handleDesktopRelayProxy))
	})
}

func (d *Deps) requireLoopbackDesktopOwner(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopback(r.RemoteAddr) {
		WriteError(w, http.StatusForbidden, "loopback_only", "桌面窗口控制仅允许本机回环调用")
		return false
	}
	if !d.RequireScope(w, r, "drive") {
		return false
	}
	return true
}

func (d *Deps) handleDesktopWindowOpen(w http.ResponseWriter, r *http.Request) {
	if !d.requireLoopbackDesktopOwner(w, r) {
		return
	}
	body := readBody(r)
	action, _ := body["action"].(string)
	if strings.TrimSpace(action) == "" {
		action = "open"
	}
	rawPath, _ := body["path"].(string)
	ev := desktopWindowEvent{
		Action: action,
		Path:   sanitizeConsolePath(rawPath),
		Ts:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	delivered := windowHubFor(d.DataDir).publish(ev)
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"handled_by_shell": delivered > 0,
		"subscribers":      delivered,
		"path":             ev.Path,
	})
}

func (d *Deps) handleDesktopWindowStream(w http.ResponseWriter, r *http.Request) {
	if !d.requireLoopbackDesktopOwner(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "streaming_unsupported", "Streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": ready\n\n")
	flusher.Flush()

	ch, unsub := windowHubFor(d.DataDir).subscribe()
	defer unsub()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev := <-ch:
			raw, _ := json.Marshal(ev)
			if _, err := fmt.Fprintf(w, "event: window\ndata: %s\n\n", raw); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// openExternalBrowserFunc is overridable in tests so unit tests never launch a real browser.
var openExternalBrowserFunc = launchOSBrowser

func launchOSBrowser(targetURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("/usr/bin/open", targetURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
		platform.HideChildWindow(cmd)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func validateExternalBrowserURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("url 不能为空")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("无效的链接格式")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("仅支持打开 http:// 或 https:// 链接")
	}
	return parsed.String(), nil
}

// handleDesktopOpenExternal opens an http/https URL in the user's real default browser
// on macOS / Windows / Linux, working around WKWebView / WebView2 blocking window.open().
func (d *Deps) handleDesktopOpenExternal(w http.ResponseWriter, r *http.Request) {
	if !d.requireLoopbackDesktopOwner(w, r) {
		return
	}
	body := readBody(r)
	rawURL, _ := body["url"].(string)
	target, err := validateExternalBrowserURL(rawURL)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_url", err.Error())
		return
	}
	if err := openExternalBrowserFunc(target); err != nil {
		WriteError(w, http.StatusInternalServerError, "open_browser_failed", "打开系统浏览器失败："+err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"opened": true,
		"url":    target,
	})
}

func normalizeRelayHTTPBase(rawServer string) (string, error) {
	s := strings.TrimSpace(rawServer)
	if s == "" {
		return "", fmt.Errorf("中继地址不能为空")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("无效的中继服务器地址")
	}
	switch strings.ToLower(u.Scheme) {
	case "wss", "https":
		u.Scheme = "https"
	case "ws", "http":
		u.Scheme = "http"
	default:
		return "", fmt.Errorf("中继协议仅支持 wss:// / ws:// / https:// / http://")
	}
	return u.Scheme + "://" + u.Host, nil
}

func isAllowedRelayProxyPath(p string) bool {
	if p == "" || !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.Contains(p, "..") {
		return false
	}
	return p == "/health" ||
		p == "/v1/account/config" ||
		strings.HasPrefix(p, "/v1/admin/")
}

// handleDesktopRelayProxy forwards strictly allowlisted relay probe & admin API
// requests from the local desktop console to the configured homing-server so
// macOS/Windows desktop admins can inspect and manage relay accounts without CORS friction.
func (d *Deps) handleDesktopRelayProxy(w http.ResponseWriter, r *http.Request) {
	if !d.requireLoopbackDesktopOwner(w, r) {
		return
	}
	var reqBody struct {
		Server     string `json:"server"`
		Method     string `json:"method"`
		Path       string `json:"path"`
		AdminToken string `json:"admin_token"`
		Payload    any    `json:"payload"`
		UseProxy   *bool  `json:"use_system_proxy"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&reqBody); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_json", "请求格式错误")
		return
	}
	base, err := normalizeRelayHTTPBase(reqBody.Server)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_server", err.Error())
		return
	}
	cleanPath := strings.TrimSpace(reqBody.Path)
	if !isAllowedRelayProxyPath(cleanPath) {
		WriteError(w, http.StatusBadRequest, "path_not_allowed", "不支持的中继管理路径")
		return
	}
	method := strings.ToUpper(strings.TrimSpace(reqBody.Method))
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		WriteError(w, http.StatusBadRequest, "invalid_method", "不支持的 HTTP 方法")
		return
	}

	var bodyReader io.Reader
	if reqBody.Payload != nil && method != http.MethodGet {
		raw, err := json.Marshal(reqBody.Payload)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_payload", "无法序列化请求数据")
			return
		}
		bodyReader = bytes.NewReader(raw)
	}
	upReq, err := http.NewRequestWithContext(r.Context(), method, base+cleanPath, bodyReader)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if bodyReader != nil {
		upReq.Header.Set("Content-Type", "application/json")
	}
	if tok := strings.TrimSpace(reqBody.AdminToken); tok != "" {
		upReq.Header.Set("Authorization", "Bearer "+tok)
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	if reqBody.UseProxy == nil || !*reqBody.UseProxy {
		tr.Proxy = nil // Honor Homing's Direct-Dial by default
	}
	client := &http.Client{Transport: tr, Timeout: 12 * time.Second}
	resp, err := client.Do(upReq)
	if err != nil {
		WriteError(w, http.StatusBadGateway, "relay_unreachable", "无法连接中继服务器："+err.Error())
		return
	}
	defer resp.Body.Close()

	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		WriteError(w, http.StatusBadGateway, "relay_read_failed", "读取中继响应失败")
		return
	}
	var decoded any
	if json.Unmarshal(rawResp, &decoded) != nil {
		decoded = map[string]any{"raw": strings.TrimSpace(string(rawResp))}
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok":     resp.StatusCode >= 200 && resp.StatusCode < 300,
		"status": resp.StatusCode,
		"data":   decoded,
	})
}
