//go:build ((darwin && !ios) || (linux && !android) || windows) && !headless

package gui

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const desktopAvailable = true

//go:embed assets/tray/mac/*.png assets/tray/win/*.ico
var trayAssets embed.FS

type traySnapshot struct {
	online   bool
	away     bool
	needsYou int
	newItems int
	running  int
}

type trayController struct {
	baseURL string
	dataDir string
	client  *http.Client
	token   string
	log     *slog.Logger

	mu       sync.Mutex
	state    string
	snapshot traySnapshot
	tapping  bool
}

func run(ctx context.Context, options Options) error {
	if options.Log == nil {
		options.Log = slog.Default()
	}
	baseURL := strings.TrimRight(strings.TrimSpace(options.URL), "/")
	if baseURL == "" {
		return fmt.Errorf("desktop shell needs the local service URL")
	}

	// Scope the native instance to its service/profile, so an isolated preview
	// can coexist with the installed app without stealing its window.
	profile, _ := filepath.Abs(options.DataDir)
	if resolved, err := filepath.EvalSymlinks(profile); err == nil {
		profile = resolved
	}
	instanceHash := sha256.Sum256([]byte(profile + "|" + baseURL))
	type shellWindows struct{ main, panel *application.WebviewWindow }
	var windows atomic.Pointer[shellWindows]
	var appRef atomic.Pointer[application.App]

	showMain := func(targetPath string) {
		application.InvokeAsync(func() {
			w := windows.Load()
			if w == nil {
				return
			}
			w.panel.Hide()
			cleanPath := strings.TrimSpace(targetPath)
			if cleanPath != "" && strings.HasPrefix(cleanPath, "/console") && !strings.HasPrefix(cleanPath, "//") {
				pathJSON, _ := json.Marshal(cleanPath)
				fullJSON, _ := json.Marshal(baseURL + cleanPath)
				js := fmt.Sprintf(
					`(function(){var p=%s,u=%s;try{if(location.pathname.indexOf("/console")===0){if(location.pathname+location.search!==p){history.pushState(null,"",p);window.dispatchEvent(new PopStateEvent("popstate"));}}else{location.replace(u);}}catch(_){location.href=u;}})();`,
					string(pathJSON),
					string(fullJSON),
				)
				w.main.ExecJS(js)
			}
			if a := appRef.Load(); a != nil {
				a.Show()
			}
			if w.main.IsMinimised() {
				w.main.UnMinimise()
				w.main.Restore()
			}
			w.main.Show()
			w.main.Focus()
			if runtime.GOOS == "darwin" {
				// Accessory (LSUIElement) apps on macOS Sonoma/Sequoia need an explicit
				// window-level bump to surface reliably above fullscreen/active apps.
				w.main.SetAlwaysOnTop(true)
				w.main.Focus()
				time.AfterFunc(120*time.Millisecond, func() {
					application.InvokeAsync(func() {
						w.main.SetAlwaysOnTop(false)
						w.main.Focus()
					})
				})
			}
		})
	}
	app := application.New(application.Options{
		Name:        "Homing",
		Description: "跨 Agent 会话管理",
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
		Logger: options.Log,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               fmt.Sprintf("dev.homing.desktop.%x", instanceHash[:12]),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { showMain("") },
		},
	})
	appRef.Store(app)

	devTools := options.Version == "" || options.Version == "dev" || os.Getenv("HOMING_DEVTOOLS") == "1"
	mainURL := baseURL + "/console/"
	if v := strings.TrimSpace(options.Version); v != "" && v != "dev" {
		mainURL += "?desktop_version=" + url.QueryEscape(v)
	}
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Homing",
		URL:              mainURL,
		Width:            1320,
		Height:           860,
		MinWidth:         820,
		MinHeight:        560,
		Hidden:           options.StartHidden,
		BackgroundColour: application.NewRGB(15, 23, 42),
		DevToolsEnabled:  devTools,
	})
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		mainWindow.Hide()
		event.Cancel()
	})

	panel := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "tray-panel",
		Title:            "Homing",
		URL:              baseURL + "/console/tray",
		Width:            360,
		Height:           620,
		Frameless:        true,
		AlwaysOnTop:      true,
		Hidden:           true,
		DisableResize:    true,
		HideOnEscape:     true,
		HideOnFocusLost:  true,
		BackgroundColour: application.NewRGB(15, 23, 42),
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 1,
			Backdrop:                application.MacBackdropTranslucent,
		},
	})
	panel.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		panel.Hide()
		event.Cancel()
	})

	windows.Store(&shellWindows{main: mainWindow, panel: panel})
	if runtime.GOOS == "darwin" {
		// Wails' default reopen listener shows every hidden window, including
		// our tray panel. Finder launches should bring back only the main window.
		app.Event.RegisterApplicationEventHook(events.Mac.ApplicationShouldHandleReopen, func(event *application.ApplicationEvent) {
			showMain("")
			event.Cancel()
		})
	}

	tray := app.SystemTray.New()
	tray.AttachWindow(panel).WindowOffset(4)
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(mustAsset("assets/tray/mac/ok@2x.png"))
	} else {
		tray.SetIcon(mustAsset("assets/tray/win/ok.ico"))
	}

	menu := app.NewMenu()
	menu.Add("Homing " + options.Version).SetEnabled(false)
	menu.AddSeparator()
	openMain := menu.Add("打开主窗口")
	openMain.OnClick(func(*application.Context) {
		showMain("")
	})
	openConsole := menu.Add("在浏览器打开控制台")
	openConsole.OnClick(func(*application.Context) {
		_ = openExternal(baseURL + "/console/")
	})
	menu.AddSeparator()
	awayItem := menu.AddCheckbox("离席监听（向手机推送新消息）", false)
	awayItem.OnClick(func(ctx *application.Context) {
		checked := ctx.ClickedMenuItem().Checked()
		if err := setAway(baseURL, tokenFromDataDir(options.DataDir), checked); err != nil {
			options.Log.Warn("toggle away from tray", "err", err)
			awayItem.SetChecked(!checked)
		}
	})
	pauseItem := menu.AddCheckbox("暂停推送", false)
	pauseItem.OnClick(func(ctx *application.Context) {
		checked := ctx.ClickedMenuItem().Checked()
		if err := setPause(baseURL, tokenFromDataDir(options.DataDir), checked); err != nil {
			options.Log.Warn("toggle push pause from tray", "err", err)
			pauseItem.SetChecked(!checked)
		}
	})
	var keepAwakeItem *application.MenuItem
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		label := "防止电脑休眠"
		if runtime.GOOS == "darwin" {
			label = "防止 Mac 休眠"
		}
		keepAwakeItem = menu.AddCheckbox(label, false)
		keepAwakeItem.OnClick(func(ctx *application.Context) {
			checked := ctx.ClickedMenuItem().Checked()
			if err := setKeepAwake(baseURL, tokenFromDataDir(options.DataDir), checked); err != nil {
				options.Log.Warn("toggle keepawake from tray", "err", err)
				keepAwakeItem.SetChecked(!checked)
			}
		})
	}
	menu.AddSeparator()
	statusItem := menu.Add("正在连接本机服务…").SetEnabled(false)
	menu.AddSeparator()
	menu.Add("退出 Homing").OnClick(func(*application.Context) {
		app.Quit()
	})
	tray.SetMenu(menu)

	controller := &trayController{
		baseURL: baseURL,
		dataDir: options.DataDir,
		client:  &http.Client{Timeout: 3 * time.Second},
		token:   tokenFromDataDir(options.DataDir),
		log:     options.Log,
		state:   "ok",
	}
	// 点击托盘图标：先播一遍「轻点」鸽子动画（pigeon_v2 tap，20 帧约 0.6 秒），
	// 同时照常弹出面板。
	tray.OnClick(func() {
		controller.playTap(tray)
		tray.ToggleWindow()
	})
	go controller.poll(ctx, app, tray, awayItem, pauseItem, keepAwakeItem, statusItem)
	go controller.watchWindowStream(ctx, showMain)

	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			app.Quit()
		}()
	}
	return app.Run()
}

func (c *trayController) poll(
	ctx context.Context,
	app *application.App,
	tray *application.SystemTray,
	awayItem *application.MenuItem,
	pauseItem *application.MenuItem,
	keepAwakeItem *application.MenuItem,
	statusItem *application.MenuItem,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		c.update(tray, awayItem, pauseItem, keepAwakeItem, statusItem)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *trayController) update(
	tray *application.SystemTray,
	awayItem *application.MenuItem,
	pauseItem *application.MenuItem,
	keepAwakeItem *application.MenuItem,
	statusItem *application.MenuItem,
) {
	snapshot, err := c.fetchSnapshot()
	if err != nil {
		c.log.Debug("tray snapshot", "err", err)
		c.apply(tray, statusItem, traySnapshot{}, "off")
		return
	}
	state := "ok"
	if snapshot.needsYou > 0 {
		state = "wait"
	} else if snapshot.newItems > 0 {
		state = "new"
	}
	c.apply(tray, statusItem, snapshot, state)

	paused := false
	if body, err := c.get("/api/push/pause"); err == nil {
		var payload struct {
			Paused bool `json:"paused"`
		}
		if json.Unmarshal(body, &payload) == nil {
			paused = payload.Paused
		}
	}

	keepActive := false
	if keepAwakeItem != nil {
		if body, err := c.get("/api/keepawake"); err == nil {
			var payload struct {
				Active bool `json:"active"`
			}
			if json.Unmarshal(body, &payload) == nil {
				keepActive = payload.Active
			}
		}
	}
	application.InvokeAsync(func() {
		awayItem.SetChecked(snapshot.away)
		pauseItem.SetChecked(paused)
		if keepAwakeItem != nil {
			keepAwakeItem.SetChecked(keepActive)
		}
	})
}

func (c *trayController) apply(
	tray *application.SystemTray,
	statusItem *application.MenuItem,
	snapshot traySnapshot,
	state string,
) {
	c.mu.Lock()
	changed := c.state != state || c.snapshot != snapshot
	c.state = state
	c.snapshot = snapshot
	tapping := c.tapping
	c.mu.Unlock()
	if !changed {
		return
	}
	var label, tooltip string
	switch {
	case !snapshot.online:
		label = "本机服务未响应"
		tooltip = "Homing · 本机服务未响应"
	case snapshot.needsYou > 0:
		label = fmt.Sprintf("%d 件事等你处理 · %d 个运行中", snapshot.needsYou, snapshot.running)
		tooltip = fmt.Sprintf("Homing · %d 件事等你处理", snapshot.needsYou)
	case snapshot.newItems > 0:
		label = fmt.Sprintf("%d 个新动态 · %d 个运行中", snapshot.newItems, snapshot.running)
		tooltip = fmt.Sprintf("Homing · %d 个新动态", snapshot.newItems)
	default:
		label = fmt.Sprintf("已连接 · %d 个运行中", snapshot.running)
		tooltip = "Homing · 已连接"
	}
	application.InvokeAsync(func() {
		if !tapping {
			setTrayFrame(tray, state)
		}
		statusItem.SetLabel(label)
		tray.SetTooltip(tooltip)
	})
}

// watchWindowStream subscribes to GET /api/desktop/window/stream so clicks inside
// the HTTP-served /console/tray popover ("打开主窗口", "配对手机", session rows)
// immediately raise the native Wails main window—even when homingd runs in launchd.
func (c *trayController) watchWindowStream(ctx context.Context, onOpen func(path string)) {
	streamClient := &http.Client{}
	for {
		if ctx.Err() != nil {
			return
		}
		tok := strings.TrimSpace(c.token)
		if c.dataDir != "" {
			if fresh := tokenFromDataDir(c.dataDir); fresh != "" {
				tok = fresh
				c.token = fresh
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/desktop/window/stream", nil)
		if err == nil {
			if tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			resp, doErr := streamClient.Do(req)
			if doErr == nil {
				if resp.StatusCode == http.StatusOK {
					scanner := bufio.NewScanner(resp.Body)
					for scanner.Scan() {
						line := scanner.Text()
						if !strings.HasPrefix(line, "data:") {
							continue
						}
						raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
						var payload struct {
							Action string `json:"action"`
							Path   string `json:"path"`
						}
						if json.Unmarshal([]byte(raw), &payload) == nil && payload.Action == "open" {
							onOpen(payload.Path)
						}
					}
				}
				_ = resp.Body.Close()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func (c *trayController) fetchSnapshot() (traySnapshot, error) {
	body, err := c.get("/api/home")
	if err != nil {
		return traySnapshot{}, err
	}
	var payload struct {
		Away struct {
			Enabled bool `json:"enabled"`
		} `json:"away"`
		Counts struct {
			NeedsYou int `json:"needs_you"`
			AwayNew  int `json:"away_new"`
			Running  int `json:"running"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return traySnapshot{}, err
	}
	return traySnapshot{
		online: true, away: payload.Away.Enabled,
		needsYou: payload.Counts.NeedsYou,
		newItems: payload.Counts.AwayNew,
		running:  payload.Counts.Running,
	}, nil
}

func (c *trayController) get(path string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func setAway(baseURL, token string, enabled bool) error {
	body, _ := json.Marshal(map[string]any{"enabled": enabled, "source": "tray"})
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/away", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
		return fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	return nil
}

func setPause(baseURL, token string, paused bool) error {
	var (
		method string
		body   io.Reader
	)
	if paused {
		method = http.MethodPost
		raw, _ := json.Marshal(map[string]any{"minutes": 60, "reason": "托盘暂停"})
		body = bytes.NewReader(raw)
	} else {
		method = http.MethodDelete
	}
	request, err := http.NewRequest(method, baseURL+"/api/push/pause", body)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
		return fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	return nil
}

func setKeepAwake(baseURL, token string, enabled bool) error {
	body, _ := json.Marshal(map[string]any{"enabled": enabled})
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/keepawake", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
		return fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	return nil
}

func tokenFromDataDir(dataDir string) string {
	var config struct {
		Token string `json:"token"`
	}
	paths := platform.NewPaths(dataDir)
	if !platform.ReadJSON(paths.Config, &config) {
		return ""
	}
	return strings.TrimSpace(config.Token)
}

// setTrayFrame shows one tray image: a state (ok/new/wait/error/off) or a
// tap animation frame (tap-01 … tap-20).
func setTrayFrame(tray *application.SystemTray, name string) {
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(mustAsset("assets/tray/mac/" + name + "@2x.png"))
	} else {
		tray.SetIcon(mustAsset("assets/tray/win/" + name + ".ico"))
	}
}

// playTap plays the 20-frame tap animation once, then restores whatever
// state the poller last saw. A second click while it plays is ignored.
func (c *trayController) playTap(tray *application.SystemTray) {
	c.mu.Lock()
	if c.tapping {
		c.mu.Unlock()
		return
	}
	c.tapping = true
	c.mu.Unlock()
	go func() {
		for frame := 1; frame <= 20; frame++ {
			name := fmt.Sprintf("tap-%02d", frame)
			application.InvokeAsync(func() {
				setTrayFrame(tray, name)
			})
			time.Sleep(30 * time.Millisecond)
		}
		c.mu.Lock()
		c.tapping = false
		state := c.state
		c.mu.Unlock()
		if state == "" {
			state = "ok"
		}
		application.InvokeAsync(func() {
			setTrayFrame(tray, state)
		})
	}()
}

func mustAsset(name string) []byte {
	raw, err := trayAssets.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return raw
}

func openExternal(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Start()
}
