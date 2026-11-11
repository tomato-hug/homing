package api

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/tomato-hug/homing/internal/agent"
)

const maxSSEFrameBytes = 48 * 1024

// prepareNamedPayload keeps live item frames below the cloud/SSE frame budget.
// The durable items endpoint still returns the complete transcript.
func prepareNamedPayload(name string, data any) any {
	if name != "item" {
		return data
	}
	payload, ok := data.(map[string]any)
	if !ok {
		return data
	}
	item, ok := payload["item"].(agent.Item)
	if !ok {
		return data
	}
	sessionID, _ := payload["session_id"].(string)
	prepared, changed := truncateItemFrame(sessionID, item)
	if !changed {
		return data
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = value
	}
	out["item"] = prepared
	return out
}

func truncateItemFrame(sessionID string, item agent.Item) (agent.Item, bool) {
	raw, err := json.Marshal(map[string]any{"session_id": sessionID, "item": item})
	if err != nil || len(raw) <= maxSSEFrameBytes {
		return item, false
	}

	out := cloneItem(item)
	out.Truncated = true
	if out.Text != "" {
		out.Text = truncateUTF8Bytes(out.Text, 16<<10)
	}
	if out.Error != "" {
		out.Error = truncateUTF8Bytes(out.Error, 4<<10)
	}
	if out.Tool != nil {
		if out.Tool.InputSummary != nil {
			value := truncateUTF8Bytes(*out.Tool.InputSummary, 4<<10)
			out.Tool.InputSummary = &value
		}
		if out.Tool.OutputTail != nil {
			value := truncateUTF8Bytes(*out.Tool.OutputTail, 8<<10)
			out.Tool.OutputTail = &value
		}
	}
	for index := range out.Files {
		out.Files[index].Diff = truncateUTF8Bytes(out.Files[index].Diff, 8<<10)
	}

	for frameSize(sessionID, out) > maxSSEFrameBytes {
		changed := false
		out.Text, changed = halveField(out.Text, changed)
		out.Error, changed = halveField(out.Error, changed)
		if out.Tool != nil {
			if out.Tool.InputSummary != nil {
				value, reduced := halveField(*out.Tool.InputSummary, false)
				if reduced {
					out.Tool.InputSummary = &value
					changed = true
				}
			}
			if out.Tool.OutputTail != nil {
				value, reduced := halveField(*out.Tool.OutputTail, false)
				if reduced {
					out.Tool.OutputTail = &value
					changed = true
				}
			}
		}
		for index := range out.Files {
			out.Files[index].Diff, changed = halveField(out.Files[index].Diff, changed)
		}
		if !changed && len(out.Files) > 1 {
			out.Files = out.Files[:maxInt(1, len(out.Files)/2)]
			changed = true
		}
		if !changed {
			// A syntactically valid but unusually large item can still exceed the
			// budget through arbitrary metadata. Preserve the frame header and
			// make it clear that the durable endpoint has the full record.
			out.Text = ""
			out.Tool = nil
			out.Plan = nil
			out.Files = nil
			break
		}
	}
	return out, true
}

func cloneItem(item agent.Item) agent.Item {
	out := item
	out.Files = append([]agent.ItemFile(nil), item.Files...)
	out.Plan = append([]agent.ItemPlan(nil), item.Plan...)
	if item.Tool != nil {
		tool := *item.Tool
		out.Tool = &tool
	}
	return out
}

func frameSize(sessionID string, item agent.Item) int {
	raw, err := json.Marshal(map[string]any{"session_id": sessionID, "item": item})
	if err != nil {
		return maxSSEFrameBytes + 1
	}
	return len(raw)
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	if cut <= 0 {
		return ""
	}
	trimmed := strings.TrimRight(value[:cut], " \t\r\n")
	if trimmed == "" {
		return ""
	}
	return trimmed + "\n…（即時預覽已截斷）"
}

func halveField(value string, changed bool) (string, bool) {
	if len(value) <= 256 {
		return value, changed
	}
	return truncateUTF8Bytes(value, len(value)/2), true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
