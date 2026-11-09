package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
)

const (
	hostSeed   = "homing-test-host-seed-0000000001"
	clientSeed = "homing-test-client-seed-00000001"
)

type testPeer struct {
	ws    *websocket.Conn
	in    chan map[string]any
	stash []map[string]any
	done  chan struct{}
	once  sync.Once
}

func newTestPeer(ws *websocket.Conn) *testPeer {
	peer := &testPeer{
		ws:   ws,
		in:   make(chan map[string]any, 128),
		done: make(chan struct{}),
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
			select {
			case peer.in <- message:
			case <-peer.done:
				return
			}
		}
	}()
	return peer
}

func (p *testPeer) send(t *testing.T, value any) {
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

func (p *testPeer) next(t *testing.T, predicate func(map[string]any) bool, timeout time.Duration) map[string]any {
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

func (p *testPeer) has(predicate func(map[string]any) bool) bool {
	for _, message := range p.stash {
		if predicate(message) {
			return true
		}
	}
	return false
}

func (p *testPeer) close() {
	p.once.Do(func() {
		close(p.done)
		_ = p.ws.Close(websocket.StatusNormalClosure, "test done")
	})
}

type testIdentity struct {
	keys     protocol.KeyPair
	pub      string
	deviceID string
}

func identityFromSeed(t *testing.T, seed string) testIdentity {
	t.Helper()
	keys, err := protocol.KeyPairFromSeed([]byte(seed))
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	return testIdentity{
		keys:     keys,
		pub:      protocol.ToBase64(keys.PublicKey),
		deviceID: protocol.DeviceID(keys.PublicKey),
	}
}

type serverFixture struct {
	server *Server
	url    string
}

func newServerFixture(t *testing.T, dbPath string, waker Waker) serverFixture {
	t.Helper()
	if dbPath == "" {
		dbPath = ":memory:"
	}
	srv, err := New(Config{
		Addr:   "127.0.0.1:0",
		DBPath: dbPath,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Waker:  waker,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	listener, err := srv.Start()
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return serverFixture{
		server: srv,
		url:    "ws://" + listener.Addr().String() + "/v1/ws",
	}
}

func connectPeer(t *testing.T, url string, identity testIdentity) *testPeer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	peer := newTestPeer(ws)
	challenge := peer.next(t, func(message map[string]any) bool {
		return message["t"] == "challenge"
	}, 2*time.Second)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	signature, err := protocol.HelloSignature(identity.keys.PrivateKey, stringField(challenge, "nonce"), ts)
	if err != nil {
		t.Fatalf("hello signature: %v", err)
	}
	peer.send(t, map[string]any{
		"t":         "hello",
		"device_id": identity.deviceID,
		"pub":       identity.pub,
		"ts":        ts,
		"sig":       protocol.ToBase64(signature),
	})
	answer := peer.next(t, func(message map[string]any) bool {
		return message["t"] == "hello.ok" || message["t"] == "error"
	}, 2*time.Second)
	if answer["t"] == "error" {
		t.Fatalf("hello rejected: %v: %v", answer["code"], answer["message"])
	}
	return peer
}

type pairResult struct {
	started  map[string]any
	request  map[string]any
	waiting  map[string]any
	accepted map[string]any
	ok       map[string]any
}

func pairPeers(t *testing.T, host *testPeer, client *testPeer, clientIdentity testIdentity, mode string) pairResult {
	t.Helper()
	host.send(t, map[string]any{"t": "pair.start"})
	started := host.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.start.ok"
	}, 2*time.Second)
	join := map[string]any{
		"t":        "pair.join",
		"pub":      clientIdentity.pub,
		"name":     "测试手机",
		"platform": "android",
	}
	if mode == "code" {
		join["code6"] = stringField(started, "code6")
	} else {
		join["token"] = stringField(started, "pair_token")
	}
	client.send(t, join)
	request := host.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.request"
	}, 2*time.Second)
	waiting := client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.waiting"
	}, 2*time.Second)
	host.send(t, map[string]any{"t": "pair.accept", "client_id": clientIdentity.deviceID})
	accepted := client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.accepted"
	}, 2*time.Second)
	ok := host.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.ok"
	}, 2*time.Second)
	return pairResult{started: started, request: request, waiting: waiting, accepted: accepted, ok: ok}
}

func makeEnvelope(t *testing.T, from testIdentity, to testIdentity, payload string) map[string]any {
	t.Helper()
	envelope, err := protocol.CreateEnvelope(from.deviceID, to.deviceID, []byte(payload), from.keys, to.keys.PublicKey)
	if err != nil {
		t.Fatalf("create envelope: %v", err)
	}
	return map[string]any{
		"v":     envelope.V,
		"t":     envelope.T,
		"id":    envelope.ID,
		"from":  envelope.From,
		"to":    envelope.To,
		"ts":    envelope.TS,
		"nonce": envelope.Nonce,
		"ct":    envelope.CT,
	}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", message)
}

func TestPairingThroughTokenAndCode(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	hostIdentity := identityFromSeed(t, hostSeed)
	clientIdentity := identityFromSeed(t, clientSeed)
	host := connectPeer(t, fixture.url, hostIdentity)
	defer host.close()
	client := connectPeer(t, fixture.url, clientIdentity)
	defer client.close()

	tokenPair := pairPeers(t, host, client, clientIdentity, "token")
	if got := stringField(tokenPair.request, "client_id"); got != clientIdentity.deviceID {
		t.Fatalf("pair.request client_id = %q, want %q", got, clientIdentity.deviceID)
	}
	hostPub, err := protocol.FromBase64(stringField(tokenPair.waiting, "host_pub"))
	if err != nil {
		t.Fatalf("host_pub: %v", err)
	}
	if !protocol.VerifyHostFingerprint(hostPub, stringField(tokenPair.waiting, "host_fingerprint")) {
		t.Fatal("host fingerprint did not verify")
	}
	if protocol.VerifyHostFingerprint(hostPub, "0000000000000000") {
		t.Fatal("wrong fingerprint verified")
	}
	paired, err := fixture.server.db.IsPaired(context.Background(), hostIdentity.deviceID, clientIdentity.deviceID)
	if err != nil || !paired {
		t.Fatalf("is paired after token pair = %v, %v", paired, err)
	}

	codePair := pairPeers(t, host, client, clientIdentity, "code")
	if got := stringField(codePair.accepted, "host_id"); got != hostIdentity.deviceID {
		t.Fatalf("pair.accepted host_id = %q", got)
	}
	paired, err = fixture.server.db.IsPaired(context.Background(), hostIdentity.deviceID, clientIdentity.deviceID)
	if err != nil || !paired {
		t.Fatalf("is paired after code pair = %v, %v", paired, err)
	}
}

func TestWrongCodeLimitedToFiveAttemptsPerIP(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	host := connectPeer(t, fixture.url, identityFromSeed(t, hostSeed))
	defer host.close()
	clientIdentity := identityFromSeed(t, clientSeed)
	client := connectPeer(t, fixture.url, clientIdentity)
	defer client.close()

	host.send(t, map[string]any{"t": "pair.start"})
	started := host.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.start.ok"
	}, 2*time.Second)
	var code int
	_, _ = fmt.Sscanf(stringField(started, "code6"), "%d", &code)
	wrong := fmt.Sprintf("%06d", (code+1)%1_000_000)
	for attempt := 0; attempt < 5; attempt++ {
		client.send(t, map[string]any{
			"t":        "pair.join",
			"code6":    wrong,
			"pub":      clientIdentity.pub,
			"name":     "测试手机",
			"platform": "android",
		})
		answer := client.next(t, func(message map[string]any) bool {
			return message["t"] == "error"
		}, 2*time.Second)
		code := stringField(answer, "code")
		if code != "invalid_code" && code != "too_many_attempts" {
			t.Fatalf("unexpected error code %q", code)
		}
	}
	client.send(t, map[string]any{
		"t":        "pair.join",
		"code6":    stringField(started, "code6"),
		"pub":      clientIdentity.pub,
		"name":     "测试手机",
		"platform": "android",
	})
	blocked := client.next(t, func(message map[string]any) bool {
		return message["t"] == "error"
	}, 2*time.Second)
	if got := stringField(blocked, "code"); got != "too_many_attempts" {
		t.Fatalf("blocked code = %q, want too_many_attempts", got)
	}
	if host.has(func(message map[string]any) bool { return message["t"] == "pair.request" }) {
		t.Fatal("host received a pair.request after the attempts were exhausted")
	}
}

func TestMessagesOnlyForwardedBetweenPairedDevices(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	hostIdentity := identityFromSeed(t, hostSeed)
	clientIdentity := identityFromSeed(t, clientSeed)
	host := connectPeer(t, fixture.url, hostIdentity)
	defer host.close()
	client := connectPeer(t, fixture.url, clientIdentity)
	defer client.close()

	envelope := makeEnvelope(t, hostIdentity, clientIdentity,
		`{"type":"event","name":"message","data":{"text":"not paired"}}`)
	host.send(t, envelope)
	rejected := host.next(t, func(message map[string]any) bool {
		return message["t"] == "error"
	}, 2*time.Second)
	if got := stringField(rejected, "code"); got != "not_paired" {
		t.Fatalf("error code = %q, want not_paired", got)
	}
	if client.has(func(message map[string]any) bool { return message["t"] == "msg" }) {
		t.Fatal("unpaired client received a message")
	}
}

type countingWaker struct {
	count atomic.Int32
}

func (w *countingWaker) Wake(context.Context, string) WakeResult {
	w.count.Add(1)
	return WakeResult{}
}

func TestOfflineMessagesQueuedReplayedAndAcked(t *testing.T) {
	waker := &countingWaker{}
	fixture := newServerFixture(t, "", waker)
	hostIdentity := identityFromSeed(t, hostSeed)
	clientIdentity := identityFromSeed(t, clientSeed)
	host := connectPeer(t, fixture.url, hostIdentity)
	defer host.close()
	client := connectPeer(t, fixture.url, clientIdentity)
	pairPeers(t, host, client, clientIdentity, "token")
	client.close()
	eventually(t, 2*time.Second, func() bool {
		return !fixture.server.isOnline(clientIdentity.deviceID)
	}, "client should disconnect")

	envelope := makeEnvelope(t, hostIdentity, clientIdentity,
		`{"type":"event","name":"message","data":{"text":"离线消息"}}`)
	host.send(t, envelope)
	eventually(t, 2*time.Second, func() bool {
		count, err := fixture.server.db.QueueCount(context.Background(), clientIdentity.deviceID)
		return err == nil && count == 1
	}, "envelope should be queued")
	eventually(t, 2*time.Second, func() bool {
		return waker.count.Load() == 1
	}, "waker should be called")

	reconnected := connectPeer(t, fixture.url, clientIdentity)
	defer reconnected.close()
	replayed := reconnected.next(t, func(message map[string]any) bool {
		return message["t"] == "msg"
	}, 2*time.Second)
	if got := stringField(replayed, "id"); got != stringField(envelope, "id") {
		t.Fatalf("replayed id = %q, want %q", got, stringField(envelope, "id"))
	}
	reconnected.send(t, map[string]any{"t": "ack", "id": stringField(envelope, "id")})
	eventually(t, 2*time.Second, func() bool {
		count, err := fixture.server.db.QueueCount(context.Background(), clientIdentity.deviceID)
		return err == nil && count == 0
	}, "ack should remove the queued envelope")
}

func TestRevokingPairingClearsQueue(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	hostIdentity := identityFromSeed(t, hostSeed)
	clientIdentity := identityFromSeed(t, clientSeed)
	host := connectPeer(t, fixture.url, hostIdentity)
	defer host.close()
	client := connectPeer(t, fixture.url, clientIdentity)
	pairPeers(t, host, client, clientIdentity, "token")
	client.close()
	eventually(t, 2*time.Second, func() bool {
		return !fixture.server.isOnline(clientIdentity.deviceID)
	}, "client should disconnect")

	host.send(t, makeEnvelope(t, hostIdentity, clientIdentity,
		`{"type":"event","name":"message","data":{"text":"before revoke"}}`))
	eventually(t, 2*time.Second, func() bool {
		count, err := fixture.server.db.QueueCount(context.Background(), clientIdentity.deviceID)
		return err == nil && count == 1
	}, "envelope should be queued")

	host.send(t, map[string]any{"t": "pair.revoke", "peer_id": clientIdentity.deviceID})
	revoked := host.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.revoked.ok"
	}, 2*time.Second)
	if got := stringField(revoked, "peer_id"); got != clientIdentity.deviceID {
		t.Fatalf("revoked peer_id = %q", got)
	}
	paired, err := fixture.server.db.IsPaired(context.Background(), hostIdentity.deviceID, clientIdentity.deviceID)
	if err != nil || paired {
		t.Fatalf("is paired after revoke = %v, %v", paired, err)
	}
	count, err := fixture.server.db.QueueCount(context.Background(), clientIdentity.deviceID)
	if err != nil || count != 0 {
		t.Fatalf("queue count after revoke = %d, %v", count, err)
	}
}

func TestDatabaseNeverContainsPlaintext(t *testing.T) {
	directory := t.TempDir()
	dbPath := filepath.Join(directory, "homing.sqlite")
	fixture := newServerFixture(t, dbPath, nil)
	hostIdentity := identityFromSeed(t, hostSeed)
	clientIdentity := identityFromSeed(t, clientSeed)
	host := connectPeer(t, fixture.url, hostIdentity)
	client := connectPeer(t, fixture.url, clientIdentity)
	pairPeers(t, host, client, clientIdentity, "token")
	client.close()
	eventually(t, 2*time.Second, func() bool {
		return !fixture.server.isOnline(clientIdentity.deviceID)
	}, "client should disconnect")

	const marker = "HOMING_PLAINTEXT_MARKER_DO_NOT_PERSIST"
	host.send(t, makeEnvelope(t, hostIdentity, clientIdentity,
		`{"type":"event","name":"message","data":{"text":"`+marker+`"}}`))
	eventually(t, 2*time.Second, func() bool {
		count, err := fixture.server.db.QueueCount(context.Background(), clientIdentity.deviceID)
		return err == nil && count == 1
	}, "envelope should be queued")
	host.close()
	if err := fixture.server.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	contents, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read database: %v", err)
	}
	if bytes.Contains(contents, []byte(marker)) {
		t.Fatal("sqlite database contains a plaintext payload")
	}
	matches, _ := filepath.Glob(dbPath + "*")
	for _, match := range matches {
		sidecar, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		if bytes.Contains(sidecar, []byte(marker)) {
			t.Fatalf("sidecar %s contains a plaintext payload", filepath.Base(match))
		}
	}
}

func TestHealthAndUnauthenticatedRequests(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	response, err := httpGet("http://" + fixture.server.listener.Addr().String() + "/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if !strings.Contains(response, `"service":"homing-server"`) {
		t.Fatalf("unexpected health body: %s", response)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, fixture.url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	peer := newTestPeer(ws)
	defer peer.close()
	peer.next(t, func(message map[string]any) bool {
		return message["t"] == "challenge"
	}, 2*time.Second)
	peer.send(t, map[string]any{"t": "pair.start"})
	rejected := peer.next(t, func(message map[string]any) bool {
		return message["t"] == "error"
	}, 2*time.Second)
	if got := stringField(rejected, "code"); got != "not_authenticated" {
		t.Fatalf("error code = %q, want not_authenticated", got)
	}
}

func TestHelloRejectsDeviceIDMismatch(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	identity := identityFromSeed(t, hostSeed)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, fixture.url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	peer := newTestPeer(ws)
	defer peer.close()
	challenge := peer.next(t, func(message map[string]any) bool {
		return message["t"] == "challenge"
	}, 2*time.Second)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	signature, err := protocol.HelloSignature(identity.keys.PrivateKey, stringField(challenge, "nonce"), ts)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	peer.send(t, map[string]any{
		"t":         "hello",
		"device_id": "not-the-real-device-id",
		"pub":       identity.pub,
		"ts":        ts,
		"sig":       protocol.ToBase64(signature),
	})
	rejected := peer.next(t, func(message map[string]any) bool {
		return message["t"] == "error"
	}, 2*time.Second)
	if got := stringField(rejected, "code"); got != "device_id_mismatch" {
		t.Fatalf("error code = %q, want device_id_mismatch", got)
	}
}

func TestPushRegistration(t *testing.T) {
	fixture := newServerFixture(t, "", nil)
	identity := identityFromSeed(t, hostSeed)
	peer := connectPeer(t, fixture.url, identity)
	defer peer.close()
	peer.send(t, map[string]any{"t": "push.register", "provider": "fcm", "token": "demo-token"})
	registered := peer.next(t, func(message map[string]any) bool {
		return message["t"] == "push.registered"
	}, 2*time.Second)
	if got := stringField(registered, "provider"); got != "fcm" {
		t.Fatalf("provider = %q", got)
	}
	tokens, err := fixture.server.db.PushTokens(context.Background(), identity.deviceID)
	if err != nil || len(tokens) != 1 || tokens[0].Token != "demo-token" {
		t.Fatalf("push tokens = %#v, %v", tokens, err)
	}
	peer.send(t, map[string]any{"t": "push.register", "provider": "unknown", "token": "x"})
	rejected := peer.next(t, func(message map[string]any) bool {
		return message["t"] == "error"
	}, 2*time.Second)
	if got := stringField(rejected, "code"); got != "invalid_push" {
		t.Fatalf("error code = %q, want invalid_push", got)
	}
}

func httpGet(url string) (string, error) {
	response, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return string(body), err
}
