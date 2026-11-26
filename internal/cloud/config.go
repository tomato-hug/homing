package cloud

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/protocol"
)

// ConfigureServer persists a cloud/self-hosted relay address and replaces the
// background connector. An empty address disables the connector but leaves the
// paired peer list untouched. The connector is only swapped after the new
// address validates and peers.json saves successfully.
func ConfigureServer(
	dataDir string,
	rawServer string,
	hub *Hub,
	handler http.Handler,
	log *slog.Logger,
) (api.CloudStatus, error) {
	return ConfigureServerWithOptions(dataDir, rawServer, nil, hub, handler, log)
}

func ConfigureServerWithOptions(
	dataDir string,
	rawServer string,
	useSystemProxy *bool,
	hub *Hub,
	handler http.Handler,
	log *slog.Logger,
) (api.CloudStatus, error) {
	return ConfigureServerWithAuth(dataDir, rawServer, nil, useSystemProxy, hub, handler, log)
}

func ConfigureServerWithAuth(
	dataDir string,
	rawServer string,
	relayKey *string,
	useSystemProxy *bool,
	hub *Hub,
	handler http.Handler,
	log *slog.Logger,
) (api.CloudStatus, error) {
	if hub == nil {
		return api.CloudStatus{}, fmt.Errorf("cloud: hub is unavailable")
	}
	if log == nil {
		log = slog.Default()
	}
	if relayKey != nil {
		if err := hub.Peers().SetRelayKey(strings.TrimSpace(*relayKey)); err != nil {
			return StatusFor(dataDir, hub), err
		}
	}
	if useSystemProxy != nil {
		if err := hub.Peers().SetUseSystemProxy(*useSystemProxy); err != nil {
			return StatusFor(dataDir, hub), err
		}
	}
	server := strings.TrimSpace(rawServer)
	if server == "" {
		if err := hub.Peers().SetServer(""); err != nil {
			return StatusFor(dataDir, hub), err
		}
		hub.SetServerURL("")
		removeConnector(dataDir)
		return StatusFor(dataDir, hub), nil
	}

	normalized, err := protocol.NormalizeWebSocketURL(server)
	if err != nil {
		return StatusFor(dataDir, hub), err
	}
	next, err := NewConnector(ConnectorConfig{
		ServerURL:      normalized,
		RelayKey:       hub.Peers().RelayKey(),
		Identity:       hub.Identity(),
		Peers:          hub.Peers(),
		Handler:        handler,
		Logger:         log,
		UseSystemProxy: hub.Peers().UseSystemProxy(),
	})
	if err != nil {
		return StatusFor(dataDir, hub), err
	}
	if err := hub.Peers().SetServer(normalized); err != nil {
		next.Close()
		return StatusFor(dataDir, hub), err
	}
	hub.SetServerURL(normalized)
	installConnector(dataDir, next)
	return StatusFor(dataDir, hub), nil
}

func mountConfig(mux *http.ServeMux, dataDir string, hub *Hub, deps *api.Deps) {
	mux.HandleFunc("PUT /api/cloud/config", loopbackOnly(func(w http.ResponseWriter, r *http.Request) {
		if !deps.Authorized(r) {
			api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "Unauthorized"})
			return
		}
		if !deps.RequireScope(w, r, "drive") {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var body struct {
			Server         *string `json:"server"`
			RelayKey       *string `json:"relay_key"`
			UseSystemProxy *bool   `json:"use_system_proxy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
			return
		}
		targetServer := hub.Peers().Server()
		if body.Server != nil {
			targetServer = *body.Server
		}
		status, err := ConfigureServerWithAuth(dataDir, targetServer, body.RelayKey, body.UseSystemProxy, hub, mux, deps.Log)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{
				"ok":    false,
				"error": err.Error(),
				"cloud": status,
			})
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "cloud": status})
	}))
}
