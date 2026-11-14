package relayapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomato-hug/homing/internal/store"
)

func TestRoomActionObservesNativeCodexCompletionWithoutHook(t *testing.T) {
	relay, _, room := actionFixture(t)
	response, raw := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/action-1/dispatch", []byte(`{"agent":"codex","text":"demo action","confirmed":true}`), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("dispatch status=%d body=%s", response.StatusCode, raw)
	}
	ds := relay.store.Dispatches()
	if len(ds) != 1 {
		t.Fatalf("dispatch count=%d", len(ds))
	}
	dispatch := ds[0]
	dispatch.TargetSessionID = "12345678-1234-1234-1234-123456789abc"
	if _, err := relay.store.SaveDispatch(dispatch); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	previous := roomCodexSessionsDir
	roomCodexSessionsDir = func() string { return root }
	t.Cleanup(func() { roomCodexSessionsDir = previous })
	dir := filepath.Join(root, "2026", "11", "14")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "rollout-2026-11-14T12-00-00-"+dispatch.TargetSessionID+".jsonl")
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]any{"id": dispatch.TargetSessionID, "cwd": dispatch.Cwd}})
	started := `{"type":"event_msg","payload":{"type":"task_started","turn_id":"demo-turn-1"}}`
	completed := `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"demo-turn-1","last_agent_message":"demo completed"}}`
	write := func(tail string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(fmt.Sprintf("%s\n%s\n%s", meta, started, tail)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	status := func() RoomAction {
		t.Helper()
		response, raw := relay.do(t, http.MethodGet, "/api/rooms/"+room.ID, nil, true)
		if response.StatusCode != 200 {
			t.Fatalf("get=%d", response.StatusCode)
		}
		var result struct {
			Room Room `json:"room"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result.Room.Actions[0]
	}
	write("")
	if got := status(); got.Status == "completed" {
		t.Fatal("running native task reported completed")
	}
	write(completed + "\n")
	if got := status(); got.Status != "completed" || got.Result != "demo completed" {
		t.Fatalf("native completion not reconciled: status=%s result=%q", got.Status, got.Result)
	}
}

func TestRoomCodexFinishedRequiresMatchingCompletedTurn(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	id := "12345678-1234-1234-1234-123456789abc"
	dir := filepath.Join(root, "2026", "11", "14")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-11-14T12-00-00-"+id+".jsonl")
	meta := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q}}`+"\n", id, cwd)
	start := `{"type":"event_msg","payload":{"type":"task_started","turn_id":"demo-turn-1"}}` + "\n"
	done := `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"demo-turn-1","last_agent_message":"demo result"}}` + "\n"
	cases := []struct {
		name, data, id, cwd string
		want                bool
	}{
		{"completed", meta + start + done, id, cwd, true},
		{"running", meta + start, id, cwd, false},
		{"new turn running", meta + start + done + `{"type":"event_msg","payload":{"type":"task_started","turn_id":"demo-turn-2"}}` + "\n", id, cwd, false},
		{"aborted", meta + start + `{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"demo-turn-1"}}` + "\n", id, cwd, false},
		{"different cwd", meta + start + done, id, t.TempDir(), false},
		{"different session", meta + start + done, "../../other", cwd, false},
		{"partial trailing record", meta + start + done + `{"type":`, id, cwd, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			text, ok := roomCodexFinished(root, tc.id, tc.cwd)
			if ok != tc.want || (ok && text != "demo result") {
				t.Fatalf("got ok=%v text=%q", ok, text)
			}
		})
	}
}

func TestCompletionHookDoesNotReviveCancelledDispatch(t *testing.T) {
	relay, _, room := actionFixture(t)
	response, _ := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/action-1/dispatch", []byte(`{"agent":"codex","text":"demo action","confirmed":true}`), true)
	if response.StatusCode != 200 {
		t.Fatalf("dispatch status=%d", response.StatusCode)
	}
	dispatch := relay.store.Dispatches()[0]
	dispatch.Status = "cancelled"
	if _, err := relay.store.SaveDispatch(dispatch); err != nil {
		t.Fatal(err)
	}
	trackDispatchLifecycle(relay.deps, store.Event{Source: "codex", SessionID: dispatch.TargetSessionID, Kind: "done", Text: "demo late result"})
	got, _ := findDispatch(relay.store, dispatch.ID)
	if got.Status != "cancelled" || got.Result != nil {
		t.Fatalf("late completion changed terminal status: %s", got.Status)
	}
}
