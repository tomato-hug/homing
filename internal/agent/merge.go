package agent

// MergeItem combines two entries that share an id — a tool call and its later
// result — the way the Node relay's readRecentItems does (relay/server.mjs):
// `{...previous, ...next}`, then the type stays the first one's, files/plan fall
// back to the first when the second has none, and the tool block is merged key
// by key with input_summary and a useful name carried over from the call.
//
// A plain "last write wins" loses the call's command and input summary, which is
// what the phone shows for a tool call.
func MergeItem(prev, next Item) Item {
	merged := next
	if merged.Text == "" {
		merged.Text = prev.Text
	}
	if merged.Command == "" {
		merged.Command = prev.Command
	}
	if merged.ExitCode == nil {
		merged.ExitCode = prev.ExitCode
	}
	if merged.Error == "" {
		merged.Error = prev.Error
	}
	merged.Type = prev.Type
	if len(next.Files) == 0 {
		merged.Files = prev.Files
	}
	if len(next.Plan) == 0 {
		merged.Plan = prev.Plan
	}
	if prev.Tool != nil || next.Tool != nil {
		var tool ItemTool
		if prev.Tool != nil {
			tool = *prev.Tool
		}
		if next.Tool != nil {
			tool = *next.Tool
		}
		if prev.Tool != nil {
			if tool.InputSummary == nil || *tool.InputSummary == "" {
				tool.InputSummary = prev.Tool.InputSummary
			}
			if (tool.Name == "" || tool.Name == "tool") && prev.Tool.Name != "" {
				tool.Name = prev.Tool.Name
			}
		}
		merged.Tool = &tool
	}
	return merged
}
