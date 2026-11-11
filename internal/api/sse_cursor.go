package api

import (
	"fmt"
	"strconv"
	"strings"
)

// streamCursor is the resumable position of one /api/stream connection. A
// stream carries two independent sequences: durable event ids and live item
// sequence numbers. Encoding both in one SSE id lets a client resume without
// having to know which frame arrived last.
type streamCursor struct {
	EventID int64
	ItemSeq int
}

func parseStreamCursor(raw string) streamCursor {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return streamCursor{}
	}
	var cursor streamCursor
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '.' || r == ',' || r == ';' || r == ' ' || r == '\t'
	}) {
		token = strings.ToLower(strings.TrimSpace(token))
		switch {
		case strings.HasPrefix(token, "e") || strings.HasPrefix(token, "event:"):
			value := strings.TrimPrefix(strings.TrimPrefix(token, "event:"), "e")
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > cursor.EventID {
				cursor.EventID = parsed
			}
		case strings.HasPrefix(token, "i") || strings.HasPrefix(token, "item:"):
			value := strings.TrimPrefix(strings.TrimPrefix(token, "item:"), "i")
			if parsed, err := strconv.Atoi(value); err == nil && parsed > cursor.ItemSeq {
				cursor.ItemSeq = parsed
			}
		default:
			// Older clients sent the durable event id as a bare number.
			if parsed, err := strconv.ParseInt(token, 10, 64); err == nil && parsed > cursor.EventID {
				cursor.EventID = parsed
			}
		}
	}
	return cursor
}

func (c streamCursor) String() string {
	return fmt.Sprintf("e%d.i%d", c.EventID, c.ItemSeq)
}

func (c streamCursor) eventIDString() string {
	return fmt.Sprintf("e%d.i%d", c.EventID, c.ItemSeq)
}
