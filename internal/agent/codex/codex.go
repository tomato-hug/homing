// Package codex reads Codex CLI sessions: ~/.codex/sessions/<年>/<月>/<日>/rollout-*.jsonl
// plus the title index ~/.codex/session_index.jsonl.
//
// Ported from relay/scanners/codex.mjs. Reply injection prefers Codex's native
// `codex queue` command so it can coexist with the desktop app's thread writer.
// Older CLIs fall back to `codex exec resume`.
//
// Owner: WorkBuddy.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

var sessionProcs = struct {
	sync.Mutex
	byID map[string]*exec.Cmd
}{byID: map[string]*exec.Cmd{}}

const id = "codex"

// Icon is the monochrome 16×16 glyph shared with the console.
const Icon = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><path d="M8 1.8 13.4 4.9v6.2L8 14.2 2.6 11.1V4.9z"/><path d="M5.2 6.4 8 8l2.8-1.6M8 8v3.6"/></svg>`

// ReplyTimeout kills a runaway legacy `codex exec resume` after 10 minutes.
const ReplyTimeout = 10 * time.Minute

const injectionTailMax = 4000

func init() { agent.Register(Agent{}) }

// Agent implements agent.Agent, agent.Replier and agent.ItemReader.
type Agent struct{}

func (Agent) ID() string                   { return id }
func (Agent) Name() string                 { return "Codex" }
func (Agent) Icon() string                 { return Icon }
func (Agent) PushSignal() agent.PushSignal { return agent.PushHook }
func (Agent) Capabilities() agent.Capabilities {
	return agent.Capabilities{
		Core:            true,
		Reply:           true,
		Sessions:        true,
		NewSession:      true,
		Stop:            true,
		Approve:         true,
		Live:            true,
		Dispatch:        true,
		SwitchModel:     true,
		Effort:          true,
		PermissionModes: []string{"default", "read_only"},
	}
}
func (Agent) home() string                              { return filepath.Join(platform.Home(), ".codex") }
func (Agent) Meta(sessionID, cwdHint string) agent.Meta { return metaFor(sessionID, cwdHint) }

// ApprovalHook points the API at the settings file that routes permission
// requests to Homing. The file is never modified here.
func (Agent) ApprovalHook() (string, string) {
	return filepath.Join(".codex", "hooks.json"), "/hook/codex"
}

// StartSession launches a fresh `codex exec` thread and returns as soon as the
// CLI announces its thread id. The process stays attached to the returned id so
// /api/sessions/{id}/stop and dispatch cancellation can stop this turn.
func (Agent) StartSession(ctx context.Context, req agent.StartRequest) (agent.StartResult, error) {
	return startSession(ctx, req, time.Minute)
}

func startSession(ctx context.Context, req agent.StartRequest, startupTimeout time.Duration) (agent.StartResult, error) {
	binary := codexBinary()
	if binary == "" {
		return agent.StartResult{}, errors.New("本机没有 codex 命令")
	}
	cwd := strings.TrimSpace(req.Cwd)
	text := strings.TrimSpace(req.Text)
	if cwd == "" || text == "" {
		return agent.StartResult{}, errors.New("cwd 和 text 不能为空")
	}
	args := []string{"exec", "--json", "--skip-git-repo-check", "-C", cwd}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.Effort != "" {
		args = append(args, "-c", `model_reasoning_effort="`+req.Effort+`"`)
	}
	if req.PermissionMode == "read_only" || req.PermissionMode == "plan" {
		args = append(args, "--sandbox", "read-only")
	}
	args = append(args, text)

	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return agent.StartResult{}, fmt.Errorf("无法读取 codex 输出：%w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return agent.StartResult{}, fmt.Errorf("无法启动 codex：%w", err)
	}

	idCh := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(line), &payload) != nil {
				continue
			}
			if stringValue(payload["type"]) != "thread.started" {
				continue
			}
			if id := firstNonEmpty(
				stringValue(payload["thread_id"]),
				stringValue(payload["thread-id"]),
			); id != "" {
				sessionProcs.Lock()
				sessionProcs.byID[id] = cmd
				sessionProcs.Unlock()
				select {
				case idCh <- id:
				default:
				}
			}
		}
		// Drain stdout before Wait closes the pipe, including short-lived turns.
		if scanner.Err() != nil {
			_ = cmd.Process.Kill()
		}
		err := cmd.Wait()
		sessionProcs.Lock()
		for id, candidate := range sessionProcs.byID {
			if candidate == cmd {
				delete(sessionProcs.byID, id)
			}
		}
		sessionProcs.Unlock()
		done <- err
	}()

	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	select {
	case id := <-idCh:
		return agent.StartResult{SessionID: id, Status: "started"}, nil
	case err := <-done:
		select {
		case id := <-idCh:
			return agent.StartResult{SessionID: id, Status: "started"}, nil
		default:
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" && err != nil {
			message = err.Error()
		}
		if message == "" {
			message = "codex 未返回 thread id"
		}
		return agent.StartResult{}, errors.New(truncateOneLine(message, 300))
	case <-timer.C:
		_ = cmd.Process.Kill()
		return agent.StartResult{}, fmt.Errorf("Codex 启动超时，请检查 CLI 登录及本机环境后重试：%w", context.DeadlineExceeded)
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return agent.StartResult{}, ctx.Err()
	}
}

// StopSession stops a thread launched by StartSession.
func (Agent) StopSession(_ context.Context, sessionID string) error {
	sessionProcs.Lock()
	cmd := sessionProcs.byID[strings.TrimSpace(sessionID)]
	if cmd != nil {
		delete(sessionProcs.byID, strings.TrimSpace(sessionID))
	}
	sessionProcs.Unlock()
	if cmd == nil || cmd.Process == nil {
		return agent.ErrUnsupported
	}
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Process.Kill()
	return nil
}

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
		rememberRolloutPath(sessionID, file)
		title, titleSource := "", "first_prompt"
		if name, ok := titles[sessionID]; ok && name != "" {
			title, titleSource = name, "agent"
		}
		// Node 的规则：会话在 session_index 里有 updated_at 就用它，否则才用文件 mtime。
		lastTS, sortTime := scanlib.ISO(stat.ModTime()), stat.ModTime()
		if ts := updated[sessionID]; ts != "" {
			lastTS = ts
			if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				sortTime = parsed
			}
		}
		meta := readRolloutMetaCached(file)
		// Skip internal subagent worker rollouts and auxiliary title-generator rollouts
		// unless explicitly indexed as a top-level user thread in session_index.jsonl.
		if titleSource != "agent" && (meta.isSubagent || meta.isTitleHelper) {
			continue
		}
		rows = append(rows, row{
			session: agent.Session{
				SessionID:   sessionID,
				Source:      id,
				Title:       title,
				TitleSource: titleSource,
				LastTS:      lastTS,
				LastUserTS:  meta.lastUserTS,
				LastPreview: meta.lastPreview,
			},
			mtime: sortTime,
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

type rolloutPathEntry struct {
	at   time.Time
	byID map[string]string
}

var rolloutPaths = struct {
	sync.Mutex
	entry rolloutPathEntry
}{}

type titleIndexEntry struct {
	at      time.Time
	size    int64
	modTime time.Time
	titles  map[string]string
	updated map[string]string
}

var titleIndexCache = struct {
	sync.Mutex
	entry titleIndexEntry
}{}

type rolloutMetaEntry struct {
	at      time.Time
	size    int64
	modTime time.Time
	meta    rolloutMeta
}

var rolloutMetaCache = struct {
	sync.Mutex
	byFile map[string]rolloutMetaEntry
}{
	byFile: map[string]rolloutMetaEntry{},
}

type rolloutMeta struct {
	cwd            string
	branch         string
	model          string
	effort         string
	permissionMode string
	firstPrompt    string
	lastPreview    string
	lastTs         string
	lastUserTS     string
	contextUsed    int
	contextLimit   int
	// hasConversation is set when the rollout holds a user or assistant turn.
	hasConversation bool
	isSubagent      bool
	isTitleHelper   bool
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
	m := readRolloutMetaCached(file)
	out.Cwd = firstNonEmpty(out.Cwd, m.cwd)
	out.Branch = m.branch
	out.Model = m.model
	out.Effort = m.effort
	out.PermissionMode = m.permissionMode
	out.FirstPrompt = m.firstPrompt
	out.NativeTitle = title
	out.LastUserTS = m.lastUserTS
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

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func truncateOneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}

// titleIndex maps thread id -> thread_name from session_index.jsonl (last write wins).
func titleIndex() map[string]string {
	titles, _ := titleIndexFull()
	return titles
}

// titleIndexFull is titleIndex plus each thread's updated_at, which the list
// uses as last_ts for threads whose rollout file is gone.
func titleIndexFull() (map[string]string, map[string]string) {
	file := filepath.Join(platform.Home(), ".codex", "session_index.jsonl")
	stat, err := os.Stat(file)
	now := time.Now()
	titleIndexCache.Lock()
	if err == nil {
		hit := titleIndexCache.entry
		if !hit.at.IsZero() &&
			time.Since(hit.at) < scanlib.MetaTTL &&
			hit.size == stat.Size() &&
			hit.modTime.Equal(stat.ModTime()) {
			titleIndexCache.Unlock()
			return hit.titles, hit.updated
		}
	}
	titleIndexCache.Unlock()

	out := map[string]string{}
	updated := map[string]string{}
	for _, row := range scanlib.ReadJSONLines(file) {
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
	titleIndexCache.Lock()
	entry := titleIndexEntry{at: now, titles: out, updated: updated}
	if err == nil {
		entry.size = stat.Size()
		entry.modTime = stat.ModTime()
	} else {
		// A missing index is a valid empty answer; retry after the TTL.
		entry.size = -1
	}
	titleIndexCache.entry = entry
	titleIndexCache.Unlock()
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
	now := time.Now()
	rolloutPaths.Lock()
	if !rolloutPaths.entry.at.IsZero() && time.Since(rolloutPaths.entry.at) < 30*time.Second {
		// A fresh index is authoritative for misses too. Rebuilding it for every
		// session whose rollout was pruned made one /api/sessions call walk the
		// whole Codex directory hundreds of times.
		file := rolloutPaths.entry.byID[sessionID]
		rolloutPaths.Unlock()
		return file
	}
	rolloutPaths.Unlock()

	byID := map[string]string{}
	for _, file := range rolloutFiles(0) {
		match := rolloutName.FindStringSubmatch(filepath.Base(file))
		if len(match) < 2 {
			continue
		}
		// rolloutFiles is newest-first. A resumed thread can have multiple
		// files; the newest one is the live transcript.
		if byID[match[1]] == "" {
			byID[match[1]] = file
		}
	}
	rolloutPaths.Lock()
	rolloutPaths.entry = rolloutPathEntry{at: now, byID: byID}
	rolloutPaths.Unlock()
	return byID[sessionID]
}

// rememberRolloutPath keeps the id -> file index current when ListSessions finds
// a transcript that appeared after the last directory walk.
func rememberRolloutPath(sessionID, file string) {
	if sessionID == "" || file == "" {
		return
	}
	rolloutPaths.Lock()
	if !rolloutPaths.entry.at.IsZero() && rolloutPaths.entry.byID != nil {
		rolloutPaths.entry.byID[sessionID] = file
	}
	rolloutPaths.Unlock()
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
			if srcMap, ok := payload["source"].(map[string]any); ok && srcMap["subagent"] != nil {
				m.isSubagent = true
			}
		}
		if payload["type"] == "message" && (payload["role"] == "user" || payload["role"] == "assistant") {
			text := strings.TrimSpace(scanlib.NormalizeContent(payload["content"]))
			if text != "" {
				if payload["role"] == "assistant" && IsInternalTitleGeneratorPayload(text) {
					m.isTitleHelper = true
				} else {
					m.hasConversation = true
				}
			}
		}
		if payload["type"] == "message" && payload["role"] == "assistant" {
			if text := strings.TrimSpace(scanlib.NormalizeContent(payload["content"])); text != "" && !IsInternalTitleGeneratorPayload(text) {
				m.hasConversation = true
				m.isTitleHelper = false
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
		// 与 Node 一致：上下文用量只在尾部窗口里取最后一条 token_count。
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
		if payload["type"] == "message" && payload["role"] == "assistant" {
			if text := strings.TrimSpace(scanlib.NormalizeContent(payload["content"])); text != "" {
				m.lastPreview = scanlib.FirstLine(text, 120)
			}
		}
		if payload["type"] == "message" && payload["role"] == "user" {
			if text := strings.TrimSpace(scanlib.NormalizeContent(payload["content"])); text != "" && !codexNoise(text) {
				if ts, ok := row["timestamp"].(string); ok && ts != "" {
					m.lastUserTS = ts
				}
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

// readRolloutMetaCached avoids parsing the same rollout three times while one
// session list is built (Meta, ContextUsage and IsEmptySession all ask for it).
// File size and mtime invalidate the entry; the one-minute TTL bounds the
// per-file bookkeeping for long-running servers.
func readRolloutMetaCached(file string) rolloutMeta {
	stat, err := os.Stat(file)
	if err != nil {
		return rolloutMeta{}
	}
	now := time.Now()
	rolloutMetaCache.Lock()
	if hit, ok := rolloutMetaCache.byFile[file]; ok &&
		time.Since(hit.at) < scanlib.MetaTTL &&
		hit.size == stat.Size() &&
		hit.modTime.Equal(stat.ModTime()) {
		rolloutMetaCache.Unlock()
		return hit.meta
	}
	rolloutMetaCache.Unlock()

	m := readRolloutMeta(file)
	rolloutMetaCache.Lock()
	if len(rolloutMetaCache.byFile) >= 4096 {
		cutoff := now.Add(-scanlib.MetaTTL)
		for candidate, entry := range rolloutMetaCache.byFile {
			if entry.at.Before(cutoff) {
				delete(rolloutMetaCache.byFile, candidate)
			}
		}
	}
	rolloutMetaCache.byFile[file] = rolloutMetaEntry{
		at: now, size: stat.Size(), modTime: stat.ModTime(), meta: m,
	}
	rolloutMetaCache.Unlock()
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
	m := readRolloutMetaCached(file)
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

// IsInternalTitleGeneratorPayload detects Codex Desktop's background thread-title
// helper JSON responses (e.g. `{"title":"...","description":"..."}`) so they do
// not spawn duplicate untitled project-named sessions or raw JSON bubbles.
func IsInternalTitleGeneratorPayload(text string) bool {
	return scanlib.IsInternalTitlePayload(text)
}

// IsAuxiliarySession reports internal Codex subagent worker threads or title-helper
// rollouts that are not top-level user threads in session_index.jsonl.
func (Agent) IsAuxiliarySession(sessionID string) bool {
	if titles := titleIndex(); titles[sessionID] != "" {
		return false
	}
	file := rolloutPathFor(sessionID)
	if file == "" {
		return false
	}
	m := readRolloutMetaCached(file)
	return m.isSubagent || m.isTitleHelper
}

// IsEmptySession reports a thread that never got a real prompt or answer. It
// reads the same head/tail slices as the title, which is what the Node relay
// decides on after parsing the transcript.
func (Agent) IsEmptySession(sessionID string) bool {
	file := rolloutPathFor(sessionID)
	if file == "" {
		return true
	}
	return !readRolloutMetaCached(file).hasConversation
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
	sessionID      string
	text           string
	model          string
	permissionMode string
	replyID        int64
}

// Reply injects one message into an existing Codex thread.
//
// Newer Codex versions expose `codex queue`, which writes through the same
// thread store as the desktop app without opening a second writer. If the
// installed CLI predates that command, fall back to one serialized
// `codex exec resume` per thread.
func (a Agent) Reply(ctx context.Context, sessionID, text string, opts agent.ReplyOpts) error {
	sessionID = strings.TrimSpace(sessionID)
	text = strings.TrimSpace(text)
	if sessionID == "" {
		return errors.New("Codex thread id 不能为空")
	}
	if text == "" {
		return errors.New("回复内容不能为空")
	}
	binary := codexBinary()
	if binary == "" {
		return errors.New("本机没有 codex 命令")
	}

	queued, err := startCodexQueue(binary, sessionID, text)
	if err == nil {
		if queued {
			return fmt.Errorf("%w: 已排入 Codex 原生队列，等待 Codex 恢复该会话", agent.ErrQueued)
		}
		return nil
	}
	if !codexQueueUnsupported(err) {
		return err
	}

	threadMu.Lock()
	state, ok := threads[sessionID]
	if !ok {
		state = &threadState{}
		threads[sessionID] = state
	}
	if state.running {
		state.queue = append(state.queue, queuedReply{
			sessionID:      sessionID,
			text:           text,
			model:          opts.Model,
			permissionMode: opts.PermissionMode,
			replyID:        opts.ReplyID,
		})
		threadMu.Unlock()
		return fmt.Errorf("%w: 该 thread 正在执行上一轮注入，已排队", agent.ErrQueued)
	}
	state.running = true
	threadMu.Unlock()

	go runInjection(state, sessionID, text, opts.Model, opts.PermissionMode)
	return nil
}

// startCodexQueue uses the app-server-backed queue command. With a live daemon
// the CLI keeps running for the duration of the resumed turn, so this waits
// only long enough to catch an immediate error and then lets it finish in the
// background. queued=true means the item was stored but resume was refused.
func startCodexQueue(binary, sessionID, text string) (queued bool, err error) {
	ensureCodexDaemon(binary)

	ctx, cancel := context.WithTimeout(context.Background(), ReplyTimeout)

	dir := sessionDir(sessionID)
	args := []string{
		"queue",
		"--thread", sessionID,
		"--message", text,
		"--resume",
	}
	if dir != "" {
		args = append(args, "-C", dir)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Start(); runErr != nil {
		cancel()
		return false, fmt.Errorf("codex queue 无法启动：%w", runErr)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		cancel()
	}()

	select {
	case runErr := <-done:
		return inspectCodexQueueResult(strings.TrimSpace(stdout.String()+"\n"+stderr.String()), runErr)
	case <-time.After(800 * time.Millisecond):
		// The message has been handed to Codex; keep the child attached to the
		// process so a long reasoning turn is not killed.
		return false, nil
	}
}

func inspectCodexQueueResult(output string, runErr error) (queued bool, err error) {
	detail := strings.TrimSpace(output)
	if len(detail) > injectionTailMax {
		detail = detail[len(detail)-injectionTailMax:]
	}
	if runErr == nil {
		lower := strings.ToLower(detail)
		return strings.Contains(lower, "failed to resume") ||
			strings.Contains(lower, "warning:"), nil
	}
	if detail == "" {
		detail = runErr.Error()
	}
	return false, fmt.Errorf("codex queue 注入失败：%s", truncateOneLine(detail, 500))
}

// ensureCodexDaemon makes `codex queue --resume` able to actually start the
// thread. Without a daemon the CLI only stores the queue item and prints a
// warning, which looks successful but never runs.
func ensureCodexDaemon(binary string) {
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer checkCancel()
	check := exec.CommandContext(checkCtx, binary, "app-server", "daemon", "version")
	check.Stdout = io.Discard
	check.Stderr = io.Discard
	if check.Run() == nil {
		return
	}

	startCtx, startCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startCancel()
	start := exec.CommandContext(startCtx, binary, "app-server", "daemon", "start")
	start.Stdout = io.Discard
	start.Stderr = io.Discard
	_ = start.Run()
}

func codexQueueUnsupported(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unrecognized subcommand") ||
		strings.Contains(message, "unknown command") ||
		strings.Contains(message, "unexpected argument") ||
		strings.Contains(message, "unrecognized command")
}

// CancelQueuedReply removes one accepted reply before runInjection starts it.
// A reply that is already running is intentionally not cancellable here; the
// caller should use the session stop endpoint instead.
func (Agent) CancelQueuedReply(_ context.Context, sessionID string, replyID int64) error {
	if replyID <= 0 {
		return agent.ErrNotQueued
	}
	threadMu.Lock()
	defer threadMu.Unlock()
	state, ok := threads[sessionID]
	if !ok {
		return agent.ErrNotQueued
	}
	for index, queued := range state.queue {
		if queued.replyID != replyID {
			continue
		}
		state.queue = append(state.queue[:index], state.queue[index+1:]...)
		return nil
	}
	return agent.ErrNotQueued
}

func runInjection(state *threadState, sessionID, text, model, permissionMode string) {
	threadMu.Lock()
	binary := codexBinary()
	if binary == "" {
		state.running = false
		next, hasNext := pop(state)
		threadMu.Unlock()
		if hasNext {
			go runInjection(state, next.sessionID, next.text, next.model, next.permissionMode)
		}
		return
	}
	threadMu.Unlock()

	args := []string{"exec", "resume", "--skip-git-repo-check"}
	if model != "" {
		args = append(args, "-m", model)
	}
	if permissionMode == "read_only" || permissionMode == "plan" {
		args = append(args, "--sandbox", "read-only")
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
		go runInjection(state, next.sessionID, next.text, next.model, next.permissionMode)
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
