package orchestrator

import (
	"encoding/json"
	"os"
	"testing"
)

type mentionVector struct {
	Version            int      `json:"version"`
	DispatchableAgents []string `json:"dispatchable_agents"`
	Cases              []struct {
		Name     string `json:"name"`
		Input    string `json:"input"`
		Message  string `json:"message"`
		Mentions []struct {
			Agent        string `json:"agent"`
			Preset       string `json:"preset"`
			Text         string `json:"text"`
			Dispatchable bool   `json:"dispatchable"`
		} `json:"mentions"`
	} `json:"cases"`
}

func TestMentionVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/mentions.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector mentionVector
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, agent := range vector.DispatchableAgents {
		known[agent] = true
	}
	for _, tc := range vector.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			message, mentions := ParseMentions(tc.Input)
			if message != tc.Message {
				t.Fatalf("message = %q, want %q", message, tc.Message)
			}
			if len(mentions) != len(tc.Mentions) {
				t.Fatalf("mentions = %+v, want %d", mentions, len(tc.Mentions))
			}
			for i, expected := range tc.Mentions {
				got := mentions[i]
				if got.Agent != expected.Agent || got.Preset != expected.Preset || got.Text != expected.Text {
					t.Fatalf("mention[%d] = %+v, want %+v", i, got, expected)
				}
				if known[got.Agent] != expected.Dispatchable {
					t.Fatalf("mention[%d] dispatchable = %v, want %v", i, known[got.Agent], expected.Dispatchable)
				}
				_ = ValidateMention(got)
			}
		})
	}
}

func TestValidateMentionRejectsEmptyTaskAndUnknownPreset(t *testing.T) {
	if err := ValidateMention(Mention{Agent: "claude", Preset: "ask"}); err == nil {
		t.Fatal("empty task should be rejected")
	}
	if err := ValidateMention(Mention{Agent: "claude", Preset: "raview", Text: "demo"}); err == nil {
		t.Fatal("unknown preset should be rejected")
	}
}
