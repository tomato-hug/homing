//go:build ((darwin && !ios) || (linux && !android) || windows) && !headless

package gui

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
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
	client  *http.Client
	token   string
	log     *slog.Logger

	mu       sync.Mutex
	state    string
	snapshot traySnapshot
}

func run(ctx context.Context, options Options) error {
	if options.Log == nil {
		options.Log = slog.Default()
	}
	baseURL := strings.TrimRight(strings.TrimSpace(options.URL), "/")
	if baseURL == "" {
		return fmt.Errorf("desktop shell needs the local service URL")
	}

	app := application.New(application.Options{
		Name:        "Homing",
		Description: "跨 Agent 会话管理",
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
		Logger: options.Log,
	})

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Homing",
		URL:              baseURL + "/console/",
		Width:            1320,
		Height:           860,
		MinWidth:         760,
		MinHeight:        560,
		BackgroundColour: application.NewRGB(15, 23, 42),
		DevToolsEnabled:  true,
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

	tray := app.SystemTray.New()
	tray.AttachWindow(panel).WindowOffset(4)
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(mustAsset("assets/tray/mac/ok@2x.png"))
	} else {
		tray.SetIcon(mustAsset("assets/tray/win/ok.ico"))
	}

	menu := app.NewMenu()
	openMain := menu.Add("打开 Homing")
	openMain.OnClick(func(*application.Context) {
		mainWindow.Show().Focus()
	})
	openConsole := menu.Add("在浏览器打开控制台")
	openConsole.OnClick(func(*application.Context) {
		_ = openExternal(baseURL + "/console/")
	})
	menu.AddSeparator()
	awayItem := menu.AddCheckbox("离开模式", false)
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
	menu.AddSeparator()
	statusItem := menu.Add("正在连接本机服务…").SetEnabled(false)
	menu.AddSeparator()
	menu.Add("退出 Homing").OnClick(func(*application.Context) {
		app.Quit()
	})
	tray.SetMenu(menu)

	controller := &trayController{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 3 * time.Second},
		token:   tokenFromDataDir(options.DataDir),
		log:     options.Log,
		state:   "ok",
	}
	go controller.poll(ctx, app, tray, awayItem, pauseItem, statusItem)

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
	statusItem *application.MenuItem,
) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		c.update(tray, awayItem, pauseItem, statusItem)
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
	awayItem.SetChecked(snapshot.away)

	paused := false
	if body, err := c.get("/api/push/pause"); err == nil {
		var payload struct {
			Paused bool `json:"paused"`
		}
		if json.Unmarshal(body, &payload) == nil {
			paused = payload.Paused
		}
	}
	pauseItem.SetChecked(paused)
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
	c.mu.Unlock()
	if !changed {
		return
	}
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(mustAsset("assets/tray/mac/" + state + "@2x.png"))
	} else {
		tray.SetIcon(mustAsset("assets/tray/win/" + state + ".ico"))
	}
	switch {
	case !snapshot.online:
		statusItem.SetLabel("本机服务未响应")
		tray.SetTooltip("Homing · 本机服务未响应")
	case snapshot.needsYou > 0:
		statusItem.SetLabel(fmt.Sprintf("%d 件事等你处理 · %d 个运行中", snapshot.needsYou, snapshot.running))
		tray.SetTooltip(fmt.Sprintf("Homing · %d 件事等你处理", snapshot.needsYou))
	case snapshot.newItems > 0:
		statusItem.SetLabel(fmt.Sprintf("%d 个新动态 · %d 个运行中", snapshot.newItems, snapshot.running))
		tray.SetTooltip(fmt.Sprintf("Homing · %d 个新动态", snapshot.newItems))
	default:
		statusItem.SetLabel(fmt.Sprintf("已连接 · %d 个运行中", snapshot.running))
		tray.SetTooltip("Homing · 已连接")
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
	body, _ := json.Marshal(map[string]any{"paused": paused})
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/push/pause", bytes.NewReader(body))
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
