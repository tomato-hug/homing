package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/tomato-hug/homing/internal/agent"
)

// TestBuiltinsRegistered guards the import block above: forgetting a line here
// means /api/agents silently loses a card.
func TestBuiltinsRegistered(t *testing.T) {
	got := map[string]bool{}
	for _, a := range agent.All(context.Background()) {
		got[a.ID()] = true
	}
	for _, id := range []string{"codex", "claude", "workbuddy", "opencode", "zcode"} {
		if !got[id] {
			t.Errorf("agent %q is not registered", id)
		}
	}
}

// TestBuiltinsAnswerTheInterface keeps every package compiling against the
// interface the API reads, and catches a nil icon or an unknown push signal.
func TestBuiltinsAnswerTheInterface(t *testing.T) {
	for _, a := range agent.All(context.Background()) {
		if a.ID() == "" || a.Name() == "" {
			t.Errorf("%T: empty id or name", a)
		}
		if !strings.HasPrefix(a.Icon(), "<svg") {
			t.Errorf("%s: icon is not an svg", a.ID())
		}
		if !a.Capabilities().Sessions {
			t.Errorf("%s: must read sessions", a.ID())
		}
		switch a.PushSignal() {
		case agent.PushHook, agent.PushHTTPEvents, agent.PushPoll:
		default:
			t.Errorf("%s: unknown push signal %q", a.ID(), a.PushSignal())
		}
	}
}
