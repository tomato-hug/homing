package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTruncateCountsWideRunes(t *testing.T) {
	if got := truncate("abc登入", 5); got != "abc登" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("truncate ascii = %q", got)
	}
}

func TestVisibleRangeKeepsSelectionVisible(t *testing.T) {
	start, end := visibleRange(7, 20, 5)
	if start != 5 || end != 10 {
		t.Fatalf("range = %d..%d", start, end)
	}
	start, end = visibleRange(19, 20, 5)
	if start != 15 || end != 20 {
		t.Fatalf("tail range = %d..%d", start, end)
	}
}

func TestClientSendsBearerAndDecodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer demo-token" {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path != "/api/home" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"away":{"enabled":true},"counts":{"needs_you":2}}`))
	}))
	defer server.Close()

	var home homePayload
	c := &client{baseURL: server.URL, token: "demo-token", http: server.Client()}
	if err := c.get(context.Background(), "/api/home", &home); err != nil {
		t.Fatal(err)
	}
	if !home.Away.Enabled || home.Counts.NeedsYou != 2 {
		t.Fatalf("decoded = %+v", home)
	}
}

func TestRenderIsTerminalSafe(t *testing.T) {
	app := &application{
		client: &client{baseURL: "http://127.0.0.1:18787"},
		home: homePayload{
			Attention: []struct {
				Reason    string         `json:"reason"`
				Headline  string         `json:"headline"`
				Summary   string         `json:"summary"`
				At        string         `json:"at"`
				Session   sessionSummary `json:"session"`
				EventID   int64          `json:"event_id"`
				Unseen    int            `json:"unseen_count"`
				SinceAway bool           `json:"since_away"`
			}{{
				Reason: "needs_reply", Headline: "登录\n按钮", Summary: "请选择\t方案",
				Session: sessionSummary{Source: "codex"},
			}},
		},
	}
	app.home.Counts.NeedsYou = 1
	text := app.render(80, 24)
	if strings.Contains(text, "登录\n按钮") || strings.Contains(text, "\t") {
		t.Fatalf("unsafe terminal content: %q", text)
	}
	if !strings.Contains(text, "Homing TUI") || !strings.Contains(text, "登录 按钮") {
		t.Fatalf("render missing content: %q", text)
	}
}
