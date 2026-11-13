package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func demoDesktopServer(t *testing.T, dir, impl, token string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("health probe must not carry a token")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "impl": impl})
	})
	mux.HandleFunc("GET /api/connection", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"product": "Homing", "runtime": "go", "data_dir": dir})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDesktopAttachesToExistingGoService(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"token":"demo-desktop-token"}`), 0600); err != nil {
		t.Fatal(err)
	}
	srv := demoDesktopServer(t, dir, "go", "demo-desktop-token")
	service, err := startDesktopService(context.Background(), strings.TrimPrefix(srv.URL, "http://"), dir, "demo-version", nil)
	if err != nil {
		t.Fatal(err)
	}
	if service.owned || service.URL != srv.URL {
		t.Fatalf("attached service = %+v", service)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal("closing shell stopped external service:", err)
	}
	response.Body.Close()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("attach opened or created service state: %v", files)
	}
}

func TestDesktopRejectsForeignOrUnauthenticatedService(t *testing.T) {
	for _, tc := range []struct{ name, impl, token, serverDir string }{
		{"node", "node", "demo-token", ""}, {"bad-token", "go", "demo-other-token", ""}, {"different-profile", "go", "demo-token", "different-profile"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"token":"demo-token"}`), 0600); err != nil {
				t.Fatal(err)
			}
			serverDir := dir
			if tc.serverDir != "" {
				serverDir = filepath.Join(dir, tc.serverDir)
			}
			srv := demoDesktopServer(t, serverDir, tc.impl, tc.token)
			if _, err := startDesktopService(context.Background(), strings.TrimPrefix(srv.URL, "http://"), dir, "demo", nil); err == nil {
				t.Fatal("unverified service was accepted")
			}
		})
	}
}

func TestDesktopStartsAndClosesOnlyOwnedService(t *testing.T) {
	service, err := startDesktopService(context.Background(), "127.0.0.1:0", t.TempDir(), "demo-version", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	if !service.owned {
		t.Fatal("new service should be owned")
	}
	response, err := http.Get(service.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if response, err := http.Get(service.URL + "/health"); err == nil {
		response.Body.Close()
		t.Fatal("owned service still running")
	}
}

func TestDesktopProbeDoesNotFollowRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	srv := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusFound))
	defer srv.Close()
	if _, err := probeDesktopService(context.Background(), srv.URL, "demo-token", srv.Client()); err == nil {
		t.Fatal("redirect accepted")
	}
	if reached {
		t.Fatal("probe followed redirect")
	}
}

func TestDesktopRejectsAmbiguousRelativeServiceDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if sameDesktopDataDir("demo-state", filepath.Join(cwd, "demo-state")) {
		t.Fatal("service-relative path was interpreted against shell working directory")
	}
}

func TestOwnedDesktopAdvertisesAbsoluteDataDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	relative, err := filepath.Rel(cwd, dir)
	if err != nil {
		t.Fatal(err)
	}
	local, err := startDesktopService(context.Background(), "127.0.0.1:0", relative, "demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	r, _ := http.NewRequest(http.MethodGet, local.URL+"/api/connection", nil)
	r.Header.Set("Authorization", "Bearer "+desktopToken(dir))
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var info struct {
		DataDir string `json:"data_dir"`
	}
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(info.DataDir) {
		t.Fatalf("service advertises relative directory %q", info.DataDir)
	}
}
