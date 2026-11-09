package cloud

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/notify"
	"github.com/tomato-hug/homing/internal/server"
)

type recordingWaker struct {
	mu    sync.Mutex
	wakes []string
	seen  chan string
}

func newRecordingWaker() *recordingWaker {
	return &recordingWaker{seen: make(chan string, 4)}
}

func (w *recordingWaker) Wake(_ context.Context, deviceID string) server.WakeResult {
	w.mu.Lock()
	w.wakes = append(w.wakes, deviceID)
	w.mu.Unlock()
	select {
	case w.seen <- deviceID:
	default:
	}
	return server.WakeResult{}
}

func demoNotification(level, sessionID string) notify.Payload {
	return notify.Payload{
		Type:      "notify",
		Level:     level,
		Source:    "codex",
		SessionID: sessionID,
		EventID:   42,
		Title:     "示例：修复登录页",
		Summary:   "示例摘要，不是真实会话内容",
		At:        "2026-11-09T08:00:00.000Z",
	}
}

func TestDeliverNotificationLANOnline(t *testing.T) {
	dir := t.TempDir()
	hub, err := NewHub(dir, testMux(), testLogger())
	if err != nil {
		t.Fatalf("hub: %v", err)
	}
	t.Cleanup(hub.Close)
	httpServer := httptest.NewServer(hub)
	t.Cleanup(httpServer.Close)
	wsURL := "ws://" + strings.TrimPrefix(httpServer.URL, "http://") + "/"
	hub.SetServerURL(wsURL)

	invite, err := hub.BeginPairing()
	if err != nil {
		t.Fatalf("begin pairing: %v", err)
	}
	client := dialPeer(t, wsURL, clientSeed)
	client.hello(t)
	client.send(t, map[string]any{
		"t":        "pair.join",
		"token":    invite.Token,
		"pub":      client.publicKey,
		"name":     "演示手机",
		"platform": "android",
	})
	waiting := client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.waiting"
	}, 2*time.Second)
	hostPub := decodeHostPublicKey(t, waiting["host_pub"])
	if err := hub.Approve(client.deviceID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.accepted"
	}, 2*time.Second)

	payload := demoNotification("P0", "demo-session-lan")
	delivered, err := deliverNotification(context.Background(), hub, nil, payload)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered = %d, want 1", delivered)
	}
	message := client.next(t, func(message map[string]any) bool {
		return message["t"] == "msg"
	}, 2*time.Second)
	plaintext := openClientEnvelope(t, message, client.keys, hostPub)
	var event struct {
		Type string         `json:"type"`
		Name string         `json:"name"`
		Data notify.Payload `json:"data"`
	}
	if err := json.Unmarshal(plaintext, &event); err != nil {
		t.Fatalf("event: %v", err)
	}
	if event.Type != "event" || event.Name != "notify" {
		t.Fatalf("event = %#v", event)
	}
	if event.Data.Level != "P0" || event.Data.SessionID != "demo-session-lan" ||
		event.Data.Title != payload.Title {
		t.Fatalf("notify payload = %#v", event.Data)
	}
}

func TestDeliverNotificationCloudQueuesOfflineAndWakes(t *testing.T) {
	waker := newRecordingWaker()
	relay, err := server.New(server.Config{
		Addr:   "127.0.0.1:0",
		DBPath: ":memory:",
		Logger: testLogger(),
		Waker:  waker,
	})
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	listener, err := relay.Start()
	if err != nil {
		t.Fatalf("start relay: %v", err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	relayURL := "ws://" + listener.Addr().String() + "/v1/ws"

	dir := filepath.Join(t.TempDir(), "cloud")
	identity, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	peers, err := LoadPeerStore(dir)
	if err != nil {
		t.Fatalf("peers: %v", err)
	}
	connector, err := NewConnector(ConnectorConfig{
		ServerURL: relayURL,
		Identity:  identity,
		Peers:     peers,
		Handler:   testMux(),
		Logger:    testLogger(),
	})
	if err != nil {
		t.Fatalf("connector: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go connector.Run(ctx)
	eventually(t, 5*time.Second, connector.Connected, "connector should connect")

	invite, err := connector.StartPairing(ctx)
	if err != nil {
		t.Fatalf("start pairing: %v", err)
	}
	client := dialPeer(t, relayURL, secondSeed)
	client.hello(t)
	client.send(t, map[string]any{
		"t":        "pair.join",
		"token":    invite.Token,
		"pub":      client.publicKey,
		"name":     "演示手机",
		"platform": "android",
	})
	client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.waiting"
	}, 2*time.Second)
	var request PairingRequest
	select {
	case request = <-connector.PairRequests():
	case <-time.After(3 * time.Second):
		t.Fatal("connector did not receive the pairing request")
	}
	if err := connector.Accept(ctx, request.ClientID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.accepted"
	}, 3*time.Second)

	client.close()
	eventually(t, 3*time.Second, func() bool {
		return !relay.IsOnline(client.deviceID)
	}, "phone should be offline")

	payload := demoNotification("P1", "demo-session-cloud")
	delivered, err := deliverNotification(ctx, nil, connector, payload)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if delivered != 1 {
		t.Fatalf("delivered = %d, want 1", delivered)
	}
	select {
	case deviceID := <-waker.seen:
		if deviceID != client.deviceID {
			t.Fatalf("wake device = %q, want %q", deviceID, client.deviceID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("offline delivery did not wake the phone")
	}
	eventually(t, 3*time.Second, func() bool {
		count, err := relay.DB().QueueCount(context.Background(), client.deviceID)
		return err == nil && count == 1
	}, "notification envelope should be queued")

	reconnected := dialPeer(t, relayURL, secondSeed)
	reconnected.hello(t)
	message := reconnected.next(t, func(message map[string]any) bool {
		return message["t"] == "msg"
	}, 3*time.Second)
	plaintext := openClientEnvelope(t, message, reconnected.keys,
		identity.Keys().PublicKey)
	var event struct {
		Type string         `json:"type"`
		Name string         `json:"name"`
		Data notify.Payload `json:"data"`
	}
	if err := json.Unmarshal(plaintext, &event); err != nil {
		t.Fatalf("event: %v", err)
	}
	if event.Name != "notify" || event.Data.SessionID != "demo-session-cloud" {
		t.Fatalf("event = %#v", event)
	}
	reconnected.send(t, map[string]any{"t": "ack", "id": stringField(message, "id")})
	eventually(t, 3*time.Second, func() bool {
		count, err := relay.DB().QueueCount(context.Background(), client.deviceID)
		return err == nil && count == 0
	}, "ack should remove the queued envelope")
}
