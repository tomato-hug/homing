package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/platform"
	"github.com/tomato-hug/homing/internal/store"
)

const (
	pairOfferTTL    = 5 * time.Minute
	pairOfferKeep   = 24 * time.Hour
	pairMaxAttempts = 12
	pairMaxDevices  = 24
)

var pairMu sync.Mutex

type pairOfferView struct {
	OfferID         string   `json:"offer_id"`
	Code            string   `json:"code"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"created_at"`
	ExpiresAt       string   `json:"expires_at"`
	TTLSeconds      int64    `json:"ttl_s"`
	LANURL          *string  `json:"lan_url"`
	LANURLs         []string `json:"lan_urls"`
	QRPayload       *string  `json:"qr_payload"`
	ClaimedDeviceID *string  `json:"claimed_device_id"`
	Token           string   `json:"token,omitempty"`
}

type pairDeviceView struct {
	DeviceID   string   `json:"device_id"`
	Name       string   `json:"name"`
	Platform   string   `json:"platform"`
	AppVersion *string  `json:"app_version"`
	CreatedAt  *string  `json:"created_at"`
	LastSeenAt *string  `json:"last_seen_at"`
	Online     bool     `json:"online"`
	PairedVia  string   `json:"paired_via"`
	LastIP     *string  `json:"last_ip"`
	Scopes     []string `json:"scopes"`
}

type pairServerView struct {
	Name      string   `json:"name"`
	Platform  string   `json:"platform"`
	LANURL    *string  `json:"lan_url"`
	LANURLs   []string `json:"lan_urls"`
	Version   string   `json:"version"`
	TokenType string   `json:"token_type"`
}

func init() {
	Register("pair", 20, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("POST /api/pair/offer", d.guard(d.handleCreateOffer))
		mux.HandleFunc("GET /api/pair/latest", d.guard(d.handleLatestOffer))
		mux.HandleFunc("GET /api/pair/offer/{id}", d.guard(d.handleOffer))
		mux.HandleFunc("POST /api/pair/claim", d.handlePairClaim)
		mux.HandleFunc("POST /api/pair/cancel", d.guard(d.handleCancelOffer))
	})
}

func (d *Deps) handleCreateOffer(w http.ResponseWriter, r *http.Request) {
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "打不开配对数据"})
		return
	}
	body := readBody(r)
	lanURL, _ := body["lan_url"].(string)
	offer, err := createPairOffer(s, lanURL, requestPort(r))
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "生成本机配对码失败"})
		return
	}
	WriteJSON(w, http.StatusOK, offer)
}

func (d *Deps) handleLatestOffer(w http.ResponseWriter, r *http.Request) {
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "打不开配对数据"})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"offer": latestPairOffer(s)})
}

func (d *Deps) handleOffer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validPairID(id) {
		WriteJSON(w, http.StatusNotFound, map[string]any{"error": "没有这个配对请求"})
		return
	}
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "打不开配对数据"})
		return
	}
	offer, found := findPairOffer(s, id)
	if !found {
		WriteJSON(w, http.StatusNotFound, map[string]any{"error": "没有这个配对请求"})
		return
	}
	WriteJSON(w, http.StatusOK, publicPairOffer(offer, false))
}

func (d *Deps) handleCancelOffer(w http.ResponseWriter, r *http.Request) {
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "打不开配对数据"})
		return
	}
	body := readBody(r)
	id, _ := body["offer_id"].(string)
	offer, found := findPairOffer(s, id)
	if !found {
		WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "offer": nil})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "offer": publicPairOffer(offer, false)})
}

func (d *Deps) handlePairClaim(w http.ResponseWriter, r *http.Request) {
	if !isLocalOrLAN(r.RemoteAddr) {
		WriteJSON(w, http.StatusForbidden, map[string]any{"error": "只接受本机与局域网来源"})
		return
	}
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "打不开配对数据"})
		return
	}
	body := readBody(r)
	result, status, errText := claimPairOffer(s, body, clientIP(r.RemoteAddr), requestPort(r))
	if errText != "" {
		WriteJSON(w, status, map[string]any{"error": errText, "code": status})
		return
	}
	result["token_type"] = "device"
	WriteJSON(w, http.StatusOK, result)
}

func createPairOffer(s *store.Store, requestedLAN string, port int) (pairOfferView, error) {
	pairMu.Lock()
	defer pairMu.Unlock()

	now := time.Now()
	devices := s.Devices()
	offers := prunedOffers(s.Offers(), now)
	urls := platform.LANURLs(port)
	chosen := requestedLAN
	if chosen == "" {
		chosen = platform.LocalURL(port)
		if len(urls) > 0 {
			chosen = urls[0]
		}
	}
	living := map[string]bool{}
	for _, offer := range offers {
		if offer.ExpiresMs > now.UnixMilli() && offer.ClaimedMs == 0 {
			living[offer.Code] = true
		}
	}
	code := ""
	for i := 0; i < 20; i++ {
		code = randomPairCode()
		if !living[code] {
			break
		}
	}
	token := randomHex(16)
	id := randomHex(8)
	offer := store.Offer{
		ID:        id,
		Token:     token,
		Code:      code,
		CreatedMs: now.UnixMilli(),
		ExpiresMs: now.Add(pairOfferTTL).UnixMilli(),
		LanURL:    chosen,
		LanURLs:   urls,
		QRPayload: pairQRPayload(token, chosen, port),
	}
	kept := make([]store.Offer, 0, len(offers)+1)
	for _, old := range offers {
		if old.ClaimedMs != 0 {
			kept = append(kept, old)
		}
	}
	kept = append(kept, offer)
	if err := s.SaveDevices(devices, kept); err != nil {
		return pairOfferView{}, err
	}
	return publicPairOffer(offer, true), nil
}

func latestPairOffer(s *store.Store) *pairOfferView {
	pairMu.Lock()
	defer pairMu.Unlock()
	now := time.Now().UnixMilli()
	var latest *store.Offer
	for _, offer := range s.Offers() {
		if offer.ClaimedMs != 0 || offer.ExpiresMs <= now {
			continue
		}
		if latest == nil || offer.CreatedMs > latest.CreatedMs {
			copy := offer
			latest = &copy
		}
	}
	if latest == nil {
		return nil
	}
	view := publicPairOffer(*latest, false)
	return &view
}

func findPairOffer(s *store.Store, id string) (store.Offer, bool) {
	pairMu.Lock()
	defer pairMu.Unlock()
	for _, offer := range s.Offers() {
		if offer.ID == id {
			return offer, true
		}
	}
	return store.Offer{}, false
}

func claimPairOffer(s *store.Store, body map[string]any, ip string, port int) (map[string]any, int, string) {
	pairMu.Lock()
	defer pairMu.Unlock()

	now := time.Now()
	devices := s.Devices()
	offers := prunedOffers(s.Offers(), now)
	deviceInput, _ := body["device"].(map[string]any)
	tokenValue, _ := body["token"].(string)
	codeValue, _ := body["code"].(string)
	cleanToken := normalizePairValue(tokenValue)
	cleanCode := normalizePairValue(codeValue)
	if cleanToken == "" && cleanCode == "" {
		return nil, http.StatusBadRequest, "需要 token 或 code"
	}
	index := -1
	for i := range offers {
		offer := offers[i]
		if cleanToken != "" && offer.Token == cleanToken {
			index = i
			break
		}
		if cleanCode != "" && offer.Code == cleanCode {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, http.StatusNotFound, "配对码无效，请在电脑上换一个"
	}
	offer := &offers[index]
	if offer.ClaimedMs != 0 {
		return nil, http.StatusConflict, "这个配对码已经用过了，请在电脑上换一个"
	}
	if offer.ExpiresMs <= now.UnixMilli() {
		return nil, http.StatusGone, "配对码已过期（5 分钟内有效），请换一个"
	}
	offer.Attempts++
	if offer.Attempts > pairMaxAttempts {
		_ = s.SaveDevices(devices, offers)
		return nil, http.StatusTooManyRequests, "尝试次数过多，请重新开始配对"
	}

	deviceToken := "hm_" + base64.RawURLEncoding.EncodeToString(randomBytes(32))
	digest := sha256.Sum256([]byte(deviceToken))
	device := store.Device{
		ID:          randomHex(6),
		Name:        cleanPairName(stringField(deviceInput, "name")),
		Platform:    truncateString(stringField(deviceInput, "platform"), 20),
		AppVersion:  truncateString(stringField(deviceInput, "app_version"), 32),
		CreatedAt:   now.UTC().Format("2006-01-02T15:04:05.000Z"),
		LastSeenMs:  now.UnixMilli(),
		PairedVia:   "code",
		LastIP:      ip,
		TokenSHA256: hex.EncodeToString(digest[:]),
		Scopes:      []string{"read", "reply", "drive"},
	}
	if device.Platform == "" {
		device.Platform = "android"
	}
	if cleanToken != "" {
		device.PairedVia = "qr"
	}
	devices = append(devices, device)
	if len(devices) > pairMaxDevices {
		sort.Slice(devices, func(i, j int) bool { return devices[i].LastSeenMs > devices[j].LastSeenMs })
		devices = devices[:pairMaxDevices]
	}
	offer.ClaimedMs = now.UnixMilli()
	offer.ClaimedDeviceID = device.ID
	if err := s.SaveDevices(devices, offers); err != nil {
		return nil, http.StatusInternalServerError, "保存配对结果失败"
	}

	version := platform.Version
	return map[string]any{
		"ok":           true,
		"device":       publicPairDevice(device),
		"device_token": deviceToken,
		"server": pairServerView{
			Name:      platform.HostName(),
			Platform:  platform.PlatformLabel(),
			LANURL:    stringPointerOrNil(offer.LanURL),
			LANURLs:   offer.LanURLs,
			Version:   version,
			TokenType: "device",
		},
	}, 0, ""
}

func publicPairOffer(offer store.Offer, includeToken bool) pairOfferView {
	now := time.Now().UnixMilli()
	status := "waiting"
	if offer.ClaimedMs != 0 {
		status = "claimed"
	} else if offer.ExpiresMs <= now {
		status = "expired"
	}
	ttl := (offer.ExpiresMs - now) / 1000
	if ttl < 0 {
		ttl = 0
	}
	out := pairOfferView{
		OfferID:         offer.ID,
		Code:            offer.Code,
		Status:          status,
		CreatedAt:       isoMillis(offer.CreatedMs),
		ExpiresAt:       isoMillis(offer.ExpiresMs),
		TTLSeconds:      ttl,
		LANURL:          stringPointerOrNil(offer.LanURL),
		LANURLs:         append([]string(nil), offer.LanURLs...),
		QRPayload:       stringPointerOrNil(offer.QRPayload),
		ClaimedDeviceID: stringPointerOrNil(offer.ClaimedDeviceID),
	}
	if out.LANURLs == nil {
		out.LANURLs = []string{}
	}
	if includeToken {
		out.Token = offer.Token
	}
	return out
}

func publicPairDevice(device store.Device) pairDeviceView {
	now := time.Now().UnixMilli()
	out := pairDeviceView{
		DeviceID:  device.ID,
		Name:      device.Name,
		Platform:  device.Platform,
		Online:    device.LastSeenMs > 0 && now-device.LastSeenMs < 90_000,
		PairedVia: device.PairedVia,
	}
	if out.Name == "" {
		out.Name = "手机"
	}
	if out.Platform == "" {
		out.Platform = "android"
	}
	if out.PairedVia == "" {
		out.PairedVia = "qr"
	}
	if device.AppVersion != "" {
		out.AppVersion = &device.AppVersion
	}
	if device.CreatedAt != "" {
		out.CreatedAt = &device.CreatedAt
	}
	if device.LastSeenMs > 0 {
		value := isoMillis(device.LastSeenMs)
		out.LastSeenAt = &value
	}
	if device.LastIP != "" {
		out.LastIP = &device.LastIP
	}
	out.Scopes = normaliseScopes(device.Scopes)
	return out
}

func prunedOffers(offers []store.Offer, now time.Time) []store.Offer {
	cutoff := now.Add(-pairOfferKeep).UnixMilli()
	out := make([]store.Offer, 0, len(offers))
	for _, offer := range offers {
		if offer.CreatedMs > cutoff {
			out = append(out, offer)
		}
	}
	return out
}

func isLocalOrLAN(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

func randomPairCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "123456"
	}
	return strconv.Itoa(int(n.Int64()) + 100000)
}

func randomHex(n int) string {
	return hex.EncodeToString(randomBytes(n))
}

func randomBytes(n int) []byte {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("homing: crypto/rand unavailable: " + err.Error())
	}
	return buf
}

func pairQRPayload(token, lanURL string, port int) string {
	host := "127.0.0.1:" + strconv.Itoa(port)
	if parsed, err := url.Parse(lanURL); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	return "homing://pair?v=1&host=" + url.QueryEscape(host) + "&token=" + token
}

func normalizePairValue(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "-", "")
	return strings.TrimSpace(value)
}

func validPairID(value string) bool {
	if len(value) != 16 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func cleanPairName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "手机"
	}
	return truncateString(value, 60)
}

func truncateString(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func stringField(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func isoMillis(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

func stringPointerOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
