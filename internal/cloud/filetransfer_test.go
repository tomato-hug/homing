package cloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
)

type pipeAddr string

func (p pipeAddr) Network() string { return "pipe" }
func (p pipeAddr) String() string  { return string(p) }

type singlePipeListener struct {
	mu     sync.Mutex
	conn   net.Conn
	closed chan struct{}
	once   sync.Once
}

func (l *singlePipeListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	c := l.conn
	l.conn = nil
	l.mu.Unlock()
	if c != nil {
		return c, nil
	}
	<-l.closed
	return nil, io.EOF
}

func (l *singlePipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *singlePipeListener) Addr() net.Addr { return pipeAddr("in-memory-ws") }

func newInMemoryWebSocketPair(t *testing.T, ctx context.Context, serverHandler func(*websocket.Conn)) *websocket.Conn {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	ln := &singlePipeListener{conn: serverConn, closed: make(chan struct{})}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			serverHandler(peer)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = ln.Close()
		_ = srv.Close()
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return clientConn, nil
			},
		},
	}
	ws, _, err := websocket.Dial(ctx, "ws://pipe.local/v1/ws", &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		t.Fatalf("in-memory websocket dial failed: %v", err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	return ws
}

func TestConnectorReadLoopWaitsForInFlightWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entered := make(chan struct{})
	var workerFinished atomic.Bool

	connector, keys, clientID := setupConnectorTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		// Simulate an ordered write mutation finishing local store sync as the socket drops.
		time.Sleep(35 * time.Millisecond)
		workerFinished.Store(true)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	env := clientEnvelope(t, keys, clientID, connector.identity.DeviceID(), connector.identity.keys.PublicKey,
		`{"type":"rpc","rid":"demo-drain-ordered-1","method":"POST","path":"/api/reply","body":{"text":"drain"}}`)

	ws := newInMemoryWebSocketPair(t, ctx, func(peer *websocket.Conn) {
		raw, _ := json.Marshal(env)
		_ = peer.Write(ctx, websocket.MessageText, raw)
		<-entered
		// Drop relay connection while ordered mutation worker is still in flight.
		_ = peer.CloseNow()
	})

	_ = connector.readLoop(ctx, ws)
	if !workerFinished.Load() {
		t.Fatal("readLoop returned before in-flight ordered RPC worker finished draining")
	}
}

func TestFileTransferInstanceIsolationAndRevokeCleanup(t *testing.T) {
	var hubBUploads atomic.Int32
	handlerA := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	handlerB := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		hubBUploads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	hubA, err := NewHub(t.TempDir(), handlerA, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hubA.Close)

	hubB, err := NewHub(t.TempDir(), handlerB, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hubB.Close)

	keys, err := protocol.KeyPairFromSeed([]byte(clientSeed))
	if err != nil {
		t.Fatal(err)
	}
	deviceID := protocol.DeviceID(keys.PublicKey)
	pubB64 := protocol.ToBase64(keys.PublicKey)

	for _, h := range []*Hub{hubA, hubB} {
		if err := h.Peers().Add(Peer{
			DeviceID:  deviceID,
			PublicKey: pubB64,
			Name:      "demo-phone",
			Platform:  "android",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Start a multi-chunk upload on hubA (Seq 0, Last=false).
	chunk0Plain := mustMarshal(t, cloudFileChunk{
		Type:        "file.chunk",
		FID:         "fid_shared_1",
		Seq:         0,
		Last:        false,
		Path:        "/api/upload?name=demo.txt",
		ContentType: "text/plain",
		Data:        base64.StdEncoding.EncodeToString([]byte("part-one-")),
	})
	envA0 := clientEnvelope(t, keys, deviceID, hubA.Identity().DeviceID(), hubA.Identity().Keys().PublicKey, string(chunk0Plain))
	connA := &hostConn{hub: hubA, deviceID: deviceID, pub: pubB64}
	connA.closed.Store(true)
	if err := hubA.handleEnvelope(connA, envA0); err != nil {
		t.Fatalf("hubA chunk 0 failed: %v", err)
	}

	// Sending Seq 1 on hubB (which never received Seq 0) must NOT steal hubA's buffer or hit handlerB!
	chunk1Plain := mustMarshal(t, cloudFileChunk{
		Type: "file.chunk",
		FID:  "fid_shared_1",
		Seq:  1,
		Last: true,
		Data: base64.StdEncoding.EncodeToString([]byte("part-two")),
	})
	envB1 := clientEnvelope(t, keys, deviceID, hubB.Identity().DeviceID(), hubB.Identity().Keys().PublicKey, string(chunk1Plain))
	connB := &hostConn{hub: hubB, deviceID: deviceID, pub: pubB64}
	connB.closed.Store(true)
	if err := hubB.handleEnvelope(connB, envB1); err != nil {
		t.Fatalf("hubB chunk 1 failed: %v", err)
	}
	if got := hubBUploads.Load(); got != 0 {
		t.Fatalf("hubB leaked partial file assembly state from hubA and completed upload (%d hits)", got)
	}

	// Revoking the peer on hubA must purge its unfinished assembly so re-pairing cannot resume stale bytes.
	if err := hubA.RevokePeer(deviceID); err != nil {
		t.Fatal(err)
	}
	if err := hubA.Peers().Add(Peer{
		DeviceID:  deviceID,
		PublicKey: pubB64,
		Name:      "demo-phone-repaired",
		Platform:  "android",
	}); err != nil {
		t.Fatal(err)
	}
	var hubAUploads atomic.Int32
	hubA.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		hubAUploads.Add(1)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	envA1 := clientEnvelope(t, keys, deviceID, hubA.Identity().DeviceID(), hubA.Identity().Keys().PublicKey, string(chunk1Plain))
	if err := hubA.handleEnvelope(connA, envA1); err != nil {
		t.Fatalf("hubA post-revoke chunk 1 failed: %v", err)
	}
	if got := hubAUploads.Load(); got != 0 {
		t.Fatalf("hubA retained stale in-flight file buffer across RevokePeer (%d uploads)", got)
	}
}

func TestFileTransferChunkBoundariesSecurityAndTTLExpiry(t *testing.T) {
	ctx := context.Background()
	clearFileAssemblies()
	t.Cleanup(clearFileAssemblies)

	var uploaded []byte
	var uploadedCT string
	downloadPayload := bytes.Repeat([]byte("ABCD"), (maxCloudFileChunkBytes/4)*2+128) // > 128 KB (3 chunks)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Homing-RPC") != "1" {
			http.Error(w, "untrusted", http.StatusForbidden)
			return
		}
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/upload"):
			uploadedCT = r.Header.Get("Content-Type")
			uploaded, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/f/demo-large.bin":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(downloadPayload)
		case r.Method == http.MethodGet && r.URL.Path == "/f/demo-empty.bin":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/f/demo-huge.bin":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, maxCloudFileBytes+1))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"ok":false,"error":"not_found"}`))
		}
	})

	// 1. Multi-chunk upload happy path across 64 KB boundary
	part0 := bytes.Repeat([]byte("x"), maxCloudFileChunkBytes)
	part1 := []byte("tail-payload")
	out, handled := dispatchFilePayload(ctx, handler, "peer-1", mustMarshal(t, cloudFileChunk{
		Type: "file.chunk", FID: "fid_ok", Seq: 0, Last: false,
		Path: "/api/upload?name=a.bin", ContentType: "application/x-demo",
		Data: base64.StdEncoding.EncodeToString(part0),
	}))
	if !handled || len(out) != 0 {
		t.Fatalf("chunk 0 = (%#v, %v), want (nil, true)", out, handled)
	}
	out, handled = dispatchFilePayload(ctx, handler, "peer-1", mustMarshal(t, cloudFileChunk{
		Type: "file.chunk", FID: "fid_ok", Seq: 1, Last: true,
		Data: base64.StdEncoding.EncodeToString(part1),
	}))
	if !handled || len(out) != 1 || out[0].(cloudFileResult).Status != http.StatusOK {
		t.Fatalf("chunk 1 result = %#v", out)
	}
	if uploadedCT != "application/x-demo" || len(uploaded) != len(part0)+len(part1) {
		t.Fatalf("uploaded len=%d ct=%q", len(uploaded), uploadedCT)
	}

	// 2. Out-of-order chunk -> 409 file_chunk_out_of_order, plus unknown fid -> 400
	_, _ = dispatchFilePayload(ctx, handler, "peer-1", mustMarshal(t, cloudFileChunk{
		Type: "file.chunk", FID: "fid_ooo", Seq: 0, Last: false,
		Path: "/api/upload", Data: base64.StdEncoding.EncodeToString([]byte("a")),
	}))
	out, _ = dispatchFilePayload(ctx, handler, "peer-1", mustMarshal(t, cloudFileChunk{
		Type: "file.chunk", FID: "fid_ooo", Seq: 2, Last: true,
		Data: base64.StdEncoding.EncodeToString([]byte("c")),
	}))
	assertFileError(t, out, http.StatusConflict, "file_chunk_out_of_order")

	out, _ = dispatchFilePayload(ctx, handler, "peer-1", mustMarshal(t, cloudFileChunk{
		Type: "file.chunk", FID: "fid_missing", Seq: 1, Last: true,
		Data: base64.StdEncoding.EncodeToString([]byte("c")),
	}))
	assertFileError(t, out, http.StatusBadRequest, "unknown_file_transfer")

	// 3. Single chunk > 64 KB -> 413 file_chunk_too_large
	out, _ = dispatchFilePayload(ctx, handler, "peer-1", mustMarshal(t, cloudFileChunk{
		Type: "file.chunk", FID: "fid_bigchunk", Seq: 0, Last: true,
		Path: "/api/upload",
		Data: base64.StdEncoding.EncodeToString(make([]byte, maxCloudFileChunkBytes+1)),
	}))
	assertFileError(t, out, http.StatusRequestEntityTooLarge, "file_chunk_too_large")

	// 4. Total assembled size > 20 MB across valid 64 KB chunks -> 413 file_too_large
	fullChunkB64 := base64.StdEncoding.EncodeToString(make([]byte, maxCloudFileChunkBytes))
	maxChunks := maxCloudFileBytes / maxCloudFileChunkBytes
	for seq := range maxChunks {
		p := ""
		if seq == 0 {
			p = "/api/upload"
		}
		res, _ := dispatchFilePayload(ctx, handler, "peer-1", mustMarshal(t, cloudFileChunk{
			Type: "file.chunk", FID: "fid_20mb", Seq: seq, Last: false, Path: p, Data: fullChunkB64,
		}))
		if len(res) != 0 {
			t.Fatalf("unexpected failure at seq %d: %#v", seq, res)
		}
	}
	out, _ = dispatchFilePayload(ctx, handler, "peer-1", mustMarshal(t, cloudFileChunk{
		Type: "file.chunk", FID: "fid_20mb", Seq: maxChunks, Last: true,
		Data: base64.StdEncoding.EncodeToString([]byte("1")),
	}))
	assertFileError(t, out, http.StatusRequestEntityTooLarge, "file_too_large")

	// 5. Forbidden upload & path-traversal download (/f/..) -> 403
	out, _ = dispatchFilePayload(ctx, handler, "peer-1", mustMarshal(t, cloudFileChunk{
		Type: "file.chunk", FID: "fid_badup", Seq: 0, Last: true,
		Path: "/console/index.html", Data: base64.StdEncoding.EncodeToString([]byte("x")),
	}))
	assertFileError(t, out, http.StatusForbidden, "forbidden_upload_path")

	if !validDownloadPath("/f/demo/a.png") {
		t.Fatal("expected /f/ path to be allowed")
	}
	if !validDownloadPath("/api/fs/raw?path=a.png&cwd=/tmp/demo") {
		t.Fatal("expected /api/fs/raw path to be allowed")
	}
	if validDownloadPath("/api/fs/raw?path=../secret.png") {
		t.Fatal("expected traversal in /api/fs/raw to be rejected")
	}

	out, _ = dispatchFilePayload(ctx, handler, "peer-1", []byte(`{"type":"file.request","fid":"fid_trav","path":"/f/../config.json"}`))
	assertFileError(t, out, http.StatusForbidden, "forbidden_download_path")

	out, _ = dispatchFilePayload(ctx, handler, "peer-1", []byte(`{"type":"file.request","fid":"../bad","path":"/f/demo"}`))
	assertFileError(t, out, http.StatusBadRequest, "invalid_transfer_id")

	// 6. Multi-chunk download, empty download, 404 download, and >20MB download
	chunks, _ := dispatchFilePayload(ctx, handler, "peer-1", []byte(`{"type":"file.request","fid":"fid_dl","path":"/f/demo-large.bin"}`))
	if len(chunks) != 3 {
		t.Fatalf("download chunks = %d, want 3", len(chunks))
	}
	var rebuilt bytes.Buffer
	for i, raw := range chunks {
		c := raw.(cloudFileChunk)
		if c.Seq != i || c.Last != (i == len(chunks)-1) {
			t.Fatalf("chunk %d metadata = %+v", i, c)
		}
		b, err := base64.StdEncoding.DecodeString(c.Data)
		if err != nil {
			t.Fatal(err)
		}
		rebuilt.Write(b)
	}
	if !bytes.Equal(rebuilt.Bytes(), downloadPayload) {
		t.Fatal("reassembled download payload mismatch")
	}

	emptyChunks, _ := dispatchFilePayload(ctx, handler, "peer-1", []byte(`{"type":"file.request","fid":"fid_empty","path":"/f/demo-empty.bin"}`))
	if len(emptyChunks) != 1 || !emptyChunks[0].(cloudFileChunk).Last || emptyChunks[0].(cloudFileChunk).Data != "" {
		t.Fatalf("empty file chunks = %#v", emptyChunks)
	}

	notFoundOut, _ := dispatchFilePayload(ctx, handler, "peer-1", []byte(`{"type":"file.request","fid":"fid_404","path":"/f/missing.bin"}`))
	assertFileError(t, notFoundOut, http.StatusNotFound, "not_found")

	hugeOut, _ := dispatchFilePayload(ctx, handler, "peer-1", []byte(`{"type":"file.request","fid":"fid_huge","path":"/f/demo-huge.bin"}`))
	assertFileError(t, hugeOut, http.StatusRequestEntityTooLarge, "file_too_large")

	// 7. Stale assembly TTL sweep (> fileAssemblyTTL)
	_, _ = dispatchFilePayload(ctx, handler, "peer-ttl", mustMarshal(t, cloudFileChunk{
		Type: "file.chunk", FID: "fid_stale", Seq: 0, Last: false,
		Path: "/api/upload", Data: base64.StdEncoding.EncodeToString([]byte("stale")),
	}))
	fileAssemblies.Lock()
	for _, item := range fileAssemblies.items {
		item.updatedAt = time.Now().Add(-fileAssemblyTTL - time.Second)
	}
	sweepFileAssembliesLocked(time.Now())
	remaining := len(fileAssemblies.items)
	fileAssemblies.Unlock()
	if remaining != 0 {
		t.Fatalf("stale assembly was not swept after TTL: remaining=%d", remaining)
	}
}

func assertFileError(t *testing.T, out []any, wantStatus int, wantCode string) {
	t.Helper()
	if len(out) != 1 {
		t.Fatalf("expected 1 error result, got %#v", out)
	}
	res, ok := out[0].(cloudFileResult)
	if !ok {
		t.Fatalf("expected cloudFileResult, got %T (%#v)", out[0], out[0])
	}
	body, _ := res.Body.(map[string]any)
	if res.Status != wantStatus || body["error"] != wantCode {
		t.Fatalf("got status=%d body=%#v, want status=%d error=%q", res.Status, res.Body, wantStatus, wantCode)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
