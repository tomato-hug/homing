package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	consoleweb "github.com/tomato-hug/homing/web/console"
)

func consoleCall(t *testing.T, d *Deps, method, target, remote string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	d.handleConsole(rec, req)
	return rec
}

func TestConsoleServesIndexAndCookie(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	rec := consoleCall(t, d, http.MethodGet, "/console/", "127.0.0.1:12345")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `id="app"`) {
		t.Fatalf("body is not the console shell: %s", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control = %q", got)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "homing_local" || cookies[0].Value != "demo-token" || !cookies[0].HttpOnly {
		t.Fatalf("cookies = %+v, want homing_local=demo-token HttpOnly", cookies)
	}
	if got := rec.Header().Get("Content-Length"); got == "" || got == "0" {
		t.Fatalf("content-length = %q", got)
	}
}

func TestConsoleSpaFallbackAndAssets(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	spa := consoleCall(t, d, http.MethodGet, "/console/sessions", "127.0.0.1:12345")
	if spa.Code != http.StatusOK || !strings.Contains(spa.Body.String(), `id="app"`) {
		t.Fatalf("SPA route did not fall back to index: %d %s", spa.Code, spa.Body.String())
	}

	entries, err := consoleweb.Files.ReadDir("assets")
	if err != nil || len(entries) == 0 {
		t.Fatalf("embedded assets = %v, %v", entries, err)
	}
	asset := consoleCall(t, d, http.MethodGet, "/console/assets/"+entries[0].Name(), "127.0.0.1:12345")
	if asset.Code != http.StatusOK {
		t.Fatalf("asset status = %d", asset.Code)
	}
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache-control = %q", got)
	}
	if got := asset.Header().Get("Content-Type"); got == "" {
		t.Fatal("asset content-type is empty")
	}

	missing := consoleCall(t, d, http.MethodGet, "/console/assets/missing.js", "127.0.0.1:12345")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", missing.Code)
	}
}

func TestConsoleIsLoopbackOnly(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	rec := consoleCall(t, d, http.MethodGet, "/console/", "192.168.1.20:4567")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback status = %d, want 403", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/console/", nil)
	req.RemoteAddr = "192.168.1.20:4567"
	req.Header.Set("Accept", "text/html")
	htmlRec := httptest.NewRecorder()
	d.handleConsole(htmlRec, req)
	if htmlRec.Code != http.StatusForbidden || !strings.Contains(htmlRec.Body.String(), "ssh -N -L 18787") {
		t.Fatalf("non-loopback HTML guide = %d %s", htmlRec.Code, htmlRec.Body.String())
	}

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootReq.RemoteAddr = "127.0.0.1:12345"
	rootRec := httptest.NewRecorder()
	d.handleRoot(rootRec, rootReq)
	if rootRec.Code != http.StatusFound || rootRec.Header().Get("Location") != "/console/" {
		t.Fatalf("loopback root = %d location=%q", rootRec.Code, rootRec.Header().Get("Location"))
	}
}

func TestConsoleMethodsAndHead(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	post := consoleCall(t, d, http.MethodPost, "/console/", "127.0.0.1:12345")
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", post.Code)
	}
	head := consoleCall(t, d, http.MethodHead, "/console/", "127.0.0.1:12345")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD status/body = %d/%d", head.Code, head.Body.Len())
	}
}
