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
	".ttf":   "font/ttf",
	".ico":   "image/x-icon",
	".map":   "application/json; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
}

func init() {
	// Lower than the API groups: /console is static and never falls through.
	Register("console", -10, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /{$}", d.handleRoot)
		mux.HandleFunc("GET /console", d.handleConsole)
		mux.HandleFunc("GET /console/{path...}", d.handleConsole)
	})
}

const loopbackGuideHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="dark">
<title>Homing · Local Console Only</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0f172a;color:#f8fafc;font:14px/1.6 -apple-system,BlinkMacSystemFont,"PingFang SC","Segoe UI",sans-serif;padding:24px}
main{max-width:540px;background:#1b2336;border:1px solid #334155;border-radius:14px;padding:24px}
h1{font-size:18px;margin:0 0 8px}
p{margin:0 0 12px;color:#cbd5e1}
pre{background:#0b1222;border:1px solid #1e293b;border-radius:8px;padding:12px;overflow:auto;color:#2dd4bf;font:12.5px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}
.sub{color:#94a3b8;font-size:13px;margin-top:14px;padding-top:12px;border-top:1px solid #1e293b}
</style>
</head>
<body>
<main>
  <h1>控制台仅对本机开放 · Loopback Only</h1>
  <p>出于安全考虑，Homing Web 控制台默认只允许本机回环地址（<code>127.0.0.1</code>）直接访问。</p>
  <p>若服务运行在远程服务器或 Docker 中，请在本机终端建立 SSH 端口转发后访问：</p>
  <pre>ssh -N -L 18787:127.0.0.1:18787 user@your-server
# 然后在本机浏览器打开 / Open locally:
http://127.0.0.1:18787/console/</pre>
  <div class="sub">For security, the Homing console only accepts loopback requests. Forward port 18787 over SSH to access it from a remote devbox or container, or open <code>/get</code> on a phone on the same LAN to pair.</div>
</main>
</body>
</html>`

func (d *Deps) handleRoot(w http.ResponseWriter, r *http.Request) {
	if isLoopback(r.RemoteAddr) {
		http.Redirect(w, r, "/console/", http.StatusFound)
		return
	}
	d.writeLoopbackOnly(w, r)
}

func (d *Deps) writeLoopbackOnly(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusForbidden)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(loopbackGuideHTML))
		}
		return
	}
	WriteJSON(w, http.StatusForbidden, map[string]any{
		"error": "控制台只对本机开放",
		"code":  "loopback_only",
		"hint":  "请在运行 homing serve 的那台电脑上打开 http://127.0.0.1:18787/console，或使用 ssh -L 18787:127.0.0.1:18787 转发访问",
	})
}

// handleConsole serves the embedded control panel. Like the Node relay it is
// loopback-only, sends the console cookie on HTML, and falls back to index.html
// for extension-less client-side routes.
func (d *Deps) handleConsole(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) {
		d.writeLoopbackOnly(w, r)
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
