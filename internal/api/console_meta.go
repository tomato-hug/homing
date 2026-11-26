package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
)

const (
	maxLogLines = 500
	maxLogBytes = 512 * 1024
)

var (
	logPrefixPattern = regexp.MustCompile(`^\[(\w+)\]\s*(.*)$`)
	logErrorPattern  = regexp.MustCompile(`(?i)(error|失败)`)
	logTimePattern   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)
	logTokenPattern  = regexp.MustCompile(`(?i)(Token:\s*)([0-9a-f]{16,})`)
	logHMACPattern   = regexp.MustCompile(`\bhm_[A-Za-z0-9_-]{20,}`)
	logQueryPattern  = regexp.MustCompile(`(?i)(token=)([0-9a-f]{16,})`)
)

type notifyStats struct {
	Day     string         `json:"day"`
	Pushed  int            `json:"pushed"`
	Merged  int            `json:"merged"`
	ByLevel map[string]int `json:"by_level"`
}

type logLine struct {
	At    *string `json:"at"`
	Level string  `json:"level"`
	Text  string  `json:"text"`
}

type setupRecord struct {
	CompletedAt string   `json:"completed_at"`
	Agents      []string `json:"agents"`
	Method      string   `json:"method"`
}

type autostartResponse struct {
	OK bool `json:"ok"`
	platform.AutostartState
	Note string `json:"note,omitempty"`
}

func init() {
	Register("console-meta", 20, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/stats", d.guard(d.handleStats))
		mux.HandleFunc("GET /api/push/pause", d.guard(d.handlePause))
		mux.HandleFunc("POST /api/push/pause", d.guard(d.handlePause))
		mux.HandleFunc("DELETE /api/push/pause", d.guard(d.handlePause))
		mux.HandleFunc("GET /api/agent-paths", d.guard(d.handleAgentPaths))
		mux.HandleFunc("GET /api/logs", d.guard(d.handleLogs))
		mux.HandleFunc("GET /api/setup", d.guard(d.handleSetup))
		mux.HandleFunc("POST /api/setup", d.guard(d.handleSetup))
		mux.HandleFunc("GET /api/autostart", d.loopbackGuard(d.handleAutostart))
		mux.HandleFunc("POST /api/autostart", d.loopbackGuard(d.handleAutostart))
	})
}

func (d *Deps) loopbackGuard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r.RemoteAddr) {
			WriteError(w, http.StatusForbidden, "loopback_only", "只允许在本机操作")
			return
		}
		d.guard(h)(w, r)
	}
}

func (d *Deps) handleStats(w http.ResponseWriter, r *http.Request) {
	devices := 0
	if s, ok := d.store(); ok {
		devices = len(s.Devices())
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"notify":  readNotifyStats(d.DataDir),
		"pause":   platform.GetPause(platform.NewPaths(d.DataDir)),
		"devices": devices,
	})
}

func readNotifyStats(dataDir string) notifyStats {
	today := time.Now().Format("2006-01-02")
	out := notifyStats{
		Day:     today,
		ByLevel: map[string]int{"P0": 0, "P1": 0, "P2": 0},
	}
	var saved notifyStats
	if !platform.ReadJSON(filepath.Join(dataDir, "notify-stats.json"), &saved) || saved.Day != today {
		return out
	}
	out.Pushed = saved.Pushed
	out.Merged = saved.Merged
	for level, count := range saved.ByLevel {
		out.ByLevel[level] = count
	}
	return out
}

func (d *Deps) handlePause(w http.ResponseWriter, r *http.Request) {
	paths := platform.NewPaths(d.DataDir)
	switch r.Method {
	case http.MethodGet:
		WriteJSON(w, http.StatusOK, platform.GetPause(paths))
	case http.MethodPost:
		body := readBody(r)
		minutes := numberFromJSON(body["minutes"])
		if minutes <= 0 {
			if paused, ok := body["paused"].(bool); ok && paused {
				minutes = 60
			}
		}
		reason := "手动暂停"
		if value, ok := body["reason"].(string); ok && strings.TrimSpace(value) != "" {
			reason = strings.TrimSpace(value)
		}
		var state platform.PauseState
		if minutes > 0 {
			state = platform.SetPause(paths, minutes, &reason)
		} else {
			state = platform.ClearPause(paths)
		}
		WriteJSON(w, http.StatusOK, state)
	case http.MethodDelete:
		WriteJSON(w, http.StatusOK, platform.ClearPause(paths))
	default:
		methodNotAllowed(w)
	}
}

func numberFromJSON(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func (d *Deps) handleAgentPaths(w http.ResponseWriter, r *http.Request) {
	paths := make(map[string]platform.HomeInfo, len(displayOrder))
	for _, id := range displayOrder {
		paths[id] = platform.DescribeHome(id)
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"paths":         paths,
		"homedir":       platform.Home(),
		"wsl_supported": runtime.GOOS == "windows",
	})
}

func (d *Deps) handleLogs(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(r.URL.Query().Get("limit"), 200, 1, maxLogLines)
	source := pickLogFile(d)
	lines := []logLine{}
	if source != "" {
		lines = tailLogLines(source, limit)
	}
	var sourceValue any
	if source != "" {
		sourceValue = source
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"source": sourceValue,
		"count":  len(lines),
		"lines":  lines,
	})
}

func pickLogFile(d *Deps) string {
	candidates := []string{platform.NewPaths(d.DataDir).Log}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "relay", "relay.log"))
	}
	for _, file := range candidates {
		if stat, err := os.Stat(file); err == nil && stat.Mode().IsRegular() && stat.Size() > 0 {
			return file
		}
	}
	return ""
}

func tailLogLines(file string, limit int) []logLine {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil
	}
	start := stat.Size() - maxLogBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxLogBytes))
	if err != nil {
		return nil
	}
	text := string(raw)
	if start > 0 {
		if newline := strings.IndexByte(text, '\n'); newline >= 0 {
			text = text[newline+1:]
		}
	}
	rows := strings.Split(text, "\n")
	filtered := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row) != "" {
			filtered = append(filtered, redactLog(row))
		}
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	lines := make([]logLine, 0, len(filtered))
	for _, row := range filtered {
		at := logTimestamp(row)
		lines = append(lines, logLine{At: at, Level: logLevel(row), Text: row})
	}
	return lines
}

func redactLog(line string) string {
	line = logTokenPattern.ReplaceAllString(line, "$1••••••••（已隐藏）")
	line = logHMACPattern.ReplaceAllString(line, "hm_••••••••（已隐藏）")
	line = logQueryPattern.ReplaceAllString(line, "$1••••••••（已隐藏）")
	return line
}

func logTimestamp(line string) *string {
	match := logTimePattern.FindString(line)
	if match == "" {
		return nil
	}
	return &match
}

func logLevel(line string) string {
	if logErrorPattern.MatchString(line) {
		return "error"
	}
	lower := strings.ToLower(line)
	if strings.Contains(lower, "level=warn") || strings.Contains(lower, "level=\"warn\"") {
		return "warn"
	}
	if match := logPrefixPattern.FindStringSubmatch(line); len(match) > 1 {
		return strings.ToLower(match[1])
	}
	return "info"
}

func (d *Deps) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		body := readBody(r)
		method := "lan"
		if value, ok := body["method"].(string); ok && (value == "cloud" || value == "self_hosted") {
			method = value
		}
		record := setupRecord{
			CompletedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
			Agents:      jsonStringList(body["agents"], 20),
			Method:      method,
		}
		if err := platform.WriteJSON(filepath.Join(d.DataDir, "setup.json"), record); err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存设置失败"})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"completed_at": record.CompletedAt,
			"agents":       record.Agents,
			"method":       record.Method,
		})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	record := readSetupRecord(d.DataDir)
	devices := 0
	if s, ok := d.store(); ok {
		devices = len(s.Devices())
	}
	var completedAt any
	if record.CompletedAt != "" {
		completedAt = record.CompletedAt
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"completed":     record.CompletedAt != "",
		"completed_at":  completedAt,
		"chosen_agents": record.Agents,
		"method":        record.Method,
		"devices":       devices,
	})
}

func readSetupRecord(dataDir string) setupRecord {
	record := setupRecord{Agents: []string{}, Method: "lan"}
	_ = platform.ReadJSON(filepath.Join(dataDir, "setup.json"), &record)
	if record.Agents == nil {
		record.Agents = []string{}
	}
	if record.Method == "" {
		record.Method = "lan"
	}
	return record
}

func jsonStringList(value any, limit int) []string {
	out := []string{}
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
			if len(out) == limit {
				break
			}
		}
	case []string:
		for _, item := range items {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

func (d *Deps) handleAutostart(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		WriteJSON(w, http.StatusOK, platform.AutostartInfo())
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	body := readBody(r)
	enabled, _ := body["enabled"].(bool)
	execPath, err := os.Executable()
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "找不到 homing 可执行文件"})
		return
	}
	info, err := platform.SetAutostart(enabled, execPath, d.DataDir, requestPort(r))
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	note := "已移除 LaunchAgent，下次登录不再自启"
	if enabled {
		note = "已写入 LaunchAgent，下次登录自动启动"
	}
	WriteJSON(w, http.StatusOK, autostartResponse{OK: true, AutostartState: info, Note: note})
}
