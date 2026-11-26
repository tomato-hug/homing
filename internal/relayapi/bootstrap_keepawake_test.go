package relayapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/store"
)

func TestMobileBootstrapAndKeepAwakeRoutes(t *testing.T) {
	dir := t.TempDir()
	deps := &api.Deps{DataDir: dir, Version: "0.11.6-test", Log: slog.Default()}
	defer deps.Close()
	mux := api.NewMux(deps)

	st, err := deps.Store()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := st.AppendEvent(store.Event{
		Source:    "codex",
		Kind:      "reply",
		SessionID: "demo-session-1",
		Title:     "Demo task",
		Text:      "All checks passed.",
		Ts:        "2026-11-25T12:00:00.000Z",
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	// 1. Mobile bootstrap via trusted local RPC (same path Connector uses)
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/bootstrap?since=0&session_limit=50&include_meta=1", nil)
	req = api.WithTrustedLocalRPC(req)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	for _, key := range []string{"events", "sessions", "away", "home", "inbox"} {
		if len(payload[key]) == 0 {
			t.Fatalf("bootstrap missing key %q: %s", key, rec.Body.String())
		}
	}

	// 2. KeepAwake PATCH + GET round-trip
	patchBody := bytes.NewBufferString(`{"enabled":true,"auto_on_away":false,"max_minutes":90}`)
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/keepawake", patchBody)
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq = api.WithTrustedLocalRPC(patchReq)
	patchRec := httptest.NewRecorder()
	mux.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("keepawake patch status=%d body=%s", patchRec.Code, patchRec.Body.String())
	}
	var ka struct {
		Enabled    bool `json:"enabled"`
		AutoOnAway bool `json:"auto_on_away"`
		MaxMinutes int  `json:"max_minutes"`
	}
	if err := json.Unmarshal(patchRec.Body.Bytes(), &ka); err != nil {
		t.Fatalf("decode keepawake: %v", err)
	}
	if ka.AutoOnAway != false || ka.MaxMinutes != 90 {
		t.Fatalf("unexpected keepawake state: %+v", ka)
	}
}
