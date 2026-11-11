package orchestrator

import (
	"fmt"
	"regexp"
	"strings"
)

// Mention is one @agent task found in a composer message.
type Mention struct {
	Agent  string
	Preset string
	Text   string
	Start  int
	End    int
}

// Presets are the four collaboration intents defined by SPEC §5.5.2.
var Presets = map[string]bool{
	"ask":     true,
	"do":      true,
	"review":  true,
	"handoff": true,
}

var mentionPattern = regexp.MustCompile(`(?i)@([a-z0-9][a-z0-9._-]{0,63})(?::([a-z-]{1,24}))?`)

// ParseMentions splits a composer message into the parent reply and one task
// per @agent mention. Syntax mirrors the Android parser: emails, inline code and
// fenced code blocks are ignored. Presets are captured but validated later so a
// typo such as @claude:raview fails loudly instead of silently becoming text.
func ParseMentions(input string) (string, []Mention) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", nil
	}
	type hit struct {
		agent  string
		preset string
		start  int
		end    int
	}
	var hits []hit
	fence := ""
	lineStart := 0
	for lineStart <= len(input) {
		newline := strings.IndexByte(input[lineStart:], '\n')
		lineEnd := len(input)
		if newline >= 0 {
			lineEnd = lineStart + newline
		}
		line := strings.TrimSuffix(input[lineStart:lineEnd], "\r")
		trimLine := strings.TrimLeft(line, " \t")
		marker := ""
		if strings.HasPrefix(trimLine, "```") {
			marker = "```"
		} else if strings.HasPrefix(trimLine, "~~~") {
			marker = "~~~"
		}
		if marker != "" {
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
		} else if fence == "" {
			for _, loc := range mentionPattern.FindAllStringSubmatchIndex(line, -1) {
				start := loc[0]
				if insideMentionToken(line, start) || insideInlineCode(line, start) {
					continue
				}
				agent := strings.ToLower(line[loc[2]:loc[3]])
				preset := ""
				if loc[4] >= 0 {
					preset = strings.ToLower(line[loc[4]:loc[5]])
				}
				hits = append(hits, hit{agent: agent, preset: preset, start: lineStart + start, end: lineStart + loc[1]})
			}
		}
		if newline < 0 {
			break
		}
		lineStart = lineEnd + 1
	}
	if len(hits) == 0 {
		return trimmed, nil
	}
	mentions := make([]Mention, 0, len(hits))
	for i, item := range hits {
		taskEnd := len(input)
		if i+1 < len(hits) {
			taskEnd = hits[i+1].start
		}
		preset := item.preset
		if preset == "" {
			preset = "ask"
		}
		mentions = append(mentions, Mention{
			Agent:  item.agent,
			Preset: preset,
			Text:   strings.TrimSpace(input[item.end:taskEnd]),
			Start:  item.start,
			End:    item.end,
		})
	}
	return strings.TrimSpace(input[:hits[0].start]), mentions
}

// ValidateMention rejects unknown presets and empty tasks before anything is
// sent. The HTTP layer maps this to 400.
func ValidateMention(m Mention) error {
	if !Presets[strings.ToLower(m.Preset)] {
		return fmt.Errorf("未知协作预设 %q", m.Preset)
	}
	if strings.TrimSpace(m.Text) == "" {
		return fmt.Errorf("@%s 后面没有任务内容", m.Agent)
	}
	return nil
}

func insideMentionToken(line string, index int) bool {
	if index <= 0 {
		return false
	}
	r := line[index-1]
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '_', r == '%', r == '+', r == '-':
		return true
	}
	return false
}

func insideInlineCode(line string, index int) bool {
	ticks := 0
	limit := index
	if limit > len(line) {
		limit = len(line)
	}
	for i := 0; i < limit; i++ {
		if line[i] == '`' {
			ticks++
		}
	}
	return ticks%2 == 1
}
