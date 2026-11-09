package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
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
			byID[it.ID] = it // 后写覆盖：与 Node 的 Map.set 一致
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
	id    string
	kind  string
	name  string
	files []agent.ItemFile
	plan  []agent.ItemPlan
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
	model := s.model

	if str(row["type"]) == "turn_context" {
		if m := str(payload["model"]); m != "" {
			s.model = m
			model = m
		}
	}
	if str(row["type"]) == "event_msg" && str(payload["type"]) == "thread_settings_applied" {
		if ts, ok := payload["thread_settings"].(map[string]any); ok {
			if m := str(ts["model"]); m != "" {
				s.model = m
				model = m
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
		return []agent.Item{{TS: at, ID: id, Seq: seq, Type: role, Role: role, Text: clip(text, 4000), Model: model}}

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
		return []agent.Item{{TS: at, ID: id, Seq: seq, Type: "reasoning", Role: "assistant", Text: text, Model: model}}

	case "function_call":
		callID := str(payload["call_id"])
		id := fmt.Sprintf("codex-tool-%s", callID)
		s.calls[callID] = id
		name := firstOf(str(payload["name"]), "shell")
		command := codexCommand(str(payload["arguments"]))
		status := "running"
		if str(payload["status"]) == "completed" {
			status = "done"
		}
		s.byCall[callID] = &codexCall{id: id, kind: "tool_call", name: name}
		return []agent.Item{{
			TS: at, ID: id, Seq: s.seq, Type: "tool_call", Role: "assistant",
			Command: command, Model: model,
			Tool: &agent.ItemTool{Name: name, InputSummary: &command, Status: status},
		}}

	case "function_call_output":
		callID := str(payload["call_id"])
		return s.toolResult(callID, str(payload["output"]), at, model)

	case "custom_tool_call":
		callID := str(payload["call_id"])
		id := fmt.Sprintf("codex-tool-%s", callID)
		s.calls[callID] = id
		name := firstOf(str(payload["name"]), "tool")

		if plan := parsePlanInput(str(payload["input"])); len(plan) > 0 {
			s.byCall[callID] = &codexCall{id: id, kind: "plan", name: name, plan: plan}
			summary := "update_plan"
			return []agent.Item{{
				TS: at, ID: id, Seq: s.seq, Type: "plan", Role: "assistant",
				Plan: plan, Model: model,
				Tool: &agent.ItemTool{Name: name, InputSummary: &summary, Status: "running"},
			}}
		}
		input := clip(str(payload["input"]), 300)
		s.byCall[callID] = &codexCall{id: id, kind: "tool_call", name: name}
		return []agent.Item{{
			TS: at, ID: id, Seq: s.seq, Type: "tool_call", Role: "assistant",
			Model: model,
			Tool:  &agent.ItemTool{Name: name, InputSummary: &input, Status: "running"},
		}}

	case "custom_tool_call_output":
		callID := str(payload["call_id"])
		return s.toolResult(callID, str(payload["output"]), at, model)
	}
	return nil
}

func (s *codexItems) toolResult(callID, output, at, model string) []agent.Item {
	known := s.byCall[callID]
	id := fmt.Sprintf("codex-tool-%s", callID)
	kind := "tool_call"
	name := "tool"
	var files []agent.ItemFile
	var plan []agent.ItemPlan
	if known != nil {
		if known.id != "" {
			id = known.id
		}
		if known.kind != "" {
			kind = known.kind
		}
		if known.name != "" {
			name = known.name
		}
		files = known.files
		plan = known.plan
	} else if cached, ok := s.calls[callID]; ok {
		id = cached
	}

	tail := tailRunes(output, 800)
	item := agent.Item{
		TS: at, ID: id, Seq: s.seq, Type: kind, Role: "tool",
		Model: model, Files: files, Plan: plan,
		Tool: &agent.ItemTool{Name: name, OutputTail: &tail, Status: "done"},
	}
	if code, ok := exitCodeOf(output); ok {
		item.Tool.ExitCode = &code
		item.ExitCode = &code
		if code != 0 {
			item.Tool.Status = "failed"
			item.Error = fmt.Sprintf("exit_code=%d", code)
		}
	}
	return []agent.Item{item}
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

// parsePlanInput mirrors the Node helper: {plan|steps:[{step|title|text,status}]}.
func parsePlanInput(input string) []agent.ItemPlan {
	if input == "" {
		return nil
	}
	var parsed any
	if json.Unmarshal([]byte(input), &parsed) != nil {
		return nil
	}
	var raw []any
	switch t := parsed.(type) {
	case []any:
		raw = t
	case map[string]any:
		if v, ok := t["plan"].([]any); ok {
			raw = v
		} else if v, ok := t["steps"].([]any); ok {
			raw = v
		}
	}
	if len(raw) == 0 {
		return nil
	}
	var out []agent.ItemPlan
	for _, entry := range raw {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		step := firstOf(str(m["step"]), str(m["title"]), str(m["text"]))
		if step == "" {
			continue
		}
		status := firstOf(str(m["status"]), "pending")
		out = append(out, agent.ItemPlan{Step: clip(step, 300), Status: status})
		if len(out) >= 50 {
			break
		}
	}
	return out
}

var patchHeader = regexp.MustCompile(`(?m)^\*\*\* (Add|Update|Delete) File: (.+)$`)

// parsePatchFiles mirrors the Node helper for `*** Begin Patch` style diffs.
func parsePatchFiles(patch string) []agent.ItemFile {
	if patch == "" {
		return nil
	}
	matches := patchHeader.FindAllStringSubmatchIndex(patch, -1)
	var out []agent.ItemFile
	for i, m := range matches {
		start := m[1]
		end := len(patch)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		section := patch[start:end]
		pathStart, pathEnd := m[4], m[5]
		out = append(out, agent.ItemFile{
			Path:    strings.TrimSpace(patch[pathStart:pathEnd]),
			Added:   countPatchLines(section, "+"),
			Removed: countPatchLines(section, "-"),
			Diff:    clip(strings.TrimSpace(section), 1500),
		})
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func countPatchLines(section, sign string) int {
	n := 0
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, sign) && !strings.HasPrefix(line, sign+sign+sign) {
			n++
		}
	}
	return n
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
