package api

import (
	"testing"
	"time"

	"github.com/tomato-hug/homing/internal/store"
)

func newTestFlow() *flowRecorder {
	return &flowRecorder{
		subs:      map[chan FlowHop]struct{}{},
		sources:   map[string]string{},
		lastItem:  map[string]time.Time{},
		dispatchs: map[string]string{},
		rooms:     map[string]flowRoomState{},
	}
}

func TestFlowEventThenReplyUsesLearnedSource(t *testing.T) {
	f := newTestFlow()
	f.onEvent(store.Event{ID: 1, Source: "claude", Kind: "Stop", SessionID: "s1", Text: "done"})
	f.onNamed(NamedMessage{Name: "reply_status", Data: map[string]any{"session_id": "s1", "status": "delivered"}})
	hops := f.since(0)
	if len(hops) != 2 {
		t.Fatalf("hops = %d, want 2", len(hops))
	}
	if hops[0].From != "agent:claude" || hops[0].To != "homing" || hops[0].Title != "完成一轮" {
		t.Fatalf("event hop = %+v", hops[0])
	}
	if hops[1].From != "homing" || hops[1].To != "agent:claude" || hops[1].Status != "ok" {
		t.Fatalf("reply hop = %+v", hops[1])
	}
}

func TestFlowDispatchOnlyOncePerDispatch(t *testing.T) {
	f := newTestFlow()
	dispatch := store.Dispatch{ID: "d1", ParentSource: "codex", TargetAgent: "claude", Preset: "review", Text: "看看这个改动", Status: "running"}
	f.onNamed(NamedMessage{Name: "dispatch", Data: dispatch})
	dispatch.Status = "done"
	f.onNamed(NamedMessage{Name: "dispatch", Data: dispatch})
	hops := f.since(0)
	if len(hops) != 1 {
		t.Fatalf("hops = %d, want 1", len(hops))
	}
	if hops[0].From != "agent:codex" || hops[0].To != "agent:claude" || hops[0].Title != "请求审查" {
		t.Fatalf("dispatch hop = %+v", hops[0])
	}
}

func TestFlowRoomSpeakerHop(t *testing.T) {
	f := newTestFlow()
	f.onNamed(NamedMessage{Name: "room", Data: map[string]any{"id": "r1", "title": "方案", "speaking": "codex", "message_count": 1.0, "status": "running"}})
	f.onNamed(NamedMessage{Name: "room", Data: map[string]any{"id": "r1", "title": "方案", "speaking": "claude", "message_count": 2.0, "status": "running", "preview": "Codex：我建议"}})
	hops := f.since(0)
	if len(hops) != 1 || hops[0].From != "agent:codex" || hops[0].To != "room:r1" {
		t.Fatalf("room hops = %+v", hops)
	}
}

func TestFlowShortTrimsLongPreview(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "字"
	}
	if got := []rune(flowShort(long)); len(got) != flowPreviewRunes+1 {
		t.Fatalf("len = %d", len(got))
	}
}
