package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agentconfig"
	"github.com/tomato-hug/homing/internal/notify"
)

func init() {
	Register("agent-settings", 11, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/agents/settings", d.guard(d.handleAgentSettings))
		mux.HandleFunc("GET /api/agents/{id}/settings", d.guard(d.handleAgentSetting))
		mux.HandleFunc("PATCH /api/agents/{id}/settings", d.guard(d.handleAgentSetting))
		mux.HandleFunc("POST /api/agents/{id}/settings", d.guard(d.handleAgentSetting))
		mux.HandleFunc("PUT /api/agents/order", d.guard(d.handleAgentOrder))
		mux.HandleFunc("POST /api/agents/{id}/hooks", d.handleAgentHooks)
		mux.HandleFunc("POST /api/agents/{id}/test-push", d.guard(d.handleAgentTestPush))
		mux.HandleFunc("POST /api/agents/{id}/disconnect", d.guard(d.handleAgentDisconnect))
	})
}

func (d *Deps) handleAgentSettings(w http.ResponseWriter, r *http.Request) {
	ids := agentSettingsIDs(ctxOf(r))
	store := agentconfig.NewStore(d.DataDir)
	WriteJSON(w, http.StatusOK, map[string]any{
		"settings": store.Snapshot(ids),
		"order":    store.Ordered(ids),
	})
}

func (d *Deps) handleAgentSetting(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !agentconfig.ValidID(id) {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
		return
	}
	store := agentconfig.NewStore(d.DataDir)
	if r.Method == http.MethodGet {
		WriteJSON(w, http.StatusOK, store.Read(id))
		return
	}
	next, err := store.Patch(id, readBody(r))
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存接入设置失败：" + err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, next)
}

func (d *Deps) handleAgentOrder(w http.ResponseWriter, r *http.Request) {
	body := readBody(r)
	raw, _ := body["order"].([]any)
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		if id, ok := item.(string); ok {
			ids = append(ids, id)
		}
	}
	store := agentconfig.NewStore(d.DataDir)
	clean, err := store.SetOrder(ids)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存排序失败：" + err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "order": clean})
}

func (d *Deps) handleAgentHooks(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) {
		WriteJSON(w, http.StatusForbidden, map[string]any{"error": "装钩子只允许在本机操作"})
		return
	}
	if !d.Authorized(r) {
		WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
		return
	}
	id := r.PathValue("id")
	if !agentconfig.ValidID(id) {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
		return
	}
	result := agentconfig.NewStore(d.DataDir).InstallHooks(id, requestPort(r))
	status := http.StatusOK
	switch {
	case !result.OK && result.Unsupported:
		status = http.StatusBadRequest
	case !result.OK:
		status = http.StatusAccepted
	}
	WriteJSON(w, status, result)
}

func (d *Deps) handleAgentTestPush(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !agentconfig.ValidID(id) {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
		return
	}
	settings := agentconfig.NewStore(d.DataDir).Snapshot([]string{id})[id]
	if !settings.PushToPhone {
		WriteJSON(w, http.StatusConflict, map[string]any{
			"ok":    false,
			"error": "这个 agent 的「推送到手机」是关的，先打开再试",
		})
		return
	}

	online := 0
	if s, ok := d.store(); ok {
		now := time.Now().UnixMilli()
		for _, device := range s.Devices() {
			if device.LastSeenMs > 0 && now-device.LastSeenMs <= 120_000 {
				online++
			}
		}
	}
	payload := notify.Payload{
		Type:    "notify",
		Level:   "P1",
		Kind:    "needs_input",
		Source:  id,
		Title:   "Homing 测试通知",
		Summary: "接入与手机推送通道正常；这不是 Agent 的真实任务。",
		At:      nowISO(),
		Temp:    true,
		Count:   1,
	}
	delivered, err := deliverTestPush(r.Context(), payload)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		WriteJSON(w, http.StatusGatewayTimeout, map[string]any{
			"ok": false, "agent": id, "devices": online,
			"error": "测试推送超时，手机可能不在线",
		})
		return
	}
	if err != nil || delivered == 0 {
		message := "没有已配对的手机可接收测试推送"
		if err != nil {
			message = "测试推送发送失败：" + err.Error()
		}
		WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok": false, "agent": id, "devices": online,
			"delivered": delivered, "error": message,
		})
		return
	}
	d.Audit(r, "agent_test_push", map[string]any{
		"target": id, "delivered": delivered,
	})
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"placeholder": false,
		"agent":       id,
		"devices":     online,
		"delivered":   delivered,
		"message":     "测试通知已送出",
	})
}

func agentSettingsIDs(ctx context.Context) []string {
	ids := make([]string, 0, len(displayOrder))
	seen := make(map[string]bool, len(displayOrder))
	for _, id := range displayOrder {
		seen[id] = true
		ids = append(ids, id)
	}
	for _, item := range sortedAgents(ctx) {
		if !seen[item.ID()] {
			seen[item.ID()] = true
			ids = append(ids, item.ID())
		}
	}
	return ids
}

func (d *Deps) handleAgentDisconnect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !agentconfig.ValidID(id) {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "agent id 不合法"})
		return
	}
	store := agentconfig.NewStore(d.DataDir)
	next, err := store.Patch(id, map[string]any{"connected": false, "push_to_phone": false})
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "断开接入失败：" + err.Error()})
		return
	}
	file := store.ConfigFile(id)
	if strings.TrimSpace(file) == "" {
		file = ""
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"settings": next,
		"file":     file,
		"note":     "只切断了 Homing 这边的接入，没有改 agent 的配置文件；要清掉钩子请按上面的路径手工删除（安装时留有 .bak-homing-* 备份）",
	})
}
