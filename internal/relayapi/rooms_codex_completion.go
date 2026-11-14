package relayapi

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tomato-hug/homing/internal/platform"
)

var roomCodexSessionsDir = func() string { return filepath.Join(platform.Home(), ".codex", "sessions") }
var roomCodexSessionID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// roomCodexFinished accepts only a complete native turn in the newly launched
// session and the expected worktree. An assistant message alone may precede a
// tool call or a permission request and is not evidence of completion.
func roomCodexFinished(root, session, cwd string) (string, bool) {
	if !roomCodexSessionID.MatchString(session) || cwd == "" {
		return "", false
	}
	expectedCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", false
	}
	files, err := filepath.Glob(filepath.Join(root, "*", "*", "*", "rollout-*-"+session+".jsonl"))
	if err != nil || len(files) == 0 {
		return "", false
	}
	f, err := os.Open(files[len(files)-1])
	if err != nil {
		return "", false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.Size() == 0 {
		return "", false
	}
	var last [1]byte
	if _, err = f.ReadAt(last[:], stat.Size()-1); err != nil || last[0] != '\n' {
		return "", false
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
	validMeta := false
	turn := ""
	result := ""
	complete := false
	first := true
	for scan.Scan() {
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Type             string `json:"type"`
				ID               string `json:"id"`
				Cwd              string `json:"cwd"`
				TurnID           string `json:"turn_id"`
				LastAgentMessage string `json:"last_agent_message"`
			} `json:"payload"`
		}
		if json.Unmarshal(scan.Bytes(), &row) != nil {
			return "", false
		}
		if first {
			first = false
			actualCwd, err := filepath.EvalSymlinks(row.Payload.Cwd)
			validMeta = row.Type == "session_meta" && row.Payload.ID == session && err == nil && actualCwd == expectedCwd
			if !validMeta {
				return "", false
			}
			continue
		}
		if row.Type != "event_msg" {
			continue
		}
		switch row.Payload.Type {
		case "task_started":
			turn = row.Payload.TurnID
			result = ""
			complete = false
		case "task_complete":
			complete = turn != "" && row.Payload.TurnID == turn && strings.TrimSpace(row.Payload.LastAgentMessage) != ""
			if complete {
				result = row.Payload.LastAgentMessage
			}
		case "turn_aborted":
			complete = false
			result = ""
		}
	}
	if scan.Err() != nil || !validMeta || !complete {
		return "", false
	}
	after, err := f.Stat()
	if err != nil || after.Size() != stat.Size() {
		return "", false
	}
	return result, true
}
