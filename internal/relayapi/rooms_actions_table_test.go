package relayapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestRoomActionsFromMarkdownTableKeepExistingListIDs(t *testing.T) {
	minutes := "## 行动项\n| 负责人 | 行动 | 验收标准 |\n| :--- | --- | ---: |\n| Codex | 添加 demo 文件 | 内容正确 |\n| Hermes | 核对 demo 结果 | 返回结论 |\n\n- 既有示例行动\n\n## 其他\n| 负责人 | 行动 | 验收标准 |\n| --- | --- | --- |\n| Demo | 不应提取 | 不应提取 |\n"
	actions := proposedRoomActions(minutes)
	if len(actions) != 3 {
		t.Fatalf("got %d actions, want existing list plus two table rows", len(actions))
	}
	if actions[0].ID != "action-1" || actions[0].Text != "既有示例行动" {
		t.Fatalf("existing list ID changed: %+v", actions[0])
	}
	for i, want := range []struct{ id, task, owner, acceptance string }{{"action-table-1", "添加 demo 文件", "Codex", "内容正确"}, {"action-table-2", "核对 demo 结果", "Hermes", "返回结论"}} {
		got := actions[i+1]
		if got.ID != want.id || !strings.Contains(got.Text, want.task) || !strings.Contains(got.Text, want.owner) || !strings.Contains(got.Text, want.acceptance) {
			t.Fatalf("table action %d: %+v", i, got)
		}
	}
}

func TestRoomTableActionCanBeDispatched(t *testing.T) {
	relay, launcher, room := actionFixture(t)
	_, err := mutateRoom(relay.deps, room.ID, func(r *Room) error {
		r.Minutes = "## 行动项\n| 负责人 | 行动 | 验收标准 |\n| --- | --- | --- |\n| Codex | 完成 demo 任务 | 检查 demo 文件 |\n"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	res, raw := relay.do(t, http.MethodGet, "/api/rooms/"+room.ID, nil, true)
	if res.StatusCode != 200 {
		t.Fatalf("get status=%d", res.StatusCode)
	}
	var payload struct {
		Room Room `json:"room"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Room.Actions) != 1 || payload.Room.Actions[0].ID != "action-table-1" {
		t.Fatalf("table action missing: %+v", payload.Room.Actions)
	}
	path := "/api/rooms/" + room.ID + "/actions/action-table-1/dispatch"
	body := []byte(`{"agent":"codex","text":"demo table action","confirmed":true}`)
	for i := 0; i < 2; i++ {
		res, raw = relay.do(t, http.MethodPost, path, body, true)
		if res.StatusCode != 200 {
			t.Fatalf("dispatch status=%d body=%s", res.StatusCode, raw)
		}
	}
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	if len(launcher.starts) != 1 {
		t.Fatalf("duplicate launch count=%d", len(launcher.starts))
	}
}

func TestRoomActionTablesIgnoreMalformedAndCodeBlocks(t *testing.T) {
	minutes := "## 行动项\n```\n| 负责人 | 行动 |\n| --- | --- |\n| Demo | fenced |\n```\n| 类型 | 说明 |\n| --- | --- |\n| Demo | not an action |\n\n| 负责人 | 行动 |\n| --- | --- |\n| Demo | execute \\| verify |\n| Demo | |\n"
	actions := proposedRoomActions(minutes)
	if len(actions) != 1 || !strings.Contains(actions[0].Text, "execute | verify") {
		t.Fatalf("unexpected table actions: %+v", actions)
	}
}

func TestRoomActionTablesDoNotDisplacePreviouslyVisibleListItems(t *testing.T) {
	var b strings.Builder
	b.WriteString("## 行动项\n| Owner | Task |\n| --- | --- |\n| Demo | new table action |\n\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "- demo list item %d\n", i+1)
	}
	actions := proposedRoomActions(b.String())
	if len(actions) != 30 {
		t.Fatalf("action count=%d", len(actions))
	}
	for i, a := range actions {
		if a.ID != fmt.Sprintf("action-%d", i+1) {
			t.Fatalf("list item %d displaced by table: %+v", i+1, a)
		}
	}
}
