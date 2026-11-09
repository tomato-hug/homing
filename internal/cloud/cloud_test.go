package cloud

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
	"github.com/tomato-hug/homing/internal/server"
)

const (
	clientSeed = "homing-test-client-seed-00000001"
	secondSeed = "homing-test-client-seed-00000002"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type wsPeer struct {
	ws        *websocket.Conn
	in        chan map[string]any
	stash     []map[string]any
	nonce     string
	keys      protocol.KeyPair
	deviceID  string
	publicKey string
	once      sync.Once
}

func dialPeer(t *testing.T, url string, seed string) *wsPeer {
	t.Helper()
	keys, err := protocol.KeyPairFromSeed([]byte(seed))
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	peer := &wsPeer{
		ws:        ws,
		in:        make(chan map[string]any, 64),
		keys:      keys,
		deviceID:  protocol.DeviceID(keys.PublicKey),
		publicKey: protocol.ToBase64(keys.PublicKey),
	}
	go func() {
		defer close(peer.in)
		for {
			_, raw, err := ws.Read(context.Background())
			if err != nil {
				return
			}
			var message map[string]any
			if err := json.Unmarshal(raw, &message); err != nil {
				continue
			}
			peer.in <- message
		}
	}()
	t.Cleanup(peer.close)
	challenge := peer.next(t, func(message map[string]any) bool {
		return message["t"] == "challenge"
	}, 2*time.Second)
	peer.nonce = stringField(challenge, "nonce")
	return peer
}

func (p *wsPeer) hello(t *testing.T) {
	t.Helper()
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	signature, err := protocol.HelloSignature(p.keys.PrivateKey, p.nonce, ts)
	if err != nil {
		t.Fatalf("sign hello: %v", err)
	}
	p.send(t, map[string]any{
		"t":         "hello",
		"device_id": p.deviceID,
		"pub":       p.publicKey,
		"ts":        ts,
		"sig":       protocol.ToBase64(signature),
	})
	answer := p.next(t, func(message map[string]any) bool {
		return message["t"] == "hello.ok" || message["t"] == "error"
	}, 2*time.Second)
	if answer["t"] == "error" {
		t.Fatalf("hello rejected: %v: %v", answer["code"], answer["message"])
	}
}

func (p *wsPeer) send(t *testing.T, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.ws.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func (p *wsPeer) next(t *testing.T, predicate func(map[string]any) bool, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for index, message := range p.stash {
			if predicate(message) {
				p.stash = append(p.stash[:index], p.stash[index+1:]...)
				return message
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("timed out waiting for websocket message")
		}
		timer := time.NewTimer(remaining)
		select {
		case message, ok := <-p.in:
			timer.Stop()
			if !ok {
				t.Fatalf("websocket closed while waiting")
			}
			if predicate(message) {
				return message
			}
			p.stash = append(p.stash, message)
		case <-timer.C:
			t.Fatalf("timed out waiting for websocket message")
		}
	}
}

func (p *wsPeer) close() {
	p.once.Do(func() { _ = p.ws.Close(websocket.StatusNormalClosure, "test done") })
}

func testMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"impl":"test"}`))
	})
	mux.HandleFunc("POST /api/echo", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "body": body})
	})
	return mux
}

func clientEnvelope(t *testing.T, from protocol.KeyPair, fromID string, toID string, hostPub ed25519.PublicKey, payload string) map[string]any {
	t.Helper()
	envelope, err := protocol.CreateEnvelope(fromID, toID, []byte(payload), from, hostPub)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	return envelopeMap(envelope)
}

func openClientEnvelope(t *testing.T, message map[string]any, keys protocol.KeyPair, hostPub ed25519.PublicKey) []byte {
	t.Helper()
	envelope, ok := envelopeFromMap(message)
	if !ok {
		t.Fatalf("not an envelope: %#v", message)
	}
	plaintext, err := protocol.OpenEnvelope(envelope, keys, hostPub)
	if err != nil {
		t.Fatalf("open envelope: %v", err)
	}
	return plaintext
}

func decodeHostPublicKey(t *testing.T, value any) ed25519.PublicKey {
	t.Helper()
	raw, err := protocol.FromBase64(value.(string))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		t.Fatalf("host public key: %v", err)
	}
	return ed25519.PublicKey(raw)
}

func TestIdentityPersists(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	second, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("reload identity: %v", err)
	}
	if first.DeviceID() != second.DeviceID() {
		t.Fatalf("identity changed across loads: %q vs %q", first.DeviceID(), second.DeviceID())
	}
	info, err := os.Stat(filepath.Join(dir, "identity.json"))
	if err != nil {
		t.Fatalf("stat identity: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("identity file mode = %o, want 600", mode)
	}
	fingerprint := first.Fingerprint()
	if len(fingerprint) != 16 {
		t.Fatalf("fingerprint = %q, want 16 hex chars", fingerprint)
	}
}

func TestPeerStorePersists(t *testing.T) {
	dir := t.TempDir()
	store, err := LoadPeerStore(dir)
	if err != nil {
		t.Fatalf("load peers: %v", err)
	}
	peer := Peer{
		DeviceID:  "demo-device-1",
		PublicKey: "ZGVtby1wdWJsaWMta2V5",
		Name:      "测试手机",
		Platform:  "android",
	}
	if err := store.Add(peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	if err := store.SetServer("wss://relay.example.test/v1/ws"); err != nil {
		t.Fatalf("set server: %v", err)
	}
	reloaded, err := LoadPeerStore(dir)
	if err != nil {
		t.Fatalf("reload peers: %v", err)
	}
	got, ok := reloaded.Get(peer.DeviceID)
	if !ok || got.PublicKey != peer.PublicKey {
		t.Fatalf("reloaded peer = %#v, %v", got, ok)
	}
	if reloaded.Server() != "wss://relay.example.test/v1/ws" {
		t.Fatalf("server = %q", reloaded.Server())
	}
	if err := reloaded.Remove(peer.DeviceID); err != nil {
		t.Fatalf("remove peer: %v", err)
	}
	if _, ok := reloaded.Get(peer.DeviceID); ok {
		t.Fatal("peer still present after remove")
	}
}

func TestLANHubPairingRPCAndEvent(t *testing.T) {
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
	if len(invite.Token) != 32 || len(invite.Code6) != 6 {
		t.Fatalf("unexpected invite: %#v", invite)
	}
	parsed, err := protocol.ParsePairURI(invite.URI)
	if err != nil || parsed.Token != invite.Token || parsed.HostID != invite.HostID {
		t.Fatalf("pair uri = %#v, %v", parsed, err)
	}

	client := dialPeer(t, wsURL, clientSeed)
	client.hello(t)
	client.send(t, map[string]any{
		"t":        "pair.join",
		"token":    invite.Token,
		"pub":      client.publicKey,
		"name":     "测试手机",
		"platform": "android",
	})
	waiting := client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.waiting"
	}, 2*time.Second)
	hostPub := decodeHostPublicKey(t, waiting["host_pub"])
	if !protocol.VerifyHostFingerprint(hostPub, stringField(waiting, "host_fingerprint")) {
		t.Fatal("host fingerprint in pair.waiting does not verify")
	}
	if protocol.VerifyHostFingerprint(hostPub, "0000000000000000") {
		t.Fatal("wrong fingerprint accepted")
	}

	var request PairingRequest
	select {
	case request = <-hub.Requests():
	case <-time.After(2 * time.Second):
		t.Fatal("hub did not surface the pairing request")
	}
	if request.ClientID != client.deviceID {
		t.Fatalf("request client id = %q, want %q", request.ClientID, client.deviceID)
	}
	expectedSAS, err := protocol.SASWords(hub.Identity().Keys().PublicKey, client.keys.PublicKey)
	if err != nil {
		t.Fatalf("sas: %v", err)
	}
	if request.SAS != expectedSAS {
		t.Fatalf("SAS = %v, want %v", request.SAS, expectedSAS)
	}

	if err := hub.Approve(client.deviceID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.accepted"
	}, 2*time.Second)

	rpcPayload := `{"type":"rpc","rid":"r1","method":"GET","path":"/api/ping"}`
	client.send(t, clientEnvelope(t, client.keys, client.deviceID, hub.Identity().DeviceID(), hostPub, rpcPayload))
	response := client.next(t, func(message map[string]any) bool {
		return message["t"] == "msg"
	}, 2*time.Second)
	plaintext := openClientEnvelope(t, response, client.keys, hostPub)
	var result struct {
		Type   string         `json:"type"`
		RID    string         `json:"rid"`
		Status int            `json:"status"`
		Body   map[string]any `json:"body"`
	}
	if err := json.Unmarshal(plaintext, &result); err != nil {
		t.Fatalf("rpc result: %v", err)
	}
	if result.Type != "rpc_result" || result.RID != "r1" || result.Status != 200 || result.Body["ok"] != true {
		t.Fatalf("rpc result = %#v", result)
	}

	if _, err := hub.SendEvent(context.Background(), "message", map[string]any{"text": "示例消息"}); err != nil {
		t.Fatalf("send event: %v", err)
	}
	eventMessage := client.next(t, func(message map[string]any) bool {
		return message["t"] == "msg"
	}, 2*time.Second)
	eventPlaintext := openClientEnvelope(t, eventMessage, client.keys, hostPub)
	var event struct {
		Type string         `json:"type"`
		Name string         `json:"name"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(eventPlaintext, &event); err != nil {
		t.Fatalf("event: %v", err)
	}
	if event.Type != "event" || event.Name != "message" || event.Data["text"] != "示例消息" {
		t.Fatalf("event = %#v", event)
	}
}

func TestLANHubRejectsBadToken(t *testing.T) {
	dir := t.TempDir()
	hub, err := NewHub(dir, testMux(), testLogger())
	if err != nil {
		t.Fatalf("hub: %v", err)
	}
	t.Cleanup(hub.Close)
	httpServer := httptest.NewServer(hub)
	t.Cleanup(httpServer.Close)
	wsURL := "ws://" + strings.TrimPrefix(httpServer.URL, "http://") + "/"

	client := dialPeer(t, wsURL, clientSeed)
	client.hello(t)
	client.send(t, map[string]any{
		"t":     "pair.join",
		"token": "not-a-real-token",
		"pub":   client.publicKey,
	})
	rejected := client.next(t, func(message map[string]any) bool {
		return message["t"] == "error"
	}, 2*time.Second)
	if got := stringField(rejected, "code"); got != "invalid_pair_token" {
		t.Fatalf("error code = %q, want invalid_pair_token", got)
	}
	if len(hub.Pending()) != 0 {
		t.Fatal("a bad token created a pending pairing")
	}
}

func TestRPCForbidsNonAPIPaths(t *testing.T) {
	result := dispatchRPC(context.Background(), testMux(),
		[]byte(`{"type":"rpc","rid":"r9","method":"GET","path":"/console"}`))
	var decoded rpcResult
	if err := json.Unmarshal(result, &decoded); err != nil {
		t.Fatalf("result: %v", err)
	}
	if decoded.Status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", decoded.Status)
	}
}

func TestConnectorPairsThroughRelayAndRunsRPC(t *testing.T) {
	srv, err := server.New(server.Config{
		Addr:   "127.0.0.1:0",
		DBPath: ":memory:",
		Logger: testLogger(),
	})
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	listener, err := srv.Start()
	if err != nil {
		t.Fatalf("start relay: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
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
	if invite.Token == "" || invite.Code6 == "" || !strings.Contains(invite.URI, "homing://pair?") {
		t.Fatalf("invite = %#v", invite)
	}

	client := dialPeer(t, relayURL, secondSeed)
	client.hello(t)
	client.send(t, map[string]any{
		"t":        "pair.join",
		"token":    invite.Token,
		"pub":      client.publicKey,
		"name":     "测试手机",
		"platform": "android",
	})
	waiting := client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.waiting"
	}, 2*time.Second)
	hostPub := decodeHostPublicKey(t, waiting["host_pub"])

	var request PairingRequest
	select {
	case request = <-connector.PairRequests():
	case <-time.After(3 * time.Second):
		t.Fatal("connector did not receive the pairing request")
	}
	if request.ClientID != client.deviceID {
		t.Fatalf("request client = %q, want %q", request.ClientID, client.deviceID)
	}
	expectedSAS, err := protocol.SASWords(identity.Keys().PublicKey, client.keys.PublicKey)
	if err != nil {
		t.Fatalf("sas: %v", err)
	}
	if request.SAS != expectedSAS {
		t.Fatalf("SAS = %v, want %v", request.SAS, expectedSAS)
	}

	if err := connector.Accept(ctx, request.ClientID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.accepted"
	}, 3*time.Second)

	client.send(t, clientEnvelope(t, client.keys, client.deviceID, identity.DeviceID(), hostPub,
		`{"type":"rpc","rid":"r2","method":"POST","path":"/api/echo","body":{"text":"你好"}}`))
	response := client.next(t, func(message map[string]any) bool {
		return message["t"] == "msg"
	}, 3*time.Second)
	plaintext := openClientEnvelope(t, response, client.keys, hostPub)
	var result struct {
		Type   string `json:"type"`
		RID    string `json:"rid"`
		Status int    `json:"status"`
		Body   struct {
			OK   bool `json:"ok"`
			Body struct {
				Text string `json:"text"`
			} `json:"body"`
		} `json:"body"`
	}
	if err := json.Unmarshal(plaintext, &result); err != nil {
		t.Fatalf("rpc result: %v", err)
	}
	if result.Type != "rpc_result" || result.RID != "r2" || result.Status != 200 ||
		!result.Body.OK || result.Body.Body.Text != "你好" {
		t.Fatalf("rpc result = %#v", result)
	}

	if err := connector.SendEvent(ctx, "message", map[string]any{"text": "云端事件"}); err != nil {
		t.Fatalf("send event: %v", err)
	}
	eventMessage := client.next(t, func(message map[string]any) bool {
		return message["t"] == "msg"
	}, 3*time.Second)
	eventPlaintext := openClientEnvelope(t, eventMessage, client.keys, hostPub)
	var event struct {
		Type string         `json:"type"`
		Name string         `json:"name"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(eventPlaintext, &event); err != nil {
		t.Fatalf("event: %v", err)
	}
	if event.Type != "event" || event.Name != "message" || event.Data["text"] != "云端事件" {
		t.Fatalf("event = %#v", event)
	}

	reloaded, err := LoadPeerStore(dir)
	if err != nil {
		t.Fatalf("reload peers: %v", err)
	}
	if _, ok := reloaded.Get(client.deviceID); !ok {
		t.Fatal("accepted peer was not persisted")
	}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", message)
}
