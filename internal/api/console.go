package api

import (
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	consoleweb "github.com/tomato-hug/homing/web/console"
)

// Console asset types match relay/console.mjs and the Caddy-style defaults.
var consoleTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".ico":   "image/x-icon",
	".map":   "application/json; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
}

func init() {
	// Lower than the API groups: /console is static and never falls through.
	Register("console", -10, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /console", d.handleConsole)
		mux.HandleFunc("GET /console/{path...}", d.handleConsole)
	})
}

// handleConsole serves the embedded control panel. Like the Node relay it is
// loopback-only, sends the console cookie on HTML, and falls back to index.html
// for extension-less client-side routes.
func (d *Deps) handleConsole(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) {
		WriteJSON(w, http.StatusForbidden, map[string]any{
			"error": "控制台只对本机开放",
			"hint":  "请在运行 homing serve 的那台电脑上打开 http://127.0.0.1:18787/console",
		})
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}

	rel := strings.TrimPrefix(r.URL.Path, "/console")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || path.Ext(rel) == "" {
		rel = "index.html"
	}
	rel = path.Clean("/" + rel)[1:]

	body, err := consoleweb.Files.ReadFile(rel)
	if err != nil {
		if rel == "index.html" {
			WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "控制台资源缺失"})
			return
		}
		WriteJSON(w, http.StatusNotFound, map[string]any{"error": "控制台资源不存在"})
		return
	}

	ext := strings.ToLower(path.Ext(rel))
	contentType := consoleTypes[ext]
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	cache := "public, max-age=31536000, immutable"
	if ext == ".html" {
		cache = "no-store"
	}

	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Cache-Control", cache)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	if ext == ".html" {
		if s, ok := d.store(); ok && s.Token() != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     "homing_local",
				Value:    s.Token(),
				Path:     "/",
				MaxAge:   30 * 24 * 60 * 60,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
