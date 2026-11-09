package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
			if _, seen := byID[it.ID]; !seen {
				order = append(order, it.ID)
			}
			byID[it.ID] = it   // 后写覆盖：与 Node 的 Map.set 一致
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
	id    string
	kind  string
	name  string
	files bool
	plan  bool
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

	switch str(row["type"]) {
	case "reasoning":
		text := clip(normalize(row["content"], row["text"]), 300)
		if text == "" {
			return nil
		}
		id, seq := st.next(at)
		return []agent.Item{{ID: id, Role: "assistant", Kind: "reasoning", Seq: seq, At: at, Text: text}}
	case "function_call":
		callID := firstOf(str(row["callId"]), str(row["id"]))
		toolID := fmt.Sprintf("%s-tool-%s", st.source, callID)
		st.calls[callID] = toolID
		input := clip(firstOf(str(row["arguments"]), str(row["input"])), 300)
		name := firstOf(str(row["name"]), "tool")
		st.byCall[callID] = &callInfo{id: toolID, kind: "tool_call", name: name}
		return []agent.Item{{ID: toolID, Role: "assistant", Kind: "tool_call", Seq: st.seq, At: at, Text: input}}
	case "function_call_result":
		callID := firstOf(str(row["callId"]), str(row["id"]))
		return toolResultItem(st, row, callID, at)
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
			out = append(out, agent.Item{ID: id, Role: itemRole(role), Kind: itemRole(role), Seq: seq, At: at, Text: clip(text, 4000)})
		case map[string]any:
			switch str(b["type"]) {
			case "text", "input_text", "output_text":
				text := strings.TrimSpace(str(b["text"]))
				if text == "" || (role == "user" && noise(text)) {
					continue
				}
				id, seq := st.next(at)
				out = append(out, agent.Item{ID: id, Role: itemRole(role), Kind: itemRole(role), Seq: seq, At: at, Text: clip(text, 4000)})
			case "thinking":
				text := clip(firstOf(str(b["thinking"]), str(b["text"])), 300)
				if text == "" {
					continue
				}
				id, seq := st.next(at)
				out = append(out, agent.Item{ID: id, Role: "assistant", Kind: "reasoning", Seq: seq, At: at, Text: text})
			case "tool_use":
				callID := str(b["id"])
				toolID := fmt.Sprintf("%s-tool-%s", st.source, callID)
				st.calls[callID] = toolID
				name := firstOf(str(b["name"]), "tool")
				input, _ := b["input"].(map[string]any)
				files := fileChange(name, input)
				kind := "tool_call"
				if files {
					kind = "file_change"
				}
				st.byCall[callID] = &callInfo{id: toolID, kind: kind, name: name, files: files}
				text := ""
				if files {
					text = fileSummary(input)
				} else {
					text = clip(jsonText(input), 300)
				}
				out = append(out, agent.Item{ID: toolID, Role: "assistant", Kind: kind, Seq: st.seq, At: at, Text: text})
			case "tool_result":
				callID := str(b["tool_use_id"])
				text := clip(jsonText(b["content"]), 800)
				known := st.byCall[callID]
				id := toolIDFor(st, callID)
				kind := "tool_call"
				if known != nil && known.kind != "" {
					kind = known.kind
				}
				out = append(out, agent.Item{ID: id, Role: "tool", Kind: kind, Seq: st.seq, At: at, Text: text})
			}
		}
	}
	return out
}

func toolResultItem(st *itemState, row map[string]any, callID, at string) []agent.Item {
	outText := normalize(row["output"])
	if outText == "" {
		outText = clip(jsonText(row["output"]), 800)
	}
	known := st.byCall[callID]
	id := toolIDFor(st, callID)
	kind := "tool_call"
	if known != nil && known.kind != "" {
		kind = known.kind
	}
	return []agent.Item{{
		ID: id, Role: "tool", Kind: kind, Seq: st.seq, At: at,
		Text:  tail(clip(outText, 1600), 800),
		Error: exitError(outText),
	}}
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

func fileChange(name string, input map[string]any) bool {
	if input == nil {
		return false
	}
	switch name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return true
	}
	return false
}

func fileSummary(input map[string]any) string {
	path := firstOf(str(input["file_path"]), str(input["path"]))
	return clip(path, 300)
}

func exitError(output string) string {
	code, ok := exitCodeOf(output)
	if !ok || code == 0 {
		return ""
	}
	return fmt.Sprintf("exit_code=%d", code)
}

func exitCodeOf(text string) (int, bool) {
	for _, line := range strings.Split(text, "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if i := strings.Index(l, "process exited with code"); i >= 0 {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(l[i+len("process exited with code"):]), "%d", &n); err == nil {
				return n, true
			}
		}
		if i := strings.Index(l, "exit code:"); i >= 0 {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(l[i+len("exit code:"):]), "%d", &n); err == nil {
				return n, true
			}
		}
	}
	return 0, false
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
	if ts := str(row["createdAt"]); ts != "" {
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
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func tail(s string, n int) string {
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

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
