package cloud

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
)

const (
	lanChallengeTTL = 15 * time.Second
	lanPairTTL      = 5 * time.Minute
	lanMaxMessage   = 25 * 1024 * 1024
	lanHeartbeat    = 25 * time.Second
)

// PairInvite is what `homing pair` shows as a QR code and a 6-digit code.
type PairInvite struct {
	Server      string `json:"server"`
	Token       string `json:"pair_token"`
	Code6       string `json:"code6"`
	ExpiresIn   int    `json:"expires_in"`
	HostID      string `json:"host_id"`
	Fingerprint string `json:"host_fingerprint"`
	URI         string `json:"uri"`
}

// PairingRequest is a phone asking to pair. The caller confirms the SAS words
// on the computer before calling Approve.
type PairingRequest struct {
	ClientID    string    `json:"client_id"`
	PublicKey   string    `json:"public_key"`
	Name        string    `json:"name"`
	Platform    string    `json:"platform"`
	Fingerprint string    `json:"fingerprint"`
	SAS         [4]string `json:"sas"`
	ReceivedAt  time.Time `json:"received_at"`

	conn  *hostConn
	token string
}

// Hub serves LAN-direct phones on /v1/ws: it is the host side of protocol v1
// without the cloud relay in the middle.
type Hub struct {
	dir      string
	identity *Identity
	peers    *PeerStore
	handler  http.Handler
	log      *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu         sync.Mutex
	serverHint string
	conns      map[string]map[*hostConn]struct{}
	all        map[*hostConn]struct{}
	pairTokens map[string]*lanToken
	codeIndex  map[string]string
	pending    map[string]*PairingRequest
	requests   chan PairingRequest
	files      fileAssembler
}

type lanToken struct {
	hostID    string
	code6     string
	expiresAt time.Time
	used      bool
}

type hostConn struct {
	hub *Hub
	ws  *websocket.Conn

	writeMu sync.Mutex
	closed  atomic.Bool
	authed  atomic.Bool

	addr     string
	nonce    string
	deviceID string
	pub      string
}

// NewHub loads (or creates) this computer's identity and pairing list.
func NewHub(dataDir string, handler http.Handler, log *slog.Logger) (*Hub, error) {
	if log == nil {
		log = slog.Default()
	}
	cloudDir := filepath.Join(dataDir, "cloud")
	identity, err := LoadIdentity(cloudDir)
	if err != nil {
		return nil, err
	}
	peers, err := LoadPeerStore(cloudDir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	hub := &Hub{
		dir:        cloudDir,
		identity:   identity,
		peers:      peers,
		handler:    handler,
		log:        log,
		ctx:        ctx,
		cancel:     cancel,
		serverHint: "ws://127.0.0.1:18787",
		conns:      make(map[string]map[*hostConn]struct{}),
		all:        make(map[*hostConn]struct{}),
		pairTokens: make(map[string]*lanToken),
		codeIndex:  make(map[string]string),
		pending:    make(map[string]*PairingRequest),
		requests:   make(chan PairingRequest, 32),
	}
	hub.wg.Add(2)
	go func() {
		defer hub.wg.Done()
		hub.heartbeatLoop()
	}()
	go func() {
		defer hub.wg.Done()
		hub.cleanupLoop()
	}()
	return hub, nil
}

func (h *Hub) Identity() *Identity { return h.identity }

func (h *Hub) Peers() *PeerStore { return h.peers }

// Requests is how the future `homing pair` CLI learns that a phone is waiting.
func (h *Hub) Requests() <-chan PairingRequest { return h.requests }

func (h *Hub) SetServerURL(server string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.serverHint = server
}

func (h *Hub) ServerURL() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.serverHint
}

// BeginPairing creates a single-use token plus a 6-digit code and the QR URI.
func (h *Hub) BeginPairing() (PairInvite, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneTokensLocked()
	token, err := protocol.RandomPairToken()
	if err != nil {
		return PairInvite{}, err
	}
	code6, err := protocol.RandomCode6()
	if err != nil {
		return PairInvite{}, err
	}
	for {
		if _, exists := h.codeIndex[code6]; !exists {
			break
		}
		if code6, err = protocol.RandomCode6(); err != nil {
			return PairInvite{}, err
		}
	}
	expires := time.Now().Add(lanPairTTL)
	h.pairTokens[token] = &lanToken{hostID: h.identity.DeviceID(), code6: code6, expiresAt: expires}
	h.codeIndex[code6] = token
	invite := PairInvite{
		Server:      h.serverHint,
		Token:       token,
		Code6:       code6,
		ExpiresIn:   int(lanPairTTL / time.Second),
		HostID:      h.identity.DeviceID(),
		Fingerprint: h.identity.Fingerprint(),
	}
	invite.URI = protocol.PairURI(protocol.PairingInvite{
		Server:      invite.Server,
		Token:       invite.Token,
		HostID:      invite.HostID,
		Fingerprint: invite.Fingerprint,
	})
	return invite, nil
}

// Pending returns the requests waiting for a human decision.
func (h *Hub) Pending() []PairingRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]PairingRequest, 0, len(h.pending))
	for _, request := range h.pending {
		out = append(out, *request)
	}
	return out
}

// Approve accepts a pending request, saves the peer and tells the phone.
func (h *Hub) Approve(clientID string) error {
	h.mu.Lock()
	request := h.pending[clientID]
	if request == nil {
		h.mu.Unlock()
		return errors.New("cloud: no such pairing request")
	}
	delete(h.pending, clientID)
	h.cleanupTokenLocked(request.token)
	h.mu.Unlock()

	if request.conn == nil || request.conn.closed.Load() {
		return errors.New("cloud: the phone disconnected before approval")
	}
	if err := h.peers.Add(Peer{
		DeviceID:  request.ClientID,
		PublicKey: request.PublicKey,
		Name:      request.Name,
		Platform:  request.Platform,
		PairedAt:  time.Now().UTC(),
	}); err != nil {
		return err
	}
	request.conn.send(h.ctx, map[string]any{
		"t":         "pair.accepted",
		"host_id":   h.identity.DeviceID(),
		"host_pub":  h.identity.PublicKeyBase64(),
		"client_id": request.ClientID,
		"lan_urls":  LANWebSocketURLs(),
	})
	return nil
}

// Reject refuses a pending request.
func (h *Hub) Reject(clientID string) error {
	h.mu.Lock()
	request := h.pending[clientID]
	if request == nil {
		h.mu.Unlock()
		return errors.New("cloud: no such pairing request")
	}
	delete(h.pending, clientID)
	h.cleanupTokenLocked(request.token)
	h.mu.Unlock()
	if request.conn != nil {
		request.conn.send(h.ctx, map[string]any{"t": "pair.rejected", "host_id": h.identity.DeviceID()})
	}
	return nil
}

// RevokePeer removes one paired device and tells any live LAN sockets that the
// pairing is no longer valid. The same PeerStore is shared with the cloud
// connector, so the peer also stops receiving relayed notifications.
func (h *Hub) RevokePeer(peerID string) error {
	if _, ok := h.peers.Get(peerID); !ok {
		return errors.New("cloud: no such peer")
	}
	if err := h.peers.Remove(peerID); err != nil {
		return err
	}
	h.files.dropPeer(peerID)
	h.sendTo(peerID, map[string]any{
		"t": "pair.revoked", "peer_id": h.identity.DeviceID(),
	})
	h.mu.Lock()
	conns := make([]*hostConn, 0, len(h.conns[peerID]))
	for conn := range h.conns[peerID] {
		conns = append(conns, conn)
	}
	h.mu.Unlock()
	for _, conn := range conns {
		conn.close(websocket.StatusPolicyViolation, "pairing revoked")
	}
	return nil
}

// SendEvent pushes an SSE-shaped event to every paired phone.
func (h *Hub) SendEvent(ctx context.Context, name string, data any) (int, error) {
	sent := 0
	for _, peer := range h.peers.List() {
		envelope, err := sealPayload(h.identity, peer, eventPayload{Type: "event", Name: name, Data: data})
		if err != nil {
			return sent, err
		}
		sent += h.sendTo(peer.DeviceID, envelopeMap(envelope))
	}
	return sent, nil
}

// OnlinePeers returns the paired phones with at least one live LAN socket.
func (h *Hub) OnlinePeers() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]bool, len(h.conns))
	for deviceID, conns := range h.conns {
		if len(conns) > 0 {
			out[deviceID] = true
		}
	}
	return out
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		h.log.Warn("lan websocket accept failed", "err", err)
		return
	}
	ws.SetReadLimit(lanMaxMessage)
	nonce, err := protocol.RandomBytes(32)
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "random failed")
		return
	}
	conn := &hostConn{
		hub:   h,
		ws:    ws,
		addr:  r.RemoteAddr,
		nonce: protocol.ToBase64(nonce),
	}
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.readLoop(conn)
	}()
}

// Close stops the hub and every LAN connection.
func (h *Hub) Close() {
	h.cancel()
	h.mu.Lock()
	conns := make([]*hostConn, 0, len(h.all))
	for conn := range h.all {
		conns = append(conns, conn)
	}
	h.mu.Unlock()
	for _, conn := range conns {
		conn.close(websocket.StatusGoingAway, "hub shutdown")
	}
	h.wg.Wait()
	h.files.clear()
}

func (h *Hub) readLoop(conn *hostConn) {
	defer func() {
		conn.closed.Store(true)
		h.removeConn(conn)
		_ = conn.ws.CloseNow()
	}()
	conn.send(h.ctx, map[string]any{"t": "challenge", "nonce": conn.nonce})
	timer := time.AfterFunc(lanChallengeTTL, func() {
		if !conn.authed.Load() {
			conn.close(4001, "hello timeout")
		}
	})
	defer timer.Stop()

	for {
		_, raw, err := conn.ws.Read(h.ctx)
		if err != nil {
			return
		}
		var message map[string]any
		if err := json.Unmarshal(raw, &message); err != nil {
			conn.sendError(h.ctx, "invalid_json", "消息不是有效 JSON")
			continue
		}
		if err := h.handleMessage(conn, message); err != nil {
			h.log.Error("lan handler failed", "err", err)
			conn.sendError(h.ctx, "internal_error", "服务器处理消息失败")
		}
	}
}

func (h *Hub) handleMessage(conn *hostConn, message map[string]any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	kind, _ := message["t"].(string)
	if kind == "hello" {
		return h.handleHello(conn, message)
	}
	if !conn.authed.Load() {
		conn.sendError(h.ctx, "not_authenticated", "请先完成 hello")
		return nil
	}
	switch kind {
	case "pair.join":
		return h.handlePairJoin(conn, message)
	case "pair.revoke":
		return h.handleRevoke(conn, message)
	case "push.register":
		// Pushes are only meaningful through the cloud relay, but accept the
		// registration on the LAN socket so the same client code can connect
		// to either transport.
		conn.send(h.ctx, map[string]any{"t": "push.registered", "provider": "lan"})
		return nil
	case "msg":
		return h.handleEnvelope(conn, message)
	case "ping":
		conn.send(h.ctx, map[string]any{"t": "pong", "ts": time.Now().UTC().Format(time.RFC3339Nano)})
		return nil
	case "pong", "ack":
		return nil
	default:
		conn.sendError(h.ctx, "unknown_type", "未知消息类型: "+kind)
		return nil
	}
}

func (h *Hub) handleHello(conn *hostConn, message map[string]any) error {
	if conn.authed.Load() {
		conn.sendError(h.ctx, "already_authenticated", "设备已经完成认证")
		return nil
	}
	pub, err := protocol.FromBase64(stringField(message, "pub"))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		conn.sendError(h.ctx, "invalid_hello", "公钥或签名不是有效 base64")
		return nil
	}
	signature, err := protocol.FromBase64(stringField(message, "sig"))
	if err != nil {
		conn.sendError(h.ctx, "invalid_hello", "公钥或签名不是有效 base64")
		return nil
	}
	deviceID := protocol.DeviceID(pub)
	if deviceID != stringField(message, "device_id") {
		conn.sendError(h.ctx, "device_id_mismatch", "device_id 与公钥不匹配")
		return nil
	}
	ts := stringField(message, "ts")
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil || abs(parsed.Sub(time.Now())) > 5*time.Minute {
		conn.sendError(h.ctx, "stale_hello", "hello 时间戳超出允许范围")
		return nil
	}
	if !protocol.VerifyHelloSignature(pub, conn.nonce, ts, signature) {
		conn.sendError(h.ctx, "invalid_signature", "hello 签名校验失败")
		return nil
	}
	conn.deviceID = deviceID
	conn.pub = protocol.ToBase64(pub)
	conn.authed.Store(true)
	h.addConn(conn)
	conn.send(h.ctx, map[string]any{
		"t":           "hello.ok",
		"device_id":   h.identity.DeviceID(),
		"fingerprint": h.identity.Fingerprint(),
	})
	return nil
}

func (h *Hub) handlePairJoin(conn *hostConn, message map[string]any) error {
	token := stringField(message, "token")
	if token == "" {
		code6 := stringField(message, "code6")
		if code6 == "" {
			conn.sendError(h.ctx, "invalid_pair", "需要 pair_token 或 code6")
			return nil
		}
		h.mu.Lock()
		candidate, exists := h.codeIndex[code6]
		h.mu.Unlock()
		if !exists {
			conn.sendError(h.ctx, "invalid_code", "配对码不正确")
			return nil
		}
		token = candidate
	}
	clientPub, err := protocol.FromBase64(stringField(message, "pub"))
	if err != nil || len(clientPub) != ed25519.PublicKeySize {
		conn.sendError(h.ctx, "invalid_pub", "客户端公钥不是有效 base64")
		return nil
	}
	clientID := protocol.DeviceID(clientPub)
	if clientID == h.identity.DeviceID() {
		conn.sendError(h.ctx, "self_pair", "不能和自己配对")
		return nil
	}
	if provided := stringField(message, "client_id"); provided != "" && provided != clientID {
		conn.sendError(h.ctx, "client_id_mismatch", "client_id 与公钥不匹配")
		return nil
	}
	h.mu.Lock()
	h.pruneTokensLocked()
	entry := h.pairTokens[token]
	if entry == nil || entry.used || !entry.expiresAt.After(time.Now()) {
		h.mu.Unlock()
		conn.sendError(h.ctx, "invalid_pair_token", "配对 token 已失效")
		return nil
	}
	entry.used = true
	expiresAt := entry.expiresAt
	h.mu.Unlock()
	words, err := protocol.SASWords(h.identity.keys.PublicKey, clientPub)
	if err != nil {
		return err
	}
	request := &PairingRequest{
		ClientID:    clientID,
		PublicKey:   protocol.ToBase64(clientPub),
		Name:        stringField(message, "name"),
		Platform:    stringField(message, "platform"),
		Fingerprint: protocol.Fingerprint(clientPub),
		SAS:         words,
		ReceivedAt:  time.Now().UTC(),
		conn:        conn,
		token:       token,
	}
	h.mu.Lock()
	h.pending[clientID] = request
	h.mu.Unlock()
	conn.send(h.ctx, map[string]any{
		"t":                "pair.waiting",
		"host_id":          h.identity.DeviceID(),
		"host_pub":         h.identity.PublicKeyBase64(),
		"host_fingerprint": h.identity.Fingerprint(),
		"expires_in":       int(time.Until(expiresAt) / time.Second),
	})
	select {
	case h.requests <- *request:
	default:
		h.log.Warn("pairing request queue is full", "client", clientID)
	}
	return nil
}

func (h *Hub) handleRevoke(conn *hostConn, message map[string]any) error {
	peerID := stringField(message, "peer_id")
	if _, ok := h.peers.Get(peerID); !ok {
		conn.sendError(h.ctx, "not_paired", "没有与该设备的配对")
		return nil
	}
	if err := h.peers.Remove(peerID); err != nil {
		return err
	}
	h.files.dropPeer(peerID)
	h.sendTo(peerID, map[string]any{"t": "pair.revoked", "peer_id": conn.deviceID})
	conn.send(h.ctx, map[string]any{"t": "pair.revoked.ok", "peer_id": peerID})
	return nil
}

func (h *Hub) handleEnvelope(conn *hostConn, message map[string]any) error {
	envelope, ok := envelopeFromMap(message)
	if !ok {
		conn.sendError(h.ctx, "invalid_envelope", "不支持的消息封套")
		return nil
	}
	if envelope.From != conn.deviceID {
		conn.sendError(h.ctx, "from_mismatch", "from 与当前设备不一致")
		return nil
	}
	if envelope.To != h.identity.DeviceID() {
		conn.sendError(h.ctx, "wrong_recipient", "消息不是发给这台电脑的")
		return nil
	}
	peer, paired := h.peers.Get(envelope.From)
	if !paired {
		conn.sendError(h.ctx, "not_paired", "只能接收已配对设备的消息")
		return nil
	}
	if peer.PublicKey != conn.pub {
		conn.sendError(h.ctx, "device_key_changed", "设备公钥与配对记录不一致")
		return nil
	}
	peerPublicKey, err := decodePublicKey(peer.PublicKey)
	if err != nil {
		return err
	}
	plaintext, err := openPayload(h.identity, peerPublicKey, envelope)
	if err != nil {
		conn.sendError(h.ctx, "decrypt_failed", "消息解密失败")
		return nil
	}
	if payloads, handled := h.files.dispatch(h.ctx, h.handler, peer.DeviceID, plaintext); handled {
		for _, payload := range payloads {
			response, err := sealPayload(h.identity, peer, payload)
			if err != nil {
				return err
			}
			conn.send(h.ctx, envelopeMap(response))
		}
		return nil
	}
	result := dispatchRPC(h.ctx, h.handler, plaintext)
	var payload any
	if err := json.Unmarshal(result, &payload); err != nil {
		return err
	}
	response, err := sealPayload(h.identity, peer, payload)
	if err != nil {
		return err
	}
	conn.send(h.ctx, envelopeMap(response))
	return nil
}

func (h *Hub) addConn(conn *hostConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.conns[conn.deviceID]
	if set == nil {
		set = make(map[*hostConn]struct{})
		h.conns[conn.deviceID] = set
	}
	set[conn] = struct{}{}
	h.all[conn] = struct{}{}
}

func (h *Hub) removeConn(conn *hostConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.all, conn)
	if conn.deviceID == "" {
		return
	}
	dropFiles := false
	if set := h.conns[conn.deviceID]; set != nil {
		delete(set, conn)
		if len(set) == 0 {
			delete(h.conns, conn.deviceID)
			dropFiles = true
		}
	}
	if dropFiles {
		h.files.dropPeer(conn.deviceID)
	}
}

func (h *Hub) sendTo(deviceID string, value any) int {
	h.mu.Lock()
	conns := make([]*hostConn, 0, len(h.conns[deviceID]))
	for conn := range h.conns[deviceID] {
		conns = append(conns, conn)
	}
	h.mu.Unlock()
	sent := 0
	for _, conn := range conns {
		if conn.send(h.ctx, value) {
			sent++
		}
	}
	return sent
}

func (h *Hub) pruneTokensLocked() {
	now := time.Now()
	for token, entry := range h.pairTokens {
		if !entry.expiresAt.After(now) {
			delete(h.pairTokens, token)
			if h.codeIndex[entry.code6] == token {
				delete(h.codeIndex, entry.code6)
			}
		}
	}
}

func (h *Hub) cleanupTokenLocked(token string) {
	if token == "" {
		return
	}
	if entry := h.pairTokens[token]; entry != nil && h.codeIndex[entry.code6] == token {
		delete(h.codeIndex, entry.code6)
	}
	delete(h.pairTokens, token)
}

func (h *Hub) heartbeatLoop() {
	ticker := time.NewTicker(lanHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			h.mu.Lock()
			conns := make([]*hostConn, 0, len(h.all))
			for conn := range h.all {
				conns = append(conns, conn)
			}
			h.mu.Unlock()
			for _, conn := range conns {
				ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
				err := conn.ws.Ping(ctx)
				cancel()
				if err != nil {
					conn.close(websocket.StatusPolicyViolation, "heartbeat timeout")
				}
			}
		}
	}
}

func (h *Hub) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			h.mu.Lock()
			h.pruneTokensLocked()
			for id, request := range h.pending {
				if now.Sub(request.ReceivedAt) > lanPairTTL || request.conn.closed.Load() {
					delete(h.pending, id)
				}
			}
			h.mu.Unlock()
		}
	}
}

func (c *hostConn) send(ctx context.Context, value any) bool {
	if c.closed.Load() {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.ws.Write(writeCtx, websocket.MessageText, raw); err != nil {
		c.close(websocket.StatusInternalError, "write failed")
		return false
	}
	return true
}

func (c *hostConn) sendError(ctx context.Context, code, message string) {
	c.send(ctx, map[string]any{"t": "error", "code": code, "message": message})
}

func (c *hostConn) close(code websocket.StatusCode, reason string) {
	if c.closed.CompareAndSwap(false, true) {
		_ = c.ws.Close(code, reason)
	}
}

func abs(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func stringField(message map[string]any, key string) string {
	value, _ := message[key].(string)
	return value
}
