package api

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agentconfig"
	"github.com/tomato-hug/homing/internal/driver"
	"github.com/tomato-hug/homing/internal/platform"
)

const (
	processSnapshotTTL = 3 * time.Second
	wokenStateTTL      = 12 * time.Hour
	recentActiveWindow = 10 * time.Minute
)

var (
	procCacheMu   sync.Mutex
	procCacheAt   time.Time
	procCacheData map[string]bool

	// processSnapshotRunner returns lines of `ps -axo comm=,args=`. Tests may override it.
	processSnapshotRunner = defaultProcessSnapshotLines

	// macAppBundleFinder locates an installed macOS .app bundle for desktop agents. Tests may override it.
	macAppBundleFinder = defaultFindMacAppBundle

	// agentWakeRunner performs the OS launch for wakeMode ("app" | "serve" | "cli"). Tests may override it.
	agentWakeRunner = defaultRunAgentWake

	wokenMu    sync.Mutex
	wokenUntil = map[string]time.Time{}
)

// macDesktopAppNames maps agent IDs to candidate macOS .app bundle names.
var macDesktopAppNames = map[string][]string{
	"workbuddy":   {"WorkBuddy.app"},
	"cursor":      {"Cursor.app"},
	"windsurf":    {"Windsurf.app"},
	"trae":        {"Trae.app", "Trae CN.app"},
	"zed":         {"Zed.app", "Zed Preview.app"},
	"antigravity": {"Antigravity.app"},
	"codebuddy":   {"CodeBuddy.app"},
	"warp":        {"Warp.app"},
}

// windowsDesktopExeCandidates maps agent IDs to relative executable paths under
// %LOCALAPPDATA%\Programs, %LOCALAPPDATA%, and %ProgramFiles% on Windows.
var windowsDesktopExeCandidates = map[string][]string{
	"workbuddy":   {`WorkBuddy\WorkBuddy.exe`, `Programs\WorkBuddy\WorkBuddy.exe`},
	"cursor":      {`Programs\cursor\Cursor.exe`, `Programs\Cursor\Cursor.exe`, `Cursor\Cursor.exe`},
	"windsurf":    {`Programs\Windsurf\Windsurf.exe`, `Windsurf\Windsurf.exe`},
	"trae":        {`Programs\Trae\Trae.exe`, `Programs\Trae CN\Trae CN.exe`, `Trae\Trae.exe`},
	"zed":         {`Programs\Zed\Zed.exe`, `Zed\Zed.exe`},
	"antigravity": {`Programs\Antigravity\Antigravity.exe`, `Antigravity\Antigravity.exe`},
	"codebuddy":   {`Programs\CodeBuddy\CodeBuddy.exe`, `CodeBuddy\CodeBuddy.exe`},
	"warp":        {`Programs\Warp\warp.exe`, `Warp\warp.exe`},
}

// windowsTasklistToAgent maps lowercase Windows executable names (without .exe) to Homing agent IDs.
var windowsTasklistToAgent = map[string]string{
	"workbuddy":   "workbuddy",
	"cursor":      "cursor",
	"windsurf":    "windsurf",
	"trae":        "trae",
	"trae cn":     "trae",
	"zed":         "zed",
	"antigravity": "antigravity",
	"codebuddy":   "codebuddy",
	"warp":        "warp",
}
// desktopAppProcessMarkers maps agent IDs to macOS app bundle executable path substrings.
var desktopAppProcessMarkers = map[string][]string{
	"workbuddy":   {"WorkBuddy.app/Contents/MacOS/"},
	"cursor":      {"Cursor.app/Contents/MacOS/"},
	"windsurf":    {"Windsurf.app/Contents/MacOS/"},
	"trae":        {"Trae.app/Contents/MacOS/", "Trae CN.app/Contents/MacOS/"},
	"zed":         {"Zed.app/Contents/MacOS/", "Zed Preview.app/Contents/MacOS/"},
	"antigravity": {"Antigravity.app/Contents/MacOS/"},
	"codebuddy":   {"CodeBuddy.app/Contents/MacOS/"},
	"warp":        {"Warp.app/Contents/MacOS/"},
}

// nodeModuleMarkers maps npm package path fragments in ps args to Homing agent IDs
// (covering wrapper scripts like `node .../node_modules/openclaw/dist/index.js`).
var nodeModuleMarkers = map[string]string{
	"/node_modules/openclaw/":                 "openclaw",
	"/node_modules/@anthropic-ai/claude-code": "claude",
	"/node_modules/@openai/codex":             "codex",
	"/node_modules/@google/gemini-cli":        "gemini-cli",
	"/node_modules/@qwen-code/qwen-code":      "qwen-code",
	"/node_modules/opencode":                  "opencode",
}

// cliExecutableToAgent maps exact executable basenames to Homing agent IDs.
var cliExecutableToAgent = map[string]string{
	"claude":       "claude",
	"codex":        "codex",
	"kimi":         "kimi",
	"opencode":     "opencode",
	"zcode":        "zcode",
	"dsh":          "dsh",
	"gemini":       "gemini-cli",
	"qwen":         "qwen-code",
	"copilot":      "copilot-cli",
	"codebuddy":    "codebuddy",
	"cursor-agent": "cursor",
	"aider":        "aider",
	"goose":        "goose",
	"openhands":    "openhands",
	"amp":          "amp",
	"kilo":         "kilo",
	"kilocode":     "kilo",
	"droid":        "droid",
	"openclaw":     "openclaw",
	"hermes":       "hermes",
	"crush":        "crush",
	"pi":           "pi",
	"auggie":       "auggie",
}

func init() {
	Register("agent-wake", 12, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("POST /api/agents/{id}/wake", d.guard(d.handleAgentWake))
		mux.HandleFunc("POST /api/agents/{id}/sleep", d.guard(d.handleAgentSleep))
	})
}

func defaultProcessSnapshotLines(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	if runtime.GOOS == "windows" {
		cmd := exec.CommandContext(ctx, "tasklist", "/FO", "CSV", "/NH")
		platform.HideChildWindow(cmd)
		out, err := cmd.Output()
		if err != nil {
			return nil, err
		}
		return strings.Split(string(out), "\n"), nil
	}
	out, err := exec.CommandContext(ctx, "ps", "-axo", "comm=,args=").Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(string(out), "\n"), nil
}

// parseRunningAgentsFromPS extracts running Homing agent IDs from `ps -axo comm=,args=`
// lines on POSIX or `tasklist /FO CSV /NH` lines on Windows.
func parseRunningAgentsFromPS(lines []string) map[string]bool {
	running := make(map[string]bool, 8)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Handle Windows `tasklist /FO CSV /NH`: `"ImageName.exe","1234","Console",...`
		if strings.HasPrefix(line, `"`) {
			if endQuote := strings.IndexByte(line[1:], '"'); endQuote > 0 {
				img := strings.ToLower(strings.TrimSpace(line[1 : 1+endQuote]))
				img = strings.TrimSuffix(img, ".exe")
				if img == "homing" || img == "homing-cli" || img == "homing-server" || img == "tasklist" {
					continue
				}
				if id, ok := windowsTasklistToAgent[img]; ok {
					running[id] = true
				}
				if id, ok := cliExecutableToAgent[img]; ok {
					running[id] = true
				}
				continue
			}
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		firstToken := strings.ReplaceAll(fields[0], `\`, `/`)
		firstBase := strings.ToLower(filepath.Base(firstToken))
		firstBase = strings.TrimSuffix(firstBase, ".exe")
		if firstBase == "ps" || firstBase == "pgrep" || firstBase == "grep" || firstBase == "homing" || firstBase == "homing-cli" || firstBase == "homing-server" {
			continue
		}

		for agentID, markers := range desktopAppProcessMarkers {
			for _, marker := range markers {
				if strings.Contains(line, marker) {
					running[agentID] = true
					break
				}
			}
		}

		normLine := strings.ReplaceAll(line, `\`, `/`)
		for marker, agentID := range nodeModuleMarkers {
			if strings.Contains(normLine, marker) {
				running[agentID] = true
			}
		}

		if id, ok := windowsTasklistToAgent[firstBase]; ok {
			running[id] = true
		}
		if id, ok := cliExecutableToAgent[firstBase]; ok {
			running[id] = true
			continue
		}

		// Check wrapper runtimes (node / python / bun / deno) executing a CLI entrypoint
		for i := 1; i < len(fields) && i <= 4; i++ {
			tok := strings.ReplaceAll(fields[i], `\`, `/`)
			if strings.HasPrefix(tok, "-") {
				continue
			}
			base := strings.ToLower(filepath.Base(tok))
			base = strings.TrimSuffix(base, ".exe")
			base = strings.TrimSuffix(base, ".cmd")
			base = strings.TrimSuffix(base, ".js")
			base = strings.TrimSuffix(base, ".mjs")
			if id, ok := cliExecutableToAgent[base]; ok {
				running[id] = true
				break
			}
		}
	}
	return running
}

func liveAgentProcessMap(ctx context.Context) map[string]bool {
	now := time.Now()
	procCacheMu.Lock()
	if procCacheData != nil && now.Sub(procCacheAt) < processSnapshotTTL {
		copied := make(map[string]bool, len(procCacheData))
		for k, v := range procCacheData {
			copied[k] = v
		}
		procCacheMu.Unlock()
		return copied
	}
	procCacheMu.Unlock()

	lines, err := processSnapshotRunner(ctx)
	var parsed map[string]bool
	if err != nil {
		parsed = map[string]bool{}
	} else {
		parsed = parseRunningAgentsFromPS(lines)
	}

	procCacheMu.Lock()
	procCacheAt = time.Now()
	procCacheData = parsed
	copied := make(map[string]bool, len(parsed))
	for k, v := range parsed {
		copied[k] = v
	}
	procCacheMu.Unlock()
	return copied
}

func invalidateAgentProcessCache() {
	procCacheMu.Lock()
	procCacheAt = time.Time{}
	procCacheData = nil
	procCacheMu.Unlock()
}

// InstallWakeTestHooksForTesting overrides OS process probing and app wake launching for tests.
func InstallWakeTestHooksForTesting(
	psFn func(context.Context) ([]string, error),
	appFinder func(string) string,
) func() {
	prevPS := processSnapshotRunner
	prevFinder := macAppBundleFinder
	prevWake := agentWakeRunner
	invalidateAgentProcessCache()
	if psFn != nil {
		processSnapshotRunner = psFn
	}
	if appFinder != nil {
		macAppBundleFinder = appFinder
	}
	agentWakeRunner = func(_ context.Context, agentID, _, _ string) error {
		// Simulate immediate process appearance on wake during tests so no real Mac app is launched.
		prev := processSnapshotRunner
		processSnapshotRunner = func(ctx context.Context) ([]string, error) {
			lines, err := prev(ctx)
			if err != nil {
				return nil, err
			}
			if markerList, ok := desktopAppProcessMarkers[agentID]; ok && len(markerList) > 0 {
				return append(append([]string{}, lines...), markerList[0]+"Electron"), nil
			}
			return append(append([]string{}, lines...), agentID+" "+agentID), nil
		}
		return nil
	}
	return func() {
		processSnapshotRunner = prevPS
		macAppBundleFinder = prevFinder
		agentWakeRunner = prevWake
		invalidateAgentProcessCache()
	}
}

func defaultFindMacAppBundle(agentID string) string {
	switch runtime.GOOS {
	case "darwin":
		names, ok := macDesktopAppNames[agentID]
		if !ok {
			return ""
		}
		roots := []string{"/Applications"}
		if home := strings.TrimSpace(platform.Home()); home != "" {
			roots = append(roots, filepath.Join(home, "Applications"))
		}
		for _, root := range roots {
			for _, name := range names {
				candidate := filepath.Join(root, name)
				if st, err := os.Stat(candidate); err == nil && st.IsDir() {
					return candidate
				}
			}
		}
		return ""
	case "windows":
		rels, ok := windowsDesktopExeCandidates[agentID]
		if !ok {
			return ""
		}
		var roots []string
		if lad := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); lad != "" {
			roots = append(roots, lad)
		} else if home := strings.TrimSpace(platform.Home()); home != "" {
			roots = append(roots, filepath.Join(home, "AppData", "Local"))
		}
		for _, envKey := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
				roots = append(roots, v)
			}
		}
		for _, root := range roots {
			for _, rel := range rels {
				candidate := filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(rel, `\`, `/`)))
				if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
					return candidate
				}
			}
		}
		return ""
	default:
		return ""
	}
}

func wokenKey(dataDir, agentID string) string {
	return strings.TrimSpace(dataDir) + "::" + strings.ToLower(strings.TrimSpace(agentID))
}

func markAgentWoken(dataDir, agentID string, now time.Time) {
	wokenMu.Lock()
	wokenUntil[wokenKey(dataDir, agentID)] = now.Add(wokenStateTTL)
	wokenMu.Unlock()
}

func clearAgentWoken(dataDir, agentID string) {
	wokenMu.Lock()
	delete(wokenUntil, wokenKey(dataDir, agentID))
	wokenMu.Unlock()
}

func isAgentWoken(dataDir, agentID string, now time.Time) bool {
	key := wokenKey(dataDir, agentID)
	wokenMu.Lock()
	defer wokenMu.Unlock()
	until, ok := wokenUntil[key]
	if !ok {
		return false
	}
	if now.After(until) {
		delete(wokenUntil, key)
		return false
	}
	return true
}

func evaluateAgentWakeState(
	dataDir string,
	agentID string,
	installed bool,
	driverInfo *AgentDriver,
	procMap map[string]bool,
	lastEventMillis int64,
	now time.Time,
) (processRunning bool, activeOnline bool, canWake bool, wakeMode string) {
	id := strings.ToLower(strings.TrimSpace(agentID))
	processRunning = procMap[id]
	appBundle := macAppBundleFinder(id)
	driverInstalled := driverInfo != nil && driverInfo.Installed

	switch {
	case appBundle != "":
		wakeMode = "app"
		canWake = true
	case id == "opencode" && (installed || driverInstalled):
		wakeMode = "serve"
		canWake = true
	case installed || driverInstalled:
		wakeMode = "cli"
		canWake = true
	}

	woken := isAgentWoken(dataDir, id, now)
	recent := lastEventMillis > 0 && now.Sub(time.UnixMilli(lastEventMillis)) < recentActiveWindow

	switch {
	case id == "workbuddy":
		// WorkBuddy relies on its Electron desktop process to inject replies; waking launches WorkBuddy.app.
		activeOnline = processRunning
	case wakeMode == "app":
		activeOnline = processRunning || woken
	default:
		activeOnline = processRunning || woken || recent
	}
	return processRunning, activeOnline, canWake, wakeMode
}

func defaultRunAgentWake(ctx context.Context, agentID, wakeMode, appBundle string) error {
	switch wakeMode {
	case "app":
		if runtime.GOOS == "windows" {
			cmd := exec.Command(appBundle)
			platform.HideChildWindow(cmd)
			if err := cmd.Start(); err != nil {
				return err
			}
			go func() { _ = cmd.Wait() }()
			return nil
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, "open", "-g", "-a", appBundle).Run()
	case "serve":
		if agentID == "opencode" {
			bin := platform.ResolveBinary("opencode")
			if bin == "" {
				return nil
			}
			cmd := exec.Command(bin, "serve")
			cmd.Env = platform.EnvWithAugmentedPATH(nil)
			platform.HideChildWindow(cmd)
			cmd.Stdin = nil
			cmd.Stdout = nil
			cmd.Stderr = nil
			if err := cmd.Start(); err != nil {
				return nil
			}
			go func() { _ = cmd.Wait() }()
		}
		return nil
	default:
		return nil
	}
}

func (d *Deps) handleAgentWake(w http.ResponseWriter, r *http.Request) {
	if !d.RequireScope(w, r, "drive") {
		return
	}
	id := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !agentconfig.ValidID(id) {
		WriteError(w, http.StatusBadRequest, "invalid_agent_id", "agent id 不合法")
		return
	}

	ctx := ctxOf(r)
	a, knownAgent := agent.Get(ctx, id)
	var (
		installed   bool
		displayName = id
		driverInfo  *AgentDriver
	)
	if knownAgent {
		det := a.Detect(ctx)
		installed = det.Installed
		if strings.TrimSpace(a.Name()) != "" {
			displayName = a.Name()
		}
		if spec, ok := driver.SpecFor(id); ok {
			readiness := driver.CheckReadiness(spec)
			driverInfo = &AgentDriver{
				Kind:      "acp",
				Installed: readiness.Installed,
				Ready:     readiness.Ready,
			}
		}
	} else if spec, ok := driver.SpecFor(id); ok {
		readiness := driver.CheckReadiness(spec)
		installed = readiness.Installed
		if strings.TrimSpace(spec.Label) != "" {
			displayName = spec.Label
		}
		driverInfo = &AgentDriver{
			Kind:      "acp",
			Installed: readiness.Installed,
			Ready:     readiness.Ready,
		}
	}

	appBundle := macAppBundleFinder(id)
	if !knownAgent && driverInfo == nil && appBundle == "" {
		WriteError(w, http.StatusNotFound, "agent_not_found", "未找到该 Agent："+id)
		return
	}

	now := time.Now()
	procMap := liveAgentProcessMap(ctx)
	procRunning, _, canWake, wakeMode := evaluateAgentWakeState(d.DataDir, id, installed, driverInfo, procMap, 0, now)
	if !canWake {
		WriteError(w, http.StatusConflict, "agent_not_installed", displayName+" 尚未在电脑上安装，无法远程唤醒")
		return
	}

	if !procRunning {
		if err := agentWakeRunner(ctx, id, wakeMode, appBundle); err != nil {
			WriteError(w, http.StatusInternalServerError, "agent_wake_failed", "唤醒 "+displayName+" 失败："+err.Error())
			return
		}
		invalidateAgentProcessCache()
		if wakeMode == "app" || wakeMode == "serve" {
			deadline := time.Now().Add(2500 * time.Millisecond)
			for time.Now().Before(deadline) {
				invalidateAgentProcessCache()
				if liveAgentProcessMap(ctx)[id] {
					procRunning = true
					break
				}
				time.Sleep(220 * time.Millisecond)
			}
		}
	}

	markAgentWoken(d.DataDir, id, time.Now())
	invalidateAgentProcessCache()
	procMap = liveAgentProcessMap(ctx)
	procRunning = procMap[id]
	activeOnline := true
	if id == "workbuddy" && wakeMode == "app" && !procRunning {
		// Still booting Electron; keep activeOnline true for the wake response if open succeeded,
		// while reporting accurate process_running.
		activeOnline = true
	}

	d.Audit(r, "agent_wake", map[string]any{
		"target":          id,
		"wake_mode":       wakeMode,
		"process_running": procRunning,
	})

	message := displayName + " 已唤醒上线"
	switch wakeMode {
	case "app":
		if procRunning {
			message = "已在电脑后台打开 " + displayName
		} else {
			message = "已向电脑发送打开 " + displayName + " 指令，正在启动中"
		}
	case "serve":
		message = displayName + " 后台服务已唤醒就绪"
	case "cli":
		message = displayName + " 已激活上线，可随时发起任务"
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"source":          id,
		"display_name":    displayName,
		"wake_mode":       wakeMode,
		"process_running": procRunning,
		"active_online":   activeOnline,
		"message":         message,
	})
}

func (d *Deps) handleAgentSleep(w http.ResponseWriter, r *http.Request) {
	if !d.RequireScope(w, r, "drive") {
		return
	}
	id := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !agentconfig.ValidID(id) {
		WriteError(w, http.StatusBadRequest, "invalid_agent_id", "agent id 不合法")
		return
	}
	clearAgentWoken(d.DataDir, id)
	invalidateAgentProcessCache()
	procRunning := liveAgentProcessMap(ctxOf(r))[id]
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"source":          id,
		"process_running": procRunning,
		"active_online":   procRunning,
	})
}
