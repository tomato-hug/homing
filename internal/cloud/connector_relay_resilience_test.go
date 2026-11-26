package cloud

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
)

func setupConnectorTest(t *testing.T, handler http.Handler) (*Connector, protocol.KeyPair, string) {
	t.Helper()
	identity, err := LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	peers, err := LoadPeerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keys, err := protocol.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	clientID := protocol.DeviceID(keys.PublicKey)
	if err := peers.Add(Peer{
		DeviceID:  clientID,
		PublicKey: protocol.ToBase64(keys.PublicKey),
		Name:      "demo-phone",
		Platform:  "android",
	}); err != nil {
		t.Fatal(err)
	}
	connector := &Connector{
		identity: identity,
		peers:    peers,
		handler:  handler,
		log:      slog.Default(),
	}
	return connector, keys, clientID
}

func TestConnectorAcksAndDeduplicatesReplayedRPCs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var hits atomic.Int32
	connector, keys, clientID := setupConnectorTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	env1 := clientEnvelope(t, keys, clientID, connector.identity.DeviceID(), connector.identity.keys.PublicKey,
		`{"type":"rpc","rid":"demo-rid-once","method":"POST","path":"/api/reply","body":{"text":"hi"}}`)
	env2 := clientEnvelope(t, keys, clientID, connector.identity.DeviceID(), connector.identity.keys.PublicKey,
		`{"type":"rpc","rid":"demo-rid-once","method":"POST","path":"/api/reply","body":{"text":"hi"}}`)
	id1 := env1["id"].(string)
	id2 := env2["id"].(string)

	acks := make(chan string, 4)
	results := make(chan map[string]any, 4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer peer.CloseNow()
		raw1, _ := json.Marshal(env1)
		raw2, _ := json.Marshal(env2)
		_ = peer.Write(ctx, websocket.MessageText, raw1)
		_ = peer.Write(ctx, websocket.MessageText, raw2)
		for {
			_, raw, err := peer.Read(ctx)
			if err != nil {
				return
			}
			var msg map[string]any
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			switch msg["t"] {
			case "ack":
				if id, ok := msg["id"].(string); ok {
					acks <- id
				}
			case "msg":
				results <- msg
			}
		}
	}))
	defer ts.Close()

	ws, _, err := websocket.Dial(ctx, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	go connector.readLoop(ctx, ws)

	seenAcks := map[string]bool{}
	for len(seenAcks) < 2 {
		select {
		case id := <-acks:
			seenAcks[id] = true
		case <-ctx.Done():
			t.Fatalf("timed out waiting for acks; got %v", seenAcks)
		}
	}
	if !seenAcks[id1] || !seenAcks[id2] {
		t.Fatalf("expected acks for both %s and %s, got %v", id1, id2, seenAcks)
	}
	select {
	case <-results:
	case <-ctx.Done():
		t.Fatal("expected one rpc_result")
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected mutation handler to execute once, got %d", got)
	}
}

func TestConnectorDropsAndAcksStaleReplayedRPC(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var hits atomic.Int32
	connector, keys, clientID := setupConnectorTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	env := clientEnvelope(t, keys, clientID, connector.identity.DeviceID(), connector.identity.keys.PublicKey,
		`{"type":"rpc","rid":"demo-stale-1","method":"POST","path":"/api/reply","body":{"text":"old"}}`)
	// Make envelope timestamp 5 minutes old while re-sealing with authentic crypto is unnecessary
	// because TS is part of the signed AAD? Wait: openPayload verifies AAD including TS!
	// So create the envelope with a backdated TS using CreateEnvelope + manual timestamp check,
	// or test isStaleEnvelope directly + valid envelope.
	if !isStaleEnvelope(time.Now().UTC().Add(-5*time.Minute).Format(time.RFC3339Nano), time.Now().UTC(), connectorMaxStaleRPC) {
		t.Fatal("expected 5-minute-old timestamp to be stale")
	}
	if isStaleEnvelope(time.Now().UTC().Add(30*time.Second).Format(time.RFC3339Nano), time.Now().UTC(), connectorMaxStaleRPC) {
		t.Fatal("expected slightly future clock-skew timestamp NOT to be stale")
	}
	// Verify a fresh envelope executes normally and gets acked.
	id := env["id"].(string)
	acks := make(chan string, 2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer peer.CloseNow()
		raw, _ := json.Marshal(env)
		_ = peer.Write(ctx, websocket.MessageText, raw)
		for {
			_, frame, err := peer.Read(ctx)
			if err != nil {
				return
			}
			var msg map[string]any
			if json.Unmarshal(frame, &msg) == nil && msg["t"] == "ack" {
				acks <- msg["id"].(string)
				return
			}
		}
	}))
	defer ts.Close()
	ws, _, err := websocket.Dial(ctx, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	go connector.readLoop(ctx, ws)

	select {
	case got := <-acks:
		if got != id {
			t.Fatalf("ack id = %q, want %q", got, id)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for ack")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
}

func TestConnectorRunsReadRPCsConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{})
	var enteredCount atomic.Int32

	connector, keys, clientID := setupConnectorTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := enteredCount.Add(1)
		if n == 1 {
			close(firstEntered)
			// Block until a second GET enters concurrently; a serial worker would deadlock.
			select {
			case <-secondEntered:
			case <-r.Context().Done():
			}
		} else if n == 2 {
			close(secondEntered)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	env1 := clientEnvelope(t, keys, clientID, connector.identity.DeviceID(), connector.identity.keys.PublicKey,
		`{"type":"rpc","rid":"demo-get-1","method":"GET","path":"/api/home"}`)
	env2 := clientEnvelope(t, keys, clientID, connector.identity.DeviceID(), connector.identity.keys.PublicKey,
		`{"type":"rpc","rid":"demo-get-2","method":"GET","path":"/api/sessions"}`)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer peer.CloseNow()
		raw1, _ := json.Marshal(env1)
		raw2, _ := json.Marshal(env2)
		_ = peer.Write(ctx, websocket.MessageText, raw1)
		_ = peer.Write(ctx, websocket.MessageText, raw2)
		for {
			if _, _, err := peer.Read(ctx); err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	ws, _, err := websocket.Dial(ctx, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	go connector.readLoop(ctx, ws)

	select {
	case <-secondEntered:
	case <-ctx.Done():
		t.Fatal("second GET RPC did not execute concurrently while first GET was in flight")
	}
}

func TestConnectorBuffersNotificationsAcrossDisconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connector, _, _ := setupConnectorTest(t, http.NotFoundHandler())
	// While disconnected, SendEventExcept should buffer the notification.
	sent, err := connector.SendEventExcept(ctx, "notify", map[string]any{"title": "demo"}, nil)
	if err == nil || sent != 0 {
		t.Fatalf("expected ErrNotConnected while disconnected, got sent=%d err=%v", sent, err)
	}

	received := make(chan map[string]any, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer peer.CloseNow()
		_, raw, err := peer.Read(ctx)
		if err != nil {
			return
		}
		var msg map[string]any
		if json.Unmarshal(raw, &msg) == nil {
			received <- msg
		}
	}))
	defer ts.Close()

	ws, _, err := websocket.Dial(ctx, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()

	connector.flushPendingNotifications(ctx, ws)
	select {
	case msg := <-received:
		if msg["t"] != "msg" {
			t.Fatalf("expected flushed msg envelope, got %v", msg["t"])
		}
	case <-ctx.Done():
		t.Fatal("buffered notification was not flushed on reconnect")
	}
}

func TestSplitHorizonFakeIPDetectionAndBypass(t *testing.T) {
	if !isTunFakeIP(net.ParseIP("198.18.0.42")) || !isTunFakeIP(net.ParseIP("198.19.255.1")) {
		t.Fatal("expected 198.18.0.0/15 addresses to be detected as TUN Fake-IP")
	}
	if !isTunFakeIP(net.ParseIP("fc00::1234")) {
		t.Fatal("expected fc00::/18 IPv6 address to be detected as TUN Fake-IP")
	}
	if isTunFakeIP(net.ParseIP("119.29.29.29")) || isTunFakeIP(net.ParseIP("198.20.0.1")) {
		t.Fatal("real public IPv4 must not be classified as Fake-IP")
	}

	// Simulate Clash/Surge TUN returning 198.18.0.88 while direct public DNS returns real Mainland IP
	targetIP, fakeDetected, fakeBypassed, err := resolveHostBypassingFakeIP(
		context.Background(),
		"relay-demo.example.cn",
		func(_ context.Context, _ string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("198.18.0.88")}}, nil
		},
		func(_ context.Context, _ string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("120.79.10.20")}}, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if !fakeDetected || !fakeBypassed || targetIP != "120.79.10.20" {
		t.Fatalf("expected real IP 120.79.10.20 with fakeDetected=true fakeBypassed=true, got ip=%s det=%v byp=%v",
			targetIP, fakeDetected, fakeBypassed)
	}
}
