// Package tui implements the small terminal client required by `homing tui`.
// It deliberately talks to the same local HTTP API as the browser console and
// never reads agent files directly, so it remains a thin, read-mostly client.
package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/cli"
	"github.com/tomato-hug/homing/internal/platform"
	"golang.org/x/term"
)

func init() {
	cli.Register(cli.Command{
		Name:    "tui",
		Summary: "在终端里查看收件箱、会话与事件（q 退出）",
		Run:     run,
	})
}

type client struct {
	baseURL string
	token   string
	http    httpDoer
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	url := fs.String("url", "http://127.0.0.1:18787", "本机服务地址")
	dataDir := fs.String("data-dir", "", "数据目录（默认使用 Homing 的系统目录）")
	token := fs.String("token", os.Getenv("HOMING_TOKEN"), "Bearer token（默认读取 config.json）")
	once := fs.Bool("once", false, "只打印一次快照，不进入交互模式")
	interval := fs.Duration("interval", 2*time.Second, "自动刷新间隔")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dataDir == "" {
		d, err := platform.DataDir()
		if err != nil {
			return err
		}
		*dataDir = d
	}
	if *token == "" {
		*token = readToken(*dataDir)
	}
	if *token == "" {
		return errors.New("找不到本机 token：请启动 homing serve，或设置 HOMING_TOKEN")
	}
	if *interval < 500*time.Millisecond {
		return errors.New("--interval 不能短于 500ms")
	}

	app := &application{
		client: &client{
			baseURL: strings.TrimRight(*url, "/"),
			token:   *token,
			http:    &http.Client{Timeout: 5 * time.Second},
		},
	}
	if err := app.refresh(ctx); err != nil {
		if *once || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return err
		}
	}
	if *once || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprint(os.Stdout, app.render(0, 0))
		return nil
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[?25l")
	defer fmt.Fprint(os.Stdout, "\x1b[?25h\x1b[?1049l")

	app.interactive = true
	keys := make(chan byte, 16)
	keyErr := make(chan error, 1)
	go readKeys(os.Stdin, keys, keyErr)

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		width, height, err := term.GetSize(int(os.Stdout.Fd()))
		if err != nil || width <= 0 {
			width, height = 100, 32
		}
		app.draw(width, height)
		select {
		case <-ctx.Done():
			return nil
		case err := <-keyErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case key := <-keys:
			switch key {
			case 'q', 3:
				return nil
			case '1':
				app.tab = tabHome
				app.selected = 0
			case '2':
				app.tab = tabSessions
				app.selected = 0
			case '3':
				app.tab = tabEvents
				app.selected = 0
			case '\t', 'l':
				app.tab = (app.tab + 1) % 3
				app.selected = 0
			case 'h':
				app.tab = (app.tab + 2) % 3
				app.selected = 0
			case 'j':
				app.move(1)
			case 'k':
				app.move(-1)
			case 'd':
				app.move(10)
			case 'u':
				app.move(-10)
			case 'g':
				app.selected = 0
			case 'G':
				app.selected = app.rows() - 1
			case 'a':
				app.toggleAway(ctx)
			case 'r':
				_ = app.refresh(ctx)
			}
		case <-ticker.C:
			_ = app.refresh(ctx)
		}
	}
}

func readToken(dataDir string) string {
	var cfg struct {
		Token string `json:"token"`
	}
	raw, err := os.ReadFile(platform.NewPaths(dataDir).Config)
	if err != nil {
		return ""
	}
	_ = json.Unmarshal(raw, &cfg)
	return strings.TrimSpace(cfg.Token)
}

func readKeys(r io.Reader, keys chan<- byte, failed chan<- error) {
	buffer := make([]byte, 16)
	for {
		n, err := r.Read(buffer)
		if n > 0 {
			for i := 0; i < n; i++ {
				if buffer[i] == 0x1b && i+2 < n && (buffer[i+1] == '[' || buffer[i+1] == 'O') {
					switch buffer[i+2] {
					case 'A':
						keys <- 'k'
						i += 2
						continue
					case 'B':
						keys <- 'j'
						i += 2
						continue
					case 'C':
						keys <- 'l'
						i += 2
						continue
					case 'D':
						keys <- 'h'
						i += 2
						continue
					case '5':
						if i+3 < n && buffer[i+3] == '~' {
							keys <- 'u'
							i += 3
							continue
						}
					case '6':
						if i+3 < n && buffer[i+3] == '~' {
							keys <- 'd'
							i += 3
							continue
						}
					}
				}
				keys <- buffer[i]
			}
		}
		if err != nil {
			failed <- err
			return
		}
	}
}

const (
	tabHome = iota
	tabSessions
	tabEvents
)

type application struct {
	client      *client
	tab         int
	selected    int
	interactive bool
	offline     string

	home     homePayload
	sessions []sessionSummary
	events   []eventRecord
}

type homePayload struct {
	Away struct {
		Enabled bool `json:"enabled"`
		Current *struct {
			StartedAt string `json:"started_at"`
		} `json:"current"`
	} `json:"away"`
	Attention []struct {
		Reason    string         `json:"reason"`
		Headline  string         `json:"headline"`
		Summary   string         `json:"summary"`
		At        string         `json:"at"`
		Session   sessionSummary `json:"session"`
		EventID   int64          `json:"event_id"`
		Unseen    int            `json:"unseen_count"`
		SinceAway bool           `json:"since_away"`
	} `json:"attention"`
	Running []sessionSummary `json:"running"`
	Counts  struct {
		NeedsYou int `json:"needs_you"`
		AwayNew  int `json:"away_new"`
		Running  int `json:"running"`
		Earlier  int `json:"earlier"`
	} `json:"counts"`
}

type sessionSummary struct {
	SessionID   string `json:"session_id"`
	Source      string `json:"source"`
	Title       string `json:"title"`
	Project     string `json:"project"`
	Cwd         string `json:"cwd"`
	LastTS      string `json:"last_ts"`
	LastPreview string `json:"last_preview"`
	Running     bool   `json:"running"`
	Read        bool   `json:"read"`
}

type eventRecord struct {
	ID        int64  `json:"id"`
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Ts        string `json:"ts"`
}

func (a *application) refresh(ctx context.Context) error {
	var (
		home     homePayload
		sessions struct {
			Sessions []sessionSummary `json:"sessions"`
		}
		events []eventRecord
	)
	if err := a.client.get(ctx, "/api/home", &home); err != nil {
		a.offline = err.Error()
		return err
	}
	if err := a.client.get(ctx, "/api/sessions?limit=100", &sessions); err != nil {
		a.offline = err.Error()
		return err
	}
	if err := a.client.get(ctx, "/api/events?since=0", &events); err != nil {
		a.offline = err.Error()
		return err
	}
	a.home = home
	a.sessions = sessions.Sessions
	a.events = events
	a.offline = ""
	a.clampSelection()
	return nil
}

func (a *application) toggleAway(ctx context.Context) {
	enabled := !a.home.Away.Enabled
	body, _ := json.Marshal(map[string]any{"enabled": enabled})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.client.baseURL+"/api/away", bytes.NewReader(body))
	if err != nil {
		a.offline = err.Error()
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+a.client.token)
	response, err := a.client.http.Do(request)
	if err != nil {
		a.offline = err.Error()
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		a.offline = strings.TrimSpace(string(raw))
		return
	}
	_ = a.refresh(ctx)
}

func (a *application) rows() int {
	switch a.tab {
	case tabHome:
		return len(a.home.Attention)
	case tabSessions:
		return len(a.sessions)
	default:
		return len(a.events)
	}
}

func (a *application) move(delta int) {
	a.selected += delta
	a.clampSelection()
}

func (a *application) clampSelection() {
	rows := a.rows()
	if rows == 0 {
		a.selected = 0
		return
	}
	if a.selected >= rows {
		a.selected = rows - 1
	}
	if a.selected < 0 {
		a.selected = 0
	}
}

func (a *application) draw(width, height int) {
	fmt.Fprint(os.Stdout, "\x1b[H")
	fmt.Fprint(os.Stdout, a.render(width, height))
	fmt.Fprint(os.Stdout, "\x1b[J")
}

func (a *application) render(width, height int) string {
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 32
	}
	var out strings.Builder
	line := func(text string) {
		out.WriteString(ansiTruncate(text, width))
		out.WriteString("\x1b[0m\x1b[K\r\n")
	}

	connection := "已连接"
	if a.offline != "" {
		connection = "连接失败"
	}
	away := "离开模式关闭"
	if a.home.Away.Enabled {
		away = "离开中"
	}
	line("\x1b[1;36mHoming TUI\x1b[0m  " + dim(a.client.baseURL) + "  " + connection + " · " + away)
	tabLabel := func(idx int, label string) string {
		if a.tab == idx {
			return "\x1b[1;36m" + label + "\x1b[0m"
		}
		return label
	}
	line(fmt.Sprintf(
		"%s  %d 等你 · %d 新动态  %s  %d  %s  %d",
		tabLabel(tabHome, "[1] 总览"),
		a.home.Counts.NeedsYou, a.home.Counts.AwayNew,
		tabLabel(tabSessions, "[2] 会话"), len(a.sessions),
		tabLabel(tabEvents, "[3] 事件"), len(a.events),
	))
	if a.offline != "" {
		line("\x1b[31m" + truncate(a.offline, width-1) + "\x1b[0m")
	}
	line(strings.Repeat("─", min(width, 120)))

	available := height - 7
	if available < 4 {
		available = 4
	}
	switch a.tab {
	case tabHome:
		a.renderHome(&out, width, available, line)
	case tabSessions:
		a.renderSessions(&out, width, available, line)
	default:
		a.renderEvents(&out, width, available, line)
	}
	line("")
	line("\x1b[2m1/2/3/Tab 切换 · ↑/↓/j/k 移动 · g/G 首尾 · a 离开模式 · r 刷新 · q 退出\x1b[0m")
	return out.String()
}

func (a *application) renderHome(out *strings.Builder, width, available int, line func(string)) {
	line(fmt.Sprintf(
		"\x1b[1m总览\x1b[0m  %d 需要你 · %d 运行中 · %d 较早",
		a.home.Counts.NeedsYou, a.home.Counts.Running, a.home.Counts.Earlier,
	))
	if len(a.home.Attention) == 0 {
		line(dim("现在没有需要你决定的事。"))
		return
	}
	start, end := visibleRange(a.selected, len(a.home.Attention), available)
	for index := start; index < end; index++ {
		item := a.home.Attention[index]
		prefix := "  "
		if index == a.selected {
			prefix = "\x1b[7m> "
		}
		mark := "·"
		switch item.Reason {
		case "approval":
			mark = "!"
		case "needs_reply":
			mark = "?"
		case "error":
			mark = "x"
		case "done":
			mark = "✓"
		case "collab_result":
			mark = "@"
		}
		text := fmt.Sprintf("%s%s %-12s %s", prefix, mark, shortSource(item.Session.Source), plain(item.Headline))
		line(text + "\x1b[0m")
		if index == a.selected && item.Summary != "" {
			line("    " + dim(truncate(plain(item.Summary), max(10, width-4))))
		}
	}
	if end < len(a.home.Attention) {
		line(dim(fmt.Sprintf("… 还有 %d 条", len(a.home.Attention)-end)))
	}
}

func (a *application) renderSessions(out *strings.Builder, width, available int, line func(string)) {
	if len(a.sessions) == 0 {
		line(dim("还没有会话。"))
		return
	}
	line("\x1b[1m会话\x1b[0m  j/k 选择")
	start, end := visibleRange(a.selected, len(a.sessions), available)
	for index := start; index < end; index++ {
		session := a.sessions[index]
		prefix := "  "
		if index == a.selected {
			prefix = "\x1b[7m> "
		}
		status := ""
		if session.Running {
			status = " ●"
		}
		line(fmt.Sprintf(
			"%s%-12s %-36s %8s%s\x1b[0m",
			prefix,
			shortSource(session.Source),
			plain(truncate(firstNonEmpty(session.Title, session.SessionID), 36)),
			shortTime(session.LastTS),
			status,
		))
		if index == a.selected {
			meta := firstNonEmpty(session.Project, session.Cwd, "未分组")
			if session.LastPreview != "" {
				meta += " · " + plain(session.LastPreview)
			}
			line("    " + dim(truncate(meta, max(10, width-4))))
		}
	}
	if end < len(a.sessions) {
		line(dim(fmt.Sprintf("… 还有 %d 条（会话页默认取最近 100 条）", len(a.sessions)-end)))
	}
}

func (a *application) renderEvents(out *strings.Builder, width, available int, line func(string)) {
	if len(a.events) == 0 {
		line(dim("还没有事件。"))
		return
	}
	line("\x1b[1m最近事件\x1b[0m  最新的在最上面")
	ordered := make([]eventRecord, len(a.events))
	copy(ordered, a.events)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID > ordered[j].ID })
	start, end := visibleRange(a.selected, len(ordered), available)
	for index := start; index < end; index++ {
		item := ordered[index]
		prefix := "  "
		if index == a.selected {
			prefix = "\x1b[7m> "
		}
		line(fmt.Sprintf(
			"%s#%-5d %-12s %-15s %s\x1b[0m",
			prefix, item.ID, shortSource(item.Source), plain(item.Kind),
			plain(truncate(firstNonEmpty(item.Title, item.Text), max(10, width-45))),
		))
		if index == a.selected {
			line("    " + dim(truncate(plain(firstNonEmpty(item.Text, item.SessionID)), max(10, width-4))))
		}
	}
}

func (c *client) get(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("%s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target)
}

func visibleRange(selected, total, height int) (int, int) {
	if total <= height {
		return 0, total
	}
	start := selected - height/2
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > total {
		end = total
		start = max(0, end-height)
	}
	return start, end
}

func shortSource(source string) string {
	switch source {
	case "workbuddy":
		return "WorkBuddy"
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	}
	if source == "" {
		return "unknown"
	}
	return source
}

func shortTime(value string) string {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Local().Format("01-02 15:04")
		}
	}
	return "—"
}

func plain(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.Join(strings.Fields(value), " ")
}

func ansiTruncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	used := 0
	runes := []rune(value)
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && ((runes[j] >= '0' && runes[j] <= '9') || runes[j] == ';') {
				j++
			}
			if j < len(runes) && ((runes[j] >= 'a' && runes[j] <= 'z') || (runes[j] >= 'A' && runes[j] <= 'Z')) {
				for k := i; k <= j; k++ {
					out.WriteRune(runes[k])
				}
				i = j
				continue
			}
		}
		charWidth := runeWidth(runes[i])
		if used+charWidth > width {
			break
		}
		out.WriteRune(runes[i])
		used += charWidth
	}
	return out.String()
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	used := 0
	for _, char := range value {
		charWidth := runeWidth(char)
		if used+charWidth > width {
			break
		}
		out.WriteRune(char)
		used += charWidth
	}
	return out.String()
}

func runeWidth(char rune) int {
	switch {
	case char == 0:
		return 0
	case char < 32:
		return 0
	case char >= 0x1100 && (char <= 0x115f ||
		char == 0x2329 || char == 0x232a ||
		(char >= 0x2e80 && char <= 0xa4cf) ||
		(char >= 0xac00 && char <= 0xd7a3) ||
		(char >= 0xf900 && char <= 0xfaff) ||
		(char >= 0xfe10 && char <= 0xfe19) ||
		(char >= 0xfe30 && char <= 0xfe6f) ||
		(char >= 0xff00 && char <= 0xff60) ||
		(char >= 0xffe0 && char <= 0xffe6) ||
		(char >= 0x1f300 && char <= 0x1faff) ||
		(char >= 0x20000 && char <= 0x3fffd)):
		return 2
	default:
		return 1
	}
}

func dim(value string) string { return "\x1b[2m" + value + "\x1b[0m" }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
