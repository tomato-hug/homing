package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeFCM struct {
	server     *httptest.Server
	tokenCalls atomic.Int32
	mu         sync.Mutex
	sent       []map[string]any
}

func newFakeFCM(t *testing.T) *fakeFCM {
	t.Helper()
	fake := &fakeFCM{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		fake.tokenCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			http.Error(w, "unexpected grant type", http.StatusBadRequest)
			return
		}
		if r.Form.Get("assertion") == "" {
			http.Error(w, "missing assertion", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("POST /v1/projects/demo-project/messages:send", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		fake.mu.Lock()
		fake.sent = append(fake.sent, body)
		fake.mu.Unlock()
		message, _ := body["message"].(map[string]any)
		if message["token"] == "dead-token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"projects/demo-project/messages/1"}`))
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

func writeServiceAccount(t *testing.T, tokenURL string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	credentials := map[string]any{
		"type":                        "service_account",
		"project_id":                  "demo-project",
		"private_key_id":              "demo-key-id",
		"private_key":                 string(pemKey),
		"client_email":                "demo@demo-project.iam.gserviceaccount.com",
		"client_id":                   "1234567890",
		"auth_uri":                    "https://accounts.google.com/o/oauth2/auth",
		"token_uri":                   tokenURL,
		"auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
		"client_x509_cert_url":        "https://www.googleapis.com/robot/v1/metadata/x509/demo",
	}
	raw, err := json.Marshal(credentials)
	if err != nil {
		t.Fatalf("marshal credentials: %v", err)
	}
	path := filepath.Join(t.TempDir(), "fcm-service-account.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	return path
}

func TestPushDispatcherSendsFCMV1AndDeletesUnregistered(t *testing.T) {
	fake := newFakeFCM(t)
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.SavePushToken(ctx, "demo-device-1", "fcm", "good-token", time.Now()); err != nil {
		t.Fatalf("save good token: %v", err)
	}
	if err := db.SavePushToken(ctx, "demo-device-2", "fcm", "dead-token", time.Now()); err != nil {
		t.Fatalf("save dead token: %v", err)
	}

	dispatcher := NewPushDispatcher(db, slog.New(slog.NewTextHandler(io.Discard, nil)),
		writeServiceAccount(t, fake.server.URL+"/token"))
	dispatcher.fcmBaseURL = fake.server.URL
	dispatcher.debounce = 0

	firstWake := dispatcher.Wake(ctx, "demo-device-1")
	if firstWake.Error != "" {
		t.Fatalf("wake error: %s", firstWake.Error)
	}
	if len(firstWake.Results) != 1 {
		t.Fatalf("results = %#v", firstWake.Results)
	}
	secondWake := dispatcher.Wake(ctx, "demo-device-2")
	if len(secondWake.Results) != 1 {
		t.Fatalf("dead-token results = %#v", secondWake.Results)
	}
	if fake.tokenCalls.Load() != 1 {
		t.Fatalf("token endpoint called %d times, want 1", fake.tokenCalls.Load())
	}

	fake.mu.Lock()
	sent := append([]map[string]any(nil), fake.sent...)
	fake.mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2", len(sent))
	}
	for _, body := range sent {
		message, ok := body["message"].(map[string]any)
		if !ok {
			t.Fatalf("body has no message: %#v", body)
		}
		if len(message) != 2 || message["token"] == nil || message["data"] == nil {
			t.Fatalf("message should only contain token and data: %#v", message)
		}
		data := message["data"].(map[string]any)
		if data["type"] != "wake" {
			t.Fatalf("data = %#v", data)
		}
		if _, hasNotification := body["notification"]; hasNotification {
			t.Fatalf("body contains a notification: %#v", body)
		}
	}

	tokens, err := db.PushTokens(ctx, "demo-device-1")
	if err != nil {
		t.Fatalf("read tokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].Token != "good-token" {
		t.Fatalf("good device tokens = %#v", tokens)
	}
	deadTokens, err := db.PushTokens(ctx, "demo-device-2")
	if err != nil {
		t.Fatalf("read dead device tokens: %v", err)
	}
	if len(deadTokens) != 0 {
		t.Fatalf("unregistered token was not deleted: %#v", deadTokens)
	}

	if err := db.SavePushToken(ctx, "demo-device-3", "fcm", "second-token", time.Now()); err != nil {
		t.Fatalf("save second device: %v", err)
	}
	dispatcher.Wake(ctx, "demo-device-3")
	if fake.tokenCalls.Load() != 1 {
		t.Fatalf("access token was not cached: %d calls", fake.tokenCalls.Load())
	}
}

func TestPushDispatcherWithoutCredentialsSkips(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.SavePushToken(ctx, "demo-device", "fcm", "token", time.Now()); err != nil {
		t.Fatalf("save token: %v", err)
	}
	dispatcher := NewPushDispatcher(db, nil, "")
	result := dispatcher.Wake(ctx, "demo-device")
	first, _ := result.Results[0].(map[string]any)
	if first["skipped"] != true {
		t.Fatalf("result = %#v", result.Results)
	}
}

func TestFCMErrorParsing(t *testing.T) {
	code, status := fcmError([]byte(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
	if code != "UNREGISTERED" || status != "NOT_FOUND" {
		t.Fatalf("code=%q status=%q", code, status)
	}
	code, status = fcmError([]byte(`not json`))
	if code != "" || status != "" {
		t.Fatalf("invalid json parsed as %q/%q", code, status)
	}
	if !strings.Contains(summarize([]byte(strings.Repeat("x", 300))), "…") {
		t.Fatal("summarize did not truncate")
	}
}
