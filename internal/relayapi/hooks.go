package relayapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/api"
	filepaths "github.com/tomato-hug/homing/internal/files"
	"github.com/tomato-hug/homing/internal/service"
	"github.com/tomato-hug/homing/internal/store"
)

var sourceLabels = map[string]string{
	"claude":    "Claude Code",
	"codex":     "Codex",
	"workbuddy": "WorkBuddy",
}

var validKinds = map[string]bool{
	"reply": true, "done": true, "needs_input": true, "permission": true,
	"needs_reply": true, "approval": true, "error": true, "message": true,
}

func hookRoute(d *api.Deps, fixedSource string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r) {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
			return
		}
		if r.Method != http.MethodPost {
			api.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method Not Allowed"})
			return
		}
		body, _, err := readJSON(r)
		if err != nil {
			status := http.StatusBadRequest
			if err == errBodyTooLarge {
				status = http.StatusRequestEntityTooLarge
			}
			api.WriteJSON(w, status, map[string]any{"error": err.Error()})
			return
		}

		var payload map[string]any
		switch fixedSource {
		case "claude", "workbuddy":
			payload, err = handleAgentHook(r.Context(), d, fixedSource, body)
		case "codex":
			payload, err = handleCodexHook(r.Context(), d, body)
		default:
			payload, err = handleGenericHook(r, d, body)
		}
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if payload == nil {
			payload = map[string]any{}
		}
		api.WriteJSON(w, http.StatusOK, payload)
	}
}

func handleAgentHook(ctx context.Context, d *api.Deps, source string, body map[string]any) (map[string]any, error) {
	name := stringField(body, "hook_event_name")
	sessionID := stringField(body, "session_id")
	cwd := stringField(body, "cwd")
	label := sourceLabels[source]
	if label == "" {
		label = source
	}
	if sessionID == "" {
		return nil, errText("session_id 不能为空")
	}
	if transcriptPath := stringField(body, "transcript_path"); transcriptPath != "" {
		scanlib.RecordTranscript(sessionID, source, transcriptPath, cwd)
	}

	switch name {
	case "Stop":
		text := stringField(body, "last_assistant_message")
		if source == "codex" && scanlib.IsInternalTitlePayload(text) {
			return map[string]any{"ok": true, "ignored": "title_generator"}, nil
		}
		event, err := ingestEvent(d, source, "reply", sessionID, cwd,
			label+" 回复", text)
		if err != nil {
			return nil, err
		}
		claimLaunch(d, event)
		trackDispatchLifecycle(d, event)
		return resumeFromPhone(ctx, d, event), nil

	case "PreToolUse":
		toolName := stringField(body, "tool_name")
		if !agent.IsInteractiveQuestionTool(toolName) {
			return map[string]any{"ok": true, "ignored": "non_question_tool"}, nil
		}
		input := body["tool_input"]
		if input == nil {
			input = body["input"]
		}
		questions, summary := agent.ParseInteractiveQuestions(toolName, input)
		qTitle, qBody := agent.PrimaryQuestionPrompt(questions, summary)
		if summary != "" {
			qBody = summary
		}
		qTitle = scanlib.NormalizeHookSourceBranding(source, qTitle)
		qBody = scanlib.NormalizeHookSourceBranding(source, qBody)
		toolUseID := stringField(body, "tool_use_id")
		service.ClaimInteractiveQuestion(source, sessionID, toolUseID, qBody, len(questions) > 0)
		event, err := ingestEventWithQuestions(d, source, "needs_input", sessionID, cwd,
			label+" · "+qTitle, qBody, questions)
		if err != nil {
			return nil, err
		}
		claimLaunch(d, event)
		trackDispatchLifecycle(d, event)
		return map[string]any{"ok": true}, nil

	case "PermissionRequest":
		text := stringField(body, "tool_name")
		if text != "" {
			input := body["tool_input"]
			if input == nil {
				input = body["input"]
			}
			text += "：" + shortJSON(input, 400)
		} else {
			text = firstNonEmpty(stringField(body, "message"), stringField(body, "reason"), "需要授权")
		}
		text = scanlib.NormalizeHookSourceBranding(source, text)
		event, err := ingestEvent(d, source, "permission", sessionID, cwd,
			label+" 请求授权", text)
		if err != nil {
			return nil, err
		}
		claimLaunch(d, event)
		trackDispatchLifecycle(d, event)
		return holdApprovalWithDeps(ctx, d, event), nil

	case "Notification":
		notifType := stringField(body, "notification_type")
		text := stringField(body, "message")
		if scanlib.IsIdlePromptNotification(notifType, text) {
			return map[string]any{"ok": true, "ignored": "idle_prompt"}, nil
		}
		if notifType == "permission_prompt" && HasPendingApprovalForSession(sessionID) {
			return map[string]any{"ok": true, "ignored": "duplicate_permission_prompt"}, nil
		}
		text = scanlib.NormalizeHookSourceBranding(source, text)
		kind := "needs_input"
		title := label + " 等待输入"
		var questions []agent.ItemQuestion
		if notifType == "permission_prompt" || notifType == "elicitation_dialog" || notifType == "" {
			if qID, qs, ok := service.FindPendingQuestionForSession(ctx, source, sessionID); ok {
				questions = qs
				qTitle, qSummary := agent.PrimaryQuestionPrompt(qs, text)
				if full := agent.FormatQuestionSummary(qs, ""); full != "" {
					text = full
				} else if qSummary != "" {
					text = qSummary
				}
				text = scanlib.NormalizeHookSourceBranding(source, text)
				title = scanlib.NormalizeHookSourceBranding(source, label+" · "+qTitle)
				service.ClaimInteractiveQuestion(source, sessionID, qID, text, true)
			}
		}
		if len(questions) == 0 && service.HasRecentStructuredQuestion(source, sessionID, 30*time.Second) {
			return map[string]any{"ok": true, "ignored": "duplicate_structured_question"}, nil
		}
		if len(questions) == 0 && notifType == "permission_prompt" {
			// Hook Notification fires concurrently with or instead of PermissionRequest and
			// ignores PermissionRequest decision stdout; surface as non-blocking needs_input
			// rather than holding or registering dead allow/deny buttons.
			title = label + " 等待确认"
		}
		if len(questions) == 0 {
			service.ClaimInteractiveQuestion(source, sessionID, "", text, false)
		}
		event, err := ingestEventWithQuestions(d, source, kind, sessionID, cwd, title, text, questions)
		if err != nil {
			return nil, err
		}
		claimLaunch(d, event)
		trackDispatchLifecycle(d, event)
	}
	return map[string]any{}, nil
}

func handleCodexHook(ctx context.Context, d *api.Deps, body map[string]any) (map[string]any, error) {
	if name := stringField(body, "hook_event_name"); name != "" {
		return handleAgentHook(ctx, d, "codex", body)
	}
	if kind := stringField(body, "type"); kind != "" && kind != "agent-turn-complete" {
		return map[string]any{}, nil
	}
	sessionID := stringField(body, "thread-id")
	if sessionID == "" {
		return map[string]any{}, nil
	}
	lastMsg := stringField(body, "last-assistant-message")
	if scanlib.IsInternalTitlePayload(lastMsg) {
		return map[string]any{"ok": true, "ignored": "title_generator"}, nil
	}
	event, err := ingestEvent(d, "codex", "reply", sessionID, stringField(body, "cwd"),
		"Codex 回复", lastMsg)
	if err != nil {
		return nil, err
	}
	claimLaunch(d, event)
	trackDispatchLifecycle(d, event)
	return map[string]any{}, nil
}

func handleGenericHook(r *http.Request, d *api.Deps, body map[string]any) (map[string]any, error) {
	source := strings.TrimSpace(firstNonEmpty(
		stringField(body, "source"),
		stringField(body, "agent"),
		r.Header.Get("X-Homing-Agent"),
	))
	if !validHookSource(source) {
		return nil, errText("source 必须是 1–64 位小写字母、数字、点、下划线或连字符")
	}
	kind := strings.TrimSpace(firstNonEmpty(
		stringField(body, "kind"),
		r.Header.Get("X-Homing-Kind"),
		genericKindFromEvent(body),
	))
	if kind == "" {
		kind = "message"
	}
	if !validKinds[kind] {
		return nil, errText("kind 只能是 reply、done、needs_input、needs_reply、permission、approval、error 或 message")
	}
	sessionID := firstNonEmpty(
		stringField(body, "session_id"),
		stringField(body, "sessionId"),
		stringField(body, "thread_id"),
		stringField(body, "thread-id"),
		stringField(body, "conversation_id"),
		stringField(body, "conversationId"),
		stringField(body, "id"),
	)
	if sessionID == "" {
		return nil, errText("session_id 不能为空")
	}
	cwd := firstNonEmpty(
		stringField(body, "cwd"),
		stringField(body, "workdir"),
		stringField(body, "working_directory"),
		stringField(body, "project_dir"),
	)
	title := firstNonEmpty(stringField(body, "title"), stringField(body, "name"))
	text := firstNonEmpty(
		stringField(body, "text"),
		stringField(body, "message"),
		stringField(body, "last_assistant_message"),
		stringField(body, "last-assistant-message"),
		stringField(body, "output"),
	)
	if (kind == "needs_input" || kind == "needs_reply") &&
		scanlib.IsIdlePromptNotification(stringField(body, "notification_type"), text) {
		return map[string]any{"ok": true, "ignored": "idle_prompt"}, nil
	}
	if kind == "needs_input" || kind == "needs_reply" || kind == "permission" || kind == "approval" {
		title = scanlib.NormalizeHookSourceBranding(source, title)
		text = scanlib.NormalizeHookSourceBranding(source, text)
	}
	event, err := ingestEvent(d, source, kind, sessionID, cwd, title, text)
	if err != nil {
		return nil, err
	}
	claimLaunch(d, event)
	trackDispatchLifecycle(d, event)
	return map[string]any{}, nil
}

// genericKindFromEvent recognises the hook_event_name/type fields emitted by
// common coding agents. Unknown events stay "message" so they enter the inbox
// without being misgraded as a completion or permission request.
func genericKindFromEvent(body map[string]any) string {
	event := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		stringField(body, "hook_event_name"),
		stringField(body, "event"),
		stringField(body, "type"),
	)))
	switch event {
	case "stop", "agent-turn-complete", "turn-complete", "turn_complete":
		return "reply"
	case "permissionrequest", "permission_request", "approval", "approval_request":
		return "permission"
	case "notification", "needsinput", "needs_input", "needsreply", "needs_reply":
		return "needs_input"
	case "error", "failed", "failure":
		return "error"
	case "done", "completed", "complete":
		return "done"
	default:
		return ""
	}
}

func ingestEvent(d *api.Deps, source, kind, sessionID, cwd, title, text string) (store.Event, error) {
	return ingestEventWithAll(d, source, kind, sessionID, cwd, title, text, nil, nil)
}

func ingestEventWithQuestions(
	d *api.Deps,
	source, kind, sessionID, cwd, title, text string,
	questions []agent.ItemQuestion,
) (store.Event, error) {
	return ingestEventWithAll(d, source, kind, sessionID, cwd, title, text, nil, questions)
}

func ingestEventWithMeta(
	d *api.Deps,
	source, kind, sessionID, cwd, title, text string,
	meta map[string]any,
) (store.Event, error) {
	return ingestEventWithAll(d, source, kind, sessionID, cwd, title, text, meta, nil)
}

func ingestEventWithAll(
	d *api.Deps,
	source, kind, sessionID, cwd, title, text string,
	meta map[string]any,
	questions []agent.ItemQuestion,
) (store.Event, error) {
	svc := service.Start(d)
	if svc == nil {
		return store.Event{}, errText("事件管道不可用")
	}
	kind = agent.CanonicalEventKind(kind)
	text = truncateRunes(text, 4000)
	title = truncateRunes(title, 200)
	if title == "" {
		title = sourceLabels[source]
		if title == "" {
			title = source
		}
	}
	attachments := filepaths.ManagerFor(d.DataDir).Build(text, cwd)
	asAny := make([]any, 0, len(attachments))
	for _, attachment := range attachments {
		asAny = append(asAny, attachment)
	}
	var questionsAny any
	if len(questions) > 0 {
		questionsAny = questions
	}
	return svc.IngestStoreEvent(store.Event{
		Source:      source,
		Kind:        kind,
		SessionID:   sessionID,
		Cwd:         cwd,
		Title:       title,
		Text:        text,
		Attachments: &asAny,
		SessionMeta: meta,
		Questions:   questionsAny,
	})
}

func validHookSource(source string) bool {
	if len(source) == 0 || len(source) > 64 {
		return false
	}
	for i, r := range source {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func shortJSON(value any, limit int) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return truncateRunes(text, limit)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return truncateRunes(string(raw), limit)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type errText string

func (e errText) Error() string { return string(e) }
