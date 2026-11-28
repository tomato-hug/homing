package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/tomato-hug/homing/internal/store"
)

type trustedLocalRPCKey struct{}

// WithTrustedLocalRPC marks a request that has already passed the encrypted
// transport's pairing and envelope checks. Cloud dispatch uses this for the
// loopback handler it invokes in-process; ordinary HTTP requests can never
// acquire the marker from a header or query parameter.
func WithTrustedLocalRPC(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), trustedLocalRPCKey{}, true))
}

// IsTrustedLocalRPC reports whether r arrived through Homing's encrypted cloud
// RPC / file-transfer bridge after passing device pairing and envelope auth.
func IsTrustedLocalRPC(r *http.Request) bool {
	if r == nil {
		return false
	}
	trusted, _ := r.Context().Value(trustedLocalRPCKey{}).(bool)
	return trusted
}

// store returns the process-wide store for this Deps, opening it on first use.
// Opening is lazy so tests (and packages that only need one route group) can
// hand NewMux a Deps with nothing but a DataDir.
func (d *Deps) store() (*store.Store, bool) {
	s, err := d.Store()
	return s, err == nil
}

// authorized mirrors the Node relay's hasValidToken: a Bearer token that is
// either the config token or a paired device token, or on loopback the
// homing_local cookie the console page carries.
func (d *Deps) authorized(r *http.Request) bool {
	if trusted, _ := r.Context().Value(trustedLocalRPCKey{}).(bool); trusted {
		return true
	}
	s, ok := d.store()
	if !ok {
		return false
	}
	token := s.Token()
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		supplied := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if equalTokens(supplied, token) {
			return true
		}
		if strings.HasPrefix(supplied, "hm_") && deviceTokenValid(s, supplied) {
			return true
		}
	}
	if isLoopback(r.RemoteAddr) {
		if cookie, err := r.Cookie("homing_local"); err == nil && equalTokens(cookie.Value, token) {
			return true
		}
	}
	return false
}

var AllDeviceScopes = []string{"read", "reply", "drive"}

// RequestScopes returns the scopes that apply to this request. The local
// config token, console cookie and encrypted in-process RPC are owner
// credentials and always keep all three scopes.
func (d *Deps) RequestScopes(r *http.Request) []string {
	if trusted, _ := r.Context().Value(trustedLocalRPCKey{}).(bool); trusted {
		return append([]string(nil), AllDeviceScopes...)
	}
	s, ok := d.store()
	if !ok {
		return nil
	}
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		supplied := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if equalTokens(supplied, s.Token()) {
			return append([]string(nil), AllDeviceScopes...)
		}
		if strings.HasPrefix(supplied, "hm_") {
			if device := deviceForToken(s, supplied); device != nil {
				return normaliseScopes(device.Scopes)
			}
		}
	}
	if isLoopback(r.RemoteAddr) {
		if cookie, err := r.Cookie("homing_local"); err == nil && equalTokens(cookie.Value, s.Token()) {
			return append([]string(nil), AllDeviceScopes...)
		}
	}
	return nil
}

func normaliseScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return append([]string(nil), AllDeviceScopes...)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope != "read" && scope != "reply" && scope != "drive" {
			continue
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out
}

// HasScope reports whether the caller may perform an action.
func (d *Deps) HasScope(r *http.Request, scope string) bool {
	for _, candidate := range d.RequestScopes(r) {
		if candidate == scope {
			return true
		}
	}
	return false
}

// RequireScope writes a 403 and returns false when a paired phone has been
// downgraded below the scope an endpoint needs.
func (d *Deps) RequireScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	if d.HasScope(r, scope) {
		return true
	}
	WriteJSON(w, http.StatusForbidden, map[string]any{
		"error": "该设备没有 " + scope + " 权限",
		"code":  "scope_denied",
		"scope": scope,
	})
	return false
}

func equalTokens(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// deviceTokenValid checks a phone token against the stored sha256 hashes.
func deviceTokenValid(s *store.Store, token string) bool {
	return deviceForToken(s, token) != nil
}

func deviceForToken(s *store.Store, token string) *store.Device {
	sum := sha256.Sum256([]byte(token))
	digest := hex.EncodeToString(sum[:])
	for _, device := range s.Devices() {
		if device.TokenSHA256 != "" && subtle.ConstantTimeCompare([]byte(device.TokenSHA256), []byte(digest)) == 1 {
			found := device
			return &found
		}
	}
	return nil
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// guard wraps an /api handler with the token check.
func (d *Deps) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.authorized(r) {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
			return
		}
		if !d.HasScope(r, "read") {
			WriteJSON(w, http.StatusForbidden, map[string]any{"error": "该设备没有 read 权限", "code": "scope_denied", "scope": "read"})
			return
		}
		h(w, r)
	}
}

// readBody decodes a JSON object body; an empty body is an empty object.
func readBody(r *http.Request) map[string]any {
	out := map[string]any{}
	if r.Body == nil {
		return out
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return out
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return out
	}
	if json.Unmarshal(trimmed, &out) != nil {
		return map[string]any{}
	}
	return out
}

// methodNotAllowed is the shared 405 payload.
func methodNotAllowed(w http.ResponseWriter) {
	WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method Not Allowed")
}

// ctxOf keeps handlers readable when they need a context for agent lookups.
func ctxOf(r *http.Request) context.Context { return r.Context() }
