package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditWritesIdentifiersButNotBody(t *testing.T) {
	dir := t.TempDir()
	deps := &Deps{DataDir: dir}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/compose", strings.NewReader("secret body"))
	request.Header.Set("Authorization", "Bearer local-test-token")
	request.RemoteAddr = "192.0.2.10:5000"
	deps.Audit(request, "dispatch", map[string]any{
		"dispatch_id": "dsp_demo",
		"target":      "claude",
		"text":        "这段正文不应写入审计",
	})

	raw, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatal(err)
	}
	if entry["action"] != "dispatch" || entry["dispatch_id"] != "dsp_demo" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, present := entry["text"]; present {
		t.Fatalf("body leaked into audit: %s", raw)
	}
	if entry["device"] != "local" {
		t.Fatalf("device = %v", entry["device"])
	}
	info, err := os.Stat(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("audit permissions = %v", info.Mode().Perm())
	}
}

func TestCheckRateLimitsPerAction(t *testing.T) {
	deps := &Deps{DataDir: t.TempDir()}
	makeRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/compose", nil)
		request.Header.Set("Authorization", "Bearer hm_rate-demo-device-token")
		request.RemoteAddr = "127.0.0.1:5000"
		return request
	}
	for index := 0; index < 2; index++ {
		recorder := httptest.NewRecorder()
		if !deps.CheckRate(recorder, makeRequest(), "compose_test", 2, time.Minute) {
			t.Fatalf("request %d unexpectedly limited", index+1)
		}
	}
	recorder := httptest.NewRecorder()
	if deps.CheckRate(recorder, makeRequest(), "compose_test", 2, time.Minute) {
		t.Fatal("third request should be limited")
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", recorder.Code)
	}
}
