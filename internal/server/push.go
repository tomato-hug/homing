package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	firebaseMessagingScope = "https://www.googleapis.com/auth/firebase.messaging"
	defaultFCMBaseURL      = "https://fcm.googleapis.com"
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

// PushDispatcher sends FCM HTTP v1 wake-ups and debounces them per device. The
// only payload is data.type=wake; the message body never contains text from a
// session.
type PushDispatcher struct {
	db              *DB
	log             *slog.Logger
	client          *http.Client
	credentialsPath string
	fcmBaseURL      string
	debounce        time.Duration
	now             func() time.Time

	mu         sync.Mutex
	lastWakeAt map[string]time.Time

	tokenMu     sync.Mutex
	tokenSource oauth2.TokenSource
	projectID   string
}

// NewPushDispatcher takes the path to a Google service-account JSON file
// (HOMING_FCM_CREDENTIALS). An empty path disables push and is not an error.
func NewPushDispatcher(db *DB, log *slog.Logger, credentialsPath string) *PushDispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &PushDispatcher{
		db:              db,
		log:             log,
		client:          &http.Client{Timeout: 15 * time.Second},
		credentialsPath: credentialsPath,
		fcmBaseURL:      defaultFCMBaseURL,
		debounce:        30 * time.Second,
		now:             time.Now,
		lastWakeAt:      make(map[string]time.Time),
	}
}

func (p *PushDispatcher) Wake(ctx context.Context, deviceID string) WakeResult {
	now := p.now()
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
		result.Results = append(result.Results, p.sendFCM(ctx, deviceID, item.Token))
	}
	return result
}

func (p *PushDispatcher) sendFCM(ctx context.Context, deviceID, token string) map[string]any {
	if p.credentialsPath == "" {
		return map[string]any{
			"provider": "fcm",
			"skipped":  true,
			"reason":   "HOMING_FCM_CREDENTIALS not configured",
		}
	}
	accessToken, projectID, err := p.accessToken(ctx)
	if err != nil {
		p.log.Warn("FCM credentials failed", "err", err)
		return map[string]any{"provider": "fcm", "ok": false, "error": err.Error()}
	}
	endpoint := strings.TrimSuffix(p.fcmBaseURL, "/") +
		"/v1/projects/" + url.PathEscape(projectID) + "/messages:send"
	body, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": token,
			"data":  map[string]string{"type": "wake"},
		},
	})
	if err != nil {
		return map[string]any{"provider": "fcm", "ok": false, "error": err.Error()}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return map[string]any{"provider": "fcm", "ok": false, "error": err.Error()}
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Content-Type", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		p.log.Warn("FCM wake failed", "err", err)
		return map[string]any{"provider": "fcm", "ok": false, "error": err.Error()}
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return map[string]any{"provider": "fcm", "ok": true, "status": response.StatusCode}
	}
	errorCode, errorStatus := fcmError(raw)
	if response.StatusCode == http.StatusNotFound || errorCode == "UNREGISTERED" || errorStatus == "UNREGISTERED" {
		deleted := false
		if err := p.db.DeletePushToken(ctx, deviceID, "fcm"); err != nil {
			p.log.Warn("delete unregistered push token", "device", deviceID, "err", err)
		} else {
			deleted = true
		}
		return map[string]any{
			"provider":     "fcm",
			"ok":           false,
			"status":       response.StatusCode,
			"error":        "UNREGISTERED",
			"unregistered": true,
			"deleted":      deleted,
		}
	}
	return map[string]any{
		"provider": "fcm",
		"ok":       false,
		"status":   response.StatusCode,
		"error":    fmt.Sprintf("FCM returned %d: %s", response.StatusCode, summarize(raw)),
	}
}

// accessToken loads the service account once and lets oauth2 refresh the token
// when it is close to expiry; subsequent wake-ups reuse the cached token.
func (p *PushDispatcher) accessToken(ctx context.Context) (string, string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if p.tokenSource == nil {
		raw, err := os.ReadFile(p.credentialsPath)
		if err != nil {
			return "", "", fmt.Errorf("read HOMING_FCM_CREDENTIALS: %w", err)
		}
		var file struct {
			ProjectID string `json:"project_id"`
		}
		if err := json.Unmarshal(raw, &file); err != nil {
			return "", "", fmt.Errorf("parse HOMING_FCM_CREDENTIALS: %w", err)
		}
		if file.ProjectID == "" {
			return "", "", fmt.Errorf("HOMING_FCM_CREDENTIALS has no project_id")
		}
		credentials, err := google.CredentialsFromJSON(ctx, raw, firebaseMessagingScope)
		if err != nil {
			return "", "", fmt.Errorf("load service account: %w", err)
		}
		p.tokenSource = oauth2.ReuseTokenSource(nil, credentials.TokenSource)
		p.projectID = file.ProjectID
	}
	token, err := p.tokenSource.Token()
	if err != nil {
		return "", "", fmt.Errorf("get FCM access token: %w", err)
	}
	return token.AccessToken, p.projectID, nil
}

func fcmError(raw []byte) (errorCode, status string) {
	var payload struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", ""
	}
	for _, detail := range payload.Error.Details {
		if detail.ErrorCode != "" {
			return detail.ErrorCode, payload.Error.Status
		}
	}
	return "", payload.Error.Status
}

func summarize(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 200 {
		return text[:200] + "…"
	}
	return text
}
