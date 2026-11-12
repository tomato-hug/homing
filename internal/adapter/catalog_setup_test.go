package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConsoleOnlyRejectsPhonesAndOtherSites(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		header map[string]string
		want   bool
	}{
		{"console", "127.0.0.1:5000", map[string]string{"Origin": "http://127.0.0.1:18787"}, true},
		{"no origin", "127.0.0.1:5000", nil, true},
		{"lan phone", "192.168.1.20:5000", nil, false},
		{"cloud rpc", "127.0.0.1:5000", map[string]string{"X-Homing-RPC": "1"}, false},
		{"other site", "127.0.0.1:5000", map[string]string{"Origin": "https://evil.example"}, false},
	}
	for _, tc := range cases {
		request := httptest.NewRequest(http.MethodPost, "/api/agent-catalog/codex/setup", nil)
		request.RemoteAddr = tc.remote
		for key, value := range tc.header {
			request.Header.Set(key, value)
		}
		recorder := httptest.NewRecorder()
		if got := consoleOnly(recorder, request); got != tc.want {
			t.Fatalf("%s: consoleOnly = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestStartCatalogSetupValidatesIndex(t *testing.T) {
	if _, status, problem := StartCatalogSetup("codex", 99); problem == "" || status != http.StatusBadRequest {
		t.Fatalf("status=%d problem=%q", status, problem)
	}
	if _, status, _ := StartCatalogSetup("no-such-agent", 0); status != http.StatusNotFound {
		t.Fatalf("status=%d", status)
	}
}

func TestCatalogHasInstallCommandsForCoreAgents(t *testing.T) {
	for _, id := range []string{"codex", "claude"} {
		spec, ok := catalogSpecByID(id)
		if !ok || len(spec.InstallCommands) == 0 {
			t.Fatalf("%s has no install commands", id)
		}
	}
	if spec, _ := catalogSpecByID("workbuddy"); spec.DownloadURL == "" {
		t.Fatal("workbuddy needs a download page")
	}
}
