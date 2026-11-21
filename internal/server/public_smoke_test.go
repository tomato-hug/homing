package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
)

func smokeHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

func TestSmokeHandshakeRejectsRedirects(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "plaintext", true: "other TLS host"}[secure], func(t *testing.T) {
			var followed atomic.Bool
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				followed.Store(true)
				http.Error(w, "unexpected redirect target", http.StatusBadRequest)
			})
			target := httptest.NewUnstartedServer(handler)
			if secure {
				target.StartTLS()
			} else {
				target.Start()
			}
			defer target.Close()
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, http.StatusFound)
			}))
			defer origin.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, response, err := websocket.Dial(ctx, origin.URL, &websocket.DialOptions{HTTPClient: smokeHTTPClient(origin.Client())})
			if err == nil || response == nil || response.StatusCode != http.StatusFound || followed.Load() {
				t.Fatalf("handshake followed redirect: followed=%t, response=%v, err=%v", followed.Load(), response, err)
			}
		})
	}
}

// This opt-in check never loads a user's identity, peer store or conversations.
// It leaves two random device public-key records and revokes their test pairing.
func TestPublicRelaySmoke(t *testing.T) {
	endpoint := os.Getenv("HOMING_TEST_RELAY_URL")
	if endpoint == "" {
		t.Skip("set HOMING_TEST_RELAY_URL to explicitly test a deployed WSS relay")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "wss" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		t.Fatal("public smoke endpoint must be a credential-free wss:// URL")
	}
	runRelaySmoke(t, endpoint)
}

func TestRelaySmokeWorkflow(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	runRelaySmoke(t, fixture.url)
}

func runRelaySmoke(t *testing.T, endpoint string) {
	t.Helper()
	identity := func() testIdentity {
		keys, err := protocol.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		return testIdentity{keys: keys, pub: protocol.ToBase64(keys.PublicKey), deviceID: protocol.DeviceID(keys.PublicKey)}
	}
	hostID, phoneID := identity(), identity()
	connect := func(id testIdentity) *testPeer {
		return connectPeerWithOptions(t, endpoint, id, &websocket.DialOptions{HTTPClient: smokeHTTPClient(nil)})
	}
	host := connect(hostID)
	t.Cleanup(host.close)
	phone := connect(phoneID)
	t.Cleanup(phone.close)
	const payload = `{"type":"event","name":"homing.cloud.smoke","data":{"text":"demo-cloud-message"}}`
	matchingType := func(kind string) func(map[string]any) bool {
		return func(m map[string]any) bool { return m["t"] == kind }
	}
	barrier := func(peer *testPeer) {
		peer.send(t, map[string]any{"t": "ping"})
		peer.next(t, matchingType("pong"), 10*time.Second)
	}
	checkPayload := func(message map[string]any) {
		raw, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		var envelope protocol.Envelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		clear, err := protocol.OpenEnvelope(envelope, phoneID.keys, hostID.keys.PublicKey)
		if err != nil || string(clear) != payload {
			t.Fatal("encrypted smoke payload did not authenticate/decrypt correctly")
		}
	}
	unpaired := makeEnvelope(t, hostID, phoneID, payload)
	host.send(t, unpaired)
	if rejected := host.next(t, matchingType("error"), 10*time.Second); rejected["code"] != "not_paired" {
		t.Fatalf("unpaired traffic not rejected: %v", rejected["code"])
	}
	assertUndelivered := func(envelope map[string]any) {
		barrier(host)
		barrier(phone)
		if phone.has(func(m map[string]any) bool { return m["t"] == "msg" && m["id"] == envelope["id"] }) {
			t.Fatal("rejected envelope reached the recipient")
		}
	}
	assertUndelivered(unpaired)
	t.Log("PASS authenticated handshake and unpaired traffic rejection")
	pairPeers(t, host, phone, phoneID, "token")
	paired := true
	t.Cleanup(func() {
		if paired {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			raw, _ := json.Marshal(map[string]any{"t": "pair.revoke", "peer_id": phoneID.deviceID})
			if err := host.ws.Write(ctx, websocket.MessageText, raw); err != nil {
				t.Logf("best-effort test pairing cleanup failed: %v", err)
				return
			}
			for {
				select {
				case message, ok := <-host.in:
					if !ok || message["t"] == "pair.revoked.ok" {
						return
					}
				case <-ctx.Done():
					t.Log("best-effort test pairing cleanup timed out")
					return
				}
			}
		}
	})
	envelope := makeEnvelope(t, hostID, phoneID, payload)
	host.send(t, envelope)
	message := phone.next(t, func(m map[string]any) bool { return m["t"] == "msg" && m["id"] == envelope["id"] }, 10*time.Second)
	checkPayload(message)
	t.Log("PASS token pairing and encrypted delivery")
	phone.close()
	offline := makeEnvelope(t, hostID, phoneID, payload)
	host.send(t, offline)
	barrier(host)
	phone = connect(phoneID)
	t.Cleanup(phone.close)
	replay := phone.next(t, func(m map[string]any) bool { return m["t"] == "msg" && m["id"] == offline["id"] }, 10*time.Second)
	checkPayload(replay)
	phone.send(t, map[string]any{"t": "ack", "id": offline["id"]})
	barrier(phone)
	phone.close()
	phone = connect(phoneID)
	t.Cleanup(phone.close)
	barrier(phone)
	if phone.has(func(m map[string]any) bool { return m["t"] == "msg" && m["id"] == offline["id"] }) {
		t.Fatal("acknowledged envelope was replayed after a second reconnect")
	}
	t.Log("PASS offline replay, acknowledgement and reconnect")
	host.send(t, map[string]any{"t": "pair.revoke", "peer_id": phoneID.deviceID})
	host.next(t, matchingType("pair.revoked.ok"), 10*time.Second)
	paired = false
	revoked := makeEnvelope(t, hostID, phoneID, payload)
	host.send(t, revoked)
	if rejected := host.next(t, matchingType("error"), 10*time.Second); rejected["code"] != "not_paired" {
		t.Fatalf("revoked peer still usable: %v", rejected["code"])
	}
	assertUndelivered(revoked)
	t.Log("PASS revoked pairing cannot deliver messages")
	pairPeers(t, host, phone, phoneID, "code")
	paired = true
	t.Log("PASS six-digit code pairing")
}
