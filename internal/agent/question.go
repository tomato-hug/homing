package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ItemQuestion represents a single interactive decision prompt emitted by an agent
// tool call such as AskUserQuestion, request_user_input, or ExitPlanMode.
type ItemQuestion struct {
	Header      string               `json:"header,omitempty"`
	Question    string               `json:"question"`
	MultiSelect bool                 `json:"multi_select,omitempty"`
	Options     []ItemQuestionOption `json:"options,omitempty"`
}

// ItemQuestionOption represents a selectable answer option for an ItemQuestion.
type ItemQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// IsInteractiveQuestionTool reports whether a tool call pauses for user input/decision.
func IsInteractiveQuestionTool(name string) bool {
	switch normalizeQuestionToolName(name) {
	case "askuserquestion", "ask_user_question", "request_user_input", "exitplanmode", "exit_plan_mode":
		return true
	default:
		return false
	}
}

func normalizeQuestionToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// ParseInteractiveQuestions parses structured prompts and selectable options from
// AskUserQuestion, request_user_input, or ExitPlanMode tool arguments.
// It returns the parsed questions and a concise plain-text summary suitable for
// timeline fallback rendering or push notifications.
func ParseInteractiveQuestions(toolName string, input any) ([]ItemQuestion, string) {
	m := decodeInputMap(input)
	norm := normalizeQuestionToolName(toolName)

	var questions []ItemQuestion
	if rawList, ok := m["questions"].([]any); ok {
		for _, entry := range rawList {
			em, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			q := parseSingleQuestion(em)
			if q.Question != "" || len(q.Options) > 0 || q.Header != "" {
				questions = append(questions, q)
			}
		}
	}

	if len(questions) == 0 && len(m) > 0 {
		if single := parseSingleQuestion(m); single.Question != "" || len(single.Options) > 0 {
			questions = append(questions, single)
		}
	}

	if len(questions) == 0 && (norm == "exitplanmode" || norm == "exit_plan_mode") {
		prompt := strings.TrimSpace(firstQuestionStr(m, "prompt", "question", "summary"))
		if prompt == "" {
			if plan := strings.TrimSpace(firstQuestionStr(m, "plan")); plan != "" {
				prompt = truncateQuestionText(plan, 280)
			}
		}
		if prompt == "" {
			prompt = "请确认是否批准当前执行计划并开始实施？"
		}
		questions = []ItemQuestion{
			{
				Header:   "计划确认",
				Question: prompt,
				Options: []ItemQuestionOption{
					{Label: "批准并开始执行", Description: "确认计划并允许代理继续执行"},
					{Label: "提出修改意见", Description: "在下方输入需要调整的内容"},
				},
			},
		}
	}

	return questions, FormatQuestionSummary(questions, strings.TrimSpace(toolName))
}

func parseSingleQuestion(m map[string]any) ItemQuestion {
	qText := strings.TrimSpace(firstQuestionStr(m, "question", "prompt", "title", "message"))
	header := strings.TrimSpace(firstQuestionStr(m, "header", "id", "label"))
	multi, _ := m["multiSelect"].(bool)
	if !multi {
		multi, _ = m["multi_select"].(bool)
	}

	var opts []ItemQuestionOption
	if rawOpts, ok := m["options"].([]any); ok {
		for _, entry := range rawOpts {
			switch v := entry.(type) {
			case map[string]any:
				label := strings.TrimSpace(firstQuestionStr(v, "label", "title", "name", "value"))
				desc := strings.TrimSpace(firstQuestionStr(v, "description", "subtitle", "detail"))
				if label != "" || desc != "" {
					if label == "" {
						label = desc
						desc = ""
					}
					opts = append(opts, ItemQuestionOption{Label: label, Description: desc})
				}
			case string:
				if label := strings.TrimSpace(v); label != "" {
					opts = append(opts, ItemQuestionOption{Label: label})
				}
			}
		}
	}

	return ItemQuestion{
		Header:      header,
		Question:    qText,
		MultiSelect: multi,
		Options:     opts,
	}
}

// FormatQuestionSummary builds a readable summary string for questions and options.
func FormatQuestionSummary(questions []ItemQuestion, fallback string) string {
	if len(questions) == 0 {
		return strings.TrimSpace(fallback)
	}
	var b strings.Builder
	for i, q := range questions {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if q.Header != "" {
			fmt.Fprintf(&b, "[%s] ", q.Header)
		}
		b.WriteString(q.Question)
		for j, opt := range q.Options {
			fmt.Fprintf(&b, "\n%d. %s", j+1, opt.Label)
			if opt.Description != "" {
				fmt.Fprintf(&b, " — %s", opt.Description)
			}
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return strings.TrimSpace(fallback)
	}
	return out
}

// PrimaryQuestionPrompt returns a short notification title and body for a set of questions.
func PrimaryQuestionPrompt(questions []ItemQuestion, fallback string) (string, string) {
	if len(questions) == 0 {
		msg := strings.TrimSpace(fallback)
		if msg == "" {
			msg = "Agent has a question awaiting your decision"
		}
		return "待确认决策", msg
	}
	first := questions[0]
	title := "待确认决策"
	if first.Header != "" {
		title = "待确认：" + first.Header
	}
	var b strings.Builder
	b.WriteString(first.Question)
	if len(first.Options) > 0 {
		labels := make([]string, 0, len(first.Options))
		for i, opt := range first.Options {
			if i >= 4 {
				labels = append(labels, fmt.Sprintf("+%d 项", len(first.Options)-4))
				break
			}
			labels = append(labels, opt.Label)
		}
		b.WriteString("\n选项：")
		b.WriteString(strings.Join(labels, " / "))
	}
	if len(questions) > 1 {
		fmt.Fprintf(&b, "\n（共 %d 个待确认问题）", len(questions))
	}
	body := strings.TrimSpace(b.String())
	if body == "" {
		body = strings.TrimSpace(fallback)
	}
	return title, body
}

func decodeInputMap(input any) map[string]any {
	switch v := input.(type) {
	case map[string]any:
		return v
	case string:
		var m map[string]any
		if err := json.Unmarshal([]byte(v), &m); err == nil && m != nil {
			return m
		}
	case []byte:
		var m map[string]any
		if err := json.Unmarshal(v, &m); err == nil && m != nil {
			return m
		}
	case json.RawMessage:
		var m map[string]any
		if err := json.Unmarshal(v, &m); err == nil && m != nil {
			return m
		}
	}
	return nil
}

func firstQuestionStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func truncateQuestionText(s string, maxRunes int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= maxRunes {
		return string(r)
	}
	return string(r[:maxRunes]) + "..."
}
