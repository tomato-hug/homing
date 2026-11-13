package relayapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestAutoAwaySettingsRoundTrip(t *testing.T) {
	relay := newTestRelay(t, nil)
	response, raw := relay.do(t, http.MethodGet, "/api/away/settings", nil, true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", response.StatusCode, raw)
	}
	var initial struct {
		Enabled     bool `json:"enabled"`
		IdleMinutes int  `json:"idle_minutes"`
		Available   bool `json:"available"`
	}
	if err := json.Unmarshal(raw, &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Enabled || initial.IdleMinutes != 5 {
		t.Fatalf("initial settings=%+v", initial)
	}

	response, raw = relay.do(t, http.MethodPatch, "/api/away/settings",
		[]byte(`{"enabled":true,"idle_minutes":7}`), true)
	if !initial.Available {
		var rejected struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(raw, &rejected); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusConflict || rejected.Code != "auto_away_unavailable" {
			t.Fatalf("unsupported platform PATCH status=%d body=%s", response.StatusCode, raw)
		}
		if enabled, minutes := relay.store.AutoAwaySettings(); enabled || minutes != 5 {
			t.Fatalf("rejected enable changed settings: enabled=%v minutes=%d", enabled, minutes)
		}
		response, raw = relay.do(t, http.MethodPatch, "/api/away/settings",
			[]byte(`{"enabled":false,"idle_minutes":7}`), true)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status=%d body=%s", response.StatusCode, raw)
	}
	enabled, minutes := relay.store.AutoAwaySettings()
	if enabled != initial.Available || minutes != 7 {
		t.Fatalf("stored settings enabled=%v minutes=%d", enabled, minutes)
	}
}

func TestEndAwayReleasesStopHolds(t *testing.T) {
	relay := newTestRelay(t, nil)
	if _, err := relay.store.StartAway("manual", "demo-phone"); err != nil {
		t.Fatal(err)
	}
	hold := &stopHold{sessionID: "demo-held", ch: make(chan *string, 1)}
	waits.mu.Lock()
	waits.bySession[hold.sessionID] = []*stopHold{hold}
	waits.mu.Unlock()

	response, raw := relay.do(t, http.MethodPost, "/api/away", []byte(`{"enabled":false}`), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, raw)
	}
	select {
	case value := <-hold.ch:
		if value != nil {
			t.Fatalf("hold released with %q, want nil", *value)
		}
	case <-time.After(time.Second):
		t.Fatal("stop hold was not released when away ended")
	}
}
