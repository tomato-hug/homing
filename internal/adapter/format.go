package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Format is one way of reading a session file. The Node relay keeps the same four
// presets in relay/adapters/formats.mjs; both must produce the same session ids,
// titles and timestamps, because the phone sees whichever server is running.
type Format interface {
	// ListFiles returns one row per session found in files.
	ListFiles(files []string) ([]FormatSession, error)
	// ReadItems returns the transcript of one file.
	ReadItems(path string, limit int) ([]FormatItem, error)
	// HasConversation reports whether the file has any user/assistant text.
	HasConversation(path string) bool
}

// FormatSession is what a format extracts from one file.
type FormatSession struct {
	SessionID   string
	Title       string
	TitleSource string
	Cwd         string
	Model       string
	Branch      string
	LastTS      string
	Path        string
}

// FormatItem is one transcript entry.
type FormatItem struct {
	Role string
	Kind string
	Text string
	At   string
}

const maxText = 4000

// FormatFor builds the reader for a manifest. mapping is only used by "custom".
func FormatFor(name string, mapping map[string]any) (Format, error) {
	switch name {
	case "", "generic-jsonl":
		return customFormat{mapping: mapping}, nil
	case "custom":
		return customFormat{mapping: mapping}, nil
	case "claude-code":
		return claudeFormat{}, nil
	case "codex":
		return codexFormat{}, nil
	case "gemini-cli":
		return geminiFormat{}, nil
	case "generic-json":
		return genericJSONFormat{mapping: mapping}, nil
	case "dsh":
		return dshFormat{}, nil
	case "openclaw":
		return openclawFormat{}, nil
	}
	return nil, fmt.Errorf("未知格式 %q", name)
}

// ---- 共用 ----------------------------------------------------------------

func readLines(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// DeepSeek Harness 的会话是 zstd 压缩的 JSONL
	if strings.HasSuffix(strings.ToLower(path), ".zst") || strings.HasSuffix(strings.ToLower(path), ".zstd") {
		dec, derr := zstd.NewReader(bytes.NewReader(raw))
		if derr != nil {
			return nil
		}
		defer dec.Close()
		out, rerr := io.ReadAll(dec)
		if rerr != nil {
			return nil
		}
		raw = out
	}
	text := string(raw)
	// .zstd 的会话（DeepSeek Harness）在打开前已经解压过；见 openSessionFile。
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func isoFromAny(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		if t == "" {
			return ""
		}
		if ts, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return ts.UTC().Format(time.RFC3339)
		}
		if ts, err := time.Parse("2006-01-02T15:04:05.000Z", t); err == nil {
			return ts.UTC().Format(time.RFC3339)
		}
		return t
	case float64:
		return msToISO(t)
	case int64:
		return msToISO(float64(t))
	case json.Number:
		f, _ := t.Float64()
		return msToISO(f)
	}
	return ""
}

func msToISO(v float64) string {
	if v <= 0 {
		return ""
	}
	// 秒还是毫秒：1e12 以下按秒（Node 版同样的判断）
	if v < 1e12 {
		return time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
	}
	return time.UnixMilli(int64(v)).UTC().Format(time.RFC3339)
}

// textOf flattens a content value the way the Node reader does: a string, an
// array of blocks with .text/.content, or an object with those keys.
func textOf(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return truncateText(t)
	case []any:
		var b strings.Builder
		for _, item := range t {
			b.WriteString(textOf(item))
		}
		return truncateText(b.String())
	case map[string]any:
		if s, ok := t["text"]; ok {
			return textOf(s)
		}
		if s, ok := t["content"]; ok {
			return textOf(s)
		}
	}
	return ""
}

func truncateText(s string) string {
	if len(s) > maxText {
		return s[:maxText]
	}
	return s
}

// firstLine cuts at the first newline and then at n runes, like the Node helper.
func firstLine(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

func dig(m map[string]any, path string) any {
	if path == "" || m == nil {
		return nil
	}
	var cur any = m
	for _, seg := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[seg]
	}
	return cur
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	return ""
}

// ---- claude-code ---------------------------------------------------------

type claudeFormat struct{}

// claudeRole accepts both {"type":"user"} and {"type":"message","role":"user"}
// (CodeBuddy writes the latter).
func claudeRole(row map[string]any) string {
	if r := strings.ToLower(asString(row["role"])); r == "user" || r == "assistant" {
		return r
	}
	if t := strings.ToLower(asString(row["type"])); t == "user" || t == "assistant" {
		return t
	}
	return ""
}

func (claudeFormat) ListFiles(files []string) ([]FormatSession, error) {
	out := make([]FormatSession, 0, len(files))
	for _, path := range files {
		s := FormatSession{Path: path, TitleSource: "first_prompt"}
		var firstPrompt string
		for _, line := range readLines(path) {
			var row map[string]any
			if json.Unmarshal([]byte(line), &row) != nil {
				continue
			}
			if s.SessionID == "" {
				s.SessionID = asString(row["sessionId"])
			}
			if s.Cwd == "" {
				s.Cwd = asString(row["cwd"])
			}
			if s.Branch == "" {
				if b := asString(row["gitBranch"]); b != "" && b != "HEAD" {
					s.Branch = b
				}
			}
			if s.Model == "" {
				if msg, ok := row["message"].(map[string]any); ok {
					s.Model = asString(msg["model"])
				}
			}
			if ts := isoFromAny(row["timestamp"]); ts != "" {
				s.LastTS = ts
			}
			switch asString(row["type"]) {
			case "custom-title":
				if t := asString(row["customTitle"]); t != "" {
					s.Title, s.TitleSource = t, "custom"
				}
			case "ai-title":
				if t := asString(row["aiTitle"]); t != "" && s.TitleSource != "custom" {
					s.Title, s.TitleSource = t, "agent"
				}
			case "summary":
				if t := asString(row["summary"]); t != "" && s.Title == "" {
					s.Title, s.TitleSource = t, "agent"
				}
			}
			if firstPrompt == "" && claudeRole(row) == "user" {
				content := row["content"]
				if content == nil {
					if msg, ok := row["message"].(map[string]any); ok {
						content = msg["content"]
					}
				}
				if t := textOf(content); t != "" {
					firstPrompt = t
				}
			}
		}
		if s.SessionID == "" {
			s.SessionID = baseWithoutExt(path, ".jsonl")
		}
		if s.Title == "" && firstPrompt != "" {
			s.Title = firstLine(firstPrompt, 80)
		}
		if s.SessionID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (claudeFormat) ReadItems(path string, limit int) ([]FormatItem, error) {
	var items []FormatItem
	for _, line := range readLines(path) {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		role := claudeRole(row)
		if role == "" {
			continue
		}
		content := row["content"]
		if content == nil {
			if msg, ok := row["message"].(map[string]any); ok {
				content = msg["content"]
			}
		}
		text := textOf(content)
		if text == "" {
			continue
		}
		items = append(items, FormatItem{Role: role, Kind: "message", Text: text, At: isoFromAny(row["timestamp"])})
	}
	return tailItems(items, limit), nil
}

func (claudeFormat) HasConversation(path string) bool {
	items, _ := (claudeFormat{}).ReadItems(path, 5)
	return len(items) > 0
}

// ---- codex ---------------------------------------------------------------

type codexFormat struct{}

func (codexFormat) ListFiles(files []string) ([]FormatSession, error) {
	out := make([]FormatSession, 0, len(files))
	for _, path := range files {
		s := FormatSession{Path: path, TitleSource: "first_prompt"}
		var firstPrompt string
		for _, line := range readLines(path) {
			var row map[string]any
			if json.Unmarshal([]byte(line), &row) != nil {
				continue
			}
			if ts := isoFromAny(row["timestamp"]); ts != "" {
				s.LastTS = ts
			}
			payload, _ := row["payload"].(map[string]any)
			if payload == nil {
				continue
			}
			switch asString(payload["type"]) {
			case "":
			}
			if asString(row["type"]) == "session_meta" {
				if s.SessionID == "" {
					s.SessionID = asString(payload["id"])
				}
				if s.Cwd == "" {
					s.Cwd = asString(payload["cwd"])
				}
				if s.Branch == "" {
					if git, ok := payload["git"].(map[string]any); ok {
						s.Branch = asString(git["branch"])
					}
				}
			}
			if asString(payload["type"]) == "message" {
				if s.Model == "" {
					s.Model = asString(payload["model"])
				}
				if firstPrompt == "" && asString(payload["role"]) == "user" {
					firstPrompt = textOf(payload["content"])
				}
			}
			if asString(payload["type"]) == "turn_context" {
				if m := asString(payload["model"]); m != "" {
					s.Model = m
				}
			}
		}
		if s.SessionID == "" {
			s.SessionID = baseWithoutExt(path, ".jsonl")
		}
		if firstPrompt != "" {
			s.Title = firstLine(firstPrompt, 80)
		}
		if s.SessionID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (codexFormat) ReadItems(path string, limit int) ([]FormatItem, error) {
	var items []FormatItem
	for _, line := range readLines(path) {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		payload, _ := row["payload"].(map[string]any)
		if payload == nil || asString(payload["type"]) != "message" {
			continue
		}
		text := textOf(payload["content"])
		if text == "" {
			continue
		}
		items = append(items, FormatItem{
			Role: asString(payload["role"]), Kind: "message",
			Text: text, At: isoFromAny(row["timestamp"]),
		})
	}
	return tailItems(items, limit), nil
}

func (codexFormat) HasConversation(path string) bool {
	items, _ := (codexFormat{}).ReadItems(path, 5)
	return len(items) > 0
}

// ---- gemini-cli ----------------------------------------------------------

type geminiFormat struct{}

func (geminiFormat) ListFiles(files []string) ([]FormatSession, error) {
	out := make([]FormatSession, 0, len(files))
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		msgs, _ := doc["messages"].([]any)
		var firstUser string
		var last *map[string]any
		for _, m := range msgs {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			last = &mm
			if firstUser == "" {
				kind := strings.ToLower(asString(mm["type"]) + asString(mm["role"]))
				if strings.Contains(kind, "user") {
					firstUser = textOf(mm["content"])
				}
			}
		}
		s := FormatSession{
			Path:        path,
			SessionID:   firstNonEmpty(asString(doc["sessionId"]), asString(doc["id"]), baseWithoutExt(path, ".json")),
			Cwd:         asString(doc["cwd"]),
			Model:       asString(doc["model"]),
			TitleSource: "first_prompt",
			Title:       firstLine(firstUser, 80),
		}
		for _, key := range []string{"lastUpdated", "updatedAt"} {
			if ts := isoFromAny(doc[key]); ts != "" {
				s.LastTS = ts
				break
			}
		}
		if s.LastTS == "" && last != nil {
			s.LastTS = isoFromAny((*last)["timestamp"])
		}
		if s.SessionID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (geminiFormat) ReadItems(path string, limit int) ([]FormatItem, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return nil, nil
	}
	msgs, _ := doc["messages"].([]any)
	var items []FormatItem
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(asString(mm["type"]) + asString(mm["role"]))
		role := "assistant"
		if strings.Contains(kind, "user") {
			role = "user"
		}
		text := textOf(mm["content"])
		if text == "" {
			continue
		}
		items = append(items, FormatItem{Role: role, Kind: "message", Text: text, At: isoFromAny(mm["timestamp"])})
	}
	return tailItems(items, limit), nil
}

func (geminiFormat) HasConversation(path string) bool {
	items, _ := (geminiFormat{}).ReadItems(path, 5)
	return len(items) > 0
}

// ---- dsh（DeepSeek Harness）---------------------------------------------

// dshFormat reads DeepSeek Harness session.v4.jsonl.zstd. Records are nested:
// user text lives at data.content, assistant text at data.message.content, and
// the readable title is emitted later as a session/title record.
type dshFormat struct{}

var dshNoise = regexp.MustCompile(`(?is)^\s*(current runtime context\.|<system-reminder|# AGENTS\.md instructions|<available_skills|<skills_instructions)`)

func dshContentText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, block := range v {
			row, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if asString(row["type"]) == "text" {
				if text := asString(row["text"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func dshRole(row map[string]any) (role, text string) {
	data, _ := row["data"].(map[string]any)
	if data == nil {
		return "", ""
	}
	switch asString(row["type"]) {
	case "user/message":
		source, _ := data["source"].(map[string]any)
		kind := asString(source["kind"])
		if kind != "" && kind != "user" {
			return "", ""
		}
		text = dshContentText(data["content"])
		if dshNoise.MatchString(text) {
			return "", ""
		}
		return "user", text
	case "assistant/message":
		message, _ := data["message"].(map[string]any)
		if message == nil {
			return "", ""
		}
		text = dshContentText(message["content"])
		if text == "" {
			return "", ""
		}
		return "assistant", text
	}
	return "", ""
}

func dshTime(row map[string]any) string {
	return isoFromAny(row["time"])
}

func (dshFormat) ListFiles(files []string) ([]FormatSession, error) {
	out := make([]FormatSession, 0, len(files))
	for _, path := range files {
		s := FormatSession{Path: path, TitleSource: "first_prompt"}
		var firstPrompt string
		var fallbackTitle string
		for _, line := range readLines(path) {
			var row map[string]any
			if json.Unmarshal([]byte(line), &row) != nil {
				continue
			}
			if asString(row["type"]) == "session" {
				if s.SessionID == "" {
					s.SessionID = asString(row["id"])
				}
				if s.Cwd == "" {
					s.Cwd = asString(row["cwd"])
				}
			}
			if asString(row["type"]) == "request/context" {
				data, _ := row["data"].(map[string]any)
				if s.Model == "" {
					s.Model = asString(data["model"])
				}
			}
			if asString(row["type"]) == "session/title" {
				data, _ := row["data"].(map[string]any)
				title := asString(data["title"])
				source, _ := data["source"].(map[string]any)
				if title != "" && (asString(source["kind"]) == "provider" || fallbackTitle == "") {
					if asString(source["kind"]) == "provider" {
						s.Title, s.TitleSource = title, "agent"
					} else {
						fallbackTitle = title
					}
				}
			}
			if ts := dshTime(row); ts != "" {
				s.LastTS = ts
			}
			if firstPrompt == "" {
				if role, text := dshRole(row); role == "user" && text != "" {
					firstPrompt = text
				}
			}
		}
		if s.SessionID == "" {
			s.SessionID = baseWithoutExt(path, ".jsonl.zstd")
			if strings.HasSuffix(path, ".jsonl.zstd") {
				s.SessionID = strings.TrimSuffix(filepath.Base(path), ".jsonl.zstd")
			}
		}
		if s.Title == "" {
			if fallbackTitle != "" {
				s.Title = fallbackTitle
			} else if firstPrompt != "" {
				s.Title = firstLine(firstPrompt, 80)
			}
		}
		if s.SessionID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (dshFormat) ReadItems(path string, limit int) ([]FormatItem, error) {
	var items []FormatItem
	for _, line := range readLines(path) {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		role, text := dshRole(row)
		if role == "" || text == "" {
			continue
		}
		items = append(items, FormatItem{
			Role: role,
			Kind: "message",
			Text: text,
			At:   dshTime(row),
		})
	}
	return tailItems(items, limit), nil
}

func (dshFormat) HasConversation(path string) bool {
	items, _ := (dshFormat{}).ReadItems(path, 5)
	return len(items) > 0
}

// ---- openclaw ------------------------------------------------------------

// openclawFormat reads ~/.openclaw/agents/<agent>/sessions/<id>.jsonl. The
// session header has the id/cwd; message records nest role and content under
// message. Thinking blocks are skipped.
type openclawFormat struct{}

var openclawTimestampPrefix = regexp.MustCompile(`^\[[^\]]+\]\s*`)

func openclawText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, block := range v {
			row, ok := block.(map[string]any)
			if !ok {
				continue
			}
			kind := asString(row["type"])
			if kind == "text" {
				if text := asString(row["text"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func openclawMessage(row map[string]any) (role, text, model, at string) {
	if asString(row["type"]) != "message" {
		return "", "", "", ""
	}
	message, _ := row["message"].(map[string]any)
	if message == nil {
		return "", "", "", ""
	}
	role = strings.ToLower(asString(message["role"]))
	if role != "user" && role != "assistant" {
		return "", "", "", ""
	}
	text = strings.TrimSpace(openclawText(message["content"]))
	if role == "user" {
		text = strings.TrimSpace(openclawTimestampPrefix.ReplaceAllString(text, ""))
	}
	return role, text, asString(message["model"]), isoFromAny(message["timestamp"])
}

func (openclawFormat) ListFiles(files []string) ([]FormatSession, error) {
	out := make([]FormatSession, 0, len(files))
	for _, path := range files {
		s := FormatSession{Path: path, TitleSource: "first_prompt"}
		var firstPrompt string
		for _, line := range readLines(path) {
			var row map[string]any
			if json.Unmarshal([]byte(line), &row) != nil {
				continue
			}
			switch asString(row["type"]) {
			case "session":
				if s.SessionID == "" {
					s.SessionID = asString(row["id"])
				}
				if s.Cwd == "" {
					s.Cwd = asString(row["cwd"])
				}
				if s.LastTS == "" {
					s.LastTS = isoFromAny(row["timestamp"])
				}
			case "model_change":
				if s.Model == "" {
					s.Model = asString(row["modelId"])
				}
			}
			if role, text, model, at := openclawMessage(row); role != "" {
				if s.Model == "" {
					s.Model = model
				}
				if at != "" {
					s.LastTS = at
				}
				if firstPrompt == "" && role == "user" && text != "" {
					firstPrompt = text
				}
			}
		}
		if s.SessionID == "" {
			s.SessionID = baseWithoutExt(path, ".jsonl")
		}
		if firstPrompt != "" {
			s.Title = firstLine(firstPrompt, 80)
		}
		if s.SessionID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (openclawFormat) ReadItems(path string, limit int) ([]FormatItem, error) {
	var items []FormatItem
	for _, line := range readLines(path) {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		role, text, _, at := openclawMessage(row)
		if role == "" || text == "" {
			continue
		}
		items = append(items, FormatItem{Role: role, Kind: "message", Text: text, At: at})
	}
	return tailItems(items, limit), nil
}

func (openclawFormat) HasConversation(path string) bool {
	items, _ := (openclawFormat{}).ReadItems(path, 5)
	return len(items) > 0
}

// ---- generic-json（单文件 JSON / JSON 数组）-------------------------------

// genericJSONFormat covers the large family of agents that write one JSON file
// per session, either as a top-level object with a messages array or as a plain
// array of message objects. Field names can still be overridden with the same
// format_options as generic-jsonl/custom.
type genericJSONFormat struct {
	mapping map[string]any
}

var genericJSONFields = map[string][]string{
	"session_id": {"session_id", "sessionId", "id"},
	"cwd":        {"cwd", "workdir", "working_directory"},
	"model":      {"model"},
	"time":       {"time", "timestamp", "createdAt", "updatedAt", "lastUpdated"},
	"title":      {"title", "name", "summary"},
	"role":       {"role", "type", "author"},
	"text":       {"text", "content", "message"},
}

func (g genericJSONFormat) pick(row, root map[string]any, key string) any {
	if path, ok := g.mapping[key].(string); ok && path != "" {
		if value := dig(row, path); value != nil {
			return value
		}
		if value := dig(root, path); value != nil {
			return value
		}
	}
	for _, path := range genericJSONFields[key] {
		if value := dig(row, path); value != nil {
			return value
		}
		if value := dig(root, path); value != nil {
			return value
		}
	}
	return nil
}

func (g genericJSONFormat) roleOf(row, root map[string]any) string {
	raw := strings.ToLower(asString(g.pick(row, root, "role")))
	if raw == "" {
		return ""
	}
	users := stringList(g.mapping["user_values"], []string{"user", "human", "prompt"})
	assist := stringList(g.mapping["assistant_values"], []string{"assistant", "agent", "gemini", "model"})
	for _, value := range users {
		if strings.EqualFold(value, raw) {
			return "user"
		}
	}
	for _, value := range assist {
		if strings.EqualFold(value, raw) {
			return "assistant"
		}
	}
	return ""
}

func (g genericJSONFormat) ListFiles(files []string) ([]FormatSession, error) {
	out := make([]FormatSession, 0, len(files))
	for _, path := range files {
		rows, root := readJSONDocumentRows(path)
		if len(rows) == 0 {
			continue
		}
		s := FormatSession{Path: path, TitleSource: "first_prompt"}
		var firstPrompt string
		for _, row := range rows {
			if s.SessionID == "" {
				s.SessionID = asString(g.pick(row, root, "session_id"))
			}
			if s.Cwd == "" {
				s.Cwd = asString(g.pick(row, root, "cwd"))
			}
			if s.Model == "" {
				s.Model = asString(g.pick(row, root, "model"))
			}
			if ts := isoFromAny(g.pick(row, root, "time")); ts != "" {
				s.LastTS = ts
			}
			if title := asString(g.pick(row, root, "title")); title != "" && s.Title == "" {
				s.Title, s.TitleSource = title, "agent"
			}
			if firstPrompt == "" && g.roleOf(row, root) == "user" {
				firstPrompt = textOf(g.pick(row, root, "text"))
			}
		}
		if s.SessionID == "" {
			s.SessionID = baseWithoutExt(path, ".json")
		}
		if s.Title == "" && firstPrompt != "" {
			s.Title = firstLine(firstPrompt, 80)
		}
		if s.SessionID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (g genericJSONFormat) ReadItems(path string, limit int) ([]FormatItem, error) {
	rows, root := readJSONDocumentRows(path)
	var items []FormatItem
	for _, row := range rows {
		role := g.roleOf(row, root)
		if role == "" {
			continue
		}
		text := textOf(g.pick(row, root, "text"))
		if text == "" {
			continue
		}
		items = append(items, FormatItem{
			Role: role,
			Kind: "message",
			Text: text,
			At:   isoFromAny(g.pick(row, root, "time")),
		})
	}
	return tailItems(items, limit), nil
}

func (g genericJSONFormat) HasConversation(path string) bool {
	items, _ := g.ReadItems(path, 5)
	return len(items) > 0
}

// ---- custom（字段映射）----------------------------------------------------

type customFormat struct {
	mapping map[string]any
}

func (c customFormat) pick(row map[string]any, key string) any {
	if k, ok := c.mapping[key].(string); ok && k != "" {
		return dig(row, k)
	}
	return nil
}

func (c customFormat) roleOf(row map[string]any) string {
	raw := strings.ToLower(asString(c.pick(row, "role")))
	if raw == "" {
		return ""
	}
	users := stringList(c.mapping["user_values"], []string{"user"})
	assist := stringList(c.mapping["assistant_values"], []string{"assistant", "agent", "gemini", "model"})
	for _, u := range users {
		if strings.EqualFold(u, raw) {
			return "user"
		}
	}
	for _, a := range assist {
		if strings.EqualFold(a, raw) {
			return "assistant"
		}
	}
	return ""
}

func (c customFormat) ListFiles(files []string) ([]FormatSession, error) {
	out := make([]FormatSession, 0, len(files))
	for _, path := range files {
		s := FormatSession{Path: path, TitleSource: "first_prompt"}
		var firstPrompt string
		for _, line := range readLines(path) {
			var row map[string]any
			if json.Unmarshal([]byte(line), &row) != nil {
				continue
			}
			if s.SessionID == "" {
				s.SessionID = asString(c.pick(row, "session_id"))
			}
			if s.Cwd == "" {
				s.Cwd = asString(c.pick(row, "cwd"))
			}
			if s.Model == "" {
				s.Model = asString(c.pick(row, "model"))
			}
			if ts := isoFromAny(c.pick(row, "time")); ts != "" {
				s.LastTS = ts
			}
			if t := asString(c.pick(row, "title")); t != "" && s.Title == "" {
				s.Title, s.TitleSource = t, "agent"
			}
			if firstPrompt == "" && c.roleOf(row) == "user" {
				firstPrompt = textOf(c.pick(row, "text"))
			}
		}
		if s.SessionID == "" {
			s.SessionID = baseWithoutExt(path, ".jsonl")
		}
		if s.Title == "" && firstPrompt != "" {
			s.Title = firstLine(firstPrompt, 80)
		}
		if s.SessionID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (c customFormat) ReadItems(path string, limit int) ([]FormatItem, error) {
	var items []FormatItem
	for _, line := range readLines(path) {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		role := c.roleOf(row)
		if role == "" {
			continue
		}
		text := textOf(c.pick(row, "text"))
		if text == "" {
			continue
		}
		items = append(items, FormatItem{Role: role, Kind: "message", Text: text, At: isoFromAny(c.pick(row, "time"))})
	}
	return tailItems(items, limit), nil
}

func (c customFormat) HasConversation(path string) bool {
	items, _ := c.ReadItems(path, 5)
	return len(items) > 0
}

// ---- 小工具 --------------------------------------------------------------

func tailItems(items []FormatItem, limit int) []FormatItem {
	if limit > 0 && len(items) > limit {
		return items[len(items)-limit:]
	}
	return items
}

func baseWithoutExt(path, ext string) string {
	base := path
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, ext)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func stringList(v any, def []string) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s := asString(x); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	case []string:
		if len(t) > 0 {
			return t
		}
	case string:
		if t != "" {
			return []string{t}
		}
	}
	return def
}

// readJSONDocumentRows accepts a JSON array, a single JSON object, or a JSONL
// file. A top-level object with a messages array is treated as one session and
// the object itself supplies session id / cwd / model when messages do not.
func readJSONDocumentRows(path string) ([]map[string]any, map[string]any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil, nil
	}
	if strings.HasPrefix(text, "[") {
		var rows []map[string]any
		if json.Unmarshal([]byte(text), &rows) == nil {
			return rows, nil
		}
	}
	if strings.HasPrefix(text, "{") {
		var root map[string]any
		if json.Unmarshal([]byte(text), &root) == nil {
			if messages, ok := root["messages"].([]any); ok {
				rows := make([]map[string]any, 0, len(messages))
				for _, message := range messages {
					if row, ok := message.(map[string]any); ok {
						rows = append(rows, row)
					}
				}
				return rows, root
			}
			return []map[string]any{root}, root
		}
	}
	rows := make([]map[string]any, 0)
	for _, line := range strings.Split(text, "\n") {
		var row map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(line)), &row) == nil {
			rows = append(rows, row)
		}
	}
	return rows, nil
}
