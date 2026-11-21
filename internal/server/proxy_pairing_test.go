package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
)

func connectForwardedPeer(t *testing.T, url, forwardedFor string, identity testIdentity) *testPeer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-Forwarded-For": []string{forwardedFor}},
	})
	if err != nil {
		t.Fatal(err)
	}
	peer := newTestPeer(ws)
	t.Cleanup(peer.close)
	challenge := peer.next(t, func(m map[string]any) bool { return m["t"] == "challenge" }, 2*time.Second)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	sig, err := protocol.HelloSignature(identity.keys.PrivateKey, stringField(challenge, "nonce"), ts)
	if err != nil {
		t.Fatal(err)
	}
	peer.send(t, map[string]any{
		"t": "hello", "device_id": identity.deviceID, "pub": identity.pub,
		"ts": ts, "sig": protocol.ToBase64(sig),
	})
	result := peer.next(t, func(m map[string]any) bool {
		return m["t"] == "hello.ok" || m["t"] == "error"
	}, 2*time.Second)
	if result["t"] != "hello.ok" {
		t.Fatalf("hello failed: %v", result["code"])
	}
	return peer
}

func TestPairCodeLimitSeparatesClientsBehindProxy(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	host := connectPeer(t, fixture.url, identityFromSeed(t, hostSeed))
	t.Cleanup(host.close)
	host.send(t, map[string]any{"t": "pair.start"})
	invite := host.next(t, func(m map[string]any) bool { return m["t"] == "pair.start.ok" }, 2*time.Second)
	badIdentity := identityFromSeed(t, clientSeed)
	bad := connectForwardedPeer(t, fixture.url, "198.51.100.10", badIdentity)
	wrongCode := "000000"
	if wrongCode == stringField(invite, "code6") {
		wrongCode = "999999"
	}
	for attempt := 0; attempt < 5; attempt++ {
		bad.send(t, map[string]any{"t": "pair.join", "code6": wrongCode, "pub": badIdentity.pub})
		result := bad.next(t, func(m map[string]any) bool { return m["t"] == "error" }, 2*time.Second)
		if attempt == 4 && result["code"] != "too_many_attempts" {
			t.Fatalf("fifth invalid code must be limited, got %v", result["code"])
		}
	}
	// A changed, attacker-supplied leftmost value must not evade the limit
	// attached to the real address appended by the trusted proxy.
	spoofed := connectForwardedPeer(t, fixture.url, "203.0.113.99, 198.51.100.10", badIdentity)
	spoofed.send(t, map[string]any{"t": "pair.join", "code6": invite["code6"], "pub": badIdentity.pub})
	blocked := spoofed.next(t, func(m map[string]any) bool {
		return m["t"] == "error" || m["t"] == "pair.waiting"
	}, 2*time.Second)
	if blocked["code"] != "too_many_attempts" {
		t.Fatalf("spoofed prefix bypassed the source limit: %v", blocked["t"])
	}
	goodIdentity := identityFromSeed(t, "homing-test-client-seed-00000002")
	good := connectForwardedPeer(t, fixture.url, "198.51.100.11", goodIdentity)
	good.send(t, map[string]any{"t": "pair.join", "code6": invite["code6"], "pub": goodIdentity.pub})
	result := good.next(t, func(m map[string]any) bool {
		return m["t"] == "pair.waiting" || m["t"] == "error"
	}, 2*time.Second)
	if result["t"] != "pair.waiting" {
		t.Fatalf("another proxy client was incorrectly blocked: %v", result["code"])
	}
}

func TestReplacingInvitesRejectsOldTokenAndKeepsNewest(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	host := connectPeer(t, fixture.url, identityFromSeed(t, hostSeed))
	t.Cleanup(host.close)
	var invites []map[string]any
	for i := 0; i < 4; i++ {
		host.send(t, map[string]any{"t": "pair.start"})
		invites = append(invites, host.next(t, func(m map[string]any) bool {
			return m["t"] == "pair.start.ok"
		}, 2*time.Second))
	}
	clientIdentity := identityFromSeed(t, clientSeed)
	client := connectPeer(t, fixture.url, clientIdentity)
	t.Cleanup(client.close)
	client.send(t, map[string]any{"t": "pair.join", "token": invites[0]["pair_token"], "pub": clientIdentity.pub})
	old := client.next(t, func(m map[string]any) bool {
		return m["t"] == "error" || m["t"] == "pair.waiting"
	}, 2*time.Second)
	if old["code"] != "invalid_pair_token" {
		t.Fatalf("superseded invitation still usable: %v", old["t"])
	}
	client.send(t, map[string]any{"t": "pair.join", "token": invites[3]["pair_token"], "pub": clientIdentity.pub})
	latest := client.next(t, func(m map[string]any) bool {
		return m["t"] == "pair.waiting" || m["t"] == "error"
	}, 2*time.Second)
	if latest["t"] != "pair.waiting" {
		t.Fatalf("newest invitation rejected: %v", latest["code"])
	}
}

func TestForwardedAddressKeepsEveryTrustBoundary(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	for _, tc := range []struct {
		name   string
		values []string
		want   string
	}{
		{"separate header lines", []string{"203.0.113.99", "198.51.100.10"}, "198.51.100.10"},
		{"invalid nearest hop", []string{"203.0.113.99, unknown"}, "127.0.0.1"},
		{"invalid intermediate hop", []string{"203.0.113.99, unknown, 127.0.0.2"}, "127.0.0.1"},
		{"empty nearest hop", []string{"203.0.113.99,"}, "127.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Request{RemoteAddr: "127.0.0.1:1234", Header: http.Header{
				"X-Forwarded-For": tc.values, "X-Real-Ip": []string{"203.0.113.98"},
			}}
			if got := fixture.server.clientAddress(r); got != tc.want {
				t.Fatalf("source = %q, want %q", got, tc.want)
			}
		})
	}
}
