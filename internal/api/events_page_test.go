package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/tomato-hug/homing/internal/store"
)

func TestEventPagesPreserveEveryEventAndLegacyResponse(t *testing.T) {
	var fixture strings.Builder
	for i := 1; i <= 75; i++ {
		b, _ := json.Marshal(store.Event{ID: int64(i * 2), Source: "codex", SessionID: "demo-page-session", Text: strings.Repeat("demo 内容 ", 300)})
		fixture.Write(b)
		fixture.WriteByte('\n')
	}
	d := writeStore(t, fixture.String(), `{"version":1,"sessions":{}}`)
	var seen []int64
	var since int64
	for attempt := 0; attempt < 75; attempt++ {
		rec := serveConsoleRequest(t, d, http.MethodGet, fmt.Sprintf("/api/events?since=%d&limit=32", since), "")
		var page struct {
			Events []store.Event `json:"events"`
			Next   int64         `json:"next_since"`
			More   bool          `json:"has_more"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || len(page.Events) == 0 || len(page.Events) > 32 || rec.Body.Len() > 66*1024 {
			t.Fatalf("invalid bounded page: status=%d count=%d bytes=%d", rec.Code, len(page.Events), rec.Body.Len())
		}
		for _, event := range page.Events {
			if event.ID <= since {
				t.Fatalf("cursor repeated an event: %d <= %d", event.ID, since)
			}
			if event.Text != strings.Repeat("demo 内容 ", 300) {
				t.Fatal("page truncated event text")
			}
			seen = append(seen, event.ID)
		}
		if page.Next != page.Events[len(page.Events)-1].ID {
			t.Fatal("cursor does not match delivered tail")
		}
		since = page.Next
		if !page.More {
			break
		}
	}
	if len(seen) != 75 {
		t.Fatalf("received %d events, want 75", len(seen))
	}
	for i, id := range seen {
		if id != int64((i+1)*2) {
			t.Fatalf("missing event at %d", i)
		}
	}
	legacy := serveConsoleRequest(t, d, http.MethodGet, "/api/events", "")
	var events []store.Event
	if err := json.Unmarshal(legacy.Body.Bytes(), &events); err != nil || len(events) != 75 {
		t.Fatal("legacy array changed")
	}
	empty := serveConsoleRequest(t, d, http.MethodGet, "/api/events?since=150&limit=32", "")
	var page map[string]any
	if err := json.Unmarshal(empty.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page["next_since"] != float64(150) || page["has_more"] != false {
		t.Fatalf("empty page lost cursor: %v", page)
	}
}

func TestEventPageDoesNotDropOversizeSingleEvent(t *testing.T) {
	text := strings.Repeat("demo-large-message ", 5000)
	raw, _ := json.Marshal(store.Event{ID: 1, Source: "codex", SessionID: "demo-large", Text: text})
	d := writeStore(t, string(raw)+"\n", `{"version":1,"sessions":{}}`)
	rec := serveConsoleRequest(t, d, http.MethodGet, "/api/events?limit=32", "")
	var page struct {
		Events []store.Event `json:"events"`
		Next   int64         `json:"next_since"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Text != text || page.Next != 1 {
		t.Fatal("oversize event was lost or truncated")
	}
}
