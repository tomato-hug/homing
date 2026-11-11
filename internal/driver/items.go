package driver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

// turnMapper turns ACP session/update notifications into the same Item shape
// the file scanners publish. Chunks for one assistant message keep one stable
// id and sequence number, so a client can replace a partial item in place.
type turnMapper struct {
	sessionID string
	agentID   string
	turn      int

	nextSeq int
	now     func() time.Time

	assistant strings.Builder
	reasoning strings.Builder
	user      strings.Builder

	assistantID  string
	assistantSeq int
	reasoningID  string
	reasoningSeq int
	userID       string
	userSeq      int

	toolSeq  map[string]int
	toolItem map[string]agent.Item
}

func newTurnMapper(sessionID, agentID string, turn, nextSeq int) *turnMapper {
	return &turnMapper{
		sessionID: sessionID,
		agentID:   agentID,
		turn:      turn,
		nextSeq:   nextSeq,
		now:       time.Now,
		toolSeq:   map[string]int{},
		toolItem:  map[string]agent.Item{},
	}
}

func (m *turnMapper) SessionID() string { return m.sessionID }

func (m *turnMapper) Seq() int { return m.nextSeq }

func (m *turnMapper) FinalText() string {
	text := strings.TrimSpace(m.assistant.String())
	if text != "" {
		return text
	}
	return strings.TrimSpace(m.reasoning.String())
}

func (m *turnMapper) Map(update Update) []agent.Item {
	body := updateBody(update.Raw)
	switch update.Kind {
	case "user_message_chunk":
		text := firstNonEmptyString(update.Text, contentText(body["content"]))
		if text == "" {
			return nil
		}
		m.user.WriteString(text)
		if m.userID == "" {
			m.userID = fmt.Sprintf("acp-%d-user", m.turn)
			m.userSeq = m.takeSeq()
		}
		return []agent.Item{m.messageItem(m.userID, m.userSeq, "user", m.user.String(), true)}

	case "agent_message_chunk":
		text := firstNonEmptyString(update.Text, contentText(body["content"]))
		if text == "" {
			return nil
		}
		m.assistant.WriteString(text)
		if m.assistantID == "" {
			m.assistantID = fmt.Sprintf("acp-%d-assistant", m.turn)
			m.assistantSeq = m.takeSeq()
		}
		return []agent.Item{m.messageItem(m.assistantID, m.assistantSeq, "assistant", m.assistant.String(), true)}

	case "agent_thought_chunk":
		text := firstNonEmptyString(update.Text, contentText(body["content"]))
		if text == "" {
			return nil
		}
		m.reasoning.WriteString(text)
		if m.reasoningID == "" {
			m.reasoningID = fmt.Sprintf("acp-%d-thought", m.turn)
			m.reasoningSeq = m.takeSeq()
		}
		return []agent.Item{m.messageItem(m.reasoningID, m.reasoningSeq, "assistant", m.reasoning.String(), true, "reasoning")}

	case "tool_call", "tool_call_update":
		item, ok := m.toolItemFor(body)
		if !ok {
			return nil
		}
		return []agent.Item{item}

	case "plan":
		entries := planEntries(body)
		if len(entries) == 0 {
			return nil
		}
		id := fmt.Sprintf("acp-%d-plan", m.turn)
		sequence, ok := m.toolSeq[id]
		if !ok {
			sequence = m.takeSeq()
			m.toolSeq[id] = sequence
		}
		return []agent.Item{{
			TS:   m.timestamp(),
			ID:   id,
			Seq:  sequence,
			Type: "plan",
			Plan: entries,
		}}
	}
	return nil
}

func (m *turnMapper) Finish() []agent.Item {
	var out []agent.Item
	if m.assistantID != "" && m.assistant.Len() > 0 {
		out = append(out, m.messageItem(
			m.assistantID, m.assistantSeq, "assistant", m.assistant.String(), false,
		))
	}
	if m.reasoningID != "" && m.reasoning.Len() > 0 {
		out = append(out, m.messageItem(
			m.reasoningID, m.reasoningSeq, "assistant", m.reasoning.String(), false, "reasoning",
		))
	}
	// A tool that is still running when the prompt response arrives must get a
	// final frame as well. Otherwise clients keep showing a spinner forever.
	// Completed tools were already emitted with Partial=false by Map.
	toolIDs := make([]string, 0, len(m.toolItem))
	for id, item := range m.toolItem {
		if !item.Partial {
			continue
		}
		item.Partial = false
		m.toolItem[id] = item
		toolIDs = append(toolIDs, id)
	}
	sort.Slice(toolIDs, func(i, j int) bool {
		return m.toolItem[toolIDs[i]].Seq < m.toolItem[toolIDs[j]].Seq
	})
	for _, id := range toolIDs {
		out = append(out, m.toolItem[id])
	}
	return out
}

func (m *turnMapper) messageItem(id string, sequence int, role, text string, partial bool, kinds ...string) agent.Item {
	kind := "message"
	if len(kinds) > 0 && kinds[0] != "" {
		kind = kinds[0]
	}
	return agent.Item{
		TS:      m.timestamp(),
		ID:      id,
		Seq:     sequence,
		Type:    kind,
		Role:    role,
		Partial: partial,
		Text:    text,
	}
}

func (m *turnMapper) toolItemFor(body map[string]any) (agent.Item, bool) {
	callID := firstNonEmptyString(
		stringValue(body["toolCallId"]),
		stringValue(body["tool_call_id"]),
		stringValue(body["id"]),
	)
	if callID == "" {
		return agent.Item{}, false
	}
	id := "acp-tool-" + callID
	previous, exists := m.toolItem[id]
	if !exists {
		previous = agent.Item{
			TS:   m.timestamp(),
			ID:   id,
			Seq:  m.takeSeq(),
			Type: "tool_call",
			Role: "tool",
			Tool: &agent.ItemTool{},
		}
		m.toolSeq[id] = previous.Seq
	}
	kind := strings.ToLower(firstNonEmptyString(
		stringValue(body["kind"]),
		stringValue(body["toolKind"]),
	))
	title := firstNonEmptyString(stringValue(body["title"]), stringValue(body["name"]))
	status := toolStatus(firstNonEmptyString(stringValue(body["status"]), stringValue(body["state"])))
	rawInput, hasRawInput := body["rawInput"]
	if !hasRawInput {
		rawInput = body["raw_input"]
	}

	files := diffFiles(body["content"])
	if len(files) == 0 {
		files = diffFiles(body["contentItems"])
	}
	if len(files) == 0 {
		if diff, ok := body["diff"].(map[string]any); ok {
			if file, fileOK := diffFileFromMap(diff); fileOK {
				files = []agent.ItemFile{file}
			}
		}
	}

	if len(files) > 0 && (kind == "edit" || kind == "delete" || kind == "move" || kind == "") {
		previous.Type = "file_change"
		previous.Files = mergeFiles(previous.Files, files)
		previous.Text = title
	} else {
		previous.Type = "tool_call"
		if previous.Tool == nil {
			previous.Tool = &agent.ItemTool{}
		}
		previous.Tool.Name = firstNonEmptyString(title, previous.Tool.Name, "工具调用")
		previous.Tool.Status = status
		previous.Tool.InputSummary = stringPointer(inputSummary(rawInput))
		if output := toolOutput(body["content"]); output != "" {
			previous.Tool.OutputTail = stringPointer(output)
		}
		previous.Command = commandFrom(rawInput)
		previous.Text = firstNonEmptyString(previous.Text, title)
	}
	if status == "failed" {
		previous.Type = "error"
		previous.Error = firstNonEmptyString(previous.Text, "ACP tool call failed")
	}
	previous.Partial = status == "running" || status == "pending"
	if bodyTS := firstNonEmptyString(stringValue(body["ts"]), stringValue(body["timestamp"])); bodyTS != "" {
		previous.TS = bodyTS
	}
	previous.TS = firstNonEmptyString(previous.TS, m.timestamp())
	m.toolItem[id] = previous
	return previous, true
}

func (m *turnMapper) takeSeq() int {
	m.nextSeq++
	return m.nextSeq
}

func (m *turnMapper) timestamp() string {
	if m.now == nil {
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return m.now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func updateBody(raw map[string]any) map[string]any {
	if nested, ok := raw["update"].(map[string]any); ok {
		return nested
	}
	return raw
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func stringPointer(value string) *string {
	text := value
	return &text
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func toolStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending":
		return "pending"
	case "in_progress", "running":
		return "running"
	case "completed", "complete", "ok", "success":
		return "ok"
	case "failed", "error":
		return "error"
	}
	return value
}

func inputSummary(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return truncateOneLine(text, 500)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return truncateOneLine(string(raw), 500)
}

func commandFrom(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		if command, ok := typed["command"].(string); ok {
			return command
		}
		if args, ok := typed["args"].([]any); ok {
			parts := make([]string, 0, len(args)+1)
			if command, ok := typed["command"].(string); ok && command != "" {
				parts = append(parts, command)
			}
			for _, arg := range args {
				if text, ok := arg.(string); ok {
					parts = append(parts, text)
				}
			}
			return strings.Join(parts, " ")
		}
	case string:
		return typed
	}
	return ""
}

func toolOutput(value any) string {
	var parts []string
	collectACPContent(value, func(kind, text string) {
		if kind == "text" && strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	})
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func diffFiles(value any) []agent.ItemFile {
	var files []agent.ItemFile
	rawItems, ok := value.([]any)
	if !ok {
		return files
	}
	for _, raw := range rawItems {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		if strings.ToLower(stringValue(entry["type"])) != "diff" {
			continue
		}
		file, ok := diffFileFromMap(entry)
		if ok {
			files = append(files, file)
		}
	}
	return files
}

func diffFileFromMap(entry map[string]any) (agent.ItemFile, bool) {
	path := firstNonEmptyString(
		stringValue(entry["path"]),
		stringValue(entry["file"]),
		stringValue(entry["filePath"]),
	)
	if path == "" {
		return agent.ItemFile{}, false
	}
	oldText := firstNonEmptyString(stringValue(entry["oldText"]), stringValue(entry["old_text"]))
	newText := firstNonEmptyString(stringValue(entry["newText"]), stringValue(entry["new_text"]))
	diff := stringValue(entry["diff"])
	added, removed := diffCounts(diff)
	if diff == "" && (oldText != "" || newText != "") {
		diff = unifiedDiff(path, oldText, newText)
		added, removed = diffCounts(diff)
	}
	return agent.ItemFile{
		Path:    path,
		Added:   added,
		Removed: removed,
		Diff:    diff,
	}, true
}

func collectACPContent(value any, visit func(kind, text string)) {
	rawItems, ok := value.([]any)
	if !ok {
		if item, ok := value.(map[string]any); ok {
			collectACPContent([]any{item}, visit)
		}
		return
	}
	for _, raw := range rawItems {
		entry, _ := raw.(map[string]any)
		if entry == nil {
			continue
		}
		kind := strings.ToLower(stringValue(entry["type"]))
		text := firstNonEmptyString(stringValue(entry["text"]), contentText(entry["content"]))
		visit(kind, text)
	}
}

func mergeFiles(previous, next []agent.ItemFile) []agent.ItemFile {
	index := map[string]int{}
	out := append([]agent.ItemFile(nil), previous...)
	for i, file := range out {
		index[file.Path] = i
	}
	for _, file := range next {
		if i, ok := index[file.Path]; ok {
			out[i] = file
			continue
		}
		index[file.Path] = len(out)
		out = append(out, file)
	}
	return out
}

func planEntries(body map[string]any) []agent.ItemPlan {
	value := body["entries"]
	if value == nil {
		value = body["plan"]
	}
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]agent.ItemPlan, 0, len(raw))
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		step := firstNonEmptyString(
			stringValue(entry["content"]),
			stringValue(entry["step"]),
			stringValue(entry["title"]),
		)
		if step == "" {
			continue
		}
		out = append(out, agent.ItemPlan{
			Step:   step,
			Status: firstNonEmptyString(stringValue(entry["status"]), "pending"),
		})
	}
	return out
}

func diffCounts(diff string) (int, int) {
	added, removed := 0, 0
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

func unifiedDiff(path, oldText, newText string) string {
	oldLines := strings.Split(strings.TrimSuffix(oldText, "\n"), "\n")
	newLines := strings.Split(strings.TrimSuffix(newText, "\n"), "\n")
	if oldText == "" {
		oldLines = nil
	}
	if newText == "" {
		newLines = nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines))
	for _, line := range oldLines {
		b.WriteString("-")
		b.WriteString(line)
		b.WriteString("\n")
	}
	for _, line := range newLines {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}
