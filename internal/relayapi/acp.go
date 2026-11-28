package relayapi

import (
	"context"
	"strings"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/driver"
)

type acpObserver struct {
	d *api.Deps
}

func installACPObserver(d *api.Deps) {
	driver.Default().SetObserver(acpObserver{d: d})
}

func (o acpObserver) Started(sessionID, agentID, cwd string) {
	label := acpLabel(agentID)
	event, err := ingestEventWithMeta(
		o.d, agentID, "message", sessionID, cwd,
		label+" 会话已启动", "", map[string]any{"acp": map[string]any{"started": true}},
	)
	if err == nil {
		trackDispatchLifecycle(o.d, event)
	}
}

func (o acpObserver) Item(sessionID, agentID string, item agent.Item) {
	api.PublishNamed("item", sessionID, map[string]any{
		"session_id": sessionID,
		"source":     agentID,
		"item":       item,
	})
}

func (o acpObserver) Turn(sessionID, agentID, state string) {
	api.PublishNamed("turn", sessionID, map[string]any{
		"session_id": sessionID,
		"source":     agentID,
		"state":      state,
		"ts":         nowISO(),
	})
}

func (o acpObserver) Permission(
	ctx context.Context,
	sessionID, agentID string,
	request driver.PermissionRequest,
) driver.PermissionDecision {
	toolCall := request.ToolCall
	title := firstNonEmpty(
		stringField(toolCall, "title"),
		stringField(toolCall, "name"),
		"工具调用",
	)
	detail := firstNonEmpty(
		shortJSON(toolCall["rawInput"], 500),
		shortJSON(toolCall["raw_input"], 500),
		title,
	)
	options := make([]any, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, map[string]any{
			"id":        option.OptionID,
			"option_id": option.OptionID,
			"label":     firstNonEmpty(option.Name, option.Kind, option.OptionID),
			"name":      option.Name,
			"kind":      option.Kind,
		})
	}
	meta := map[string]any{
		"acp": map[string]any{
			"session_id": sessionID,
			"tool_call":  toolCall,
			"options":    options,
		},
	}
	event, err := ingestEventWithMeta(
		o.d, agentID, "permission", sessionID, driver.Default().SessionCwd(sessionID),
		acpLabel(agentID)+" 请求授权", title+"："+detail, meta,
	)
	if err != nil {
		return driver.PermissionDecision{Cancel: true}
	}
	trackDispatchLifecycle(o.d, event)
	answer := holdApprovalWithDeps(ctx, o.d, event)
	if len(answer) == 0 {
		return driver.PermissionDecision{Cancel: true}
	}
	decision := strings.ToLower(stringField(answer, "decision"))
	optionID := strings.TrimSpace(stringField(answer, "option_id"))
	if optionID != "" {
		return driver.PermissionDecision{
			Allow: decision != "deny", OptionID: optionID,
		}
	}
	if decision == "allow" {
		return driver.PermissionDecision{Allow: true}
	}
	return driver.PermissionDecision{Allow: false}
}

func (o acpObserver) Finished(
	ctx context.Context,
	sessionID, agentID string,
	result driver.TurnResult,
) {
	kind := "done"
	title := acpLabel(agentID) + " 已完成"
	text := strings.TrimSpace(result.Text)
	if result.Cancelled {
		kind = "stop"
		title = acpLabel(agentID) + " 已停止"
	} else if strings.TrimSpace(result.Error) != "" {
		kind = "error"
		title = acpLabel(agentID) + " 运行出错"
		text = firstNonEmpty(text, result.Error)
	}
	meta := map[string]any{
		"acp": map[string]any{
			"stop_reason": result.StopReason,
		},
	}
	event, err := ingestEventWithMeta(
		o.d, agentID, kind, sessionID, driver.Default().SessionCwd(sessionID),
		title, text, meta,
	)
	if err != nil {
		o.d.Log.Error("store ACP completion", "session", sessionID, "err", err)
		return
	}
	trackDispatchLifecycle(o.d, event)
	_ = ctx
}

func acpLabel(id string) string {
	if label := sourceLabels[id]; label != "" {
		return label
	}
	if spec, ok := driver.SpecFor(id); ok && spec.Label != "" {
		return spec.Label
	}
	return id
}
