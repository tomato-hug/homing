package relayapi

import (
	"context"
	"fmt"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/driver"
)

// effectiveCaps applies the same ACP overlay as /api/agents. Keeping the rule
// in one place means a target cannot look dispatchable in the app but fail at
// launch because only the catalog knew about its driver.
func effectiveCaps(a agent.Agent) agent.Capabilities {
	caps := agent.EffectiveCapabilities(a)
	spec, ok := driver.SpecFor(a.ID())
	if !ok || !spec.Dispatch {
		return caps
	}
	ready := driver.CheckReadiness(spec)
	usable := ready.Ready && ready.Installed
	caps.NewSession = usable
	caps.Stop = usable
	caps.Approve = usable
	caps.Live = usable
	caps.Dispatch = usable
	return caps
}

// startAgentSession prefers the agent's own launcher and falls back to the
// generic ACP manager. The fallback is what lets a read-only scanner such as
// Kimi or OpenCode gain L3/L4 without a second registration that would shadow
// its session reader.
func startAgentSession(ctx context.Context, a agent.Agent, req agent.StartRequest) (agent.StartResult, error) {
	caps := effectiveCaps(a)
	if !caps.NewSession {
		if spec, ok := driver.SpecFor(a.ID()); ok {
			readiness := driver.CheckReadiness(spec)
			if !readiness.Installed {
				return agent.StartResult{}, fmt.Errorf("%w：找不到 %s", agent.ErrUnsupported, spec.CommandString())
			}
			if !readiness.Ready {
				return agent.StartResult{}, fmt.Errorf("%w：%s", agent.ErrUnsupported, readiness.Detail)
			}
		}
		return agent.StartResult{}, agent.ErrUnsupported
	}
	if starter, ok := a.(agent.SessionStarter); ok {
		return starter.StartSession(ctx, req)
	}
	spec, ok := driver.SpecFor(a.ID())
	if !ok || !spec.Dispatch {
		return agent.StartResult{}, agent.ErrUnsupported
	}
	sessionID, err := driver.Default().Start(ctx, a.ID(), req.Cwd, req.Text, nil)
	if err != nil {
		return agent.StartResult{}, err
	}
	return agent.StartResult{SessionID: sessionID, Status: "started"}, nil
}
