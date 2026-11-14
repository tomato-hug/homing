package relayapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestRoomMentionMessageIsAtomicAndIdempotent(t *testing.T) {
	relay, f, room := createControlledRoom(t, "codex", "claude", "workbuddy")
	current := nextControlledCall(t, f)
	roomControl(t, relay, room.ID, "pause", "", 200)
	body := []byte(`{"text":"@workbuddy demo question","mention":"workbuddy","client_id":"demo-message-1"}`)
	for i := 0; i < 2; i++ {
		response, raw := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/messages", body, true)
		if response.StatusCode != 200 {
			t.Fatalf("attempt %d status=%d body=%s", i, response.StatusCode, raw)
		}
	}
	got, _ := loadRoom(relay.deps, room.ID)
	if got.Status != roomStatusPaused || got.NextSpeaker != "workbuddy" || len(got.Messages) != 1 {
		t.Fatalf("atomic mention/dedup failed: %+v", got)
	}
	var raw map[string]any
	b, _ := json.Marshal(got.Messages[0])
	_ = json.Unmarshal(b, &raw)
	if raw["client_id"] != "demo-message-1" || raw["mention"] != "workbuddy" {
		t.Fatalf("missing correlation metadata: %v", raw)
	}
	close(current.release)
	waitNoRoomEngine(t, room.ID)
	select {
	case <-f.calls:
		t.Fatal("mention resumed a paused meeting")
	case <-time.After(20 * time.Millisecond):
	}
	roomControl(t, relay, room.ID, "resume", "", 200)
	if next := nextControlledCall(t, f); next.agent != "workbuddy" {
		t.Fatalf("next=%s", next.agent)
	}
}
func TestRoomInvalidMentionLeavesNoMessage(t *testing.T) {
	relay, _, room := createControlledRoom(t, "codex", "claude")
	for _, target := range []string{"codex", "not-a-member"} {
		body, _ := json.Marshal(map[string]string{"text": "demo rejected", "mention": target, "client_id": "demo-invalid-" + target})
		response, _ := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/messages", body, true)
		if response.StatusCode < 400 {
			t.Fatalf("invalid mention %s accepted", target)
		}
	}
	got, _ := loadRoom(relay.deps, room.ID)
	if len(got.Messages) != 0 || got.NextSpeaker != "" {
		t.Fatal("invalid mention changed room")
	}
}
func TestRoomClientIDCannotBeReusedForDifferentMessage(t *testing.T) {
	relay, _, room := createControlledRoom(t, "codex", "claude")
	for i, text := range []string{"demo original", "demo conflicting"} {
		b, _ := json.Marshal(map[string]string{"text": text, "client_id": "demo-stable-id"})
		response, _ := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/messages", b, true)
		want := 200
		if i == 1 {
			want = 409
		}
		if response.StatusCode != want {
			t.Fatalf("status=%d want=%d", response.StatusCode, want)
		}
	}
}

func TestRoomMentionNeverOverwritesPendingCall(t *testing.T) {
	relay, _, room := createControlledRoom(t, "codex", "claude", "workbuddy")
	first := []byte(`{"text":"@claude demo one","mention":"claude","client_id":"demo-mention-1"}`)
	response, _ := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/messages", first, true)
	if response.StatusCode != 200 {
		t.Fatalf("first status %d", response.StatusCode)
	}
	second := []byte(`{"text":"@workbuddy demo two","mention":"workbuddy","client_id":"demo-mention-2"}`)
	response, _ = relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/messages", second, true)
	if response.StatusCode != 409 {
		t.Fatalf("second status %d", response.StatusCode)
	}
	roomControl(t, relay, room.ID, "call", "workbuddy", 409)
	got, _ := loadRoom(relay.deps, room.ID)
	if got.NextSpeaker != "claude" || len(got.Messages) != 1 {
		t.Fatalf("pending call overwritten: %+v", got)
	}
}
