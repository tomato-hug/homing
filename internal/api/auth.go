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

func equalTokens(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// deviceTokenValid checks a phone token against the stored sha256 hashes.
func deviceTokenValid(s *store.Store, token string) bool {
	sum := sha256.Sum256([]byte(token))
	digest := hex.EncodeToString(sum[:])
	for _, device := range s.Devices() {
		if device.TokenSHA256 != "" && subtle.ConstantTimeCompare([]byte(device.TokenSHA256), []byte(digest)) == 1 {
			return true
		}
	}
	return false
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
			WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
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
	WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "Method Not Allowed"})
}

// ctxOf keeps handlers readable when they need a context for agent lookups.
func ctxOf(r *http.Request) context.Context { return r.Context() }
