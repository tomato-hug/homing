package cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPairControlEndToEnd(t *testing.T) {
	dir := t.TempDir()
	hub, err := NewHub(dir, testMux(), testLogger())
	if err != nil {
		t.Fatalf("hub: %v", err)
	}
	t.Cleanup(hub.Close)

	controlMux := http.NewServeMux()
	mountPairControl(controlMux, dir, hub)
	controlServer := httptest.NewServer(controlMux)
	t.Cleanup(controlServer.Close)

	hubServer := httptest.NewServer(hub)
	t.Cleanup(hubServer.Close)
	wsURL := "ws://" + strings.TrimPrefix(hubServer.URL, "http://") + "/v1/ws"

	var started pairStartResponse
	if err := callJSON(context.Background(), http.MethodPost,
		controlServer.URL+"/api/cloud/pair/start",
		map[string]string{"server": wsURL}, &started); err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.Mode != "lan" || started.Token == "" || started.Code6 == "" {
		t.Fatalf("started = %#v", started)
	}
	if started.Server != wsURL {
		t.Fatalf("server = %q, want %q", started.Server, wsURL)
	}

	client := dialPeer(t, wsURL, clientSeed)
	client.hello(t)
	client.send(t, map[string]any{
		"t":        "pair.join",
		"token":    started.Token,
		"pub":      client.publicKey,
		"name":     "测试手机",
		"platform": "android",
	})
	client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.waiting"
	}, 2*time.Second)

	var list pairRequestList
	if err := callJSON(context.Background(), http.MethodGet,
		controlServer.URL+"/api/cloud/pair/requests", nil, &list); err != nil {
		t.Fatalf("requests: %v", err)
	}
	if len(list.Requests) != 1 || len(list.Requests[0].SAS) != 4 {
		t.Fatalf("requests = %#v", list)
	}
	if list.Requests[0].ClientID != client.deviceID {
		t.Fatalf("client id = %q, want %q", list.Requests[0].ClientID, client.deviceID)
	}

	var decision map[string]any
	if err := callJSON(context.Background(), http.MethodPost,
		controlServer.URL+"/api/cloud/pair/decision",
		pairDecision{ClientID: client.deviceID, Accept: true}, &decision); err != nil {
		t.Fatalf("decision: %v", err)
	}
	if decision["ok"] != true {
		t.Fatalf("decision = %#v", decision)
	}
	client.next(t, func(message map[string]any) bool {
		return message["t"] == "pair.accepted"
	}, 2*time.Second)
	if _, ok := hub.Peers().Get(client.deviceID); !ok {
		t.Fatal("accepted peer was not persisted")
	}
}

func TestPairURLHelpers(t *testing.T) {
	normalized, err := normalizeServeURL("127.0.0.1:18788/")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if normalized != "http://127.0.0.1:18788" {
		t.Fatalf("normalized = %q", normalized)
	}
	wsURL, err := lanWebSocketURL(normalized, "192.168.1.3")
	if err != nil {
		t.Fatalf("lan url: %v", err)
	}
	if wsURL != "ws://192.168.1.3:18788/v1/ws" {
		t.Fatalf("ws url = %q", wsURL)
	}
	if !isYes(" Y ") || !isYes("是") || isYes("n") {
		t.Fatal("isYes did not accept the expected answers")
	}
}

func TestPairControlRejectsCloudServerWithoutConnector(t *testing.T) {
	dir := t.TempDir()
	hub, err := NewHub(dir, testMux(), testLogger())
	if err != nil {
		t.Fatalf("hub: %v", err)
	}
	t.Cleanup(hub.Close)

	controlMux := http.NewServeMux()
	mountPairControl(controlMux, dir, hub)
	controlServer := httptest.NewServer(controlMux)
	t.Cleanup(controlServer.Close)

	err = callJSON(context.Background(), http.MethodPost,
		controlServer.URL+"/api/cloud/pair/start",
		map[string]string{"server": "wss://relay.example.test/v1/ws"}, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "云中转尚未配置") {
		t.Fatalf("start error = %v, want cloud-not-configured", err)
	}
}
