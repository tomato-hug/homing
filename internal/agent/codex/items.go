package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tomato-hug/homing/internal/agent"
)

// ReadItems implements agent.ItemReader for a Codex rollout: the last `limit`
// entries, numbered `codex-i1`, `codex-i2`, … in file order (same as Node).
func (Agent) ReadItems(_ context.Context, sessionID string, limit int) ([]agent.Item, error) {
	file := rolloutPathFor(sessionID)
	if file == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	rows := rolloutTail(file, 2*1024*1024)
	st := &codexItems{calls: map[string]string{}, byCall: map[string]*codexCall{}}
	var order []string
	byID := map[string]agent.Item{}

	for _, row := range rows {
		for _, it := range st.row(row) {
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
	return out, nil
}

type codexCall struct {
	id   string
	kind string
	name string
}

type codexItems struct {
	seq    int
	model  string
	calls  map[string]string
	byCall map[string]*codexCall
}

func (s *codexItems) next() (string, int) {
	s.seq++
	return fmt.Sprintf("codex-i%d", s.seq), s.seq
}

func (s *codexItems) row(row map[string]any) []agent.Item {
	payload, _ := row["payload"].(map[string]any)
	if payload == nil {
		return nil
	}
	at := str(row["timestamp"])

	if str(row["type"]) == "turn_context" {
		if m := str(payload["model"]); m != "" {
			s.model = m
		}
	}
	if str(row["type"]) == "event_msg" && str(payload["type"]) == "thread_settings_applied" {
		if ts, ok := payload["thread_settings"].(map[string]any); ok {
			if m := str(ts["model"]); m != "" {
				s.model = m
			}
		}
	}

	switch str(payload["type"]) {
	case "message":
		text := strings.TrimSpace(contentText(payload["content"]))
		if text == "" {
			return nil
		}
		role := "user"
		if str(payload["role"]) == "assistant" {
			role = "assistant"
		}
		if role == "user" && codexNoise(text) {
			return nil
		}
		id, seq := s.next()
		return []agent.Item{{ID: id, Role: role, Kind: role, Seq: seq, At: at, Text: clip(text, 4000)}}

	case "reasoning":
		summary := ""
		if parts, ok := payload["summary"].([]any); ok {
			var b strings.Builder
			for _, p := range parts {
				if m, ok := p.(map[string]any); ok {
					b.WriteString(str(m["text"]))
					b.WriteString("\n")
				}
			}
			summary = strings.TrimSpace(b.String())
		}
		text := clip(firstOf(summary, contentText(payload["content"])), 4000)
		if text == "" {
			return nil
		}
		id, seq := s.next()
		return []agent.Item{{ID: id, Role: "assistant", Kind: "reasoning", Seq: seq, At: at, Text: text}}

	case "function_call":
		callID := str(payload["call_id"])
		id := fmt.Sprintf("codex-tool-%s", callID)
		s.calls[callID] = id
		name := firstOf(str(payload["name"]), "shell")
		command := codexCommand(str(payload["arguments"]))
		s.byCall[callID] = &codexCall{id: id, kind: "tool_call", name: name}
		return []agent.Item{{ID: id, Role: "assistant", Kind: "tool_call", Seq: s.seq, At: at, Text: clip(command, 300)}}

	case "function_call_output":
		callID := str(payload["call_id"])
		output := str(payload["output"])
		return s.toolResult(callID, output, at)

	case "custom_tool_call":
		callID := str(payload["call_id"])
		id := fmt.Sprintf("codex-tool-%s", callID)
		s.calls[callID] = id
		name := firstOf(str(payload["name"]), "tool")
		s.byCall[callID] = &codexCall{id: id, kind: "tool_call", name: name}
		text := clip(str(payload["input"]), 300)
		return []agent.Item{{ID: id, Role: "assistant", Kind: "tool_call", Seq: s.seq, At: at, Text: text}}

	case "custom_tool_call_output":
		callID := str(payload["call_id"])
		return s.toolResult(callID, str(payload["output"]), at)
	}
	return nil
}

func (s *codexItems) toolResult(callID, output, at string) []agent.Item {
	known := s.byCall[callID]
	id := fmt.Sprintf("codex-tool-%s", callID)
	kind := "tool_call"
	if known != nil && known.id != "" {
		id = known.id
		if known.kind != "" {
			kind = known.kind
		}
	} else if cached, ok := s.calls[callID]; ok {
		id = cached
	}
	errText := ""
	if code, ok := exitCodeOf(output); ok && code != 0 {
		errText = fmt.Sprintf("exit_code=%d", code)
	}
	return []agent.Item{{
		ID: id, Role: "tool", Kind: kind, Seq: s.seq, At: at,
		Text:  tailRunes(output, 800),
		Error: errText,
	}}
}

// codexCommand pulls cmd out of the JSON arguments, falling back to raw text.
func codexCommand(arguments string) string {
	var doc struct {
		Cmd string `json:"cmd"`
	}
	if json.Unmarshal([]byte(arguments), &doc) == nil && doc.Cmd != "" {
		return doc.Cmd
	}
	return arguments
}

func rolloutTail(file string, window int64) []map[string]any {
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

func codexNoise(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "<") ||
		strings.HasPrefix(strings.TrimSpace(text), "# AGENTS.md")
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

func exitCodeOf(text string) (int, bool) {
	for _, line := range strings.Split(text, "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if i := strings.Index(l, "process exited with code"); i >= 0 {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(l[i+len("process exited with code"):]), "%d", &n); err == nil {
				return n, true
			}
		}
	}
	return 0, false
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

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
