package relayapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/orchestrator"
	"github.com/tomato-hug/homing/internal/store"
)

type roomActionLauncher struct {
	mu     sync.Mutex
	starts []orchestrator.StartOptions
}

func (*roomActionLauncher) CanDispatch(id string) bool { return id == "codex" || id == "claude" }
func (f *roomActionLauncher) Start(_ context.Context, o orchestrator.StartOptions) orchestrator.StartResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, o)
	return orchestrator.StartResult{SessionID: "demo-action-session", Status: "started"}
}
func actionFixture(t *testing.T) (*testRelay, *roomActionLauncher, Room) {
	t.Helper()
	relay := newTestRelay(t, nil)
	f := &roomActionLauncher{}
	useDispatchLauncher(t, f)
	cwd := demoCwd(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git demo fixture: %v %s", err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(cwd, "demo.txt"), []byte("demo fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "demo.txt")
	run("-c", "user.name=Demo", "-c", "user.email=demo@example.invalid", "commit", "-qm", "demo fixture")
	room := Room{ID: "room-demo-actions-" + randomHex(6), Title: "demo meeting", Topic: "demo topic", Cwd: cwd, Status: roomStatusEnded, Minutes: "## 结论\n演示\n\n## 行动项\n- 完成示例测试，验收：测试通过\n- [ ] 整理示例文档\n", Participants: []RoomParticipant{{Agent: "codex", Name: "Codex"}, {Agent: "claude", Name: "Claude"}}}
	roomsMu.Lock()
	ensureRoomsLoaded(relay.deps)
	roomsByID[room.ID] = &room
	if err := saveRoomLocked(relay.deps, &room); err != nil {
		t.Fatal(err)
	}
	roomsMu.Unlock()
	return relay, f, room
}
func TestRoomActionDispatchRequiresConfirmationAndUsesOneWorktree(t *testing.T) {
	relay, f, room := actionFixture(t)
	path := "/api/rooms/" + room.ID + "/actions/action-1/dispatch"
	response, _ := relay.do(t, http.MethodPost, path, []byte(`{"agent":"codex","text":"demo implement","confirmed":false}`), true)
	if response.StatusCode != 400 {
		t.Fatalf("unconfirmed status=%d", response.StatusCode)
	}
	body := []byte(`{"agent":"codex","text":"demo implement","confirmed":true}`)
	for i := 0; i < 2; i++ {
		response, raw := relay.do(t, http.MethodPost, path, body, true)
		if response.StatusCode != 200 {
			t.Fatalf("dispatch %d status=%d: %s", i, response.StatusCode, raw)
		}
	}
	f.mu.Lock()
	starts := append([]orchestrator.StartOptions(nil), f.starts...)
	f.mu.Unlock()
	if len(starts) != 1 || starts[0].Cwd == room.Cwd {
		t.Fatalf("dispatch count/worktree: %+v", starts)
	}
	ds := relay.store.Dispatches()
	if len(ds) != 1 || ds[0].Workspace != "worktree" || ds[0].ParentSessionID != room.ID {
		t.Fatalf("dispatch=%+v", ds)
	}
	ds[0].Status = "completed"
	ds[0].Result = &store.DispatchResult{Text: "demo action result"}
	if _, err := relay.store.SaveDispatch(ds[0]); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		res, raw := relay.do(t, http.MethodGet, "/api/rooms/"+room.ID, nil, true)
		if res.StatusCode != 200 {
			t.Fatal(string(raw))
		}
		var result struct {
			Room struct {
				Actions []struct{ ID, Status, Result string } `json:"actions"`
			} `json:"room"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Room.Actions) != 2 || result.Room.Actions[0].Status != "completed" || result.Room.Actions[0].Result != "demo action result" {
			t.Fatalf("result not in room: %s", raw)
		}
	}
}
func TestRoomActionUnknownAndTamperedTargetNeverStart(t *testing.T) {
	relay, f, room := actionFixture(t)
	for _, tc := range []struct{ action, agent string }{{"action-9", "codex"}, {"action-1", "not-member"}} {
		b, _ := json.Marshal(map[string]any{"agent": tc.agent, "text": "demo task", "confirmed": true})
		response, _ := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/"+tc.action+"/dispatch", b, true)
		if response.StatusCode < 400 {
			t.Fatalf("invalid request accepted: %+v", tc)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.starts) != 0 {
		t.Fatal("invalid action started agent")
	}
}

func TestRoomActionAmbiguousSubmissionIsNeverReplayed(t *testing.T) {
	relay, f, room := actionFixture(t)
	_, err := mutateRoom(relay.deps, room.ID, func(room *Room) error {
		room.Actions = []RoomAction{{ID: "action-1", Text: "demo implement", Agent: "codex", DispatchID: "dsp_demo_unknown", Status: "submitting"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response, raw := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/action-1/dispatch", []byte(`{"agent":"codex","text":"demo implement","confirmed":true}`), true)
	if response.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", response.StatusCode, raw)
	}
	var payload struct {
		Room struct {
			Actions []RoomAction `json:"actions"`
		} `json:"room"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Room.Actions) < 1 || payload.Room.Actions[0].Status != "unknown" {
		t.Fatalf("ambiguous status: %s", raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.starts) != 0 {
		t.Fatal("ambiguous dispatch was launched again")
	}
}
func TestRoomActionPreparationFailureCanBeRetriedExplicitly(t *testing.T) {
	relay, f, room := actionFixture(t)
	blocked := filepath.Join(relay.deps.DataDir, "worktrees")
	if err := os.WriteFile(blocked, []byte("demo block"), 0600); err != nil {
		t.Fatal(err)
	}
	path := "/api/rooms/" + room.ID + "/actions/action-1/dispatch"
	body := []byte(`{"agent":"codex","text":"demo task","confirmed":true}`)
	response, _ := relay.do(t, http.MethodPost, path, body, true)
	if response.StatusCode != 409 {
		t.Fatalf("first status=%d", response.StatusCode)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	response, raw := relay.do(t, http.MethodPost, path, body, true)
	if response.StatusCode != 200 {
		t.Fatalf("retry status=%d body=%s", response.StatusCode, raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.starts) != 1 {
		t.Fatalf("starts=%d", len(f.starts))
	}
}

func TestRoomActionRetryRemainsIdempotentAtDispatchLimit(t *testing.T) {
	relay, f, room := actionFixture(t)
	for i := 0; i < 5; i++ {
		d := store.Dispatch{ID: fmt.Sprintf("demo-limit-%d", i), ParentSessionID: room.ID, ParentSource: "room", TargetAgent: "codex", Cwd: room.Cwd, Status: "started"}
		if _, err := relay.store.SaveDispatch(d); err != nil {
			t.Fatal(err)
		}
	}
	_, err := mutateRoom(relay.deps, room.ID, func(room *Room) error {
		room.Actions = []RoomAction{{ID: "action-1", Text: "demo task", Agent: "codex", DispatchID: "demo-limit-0", Status: "submitting"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response, raw := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/action-1/dispatch", []byte(`{"agent":"codex","text":"demo task","confirmed":true}`), true)
	if response.StatusCode != 200 {
		t.Fatalf("retry status=%d body=%s", response.StatusCode, raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.starts) != 0 {
		t.Fatal("retry started another task")
	}
}

type gatedRoomActionLauncher struct {
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	count   int
}

func (*gatedRoomActionLauncher) CanDispatch(id string) bool { return id == "codex" }
func (f *gatedRoomActionLauncher) Start(ctx context.Context, _ orchestrator.StartOptions) orchestrator.StartResult {
	f.mu.Lock()
	f.count++
	f.mu.Unlock()
	close(f.entered)
	select {
	case <-f.release:
		return orchestrator.StartResult{Status: "started", SessionID: "demo-concurrent-session"}
	case <-ctx.Done():
		return orchestrator.StartResult{Status: "failed", Message: "demo cancelled"}
	}
}
func TestRoomActionConcurrentConfirmationsLaunchOnce(t *testing.T) {
	relay, _, room := actionFixture(t)
	f := &gatedRoomActionLauncher{entered: make(chan struct{}), release: make(chan struct{})}
	useDispatchLauncher(t, f)
	path := "/api/rooms/" + room.ID + "/actions/action-1/dispatch"
	body := `{"agent":"codex","text":"demo concurrent","confirmed":true}`
	done := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, relay.server.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+relay.token)
		req.Header.Set("Content-Type", "application/json")
		res, err := relay.client.Do(req)
		if err == nil {
			defer res.Body.Close()
			if res.StatusCode != 200 {
				err = fmt.Errorf("first status %d", res.StatusCode)
			}
		}
		done <- err
	}()
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first action did not start")
	}
	response, raw := relay.do(t, http.MethodPost, path, []byte(body), true)
	if response.StatusCode != 200 {
		close(f.release)
		t.Fatalf("concurrent status=%d %s", response.StatusCode, raw)
	}
	close(f.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.count != 1 {
		t.Fatalf("started %d tasks", f.count)
	}
}

func TestRoomActionRecoversPreparedWorktreeWithoutDuplicate(t *testing.T) {
	relay, f, room := actionFixture(t)
	const child = "dsp_demo_recovery"
	_, _, _, err := createDispatchWorktree(relay.deps.DataDir, room.Cwd, child)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mutateRoom(relay.deps, room.ID, func(room *Room) error {
		room.Actions = []RoomAction{{ID: "action-1", Text: "demo task", Agent: "codex", DispatchID: child, Status: "preparing"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response, raw := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/action-1/dispatch", []byte(`{"agent":"codex","text":"demo task","confirmed":true}`), true)
	if response.StatusCode != 200 {
		t.Fatalf("status=%d %s", response.StatusCode, raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.starts) != 1 {
		t.Fatalf("starts=%d", len(f.starts))
	}
}
func TestRoomActionPreservesProjectSubdirectory(t *testing.T) {
	relay, f, room := actionFixture(t)
	nested := filepath.Join(room.Cwd, "demo-subdir")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "demo.txt"), []byte("demo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "demo-subdir"}, {"-c", "user.name=Demo", "-c", "user.email=demo@example.invalid", "commit", "-qm", "demo nested"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = room.Cwd
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", err, out)
		}
	}
	_, err := mutateRoom(relay.deps, room.ID, func(r *Room) error { r.Cwd = nested; return nil })
	if err != nil {
		t.Fatal(err)
	}
	response, raw := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/action-1/dispatch", []byte(`{"agent":"codex","text":"demo task","confirmed":true}`), true)
	if response.StatusCode != 200 {
		t.Fatalf("status=%d %s", response.StatusCode, raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.starts) != 1 || filepath.Base(f.starts[0].Cwd) != "demo-subdir" {
		t.Fatalf("child cwd: %+v", f.starts)
	}
}

type earlyRoomActionLauncher struct{ relay *testRelay }

func (*earlyRoomActionLauncher) CanDispatch(id string) bool { return id == "codex" }
func (f *earlyRoomActionLauncher) Start(_ context.Context, _ orchestrator.StartOptions) orchestrator.StartResult {
	event, _ := f.relay.store.AppendEvent(store.Event{Source: "codex", SessionID: "demo-early-action", Kind: "reply", Text: "demo early result"})
	trackDispatchLifecycle(f.relay.deps, event)
	return orchestrator.StartResult{Status: "started", SessionID: "demo-early-action"}
}
func TestRoomActionCompletionDuringLaunchAppearsInRoom(t *testing.T) {
	relay, _, room := actionFixture(t)
	useDispatchLauncher(t, &earlyRoomActionLauncher{relay})
	response, raw := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/action-1/dispatch", []byte(`{"agent":"codex","text":"demo task","confirmed":true}`), true)
	if response.StatusCode != 200 {
		t.Fatalf("status=%d %s", response.StatusCode, raw)
	}
	response, raw = relay.do(t, http.MethodGet, "/api/rooms/"+room.ID, nil, true)
	if response.StatusCode != 200 {
		t.Fatal(string(raw))
	}
	var p struct {
		Room struct {
			Actions []RoomAction `json:"actions"`
		} `json:"room"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Room.Actions) == 0 || p.Room.Actions[0].Status != "completed" || p.Room.Actions[0].Result != "demo early result" {
		t.Fatalf("early result lost: %s", raw)
	}
}

func TestRoomActionRejectsChangedConfirmedBase(t *testing.T) {
	relay, f, room := actionFixture(t)
	response, raw := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/action-1/dispatch", []byte(`{"agent":"codex","text":"demo task","confirmed":true,"base_commit":"demo-old-commit"}`), true)
	if response.StatusCode != 409 {
		t.Fatalf("status=%d body=%s", response.StatusCode, raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.starts) != 0 {
		t.Fatal("started task against unexpected commit")
	}
}

func TestRoomActionEarlyCompletionReconcilesAfterReload(t *testing.T) {
	relay, f, room := actionFixture(t)
	const child = "dsp_demo_after_restart"
	_, err := mutateRoom(relay.deps, room.ID, func(r *Room) error {
		r.Actions = []RoomAction{{ID: "action-1", Text: "demo task", Agent: "codex", DispatchID: child, Status: "launching"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = relay.store.AppendEvent(store.Event{Source: "codex", SessionID: "demo-after-restart", Kind: "reply", Text: "demo recovered result"}); err != nil {
		t.Fatal(err)
	}
	if _, err = relay.store.SaveDispatch(store.Dispatch{ID: child, ParentSource: "room", ParentSessionID: room.ID, TargetAgent: "codex", TargetSessionID: "demo-after-restart", Cwd: room.Cwd, Status: "started"}); err != nil {
		t.Fatal(err)
	}
	roomsMu.Lock()
	delete(roomsByID, room.ID)
	delete(roomsLoaded, relay.deps.DataDir)
	roomsMu.Unlock()
	response, raw := relay.do(t, http.MethodGet, "/api/rooms/"+room.ID, nil, true)
	if response.StatusCode != 200 {
		t.Fatal(string(raw))
	}
	var p struct {
		Room struct {
			Actions []RoomAction `json:"actions"`
		} `json:"room"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Room.Actions) == 0 || p.Room.Actions[0].Result != "demo recovered result" || p.Room.Actions[0].Status != "completed" {
		t.Fatalf("result not recovered: %s", raw)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.starts) != 0 {
		t.Fatal("recovery relaunched task")
	}
}
