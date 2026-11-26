package api

import "testing"

func TestParseRunningAgentsFromWindowsTasklistAndCmdlines(t *testing.T) {
	lines := []string{
		`"System Idle Process","0","Services","0","8 K"`,
		`"Homing.exe","4120","Console","1","38,400 K"`,
		`"Cursor.exe","8812","Console","1","412,900 K"`,
		`"WorkBuddy.exe","9910","Console","1","298,100 K"`,
		`"codex.exe","10234","Console","1","54,200 K"`,
		`"opencode.exe","10555","Console","1","61,000 K"`,
		`node.exe C:\Users\demo\AppData\Roaming\npm\node_modules\openclaw\dist\index.js`,
	}
	got := parseRunningAgentsFromPS(lines)
	for _, want := range []string{"cursor", "workbuddy", "codex", "opencode", "openclaw"} {
		if !got[want] {
			t.Fatalf("expected agent %q to be detected as running on Windows, got=%v", want, got)
		}
	}
	if got["homing"] {
		t.Fatalf("homing itself must not be reported as an agent: %v", got)
	}
}
