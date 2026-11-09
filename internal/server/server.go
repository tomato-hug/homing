package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
)

const (
	challengeTTL    = 15 * time.Second
	pairTTL         = 5 * time.Minute
	maxCodeAttempts = 5
	helloClockSkew  = 5 * time.Minute
	maxMessageSize  = 25 * 1024 * 1024
	heartbeatEvery  = 25 * time.Second
	cleanupEvery    = 30 * time.Second
)

// Config configures a homing-server instance.
type Config struct {
	Addr   string
	DBPath string
	Logger *slog.Logger
	Waker  Waker
	// Now is for tests; nil means time.Now.
	Now func() time.Time
}

// Server is the cloud relay: it authenticates devices, pairs them, forwards
// encrypted envelopes and queues them while the recipient is offline.
type Server struct {
	cfg   Config
	log   *slog.Logger
	db    *DB
	waker Waker
	now   func() time.Time

	httpServer *http.Server
	listener   net.Listener
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	closeOnce  sync.Once

	mu           sync.Mutex
	conns        map[string]map[*clientConn]struct{}
	all          map[*clientConn]struct{}
	pairTokens   map[string]*pairToken
	codeIndex    map[string]string
	pendingPairs map[string]*pendingPair
	pairAttempts map[string]*codeAttempt
}

type pairToken struct {
	hostID    string
	code6     string
	expiresAt time.Time
	used      bool
}

type pendingPair struct {
	token     string
	hostID    string
	hostPub   string
	clientID  string
	clientPub string
	name      string
	platform  string
	expiresAt time.Time
}

type codeAttempt struct {
	count   int
	resetAt time.Time
}

type clientConn struct {
	ws   *websocket.Conn
	srv  *Server
	addr string

	writeMu sync.Mutex
	closed  atomic.Bool
	authed  atomic.Bool

	// Set once during hello, before the connection joins the registry and is
	// visible to other goroutines.
	deviceID string
	pub      string
	nonce    string
}

// New opens the database and prepares a server. Call Start to listen.
func New(cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = "0.0.0.0:8080"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "./data/homing.sqlite"
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	db, err := OpenDB(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	waker := cfg.Waker
	if waker == nil {
		waker = NewPushDispatcher(db, log, "")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		cfg:          cfg,
		log:          log,
		db:           db,
		waker:        waker,
		now:          now,
		ctx:          ctx,
		cancel:       cancel,
		conns:        make(map[string]map[*clientConn]struct{}),
		all:          make(map[*clientConn]struct{}),
		pairTokens:   make(map[string]*pairToken),
		codeIndex:    make(map[string]string),
		pendingPairs: make(map[string]*pendingPair),
		pairAttempts: make(map[string]*codeAttempt),
	}
	s.httpServer = &http.Server{
		Addr:              cfg.Addr,
		Handler:           http.HandlerFunc(s.handleHTTP),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

// DB exposes storage for tests and for callers that need queue statistics.
func (s *Server) DB() *DB { return s.db }

// SetWaker replaces the wake-up dispatcher. Call it before Start; it exists so
// the entry point can build a dispatcher around the database New opened.
func (s *Server) SetWaker(waker Waker) {
	if waker != nil {
		s.waker = waker
	}
}

// Start begins listening and returns the actual listener (useful with :0).
func (s *Server) Start() (net.Listener, error) {
	listener, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("server: listen: %w", err)
	}
	s.listener = listener
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("http server stopped", "err", err)
		}
	}()
	go s.heartbeatLoop()
	go s.cleanupLoop()
	return listener, nil
}

// URL is the WebSocket URL clients should dial.
func (s *Server) URL() string {
	addr := s.cfg.Addr
	if s.listener != nil {
		addr = s.listener.Addr().String()
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "127.0.0.1", "8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "ws://" + net.JoinHostPort(host, port) + "/v1/ws"
}

// Close stops the listener, closes every socket and the database.
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		conns := make([]*clientConn, 0, len(s.all))
		for conn := range s.all {
			conns = append(conns, conn)
		}
		s.mu.Unlock()
		for _, conn := range conns {
			conn.close(websocket.StatusGoingAway, "server shutdown")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if shutdownErr := s.httpServer.Shutdown(ctx); shutdownErr != nil {
			err = fmt.Errorf("server: shutdown: %w", shutdownErr)
		}
		s.wg.Wait()
		if dbErr := s.db.Close(); dbErr != nil && err == nil {
			err = dbErr
		}
	})
	return err
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"service":"homing-server","version":1}`))
		return
	}
	if r.URL.Path == "/v1/ws" {
		s.handleWS(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"ok":false,"error":"not_found"}`))
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		s.log.Warn("websocket accept failed", "err", err)
		return
	}
	ws.SetReadLimit(maxMessageSize)
	rawNonce, err := protocol.RandomBytes(32)
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "random failed")
		return
	}
	conn := &clientConn{
		ws:    ws,
		srv:   s,
		addr:  normalizeAddress(r.RemoteAddr),
		nonce: protocol.ToBase64(rawNonce),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.readLoop(conn)
	}()
}

func normalizeAddress(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	return strings.TrimPrefix(host, "::ffff:")
}

func (c *clientConn) send(ctx context.Context, value any) bool {
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return c.sendRaw(ctx, raw)
}

func (c *clientConn) sendRaw(ctx context.Context, raw []byte) bool {
	if c.closed.Load() {
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

func (c *clientConn) close(code websocket.StatusCode, reason string) {
	if c.closed.CompareAndSwap(false, true) {
		_ = c.ws.Close(code, reason)
	}
}

func (c *clientConn) sendError(ctx context.Context, code, message string) {
	c.send(ctx, map[string]any{"t": "error", "code": code, "message": message})
}

func (s *Server) readLoop(conn *clientConn) {
	defer func() {
		conn.closed.Store(true)
		s.removeConnection(conn)
		_ = conn.ws.CloseNow()
	}()
	conn.send(s.ctx, map[string]any{"t": "challenge", "nonce": conn.nonce})

	challengeTimer := time.AfterFunc(challengeTTL, func() {
		if !conn.authed.Load() {
			conn.close(4001, "hello timeout")
		}
	})
	defer challengeTimer.Stop()

	for {
		_, raw, err := conn.ws.Read(s.ctx)
		if err != nil {
			return
		}
		var message map[string]any
		if err := json.Unmarshal(raw, &message); err != nil {
			conn.sendError(s.ctx, "invalid_json", "消息不是有效 JSON")
			continue
		}
		if err := s.handleMessage(conn, message); err != nil {
			s.log.Error("websocket handler failed", "err", err)
			conn.sendError(s.ctx, "internal_error", "服务器处理消息失败")
		}
	}
}

func (s *Server) handleMessage(conn *clientConn, message map[string]any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	kind, _ := message["t"].(string)
	if kind == "hello" {
		return s.handleHello(conn, message)
	}
	if !conn.authed.Load() {
		conn.sendError(s.ctx, "not_authenticated", "请先完成 hello")
		return nil
	}
	switch kind {
	case "pair.start":
		return s.handlePairStart(conn)
	case "pair.join":
		return s.handlePairJoin(conn, message)
	case "pair.accept":
		return s.handlePairDecision(conn, message, true)
	case "pair.reject":
		return s.handlePairDecision(conn, message, false)
	case "pair.revoke":
		return s.handleRevoke(conn, message)
	case "msg":
		return s.handleEnvelope(conn, message)
	case "ack":
		return s.handleAck(conn, message)
	case "push.register":
		return s.handlePushRegister(conn, message)
	case "ping":
		conn.send(s.ctx, map[string]any{"t": "pong", "ts": s.now().UTC().Format(time.RFC3339Nano)})
		return nil
	case "pong":
		return nil
	default:
		conn.sendError(s.ctx, "unknown_type", "未知消息类型: "+kind)
		return nil
	}
}

func (s *Server) handleHello(conn *clientConn, message map[string]any) error {
	if conn.authed.Load() {
		conn.sendError(s.ctx, "already_authenticated", "设备已经完成认证")
		return nil
	}
	pub, err := protocol.FromBase64(stringField(message, "pub"))
	if err != nil {
		conn.sendError(s.ctx, "invalid_hello", "公钥或签名不是有效 base64")
		return nil
	}
	signature, err := protocol.FromBase64(stringField(message, "sig"))
	if err != nil {
		conn.sendError(s.ctx, "invalid_hello", "公钥或签名不是有效 base64")
		return nil
	}
	if len(pub) != 32 {
		conn.sendError(s.ctx, "invalid_hello", "Ed25519 公钥必须是 32 字节")
		return nil
	}
	deviceID := protocol.DeviceID(pub)
	if deviceID != stringField(message, "device_id") {
		conn.sendError(s.ctx, "device_id_mismatch", "device_id 与公钥不匹配")
		return nil
	}
	timestamp := stringField(message, "ts")
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil || absDuration(s.now().Sub(parsed)) > helloClockSkew {
		conn.sendError(s.ctx, "stale_hello", "hello 时间戳超出允许范围")
		return nil
	}
	if !protocol.VerifyHelloSignature(pub, conn.nonce, timestamp, signature) {
		conn.sendError(s.ctx, "invalid_signature", "hello 签名校验失败")
		return nil
	}
	pubBase64 := protocol.ToBase64(pub)
	ok, err := s.db.RegisterDevice(s.ctx, deviceID, pubBase64, s.now())
	if err != nil {
		return err
	}
	if !ok {
		conn.sendError(s.ctx, "device_key_changed", "device_id 已绑定另一个公钥")
		return nil
	}
	conn.deviceID = deviceID
	conn.pub = pubBase64
	conn.authed.Store(true)
	s.addConnection(conn)
	conn.send(s.ctx, map[string]any{
		"t":           "hello.ok",
		"device_id":   deviceID,
		"fingerprint": protocol.Fingerprint(pub),
	})
	s.flushQueue(conn)
	return nil
}

func (s *Server) handlePairStart(conn *clientConn) error {
	s.mu.Lock()
	s.prunePairTokensLocked()
	token, err := protocol.RandomPairToken()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	code6, err := protocol.RandomCode6()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	for {
		if _, exists := s.codeIndex[code6]; !exists {
			break
		}
		if code6, err = protocol.RandomCode6(); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.pairTokens[token] = &pairToken{
		hostID:    conn.deviceID,
		code6:     code6,
		expiresAt: s.now().Add(pairTTL),
	}
	s.codeIndex[code6] = token
	s.mu.Unlock()
	conn.send(s.ctx, map[string]any{
		"t":          "pair.start.ok",
		"pair_token": token,
		"code6":      code6,
		"expires_in": int(pairTTL / time.Second),
	})
	return nil
}

func (s *Server) handlePairJoin(conn *clientConn, message map[string]any) error {
	token := stringField(message, "token")
	if token == "" {
		code6 := stringField(message, "code6")
		if code6 == "" {
			conn.sendError(s.ctx, "invalid_pair", "需要 pair_token 或 code6")
			return nil
		}
		now := s.now()
		s.mu.Lock()
		attempt := s.pairAttempts[conn.addr]
		if attempt != nil && attempt.resetAt.After(now) && attempt.count >= maxCodeAttempts {
			s.mu.Unlock()
			conn.sendError(s.ctx, "too_many_attempts", "配对码尝试次数过多，请重新生成")
			return nil
		}
		s.prunePairTokensLocked()
		found := ""
		if candidate, exists := s.codeIndex[code6]; exists {
			if entry, ok := s.pairTokens[candidate]; ok && !entry.used && entry.expiresAt.After(now) {
				found = candidate
			}
		}
		if found == "" {
			next := &codeAttempt{count: 1, resetAt: now.Add(pairTTL)}
			if attempt != nil && attempt.resetAt.After(now) {
				next.count = attempt.count + 1
				next.resetAt = attempt.resetAt
			}
			s.pairAttempts[conn.addr] = next
			s.mu.Unlock()
			if next.count >= maxCodeAttempts {
				conn.sendError(s.ctx, "too_many_attempts", "配对码尝试次数过多，请重新生成")
			} else {
				conn.sendError(s.ctx, "invalid_code", "配对码不正确")
			}
			return nil
		}
		token = found
		s.mu.Unlock()
	}

	clientPub, err := protocol.FromBase64(stringField(message, "pub"))
	if err != nil {
		conn.sendError(s.ctx, "invalid_pub", "客户端公钥不是有效 base64")
		return nil
	}
	if len(clientPub) != 32 {
		conn.sendError(s.ctx, "invalid_pub", "客户端公钥必须是 32 字节")
		return nil
	}
	clientID := protocol.DeviceID(clientPub)
	if provided := stringField(message, "client_id"); provided != "" && provided != clientID {
		conn.sendError(s.ctx, "client_id_mismatch", "client_id 与公钥不匹配")
		return nil
	}

	s.mu.Lock()
	s.prunePairTokensLocked()
	entry := s.pairTokens[token]
	if entry == nil || entry.used || !entry.expiresAt.After(s.now()) {
		s.mu.Unlock()
		conn.sendError(s.ctx, "invalid_pair_token", "配对 token 已失效")
		return nil
	}
	if entry.hostID == conn.deviceID {
		s.mu.Unlock()
		conn.sendError(s.ctx, "self_pair", "不能和自己配对")
		return nil
	}
	hostOnline := len(s.conns[entry.hostID]) > 0
	entry.used = true
	hostID := entry.hostID
	expiresAt := entry.expiresAt
	s.mu.Unlock()
	if !hostOnline {
		conn.sendError(s.ctx, "host_offline", "电脑当前不在线")
		return nil
	}
	host, found, err := s.db.GetDevice(s.ctx, hostID)
	if err != nil {
		return err
	}
	if !found {
		conn.sendError(s.ctx, "host_missing", "电脑设备记录不存在")
		return nil
	}

	pending := &pendingPair{
		token:     token,
		hostID:    hostID,
		hostPub:   host.Pub,
		clientID:  clientID,
		clientPub: protocol.ToBase64(clientPub),
		name:      truncate(stringField(message, "name"), 120),
		platform:  truncate(stringField(message, "platform"), 40),
		expiresAt: expiresAt,
	}
	s.mu.Lock()
	s.pendingPairs[hostID+":"+clientID] = pending
	s.pendingPairs["token:"+token] = pending
	s.mu.Unlock()

	s.sendTo(hostID, map[string]any{
		"t":          "pair.request",
		"client_id":  clientID,
		"pub":        pending.clientPub,
		"name":       pending.name,
		"platform":   pending.platform,
		"pair_token": token,
	})
	hostPub, err := protocol.FromBase64(host.Pub)
	if err != nil {
		return err
	}
	conn.send(s.ctx, map[string]any{
		"t":                "pair.waiting",
		"host_id":          hostID,
		"host_pub":         host.Pub,
		"host_fingerprint": protocol.Fingerprint(hostPub),
		"expires_in":       int(expiresAt.Sub(s.now()) / time.Second),
	})
	return nil
}

func (s *Server) handlePairDecision(conn *clientConn, message map[string]any, accept bool) error {
	clientID := stringField(message, "client_id")
	s.mu.Lock()
	pending := s.pendingPairs[conn.deviceID+":"+clientID]
	s.mu.Unlock()
	if pending == nil {
		conn.sendError(s.ctx, "pair_not_pending", "没有等待处理的配对请求")
		return nil
	}
	if !pending.expiresAt.After(s.now()) {
		s.cleanupPair(pending.token)
		conn.sendError(s.ctx, "pair_expired", "配对请求已过期")
		return nil
	}
	if accept {
		if err := s.db.CreatePairing(s.ctx, pending.hostID, pending.clientID, s.now()); err != nil {
			return err
		}
		conn.send(s.ctx, map[string]any{"t": "pair.ok", "client_id": clientID})
		s.sendTo(pending.clientID, map[string]any{
			"t":         "pair.accepted",
			"host_id":   pending.hostID,
			"host_pub":  pending.hostPub,
			"client_id": pending.clientID,
		})
	} else {
		conn.send(s.ctx, map[string]any{"t": "pair.rejected", "client_id": clientID})
		s.sendTo(pending.clientID, map[string]any{
			"t":       "pair.rejected",
			"host_id": pending.hostID,
		})
	}
	s.cleanupPair(pending.token)
	return nil
}

func (s *Server) handleRevoke(conn *clientConn, message map[string]any) error {
	peerID := stringField(message, "peer_id")
	paired := false
	if peerID != "" {
		var err error
		paired, err = s.db.IsPaired(s.ctx, conn.deviceID, peerID)
		if err != nil {
			return err
		}
	}
	if !paired {
		conn.sendError(s.ctx, "not_paired", "没有与该设备的配对")
		return nil
	}
	if err := s.db.RevokePairing(s.ctx, conn.deviceID, peerID); err != nil {
		return err
	}
	s.sendTo(peerID, map[string]any{"t": "pair.revoked", "peer_id": conn.deviceID})
	conn.send(s.ctx, map[string]any{"t": "pair.revoked.ok", "peer_id": peerID})
	return nil
}

func (s *Server) handleEnvelope(conn *clientConn, message map[string]any) error {
	if numberField(message, "v") != 1 || stringField(message, "t") != "msg" {
		conn.sendError(s.ctx, "invalid_envelope", "不支持的消息封套")
		return nil
	}
	from := stringField(message, "from")
	if from != conn.deviceID {
		conn.sendError(s.ctx, "from_mismatch", "from 与当前设备不一致")
		return nil
	}
	to := stringField(message, "to")
	if to == "" {
		conn.sendError(s.ctx, "invalid_envelope", "缺少收件设备")
		return nil
	}
	id := stringField(message, "id")
	if id == "" {
		conn.sendError(s.ctx, "invalid_envelope", "缺少消息 id")
		return nil
	}
	paired, err := s.db.IsPaired(s.ctx, conn.deviceID, to)
	if err != nil {
		return err
	}
	if !paired {
		conn.sendError(s.ctx, "not_paired", "只能给已配对设备发消息")
		return nil
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if s.sendRawTo(to, raw) > 0 {
		return nil
	}
	if err := s.db.Enqueue(s.ctx, raw, id, from, to, s.now()); err != nil {
		return err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result := s.waker.Wake(ctx, to)
		if result.Error != "" {
			s.log.Warn("wake failed", "device", to, "err", result.Error)
		}
	}()
	return nil
}

func (s *Server) handleAck(conn *clientConn, message map[string]any) error {
	id := stringField(message, "id")
	if id == "" {
		conn.sendError(s.ctx, "invalid_ack", "缺少消息 id")
		return nil
	}
	_, err := s.db.Ack(s.ctx, conn.deviceID, id)
	return err
}

func (s *Server) handlePushRegister(conn *clientConn, message map[string]any) error {
	provider := stringField(message, "provider")
	token := stringField(message, "token")
	if !SupportedProviders[provider] || token == "" {
		conn.sendError(s.ctx, "invalid_push", "推送 provider 或 token 无效")
		return nil
	}
	if err := s.db.SavePushToken(s.ctx, conn.deviceID, provider, token, s.now()); err != nil {
		return err
	}
	conn.send(s.ctx, map[string]any{"t": "push.registered", "provider": provider})
	return nil
}

func (s *Server) addConnection(conn *clientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := s.conns[conn.deviceID]
	if set == nil {
		set = make(map[*clientConn]struct{})
		s.conns[conn.deviceID] = set
	}
	set[conn] = struct{}{}
	s.all[conn] = struct{}{}
}

func (s *Server) removeConnection(conn *clientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.all, conn)
	if conn.deviceID == "" {
		return
	}
	if set := s.conns[conn.deviceID]; set != nil {
		delete(set, conn)
		if len(set) == 0 {
			delete(s.conns, conn.deviceID)
		}
	}
}

func (s *Server) isOnline(deviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns[deviceID]) > 0
}

func (s *Server) sendTo(deviceID string, value any) int {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return s.sendRawTo(deviceID, raw)
}

func (s *Server) sendRawTo(deviceID string, raw []byte) int {
	s.mu.Lock()
	conns := make([]*clientConn, 0, len(s.conns[deviceID]))
	for conn := range s.conns[deviceID] {
		conns = append(conns, conn)
	}
	s.mu.Unlock()
	sent := 0
	for _, conn := range conns {
		if conn.sendRaw(s.ctx, raw) {
			sent++
		}
	}
	return sent
}

func (s *Server) flushQueue(conn *clientConn) {
	envelopes, err := s.db.QueuedFor(s.ctx, conn.deviceID)
	if err != nil {
		s.log.Warn("flush queue failed", "device", conn.deviceID, "err", err)
		return
	}
	for _, envelope := range envelopes {
		if !conn.sendRaw(s.ctx, envelope) {
			return
		}
	}
}

func (s *Server) prunePairTokensLocked() {
	now := s.now()
	for token, entry := range s.pairTokens {
		if !entry.expiresAt.After(now) {
			delete(s.pairTokens, token)
			if s.codeIndex[entry.code6] == token {
				delete(s.codeIndex, entry.code6)
			}
		}
	}
}

func (s *Server) cleanupPair(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.pairTokens[token]; entry != nil && s.codeIndex[entry.code6] == token {
		delete(s.codeIndex, entry.code6)
	}
	delete(s.pairTokens, token)
	for key, pending := range s.pendingPairs {
		if pending.token == token {
			delete(s.pendingPairs, key)
		}
	}
}

func (s *Server) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			conns := make([]*clientConn, 0, len(s.all))
			for conn := range s.all {
				conns = append(conns, conn)
			}
			s.mu.Unlock()
			for _, conn := range conns {
				ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
				err := conn.ws.Ping(ctx)
				cancel()
				if err != nil {
					conn.close(websocket.StatusPolicyViolation, "heartbeat timeout")
				}
			}
		}
	}
}

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(cleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			now := s.now()
			s.mu.Lock()
			s.prunePairTokensLocked()
			for key, pending := range s.pendingPairs {
				if !pending.expiresAt.After(now) {
					delete(s.pendingPairs, key)
				}
			}
			for ip, attempt := range s.pairAttempts {
				if !attempt.resetAt.After(now) {
					delete(s.pairAttempts, ip)
				}
			}
			s.mu.Unlock()
		}
	}
}

func stringField(message map[string]any, key string) string {
	value, _ := message[key].(string)
	return value
}

func numberField(message map[string]any, key string) float64 {
	value, _ := message[key].(float64)
	return value
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
