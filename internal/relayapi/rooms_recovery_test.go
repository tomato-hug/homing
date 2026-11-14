package relayapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/store"
)

// All sessions and dispatches below are synthetic; no native agent is launched.
func TestRoomRecoveryCollectsExistingTurnWithoutReply(t *testing.T) {
	for _, kind := range []string{"statement", "minutes"} {
		for _, status := range []string{"needs_you", "completed"} {
			t.Run(kind+"/"+status, func(t *testing.T) {
				relay := newTestRelay(t, nil)
				id := "room-demo-recover-" + randomHex(6)
				child := "demo-dispatch-" + randomHex(6)
				payload := map[string]any{"id": id, "title": "demo recovery", "topic": "demo", "round": 1, "rounds": 1, "status": "paused", "end_requested": kind == "minutes",
					"participants": []map[string]any{{"agent": "codex", "name": "Codex", "status": "waiting", "dispatch_id": child, "session_id": "demo-recovery-session"}},
					"active_turn":  map[string]any{"agent": "codex", "round": 1, "kind": kind, "phase": "submitting", "dispatch_id": child, "baseline": "demo-old-tail"}}
				raw, _ := json.Marshal(payload)
				var room Room
				if err := json.Unmarshal(raw, &room); err != nil {
					t.Fatal(err)
				}
				roomsMu.Lock()
				ensureRoomsLoaded(relay.deps)
				roomsByID[id] = &room
				roomsMu.Unlock()
				dispatch := store.Dispatch{ID: child, ParentSource: "room", ParentSessionID: id, TargetAgent: "codex", TargetSessionID: "demo-recovery-session", Status: status}
				if status == "completed" {
					dispatch.Result = &store.DispatchResult{Text: "demo original " + kind}
				}
				if _, err := relay.store.SaveDispatch(dispatch); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				result := make(chan error, 1)
				go func() {
					turn, err := (dispatchRoomSpeaker{d: relay.deps}).Speak(ctx, room, room.Participants[0], "demo must not resend", func(string) {})
					if err == nil && turn.Text != "demo original "+kind {
						err = context.Canceled
					}
					result <- err
				}()
				if status == "needs_you" {
					// Observe that the existing pending task remains pending, then deliver its result.
					time.Sleep(100 * time.Millisecond)
					dispatch.Status = "completed"
					dispatch.Result = &store.DispatchResult{Text: "demo original " + kind}
					if _, err := relay.store.SaveDispatch(dispatch); err != nil {
						t.Fatal(err)
					}
				}
				if err := <-result; err != nil {
					t.Fatalf("existing result was not recovered: %v", err)
				}
				if replies := relay.store.Replies(); len(replies) != 0 {
					t.Fatalf("recovery sent %d duplicate native replies", len(replies))
				}
			})
		}
	}
}

func blockRoomSave(t *testing.T, relay *testRelay, id string) {
	t.Helper()
	file := filepath.Join(roomsDir(relay.deps), id+".json")
	if err := os.Rename(file, file+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(file); _ = os.Rename(file+".saved", file) })
}
func TestRoomFailedControlDoesNotMutateMemory(t *testing.T) {
	relay, f, room := createControlledRoom(t, "codex", "claude")
	current := nextControlledCall(t, f)
	blockRoomSave(t, relay, room.ID)
	roomControl(t, relay, room.ID, "pause", "", http.StatusInternalServerError)
	got, _ := loadRoom(relay.deps, room.ID)
	if got.Status != roomStatusRunning {
		t.Fatal("failed control changed in-memory state")
	}
	// Keep the worker from doing more work until cleanup cancels it.
	_ = current
}
func TestRoomFailedTurnSaveStopsScheduling(t *testing.T) {
	relay, f, room := createControlledRoom(t, "codex", "claude")
	current := nextControlledCall(t, f)
	blockRoomSave(t, relay, room.ID)
	close(current.release)
	deadline := time.After(3 * time.Second)
	for {
		got, _ := loadRoom(relay.deps, room.ID)
		if got.Status == roomStatusPaused && strings.Contains(got.Note, "保存") {
			break
		}
		select {
		case <-f.calls:
			t.Fatal("next native turn started after failed save")
		case <-deadline:
			t.Fatal("storage error did not pause worker")
		case <-time.After(5 * time.Millisecond):
		}
	}
	waitNoRoomEngine(t, room.ID)
	got, _ := loadRoom(relay.deps, room.ID)
	if len(got.Messages) != 0 {
		t.Fatal("unsaved turn reported as committed")
	}
}
func TestRoomFailedMinutesSaveDoesNotReportEnded(t *testing.T) {
	relay, f, room := createControlledRoom(t, "codex", "claude")
	current := nextControlledCall(t, f)
	roomControl(t, relay, room.ID, "end", "", 200)
	close(current.release)
	minutes := nextControlledCall(t, f)
	blockRoomSave(t, relay, room.ID)
	close(minutes.release)
	waitNoRoomEngine(t, room.ID)
	got, _ := loadRoom(relay.deps, room.ID)
	if got.Status != roomStatusPaused || got.Minutes != "" || !strings.Contains(got.Note, "保存") {
		t.Fatalf("failed minutes save reported success: %+v", got)
	}
}

func TestRoomRestartRestoresActiveSlotAndCommitsOnce(t *testing.T) {
	for _, kind := range []string{"statement", "minutes"} {
		t.Run(kind, func(t *testing.T) {
			id, child := "room-demo-restart-"+randomHex(6), "demo-child-"+randomHex(6)
			relay := newTestRelay(t, func(dir string) {
				status := "running"
				if kind == "minutes" {
					status = "summarizing"
				}
				room := Room{ID: id, Title: "demo restart", Status: status, Rounds: 1, Round: 1, Speaking: "codex", ActiveTurn: &RoomTurnState{Agent: "codex", Round: 1, Kind: kind, Phase: "submitting", DispatchID: child}, Participants: []RoomParticipant{{Agent: "codex", Name: "Codex", DispatchID: child, SessionID: "demo-restart-session", Status: "speaking"}, {Agent: "claude", Name: "Claude", Status: "failed"}}}
				raw, _ := json.Marshal(room)
				_ = os.MkdirAll(filepath.Join(dir, "rooms"), 0700)
				if err := os.WriteFile(filepath.Join(dir, "rooms", id+".json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			})
			useRoomSpeaker(t, dispatchRoomSpeaker{d: relay.deps})
			_, err := relay.store.SaveDispatch(store.Dispatch{ID: child, ParentSessionID: id, ParentSource: "room", TargetAgent: "codex", TargetSessionID: "demo-restart-session", Status: "completed", Result: &store.DispatchResult{Text: "demo recovered " + kind}})
			if err != nil {
				t.Fatal(err)
			}
			recovered, ok := loadRoom(relay.deps, id)
			if !ok || recovered.Status != roomStatusPaused || recovered.ActiveTurn == nil {
				t.Fatal("restart lost active slot")
			}
			if kind == "minutes" && !recovered.EndRequested {
				t.Fatal("restart lost minutes recovery intent")
			}
			roomControl(t, relay, id, "resume", "", 200)
			if kind == "statement" {
				// Pause the scheduler while it awaits the original completion; collection
				// of that active slot still commits, without asking another participant.
				roomControl(t, relay, id, "pause", "", 200)
				// Resume once more if pause won before the worker claimed the active slot.
				current, _ := loadRoom(relay.deps, id)
				if current.Speaking == "" {
					roomControl(t, relay, id, "resume", "", 200)
					deadline := time.Now().Add(time.Second)
					for time.Now().Before(deadline) {
						current, _ = loadRoom(relay.deps, id)
						if current.Speaking != "" {
							break
						}
						time.Sleep(time.Millisecond)
					}
					roomControl(t, relay, id, "pause", "", 200)
				}
			}
			waitNoRoomEngine(t, id)
			got, _ := loadRoom(relay.deps, id)
			if len(got.Messages) != 1 || got.Messages[0].Text != "demo recovered "+kind || got.ActiveTurn != nil {
				t.Fatalf("slot not committed once: %+v", got)
			}
			if kind == "minutes" && got.Status != roomStatusEnded {
				t.Fatal("minutes did not end room")
			}
			if len(relay.store.Replies()) != 0 {
				t.Fatal("restart replayed native work")
			}
		})
	}
}

func TestRoomAmbiguousSubmissionPausesAndCanEnd(t *testing.T) {
	relay := newTestRelay(t, nil)
	useRoomSpeaker(t, dispatchRoomSpeaker{d: relay.deps})
	room := Room{ID: "room-demo-uncertain-" + randomHex(6), Title: "demo uncertainty", Status: roomStatusRunning, Rounds: 1, Round: 1, ActiveTurn: &RoomTurnState{Agent: "codex", Round: 1, Kind: "statement", Phase: "submitting", DispatchID: "demo-not-recorded"}, Participants: []RoomParticipant{{Agent: "codex", Name: "Codex"}, {Agent: "claude", Name: "Claude", Status: "failed"}}}
	roomsMu.Lock()
	ensureRoomsLoaded(relay.deps)
	roomsByID[room.ID] = &room
	roomsMu.Unlock()
	startRoomEngine(relay.deps, room.ID)
	waitNoRoomEngine(t, room.ID)
	paused, _ := loadRoom(relay.deps, room.ID)
	if paused.Status != roomStatusPaused || paused.ActiveTurn == nil || paused.ActiveTurn.Phase != "uncertain" {
		t.Fatalf("ambiguous submission did not pause: %+v", paused)
	}
	roomControl(t, relay, room.ID, "end", "", 200)
	ended := waitRoomStatus(t, relay, room.ID, roomStatusEnded)
	if ended.Minutes == "" || len(relay.store.Dispatches()) != 0 || len(relay.store.Replies()) != 0 {
		t.Fatal("ambiguous submission replayed or cannot end")
	}
}

func TestRoomFailedCreateDoesNotPublishGhost(t *testing.T) {
	relay := newTestRelay(t, nil)
	useDispatchLauncher(t, dispatchFakeLauncher{targets: map[string]bool{"codex": true, "claude": true}})
	if err := os.WriteFile(roomsDir(relay.deps), []byte("demo blocked storage"), 0600); err != nil {
		t.Fatal(err)
	}
	roomsMu.Lock()
	before := len(roomsByID)
	roomsMu.Unlock()
	body, _ := json.Marshal(map[string]any{"topic": "demo failed create", "cwd": demoCwd(t), "participants": []map[string]string{{"agent": "codex"}, {"agent": "claude"}}})
	response, _ := relay.do(t, http.MethodPost, "/api/rooms", body, true)
	if response.StatusCode != 500 {
		t.Fatalf("create status=%d", response.StatusCode)
	}
	roomsMu.Lock()
	after := len(roomsByID)
	roomsMu.Unlock()
	if after != before {
		t.Fatal("failed create published an unsaved ghost room")
	}
}

func TestRoomRecoveryPausesUnboundStartingDispatch(t *testing.T) {
	relay := newTestRelay(t, nil)
	room := Room{ID: "room-demo-starting-" + randomHex(6), ActiveTurn: &RoomTurnState{Agent: "codex", Kind: "statement", Phase: "submitting", DispatchID: "demo-starting-child"}, Participants: []RoomParticipant{{Agent: "codex"}}}
	_, err := relay.store.SaveDispatch(store.Dispatch{ID: "demo-starting-child", ParentSource: "room", ParentSessionID: room.ID, TargetAgent: "codex", Status: "starting"})
	if err != nil {
		t.Fatal(err)
	}
	roomsMu.Lock()
	ensureRoomsLoaded(relay.deps)
	roomsByID[room.ID] = &room
	roomsMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = (dispatchRoomSpeaker{d: relay.deps}).Speak(ctx, room, room.Participants[0], "demo do not resend", func(string) {})
	if !errors.Is(err, errRoomTurnUncertain) {
		t.Fatalf("unbound submission must pause rather than wait indefinitely: %v", err)
	}
}
