package relayapi

import (
	"math"
	"net/http"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/platform"
)

func syncKeepAwakeForDeps(d *api.Deps) platform.KeepAwakeState {
	away := false
	if s, err := d.Store(); err == nil {
		away = s.Away()
	}
	return platform.KeepAwakeFor(d.DataDir).Sync(away)
}

func keepAwakeGET(d *api.Deps, w http.ResponseWriter, _ *http.Request) {
	api.WriteJSON(w, http.StatusOK, syncKeepAwakeForDeps(d))
}

func keepAwakeMutate(d *api.Deps, w http.ResponseWriter, r *http.Request) {
	body, _, err := readJSON(r)
	if err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	var patch platform.KeepAwakePatch
	if enabled, present := body["enabled"]; present {
		flagVal, ok := enabled.(bool)
		if !ok {
			api.WriteError(w, http.StatusBadRequest, "invalid_enabled", "enabled 必须是布尔值")
			return
		}
		patch.Enabled = &flagVal
	}
	if autoVal, present := body["auto_on_away"]; present {
		flagVal, ok := autoVal.(bool)
		if !ok {
			api.WriteError(w, http.StatusBadRequest, "invalid_auto_on_away", "auto_on_away 必须是布尔值")
			return
		}
		patch.AutoOnAway = &flagVal
	}
	if rawMax, present := body["max_minutes"]; present {
		value, numeric := rawMax.(float64)
		if !numeric || value < 15 || value > 1440 || value != math.Trunc(value) {
			api.WriteError(w, http.StatusBadRequest, "invalid_max_minutes", "max_minutes 必须是 15 到 1440 之间的整数")
			return
		}
		minutes := int(value)
		patch.MaxMinutes = &minutes
	}
	away := false
	if s, err := d.Store(); err == nil {
		away = s.Away()
	}
	state := platform.KeepAwakeFor(d.DataDir).Update(patch, away)
	api.WriteJSON(w, http.StatusOK, state)
}
