package agent

// CanonicalEventKind maps the public vocabulary used by custom-agent hooks and
// ingest tokens to the kinds stored in the event log. The phone API exposes the
// waiting states as needs_reply / approval again, so custom integrations and the
// Android inbox use the same words, while notifications and session status keep
// their single internal spelling.
func CanonicalEventKind(kind string) string {
	switch kind {
	case "needs_reply":
		return "needs_input"
	case "approval":
		return "permission"
	default:
		return kind
	}
}
