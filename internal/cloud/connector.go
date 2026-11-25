package cloud

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/protocol"
)

const (
	connectorHeartbeat  = 25 * time.Second
	connectorMaxBackoff = 60 * time.Second
)

// ErrNotConnected means the connector has no live relay socket right now.
var ErrNotConnected = errors.New("cloud: not connected to the relay")

// ConnectorConfig wires a computer to a Homing relay (cloud or self-hosted).
type ConnectorConfig struct {
	ServerURL string
	Identity  *Identity
	Peers     *PeerStore
	Handler   http.Handler
	Logger    *slog.Logger
}

// Connector keeps a WebSocket to the relay, answers encrypted RPC payloads
// against the local /api handler, and forwards events to paired phones.
type Connector struct {
	serverURL string
	identity  *Identity
	peers     *PeerStore
	handler   http.Handler
	log       *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	mu          sync.Mutex
	conn        *websocket.Conn
	connectedAt time.Time
	writeMu     sync.Mutex
	pairStartCh chan map[string]any
	pending     map[string]PairingRequest
	requests    chan PairingRequest
}

func NewConnector(cfg ConnectorConfig) (*Connector, error) {
	if cfg.Identity == nil || cfg.Peers == nil {
		return nil, errors.New("cloud: connector needs an identity and a peer store")
	}
	serverURL, err := protocol.NormalizeWebSocketURL(cfg.ServerURL)
	if err != nil {
		return nil, err
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Connector{
		serverURL: serverURL,
		identity:  cfg.Identity,
		peers:     cfg.Peers,
		handler:   cfg.Handler,
		log:       log,
		ctx:       ctx,
		cancel:    cancel,
		pending:   make(map[string]PairingRequest),
		requests:  make(chan PairingRequest, 32),
	}, nil
}

func (c *Connector) ServerURL() string { return c.serverURL }

func (c *Connector) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

func (c *Connector) ConnectedSince() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectedAt
}

// PairRequests delivers pair.request messages from the relay (host side).
func (c *Connector) PairRequests() <-chan PairingRequest { return c.requests }

func (c *Connector) Pending() []PairingRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]PairingRequest, 0, len(c.pending))
	for _, request := range c.pending {
		out = append(out, request)
	}
	return out
}

// Run dials the relay forever, backing off from 1s up to 60s.
func (c *Connector) Run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	c.mu.Lock()
	c.ctx = ctx
	c.cancel = cancel
	c.mu.Unlock()
	defer cancel()

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		connected, err := c.runSession(ctx)
		if ctx.Err() != nil {
			return
		}
		if connected {
			backoff = time.Second
		}
		c.log.Warn("cloud relay disconnected",
			"server", c.serverURL, "err", err, "retry_in", backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff *= 2
		if backoff > connectorMaxBackoff {
			backoff = connectorMaxBackoff
		}
	}
}

// Close stops the reconnect loop.
func (c *Connector) Close() {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Connector) runSession(parent context.Context) (bool, error) {
	dialCtx, cancel := context.WithTimeout(parent, 15*time.Second)
	ws, _, err := websocket.Dial(dialCtx, c.serverURL, nil)
	cancel()
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(25 * 1024 * 1024)

	challenge, err := readJSON(parent, ws, 10*time.Second)
	if err != nil {
		return false, fmt.Errorf("challenge: %w", err)
	}
	if stringField(challenge, "t") != "challenge" {
		return false, fmt.Errorf("expected challenge, got %q", stringField(challenge, "t"))
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	signature, err := protocol.HelloSignature(c.identity.keys.PrivateKey, stringField(challenge, "nonce"), ts)
	if err != nil {
		return false, fmt.Errorf("sign hello: %w", err)
	}
	if err := c.writeJSON(parent, ws, map[string]any{
		"t":         "hello",
		"device_id": c.identity.DeviceID(),
		"pub":       c.identity.PublicKeyBase64(),
		"ts":        ts,
		"sig":       protocol.ToBase64(signature),
	}); err != nil {
		return false, fmt.Errorf("hello: %w", err)
	}
	answer, err := readJSON(parent, ws, 10*time.Second)
	if err != nil {
		return false, fmt.Errorf("hello answer: %w", err)
	}
	if stringField(answer, "t") == "error" {
		return false, fmt.Errorf("hello rejected: %s: %s",
			stringField(answer, "code"), stringField(answer, "message"))
	}
	if stringField(answer, "t") != "hello.ok" {
		return false, fmt.Errorf("expected hello.ok, got %q", stringField(answer, "t"))
	}

	c.mu.Lock()
	c.conn = ws
	c.connectedAt = time.Now()
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.conn == ws {
			c.conn = nil
			c.connectedAt = time.Time{}
		}
		c.mu.Unlock()
	}()

	heartbeatCtx, stopHeartbeat := context.WithCancel(parent)
	defer stopHeartbeat()
	go c.heartbeat(heartbeatCtx, ws)
	bridgeCtx, stopBridge := context.WithCancel(parent)
	defer stopBridge()
	go c.bridgeEvents(bridgeCtx)

	c.log.Info("cloud relay connected", "server", c.serverURL, "device_id", c.identity.DeviceID())
	err = c.readLoop(parent, ws)
	return true, err
}

func (c *Connector) readLoop(ctx context.Context, ws *websocket.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Keep reading control frames while an RPC handler or response is slow.
	// A single worker preserves mutation ordering; its queue is session-scoped
	// and bounded so reconnects cannot replay an unbounded backlog of work.
	messages := make(chan map[string]any, 32)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-messages:
				if ctx.Err() != nil {
					return
				}
				if err := c.handleMessage(ctx, ws, message); err != nil && ctx.Err() == nil {
					c.log.Warn("cloud message failed", "err", err)
				}
			}
		}
	}()
	for {
		_, raw, err := ws.Read(ctx)
		if err != nil {
			return err
		}
		var message map[string]any
		if err := json.Unmarshal(raw, &message); err != nil {
			c.log.Warn("relay sent invalid json", "err", err)
			continue
		}
		if stringField(message, "t") == "msg" {
			select {
			case messages <- message:
			default:
				return errors.New("cloud: incoming RPC queue is full")
			}
			continue
		}
		if err := c.handleMessage(ctx, ws, message); err != nil {
			c.log.Warn("cloud message failed", "err", err)
		}
	}
}

func (c *Connector) handleMessage(ctx context.Context, ws *websocket.Conn, message map[string]any) error {
	switch stringField(message, "t") {
	case "pair.start.ok":
		c.mu.Lock()
		waiter := c.pairStartCh
		c.mu.Unlock()
		if waiter != nil {
			select {
			case waiter <- message:
			default:
			}
		}
	case "pair.request":
		clientPub, err := protocol.FromBase64(stringField(message, "pub"))
		if err != nil {
			return err
		}
		if len(clientPub) != ed25519.PublicKeySize {
			return errors.New("pair.request public key is not 32 bytes")
		}
		clientID := protocol.DeviceID(clientPub)
		if provided := stringField(message, "client_id"); provided != "" && provided != clientID {
			return errors.New("pair.request client_id does not match its public key")
		}
		words, err := protocol.SASWords(c.identity.keys.PublicKey, clientPub)
		if err != nil {
			return err
		}
		request := PairingRequest{
			ClientID:    clientID,
			PublicKey:   stringField(message, "pub"),
			Name:        stringField(message, "name"),
			Platform:    stringField(message, "platform"),
			Fingerprint: protocol.Fingerprint(clientPub),
			SAS:         words,
			ReceivedAt:  time.Now().UTC(),
		}
		c.mu.Lock()
		c.pending[request.ClientID] = request
		c.mu.Unlock()
		select {
		case c.requests <- request:
		default:
			c.log.Warn("pair request queue is full", "client", request.ClientID)
		}
	case "pair.revoked":
		if err := c.peers.Remove(stringField(message, "peer_id")); err != nil {
			c.log.Warn("remove revoked peer", "err", err)
		}
	case "error":
		c.log.Warn("relay error",
			"code", stringField(message, "code"), "message", stringField(message, "message"))
	case "ping":
		return c.writeJSON(ctx, ws, map[string]any{"t": "pong"})
	case "msg":
		return c.handleEnvelope(ctx, ws, message)
	}
	return nil
}

func (c *Connector) handleEnvelope(ctx context.Context, ws *websocket.Conn, message map[string]any) error {
	envelope, ok := envelopeFromMap(message)
	if !ok {
		return errors.New("invalid envelope")
	}
	if envelope.To != c.identity.DeviceID() {
		return errors.New("envelope is not addressed to this computer")
	}
	peer, paired := c.peers.Get(envelope.From)
	if !paired {
		return fmt.Errorf("envelope from unpaired device %q", envelope.From)
	}
	peerPublicKey, err := decodePublicKey(peer.PublicKey)
	if err != nil {
		return err
	}
	plaintext, err := openPayload(c.identity, peerPublicKey, envelope)
	if err != nil {
		return err
	}
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(plaintext, &header); err != nil {
		return err
	}
	if payloads, handled := dispatchFilePayload(ctx, c.handler, peer.DeviceID, plaintext); handled {
		for _, payload := range payloads {
			response, err := sealPayload(c.identity, peer, payload)
			if err != nil {
				return err
			}
			if err := c.writeJSON(ctx, ws, envelopeMap(response)); err != nil {
				return err
			}
		}
		return nil
	}
	switch header.Type {
	case "rpc":
		result := dispatchRPC(ctx, c.handler, plaintext)
		var payload any
		if err := json.Unmarshal(result, &payload); err != nil {
			return err
		}
		response, err := sealPayload(c.identity, peer, payload)
		if err != nil {
			return err
		}
		return c.writeJSON(ctx, ws, envelopeMap(response))
	case "event", "rpc_result":
		return nil
	default:
		return fmt.Errorf("unknown payload type %q", header.Type)
	}
}

// StartPairing asks the relay for a token and 6-digit code.
func (c *Connector) StartPairing(ctx context.Context) (PairInvite, error) {
	if !c.Connected() {
		return PairInvite{}, ErrNotConnected
	}
	ch := make(chan map[string]any, 1)
	c.mu.Lock()
	if c.pairStartCh != nil {
		c.mu.Unlock()
		return PairInvite{}, errors.New("cloud: another pairing request is in flight")
	}
	c.pairStartCh = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.pairStartCh == ch {
			c.pairStartCh = nil
		}
		c.mu.Unlock()
	}()
	if err := c.send(ctx, map[string]any{"t": "pair.start"}); err != nil {
		return PairInvite{}, err
	}
	select {
	case reply := <-ch:
		invite := PairInvite{
			Server:      c.serverURL,
			Token:       stringField(reply, "pair_token"),
			Code6:       stringField(reply, "code6"),
			ExpiresIn:   int(numberField(reply, "expires_in")),
			HostID:      c.identity.DeviceID(),
			Fingerprint: c.identity.Fingerprint(),
		}
		invite.URI = protocol.PairURI(protocol.PairingInvite{
			Server:      invite.Server,
			Token:       invite.Token,
			HostID:      invite.HostID,
			Fingerprint: invite.Fingerprint,
		})
		return invite, nil
	case <-ctx.Done():
		return PairInvite{}, ctx.Err()
	case <-time.After(10 * time.Second):
		return PairInvite{}, errors.New("cloud: relay did not answer pair.start")
	}
}

func (c *Connector) Accept(ctx context.Context, clientID string) error {
	c.mu.Lock()
	request, ok := c.pending[clientID]
	c.mu.Unlock()
	if !ok {
		return errors.New("cloud: no such pairing request")
	}
	if err := c.send(ctx, map[string]any{
		"t": "pair.accept", "client_id": clientID,
		"lan_urls": LANWebSocketURLs(),
	}); err != nil {
		return err
	}
	if err := c.peers.Add(Peer{
		DeviceID:  request.ClientID,
		PublicKey: request.PublicKey,
		Name:      request.Name,
		Platform:  request.Platform,
		PairedAt:  time.Now().UTC(),
	}); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.pending, clientID)
	c.mu.Unlock()
	return nil
}

func (c *Connector) Reject(ctx context.Context, clientID string) error {
	if err := c.send(ctx, map[string]any{"t": "pair.reject", "client_id": clientID}); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.pending, clientID)
	c.mu.Unlock()
	return nil
}

// SendEvent encrypts an event for every paired phone. The relay only sees the
// envelope.
func (c *Connector) SendEvent(ctx context.Context, name string, data any) error {
	_, err := c.SendEventExcept(ctx, name, data, nil)
	return err
}

// SendEventExcept sends an encrypted event to every paired phone except the
// device IDs the caller already reached over a LAN socket.
func (c *Connector) SendEventExcept(
	ctx context.Context,
	name string,
	data any,
	skip map[string]bool,
) (int, error) {
	var firstErr error
	targets := 0
	sent := 0
	for _, peer := range c.peers.List() {
		if skip[peer.DeviceID] {
			continue
		}
		targets++
		envelope, err := sealPayload(c.identity, peer, eventPayload{Type: "event", Name: name, Data: data})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := c.send(ctx, envelopeMap(envelope)); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sent++
	}
	if targets > 0 && sent == 0 && firstErr == nil {
		firstErr = ErrNotConnected
	}
	return sent, firstErr
}

func (c *Connector) send(ctx context.Context, value any) error {
	c.mu.Lock()
	ws := c.conn
	c.mu.Unlock()
	if ws == nil {
		return ErrNotConnected
	}
	return c.writeJSON(ctx, ws, value)
}

func (c *Connector) heartbeat(ctx context.Context, ws *websocket.Conn) {
	ticker := time.NewTicker(connectorHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			// Ping waits for readLoop to consume its pong. Holding writeMu here
			// can block that reader when it replies to an earlier message.
			// websocket.Conn serializes control frames with application writes.
			err := ws.Ping(pingCtx)
			cancel()
			if err != nil {
				_ = ws.Close(websocket.StatusPolicyViolation, "heartbeat timeout")
				return
			}
		}
	}
}

func (c *Connector) writeJSON(ctx context.Context, ws *websocket.Conn, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return ws.Write(writeCtx, websocket.MessageText, raw)
}

func readJSON(ctx context.Context, ws *websocket.Conn, timeout time.Duration) (map[string]any, error) {
	readCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, raw, err := ws.Read(readCtx)
	if err != nil {
		return nil, err
	}
	var message map[string]any
	if err := json.Unmarshal(raw, &message); err != nil {
		return nil, err
	}
	return message, nil
}

func numberField(message map[string]any, key string) float64 {
	value, _ := message[key].(float64)
	return value
}
