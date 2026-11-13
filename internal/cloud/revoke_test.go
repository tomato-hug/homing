package cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/server"
)

func TestRevokePeerDisconnectsLANAndRejectsCloudRPC(t *testing.T) {
	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	dir := t.TempDir()
	hub, err := NewHub(dir, handler, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hub.Close)
	relay, err := server.New(server.Config{Addr: "127.0.0.1:0", DBPath: ":memory:", Logger: testLogger()})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := relay.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	relayURL := "ws://" + listener.Addr().String() + "/v1/ws"
	connector, err := NewConnector(ConnectorConfig{
		ServerURL: relayURL, Identity: hub.Identity(), Peers: hub.Peers(), Handler: handler, Logger: testLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go connector.Run(ctx)
	eventually(t, 3*time.Second, connector.Connected, "demo connector should connect")
	invite, err := connector.StartPairing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	phone := dialPeer(t, relayURL, clientSeed)
	phone.hello(t)
	phone.send(t, map[string]any{"t": "pair.join", "token": invite.Token, "pub": phone.publicKey, "name": "demo-phone", "platform": "android"})
	phone.next(t, func(m map[string]any) bool { return m["t"] == "pair.waiting" }, 2*time.Second)
	select {
	case request := <-connector.PairRequests():
		if err := connector.Accept(ctx, request.ClientID); err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("demo pairing request not received")
	}
	phone.next(t, func(m map[string]any) bool { return m["t"] == "pair.accepted" }, 2*time.Second)
	hostPub := hub.Identity().Keys().PublicKey
	payload := `{"type":"rpc","rid":"demo-rpc-1","method":"GET","path":"/api/demo"}`
	envelope := clientEnvelope(t, phone.keys, phone.deviceID, hub.Identity().DeviceID(), hostPub, payload)
	phone.send(t, envelope)
	response := phone.next(t, func(m map[string]any) bool { return m["t"] == "msg" }, 2*time.Second)
	if !strings.Contains(string(openClientEnvelope(t, response, phone.keys, hostPub)), `"status":200`) {
		t.Fatal("demo cloud RPC did not succeed before revocation")
	}

	lanServer := httptest.NewServer(hub)
	t.Cleanup(lanServer.Close)
	lanPhone := dialPeer(t, "ws"+strings.TrimPrefix(lanServer.URL, "http"), clientSeed)
	lanPhone.hello(t)
	lanPhone.send(t, envelope)
	lanPhone.next(t, func(m map[string]any) bool { return m["t"] == "msg" }, 2*time.Second)
	if calls.Load() != 2 {
		t.Fatalf("demo handler calls before revoke = %d, want 2", calls.Load())
	}
	if err := hub.RevokePeer(phone.deviceID); err != nil {
		t.Fatal(err)
	}
	lanPhone.next(t, func(m map[string]any) bool { return m["t"] == "pair.revoked" }, 2*time.Second)
	select {
	case _, open := <-lanPhone.in:
		if open {
			t.Fatal("LAN socket remained open after revocation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("LAN socket was not closed after revocation")
	}
	eventually(t, time.Second, func() bool { return !hub.OnlinePeers()[phone.deviceID] }, "revoked phone must not appear online")
	// The connector shares the peer store, so rejection precedes HTTP dispatch.
	if err := connector.handleEnvelope(ctx, nil, envelope); err == nil {
		t.Fatal("revoked cloud RPC was accepted")
	}
	reloaded, err := LoadPeerStore(filepath.Join(dir, "cloud"))
	if err != nil {
		t.Fatal(err)
	}
	if _, found := reloaded.Get(phone.deviceID); found {
		t.Fatal("revoked pairing returned after reload")
	}
	restarted, err := NewConnector(ConnectorConfig{
		ServerURL: relayURL, Identity: hub.Identity(), Peers: reloaded, Handler: handler, Logger: testLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	if err := restarted.handleEnvelope(ctx, nil, envelope); err == nil || calls.Load() != 2 {
		t.Fatal("revoked RPC reached the business handler after reload")
	}
}
