package relayapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/api"
	filepaths "github.com/tomato-hug/homing/internal/files"
	"github.com/tomato-hug/homing/internal/notify"
	"github.com/tomato-hug/homing/internal/service"
	"github.com/tomato-hug/homing/internal/store"
)

type testRelay struct {
	server *httptest.Server
	mux    http.Handler
	deps   *api.Deps
	store  *store.Store
	token  string
	client *http.Client
}

func newTestRelay(t *testing.T, configure func(string)) *testRelay {
	t.Helper()
	dir := t.TempDir()
	if configure != nil {
		configure(dir)
	}
	deps := &api.Deps{
		Log:     slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1})),
		DataDir: dir,
		Version: "test",
	}
	mux := api.NewMux(deps)
	st, err := deps.Store()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &testRelay{
		server: server,
		mux:    mux,
		deps:   deps,
		store:  st,
		token:  st.Token(),
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				Proxy: nil,
			},
		},
	}
}

func (r *testRelay) do(t *testing.T, method, path string, body []byte, auth bool) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, r.server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if auth {
		request.Header.Set("Authorization", "Bearer "+r.token)
	}
	response, err := r.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	return response, raw
}

func demoCwd(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo-project")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHooksFeedTheServicePipeline(t *testing.T) {
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	transcriptPath := filepath.Join(cwd, "demo-transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte("{\"type\":\"demo\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hooks := []struct {
		path string
		body string
	}{
		{
			"/hook/claude",
			`{"hook_event_name":"Stop","session_id":"demo-claude","cwd":"` + cwd + `","transcript_path":` + jsonString(transcriptPath) + `,"last_assistant_message":"示例：登录按钮已修复"}`,
		},
		{
			"/hook/codex",
			`{"type":"agent-turn-complete","thread-id":"demo-codex","cwd":"` + cwd + `","last-assistant-message":"示例：测试通过"}`,
		},
		{
			"/hook/generic",
			`{"source":"workbuddy","kind":"needs_input","session_id":"demo-workbuddy","cwd":"` + cwd + `","title":"示例：等待确认","text":"示例：继续吗？"}`,
		},
	}
	for _, hook := range hooks {
		response, body := relay.do(t, http.MethodPost, hook.path, []byte(hook.body), false)
		if response.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
			t.Fatalf("%s: status=%d body=%s", hook.path, response.StatusCode, body)
		}
	}

	response, body := relay.do(t, http.MethodGet, "/api/events?since=0", nil, true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("events status=%d", response.StatusCode)
	}
	var events []store.Event
	if err := json.Unmarshal(body, &events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("events=%d, want 3", len(events))
	}
	want := []struct {
		source string
		kind   string
		id     string
	}{
		{"claude", "reply", "demo-claude"},
		{"codex", "reply", "demo-codex"},
		{"workbuddy", "needs_input", "demo-workbuddy"},
	}
	for i, expected := range want {
		if events[i].Source != expected.source || events[i].Kind != expected.kind || events[i].SessionID != expected.id {
			t.Fatalf("event[%d]=%+v", i, events[i])
		}
		if events[i].Read {
			t.Fatalf("new event should be unread")
		}
	}
	if got, ok := scanlib.TranscriptPath("demo-claude"); !ok || got != transcriptPath {
		t.Fatalf("recorded transcript=%q ok=%v, want %q", got, ok, transcriptPath)
	}
}

func TestHooksRejectNonLoopback(t *testing.T) {
	relay := newTestRelay(t, nil)
	request := httptest.NewRequest(http.MethodPost, "/hook/claude", strings.NewReader(`{}`))
	request.RemoteAddr = "203.0.113.9:3210"
	recorder := httptest.NewRecorder()
	relay.mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", recorder.Code)
	}
}

func TestHookEventReachesNotifyPush(t *testing.T) {
	relay := newTestRelay(t, nil)
	svc := service.Start(relay.deps)
	if svc == nil {
		t.Fatal("service pipeline did not start")
	}
	pushes := make(chan notify.Payload, 1)
	svc.SetPush(func(payload notify.Payload) { pushes <- payload })

	cwd := demoCwd(t)
	response, body := relay.do(t, http.MethodPost, "/hook/generic", []byte(`{
		"source":"workbuddy",
		"kind":"error",
		"session_id":"demo-push",
		"cwd":"`+cwd+`",
		"title":"示例：构建失败",
		"text":"示例：编译错误"
	}`), false)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("hook status=%d body=%s", response.StatusCode, body)
	}

	select {
	case payload := <-pushes:
		if payload.Level != "P0" || payload.Source != "workbuddy" ||
			payload.SessionID != "demo-push" || payload.Title != "示例：构建失败" {
			t.Fatalf("payload=%+v", payload)
		}
		if payload.EventID == 0 {
			t.Fatalf("payload has no event id: %+v", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hook event did not reach the push sink")
	}
}

func TestAwayStopWaitsForPhoneReply(t *testing.T) {
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	response, _ := relay.do(t, http.MethodPost, "/api/away", []byte(`{"enabled":true}`), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("away status=%d", response.StatusCode)
	}
	t.Cleanup(func() {
		relay.do(t, http.MethodPost, "/api/away", []byte(`{"enabled":false}`), true)
	})

	hookDone := make(chan []byte, 1)
	go func() {
		status, body := postHookWithTimeout(t, relay, "/hook/claude", `{
			"hook_event_name":"Stop",
			"session_id":"demo-away",
			"cwd":"`+cwd+`",
			"last_assistant_message":"示例：等待手机回复"
		}`)
		if status != http.StatusOK {
			hookDone <- []byte(`bad status`)
			return
		}
		hookDone <- body
	}()

	event := waitForEvent(t, relay, "demo-away")
	response, body := relay.do(t, http.MethodPost, "/api/reply",
		[]byte(`{"event_id":`+jsonInt(event.ID)+`,"text":"示例：继续"}`), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reply status=%d body=%s", response.StatusCode, body)
	}
	var reply map[string]any
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatal(err)
	}
	if reply["status"] != "held" {
		t.Fatalf("reply status=%v, want held", reply["status"])
	}

	select {
	case body := <-hookDone:
		var decision map[string]any
		if err := json.Unmarshal(body, &decision); err != nil {
			t.Fatalf("hook body=%s: %v", body, err)
		}
		if decision["decision"] != "block" {
			t.Fatalf("hook decision=%v", decision["decision"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop hook did not receive the phone reply")
	}
}

func TestApprovalDecisionReturnsToHook(t *testing.T) {
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	hookDone := make(chan map[string]any, 1)
	go func() {
		_, body := postHookWithTimeout(t, relay, "/hook/claude", `{
			"hook_event_name":"PermissionRequest",
			"session_id":"demo-approval",
			"cwd":"`+cwd+`",
			"tool_name":"Bash",
			"tool_input":{"command":"echo demo"}
		}`)
		var decision map[string]any
		_ = json.Unmarshal(body, &decision)
		hookDone <- decision
	}()

	event := waitForEvent(t, relay, "demo-approval")
	response, body := relay.do(t, http.MethodPost, "/api/approvals/"+jsonInt(event.ID),
		[]byte(`{"decision":"allow"}`), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("approval status=%d body=%s", response.StatusCode, body)
	}
	select {
	case decision := <-hookDone:
		if decision["decision"] != "allow" {
			t.Fatalf("decision=%v", decision["decision"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("approval hook did not return")
	}
	response, _ = relay.do(t, http.MethodPost, "/api/approvals/"+jsonInt(event.ID),
		[]byte(`{"decision":"allow"}`), true)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("second approval status=%d, want 404", response.StatusCode)
	}
}

func TestUploadCapabilityAndReplyFiles(t *testing.T) {
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)
	imagePath := filepath.Join(cwd, "demo-image.png")
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	response, _ := relay.do(t, http.MethodPost, "/hook/generic", []byte(`{
		"source":"workbuddy",
		"kind":"reply",
		"session_id":"demo-files",
		"cwd":"`+cwd+`",
		"title":"示例：图片",
		"text":"示例图片：`+imagePath+`"
	}`), false)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("hook status=%d", response.StatusCode)
	}
	event := waitForEvent(t, relay, "demo-files")
	if event.Attachments == nil || len(*event.Attachments) != 1 {
		t.Fatalf("attachments=%v", event.Attachments)
	}
	attachment, _ := (*event.Attachments)[0].(filepaths.Attachment)
	url := attachment.URL
	if !strings.HasPrefix(url, "/f/") {
		t.Fatalf("attachment url=%q", url)
	}
	fileResponse, fileBody := relay.do(t, http.MethodGet, url, nil, false)
	if fileResponse.StatusCode != http.StatusOK || !bytes.Equal(fileBody, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("/f status=%d body=%q", fileResponse.StatusCode, fileBody)
	}

	uploadResponse, uploadBody := relay.do(t, http.MethodPost,
		"/api/upload?event_id="+jsonInt(event.ID)+"&name=demo%20upload.txt",
		[]byte("demo upload\n"), true)
	if uploadResponse.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", uploadResponse.StatusCode, uploadBody)
	}
	var uploaded map[string]any
	if err := json.Unmarshal(uploadBody, &uploaded); err != nil {
		t.Fatal(err)
	}
	path, _ := uploaded["path"].(string)
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "demo upload\n" {
		t.Fatalf("uploaded path=%q err=%v content=%q", path, err, raw)
	}

	replyResponse, replyBody := relay.do(t, http.MethodPost, "/api/reply", []byte(`{
		"event_id":`+jsonInt(event.ID)+`,
		"text":"示例：文件在此",
		"files":[`+jsonString(path)+`]
	}`), true)
	if replyResponse.StatusCode != http.StatusOK {
		t.Fatalf("reply status=%d body=%s", replyResponse.StatusCode, replyBody)
	}
	var reply map[string]any
	if err := json.Unmarshal(replyBody, &reply); err != nil {
		t.Fatal(err)
	}
	files, _ := reply["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("reply files=%v", reply["files"])
	}
}

func TestDevicesListHeartbeatRenameRevoke(t *testing.T) {
	relay := newTestRelay(t, nil)
	deviceToken := "hm_demo-device-token"
	sum := sha256.Sum256([]byte(deviceToken))
	device := store.Device{
		ID:          "0123456789ab",
		Name:        "示例手机",
		Platform:    "android",
		AppVersion:  "0.7.0",
		CreatedAt:   "2026-11-09T00:00:00.000Z",
		PairedVia:   "qr",
		TokenSHA256: hex.EncodeToString(sum[:]),
	}
	if err := relay.store.SaveDevices([]store.Device{device}, nil); err != nil {
		t.Fatal(err)
	}

	response, body := relay.do(t, http.MethodGet, "/api/devices", nil, true)
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"device_id":"0123456789ab"`)) {
		t.Fatalf("devices status=%d body=%s", response.StatusCode, body)
	}
	request, _ := http.NewRequest(http.MethodPost, relay.server.URL+"/api/devices/heartbeat",
		strings.NewReader(`{"app_version":"0.7.1"}`))
	request.Header.Set("Authorization", "Bearer "+deviceToken)
	request.Header.Set("Content-Type", "application/json")
	heartbeat, err := relay.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	heartbeatBody, _ := io.ReadAll(heartbeat.Body)
	heartbeat.Body.Close()
	if heartbeat.StatusCode != http.StatusOK || !bytes.Contains(heartbeatBody, []byte(`"online":true`)) {
		t.Fatalf("heartbeat status=%d body=%s", heartbeat.StatusCode, heartbeatBody)
	}
	response, _ = relay.do(t, http.MethodPatch, "/api/devices/0123456789ab",
		[]byte(`{"name":"一加示例"}`), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("rename status=%d", response.StatusCode)
	}
	response, _ = relay.do(t, http.MethodDelete, "/api/devices/0123456789ab", nil, true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("revoke status=%d", response.StatusCode)
	}
}

func TestNewSessionValidation(t *testing.T) {
	relay := newTestRelay(t, nil)
	cases := []string{
		`{"source":"other","cwd":"/tmp","text":"示例"}`,
		`{"source":"codex","cwd":"","text":"示例"}`,
		`{"source":"codex","cwd":"/tmp","text":""}`,
	}
	for _, body := range cases {
		response, raw := relay.do(t, http.MethodPost, "/api/sessions/new", []byte(body), true)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d raw=%s", body, response.StatusCode, raw)
		}
	}
}

func TestAPKDownloadOnlyForLocalNetwork(t *testing.T) {
	apk := filepath.Join(t.TempDir(), "demo.apk")
	if err := os.WriteFile(apk, []byte("demo apk"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APK_PATH", apk)
	relay := newTestRelay(t, nil)
	response, body := relay.do(t, http.MethodGet, "/apk", nil, false)
	if response.StatusCode != http.StatusOK || string(body) != "demo apk" {
		t.Fatalf("apk status=%d body=%q", response.StatusCode, body)
	}
	request := httptest.NewRequest(http.MethodGet, "/apk", nil)
	request.RemoteAddr = "203.0.113.7:9999"
	recorder := httptest.NewRecorder()
	relay.mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("public apk status=%d, want 403", recorder.Code)
	}
}

func TestProxyUsesAllowListAndStripsAuthorization(t *testing.T) {
	var sawAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/hello" || r.URL.Query().Get("name") != "demo" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("proxied"))
	}))
	defer upstream.Close()
	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	token := "demo-main-token"
	relay := newTestRelay(t, func(dir string) {
		raw := []byte(`{
			"token":"` + token + `",
			"away":{"enabled":false},
			"proxy":{"enabled":true,"ports":[` + strconv.Itoa(port) + `]}
		}`)
		if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if relay.token != token {
		t.Fatal("test token was not loaded")
	}
	response, body := relay.do(t, http.MethodGet,
		"/p/"+strconv.Itoa(port)+"/hello?name=demo", nil, true)
	if response.StatusCode != http.StatusOK || string(body) != "proxied" {
		t.Fatalf("proxy status=%d body=%s", response.StatusCode, body)
	}
	if sawAuth != "" {
		t.Fatalf("Authorization leaked upstream: %q", sawAuth)
	}
	response, _ = relay.do(t, http.MethodGet, "/p/1/hello", nil, true)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unlisted port status=%d, want 403", response.StatusCode)
	}
}

func postHookWithTimeout(t *testing.T, relay *testRelay, path, body string) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, relay.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Error(err)
		return 0, nil
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := relay.client.Do(request)
	if err != nil {
		t.Error(err)
		return 0, nil
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	return response.StatusCode, raw
}

func waitForEvent(t *testing.T, relay *testRelay, sessionID string) store.Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range relay.store.Events() {
			if event.SessionID == sessionID {
				return event
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event for %s did not appear", sessionID)
	return store.Event{}
}

func jsonInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func jsonString(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
