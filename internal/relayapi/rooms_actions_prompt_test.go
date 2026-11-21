package relayapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestConfirmedRoomActionSeparatesDiscussionFromExecution(t *testing.T) {
	relay, launcher, room := actionFixture(t)
	const topic = "demo discussion: 只讨论，等待用户确认，不要执行行动项。"
	const task = "demo confirmed task: 只读 README.md，确认标记后回复 demo-reviewed。"
	if _, err := mutateRoom(relay.deps, room.ID, func(current *Room) error {
		current.Topic = topic
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"agent": "codex", "text": task, "confirmed": true})
	res, raw := relay.do(t, http.MethodPost, "/api/rooms/"+room.ID+"/actions/action-1/dispatch", body, true)
	if res.StatusCode != 200 {
		t.Fatalf("dispatch status=%d: %s", res.StatusCode, raw)
	}
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	if len(launcher.starts) != 1 {
		t.Fatalf("starts=%d", len(launcher.starts))
	}
	prompt := launcher.starts[0].Text
	if strings.Index(prompt, topic) < 0 || strings.Index(prompt, task) <= strings.Index(prompt, topic) {
		t.Fatal("the confirmed task must follow the historical discussion context")
	}
	for _, boundary := range []string{"背景（仅供参考", "用户已确认", "只执行这项任务", "保留任务本身的只读"} {
		if !strings.Contains(prompt, boundary) {
			t.Errorf("missing execution boundary %q", boundary)
		}
	}
}
