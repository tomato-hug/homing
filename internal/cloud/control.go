package cloud

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/api"
)

// pairControl is the loopback-only HTTP surface `homing pair` talks to. It
// works whether the host is pairing over the LAN hub or through a cloud
// connector, without adding commands to cmd/homing.
type pairControl struct {
	dataDir string
	hub     *Hub

	mu   sync.Mutex
	mode string // "lan" or "cloud"
}

type pairStartRequest struct {
	Server string `json:"server,omitempty"`
}

type pairStartResponse struct {
	Mode string `json:"mode"`
	PairInvite
}

type pairRequestView struct {
	ClientID    string    `json:"client_id"`
	Name        string    `json:"name,omitempty"`
	Platform    string    `json:"platform,omitempty"`
	Fingerprint string    `json:"fingerprint"`
	SAS         []string  `json:"sas"`
	ReceivedAt  time.Time `json:"received_at"`
}

type pairRequestList struct {
	Mode     string            `json:"mode"`
	Requests []pairRequestView `json:"requests"`
}

type pairDecision struct {
	ClientID string `json:"client_id"`
	Accept   bool   `json:"accept"`
}

func mountPairControl(mux *http.ServeMux, dataDir string, hub *Hub) {
	control := &pairControl{dataDir: dataDir, hub: hub}
	mux.HandleFunc("POST /api/cloud/pair/start", loopbackOnly(control.start))
	mux.HandleFunc("GET /api/cloud/pair/requests", loopbackOnly(control.requests))
	mux.HandleFunc("POST /api/cloud/pair/decision", loopbackOnly(control.decision))
}

func (c *pairControl) start(w http.ResponseWriter, r *http.Request) {
	var body pairStartRequest
	_ = json.NewDecoder(r.Body).Decode(&body)

	connector, hasConnector := ConnectorFor(c.dataDir)
	if hasConnector {
		if !connector.Connected() {
			api.WriteJSON(w, http.StatusConflict, map[string]any{
				"ok":    false,
				"error": "云中转未连接，请稍后重试或检查 peers.json 中的服务器地址",
			})
			return
		}
		invite, err := connector.StartPairing(r.Context())
		if err != nil {
			api.WriteJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		c.mu.Lock()
		c.mode = "cloud"
		c.mu.Unlock()
		api.WriteJSON(w, http.StatusOK, pairStartResponse{Mode: "cloud", PairInvite: invite})
		return
	}

	if body.Server != "" {
		if strings.HasPrefix(strings.ToLower(body.Server), "wss://") {
			api.WriteJSON(w, http.StatusConflict, map[string]any{
				"ok":    false,
				"error": "云中转尚未配置：先在 peers.json 中设置服务器并重启 homing serve，或留空 --server 使用局域网直连",
			})
			return
		}
		c.hub.SetServerURL(body.Server)
	} else if c.hub.ServerURL() == "" {
		// The console starts LAN pairing without knowing the LAN address;
		// derive the same ws:// URL `homing pair` would print.
		if derived, err := lanWebSocketURL("http://"+r.Host, ""); err == nil {
			c.hub.SetServerURL(derived)
		}
	}
	invite, err := c.hub.BeginPairing()
	if err != nil {
		api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	c.mu.Lock()
	c.mode = "lan"
	c.mu.Unlock()
	api.WriteJSON(w, http.StatusOK, pairStartResponse{Mode: "lan", PairInvite: invite})
}

func (c *pairControl) requests(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	mode := c.mode
	c.mu.Unlock()
	if mode == "" {
		mode = "lan"
	}
	requests := []PairingRequest{}
	if mode == "cloud" {
		if connector, ok := ConnectorFor(c.dataDir); ok {
			requests = connector.Pending()
		}
	} else {
		requests = c.hub.Pending()
	}
	view := make([]pairRequestView, 0, len(requests))
	for _, request := range requests {
		view = append(view, pairRequestView{
			ClientID:    request.ClientID,
			Name:        request.Name,
			Platform:    request.Platform,
			Fingerprint: request.Fingerprint,
			SAS:         request.SAS[:],
			ReceivedAt:  request.ReceivedAt,
		})
	}
	api.WriteJSON(w, http.StatusOK, pairRequestList{Mode: mode, Requests: view})
}

func (c *pairControl) decision(w http.ResponseWriter, r *http.Request) {
	var body pairDecision
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
		return
	}
	if body.ClientID == "" {
		api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "client_id is required"})
		return
	}
	c.mu.Lock()
	mode := c.mode
	c.mu.Unlock()
	if mode == "" {
		mode = "lan"
	}
	var err error
	if mode == "cloud" {
		connector, ok := ConnectorFor(c.dataDir)
		if !ok {
			err = ErrNotConnected
		} else if body.Accept {
			err = connector.Accept(r.Context(), body.ClientID)
		} else {
			err = connector.Reject(r.Context(), body.ClientID)
		}
	} else if body.Accept {
		err = c.hub.Approve(body.ClientID)
	} else {
		err = c.hub.Reject(body.ClientID)
	}
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, ErrNotConnected) {
			status = http.StatusServiceUnavailable
		}
		api.WriteJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "accepted": body.Accept})
}

// loopbackOnly keeps the pairing control surface unreachable from phones even
// when `homing serve` listens on 0.0.0.0 for LAN-direct connections.
func loopbackOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "loopback_only"})
			return
		}
		next(w, r)
	}
}
