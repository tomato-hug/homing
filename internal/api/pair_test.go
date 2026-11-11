package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPairOfferClaimAndDeviceList(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	mux := NewMux(d)

	created := httptest.NewRecorder()
	mux.ServeHTTP(created, consoleRequest(t, http.MethodPost, "http://127.0.0.1:18788/api/pair/offer", `{}`))
	if created.Code != http.StatusOK {
		t.Fatalf("create offer = %d %s", created.Code, created.Body.String())
	}
	var offer pairOfferView
	if err := json.Unmarshal(created.Body.Bytes(), &offer); err != nil {
		t.Fatal(err)
	}
	if offer.OfferID == "" || offer.Token == "" || len(offer.Code) != 6 || offer.Status != "waiting" {
		t.Fatalf("offer = %+v", offer)
	}
	if offer.QRPayload == nil || !strings.Contains(*offer.QRPayload, offer.Token) {
		t.Fatalf("qr payload = %v", offer.QRPayload)
	}

	latest := httptest.NewRecorder()
	mux.ServeHTTP(latest, consoleRequest(t, http.MethodGet, "http://127.0.0.1:18788/api/pair/latest", ""))
	var latestPayload struct {
		Offer *pairOfferView `json:"offer"`
	}
	if err := json.Unmarshal(latest.Body.Bytes(), &latestPayload); err != nil {
		t.Fatal(err)
	}
	if latestPayload.Offer == nil || latestPayload.Offer.OfferID != offer.OfferID {
		t.Fatalf("latest offer = %+v", latestPayload.Offer)
	}

	claimBody := `{"token":"` + offer.Token + `","device":{"name":"demo-phone","platform":"android","app_version":"0.1-demo"}}`
	claimReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18788/api/pair/claim", strings.NewReader(claimBody))
	claimReq.RemoteAddr = "192.168.1.20:43210"
	claimed := httptest.NewRecorder()
	mux.ServeHTTP(claimed, claimReq)
	if claimed.Code != http.StatusOK {
		t.Fatalf("claim = %d %s", claimed.Code, claimed.Body.String())
	}
	var claimPayload map[string]any
	if err := json.Unmarshal(claimed.Body.Bytes(), &claimPayload); err != nil {
		t.Fatal(err)
	}
	deviceToken, _ := claimPayload["device_token"].(string)
	if !strings.HasPrefix(deviceToken, "hm_") || claimPayload["token_type"] != "device" {
		t.Fatalf("claim payload has no device token")
	}
	phoneReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:18788/api/inbox", nil)
	phoneReq.RemoteAddr = "192.168.1.20:43210"
	phoneReq.Header.Set("Authorization", "Bearer "+deviceToken)
	phoneInbox := httptest.NewRecorder()
	mux.ServeHTTP(phoneInbox, phoneReq)
	if phoneInbox.Code != http.StatusOK {
		t.Fatalf("device token was not accepted: %d", phoneInbox.Code)
	}

	polled := httptest.NewRecorder()
	mux.ServeHTTP(polled, consoleRequest(t, http.MethodGet, "http://127.0.0.1:18788/api/pair/offer/"+offer.OfferID, ""))
	var polledOffer pairOfferView
	if err := json.Unmarshal(polled.Body.Bytes(), &polledOffer); err != nil {
		t.Fatal(err)
	}
	if polledOffer.Status != "claimed" || polledOffer.ClaimedDeviceID == nil {
		t.Fatalf("polled offer = %+v", polledOffer)
	}

	againReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18788/api/pair/claim", strings.NewReader(claimBody))
	againReq.RemoteAddr = "192.168.1.20:43210"
	again := httptest.NewRecorder()
	mux.ServeHTTP(again, againReq)
	if again.Code != http.StatusConflict {
		t.Fatalf("reused offer = %d, want 409", again.Code)
	}

	saved, err := d.Store()
	if err != nil {
		t.Fatal(err)
	}
	devices := saved.Devices()
	if len(devices) != 1 || devices[0].Name != "demo-phone" {
		t.Fatalf("devices = %+v", devices)
	}
}

func TestPairClaimOnlyFromLAN(t *testing.T) {
	d := writeStore(t, "", `{"version":1,"sessions":{}}`)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:18788/api/pair/claim", strings.NewReader(`{"code":"123456"}`))
	req.RemoteAddr = "8.8.8.8:12345"
	rec := httptest.NewRecorder()
	NewMux(d).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("public claim = %d, want 403", rec.Code)
	}
}
