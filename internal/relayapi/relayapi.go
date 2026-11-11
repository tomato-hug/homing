// Package relayapi adds the local relay routes that were still missing from the
// Go port: agent hooks, uploads and capability files, new/stopped sessions,
// away mode, approvals, device management, the APK download and /p proxy.
//
// It mounts itself through api.Register, exactly like the other route groups.
// Hook events are handed to internal/service so they take the same path as
// adapters and watchers: store -> SSE -> notification grading -> encrypted push.
package relayapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/api"
)

const maxJSONBody = 1 << 20

var errBodyTooLarge = errors.New("请求体过大")

func init() {
	api.Register("relay-compat", 60, func(mux *http.ServeMux, d *api.Deps) {
		installACPObserver(d)
		mux.HandleFunc("POST /hook/claude", hookRoute(d, "claude"))
		mux.HandleFunc("POST /hook/workbuddy", hookRoute(d, "workbuddy"))
		mux.HandleFunc("POST /hook/codex", hookRoute(d, "codex"))
		mux.HandleFunc("POST /hook/generic", hookRoute(d, ""))

		mux.HandleFunc("POST /api/upload", guarded(d, uploadHandler))
		mux.HandleFunc("GET /api/away", guarded(d, awayGET))
		mux.HandleFunc("POST /api/away", guarded(d, awayPOST))
		mux.HandleFunc("GET /api/away/settings", guarded(d, awaySettingsGET))
		mux.HandleFunc("PATCH /api/away/settings", guarded(d, awaySettingsPATCH))
		mux.HandleFunc("POST /api/away/periods/{id}/clear", guarded(d, clearAwayPeriod))
		mux.HandleFunc("POST /api/sessions/new", guarded(d, newSession))
		mux.HandleFunc("POST /api/sessions/{id}/stop", guarded(d, stopSession))
		mux.HandleFunc("POST /api/dispatch", guarded(d, dispatchAgent))
		mux.HandleFunc("GET /api/dispatches", guarded(d, listDispatches))
		mux.HandleFunc("POST /api/compose", guarded(d, composeAgent))
		mux.HandleFunc("POST /api/dispatches/{id}/forward", guarded(d, forwardDispatch))
		mux.HandleFunc("POST /api/dispatches/{id}/reply", guarded(d, replyDispatch))
		mux.HandleFunc("POST /api/dispatches/{id}/cancel", guarded(d, cancelDispatch))
		mux.HandleFunc("POST /api/dispatches/{id}/discard", guarded(d, discardDispatchWorktree))
		mux.HandleFunc("POST /api/approvals/{id}", guarded(d, resolveApproval))

		mux.HandleFunc("GET /api/devices", guarded(d, listDevices))
		mux.HandleFunc("POST /api/devices/heartbeat", heartbeatDevice(d))
		mux.HandleFunc("PATCH /api/devices/{id}", guarded(d, renameDevice))
		mux.HandleFunc("DELETE /api/devices/{id}", guarded(d, removeDevice))

		mux.HandleFunc("GET /apk", downloadAPK)
		mux.HandleFunc("HEAD /apk", downloadAPK)
		mux.HandleFunc("GET /f/{token}", func(w http.ResponseWriter, r *http.Request) { capabilityFile(d, w, r) })
		mux.HandleFunc("HEAD /f/{token}", func(w http.ResponseWriter, r *http.Request) { capabilityFile(d, w, r) })
		mux.HandleFunc("GET /f/{token}/{path...}", func(w http.ResponseWriter, r *http.Request) { capabilityFile(d, w, r) })
		mux.HandleFunc("HEAD /f/{token}/{path...}", func(w http.ResponseWriter, r *http.Request) { capabilityFile(d, w, r) })
		mux.Handle("/p/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyPort(d, w, r) }))
	})
}

type handlerFunc func(*api.Deps, http.ResponseWriter, *http.Request)

// guarded applies the normal Bearer/cookie check before a route handler.
func guarded(d *api.Deps, fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.Authorized(r) {
			api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
			return
		}
		if !d.HasScope(r, "read") {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{
				"error": "该设备没有 read 权限", "code": "scope_denied",
			})
			return
		}
		fn(d, w, r)
	}
}

func requireDrive(d *api.Deps, w http.ResponseWriter, r *http.Request) bool {
	return d.RequireScope(w, r, "drive")
}

func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	api.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method Not Allowed"})
	return false
}

func readJSON(r *http.Request) (map[string]any, []byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody+1))
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > maxJSONBody {
		return nil, raw, errBodyTooLarge
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, raw, nil
	}
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, raw, err
	}
	return body, raw, nil
}

func stringField(body map[string]any, key string) string {
	value, _ := body[key].(string)
	return value
}

func boolField(body map[string]any, key string) (bool, bool) {
	value, ok := body[key]
	if !ok {
		return false, false
	}
	flag, ok := value.(bool)
	return flag, ok
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return strings.Trim(host, "[]")
}

func isLoopback(r *http.Request) bool {
	ip := net.ParseIP(remoteHost(r))
	return ip != nil && ip.IsLoopback()
}

func isLocalOrLAN(r *http.Request) bool {
	ip := net.ParseIP(remoteHost(r))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	if ip.To4() != nil {
		// 169.254.0.0/16 (link-local)
		return ip.To4()[0] == 169 && ip.To4()[1] == 254
	}
	// IPv6 link-local (fe80::/10). IsPrivate covers fc00::/7.
	return ip[0] == 0xfe && ip[1]&0xc0 == 0x80
}
