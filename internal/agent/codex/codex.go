// Package codex reads Codex CLI sessions: ~/.codex/sessions/<年>/<月>/<日>/rollout-*.jsonl
// plus the title index ~/.codex/session_index.jsonl.
//
// Ported from relay/scanners/codex.mjs. Reply injection runs
// `codex exec resume` in a child process (argument array, never a shell string),
// one turn at a time per thread.
//
// Owner: WorkBuddy.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/platform"
)

const id = "codex"

// Icon is the monochrome 16×16 glyph shared with the console.
const Icon = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><path d="M8 1.8 13.4 4.9v6.2L8 14.2 2.6 11.1V4.9z"/><path d="M5.2 6.4 8 8l2.8-1.6M8 8v3.6"/></svg>`

// ReplyTimeout kills a runaway `codex exec resume` after 10 minutes.
const ReplyTimeout = 10 * time.Minute

func init() { agent.Register(Agent{}) }

// Agent implements agent.Agent, agent.Replier and agent.ItemReader.
type Agent struct{}

func (Agent) ID() string                   { return id }
func (Agent) Name() string                 { return "Codex" }
func (Agent) Icon() string                 { return Icon }
func (Agent) PushSignal() agent.PushSignal { return agent.PushHook }
func (Agent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Reply: true, Sessions: true}
}
func (Agent) home() string                              { return filepath.Join(platform.Home(), ".codex") }
func (Agent) Meta(sessionID, cwdHint string) agent.Meta { return metaFor(sessionID, cwdHint) }

// Detect reports ~/.codex plus the version file Codex keeps there.
func (a Agent) Detect(context.Context) agent.Detection {
	dir := a.home()
	det := agent.Detection{Installed: dirExists(dir)}
	var version struct {
		Version       string `json:"version"`
		LatestVersion string `json:"latest_version"`
	}
	if platform.ReadJSON(filepath.Join(dir, "version.json"), &version) {
		v := version.Version
		if v == "" {
			v = version.LatestVersion
		}
		if v != "" {
			det.Version = &v
		}
	}
	return det
}

// ListSessions walks the last 45 day-folders, newest first, and returns one row
// per rollout file (session id comes from the session_meta line).
func (Agent) ListSessions(_ context.Context, limit int) ([]agent.Session, error) {
	titles, updated := titleIndexFull()
	type row struct {
		session agent.Session
		mtime   time.Time
	}
	var rows []row
	seen := map[string]bool{}
	for _, file := range rolloutFiles(0) {
		stat, err := os.Stat(file)
		if err != nil {
			continue
		}
		sessionID := ""
		head := scanlib.Slice(file, 0, 64*1024)
		for _, line := range strings.Split(head, "\n") {
			if !strings.Contains(line, `"session_meta"`) {
				continue
			}
			if rec := parseJSON(line); rec != nil {
				if payload, ok := rec["payload"].(map[string]any); ok {
					if v, ok := payload["id"].(string); ok {
						sessionID = v
					}
				}
			}
			break
		}
		if sessionID == "" {
			if m := rolloutName.FindStringSubmatch(filepath.Base(file)); m != nil {
				sessionID = m[1]
			}
		}
		if sessionID == "" || seen[sessionID] {
			continue
		}
		seen[sessionID] = true
		title, titleSource := "", "first_prompt"
		if name, ok := titles[sessionID]; ok && name != "" {
			title, titleSource = name, "agent"
		}
		rows = append(rows, row{
			session: agent.Session{
				SessionID:   sessionID,
				Source:      id,
				Title:       title,
				TitleSource: titleSource,
				LastTS:      scanlib.ISO(stat.ModTime()),
				LastPreview: readRolloutMeta(file).lastPreview,
			},
			mtime: stat.ModTime(),
		})
	}
	// Threads the index still remembers after their rollout file was pruned. The
	// Node relay lists these too, so the console keeps showing old conversations.
	for sessionID, name := range titles {
		if seen[sessionID] {
			continue
		}
		seen[sessionID] = true
		ts := updated[sessionID]
		mtime, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			mtime = time.Time{}
		}
		rows = append(rows, row{
			session: agent.Session{
				SessionID:   sessionID,
				Source:      id,
				Title:       name,
				TitleSource: "agent",
				LastTS:      ts,
			},
			mtime: mtime,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].mtime.After(rows[j].mtime) })
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]agent.Session, len(rows))
	for i, r := range rows {
		out[i] = r.session
	}
	return out, nil
}

// ---- metadata ------------------------------------------------------------

var rolloutName = regexp.MustCompile(`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)

type rolloutMeta struct {
	cwd            string
	branch         string
	model          string
	effort         string
	permissionMode string
	firstPrompt    string
	lastPreview    string
	lastTs         string
	contextUsed    int
	contextLimit   int
	// hasConversation is set when the rollout holds a user or assistant turn.
	hasConversation bool
}

// metaFor is the codex branch of the Node sessionMetaFor.
func metaFor(sessionID, cwdHint string) agent.Meta {
	out := agent.Meta{Cwd: cwdHint, TitleSource: "agent"}
	titles := titleIndex()
	title := titles[sessionID]
	file := rolloutPathFor(sessionID)
	if file == "" {
		if title != "" {
			out.NativeTitle = title
		}
		return out
	}
	m := readRolloutMeta(file)
	out.Cwd = firstNonEmpty(out.Cwd, m.cwd)
	out.Branch = m.branch
	out.Model = m.model
	out.Effort = m.effort
	out.PermissionMode = m.permissionMode
	out.FirstPrompt = m.firstPrompt
	out.NativeTitle = title
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// titleIndex maps thread id -> thread_name from session_index.jsonl (last write wins).
func titleIndex() map[string]string {
	titles, _ := titleIndexFull()
	return titles
}

// titleIndexFull is titleIndex plus each thread's updated_at, which the list
// uses as last_ts for threads whose rollout file is gone.
func titleIndexFull() (map[string]string, map[string]string) {
	out := map[string]string{}
	updated := map[string]string{}
	for _, row := range scanlib.ReadJSONLines(filepath.Join(platform.Home(), ".codex", "session_index.jsonl")) {
		sessionID, _ := row["id"].(string)
		if sessionID == "" {
			continue
		}
		ts, _ := row["updated_at"].(string)
		if prev, ok := updated[sessionID]; ok && ts < prev {
			continue
		}
		name, _ := row["thread_name"].(string)
		out[sessionID] = name
		updated[sessionID] = ts
	}
	return out, updated
}

// rolloutFiles lists rollout transcripts under the last few day folders.
func rolloutFiles(limitDays int) []string {
	root := filepath.Join(platform.Home(), ".codex", "sessions")
	type day struct {
		key string
		dir string
	}
	var days []day
	years, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, y := range years {
		if !y.IsDir() {
			continue
		}
		months, err := os.ReadDir(filepath.Join(root, y.Name()))
		if err != nil {
			continue
		}
		for _, m := range months {
			if !m.IsDir() {
				continue
			}
			dates, err := os.ReadDir(filepath.Join(root, y.Name(), m.Name()))
			if err != nil {
				continue
			}
			for _, d := range dates {
				if !d.IsDir() {
					continue
				}
				days = append(days, day{
					key: y.Name() + "-" + m.Name() + "-" + d.Name(),
					dir: filepath.Join(root, y.Name(), m.Name(), d.Name()),
				})
			}
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].key > days[j].key })
	if limitDays > 0 && len(days) > limitDays {
		days = days[:limitDays]
	}
	var out []string
	for _, d := range days {
		entries, err := os.ReadDir(d.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") {
				out = append(out, filepath.Join(d.dir, name))
			}
		}
	}
	return out
}

// rolloutPathFor finds the transcript of one thread id.
func rolloutPathFor(sessionID string) string {
	for _, file := range rolloutFiles(0) {
		if strings.Contains(filepath.Base(file), sessionID) {
			return file
		}
	}
	return ""
}

func readRolloutMeta(file string) rolloutMeta {
	var m rolloutMeta
	stat, err := os.Stat(file)
	if err != nil {
		return m
	}
	head, tail := scanlib.HeadAndTail(file, stat.Size())
	for _, line := range strings.Split(head, "\n") {
		row := parseJSON(line)
		if row == nil {
			continue
		}
		payload, _ := row["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		if row["type"] == "session_meta" {
			if v, ok := payload["cwd"].(string); ok {
				m.cwd = v
			}
			if git, ok := payload["git"].(map[string]any); ok {
				if v, ok := git["branch"].(string); ok {
					m.branch = v
				}
			}
		}
		if payload["type"] == "token_count" {
			if info, ok := payload["info"].(map[string]any); ok {
				usage, _ := info["last_token_usage"].(map[string]any)
				if usage == nil {
					usage, _ = info["total_token_usage"].(map[string]any)
				}
				if usage != nil {
					used := number(usage["total_tokens"])
					if used == 0 {
						used = number(usage["input_tokens"]) + number(usage["cached_input_tokens"]) + number(usage["output_tokens"])
					}
					if used > 0 {
						m.contextUsed = used
						m.contextLimit = number(info["model_context_window"])
					}
				}
			}
		}
		if payload["type"] == "message" && (payload["role"] == "user" || payload["role"] == "assistant") {
			if strings.TrimSpace(scanlib.NormalizeContent(payload["content"])) != "" {
				m.hasConversation = true
			}
		}
		if payload["type"] == "message" && payload["role"] == "assistant" {
			if text := strings.TrimSpace(scanlib.NormalizeContent(payload["content"])); text != "" {
				m.hasConversation = true
				m.lastPreview = scanlib.FirstLine(text, 120)
			}
		}
		if m.firstPrompt == "" && payload["type"] == "message" && payload["role"] == "user" {
			text := strings.TrimSpace(scanlib.NormalizeContent(payload["content"]))
			if text != "" && !scanlib.Noise.MatchString(text) {
				m.firstPrompt = text
			}
		}
	}
	for _, line := range strings.Split(tail, "\n") {
		row := parseJSON(line)
		if row == nil {
			continue
		}
		if ts, ok := row["timestamp"].(string); ok {
			m.lastTs = ts
		}
		payload, _ := row["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		if v, ok := payload["model"].(string); ok && v != "" {
			m.model = v
		}
		if v, ok := payload["effort"].(string); ok && v != "" {
			m.effort = v
		}
		if v, ok := payload["approval_policy"].(string); ok && v != "" {
			m.permissionMode = v
		} else if sp, ok := payload["sandbox_policy"].(map[string]any); ok {
			if v, ok := sp["type"].(string); ok {
				m.permissionMode = v
			}
		}
		if settings, ok := payload["thread_settings"].(map[string]any); ok {
			if v, ok := settings["model"].(string); ok && v != "" {
				m.model = v
			}
		}
		if payload["type"] == "message" && payload["role"] == "assistant" {
			if text := strings.TrimSpace(scanlib.NormalizeContent(payload["content"])); text != "" {
				m.lastPreview = scanlib.FirstLine(text, 120)
			}
		}
		if item, ok := payload["item"].(map[string]any); ok {
			if item["type"] == "agent_message" {
				if text, ok := item["text"].(string); ok && strings.TrimSpace(text) != "" {
					m.lastPreview = scanlib.FirstLine(text, 120)
				}
			}
		}
	}
	return m
}

func parseJSON(line string) map[string]any {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	var row map[string]any
	if json.Unmarshal([]byte(trimmed), &row) != nil {
		return nil
	}
	return row
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}

// ContextUsage reports the token count of the last turn for the phone's
// context ring (Node reads payload.info.model_context_window for the limit).
func (Agent) ContextUsage(sessionID string) (used int, limit int, ok bool) {
	file := rolloutPathFor(sessionID)
	if file == "" {
		return 0, 0, false
	}
	m := readRolloutMeta(file)
	if m.contextUsed <= 0 {
		return 0, 0, false
	}
	return m.contextUsed, m.contextLimit, true
}

func number(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// IsEmptySession reports a thread that never got a real prompt or answer. It
// reads the same head/tail slices as the title, which is what the Node relay
// decides on after parsing the transcript.
func (Agent) IsEmptySession(sessionID string) bool {
	file := rolloutPathFor(sessionID)
	if file == "" {
		return true
	}
	return !readRolloutMeta(file).hasConversation
}

// ---- reply injection -----------------------------------------------------

var (
	threadMu sync.Mutex
	threads  = map[string]*threadState{}
)

type threadState struct {
	running bool
	queue   []queuedReply
}

type queuedReply struct {
	sessionID string
	text      string
	model     string
}

// Reply injects one message into an existing Codex thread.
//
// Only one `codex exec resume` runs per thread at a time; the rest queue up so
// two answers never interleave inside the same conversation.
func (a Agent) Reply(ctx context.Context, sessionID, text string, opts agent.ReplyOpts) error {
	threadMu.Lock()
	state, ok := threads[sessionID]
	if !ok {
		state = &threadState{}
		threads[sessionID] = state
	}
	if state.running {
		state.queue = append(state.queue, queuedReply{sessionID: sessionID, text: text, model: opts.Model})
		threadMu.Unlock()
		return errors.New("该 thread 正在执行上一轮注入，已排队")
	}
	state.running = true
	threadMu.Unlock()

	go runInjection(state, sessionID, text, opts.Model)
	return nil
}

func runInjection(state *threadState, sessionID, text, model string) {
	threadMu.Lock()
	binary := codexBinary()
	if binary == "" {
		state.running = false
		next, hasNext := pop(state)
		threadMu.Unlock()
		if hasNext {
			go runInjection(state, next.sessionID, next.text, next.model)
		}
		return
	}
	threadMu.Unlock()

	args := []string{"exec", "resume", "--skip-git-repo-check"}
	if model != "" {
		args = append(args, "-m", model)
	}
	args = append(args, "--all", sessionID, text)

	cmd := exec.Command(binary, args...)
	cmd.Dir = sessionDir(sessionID)
	cmd.Stdout = nil
	cmd.Stderr = nil
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		done <- err
	} else {
		go func() { done <- cmd.Wait() }()
	}

	select {
	case <-done:
	case <-time.After(ReplyTimeout):
		_ = cmd.Process.Kill()
	}

	threadMu.Lock()
	state.running = false
	next, hasNext := pop(state)
	threadMu.Unlock()
	if hasNext {
		go runInjection(state, next.sessionID, next.text, next.model)
	}
}

func pop(state *threadState) (queuedReply, bool) {
	if len(state.queue) == 0 {
		return queuedReply{}, false
	}
	next := state.queue[0]
	state.queue = state.queue[1:]
	state.running = true
	return next, true
}

// sessionDir is the cwd Codex should run in: the session's own cwd if it still exists.
func sessionDir(sessionID string) string {
	meta := metaFor(sessionID, "")
	if meta.Cwd != "" {
		if stat, err := os.Stat(meta.Cwd); err == nil && stat.IsDir() {
			return meta.Cwd
		}
	}
	return platform.Home()
}

// codexBinary finds the CLI: PATH first, then the copy bundled with ChatGPT.app.
func codexBinary() string {
	if p, err := exec.LookPath("codex"); err == nil {
		return p
	}
	candidates := []string{
		"/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex",
		filepath.Join(platform.Home(), ".local", "bin", "codex"),
	}
	for _, c := range candidates {
		if stat, err := os.Stat(c); err == nil && !stat.IsDir() {
			return c
		}
	}
	return ""
}
