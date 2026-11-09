package agent

import "testing"

func TestMergeItemKeepsCallFieldsAndTakesResult(t *testing.T) {
	summary := `{"command": "ls"}`
	call := Item{
		ID: "workbuddy-tool-1", Seq: 3, Type: "tool_call", Role: "assistant",
		Command: summary,
		Tool:    &ItemTool{Name: "Bash", InputSummary: &summary, Status: "running"},
	}
	tail := "Exit Code: 0"
	code := 0
	result := Item{
		ID: "workbuddy-tool-1", Seq: 4, Type: "tool_call", Role: "tool",
		Tool: &ItemTool{Name: "tool", OutputTail: &tail, ExitCode: &code, Status: "done"},
	}

	got := MergeItem(call, result)

	if got.Role != "tool" || got.Seq != 4 {
		t.Fatalf("role/seq should come from the result, got %q/%d", got.Role, got.Seq)
	}
	if got.Command != summary {
		t.Fatalf("command lost: %q", got.Command)
	}
	if got.Tool == nil || got.Tool.Name != "Bash" {
		t.Fatalf("tool name should fall back to the call's, got %+v", got.Tool)
	}
	if got.Tool.InputSummary == nil || *got.Tool.InputSummary != summary {
		t.Fatalf("input_summary lost: %+v", got.Tool.InputSummary)
	}
	if got.Tool.OutputTail == nil || *got.Tool.OutputTail != tail || got.Tool.Status != "done" {
		t.Fatalf("result fields not applied: %+v", got.Tool)
	}
	if got.Tool.ExitCode == nil || *got.Tool.ExitCode != 0 {
		t.Fatalf("exit code lost: %+v", got.Tool.ExitCode)
	}
	// the call must stay untouched
	if call.Tool.OutputTail != nil || call.Tool.Status != "running" {
		t.Fatalf("MergeItem modified its input: %+v", call.Tool)
	}
}

func TestMergeItemKeepsFirstType(t *testing.T) {
	prev := Item{ID: "x", Type: "file_change", Files: []ItemFile{{Path: "a.go"}}}
	next := Item{ID: "x", Type: "tool_call", Role: "tool"}
	got := MergeItem(prev, next)
	if got.Type != "file_change" || len(got.Files) != 1 {
		t.Fatalf("type/files should stay the call's, got %q %v", got.Type, got.Files)
	}
}
