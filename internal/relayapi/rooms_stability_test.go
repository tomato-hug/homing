package relayapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tomato-hug/homing/internal/orchestrator"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type controlledRoomCall struct {
	agent, session, prompt string
	release                chan struct{}
}
type controlledRoomSpeaker struct{ calls chan controlledRoomCall }

func (f *controlledRoomSpeaker) Speak(ctx context.Context, _ Room, p RoomParticipant, prompt string, _ func(string)) (roomTurn, error) {
	c := controlledRoomCall{p.Agent, p.SessionID, prompt, make(chan struct{})}
	select {
	case f.calls <- c:
	case <-ctx.Done():
		return roomTurn{}, ctx.Err()
	}
	select {
	case <-c.release:
		return roomTurn{Text: "demo statement from " + p.Agent, DispatchID: "demo-dispatch-" + p.Agent, SessionID: "demo-session-" + p.Agent}, nil
	case <-ctx.Done():
		return roomTurn{}, ctx.Err()
	}
}
func nextControlledCall(t *testing.T, f *controlledRoomSpeaker) controlledRoomCall {
	t.Helper()
	select {
	case c := <-f.calls:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("room never requested next speaker")
		return controlledRoomCall{}
	}
}
func roomControl(t *testing.T, r *testRelay, id, action, target string, want int) Room {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"action": action, "agent": target})
	response, raw := r.do(t, http.MethodPost, "/api/rooms/"+id+"/control", body, true)
	if response.StatusCode != want {
		t.Fatalf("%s(%s) status=%d want %d body=%s", action, target, response.StatusCode, want, raw)
	}
	var result struct {
		Room Room `json:"room"`
	}
	_ = json.Unmarshal(raw, &result)
	return result.Room
}
func createControlledRoom(t *testing.T, participants ...string) (*testRelay, *controlledRoomSpeaker, Room) {
	t.Helper()
	speaker := &controlledRoomSpeaker{calls: make(chan controlledRoomCall, 1)}
	useRoomSpeaker(t, speaker)
	relay := newTestRelay(t, nil)
	room := Room{ID: "room-demo-" + randomHex(6), Title: "demo-room", Topic: "demo read-only discussion", Cwd: demoCwd(t), Mode: roomModeRoundRobin, Rounds: 2, Status: roomStatusRunning, Messages: []RoomMessage{}}
	for _, id := range participants {
		room.Participants = append(room.Participants, RoomParticipant{Agent: id, Name: id, Status: "waiting"})
	}
	roomsMu.Lock()
	ensureRoomsLoaded(relay.deps)
	roomsByID[room.ID] = &room
	if err := saveRoomLocked(relay.deps, &room); err != nil {
		t.Fatal(err)
	}
	snapshot := cloneRoom(&room)
	roomsMu.Unlock()
	t.Cleanup(func() {
		roomsMu.Lock()
		if cancel := roomEngines[room.ID]; cancel != nil {
			cancel()
		}
		roomsMu.Unlock()
		waitNoRoomEngine(t, room.ID)
	})
	startRoomEngine(relay.deps, room.ID)
	return relay, speaker, snapshot
}
func waitNoRoomEngine(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		roomsMu.Lock()
		_, running := roomEngines[id]
		roomsMu.Unlock()
		if !running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Errorf("room engine %s did not exit", id)
}
func TestRoomCallSchedulesParticipantWithoutInterrupting(t *testing.T) {
	relay, f, room := createControlledRoom(t, "codex", "claude", "workbuddy")
	current := nextControlledCall(t, f)
	roomControl(t, relay, room.ID, "call", "not-a-participant", 400)
	called := roomControl(t, relay, room.ID, "call", "workbuddy", 200)
	if called.Speaking != "codex" {
		t.Fatalf("call interrupted current speaker: %+v", called)
	}
	close(current.release)
	want := []string{"workbuddy", "claude", "codex", "claude", "workbuddy", "codex"}
	for i, id := range want {
		c := nextControlledCall(t, f)
		if c.agent != id {
			t.Fatalf("turn %d agent=%s want %s", i, c.agent, id)
		}
		if i == 2 && c.session != "demo-session-codex" {
			t.Fatalf("second round lost native session: %+v", c)
		}
		close(c.release)
	}
	ended := waitRoomStatus(t, relay, room.ID, roomStatusEnded)
	seen := map[string]bool{}
	for _, m := range ended.Messages {
		if m.Kind != "statement" {
			continue
		}
		key := fmt.Sprintf("%s#%d", m.From, m.Round)
		if seen[key] {
			t.Fatalf("duplicate scheduled slot %s", key)
		}
		seen[key] = true
	}
	if len(seen) != 6 {
		t.Fatalf("scheduled slots=%d want 6", len(seen))
	}
	roomControl(t, relay, room.ID, "call", "codex", 409)
}
func TestRoomCallWhilePausedWaitsForResume(t *testing.T) {
	relay, f, room := createControlledRoom(t, "codex", "claude", "workbuddy")
	first := nextControlledCall(t, f)
	roomControl(t, relay, room.ID, "pause", "", 200)
	called := roomControl(t, relay, room.ID, "call", "workbuddy", 200)
	if called.Status != roomStatusPaused {
		t.Fatal("call resumed a paused room")
	}
	close(first.release)
	waitNoRoomEngine(t, room.ID)
	select {
	case <-f.calls:
		t.Fatal("paused room advanced")
	default:
	}
	roomControl(t, relay, room.ID, "resume", "", 200)
	next := nextControlledCall(t, f)
	if next.agent != "workbuddy" {
		t.Fatalf("queued choice lost: %s", next.agent)
	}
	roomControl(t, relay, room.ID, "end", "", 200)
	close(next.release)
	minutes := nextControlledCall(t, f)
	close(minutes.release)
	waitRoomStatus(t, relay, room.ID, roomStatusEnded)
}
func TestRoomPauseResumeDoesNotDuplicateTurn(t *testing.T) {
	relay, f, room := createControlledRoom(t, "codex", "claude")
	first := nextControlledCall(t, f)
	roomControl(t, relay, room.ID, "pause", "", 200)
	roomControl(t, relay, room.ID, "resume", "", 200)
	close(first.release)
	for i, id := range []string{"claude", "codex", "claude", "codex"} {
		c := nextControlledCall(t, f)
		if c.agent != id {
			t.Fatalf("turn %d=%s want %s", i, c.agent, id)
		}
		if i == 1 && c.session != "demo-session-codex" {
			t.Fatal("native session not reused")
		}
		close(c.release)
	}
	ended := waitRoomStatus(t, relay, room.ID, roomStatusEnded)
	statements, minutes := 0, 0
	for _, m := range ended.Messages {
		if m.Kind == "statement" {
			statements++
		}
		if m.Kind == "minutes" {
			minutes++
		}
	}
	if statements != 4 || minutes != 1 {
		t.Fatalf("statements=%d minutes=%d", statements, minutes)
	}
}
func TestRoomReloadPausesRunningRoom(t *testing.T) {
	for _, status := range []string{roomStatusRunning, roomStatusSummarizing} {
		t.Run(status, func(t *testing.T) {
			id := "room-demo-reload-" + randomHex(6)
			relay := newTestRelay(t, func(dir string) {
				room := Room{ID: id, Status: status, Speaking: "codex", Participants: []RoomParticipant{{Agent: "codex", Status: "speaking", SessionID: "demo-native-session"}}}
				raw, _ := json.Marshal(room)
				_ = os.MkdirAll(filepath.Join(dir, "rooms"), 0700)
				if err := os.WriteFile(filepath.Join(dir, "rooms", id+".json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			})
			recovered, ok := loadRoom(relay.deps, id)
			if !ok || recovered.Status != roomStatusPaused || recovered.Speaking != "" || recovered.Participants[0].SessionID != "demo-native-session" {
				t.Fatalf("recovered=%+v", recovered)
			}
			raw, _ := os.ReadFile(filepath.Join(roomsDir(relay.deps), id+".json"))
			var saved Room
			_ = json.Unmarshal(raw, &saved)
			if saved.Status != roomStatusPaused {
				t.Fatal("recovered paused state was not persisted")
			}
		})
	}
}
func TestRoomEndProducesMinutesOnce(t *testing.T) {
	relay, f, room := createControlledRoom(t, "codex", "claude")
	first := nextControlledCall(t, f)
	roomControl(t, relay, room.ID, "end", "", 200)
	roomControl(t, relay, room.ID, "end", "", 200)
	roomControl(t, relay, room.ID, "pause", "", 409)
	roomControl(t, relay, room.ID, "extend", "", 409)
	close(first.release)
	minutes := nextControlledCall(t, f)
	if !strings.Contains(minutes.prompt, "会议纪要") {
		t.Fatal("end advanced discussion instead of minutes")
	}
	roomControl(t, relay, room.ID, "end", "", 409)
	roomControl(t, relay, room.ID, "call", "claude", 409)
	response, _ := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/messages", []byte(`{"text":"demo too late"}`), true)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("message during minutes status=%d", response.StatusCode)
	}
	close(minutes.release)
	ended := waitRoomStatus(t, relay, room.ID, roomStatusEnded)
	count := 0
	for _, m := range ended.Messages {
		if m.Kind == "minutes" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("minutes produced %d times", count)
	}
}

type failingRoomSpeaker struct{}

func (failingRoomSpeaker) Speak(context.Context, Room, RoomParticipant, string, func(string)) (roomTurn, error) {
	return roomTurn{}, context.DeadlineExceeded
}
func TestRoomAllParticipantsFailProducesRecoverableMinutes(t *testing.T) {
	relay := newTestRelay(t, nil)
	useRoomSpeaker(t, failingRoomSpeaker{})
	room := Room{ID: "room-demo-fail-" + randomHex(6), Title: "demo failure", Rounds: 2, Status: roomStatusRunning, Participants: []RoomParticipant{{Agent: "codex", Name: "Codex"}, {Agent: "claude", Name: "Claude"}}}
	roomsMu.Lock()
	ensureRoomsLoaded(relay.deps)
	roomsByID[room.ID] = &room
	roomsMu.Unlock()
	startRoomEngine(relay.deps, room.ID)
	ended := waitRoomStatus(t, relay, room.ID, roomStatusEnded)
	waitNoRoomEngine(t, room.ID)
	if ended.Minutes == "" || ended.Note == "" {
		t.Fatal("all failures left no readable result or next-step explanation")
	}
	for _, p := range ended.Participants {
		if p.Status != "failed" {
			t.Fatalf("failed participant remains active: %+v", p)
		}
	}
}

func TestRoomResumeDuringWorkerExitDoesNotStall(t *testing.T) {
	relay := newTestRelay(t, nil)
	f := &controlledRoomSpeaker{calls: make(chan controlledRoomCall, 1)}
	useRoomSpeaker(t, f)
	room := Room{ID: "room-demo-exit-" + randomHex(6), Title: "demo resume", Rounds: 2, Status: roomStatusPaused, Participants: []RoomParticipant{{Agent: "codex", Name: "Codex"}, {Agent: "claude", Name: "Claude"}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	roomsMu.Lock()
	ensureRoomsLoaded(relay.deps)
	roomsByID[room.ID] = &room
	roomEngines[room.ID] = cancel
	roomsMu.Unlock()
	t.Cleanup(func() {
		roomsMu.Lock()
		if stop := roomEngines[room.ID]; stop != nil {
			stop()
		}
		roomsMu.Unlock()
		waitNoRoomEngine(t, room.ID)
	})
	roomControl(t, relay, room.ID, "resume", "", 200)
	// The old worker had already observed paused, but had not removed its marker.
	finishRoomEngine(ctx, relay.deps, room.ID)
	next := nextControlledCall(t, f)
	roomControl(t, relay, room.ID, "end", "", 200)
	close(next.release)
	minutes := nextControlledCall(t, f)
	close(minutes.release)
	waitRoomStatus(t, relay, room.ID, roomStatusEnded)
}

func TestRoomApprovalWaitRetainsNativeSessionLink(t *testing.T) {
	relay := newTestRelay(t, nil)
	useDispatchLauncher(t, dispatchFakeLauncher{targets: map[string]bool{"codex": true}, result: orchestrator.StartResult{SessionID: "demo-room-approval-session", Status: "started"}})
	useRoomSpeaker(t, dispatchRoomSpeaker{d: relay.deps})
	room := Room{ID: "room-demo-approval-" + randomHex(6), Title: "demo approval", Topic: "demo read-only", Cwd: demoCwd(t), Rounds: 1, Status: roomStatusRunning, Participants: []RoomParticipant{{Agent: "codex", Name: "Codex"}, {Agent: "claude", Name: "Claude"}}}
	roomsMu.Lock()
	ensureRoomsLoaded(relay.deps)
	roomsByID[room.ID] = &room
	roomsMu.Unlock()
	t.Cleanup(func() {
		roomsMu.Lock()
		if stop := roomEngines[room.ID]; stop != nil {
			stop()
		}
		roomsMu.Unlock()
		waitNoRoomEngine(t, room.ID)
	})
	startRoomEngine(relay.deps, room.ID)
	deadline := time.Now().Add(3 * time.Second)
	var childID string
	for time.Now().Before(deadline) {
		current, _ := loadRoom(relay.deps, room.ID)
		if current.Participants[0].SessionID == "demo-room-approval-session" {
			childID = current.Participants[0].DispatchID
			break
		}
		time.Sleep(time.Millisecond)
	}
	if childID == "" {
		t.Fatal("native session link hidden while waiting for the first turn")
	}
	dispatch, ok := findDispatch(relay.store, childID)
	if !ok {
		t.Fatal("dispatch missing")
	}
	dispatch.Status = "needs_you"
	if _, err := relay.store.SaveDispatch(dispatch); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := loadRoom(relay.deps, room.ID)
		if current.Participants[0].Status == "needs_you" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("approval wait not surfaced to the room")
}
