package relayapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/orchestrator"
)

type fakeRoomSpeaker struct {
	mu      sync.Mutex
	prompts map[string][]string
	order   []string
}

func (f *fakeRoomSpeaker) Speak(_ context.Context, _ Room, p RoomParticipant, prompt string, _ func(string)) (roomTurn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts[p.Agent] = append(f.prompts[p.Agent], prompt)
	f.order = append(f.order, p.Agent)
	n := len(f.prompts[p.Agent])
	return roomTurn{
		Text:       fmt.Sprintf("%s 的第 %d 次发言", p.Agent, n),
		DispatchID: "demo-dispatch-" + p.Agent,
		SessionID:  "demo-session-" + p.Agent,
	}, nil
}

func useRoomSpeaker(t *testing.T, speaker roomSpeaker) {
	t.Helper()
	previous := roomSpeakers
	roomSpeakers = func(*api.Deps) roomSpeaker { return speaker }
	t.Cleanup(func() { roomSpeakers = previous })
}

func waitRoomStatus(t *testing.T, relay *testRelay, id, status string) Room {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, body := relay.do(t, http.MethodGet, "/api/rooms/"+id, nil, true)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("get room status=%d body=%s", response.StatusCode, body)
		}
		var payload struct {
			Room Room `json:"room"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Room.Status == status {
			return payload.Room
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("room %s never reached status %s", id, status)
	return Room{}
}

func TestRoomRoundRobinThenMinutes(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{
		targets: map[string]bool{"codex": true, "claude": true},
		result:  orchestrator.StartResult{Status: "started"},
	})
	speaker := &fakeRoomSpeaker{prompts: map[string][]string{}}
	useRoomSpeaker(t, speaker)
	relay := newTestRelay(t, nil)
	cwd := demoCwd(t)

	payload := fmt.Sprintf(`{
		"topic": "示例议题：登录模块要不要拆成独立服务",
		"cwd": %q,
		"rounds": 2,
		"participants": [{"agent": "codex", "role": "架构"}, {"agent": "claude", "role": "审查"}]
	}`, cwd)
	response, body := relay.do(t, http.MethodPost, "/api/rooms", []byte(payload), true)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", response.StatusCode, body)
	}
	var created struct {
		Room Room `json:"room"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	room := waitRoomStatus(t, relay, created.Room.ID, roomStatusEnded)

	speaker.mu.Lock()
	order := append([]string(nil), speaker.order...)
	codexPrompts := append([]string(nil), speaker.prompts["codex"]...)
	speaker.mu.Unlock()

	// 两轮轮流发言，然后由第一位（codex）整理纪要。
	want := []string{"codex", "claude", "codex", "claude", "codex"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("speaking order=%v want %v", order, want)
	}
	statements := 0
	for _, msg := range room.Messages {
		if msg.Kind == "statement" {
			statements++
		}
	}
	if statements != 4 {
		t.Fatalf("statements=%d messages=%+v", statements, room.Messages)
	}
	if room.Minutes != "codex 的第 3 次发言" || room.Messages[len(room.Messages)-1].Kind != "minutes" {
		t.Fatalf("minutes=%q last=%+v", room.Minutes, room.Messages[len(room.Messages)-1])
	}
	if !strings.Contains(codexPrompts[0], "示例议题") || !strings.Contains(codexPrompts[0], "你是第一位") {
		t.Fatalf("first prompt missing topic or opener: %s", codexPrompts[0])
	}
	// 第二轮只带 claude 的发言，不重复 codex 自己说过的话。
	if !strings.Contains(codexPrompts[1], "claude 的第 1 次发言") || strings.Contains(codexPrompts[1], "codex 的第 1 次发言") {
		t.Fatalf("second prompt should carry only the new statements: %s", codexPrompts[1])
	}
	if !strings.Contains(codexPrompts[2], "会议纪要") {
		t.Fatalf("minutes prompt=%s", codexPrompts[2])
	}
	for _, p := range room.Participants {
		if p.DispatchID != "demo-dispatch-"+p.Agent || p.SessionID != "demo-session-"+p.Agent {
			t.Fatalf("participant ids not recorded: %+v", p)
		}
	}

	// 结束后的会议不能再发言。
	response, _ = relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/messages", []byte(`{"text":"补充一句"}`), true)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("message after end status=%d", response.StatusCode)
	}
}

func TestRoomPromptCarriesUserMessages(t *testing.T) {
	room := Room{
		ID: "room-demo", Title: "示例", Topic: "示例议题", Cwd: "/tmp/demo", Rounds: 2,
		Participants: []RoomParticipant{
			{Agent: "codex", Name: "Codex", Role: "架构", DispatchID: "d1", Spoken: true, LastSeen: 1},
			{Agent: "claude", Name: "Claude Code", DispatchID: "d2", Spoken: true, LastSeen: 2},
		},
		Messages: []RoomMessage{
			{Seq: 1, From: "codex", Name: "Codex", Kind: "statement", Round: 1, Text: "示例：先拆接口"},
			{Seq: 2, From: "claude", Name: "Claude Code", Kind: "statement", Round: 1, Text: "示例：同意，但要先补测试"},
			{Seq: 3, From: "user", Name: "你", Kind: "user", Round: 1, Text: "示例：预算只有两周"},
		},
	}
	prompt := buildRoomPrompt(room, 0, 2)
	if !strings.Contains(prompt, "用户：示例：预算只有两周") || !strings.Contains(prompt, "示例：同意，但要先补测试") {
		t.Fatalf("delta prompt missing new content: %s", prompt)
	}
	if strings.Contains(prompt, "示例：先拆接口") {
		t.Fatalf("delta prompt repeats the speaker's own statement: %s", prompt)
	}
	if !strings.Contains(prompt, "只读") {
		t.Fatalf("prompt must restate the read-only rule: %s", prompt)
	}
}

func TestNextRoomSpeakerSkipsFailedParticipants(t *testing.T) {
	room := Room{
		Rounds: 2,
		Participants: []RoomParticipant{
			{Agent: "codex", Status: "waiting"},
			{Agent: "claude", Status: "failed"},
		},
		Messages: []RoomMessage{
			{Seq: 1, From: "codex", Kind: "statement", Round: 1},
			{Seq: 2, From: "claude", Kind: "system", Round: 1},
		},
	}
	idx, round := nextRoomSpeaker(room)
	if idx != 0 || round != 2 {
		t.Fatalf("next=%d round=%d", idx, round)
	}
	room.Messages = append(room.Messages, RoomMessage{Seq: 3, From: "codex", Kind: "statement", Round: 2})
	if idx, _ := nextRoomSpeaker(room); idx != -1 {
		t.Fatalf("all rounds done, got %d", idx)
	}
}

func TestNewRoomValidation(t *testing.T) {
	useDispatchLauncher(t, dispatchFakeLauncher{targets: map[string]bool{"codex": true, "claude": true}})
	cwd := demoCwd(t)
	two := []struct {
		Agent string `json:"agent"`
		Role  string `json:"role"`
	}{{Agent: "codex"}, {Agent: "claude"}}
	cases := []struct {
		name string
		req  createRoomRequest
		code string
	}{
		{"no topic", createRoomRequest{Cwd: cwd, Participants: two}, "topic_required"},
		{"no cwd", createRoomRequest{Topic: "示例", Participants: two}, "cwd_required"},
		{"missing cwd", createRoomRequest{Topic: "示例", Cwd: cwd + "/nope", Participants: two}, "cwd_invalid"},
		{"one agent", createRoomRequest{Topic: "示例", Cwd: cwd, Participants: two[:1]}, "participants_invalid"},
		{"too many rounds", createRoomRequest{Topic: "示例", Cwd: cwd, Rounds: 9, Participants: two}, "rounds_invalid"},
		{"other mode", createRoomRequest{Topic: "示例", Cwd: cwd, Mode: "council", Participants: two}, "mode_unsupported"},
	}
	for _, tc := range cases {
		if _, code, err := newRoom(context.Background(), tc.req); err == nil || code != tc.code {
			t.Errorf("%s: code=%q err=%v, want %s", tc.name, code, err, tc.code)
		}
	}
	room, _, err := newRoom(context.Background(), createRoomRequest{Topic: "示例议题\n第二行", Cwd: cwd, Participants: two})
	if err != nil {
		t.Fatal(err)
	}
	if room.Title != "示例议题" || room.Rounds != roomDefaultRounds || room.Status != roomStatusRunning {
		t.Fatalf("room=%+v", room)
	}
}
