// Package claude reads Claude Code sessions: ~/.claude/projects/<dir>/<session>.jsonl
// plus custom-title / ai-title / summary lines inside the transcript.
//
// Ported from relay/scanners/claude.mjs. Replies are injected with
// `claude -p <text> --resume <session>` (argument array, never a shell string);
// if the transcript changed in the last 15 seconds the desktop app probably owns
// the session, so the message is held for the next stop hook instead.
//
// Owner: WorkBuddy.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/platform"
)

const id = "claude"

// Icon is the monochrome 16×16 glyph shared with the console.
const Icon = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><path d="M8 1.6v3.2M8 11.2v3.2M1.6 8h3.2M11.2 8h3.2M3.5 3.5l2.3 2.3M12.5 3.5 10.2 5.8M3.5 12.5l2.3-2.3M12.5 12.5 10.2 10.2"/></svg>`

// BusyWindow is how recently the transcript must have changed for us to assume
// the desktop app is still driving that session.
const BusyWindow = 15 * time.Second

// ReplyTimeout kills a runaway `claude -p` after 10 minutes.
const ReplyTimeout = 10 * time.Minute

func init() { agent.Register(Agent{}) }

// Agent implements agent.Agent, agent.Replier and agent.ContextReporter.
type Agent struct{}

func (Agent) ID() string                   { return id }
func (Agent) Name() string                 { return "Claude Code" }
func (Agent) Icon() string                 { return Icon }
func (Agent) PushSignal() agent.PushSignal { return agent.PushHook }
func (Agent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Reply: true, Sessions: true}
}
func (Agent) Meta(sessionID, cwdHint string) agent.Meta { return metaFor(sessionID, cwdHint) }

func home() string { return filepath.Join(platform.Home(), ".claude") }

// Detect reports ~/.claude/projects plus the version in settings.json.
func (Agent) Detect(context.Context) agent.Detection {
	dir := home()
	det := agent.Detection{Installed: dirExists(filepath.Join(dir, "projects"))}
	var settings struct {
		Version string `json:"version"`
	}
	if platform.ReadJSON(filepath.Join(dir, "settings.json"), &settings) && settings.Version != "" {
		v := settings.Version
		det.Version = &v
	}
	return det
}

// ---- session files -------------------------------------------------------

type fileEntry struct {
	path      string
	sessionID string
	dirName   string
	size      int64
	mtime     time.Time
}

var (
	filesMu    sync.Mutex
	filesAt    time.Time
	filesCache []fileEntry
	metaCache  = map[string]cachedMeta{}
)

type cachedMeta struct {
	size  int64
	mtime time.Time
	meta  fileMeta
}

// sessionFiles lists every transcript under ~/.claude/projects, newest first,
// cached for a minute like the Node scanner.
func sessionFiles() []fileEntry {
	filesMu.Lock()
	if len(filesCache) > 0 && time.Since(filesAt) < scanlib.MetaTTL {
		out := filesCache
		filesMu.Unlock()
		return out
	}
	filesMu.Unlock()

	var entries []fileEntry
	base := filepath.Join(home(), "projects")
	dirs, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		dirPath := filepath.Join(base, dir.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			full := filepath.Join(dirPath, f.Name())
			stat, err := os.Stat(full)
			if err != nil {
				continue
			}
			entries = append(entries, fileEntry{
				path:      full,
				sessionID: strings.TrimSuffix(f.Name(), ".jsonl"),
				dirName:   dir.Name(),
				size:      stat.Size(),
				mtime:     stat.ModTime(),
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mtime.After(entries[j].mtime) })

	filesMu.Lock()
	filesAt = time.Now()
	filesCache = entries
	filesMu.Unlock()
	return entries
}

// filePathFor finds the transcript of one session id.
func filePathFor(sessionID string) string {
	for _, e := range sessionFiles() {
		if e.sessionID == sessionID {
			return e.path
		}
	}
	if recorded, ok := scanlib.TranscriptPath(sessionID); ok {
		return recorded
	}
	return ""
}

// ---- file meta -----------------------------------------------------------

type fileMeta struct {
	cwd            string
	branch         string
	model          string
	effort         string
	permissionMode string
	contextUsed    int
	customTitle    string
	aiTitle        string
	summary        string
	firstPrompt    string
	lastTs         string
	lastPreview    string
	// hasConversation is set when the transcript holds a user or assistant row,
	// which is the Node relay's rule for "not an empty session".
	hasConversation bool
}

// fileMetaFor parses the head and tail of one transcript, cached by size+mtime.
func fileMetaFor(e fileEntry) fileMeta {
	metaCacheKey := e.path
	filesMu.Lock()
	if hit, ok := metaCache[metaCacheKey]; ok && hit.size == e.size && hit.mtime.Equal(e.mtime) {
		filesMu.Unlock()
		return hit.meta
	}
	filesMu.Unlock()

	var m fileMeta
	head, tail := scanlib.HeadAndTail(e.path, e.size)
	for _, line := range strings.Split(head, "\n") {
		m.apply(line, true)
	}
	if tail != head {
		for _, line := range strings.Split(tail, "\n") {
			m.apply(line, false)
		}
	}

	filesMu.Lock()
	metaCache[metaCacheKey] = cachedMeta{size: e.size, mtime: e.mtime, meta: m}
	filesMu.Unlock()
	return m
}

func (m *fileMeta) apply(line string, fromHead bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	var row map[string]any
	if json.Unmarshal([]byte(trimmed), &row) != nil || row == nil {
		return
	}
	if v, ok := row["cwd"].(string); ok && v != "" {
		m.cwd = v
	}
	if v, ok := row["gitBranch"].(string); ok && v != "" {
		m.branch = v
	}
	if v, ok := row["timestamp"].(string); ok && v != "" {
		m.lastTs = v
	}
	kind, _ := row["type"].(string)
	if kind == "custom-title" {
		if v, ok := row["customTitle"].(string); ok {
			m.customTitle = v
		}
	}
	if kind == "ai-title" {
		if v, ok := row["aiTitle"].(string); ok {
			m.aiTitle = v
		}
	}
	if kind == "summary" {
		if v, ok := row["summary"].(string); ok {
			m.summary = v
		}
	}
	if v, ok := row["permissionMode"].(string); ok && v != "" {
		m.permissionMode = v
	}
	if v, ok := row["effort"].(string); ok && v != "" {
		m.effort = v
	} else if v, ok := row["perTurnEffort"].(string); ok && v != "" {
		m.effort = v
	}
	message, ok := row["message"].(map[string]any)
	if !ok || message == nil {
		return
	}
	if v, ok := message["model"].(string); ok && v != "" {
		m.model = v
	}
	if usage, ok := message["usage"].(map[string]any); ok {
		used := number(usage["input_tokens"]) + number(usage["cache_read_input_tokens"]) +
			number(usage["cache_creation_input_tokens"]) + number(usage["output_tokens"])
		if used > 0 {
			m.contextUsed = used
		}
	}
	text := strings.TrimSpace(scanlib.NormalizeContent(message["content"]))
	role, _ := message["role"].(string)
	if role == "" {
		role = kind
	}
	if (role == "user" || role == "assistant") && text != "" {
		m.hasConversation = true
	}
	if fromHead && m.firstPrompt == "" && role == "user" && text != "" && !scanlib.Noise.MatchString(text) {
		m.firstPrompt = text
	}
	if !fromHead && role == "assistant" && text != "" {
		m.lastPreview = scanlib.FirstLine(text, 120)
	}
}

func number(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

// metaFor is the claude branch of the Node sessionMetaFor. The transcript path
// reported by the hook wins over the directory scan.
func metaFor(sessionID, cwdHint string) agent.Meta {
	out := agent.Meta{Cwd: cwdHint, TitleSource: "agent"}
	if e := entryFor(sessionID); e != nil {
		m := fileMetaFor(*e)
		out.Cwd = firstNonEmpty(out.Cwd, m.cwd)
		out.Branch = m.branch
		out.Model = m.model
		out.Effort = m.effort
		out.PermissionMode = m.permissionMode
		out.FirstPrompt = m.firstPrompt
		switch {
		case m.customTitle != "":
			out.NativeTitle, out.TitleSource = m.customTitle, "custom"
		case m.aiTitle != "":
			out.NativeTitle = m.aiTitle
		case m.summary != "":
			out.NativeTitle = m.summary
		}
	}
	if recorded, ok := scanlib.TranscriptPath(sessionID); ok {
		if stat, err := os.Stat(recorded); err == nil {
			m := fileMetaFor(fileEntry{path: recorded, sessionID: sessionID, size: stat.Size(), mtime: stat.ModTime()})
			out.Cwd = firstNonEmpty(out.Cwd, m.cwd)
			out.Branch = firstNonEmpty(m.branch, out.Branch)
			out.Model = firstNonEmpty(m.model, out.Model)
			out.Effort = firstNonEmpty(out.Effort, m.effort)
			out.PermissionMode = firstNonEmpty(out.PermissionMode, m.permissionMode)
			out.FirstPrompt = firstNonEmpty(out.FirstPrompt, m.firstPrompt)
			if out.NativeTitle == "" {
				out.NativeTitle = firstNonEmpty(m.customTitle, m.aiTitle, m.summary)
				if m.customTitle != "" {
					out.TitleSource = "custom"
				}
			}
		}
	}
	return out
}

func entryFor(sessionID string) *fileEntry {
	for _, e := range sessionFiles() {
		if e.sessionID == sessionID {
			entry := e
			return &entry
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// FileMeta is the head/tail of a Claude-family transcript. WorkBuddy writes the
// same JSONL shape, so its scanner reuses this parser.
type FileMeta struct {
	Cwd             string
	FirstPrompt     string
	Model           string
	CustomTitle     string
	AITitle         string
	Summary         string
	HasConversation bool
}

// ReadFileMeta parses one transcript file from scratch (no shared cache).
func ReadFileMeta(path string) FileMeta {
	stat, err := os.Stat(path)
	if err != nil {
		return FileMeta{}
	}
	m := fileMetaFor(fileEntry{path: path, size: stat.Size(), mtime: stat.ModTime()})
	return FileMeta{
		Cwd:             m.cwd,
		FirstPrompt:     m.firstPrompt,
		Model:           m.model,
		CustomTitle:     m.customTitle,
		AITitle:         m.aiTitle,
		Summary:         m.summary,
		HasConversation: m.hasConversation,
	}
}

// ContextUsage reports the token count of the last turn, for the phone's
// context ring. The second result is the budget when the agent knows it.
func (Agent) ContextUsage(sessionID string) (used int, limit int, ok bool) {
	e := entryFor(sessionID)
	if e == nil {
		return 0, 0, false
	}
	m := fileMetaFor(*e)
	if m.contextUsed <= 0 {
		return 0, 0, false
	}
	return m.contextUsed, 0, true
}

// ---- listing -------------------------------------------------------------

// ListSessions returns one row per transcript, newest first.
func (Agent) ListSessions(_ context.Context, limit int) ([]agent.Session, error) {
	files := sessionFiles()
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	out := make([]agent.Session, 0, len(files))
	for _, e := range files {
		title, source := "", "first_prompt"
		m := fileMetaFor(e)
		switch {
		case m.customTitle != "":
			title, source = m.customTitle, "custom"
		case m.aiTitle != "":
			title, source = m.aiTitle, "agent"
		case m.summary != "":
			title, source = m.summary, "agent"
		}
		out = append(out, agent.Session{
			SessionID:   e.sessionID,
			Source:      id,
			Title:       title,
			TitleSource: source,
			LastTS:      scanlib.ISO(e.mtime),
			LastPreview: m.lastPreview,
		})
	}
	return out, nil
}

// ---- reply injection -----------------------------------------------------

var (
	procMu sync.Mutex
	procs  = map[string]*exec.Cmd{}
)

// Reply injects one message into an existing Claude Code session.
//
// A transcript touched in the last 15 seconds means the desktop app is probably
// mid-turn, so this returns agent.ErrHeld and the caller keeps the message for
// the next stop hook.
func (Agent) Reply(_ context.Context, sessionID, text string, opts agent.ReplyOpts) error {
	binary := scanlib.ResolveBinary("claude")
	if binary == "" {
		return errors.New("本机没有 claude 命令")
	}
	if path := filePathFor(sessionID); path != "" {
		if stat, err := os.Stat(path); err == nil && time.Since(stat.ModTime()) < BusyWindow {
			return fmt.Errorf("%w：Claude 会话可能正被桌面端占用，已排队等下一次 Stop 钩子", agent.ErrHeld)
		}
	}

	args := []string{"-p", text, "--resume", sessionID}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir(sessionID)
	cmd.Stdout, cmd.Stderr = nil, nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动 claude: %w", err)
	}
	procMu.Lock()
	procs[sessionID] = cmd
	procMu.Unlock()
	go func() {
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(ReplyTimeout):
			_ = cmd.Process.Kill()
		}
		procMu.Lock()
		if procs[sessionID] == cmd {
			delete(procs, sessionID)
		}
		procMu.Unlock()
	}()
	return nil
}

// Stop kills the injection process running for this session, if any.
func Stop(sessionID string) bool {
	procMu.Lock()
	cmd, ok := procs[sessionID]
	if ok {
		delete(procs, sessionID)
	}
	procMu.Unlock()
	if !ok {
		return false
	}
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Process.Kill()
	return true
}

// Busy reports whether the transcript changed too recently to inject into.
func Busy(sessionID string) bool {
	path := filePathFor(sessionID)
	if path == "" {
		return false
	}
	stat, err := os.Stat(path)
	return err == nil && time.Since(stat.ModTime()) < BusyWindow
}

// workDir is the cwd Claude should run in: the session's own cwd if it exists.
func workDir(sessionID string) string {
	if cwd := metaFor(sessionID, "").Cwd; cwd != "" {
		if stat, err := os.Stat(cwd); err == nil && stat.IsDir() {
			return cwd
		}
	}
	return platform.Home()
}

// IsEmptySession reports a transcript with no real prompt and no answer. It
// decides on the same head/tail slices the title uses.
func (Agent) IsEmptySession(sessionID string) bool {
	e := entryFor(sessionID)
	if e == nil {
		if recorded, ok := scanlib.TranscriptPath(sessionID); ok {
			stat, err := os.Stat(recorded)
			if err != nil {
				return true
			}
			e = &fileEntry{path: recorded, sessionID: sessionID, size: stat.Size(), mtime: stat.ModTime()}
		} else {
			return true
		}
	}
	return !fileMetaFor(*e).hasConversation
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}
