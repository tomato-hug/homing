package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// SupportedProviders lists the push providers the protocol accepts. Only FCM is
// implemented today; the vendor channels are registered so a client can report a
// token without the server rejecting it.
var SupportedProviders = map[string]bool{
	"fcm":    true,
	"oppo":   true,
	"xiaomi": true,
	"huawei": true,
	"honor":  true,
	"vivo":   true,
}

// Waker sends content-free wake-ups to an offline device.
type Waker interface {
	Wake(ctx context.Context, deviceID string) WakeResult
}

// WakeResult is what a Waker did, mostly for tests and logs.
type WakeResult struct {
	Skipped string `json:"skipped,omitempty"`
	Results []any  `json:"results,omitempty"`
	Error   string `json:"error,omitempty"`
}

// PushDispatcher sends FCM wake-ups (legacy HTTP API, same as the Node server)
// and debounces them per device.
type PushDispatcher struct {
	db         *DB
	log        *slog.Logger
	client     *http.Client
	apiKey     string
	debounce   time.Duration
	mu         sync.Mutex
	lastWakeAt map[string]time.Time
}

func NewPushDispatcher(db *DB, log *slog.Logger, apiKey string) *PushDispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &PushDispatcher{
		db:         db,
		log:        log,
		client:     &http.Client{Timeout: 10 * time.Second},
		apiKey:     apiKey,
		debounce:   30 * time.Second,
		lastWakeAt: make(map[string]time.Time),
	}
}

func (p *PushDispatcher) Wake(ctx context.Context, deviceID string) WakeResult {
	now := time.Now()
	p.mu.Lock()
	if last, ok := p.lastWakeAt[deviceID]; ok && now.Sub(last) < p.debounce {
		p.mu.Unlock()
		return WakeResult{Skipped: "debounced"}
	}
	p.lastWakeAt[deviceID] = now
	p.mu.Unlock()

	tokens, err := p.db.PushTokens(ctx, deviceID)
	if err != nil {
		p.log.Warn("read push tokens", "device", deviceID, "err", err)
		return WakeResult{Error: err.Error()}
	}
	if len(tokens) == 0 {
		return WakeResult{Skipped: "no_token"}
	}
	result := WakeResult{}
	for _, item := range tokens {
		if item.Provider != "fcm" {
			p.log.Info("push adapter not configured", "provider", item.Provider, "device", deviceID)
			result.Results = append(result.Results, map[string]any{
				"provider": item.Provider,
				"skipped":  true,
			})
			continue
		}
		result.Results = append(result.Results, p.sendFCM(ctx, item.Token))
	}
	return result
}

func (p *PushDispatcher) sendFCM(ctx context.Context, token string) map[string]any {
	if p.apiKey == "" {
		return map[string]any{
			"provider": "fcm",
			"skipped":  true,
			"reason":   "FCM_SERVER_KEY not configured",
		}
	}
	body, err := json.Marshal(map[string]any{
		"to":       token,
		"priority": "high",
		"notification": map[string]string{
			"title": "Homing",
			"body":  "有新消息",
		},
		"data": map[string]string{"type": "wake"},
	})
	if err != nil {
		return map[string]any{"provider": "fcm", "ok": false, "error": err.Error()}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://fcm.googleapis.com/fcm/send", bytes.NewReader(body))
	if err != nil {
		return map[string]any{"provider": "fcm", "ok": false, "error": err.Error()}
	}
	request.Header.Set("Authorization", "key="+p.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		p.log.Warn("FCM wake failed", "err", err)
		return map[string]any{"provider": "fcm", "ok": false, "error": err.Error()}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return map[string]any{
			"provider": "fcm",
			"ok":       false,
			"status":   response.StatusCode,
			"error":    fmt.Sprintf("FCM returned %d", response.StatusCode),
		}
	}
	return map[string]any{"provider": "fcm", "ok": true, "status": response.StatusCode}
}
