package relayapi

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/awayauto"
	filepaths "github.com/tomato-hug/homing/internal/files"
	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/store"
)

func awayGET(d *api.Deps, w http.ResponseWriter, _ *http.Request) {
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	payload := map[string]any{"enabled": s.Away()}
	if current, ok := s.CurrentAwayPeriod(); ok {
		payload["current"] = current
	}
	if last, ok := s.LastAwayPeriod(); ok {
		payload["last"] = last
	}
	api.WriteJSON(w, http.StatusOK, payload)
}

func awayPOST(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	enabled, ok := boolField(body, "enabled")
	if !ok {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "enabled 必须是布尔值"})
		return
	}
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	var (
		period   store.AwayPeriod
		okPeriod bool
	)
	if enabled {
		source := strings.TrimSpace(stringField(body, "source"))
		if source == "" {
			source = "manual"
		}
		deviceID := strings.TrimSpace(stringField(body, "device_id"))
		period, err = s.StartAway(source, deviceID)
		okPeriod = err == nil
	} else {
		period, okPeriod, err = s.EndAway()
	}
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存离开模式失败"})
		return
	}
	if !enabled {
		releaseStopHolds()
	}
	payload := map[string]any{"ok": true, "enabled": enabled}
	if okPeriod {
		payload["period"] = period
		if enabled {
			payload["current"] = period
		} else {
			payload["last"] = period
		}
	}
	api.WriteJSON(w, http.StatusOK, payload)
}

func awaySettingsGET(d *api.Deps, w http.ResponseWriter, _ *http.Request) {
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	enabled, minutes := s.AutoAwaySettings()
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":      enabled,
		"idle_minutes": minutes,
		"available":    autoAwayAvailable(),
	})
}

func awaySettingsPATCH(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	enabled, ok := boolField(body, "enabled")
	if !ok {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "enabled 必须是布尔值"})
		return
	}
	if enabled && !autoAwayAvailable() {
		api.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": "這個平台尚未提供自動離開探測", "code": "auto_away_unavailable",
		})
		return
	}
	minutes := 5
	if raw, present := body["idle_minutes"]; present {
		value, numeric := raw.(float64)
		if !numeric || value < 1 || value != math.Trunc(value) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "idle_minutes 必須是正整數"})
			return
		}
		minutes = int(value)
	}
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	if err := s.SetAutoAway(enabled, minutes); err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存设置失败"})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "enabled": enabled, "idle_minutes": minutes,
	})
}

func autoAwayAvailable() bool { return awayauto.Available() }

func clearAwayPeriod(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "离开时段 id 不能为空"})
		return
	}
	period, found, err := s.ClearAwayPeriod(id)
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存离开时段失败"})
		return
	}
	if !found {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "离开时段不存在"})
		return
	}
	if err := s.MarkAllSeen(); err != nil {
		d.Log.Error("mark all seen after away clear", "err", err)
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "period": period})
}

func uploadHandler(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	query := r.URL.Query()
	name := filepaths.SanitizeName(query.Get("name"))
	if name == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "文件名非法"})
		return
	}

	var cwd string
	if rawEventID := strings.TrimSpace(query.Get("event_id")); rawEventID != "" {
		eventID, err := strconv.ParseInt(rawEventID, 10, 64)
		if err != nil || eventID <= 0 {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "event_id 非法"})
			return
		}
		found := false
		for _, event := range s.Events() {
			if event.ID == eventID {
				cwd = strings.TrimSpace(event.Cwd)
				found = true
				break
			}
		}
		if !found {
			api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "事件不存在"})
			return
		}
	} else if rawCwd := strings.TrimSpace(query.Get("cwd")); rawCwd != "" {
		normalized, err := filepath.Abs(rawCwd)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "cwd 不存在"})
			return
		}
		allowed := false
		for _, event := range s.Events() {
			if event.Cwd == "" {
				continue
			}
			if candidate, err := filepath.Abs(event.Cwd); err == nil && candidate == normalized {
				allowed = true
				break
			}
		}
		if !allowed {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "cwd 不在 /api/projects 列表内"})
			return
		}
		info, err := os.Stat(normalized)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "cwd 不存在"})
			return
		}
		if !info.IsDir() {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "cwd 不是目录"})
			return
		}
		cwd = normalized
	} else {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "event_id 或 cwd 必须提供一个"})
		return
	}

	dir := uploadDir(cwd)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "无法创建上传目录"})
		return
	}
	stamped := time.Now().Format("20060102-150405") + "-" + name
	finalPath := filepath.Join(dir, stamped)
	tmp, err := os.CreateTemp(dir, ".upload-*.part")
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "无法创建临时文件"})
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	_ = tmp.Chmod(0o600)

	written, copyErr := io.Copy(tmp, io.LimitReader(r.Body, filepaths.MaxAttachmentBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "上传被中断"})
		return
	}
	if written > filepaths.MaxAttachmentBytes {
		api.WriteJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "文件超过 50MB 上限"})
		return
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存失败：" + err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"path": finalPath,
		"name": stamped,
		"size": written,
		"mime": filepaths.MimeFor(stamped),
	})
}

func uploadDir(cwd string) string {
	if strings.TrimSpace(cwd) != "" {
		dir := filepath.Join(cwd, ".agent-inbox", "uploads")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			return dir
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AgentInbox", "uploads")
}

func capabilityFile(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		api.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method Not Allowed"})
		return
	}
	if !isLocalOrLAN(r) {
		api.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
		return
	}
	token := r.PathValue("token")
	relative := r.PathValue("path")
	real, err := filepaths.ManagerFor(d.DataDir).Resolve(token, relative)
	if err != nil {
		status := http.StatusNotFound
		message := "文件不存在"
		if errors.Is(err, filepaths.ErrForbidden) {
			status = http.StatusForbidden
			message = "禁止访问"
		} else if errors.Is(err, filepaths.ErrExpired) {
			message = "链接已失效"
		}
		api.WriteJSON(w, status, map[string]any{"error": message})
		return
	}
	f, err := os.Open(real)
	if err != nil {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "文件不存在"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "文件不存在"})
		return
	}
	w.Header().Set("Content-Type", filepaths.MimeFor(real))
	w.Header().Set("Cache-Control", "private, max-age=60")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func downloadAPK(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		api.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method Not Allowed"})
		return
	}
	if !isLocalOrLAN(r) {
		api.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
		return
	}
	path := apkPath()
	f, err := os.Open(path)
	if err != nil {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "APK 尚未构建"})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "APK 尚未构建"})
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="agent-inbox.apk"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func apkPath() string {
	if env := strings.TrimSpace(os.Getenv("APK_PATH")); env != "" {
		if absolute, err := filepath.Abs(env); err == nil {
			return absolute
		}
	}
	relative := filepath.Join("android", "app", "build", "outputs", "apk", "debug", "app-debug.apk")
	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, relative)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return relative
}

var proxyClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		Proxy:               nil,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     30 * time.Second,
	},
}

func proxyPort(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	escaped := strings.TrimPrefix(r.URL.EscapedPath(), "/p/")
	portText := escaped
	rest := ""
	if index := strings.IndexByte(escaped, '/'); index >= 0 {
		portText = escaped[:index]
		rest = escaped[index:]
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || len(portText) > 5 {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "端口非法"})
		return
	}

	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	enabled, ports := s.Proxy()
	if !enabled {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "代理未开启"})
		return
	}
	allowed := false
	for _, candidate := range ports {
		if candidate == port {
			allowed = true
			break
		}
	}
	if !allowed {
		api.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "端口不在白名单"})
		return
	}
	if !d.Authorized(r) && !capabilityQueryValid(d.DataDir, r.URL) {
		api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
		return
	}

	target := "http://127.0.0.1:" + strconv.Itoa(port) + rest
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	upstream, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		api.WriteJSON(w, http.StatusBadGateway, map[string]any{"error": "上游连接失败"})
		return
	}
	copyProxyHeaders(upstream.Header, r.Header)
	upstream.Header.Set("X-Forwarded-For", remoteHost(r))
	upstream.Header.Set("X-Forwarded-Port", strconv.Itoa(port))
	response, err := proxyClient.Do(upstream)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			api.WriteJSON(w, http.StatusGatewayTimeout, map[string]any{"error": "上游超时"})
			return
		}
		api.WriteJSON(w, http.StatusBadGateway, map[string]any{"error": "上游连接失败"})
		return
	}
	defer response.Body.Close()
	copyProxyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func copyProxyHeaders(dst, src http.Header) {
	hop := map[string]bool{
		"connection": true, "proxy-connection": true, "keep-alive": true,
		"proxy-authenticate": true, "proxy-authorization": true,
		"te": true, "trailer": true, "transfer-encoding": true, "upgrade": true,
		"authorization": true, "host": true,
	}
	for key, values := range src {
		if hop[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func capabilityQueryValid(dataDir string, u *url.URL) bool {
	token := strings.TrimSpace(u.Query().Get("cap"))
	if token == "" {
		token = strings.TrimSpace(u.Query().Get("token"))
	}
	_, ok := filepaths.ManagerFor(dataDir).Lookup(token)
	return ok
}

type publicDevice struct {
	DeviceID   string   `json:"device_id"`
	Name       string   `json:"name"`
	Platform   string   `json:"platform"`
	AppVersion *string  `json:"app_version"`
	CreatedAt  *string  `json:"created_at"`
	LastSeenAt *string  `json:"last_seen_at"`
	Online     bool     `json:"online"`
	PairedVia  string   `json:"paired_via"`
	LastIP     *string  `json:"last_ip"`
	Scopes     []string `json:"scopes"`
}

func listDevices(d *api.Deps, w http.ResponseWriter, _ *http.Request) {
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	devices := s.Devices()
	items := make([]publicDevice, 0, len(devices))
	online := 0
	for _, device := range devices {
		item := publicDeviceOf(device)
		if item.Online {
			online++
		}
		items = append(items, item)
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"devices": items, "count": len(items), "online": online,
	})
}

func heartbeatDevice(d *api.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "设备令牌无效"})
			return
		}
		s, err := d.Store()
		if err != nil {
			api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
			return
		}
		body, _, _ := readJSON(r)
		device, ok := s.TouchDeviceByToken(
			strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")),
			stringField(body, "app_version"),
			remoteHost(r),
		)
		if !ok {
			api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "设备令牌无效"})
			return
		}
		pause := platform.GetPause(platform.NewPaths(d.DataDir))
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "device": publicDeviceOf(device), "server_time": nowISO(),
			"paused": pause.Paused, "pause_until": pause.Until,
		})
	}
}

var deviceIDPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

func renameDevice(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !deviceIDPattern.MatchString(id) {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "Not Found"})
		return
	}
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	device, ok := s.RenameDevice(id, stringField(body, "name"))
	if !ok {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "没有这台设备"})
		return
	}
	if _, present := body["scopes"]; present {
		scoped, saved := s.SetDeviceScopes(id, stringList(body["scopes"]))
		if !saved {
			api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "没有这台设备"})
			return
		}
		device = scoped
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "device": publicDeviceOf(device)})
}

func removeDevice(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !deviceIDPattern.MatchString(id) {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "Not Found"})
		return
	}
	s, err := d.Store()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	if !s.RemoveDevice(id) {
		api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "没有这台设备"})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "device_id": id})
}

func publicDeviceOf(device store.Device) publicDevice {
	online := device.LastSeenMs > 0 && time.Since(time.UnixMilli(device.LastSeenMs)) < 90*time.Second
	out := publicDevice{
		DeviceID:  device.ID,
		Name:      firstNonEmpty(device.Name, "手机"),
		Platform:  firstNonEmpty(device.Platform, "android"),
		Online:    online,
		PairedVia: firstNonEmpty(device.PairedVia, "qr"),
	}
	if device.AppVersion != "" {
		value := device.AppVersion
		out.AppVersion = &value
	}
	if device.CreatedAt != "" {
		value := device.CreatedAt
		out.CreatedAt = &value
	}
	if device.LastSeenMs > 0 {
		value := time.UnixMilli(device.LastSeenMs).UTC().Format("2006-01-02T15:04:05.000Z")
		out.LastSeenAt = &value
	}
	if device.LastIP != "" {
		value := device.LastIP
		out.LastIP = &value
	}
	out.Scopes = append([]string(nil), device.Scopes...)
	if len(out.Scopes) == 0 {
		out.Scopes = []string{"read", "reply", "drive"}
	}
	return out
}
