package driver

import (
	"context"
	"fmt"
	"time"
)

// ProbeResult is the observable outcome of a short ACP handshake. It never
// contains prompt text: enabling a drive is a protocol check, not a test task.
type ProbeResult struct {
	SessionID           string         `json:"session_id"`
	AgentCapabilities   map[string]any `json:"agent_capabilities,omitempty"`
	SupportsLoadSession bool           `json:"supports_load_session"`
	HandshakeDurationMS int64          `json:"handshake_duration_ms"`
	ProtocolVersion     any            `json:"protocol_version,omitempty"`
	AgentImplementation any            `json:"agent_info,omitempty"`
}

// Probe starts one ACP process, completes initialize and session/new, then
// closes it. The caller is responsible for using a temporary cwd; Probe never
// writes project files and never sends a prompt.
func Probe(ctx context.Context, spec Spec, cwd string) (ProbeResult, error) {
	started := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	client, err := Start(probeCtx, spec, cwd, nil, nil)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("启动 ACP 失败: %w", err)
	}
	defer client.Close()

	initResult, err := client.Initialize(probeCtx)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("initialize 握手失败: %w", err)
	}
	sessionID, _, err := client.NewSession(probeCtx, cwd)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("session/new 失败: %w", err)
	}

	result := ProbeResult{
		SessionID:           sessionID,
		SupportsLoadSession: client.SupportsLoadSession(),
		HandshakeDurationMS: time.Since(started).Milliseconds(),
		ProtocolVersion:     initResult["protocolVersion"],
		AgentImplementation: initResult["agentInfo"],
	}
	if caps, ok := initResult["agentCapabilities"].(map[string]any); ok {
		result.AgentCapabilities = caps
	}
	return result, nil
}
