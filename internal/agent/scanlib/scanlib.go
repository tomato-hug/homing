// Package scanlib is the shared toolbox for the built-in agents: reading JSONL
// transcripts, slicing big files, deriving titles and status words. It mirrors
// relay/scanners/lib/util.mjs so both implementations agree on every field.
//
// Owner: WorkBuddy.
package scanlib

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// HeadBytes / TailBytes bound how much of a transcript we read: the first part
// has cwd and the first prompt, the tail has the latest model and preview.
const (
	HeadBytes = 320 * 1024
	TailBytes = 192 * 1024
)

// MetaTTL is how long a directory scan or a parsed file meta stays fresh,
// same value as util.mjs META_TTL_MS.
const MetaTTL = time.Minute

// ISO formats a time the way Node's Date#toISOString does (UTC, milliseconds).
//
// Node takes a file's mtimeMs as a float64 (seconds*1e3 + nanoseconds/1e6) and
// then drops the fraction. At today's epoch a float64 only resolves ~0.0002 ms,
// so an mtime of …805.9999 ms comes out as …806. Doing the same arithmetic here
// keeps last_ts identical on both servers.
func ISO(t time.Time) string {
	ms := int64(float64(t.Unix())*1e3 + float64(t.Nanosecond())/1e6)
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// Transcripts remembers which file the last hook pointed at: for Claude Code it
// is the most reliable answer to "where is this session written".
var (
	transcriptMu sync.Mutex
	transcripts  = map[string]transcriptEntry{}
)

type transcriptEntry struct {
	source string
	path   string
	cwd    string
	at     time.Time
}

// RecordTranscript stores the transcript path a hook reported.
func RecordTranscript(sessionID, source, transcriptPath, cwd string) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(transcriptPath) == "" {
		return
	}
	transcriptMu.Lock()
	transcripts[sessionID] = transcriptEntry{source, transcriptPath, cwd, time.Now()}
	transcriptMu.Unlock()
}

// TranscriptPath returns the recorded transcript path, still on disk.
func TranscriptPath(sessionID string) (string, bool) {
	transcriptMu.Lock()
	entry, ok := transcripts[sessionID]
	transcriptMu.Unlock()
	if !ok {
		return "", false
	}
	if stat, err := os.Stat(entry.path); err == nil && !stat.IsDir() {
		return entry.path, true
	}
	return "", false
}

// ResolveBinary finds a CLI the way the Node relay does: PATH first, then the
// usual install places, then the extra candidates the caller knows about.
func ResolveBinary(name string, extra ...string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := append([]string{}, extra...)
	for _, dir := range []string{
		filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".local", "bin"),
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
	} {
		candidates = append(candidates, filepath.Join(dir, name))
	}
	for _, c := range candidates {
		if stat, err := os.Stat(c); err == nil && stat.Mode().IsRegular() {
			return c
		}
	}
	return ""
}

// ReadJSONLines parses one JSON object per line; damaged lines are skipped.
func ReadJSONLines(file string) []map[string]any {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []map[string]any
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) == nil {
			out = append(out, row)
		}
	}
	return out
}

// FirstLine is the first non-empty line, cut to limit runes.
func FirstLine(text string, limit int) string {
	value := strings.TrimSpace(text)
	if value == "" {
		return ""
	}
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return Truncate(strings.TrimSpace(line), limit)
	}
	return ""
}

// Truncate cuts to limit runes (not bytes: CJK titles would break otherwise).
func Truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// Slice reads up to length bytes at start; an unreadable file yields "".
func Slice(file string, start, length int64) string {
	if length <= 0 {
		return ""
	}
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, length)
	n, _ := f.ReadAt(buf, start)
	return string(buf[:n])
}

// HeadAndTail returns the first HeadBytes and (for big files) the last TailBytes.
func HeadAndTail(file string, size int64) (head, tail string) {
	head = Slice(file, 0, min64(size, HeadBytes))
	if size > HeadBytes {
		tail = Slice(file, max64(0, size-TailBytes), min64(size, TailBytes))
	} else {
		tail = head
	}
	return head, tail
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Noise matches the wrapper text both Codex and Claude Code put in front of the
// real prompt; it must not become a session title.
var Noise = regexp.MustCompile(`^(?i)(<skills_instructions>|<environment_context>|# AGENTS\.md instructions|<user_instructions>|<permissions|<INSTRUCTIONS|<environment|<tools>|<multi_agent_mode>|<system-reminder|<command-name|<command-message|<local-command)`)

// NormalizeContent flattens a message content field (string or block array).
func NormalizeContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, block := range v {
			switch b := block.(type) {
			case string:
				parts = append(parts, b)
			case map[string]any:
				kind, _ := b["type"].(string)
				if kind == "text" || kind == "input_text" || kind == "output_text" {
					if text, ok := b["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// ProjectFromCwd is the last path element, or 未命名项目.
func ProjectFromCwd(cwd string) string {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		return "未命名项目"
	}
	trimmed = strings.TrimRight(trimmed, `/\`)
	parts := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return trimmed
	}
	return parts[len(parts)-1]
}

// WorkspaceLabel is the parent directory name (shown as the "workspace" chip).
// A cwd that sits directly under the filesystem root has no workspace label, the
// same null the Node relay returns.
func WorkspaceLabel(cwd string) string {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		return ""
	}
	parent := filepath.Dir(trimmed)
	if parent == "" || parent == "." || parent == "/" {
		base := filepath.Base(parent)
		if base == "/" || base == "." || base == "" {
			return ""
		}
		return base
	}
	return filepath.Base(parent)
}

var (
	branchMu    sync.Mutex
	branchCache = map[string]branchEntry{}
)

type branchEntry struct {
	at     time.Time
	branch string
}

// GitBranch asks git for the checked-out branch, cached for a minute.
func GitBranch(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	branchMu.Lock()
	if hit, ok := branchCache[cwd]; ok && time.Since(hit.at) < time.Minute {
		branchMu.Unlock()
		return hit.branch
	}
	branchMu.Unlock()

	branch := ""
	span := time.Second
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Stderr = nil
	done := make(chan struct{})
	var out []byte
	go func() {
		out, _ = cmd.Output()
		close(done)
	}()
	select {
	case <-done:
		value := strings.TrimSpace(string(out))
		if value != "" && value != "HEAD" {
			branch = value
		}
	case <-time.After(span):
		_ = cmd.Process.Kill()
	}

	branchMu.Lock()
	branchCache[cwd] = branchEntry{time.Now(), branch}
	branchMu.Unlock()
	return branch
}

// Title is the display title plus where it came from (agent / custom /
// first_prompt / project / fallback), same order as the Node helper.
func Title(source, sessionID, nativeTitle, nativeTitleSource, firstPrompt, cwd string) (string, string) {
	if clean := Truncate(strings.TrimSpace(nativeTitle), 200); clean != "" {
		if nativeTitleSource == "" {
			nativeTitleSource = "agent"
		}
		return clean, nativeTitleSource
	}
	if prompt := FirstLine(firstPrompt, 40); prompt != "" {
		return prompt, "first_prompt"
	}
	if cwd != "" {
		if base := filepath.Base(strings.TrimSpace(cwd)); base != "" && base != "." {
			return base, "project"
		}
	}
	short := sessionID
	if len(short) > 8 {
		short = short[:8]
	}
	if short == "" {
		short = "unknown"
	}
	return source + "-" + short, "fallback"
}

// Status derives the session status word from the last event and native state.
func Status(lastKind string, pendingInjection bool, lastTs string, nativeState string, runningWindow time.Duration) string {
	switch {
	case lastKind == "permission":
		return "permission"
	case lastKind == "needs_input":
		return "needs_input"
	case lastKind == "error" || nativeState == "error":
		return "error"
	case pendingInjection:
		return "running"
	case lastKind == "reply":
		if ts, err := time.Parse(time.RFC3339, lastTs); err == nil && time.Since(ts) < runningWindow {
			return "running"
		}
	}
	return "done"
}

// TempCwd reports whether cwd lives under a system temp directory (a scratch
// session). Same prefixes the Node relay accepts: /tmp, /private/tmp, the macOS
// per-user temp roots, and Windows' AppData\Local\Temp.
func TempCwd(cwd string) bool {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		return false
	}
	p := strings.ReplaceAll(trimmed, `\`, "/")
	p = strings.TrimRight(p, "/")
	for _, prefix := range []string{"/tmp", "/private/tmp", "/private/var/folders", "/var/folders"} {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(p), "/appdata/local/temp")
}
