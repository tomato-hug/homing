package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/tomato-hug/homing/internal/agent"
)

// ReadItems implements agent.ItemReader: the last `limit` entries of a session.
//
// The Node relay reads only the tail of the file (2 MB window), skips the first
// partial line, and numbers items `claude-i1`, `claude-i2`, … in file order. The
// Go version does the same so both servers describe a conversation the same way.
func (Agent) ReadItems(_ context.Context, sessionID string, limit int) ([]agent.Item, error) {
	file := filePathFor(sessionID)
	if file == "" {
		return nil, nil
	}
	return ExtractFileItems(file, id, limit), nil
}

// ExtractFileItems is ReadItems for a known file path. WorkBuddy's transcripts
// use the same record shape, so that package calls this instead of duplicating it.
func ExtractFileItems(file, source string, limit int) []agent.Item {
	if limit <= 0 {
		limit = 200
	}
	rows := tailRows(file, 2*1024*1024)
	st := &itemState{source: source, calls: map[string]string{}, byCall: map[string]*callInfo{}}
	var order []string
	byID := map[string]agent.Item{}

	for _, row := range rows {
		for _, it := range extractRow(st, row) {
			if prev, seen := byID[it.ID]; seen {
				it = agent.MergeItem(prev, it) // 与 Node 的按 id 合并一致
			} else {
				order = append(order, it.ID)
			}
			byID[it.ID] = it
		}
	}
	out := make([]agent.Item, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

type callInfo struct {
	id        string
	kind      string
	name      string
	files     []agent.ItemFile
	plan      []agent.ItemPlan
	questions []agent.ItemQuestion
}

type itemState struct {
	source string
	seq    int
	model  string
	calls  map[string]string
	byCall map[string]*callInfo
}

func (s *itemState) next(at string) (string, int) {
	s.seq++
	return fmt.Sprintf("%s-i%d", s.source, s.seq), s.seq
}

func extractRow(st *itemState, row map[string]any) []agent.Item {
	if row == nil {
		return nil
	}
	at := itemTime(row)
	msg, _ := row["message"].(map[string]any)
	if m := str(msg["model"]); m != "" {
		st.model = m
	} else if pd, ok := row["providerData"].(map[string]any); ok {
		if m := str(pd["model"]); m != "" {
			st.model = m
		}
	}
	model := st.model

	switch str(row["type"]) {
	case "reasoning":
		text := clip(normalize(row["content"], row["text"]), 300)
		if text == "" {
			return nil
		}
		id, seq := st.next(at)
		return []agent.Item{{TS: at, ID: id, Seq: seq, Type: "reasoning", Role: "assistant", Text: text, Model: model}}

	case "function_call":
		callID := firstOf(str(row["callId"]), str(row["id"]))
		toolID := fmt.Sprintf("%s-tool-%s", st.source, callID)
		st.calls[callID] = toolID
		rawInput := row["arguments"]
		if rawInput == nil {
			rawInput = row["input"]
		}
		input := clip(firstOf(str(row["arguments"]), str(row["input"])), 300)
		name := firstOf(str(row["name"]), "tool")
		if agent.IsInteractiveQuestionTool(name) {
			questions, summary := agent.ParseInteractiveQuestions(name, rawInput)
			st.byCall[callID] = &callInfo{id: toolID, kind: "question", name: name, questions: questions}
			clipped := clip(summary, 300)
			if clipped == "" {
				clipped = input
			}
			return []agent.Item{{
				TS: at, ID: toolID, Seq: st.seq, Type: "question", Role: "assistant",
				Title: firstQuestionTitle(questions, name), Text: summary, Questions: questions,
				Command: clipped, Model: model,
				Tool: &agent.ItemTool{Name: name, InputSummary: &clipped, Status: "running"},
			}}
		}
		st.byCall[callID] = &callInfo{id: toolID, kind: "tool_call", name: name}
		return []agent.Item{{
			TS: at, ID: toolID, Seq: st.seq, Type: "tool_call", Role: "assistant",
			Command: input, Model: model,
			Tool: &agent.ItemTool{Name: name, InputSummary: &input, Status: "running"},
		}}

	case "function_call_result":
		callID := firstOf(str(row["callId"]), str(row["id"]))
		return toolResult(st, callID, normalize(row["output"], clip(jsonText(row["output"]), 800)),
			toolStatus(str(row["status"])), at, model)
	}

	blocks := contentBlocks(row, msg)
	role := firstOf(str(msg["role"]), str(row["role"]))
	if role == "" {
		switch str(row["type"]) {
		case "assistant":
			role = "assistant"
		case "user":
			role = "user"
		}
	}

	var out []agent.Item
	for _, raw := range blocks {
		switch b := raw.(type) {
		case string:
			text := strings.TrimSpace(b)
			if text == "" {
				continue
			}
			id, seq := st.next(at)
			out = append(out, agent.Item{TS: at, ID: id, Seq: seq, Type: itemRole(role), Role: itemRole(role), Text: clip(text, 4000), Model: model})
		case map[string]any:
			switch str(b["type"]) {
			case "text", "input_text", "output_text":
				text := strings.TrimSpace(str(b["text"]))
				if text == "" || (role == "user" && noise(text)) {
					continue
				}
				id, seq := st.next(at)
				out = append(out, agent.Item{TS: at, ID: id, Seq: seq, Type: itemRole(role), Role: itemRole(role), Text: clip(text, 4000), Model: model})

			case "thinking":
				text := clip(firstOf(str(b["thinking"]), str(b["text"])), 300)
				if text == "" {
					continue
				}
				id, seq := st.next(at)
				out = append(out, agent.Item{TS: at, ID: id, Seq: seq, Type: "reasoning", Role: "assistant", Text: text, Model: model})

			case "tool_use":
				callID := str(b["id"])
				toolID := fmt.Sprintf("%s-tool-%s", st.source, callID)
				st.calls[callID] = toolID
				name := firstOf(str(b["name"]), "tool")
				input, _ := b["input"].(map[string]any)

				files := claudeFileChange(name, input)
				if len(files) > 0 {
					st.byCall[callID] = &callInfo{id: toolID, kind: "file_change", name: name, files: files}
					paths := make([]string, 0, len(files))
					for _, f := range files {
						paths = append(paths, f.Path)
					}
					summary := strings.Join(paths, ", ")
					out = append(out, agent.Item{
						TS: at, ID: toolID, Seq: st.seq, Type: "file_change", Role: "assistant",
						Files: files, Model: model,
						Tool: &agent.ItemTool{Name: name, InputSummary: &summary, Status: "running"},
					})
					continue
				}
				if agent.IsInteractiveQuestionTool(name) {
					questions, summary := agent.ParseInteractiveQuestions(name, b["input"])
					st.byCall[callID] = &callInfo{id: toolID, kind: "question", name: name, questions: questions}
					clipped := clip(summary, 300)
					out = append(out, agent.Item{
						TS: at, ID: toolID, Seq: st.seq, Type: "question", Role: "assistant",
						Title: firstQuestionTitle(questions, name), Text: summary, Questions: questions,
						Model: model,
						Tool:  &agent.ItemTool{Name: name, InputSummary: &clipped, Status: "running"},
					})
					continue
				}
				summary := shortJson(input, 300)
				if name == "Bash" {
					summary = str(input["command"])
				}
				st.byCall[callID] = &callInfo{id: toolID, kind: "tool_call", name: name}
				clipped := clip(summary, 300)
				item := agent.Item{
					TS: at, ID: toolID, Seq: st.seq, Type: "tool_call", Role: "assistant",
					Model: model,
					Tool:  &agent.ItemTool{Name: name, InputSummary: &clipped, Status: "running"},
				}
				if name == "Bash" {
					item.Command = summary
				}
				out = append(out, item)

			case "tool_result":
				callID := str(b["tool_use_id"])
				content := normalize(b["content"])
				if content == "" {
					content = shortJson(b["content"], 800)
				}
				status := "done"
				if truthy(b["is_error"]) {
					status = "failed"
				}
				out = append(out, toolResult(st, callID, content, status, at, model)...)
			}
		}
	}
	return out
}

func toolResult(st *itemState, callID, content, status, at, model string) []agent.Item {
	known := st.byCall[callID]
	id := toolIDFor(st, callID)
	kind := "tool_call"
	name := "tool"
	var files []agent.ItemFile
	var plan []agent.ItemPlan
	var questions []agent.ItemQuestion
	if known != nil {
		if known.kind != "" {
			kind = known.kind
		}
		if known.name != "" {
			name = known.name
		}
		files = known.files
		plan = known.plan
		questions = known.questions
	}
	tailText := tailRunes(content, 800)
	code, hasCode := extractExitCode(content)
	item := agent.Item{
		TS: at, ID: id, Seq: st.seq, Type: kind, Role: "tool",
		Model: model, Files: files, Plan: plan, Questions: questions,
		Tool: &agent.ItemTool{
			Name:       name,
			OutputTail: &tailText,
			Status:     status,
		},
	}
	if kind == "question" && len(questions) > 0 {
		item.Title = firstQuestionTitle(questions, name)
		item.Text = agent.FormatQuestionSummary(questions, name)
	}
	if hasCode {
		item.Tool.ExitCode = &code
	}
	return []agent.Item{item}
}

func firstQuestionTitle(questions []agent.ItemQuestion, fallback string) string {
	for _, q := range questions {
		if h := strings.TrimSpace(q.Header); h != "" {
			return h
		}
	}
	if strings.EqualFold(strings.TrimSpace(fallback), "ExitPlanMode") {
		return "计划确认"
	}
	return "待确认决策"
}

func toolIDFor(st *itemState, callID string) string {
	if known, ok := st.byCall[callID]; ok && known.id != "" {
		return known.id
	}
	if id, ok := st.calls[callID]; ok {
		return id
	}
	return fmt.Sprintf("%s-tool-%s", st.source, callID)
}

// ---- 与 Node 对齐的小工具 ------------------------------------------------

func itemRole(role string) string {
	if role == "assistant" {
		return "assistant"
	}
	return "user"
}

func contentBlocks(row, msg map[string]any) []any {
	if v, ok := msg["content"].([]any); ok {
		return v
	}
	if v, ok := row["content"].([]any); ok {
		return v
	}
	if s, ok := msg["content"].(string); ok {
		return []any{map[string]any{"type": "text", "text": s}}
	}
	if s, ok := row["content"].(string); ok {
		return []any{map[string]any{"type": "text", "text": s}}
	}
	return nil
}

func normalize(values ...any) string {
	for _, v := range values {
		if s := strings.TrimSpace(contentText(v)); s != "" {
			return s
		}
	}
	return ""
}

func contentText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				b.WriteString(str(m["text"]))
			} else {
				b.WriteString(str(item))
			}
		}
		return b.String()
	case map[string]any:
		return str(t["text"])
	}
	return ""
}

func noise(text string) bool {
	t := strings.TrimSpace(text)
	for _, prefix := range []string{
		"<skills_instructions>", "<environment_context>", "# AGENTS.md instructions",
		"<user_instructions>", "<permissions", "<INSTRUCTIONS", "<environment",
		"<tools>", "<multi_agent_mode>", "<system-reminder", "<command-name",
		"<command-message", "<local-command",
	} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// claudeFileChange mirrors the Node helper: Edit / Write / MultiEdit become files[].
func claudeFileChange(name string, input map[string]any) []agent.ItemFile {
	if input == nil {
		return nil
	}
	filePath := firstOf(str(input["file_path"]), str(input["path"]))
	switch name {
	case "Edit":
		oldText := str(input["old_string"])
		newText := str(input["new_string"])
		diff := "+ " + newText
		if oldText != "" && newText != "" {
			diff = "- " + oldText + "\n+ " + newText
		}
		return []agent.ItemFile{{
			Path:    filePath,
			Added:   countLines(newText),
			Removed: countLines(oldText),
			Diff:    clip(diff, 1500),
		}}
	case "Write", "MultiEdit":
		if edits, ok := input["edits"].([]any); ok && len(edits) > 0 {
			out := make([]agent.ItemFile, 0, len(edits))
			for _, e := range edits {
				m, _ := e.(map[string]any)
				oldText := str(m["old_string"])
				newText := str(m["new_string"])
				out = append(out, agent.ItemFile{
					Path:    firstOf(str(m["file_path"]), filePath),
					Added:   countLines(newText),
					Removed: countLines(oldText),
					Diff:    clip("- "+oldText+"\n+ "+newText, 1500),
				})
				if len(out) >= 20 {
					break
				}
			}
			return out
		}
		content := firstOf(str(input["content"]), str(input["new_string"]))
		return []agent.ItemFile{{Path: filePath, Added: countLines(content), Diff: clip(content, 1500)}}
	}
	return nil
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

func toolStatus(value string) string {
	switch strings.ToLower(value) {
	case "failed", "error":
		return "failed"
	case "running", "pending":
		return "running"
	}
	return "done"
}

var exitCodePattern = regexp.MustCompile(`(?i)(?:Process exited with code|exit code|Exit code:?)\s*(-?\d+)`)

func extractExitCode(text string) (int, bool) {
	m := exitCodePattern.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

func shortJson(v any, limit int) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return clip(s, limit)
	}
	raw, ok := marshalPlain(v)
	if !ok {
		return ""
	}
	return clip(raw, limit)
}

// tailRows reads the last window bytes of a JSONL file and parses whole lines,
// dropping the first (possibly truncated) one — the same rule as the Node relay.
func tailRows(file string, window int64) []map[string]any {
	st, err := os.Stat(file)
	if err != nil {
		return nil
	}
	start := st.Size() - window
	if start < 0 {
		start = 0
	}
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Seek(start, 0); err != nil {
		return nil
	}
	buf := make([]byte, st.Size()-start)
	n, _ := f.Read(buf)
	lines := strings.Split(string(buf[:n]), "\n")
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	out := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
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

func itemTime(row map[string]any) string {
	if ts := str(row["timestamp"]); ts != "" {
		return ts
	}
	if ts := str(row["ts"]); ts != "" {
		return ts
	}
	return ""
}

func jsonText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	raw, ok := marshalPlain(v)
	if !ok {
		return ""
	}
	return raw
}

// marshalPlain is JSON.stringify: unlike json.Marshal it leaves < > & as they are.
func marshalPlain(v any) (string, bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return strings.TrimSuffix(buf.String(), "\n"), true
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[len(r)-n:])
	}
	return s
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func truthy(v any) bool {
	b, _ := v.(bool)
	return b
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
