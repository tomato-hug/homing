package api

import (
	"context"
	"testing"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/driver"
)

type capabilityAgent struct {
	caps agent.Capabilities
}

func (a capabilityAgent) ID() string                   { return "demo" }
func (a capabilityAgent) Name() string                 { return "Demo" }
func (a capabilityAgent) Icon() string                 { return "demo" }
func (a capabilityAgent) PushSignal() agent.PushSignal { return agent.PushHook }
func (a capabilityAgent) Capabilities() agent.Capabilities {
	return a.caps
}
func (a capabilityAgent) Detect(context.Context) agent.Detection {
	return agent.Detection{Installed: true}
}
func (a capabilityAgent) ListSessions(context.Context, int) ([]agent.Session, error) {
	return nil, nil
}
func (a capabilityAgent) Meta(string, string) agent.Meta { return agent.Meta{} }
func (a capabilityAgent) StartSession(context.Context, agent.StartRequest) (agent.StartResult, error) {
	return agent.StartResult{SessionID: "demo-session"}, nil
}
func (a capabilityAgent) StopSession(context.Context, string) error { return nil }

func TestCapabilityMapComesFromAgentNotID(t *testing.T) {
	a := capabilityAgent{caps: agent.Capabilities{
		Reply: true, Sessions: true, NewSession: true, Stop: true,
		Dispatch: true, SwitchModel: true, Effort: true, Live: true,
		PermissionModes: []string{"default", "read_only"},
	}}
	caps := agent.EffectiveCapabilities(a)
	got := capabilityMap(a, caps, true)
	for _, key := range []string{
		"reply", "sessions", "new_session", "new_session_ready", "stop", "dispatch",
		"switch_model", "effort", "live",
	} {
		if !got[key] {
			t.Errorf("%s = false, want true", key)
		}
	}
	if got["approve"] {
		t.Error("approve = true without a hook or PermissionResponder")
	}
	if modes := caps.PermissionModes; len(modes) != 2 || modes[1] != "read_only" {
		t.Fatalf("permission modes = %#v", modes)
	}
}

func TestCapabilityMapInstalledAndLegacyCoreShape(t *testing.T) {
	a := capabilityAgent{caps: agent.Capabilities{
		Core: true, Reply: true, NewSession: true, Dispatch: true,
	}}
	got := capabilityMap(a, agent.EffectiveCapabilities(a), false)
	if got["new_session"] || got["dispatch"] {
		t.Fatalf("uninstalled capabilities = %#v", got)
	}
	if _, exists := got["sessions"]; exists {
		t.Fatalf("core legacy shape unexpectedly contains sessions: %#v", got)
	}
}

func TestCapabilityMapDoesNotClaimSessionsForDriveOnlyAdapter(t *testing.T) {
	a := capabilityAgent{caps: agent.Capabilities{
		Sessions:   false,
		NewSession: true,
		Stop:       true,
		Approve:    true,
		Live:       true,
		Dispatch:   true,
	}}
	got := capabilityMap(a, agent.EffectiveCapabilities(a), true)
	if got["sessions"] {
		t.Fatal("drive-only adapter claims sessions=true without a transcript reader")
	}
	for _, key := range []string{"new_session", "stop", "approve", "live", "dispatch"} {
		if !got[key] {
			t.Errorf("%s = false, want true", key)
		}
	}
}

func TestWithDriverCapabilitiesRequiresReadyACP(t *testing.T) {
	spec := driver.Spec{ID: "demo-acp", Dispatch: true}
	base := agent.Capabilities{
		Sessions: true, Reply: true, Models: true,
		NewSession: true, Stop: true, Approve: true, Live: true, Dispatch: true,
	}
	ready := withDriverCapabilities(base, spec, driver.Readiness{Installed: true, Ready: true})
	for _, got := range []bool{ready.NewSession, ready.Stop, ready.Approve, ready.Live, ready.Dispatch} {
		if !got {
			t.Fatalf("ready overlay = %+v", ready)
		}
	}

	broken := withDriverCapabilities(base, spec, driver.Readiness{Installed: true, Ready: false})
	for _, got := range []bool{broken.NewSession, broken.Stop, broken.Approve, broken.Live, broken.Dispatch} {
		if got {
			t.Fatalf("unready overlay left a drive capability on: %+v", broken)
		}
	}
	if !broken.Sessions || !broken.Reply || !broken.Models {
		t.Fatalf("overlay removed scanner capabilities: %+v", broken)
	}

	untouched := withDriverCapabilities(base, driver.Spec{Dispatch: false}, driver.Readiness{Ready: true})
	if !untouched.NewSession || !untouched.Dispatch {
		t.Fatalf("non-dispatch spec changed caps: %+v", untouched)
	}
}
