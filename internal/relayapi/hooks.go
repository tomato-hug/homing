package relayapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

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
	"reply": true, "needs_input": true, "permission": true, "error": true,
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
			payload, err = handleGenericHook(d, body)
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
		event, err := ingestEvent(d, source, "reply", sessionID, cwd,
			label+" 回复", text)
		if err != nil {
			return nil, err
		}
		claimLaunch(d, event)
		return resumeFromPhone(ctx, d, event), nil

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
		event, err := ingestEvent(d, source, "permission", sessionID, cwd,
			label+" 请求授权", text)
		if err != nil {
			return nil, err
		}
		claimLaunch(d, event)
		return holdApproval(ctx, event), nil

	case "Notification":
		text := stringField(body, "message")
		kind := "needs_input"
		title := label + " 等待输入"
		if stringField(body, "notification_type") == "permission_prompt" {
			kind = "permission"
			title = label + " 请求权限"
		}
		event, err := ingestEvent(d, source, kind, sessionID, cwd, title, text)
		if err != nil {
			return nil, err
		}
		claimLaunch(d, event)
		if kind == "permission" {
			return holdApproval(ctx, event), nil
		}
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
	event, err := ingestEvent(d, "codex", "reply", sessionID, stringField(body, "cwd"),
		"Codex 回复", stringField(body, "last-assistant-message"))
	if err != nil {
		return nil, err
	}
	claimLaunch(d, event)
	return map[string]any{}, nil
}

func handleGenericHook(d *api.Deps, body map[string]any) (map[string]any, error) {
	source := strings.TrimSpace(stringField(body, "source"))
	if _, ok := sourceLabels[source]; !ok {
		return nil, errText("source 只能是 claude、codex 或 workbuddy")
	}
	kind := strings.TrimSpace(stringField(body, "kind"))
	if !validKinds[kind] {
		return nil, errText("kind 只能是 reply、needs_input、permission 或 error")
	}
	sessionID := stringField(body, "session_id")
	if sessionID == "" {
		return nil, errText("session_id 不能为空")
	}
	event, err := ingestEvent(d, source, kind, sessionID, stringField(body, "cwd"),
		stringField(body, "title"), stringField(body, "text"))
	if err != nil {
		return nil, err
	}
	claimLaunch(d, event)
	return map[string]any{}, nil
}

func ingestEvent(d *api.Deps, source, kind, sessionID, cwd, title, text string) (store.Event, error) {
	svc := service.Start(d)
	if svc == nil {
		return store.Event{}, errText("事件管道不可用")
	}
	text = truncateRunes(text, 4000)
	title = truncateRunes(title, 200)
	if title == "" {
		title = sourceLabels[source]
	}
	attachments := filepaths.ManagerFor(d.DataDir).Build(text, cwd)
	asAny := make([]any, 0, len(attachments))
	for _, attachment := range attachments {
		asAny = append(asAny, attachment)
	}
	return svc.IngestStoreEvent(store.Event{
		Source:      source,
		Kind:        kind,
		SessionID:   sessionID,
		Cwd:         cwd,
		Title:       title,
		Text:        text,
		Attachments: &asAny,
	})
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
