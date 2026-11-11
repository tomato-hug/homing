// Package builtin imports every built-in agent so cmd/homing gets them all with
// one blank import. Add one line per agent package:
//
//	_ "github.com/tomato-hug/homing/internal/agent/codex"
//
// Owner: WorkBuddy.
package builtin

// Registration order is the compiler's business, not ours (gc sorts the
// package list, so it is not the order written here). The order agents appear
// in GET /api/agents is set explicitly in internal/api from the display order,
// which keeps the console's card order stable.
import (
	_ "github.com/tomato-hug/homing/internal/agent/claude"
	_ "github.com/tomato-hug/homing/internal/agent/codex"
	_ "github.com/tomato-hug/homing/internal/agent/kimi"
	_ "github.com/tomato-hug/homing/internal/agent/opencode"
	_ "github.com/tomato-hug/homing/internal/agent/workbuddy"
	_ "github.com/tomato-hug/homing/internal/agent/zcode"
)
