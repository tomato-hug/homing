package cloud

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/protocol"
)

const (
	connectorHeartbeat        = 25 * time.Second
	connectorMaxBackoff       = 60 * time.Second
	connectorRPCReadWorkers   = 4
	connectorRPCReadQueue     = 64
	connectorOrderedQueue     = 32
	connectorMaxStaleRPC      = 60 * time.Second
	connectorSeenRIDTTL       = 10 * time.Minute
	connectorMaxSeenRIDs      = 512
	connectorMaxPendingNotify = 32
	connectorPendingNotifyTTL = 10 * time.Minute
)

type queuedEnvelope struct {
	envelope protocol.Envelope
	queuedAt time.Time
}

// ErrNotConnected means the connector has no live relay socket right now.
var ErrNotConnected = errors.New("cloud: not connected to the relay")

// ConnectorConfig wires a computer to a Homing relay (cloud or self-hosted).
type ConnectorConfig struct {
	ServerURL      string
	RelayKey       string
	Identity       *Identity
	Peers          *PeerStore
	Handler        http.Handler
	Logger         *slog.Logger
	UseSystemProxy bool
}

// Connector keeps a WebSocket to the relay, answers encrypted RPC payloads
// against the local /api handler, and forwards events to paired phones.
type Connector struct {
	serverURL      string
	relayKey       string
	identity       *Identity
	peers          *PeerStore
	handler        http.Handler
	log            *slog.Logger
	useSystemProxy bool

	ctx    context.Context
	cancel context.CancelFunc

	mu                sync.Mutex
	conn              *websocket.Conn
	connectedAt       time.Time
	lastRTTMs         int64
	lastError         string
	relayNotice       string
	authRequired      bool
	tunFakeIPDetected bool
	tunFakeIPBypassed bool
	resolvedIP        string
	writeMu           sync.Mutex
	pairStartCh       chan map[string]any
	pending           map[string]PairingRequest
	requests          chan PairingRequest
	seenRIDs          map[string]time.Time
	pendingNotify     []queuedEnvelope
	files             fileAssembler
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
	relayKey := strings.TrimSpace(cfg.RelayKey)
	if relayKey == "" {
		relayKey = strings.TrimSpace(os.Getenv("HOMING_RELAY_KEY"))
	}
	useProxy := cfg.UseSystemProxy || strings.TrimSpace(os.Getenv("HOMING_RELAY_USE_PROXY")) == "1"
	ctx, cancel := context.WithCancel(context.Background())
	return &Connector{
		serverURL:      serverURL,
		relayKey:       relayKey,
		identity:       cfg.Identity,
		peers:          cfg.Peers,
		handler:        cfg.Handler,
		log:            log,
		useSystemProxy: useProxy,
		ctx:            ctx,
		cancel:         cancel,
		pending:        make(map[string]PairingRequest),
		requests:       make(chan PairingRequest, 32),
	}, nil
}

func (c *Connector) ServerURL() string { return c.serverURL }

func (c *Connector) DirectDial() bool { return !c.useSystemProxy }

func (c *Connector) LastError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastError
}

// RelayNotice returns the latest account/quota message the relay sent.
func (c *Connector) RelayNotice() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.relayNotice
}

// AuthRequired reports whether the connected managed relay still requires a
// bound Relay Key before the paired phone can exchange encrypted traffic.
func (c *Connector) AuthRequired() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authRequired
}

const managedAuthNotice = "当前中继已启用官方账号验证：请前往「连接与能力 → 云中转设置 → 打开中继账号中心 (/account)」获取并填写 Relay Key，保存重连后手机即可连通"

// relayPolicyCodes are relay errors the user can act on (renew a key, wait
// for a quota reset, upgrade). They are surfaced in the console.
var relayPolicyCodes = map[string]bool{
	"daily_quota_exceeded":       true,
	"weekly_time_exceeded":       true,
	"payload_too_large_for_tier": true,
	"account_required":           true,
	"account_suspended":          true,
	"invalid_relay_key":          true,
	"host_quota_exceeded":        true,
	"device_quota_exceeded":      true,
}

func (c *Connector) LatencyMs() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastRTTMs
}

func (c *Connector) DialDiagnostics() (fakeDetected bool, fakeBypassed bool, resolvedIP string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tunFakeIPDetected, c.tunFakeIPBypassed, c.resolvedIP
}

func hasLocalProxyEnv() bool {
	for _, key := range []string{
		"HTTPS_PROXY", "https_proxy",
		"HTTP_PROXY", "http_proxy",
		"ALL_PROXY", "all_proxy",
	} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

// isTunFakeIP reports whether ip falls inside standard transparent-proxy Fake-IP
// pools (RFC 2544 198.18.0.0/15 for IPv4 or fc00::/18 for IPv6 used by Clash,
// Mihomo, Surge Enhanced Mode, and Sing-box).
func isTunFakeIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 198 && (ip4[1] == 18 || ip4[1] == 19)
	}
	if len(ip) == net.IPv6len {
		return ip[0] == 0xfc && (ip[1]&0xc0) == 0x00
	}
	return false
}

func resolveHostBypassingFakeIP(
	ctx context.Context,
	host string,
	systemLookup func(context.Context, string) ([]net.IPAddr, error),
	directLookup func(context.Context, string) ([]net.IPAddr, error),
) (targetIP string, fakeDetected bool, fakeBypassed bool, err error) {
	if parsed := net.ParseIP(host); parsed != nil {
		return parsed.String(), isTunFakeIP(parsed), false, nil
	}
	addrs, sysErr := systemLookup(ctx, host)
	var firstFake string
	for _, a := range addrs {
		if isTunFakeIP(a.IP) {
			if firstFake == "" {
				firstFake = a.IP.String()
			}
			continue
		}
		if a.IP != nil {
			return a.IP.String(), false, false, nil
		}
	}
	if firstFake == "" && sysErr != nil {
		return "", false, false, sysErr
	}
	if firstFake != "" {
		fakeDetected = true
		if directLookup != nil {
			if realAddrs, dErr := directLookup(ctx, host); dErr == nil {
				for _, a := range realAddrs {
					if a.IP != nil && !isTunFakeIP(a.IP) {
						return a.IP.String(), true, true, nil
					}
				}
			}
		}
		return firstFake, true, false, nil
	}
	return "", false, false, fmt.Errorf("no IP addresses resolved for %s", host)
}

func queryDirectPublicDNS(ctx context.Context, host string) ([]net.IPAddr, error) {
	// Query domestic public DNS resolvers directly so Mainland China relay domains
	// resolve to their real public IP rather than a local Clash/Surge TUN Fake-IP.
	resolvers := []string{"223.5.5.5:53", "119.29.29.29:53", "1.1.1.1:53"}
	var lastErr error
	for _, server := range resolvers {
		srv := server
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(dCtx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 2500 * time.Millisecond}
				return d.DialContext(dCtx, network, srv)
			},
		}
		qCtx, cancel := context.WithTimeout(ctx, 2800*time.Millisecond)
		addrs, err := r.LookupIPAddr(qCtx, host)
		cancel()
		if err == nil && len(addrs) > 0 {
			for _, a := range addrs {
				if a.IP != nil && !isTunFakeIP(a.IP) {
					return addrs, nil
				}
			}
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("direct DNS returned no real IP for %s", host)
}

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
		sessionStart := time.Now()
		connected, err := c.runSession(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.mu.Lock()
			c.lastError = err.Error()
			c.mu.Unlock()
		}
		if c.AuthRequired() {
			backoff = connectorMaxBackoff
		} else if connected && time.Since(sessionStart) >= 15*time.Second {
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

func (c *Connector) directDialHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   12 * time.Second,
		KeepAlive: 25 * time.Second,
	}
	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return dialer.DialContext(ctx, network, addr)
		}
		resolvedIP, fakeDetected, fakeBypassed, resolveErr := resolveHostBypassingFakeIP(
			ctx,
			host,
			net.DefaultResolver.LookupIPAddr,
			queryDirectPublicDNS,
		)
		if resolveErr == nil && resolvedIP != "" {
			c.mu.Lock()
			c.tunFakeIPDetected = fakeDetected
			c.tunFakeIPBypassed = fakeBypassed
			c.resolvedIP = resolvedIP
			c.mu.Unlock()
			return dialer.DialContext(ctx, network, net.JoinHostPort(resolvedIP, port))
		}
		return dialer.DialContext(ctx, network, addr)
	}

	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := base.Clone()
		cloned.Proxy = nil
		cloned.DialContext = dialContext
		return &http.Client{Transport: cloned}
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:       nil,
			DialContext: dialContext,
		},
	}
}

func (c *Connector) dialOptions() *websocket.DialOptions {
	opts := &websocket.DialOptions{}
	if !c.useSystemProxy {
		opts.HTTPClient = c.directDialHTTPClient()
	}
	if c.relayKey != "" {
		opts.HTTPHeader = http.Header{
			"Authorization": []string{"Bearer " + c.relayKey},
		}
	}
	if opts.HTTPClient == nil && len(opts.HTTPHeader) == 0 {
		return nil
	}
	return opts
}

func (c *Connector) runSession(parent context.Context) (bool, error) {
	dialCtx, cancel := context.WithTimeout(parent, 15*time.Second)
	ws, _, err := websocket.Dial(dialCtx, c.serverURL, c.dialOptions())
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
	helloStart := time.Now()
	ts := helloStart.UTC().Format(time.RFC3339Nano)
	signature, err := protocol.HelloSignature(c.identity.keys.PrivateKey, stringField(challenge, "nonce"), ts)
	if err != nil {
		return false, fmt.Errorf("sign hello: %w", err)
	}
	helloPayload := map[string]any{
		"t":         "hello",
		"device_id": c.identity.DeviceID(),
		"pub":       c.identity.PublicKeyBase64(),
		"ts":        ts,
		"sig":       protocol.ToBase64(signature),
		"name":      platform.HostName(),
	}
	if err := c.writeJSON(parent, ws, helloPayload); err != nil {
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
	bound, hasBound := answer["bound"].(bool)
	unbound := (hasBound && !bound) || (!hasBound && c.relayKey == "" && c.probeManagedRelay(parent))
	initialRTT := time.Since(helloStart).Milliseconds()
	if initialRTT < 1 {
		initialRTT = 1
	}

	c.mu.Lock()
	c.conn = ws
	c.connectedAt = time.Now()
	c.lastRTTMs = initialRTT
	c.lastError = ""
	c.authRequired = unbound
	if unbound {
		c.relayNotice = managedAuthNotice
	} else if c.relayNotice == managedAuthNotice {
		c.relayNotice = ""
	}
	c.mu.Unlock()
	defer func() {
		c.files.clear()
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
	go c.flushPendingNotifications(heartbeatCtx, ws)
	bridgeCtx, stopBridge := context.WithCancel(parent)
	defer stopBridge()
	go c.bridgeEvents(bridgeCtx)

	c.log.Info("cloud relay connected", "server", c.serverURL, "device_id", c.identity.DeviceID())
	err = c.readLoop(parent, ws)
	return true, err
}

type decodedEnvelopeWork struct {
	envelope  protocol.Envelope
	peer      Peer
	plaintext []byte
	kind      string
}

func (c *Connector) readLoop(ctx context.Context, ws *websocket.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	var workerWG sync.WaitGroup
	defer func() {
		cancel()
		workerWG.Wait()
	}()
	// Keep ws.Read non-blocking so control frames (WebSocket ping/pong) are
	// always answered immediately even under heavy RPC load.
	// Read-only RPCs (GET/HEAD/OPTIONS) run on a bounded concurrent worker pool
	// while mutations and file transfers stay serialized on a single worker.
	incoming := make(chan map[string]any, connectorRPCReadQueue)
	readJobs := make(chan decodedEnvelopeWork, connectorRPCReadQueue)
	orderedJobs := make(chan decodedEnvelopeWork, connectorOrderedQueue)

	for i := 0; i < connectorRPCReadWorkers; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case work := <-readJobs:
					if ctx.Err() != nil {
						return
					}
					if err := c.executeEnvelopeWork(ctx, ws, work); err != nil && ctx.Err() == nil {
						c.log.Warn("cloud read rpc failed", "err", err)
					}
				}
			}
		}()
	}
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case work := <-orderedJobs:
				if ctx.Err() != nil {
					return
				}
				if err := c.executeEnvelopeWork(ctx, ws, work); err != nil && ctx.Err() == nil {
					c.log.Warn("cloud ordered message failed", "err", err)
				}
			}
		}
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-incoming:
				if ctx.Err() != nil {
					return
				}
				c.routeIncomingEnvelope(ctx, ws, message, readJobs, orderedJobs)
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
			case incoming <- message:
			default:
				c.log.Warn("cloud incoming queue full, dropping envelope", "id", stringField(message, "id"))
				if id := stringField(message, "id"); id != "" {
					_ = c.ackEnvelope(ctx, ws, id)
				}
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
		peerID := stringField(message, "peer_id")
		c.files.dropPeer(peerID)
		if err := c.peers.Remove(peerID); err != nil {
			c.log.Warn("remove revoked peer", "err", err)
		}
	case "error":
		code := stringField(message, "code")
		text := stringField(message, "message")
		c.log.Warn("relay error", "code", code, "message", text)
		if relayPolicyCodes[code] {
			if text == "" {
				text = code
			}
			c.mu.Lock()
			c.relayNotice = time.Now().Format("01-02 15:04") + " · " + text
			if code == "account_required" || code == "invalid_relay_key" || code == "account_suspended" {
				c.authRequired = true
			}
			c.mu.Unlock()
		}
	case "ping":
		return c.writeJSON(ctx, ws, map[string]any{"t": "pong"})
	case "msg":
		return c.handleEnvelope(ctx, ws, message)
	}
	return nil
}

func (c *Connector) routeIncomingEnvelope(
	ctx context.Context,
	ws *websocket.Conn,
	message map[string]any,
	readJobs chan<- decodedEnvelopeWork,
	orderedJobs chan<- decodedEnvelopeWork,
) {
	envelope, ok := envelopeFromMap(message)
	if !ok {
		if id := stringField(message, "id"); id != "" {
			_ = c.ackEnvelope(ctx, ws, id)
		}
		return
	}
	if envelope.To != c.identity.DeviceID() {
		_ = c.ackEnvelope(ctx, ws, envelope.ID)
		return
	}
	peer, paired := c.peers.Get(envelope.From)
	if !paired {
		c.log.Warn("cloud envelope from unpaired device", "from", envelope.From)
		_ = c.ackEnvelope(ctx, ws, envelope.ID)
		return
	}
	peerPublicKey, err := decodePublicKey(peer.PublicKey)
	if err != nil {
		_ = c.ackEnvelope(ctx, ws, envelope.ID)
		return
	}
	plaintext, err := openPayload(c.identity, peerPublicKey, envelope)
	if err != nil {
		c.log.Warn("cloud envelope decrypt failed", "from", envelope.From, "err", err)
		_ = c.ackEnvelope(ctx, ws, envelope.ID)
		return
	}
	var header struct {
		Type   string `json:"type"`
		RID    string `json:"rid"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(plaintext, &header); err != nil {
		_ = c.ackEnvelope(ctx, ws, envelope.ID)
		return
	}
	if header.Type == "rpc" {
		if isStaleEnvelope(envelope.TS, time.Now().UTC(), connectorMaxStaleRPC) {
			c.log.Info("dropping stale replayed cloud rpc", "rid", header.RID, "ts", envelope.TS)
			_ = c.ackEnvelope(ctx, ws, envelope.ID)
			return
		}
		if header.RID != "" && !c.claimRID(header.RID, time.Now().UTC()) {
			c.log.Info("dropping duplicate cloud rpc", "rid", header.RID)
			_ = c.ackEnvelope(ctx, ws, envelope.ID)
			return
		}
	}
	work := decodedEnvelopeWork{
		envelope:  envelope,
		peer:      peer,
		plaintext: plaintext,
		kind:      header.Type,
	}
	method := strings.ToUpper(strings.TrimSpace(header.Method))
	isReadRPC := header.Type == "rpc" && (method == "" || method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions)
	if isReadRPC {
		select {
		case readJobs <- work:
		default:
			c.log.Warn("cloud read rpc queue full", "rid", header.RID)
			_ = c.ackEnvelope(ctx, ws, envelope.ID)
		}
		return
	}
	select {
	case orderedJobs <- work:
	default:
		c.log.Warn("cloud ordered queue full", "rid", header.RID, "type", header.Type)
		_ = c.ackEnvelope(ctx, ws, envelope.ID)
	}
}

func (c *Connector) executeEnvelopeWork(ctx context.Context, ws *websocket.Conn, work decodedEnvelopeWork) error {
	defer func() {
		_ = c.ackEnvelope(ctx, ws, work.envelope.ID)
	}()
	if payloads, handled := c.files.dispatch(ctx, c.handler, work.peer.DeviceID, work.plaintext); handled {
		for _, payload := range payloads {
			response, err := sealPayload(c.identity, work.peer, payload)
			if err != nil {
				return err
			}
			if err := c.writeJSON(ctx, ws, envelopeMap(response)); err != nil {
				return err
			}
		}
		return nil
	}
	switch work.kind {
	case "rpc":
		result := dispatchRPC(ctx, c.handler, work.plaintext)
		var payload any
		if err := json.Unmarshal(result, &payload); err != nil {
			return err
		}
		response, err := sealPayload(c.identity, work.peer, payload)
		if err != nil {
			return err
		}
		return c.writeJSON(ctx, ws, envelopeMap(response))
	case "event", "rpc_result":
		return nil
	default:
		return fmt.Errorf("unknown payload type %q", work.kind)
	}
}

func (c *Connector) handleEnvelope(ctx context.Context, ws *websocket.Conn, message map[string]any) error {
	envelope, ok := envelopeFromMap(message)
	if !ok {
		return errors.New("invalid envelope")
	}
	defer func() {
		_ = c.ackEnvelope(ctx, ws, envelope.ID)
	}()
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
		RID  string `json:"rid"`
	}
	if err := json.Unmarshal(plaintext, &header); err != nil {
		return err
	}
	if header.Type == "rpc" {
		if isStaleEnvelope(envelope.TS, time.Now().UTC(), connectorMaxStaleRPC) {
			return nil
		}
		if header.RID != "" && !c.claimRID(header.RID, time.Now().UTC()) {
			return nil
		}
	}
	return c.executeEnvelopeWork(ctx, ws, decodedEnvelopeWork{
		envelope:  envelope,
		peer:      peer,
		plaintext: plaintext,
		kind:      header.Type,
	})
}

func (c *Connector) ackEnvelope(ctx context.Context, ws *websocket.Conn, id string) error {
	if id == "" || ws == nil {
		return nil
	}
	return c.writeJSON(ctx, ws, map[string]any{"t": "ack", "id": id})
}

func isStaleEnvelope(tsRaw string, now time.Time, maxAge time.Duration) bool {
	if tsRaw == "" || maxAge <= 0 {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, tsRaw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, tsRaw)
		if err != nil {
			return false
		}
	}
	return now.Sub(parsed) > maxAge
}

func (c *Connector) claimRID(rid string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seenRIDs == nil {
		c.seenRIDs = make(map[string]time.Time)
	}
	for existing, ts := range c.seenRIDs {
		if now.Sub(ts) > connectorSeenRIDTTL {
			delete(c.seenRIDs, existing)
		}
	}
	if _, exists := c.seenRIDs[rid]; exists {
		return false
	}
	if len(c.seenRIDs) >= connectorMaxSeenRIDs {
		var oldestKey string
		var oldestTime time.Time
		for k, ts := range c.seenRIDs {
			if oldestKey == "" || ts.Before(oldestTime) {
				oldestKey = k
				oldestTime = ts
			}
		}
		if oldestKey != "" {
			delete(c.seenRIDs, oldestKey)
		}
	}
	c.seenRIDs[rid] = now
	return true
}

func (c *Connector) enqueuePendingNotify(envelope protocol.Envelope) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	kept := c.pendingNotify[:0]
	for _, item := range c.pendingNotify {
		if now.Sub(item.queuedAt) <= connectorPendingNotifyTTL {
			kept = append(kept, item)
		}
	}
	c.pendingNotify = append(kept, queuedEnvelope{envelope: envelope, queuedAt: now})
	if len(c.pendingNotify) > connectorMaxPendingNotify {
		c.pendingNotify = append([]queuedEnvelope(nil), c.pendingNotify[len(c.pendingNotify)-connectorMaxPendingNotify:]...)
	}
}

func (c *Connector) flushPendingNotifications(ctx context.Context, ws *websocket.Conn) {
	c.mu.Lock()
	if len(c.pendingNotify) == 0 {
		c.mu.Unlock()
		return
	}
	batch := append([]queuedEnvelope(nil), c.pendingNotify...)
	c.pendingNotify = nil
	c.mu.Unlock()

	now := time.Now().UTC()
	var remaining []queuedEnvelope
	for i, item := range batch {
		if now.Sub(item.queuedAt) > connectorPendingNotifyTTL {
			continue
		}
		if err := c.writeJSON(ctx, ws, envelopeMap(item.envelope)); err != nil {
			remaining = append(remaining, batch[i:]...)
			break
		}
	}
	if len(remaining) > 0 {
		c.mu.Lock()
		c.pendingNotify = append(remaining, c.pendingNotify...)
		if len(c.pendingNotify) > connectorMaxPendingNotify {
			c.pendingNotify = c.pendingNotify[len(c.pendingNotify)-connectorMaxPendingNotify:]
		}
		c.mu.Unlock()
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
			if name == "notify" {
				c.enqueuePendingNotify(envelope)
			}
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
			pingStart := time.Now()
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
			rtt := time.Since(pingStart).Milliseconds()
			if rtt < 1 {
				rtt = 1
			}
			c.mu.Lock()
			if c.conn == ws {
				c.lastRTTMs = rtt
			}
			c.mu.Unlock()
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

func (c *Connector) probeManagedRelay(parent context.Context) bool {
	u, err := url.Parse(c.serverURL)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	default:
		return false
	}
	u.Path = "/v1/account/config"
	u.RawQuery = ""
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 3 * time.Second}
	if !c.useSystemProxy {
		client = c.directDialHTTPClient()
		client.Timeout = 3 * time.Second
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var payload struct {
		AuthMode string `json:"auth_mode"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false
	}
	return payload.AuthMode == "managed"
}

func numberField(message map[string]any, key string) float64 {
	value, _ := message[key].(float64)
	return value
}
