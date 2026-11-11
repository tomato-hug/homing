package workbuddy

import "testing"

func TestCapabilitiesDoNotClaimUnsafeNewSessions(t *testing.T) {
	caps := (Agent{}).Capabilities()
	if caps.NewSession || caps.NewSessionAny {
		t.Fatalf("new session capabilities = %+v, want disabled", caps)
	}
	if caps.Stop {
		t.Fatalf("stop capability = true without an owned session: %+v", caps)
	}
	if !caps.Reply || !caps.Sessions {
		t.Fatalf("read/reply capabilities were lost: %+v", caps)
	}
}
