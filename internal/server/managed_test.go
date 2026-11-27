package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
)

type captureEmailSender struct {
	mu    sync.Mutex
	codes map[string]string
}

func (c *captureEmailSender) SendLoginCode(_ context.Context, toEmail, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.codes == nil {
		c.codes = make(map[string]string)
	}
	c.codes[strings.ToLower(toEmail)] = code
	return nil
}

func (c *captureEmailSender) lastCode(email string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.codes[strings.ToLower(email)]
}

func dialAuthedTestDevice(t *testing.T, wsURL, bearerKey string) (*websocket.Conn, ed25519.PublicKey, ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	devID := protocol.DeviceID(pub)
	var opts *websocket.DialOptions
	if bearerKey != "" {
		opts = &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + bearerKey}},
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	ws.SetReadLimit(maxMessageSize)
	_, rawChal, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read challenge: %v", err)
	}
	var chal map[string]any
	_ = json.Unmarshal(rawChal, &chal)
	nonce, _ := chal["nonce"].(string)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	sig, err := protocol.HelloSignature(priv, nonce, ts)
	if err != nil {
		t.Fatalf("sign hello: %v", err)
	}
	hello, _ := json.Marshal(map[string]any{
		"t":         "hello",
		"device_id": devID,
		"pub":       protocol.ToBase64(pub),
		"ts":        ts,
		"sig":       protocol.ToBase64(sig),
	})
	if err := ws.Write(ctx, websocket.MessageText, hello); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	return ws, pub, priv, devID
}

func readWSFrame(t *testing.T, ws *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, err := ws.Read(ctx)
	if err != nil {
		t.Fatalf("read ws frame: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal ws frame: %v", err)
	}
	return out
}

func writeWSFrame(t *testing.T, ws *websocket.Conn, payload map[string]any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, _ := json.Marshal(payload)
	if err := ws.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("write ws frame: %v", err)
	}
}

func TestManagedRelayRegistrationPairingQuotasAndAdmin(t *testing.T) {
	mailer := &captureEmailSender{}
	srv, err := New(Config{
		Addr:             "127.0.0.1:0",
		DBPath:           ":memory:",
		AuthMode:         AuthModeManaged,
		RegistrationMode: RegModeOpen,
		AdminToken:       "demo-admin-secret-2026",
		EmailSender:      mailer,
	})
	if err != nil {
		t.Fatalf("New server: %v", err)
	}
	defer srv.Close()
	ln, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	baseHTTP := "http://" + ln.Addr().String()
	wsURL := srv.URL()

	// 1. Unauthenticated host WITHOUT Relay Key cannot call pair.start in managed mode!
	unauthWS, _, _, _ := dialAuthedTestDevice(t, wsURL, "")
	defer unauthWS.CloseNow()
	if f := readWSFrame(t, unauthWS); f["t"] != "hello.ok" {
		t.Fatalf("expected hello.ok for pairing-only socket, got %v", f)
	}
	writeWSFrame(t, unauthWS, map[string]any{"t": "pair.start"})
	if f := readWSFrame(t, unauthWS); f["code"] != "account_required" {
		t.Fatalf("expected account_required on unauthenticated pair.start, got %v", f)
	}

	// 2. Register via Email OTP -> receive initial Relay Key (hrk_...)
	postJSON := func(path string, body any, headers map[string]string) (*http.Response, map[string]any) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, baseHTTP+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}

	resp, sendRes := postJSON("/v1/account/email/send-code", map[string]any{"email": "demo@example.com"}, nil)
	if resp.StatusCode != http.StatusOK || sendRes["ok"] != true {
		t.Fatalf("send-code failed: %d %v", resp.StatusCode, sendRes)
	}
	otp := mailer.lastCode("demo@example.com")
	if len(otp) != 6 {
		t.Fatalf("expected 6-digit OTP, got %q", otp)
	}

	resp, verRes := postJSON("/v1/account/email/verify", map[string]any{
		"email": "demo@example.com",
		"code":  otp,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify failed: %d %v", resp.StatusCode, verRes)
	}
	relayKey, _ := verRes["initial_key"].(string)
	sessionToken, _ := verRes["session_token"].(string)
	if !strings.HasPrefix(relayKey, "hrk_") {
		t.Fatalf("expected initial hrk_ key, got %q", relayKey)
	}
	accObj, _ := verRes["account"].(map[string]any)
	accountID, _ := accObj["id"].(string)

	// 3. Host connects WITH Relay Key -> pair.start succeeds -> Phone pairs WITHOUT Relay Key!
	hostWS, _, _, hostID := dialAuthedTestDevice(t, wsURL, relayKey)
	defer hostWS.CloseNow()
	if f := readWSFrame(t, hostWS); f["t"] != "hello.ok" {
		t.Fatalf("host hello: %v", f)
	}
	writeWSFrame(t, hostWS, map[string]any{"t": "pair.start"})
	pairStart := readWSFrame(t, hostWS)
	if pairStart["t"] != "pair.start.ok" {
		t.Fatalf("expected pair.start.ok, got %v", pairStart)
	}
	pairToken, _ := pairStart["pair_token"].(string)

	phoneWS, phonePub, _, phoneID := dialAuthedTestDevice(t, wsURL, "")
	defer phoneWS.CloseNow()
	if f := readWSFrame(t, phoneWS); f["t"] != "hello.ok" {
		t.Fatalf("phone hello: %v", f)
	}
	writeWSFrame(t, phoneWS, map[string]any{
		"t":         "pair.join",
		"token":     pairToken,
		"client_id": phoneID,
		"pub":       protocol.ToBase64(phonePub),
		"name":      "Demo Phone",
		"platform":  "android",
	})
	if f := readWSFrame(t, phoneWS); f["t"] != "pair.waiting" {
		t.Fatalf("expected pair.waiting, got %v", f)
	}
	if reqFrame := readWSFrame(t, hostWS); reqFrame["t"] != "pair.request" {
		t.Fatalf("expected pair.request on host, got %v", reqFrame)
	}
	writeWSFrame(t, hostWS, map[string]any{"t": "pair.accept", "client_id": phoneID})
	if f := readWSFrame(t, hostWS); f["t"] != "pair.ok" {
		t.Fatalf("expected pair.ok, got %v", f)
	}
	if f := readWSFrame(t, phoneWS); f["t"] != "pair.accepted" {
		t.Fatalf("expected pair.accepted, got %v", f)
	}

	// 4. Phone can send E2E messages immediately, AND Free tier enforces 512 KB single-message cap!
	writeWSFrame(t, phoneWS, map[string]any{
		"v":          1,
		"t":          "msg",
		"id":         "msg-small-1",
		"from":       phoneID,
		"to":         hostID,
		"ciphertext": "ZGVtby1jaXBoZXJ0ZXh0",
	})
	if delivered := readWSFrame(t, hostWS); delivered["id"] != "msg-small-1" {
		t.Fatalf("expected delivered message on host, got %v", delivered)
	}

	oversized := strings.Repeat("A", 550*1024)
	writeWSFrame(t, phoneWS, map[string]any{
		"v":          1,
		"t":          "msg",
		"id":         "msg-oversized-free",
		"from":       phoneID,
		"to":         hostID,
		"ciphertext": oversized,
	})
	if errFrame := readWSFrame(t, phoneWS); errFrame["code"] != "payload_too_large_for_tier" {
		t.Fatalf("expected payload_too_large_for_tier on Free tier, got %v", errFrame)
	}

	// 5. Admin generates a Sponsor Redeem Code -> User redeems it -> 550 KB message now succeeds!
	adminHeaders := map[string]string{"Authorization": "Bearer demo-admin-secret-2026"}
	resp, codeRes := postJSON("/v1/admin/redeem-codes", map[string]any{
		"count":    1,
		"tier":     "sponsor",
		"max_uses": 1,
		"note":     "demo-wechat-sponsor",
	}, adminHeaders)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create redeem code: %d %v", resp.StatusCode, codeRes)
	}
	codesArr, _ := codeRes["codes"].([]any)
	firstCodeObj, _ := codesArr[0].(map[string]any)
	sponsorCode, _ := firstCodeObj["code"].(string)

	resp, redeemRes := postJSON("/v1/account/redeem", map[string]any{"code": sponsorCode}, map[string]string{
		"Authorization": "Bearer " + sessionToken,
	})
	if resp.StatusCode != http.StatusOK || redeemRes["ok"] != true {
		t.Fatalf("redeem failed: %d %v", resp.StatusCode, redeemRes)
	}

	writeWSFrame(t, phoneWS, map[string]any{
		"v":          1,
		"t":          "msg",
		"id":         "msg-large-sponsor",
		"from":       phoneID,
		"to":         hostID,
		"ciphertext": oversized,
	})
	if delivered := readWSFrame(t, hostWS); delivered["id"] != "msg-large-sponsor" {
		t.Fatalf("expected sponsor oversized message delivered, got %v", delivered)
	}

	// 6. Admin bans account -> live sockets receive account_suspended!
	banBody, _ := json.Marshal(map[string]any{"status": "banned"})
	banReq, _ := http.NewRequest(http.MethodPatch, baseHTTP+"/v1/admin/accounts/"+accountID, bytes.NewReader(banBody))
	banReq.Header.Set("Content-Type", "application/json")
	banReq.Header.Set("Authorization", "Bearer demo-admin-secret-2026")
	banResp, err := http.DefaultClient.Do(banReq)
	if err != nil || banResp.StatusCode != http.StatusOK {
		t.Fatalf("admin ban failed: %v %v", err, banResp)
	}
	_ = banResp.Body.Close()

	suspended := readWSFrame(t, hostWS)
	if suspended["code"] != "account_suspended" {
		t.Fatalf("expected account_suspended pushed to banned host, got %v", suspended)
	}
}

func TestManagedGitHubOAuthFlow(t *testing.T) {
	mockGH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_, _ = w.Write([]byte(`{"access_token":"gho_demo_test_token"}`))
		case "/user":
			_, _ = w.Write([]byte(`{"id":998877,"login":"demo-dev","email":"demo-dev@example.org"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer mockGH.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		DBPath:   ":memory:",
		AuthMode: AuthModeManaged,
		GitHubOAuth: GitHubOAuthConfig{
			ClientID:     "demo-client-id",
			ClientSecret: "demo-client-secret",
			AuthorizeURL: mockGH.URL + "/login/oauth/authorize",
			TokenURL:     mockGH.URL + "/login/oauth/access_token",
			APIBaseURL:   mockGH.URL,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer srv.Close()
	ln, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	baseHTTP := "http://" + ln.Addr().String()

	noRedirect := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	startResp, err := noRedirect.Get(baseHTTP + "/v1/account/github/start")
	if err != nil || startResp.StatusCode != http.StatusFound {
		t.Fatalf("github start: %v %v", err, startResp)
	}
	loc := startResp.Header.Get("Location")
	var stateCookie *http.Cookie
	for _, ck := range startResp.Cookies() {
		if ck.Name == githubOAuthStateCk {
			stateCookie = ck
		}
	}
	_ = startResp.Body.Close()
	if stateCookie == nil || !strings.Contains(loc, "state="+stateCookie.Value) {
		t.Fatalf("missing state cookie or redirect state: %v %s", stateCookie, loc)
	}

	cbReq, _ := http.NewRequest(http.MethodGet, baseHTTP+"/v1/account/github/callback?code=demo-code&state="+stateCookie.Value, nil)
	cbReq.AddCookie(stateCookie)
	cbResp, err := noRedirect.Do(cbReq)
	if err != nil || cbResp.StatusCode != http.StatusFound {
		t.Fatalf("github callback: %v %v", err, cbResp)
	}
	cbLoc := cbResp.Header.Get("Location")
	if !strings.HasPrefix(cbLoc, "/account#new_key=hrk_") {
		t.Fatalf("expected redirect with initial hrk_ key, got %q", cbLoc)
	}
	_ = cbResp.Body.Close()
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestManagedWeeklyTimeLimitAdminControlsAndRateLimits(t *testing.T) {
	mailer := &captureEmailSender{}
	clock := &fakeClock{now: time.Now().UTC().Truncate(time.Minute).Add(5 * time.Second)}
	srv, err := New(Config{
		Addr:              "127.0.0.1:0",
		DBPath:            ":memory:",
		AuthMode:          AuthModeManaged,
		RegistrationMode:  RegModeOpen,
		AdminToken:        "demo-admin-secret-weekly",
		EmailSender:       mailer,
		FreeWeeklyMinutes: 2,
		Now:               clock.Now,
	})
	if err != nil {
		t.Fatalf("New server: %v", err)
	}
	defer srv.Close()
	ln, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	baseHTTP := "http://" + ln.Addr().String()
	wsURL := srv.URL()

	doJSON := func(method, path string, body any, headers map[string]string) (*http.Response, map[string]any) {
		raw := []byte(nil)
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, baseHTTP+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}
	admin := map[string]string{"Authorization": "Bearer demo-admin-secret-weekly"}

	// Config advertises the weekly allowance.
	if _, cfg := doJSON(http.MethodGet, "/v1/account/config", nil, nil); cfg["free_weekly_minutes"] != float64(2) {
		t.Fatalf("expected free_weekly_minutes=2 in config, got %v", cfg)
	}

	// Register and pair one host + one phone.
	if resp, out := doJSON(http.MethodPost, "/v1/account/email/send-code", map[string]any{"email": "weekly@example.com"}, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("send-code: %d %v", resp.StatusCode, out)
	}
	_, ver := doJSON(http.MethodPost, "/v1/account/email/verify", map[string]any{
		"email": "weekly@example.com", "code": mailer.lastCode("weekly@example.com"),
	}, nil)
	relayKey, _ := ver["initial_key"].(string)
	sessionToken, _ := ver["session_token"].(string)
	accObj, _ := ver["account"].(map[string]any)
	accountID, _ := accObj["id"].(string)

	hostWS, _, _, hostID := dialAuthedTestDevice(t, wsURL, relayKey)
	defer hostWS.CloseNow()
	readWSFrame(t, hostWS)
	writeWSFrame(t, hostWS, map[string]any{"t": "pair.start"})
	pairToken, _ := readWSFrame(t, hostWS)["pair_token"].(string)
	phoneWS, phonePub, _, phoneID := dialAuthedTestDevice(t, wsURL, "")
	defer phoneWS.CloseNow()
	readWSFrame(t, phoneWS)
	writeWSFrame(t, phoneWS, map[string]any{
		"t": "pair.join", "token": pairToken, "client_id": phoneID,
		"pub": protocol.ToBase64(phonePub), "name": "Demo Phone", "platform": "android",
	})
	readWSFrame(t, phoneWS) // pair.waiting
	readWSFrame(t, hostWS)  // pair.request
	writeWSFrame(t, hostWS, map[string]any{"t": "pair.accept", "client_id": phoneID})
	readWSFrame(t, hostWS)  // pair.ok
	readWSFrame(t, phoneWS) // pair.accepted

	send := func(from *websocket.Conn, fromID, toID, id string) {
		writeWSFrame(t, from, map[string]any{"v": 1, "t": "msg", "id": id, "from": fromID, "to": toID, "ciphertext": "ZGVtbw=="})
	}

	// Minute 1: two phone envelopes count once.
	send(phoneWS, phoneID, hostID, "w-1")
	if f := readWSFrame(t, hostWS); f["id"] != "w-1" {
		t.Fatalf("expected w-1 delivered, got %v", f)
	}
	send(phoneWS, phoneID, hostID, "w-2")
	if f := readWSFrame(t, hostWS); f["id"] != "w-2" {
		t.Fatalf("expected w-2 delivered, got %v", f)
	}
	// Minute 2: still inside the 2-minute allowance.
	clock.Advance(time.Minute)
	send(phoneWS, phoneID, hostID, "w-3")
	if f := readWSFrame(t, hostWS); f["id"] != "w-3" {
		t.Fatalf("expected w-3 delivered, got %v", f)
	}
	// Minute 3: allowance used up -> phone gets weekly_time_exceeded.
	clock.Advance(time.Minute)
	send(phoneWS, phoneID, hostID, "w-4")
	if f := readWSFrame(t, phoneWS); f["code"] != "weekly_time_exceeded" {
		t.Fatalf("expected weekly_time_exceeded, got %v", f)
	}
	// Desktop pushes to the phone are never limited.
	send(hostWS, hostID, phoneID, "push-1")
	if f := readWSFrame(t, phoneWS); f["id"] != "push-1" {
		t.Fatalf("expected host push delivered despite weekly limit, got %v", f)
	}

	// The account portal reports usage.
	_, me := doJSON(http.MethodGet, "/v1/account/me", nil, map[string]string{"Authorization": "Bearer " + sessionToken})
	weekly, _ := me["weekly"].(map[string]any)
	if weekly["active_minutes"] != float64(2) || weekly["limit_minutes"] != float64(2) {
		t.Fatalf("unexpected weekly usage in /me: %v", weekly)
	}

	// Admin sees usage, raises this account's allowance, phone works again.
	_, list := doJSON(http.MethodGet, "/v1/admin/accounts", nil, admin)
	rows, _ := list["accounts"].([]any)
	if len(rows) != 1 {
		t.Fatalf("expected 1 account, got %v", list)
	}
	row, _ := rows[0].(map[string]any)
	rowWeekly, _ := row["weekly"].(map[string]any)
	if rowWeekly["active_minutes"] != float64(2) || rowWeekly["limit_minutes"] != float64(2) {
		t.Fatalf("unexpected admin weekly row: %v", rowWeekly)
	}
	if resp, out := doJSON(http.MethodPatch, "/v1/admin/accounts/"+accountID, map[string]any{"weekly_minutes": 10}, admin); resp.StatusCode != http.StatusOK {
		t.Fatalf("patch weekly_minutes: %d %v", resp.StatusCode, out)
	}
	send(phoneWS, phoneID, hostID, "w-5")
	if f := readWSFrame(t, hostWS); f["id"] != "w-5" {
		t.Fatalf("expected w-5 delivered after raising allowance, got %v", f)
	}
	if resp, _ := doJSON(http.MethodPatch, "/v1/admin/accounts/acc_missing", map[string]any{"status": "banned"}, admin); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown account, got %d", resp.StatusCode)
	}

	// Older desktop builds send "invite_only"; it must map to "invite".
	if resp, out := doJSON(http.MethodPut, "/v1/admin/settings", map[string]any{"registration_mode": "invite_only"}, admin); resp.StatusCode != http.StatusOK || out["registration_mode"] != RegModeInvite {
		t.Fatalf("invite_only alias: %d %v", resp.StatusCode, out)
	}

	// Usage resets next week.
	if used, _ := srv.DB().GetAccountWeeklyMinutes(context.Background(), accountID, clock.Now().AddDate(0, 0, 7)); used != 0 {
		t.Fatalf("expected a fresh week to start at 0, got %d", used)
	}

	// Wrong admin tokens are rate limited per address.
	for i := 0; i < adminFailLimit; i++ {
		if resp, _ := doJSON(http.MethodPost, "/v1/admin/login", map[string]any{"token": "wrong"}, nil); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, resp.StatusCode)
		}
	}
	if resp, _ := doJSON(http.MethodPost, "/v1/admin/login", map[string]any{"token": "demo-admin-secret-weekly"}, nil); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after repeated failures, got %d", resp.StatusCode)
	}
}

func TestWeekKeyStartsMondayChinaTime(t *testing.T) {
	// 2026-11-29 is a Sunday; 23:59 CST is still the week of 11-23.
	sunday := time.Date(2026, 11, 29, 15, 59, 0, 0, time.UTC)
	if got := weekKey(sunday); got != "2026-11-23" {
		t.Fatalf("weekKey(sunday) = %s", got)
	}
	// 2026-11-30 00:00 CST is Monday -> new week.
	monday := time.Date(2026, 11, 29, 16, 0, 0, 0, time.UTC)
	if got := weekKey(monday); got != "2026-11-30" {
		t.Fatalf("weekKey(monday) = %s", got)
	}
	if reset := weekResetAt(sunday); !reset.Equal(monday) {
		t.Fatalf("weekResetAt = %v, want %v", reset, monday)
	}
}

func dialDeviceWithKeypair(t *testing.T, wsURL, bearerKey string, pub ed25519.PublicKey, priv ed25519.PrivateKey) *websocket.Conn {
	t.Helper()
	devID := protocol.DeviceID(pub)
	var opts *websocket.DialOptions
	if bearerKey != "" {
		opts = &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + bearerKey}},
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	ws.SetReadLimit(maxMessageSize)
	chal := readWSFrame(t, ws)
	nonce, _ := chal["nonce"].(string)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	sig, err := protocol.HelloSignature(priv, nonce, ts)
	if err != nil {
		t.Fatalf("sign hello: %v", err)
	}
	writeWSFrame(t, ws, map[string]any{
		"t":         "hello",
		"device_id": devID,
		"pub":       protocol.ToBase64(pub),
		"ts":        ts,
		"sig":       protocol.ToBase64(sig),
	})
	return ws
}

func TestManagedHelloReportsBoundAndAutoAdoptsLegacyPairing(t *testing.T) {
	mailer := &captureEmailSender{}
	srv, err := New(Config{
		Addr:             "127.0.0.1:0",
		DBPath:           ":memory:",
		AuthMode:         AuthModeManaged,
		RegistrationMode: RegModeOpen,
		EmailSender:      mailer,
	})
	if err != nil {
		t.Fatalf("New server: %v", err)
	}
	defer srv.Close()
	ln, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	baseHTTP := "http://" + ln.Addr().String()
	wsURL := srv.URL()

	// 1. Unbound socket receives hello.ok with auth_mode=managed, bound=false.
	unboundWS, _, _, _ := dialAuthedTestDevice(t, wsURL, "")
	defer unboundWS.CloseNow()
	unboundHello := readWSFrame(t, unboundWS)
	if unboundHello["t"] != "hello.ok" || unboundHello["auth_mode"] != AuthModeManaged || unboundHello["bound"] != false {
		t.Fatalf("expected managed unbound hello.ok, got %v", unboundHello)
	}

	// 2. Register an account to obtain a Relay Key.
	postJSON := func(path string, body any) map[string]any {
		raw, _ := json.Marshal(body)
		resp, err := http.Post(baseHTTP+path, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	postJSON("/v1/account/email/send-code", map[string]any{"email": "adopt@example.com"})
	ver := postJSON("/v1/account/email/verify", map[string]any{
		"email": "adopt@example.com",
		"code":  mailer.lastCode("adopt@example.com"),
	})
	relayKey, _ := ver["initial_key"].(string)
	if !strings.HasPrefix(relayKey, "hrk_") {
		t.Fatalf("expected relayKey, got %v", ver)
	}

	// 3. Simulate a legacy pairing created before managed mode existed (devices + pairings, no account_devices).
	hostPub, hostPriv, _ := ed25519.GenerateKey(nil)
	hostID := protocol.DeviceID(hostPub)
	phonePub, phonePriv, _ := ed25519.GenerateKey(nil)
	phoneID := protocol.DeviceID(phonePub)
	now := time.Now().UTC()
	if ok, err := srv.DB().RegisterDevice(context.Background(), hostID, protocol.ToBase64(hostPub), now); err != nil || !ok {
		t.Fatalf("RegisterDevice host: ok=%v err=%v", ok, err)
	}
	if ok, err := srv.DB().RegisterDevice(context.Background(), phoneID, protocol.ToBase64(phonePub), now); err != nil || !ok {
		t.Fatalf("RegisterDevice phone: ok=%v err=%v", ok, err)
	}
	if err := srv.DB().CreatePairing(context.Background(), hostID, phoneID, now); err != nil {
		t.Fatalf("CreatePairing: %v", err)
	}

	// Host connects with Relay Key -> bound=true.
	hostWS := dialDeviceWithKeypair(t, wsURL, relayKey, hostPub, hostPriv)
	defer hostWS.CloseNow()
	hostHello := readWSFrame(t, hostWS)
	if hostHello["t"] != "hello.ok" || hostHello["bound"] != true {
		t.Fatalf("expected bound host hello.ok, got %v", hostHello)
	}

	// Pre-paired phone reconnects without Relay Key -> auto-adopted -> bound=true and can relay E2E messages immediately.
	phoneWS := dialDeviceWithKeypair(t, wsURL, "", phonePub, phonePriv)
	defer phoneWS.CloseNow()
	phoneHello := readWSFrame(t, phoneWS)
	if phoneHello["t"] != "hello.ok" || phoneHello["bound"] != true {
		t.Fatalf("expected auto-adopted phone hello.ok bound=true, got %v", phoneHello)
	}
	writeWSFrame(t, phoneWS, map[string]any{
		"v": 1, "t": "msg", "id": "legacy-adopt-1", "from": phoneID, "to": hostID, "ciphertext": "ZGVtbw==",
	})
	if delivered := readWSFrame(t, hostWS); delivered["id"] != "legacy-adopt-1" {
		t.Fatalf("expected legacy-adopt-1 delivered to host, got %v", delivered)
	}
}
