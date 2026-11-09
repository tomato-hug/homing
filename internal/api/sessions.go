package api

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/store"
)

// runningWindow is how long after a "reply" event a session still counts as
// running (Node: RUNNING_WINDOW_MS = 90s).
const runningWindow = 90 * time.Second

// ContextUsage is the phone's context ring.
type ContextUsage struct {
	Used  int  `json:"used"`
	Limit *int `json:"limit"`
}

// Summary is one row of GET /api/sessions. Field names and null-ness match the
// Node relay so the Android app and the console read it unchanged.
type Summary struct {
	SessionID      string        `json:"session_id"`
	Source         string        `json:"source"`
	Title          string        `json:"title"`
	TitleSource    string        `json:"title_source"`
	Workspace      *string       `json:"workspace"`
	Project        *string       `json:"project"`
	Branch         *string       `json:"branch"`
	Cwd            *string       `json:"cwd"`
	Model          *string       `json:"model"`
	Status         string        `json:"status"`
	Unread         int           `json:"unread"`
	LastTS         *string       `json:"last_ts"`
	LastPreview    *string       `json:"last_preview"`
	EventCount     int           `json:"event_count"`
	Effort         *string       `json:"effort"`
	PermissionMode *string       `json:"permission_mode"`
	ContextUsage   *ContextUsage `json:"context_usage"`
	Running        bool          `json:"running"`
	RunningSince   *string       `json:"running_since"`
	Read           bool          `json:"read"`
	Pinned         bool          `json:"pinned"`
	Muted          bool          `json:"muted"`
	Archived       bool          `json:"archived"`
	IsTemp         bool          `json:"is_temp"`
	IsEmpty        bool          `json:"is_empty"`
}

// contextReporter is the optional capability an agent has when it can report the
// token count of the last turn.
type contextReporter interface {
	ContextUsage(sessionID string) (used int, limit int, ok bool)
}

// sessionMeta is the derived metadata Node keeps in sessionMetaFor.
type sessionMeta struct {
	Cwd            string
	NativeTitle    string
	Title          string
	TitleSource    string
	FirstPrompt    string
	Branch         string
	Model          string
	Workspace      string
	Project        string
	Effort         string
	PermissionMode string
	ContextUsage   *ContextUsage
	NativeState    string
}

// metaFor asks the agent for the session metadata and fills in the shared
// derived fields (title, workspace, project, git branch).
func (d *Deps) metaFor(ctx context.Context, source, sessionID, cwdHint string) sessionMeta {
	out := sessionMeta{Cwd: cwdHint, TitleSource: "agent"}
	if a, ok := agent.Get(ctx, source); ok {
		m := a.Meta(sessionID, cwdHint)
		out.Cwd = firstNonEmpty(out.Cwd, m.Cwd)
		out.TitleSource = firstNonEmpty(m.TitleSource, "agent")
		out.FirstPrompt = m.FirstPrompt
		out.Branch = m.Branch
		out.Model = m.Model
		out.NativeTitle = m.NativeTitle
		out.Effort = m.Effort
		out.PermissionMode = m.PermissionMode
		out.Workspace = m.Workspace
		if reporter, ok := a.(contextReporter); ok {
			if used, limit, ok := reporter.ContextUsage(sessionID); ok {
				usage := &ContextUsage{Used: used}
				if limit > 0 {
					usage.Limit = &limit
				}
				out.ContextUsage = usage
			}
		}
	}
	if out.Branch == "HEAD" {
		out.Branch = ""
	}
	if out.Branch == "" {
		out.Branch = scanlib.GitBranch(out.Cwd)
	}
	title, titleSource := scanlib.Title(source, sessionID, out.NativeTitle, out.TitleSource, out.FirstPrompt, out.Cwd)
	out.Title = title
	out.TitleSource = titleSource
	out.Workspace = firstNonEmpty(out.Workspace, scanlib.WorkspaceLabel(out.Cwd))
	if strings.TrimSpace(out.Cwd) != "" {
		out.Project = path.Base(strings.TrimSpace(out.Cwd))
	}
	if out.Model == "<synthetic>" {
		out.Model = ""
	}
	return out
}

// ---- session index -------------------------------------------------------

// relayEntry accumulates the events of one session, like the Node relayIndex.
type relayEntry struct {
	sessionID     string
	source        string
	cwd           string
	eventCount    int
	unread        int
	lastTS        string
	lastKind      string
	lastPreview   string
	pendingInject bool
}

// relayIndex folds the event log into one entry per session.
func relayIndex(events []store.Event, replies []store.Reply) map[string]*relayEntry {
	index := map[string]*relayEntry{}
	for _, event := range events {
		if event.SessionID == "" {
			continue
		}
		key := event.Source + ":" + event.SessionID
		entry := index[key]
		if entry == nil {
			entry = &relayEntry{sessionID: event.SessionID, source: event.Source}
			index[key] = entry
		}
		entry.eventCount++
		if !event.Read {
			entry.unread++
		}
		if event.Cwd != "" {
			entry.cwd = event.Cwd
		}
		if entry.lastTS == "" || event.Ts >= entry.lastTS {
			entry.lastTS = event.Ts
			entry.lastKind = event.Kind
			if event.Kind == "reply" && event.Text != "" {
				entry.lastPreview = scanlib.FirstLine(event.Text, 120)
			}
		}
	}
	for _, reply := range replies {
		if reply.Status != "injected" && reply.Status != "held" {
			continue
		}
		source := ""
		if reply.EventID != nil {
			for _, event := range events {
				if event.ID == *reply.EventID {
					source = event.Source
					break
				}
			}
		}
		if entry := index[source+":"+reply.SessionID]; entry != nil && reply.Ts >= entry.lastTS {
			entry.pendingInject = true
		}
	}
	return index
}

// candidate is one session the list may contain: either known from the event
// log or discovered by an agent scanner.
type candidate struct {
	key    string
	relay  *relayEntry
	row    agent.Session
	lastTS string
}

// summaries builds the session list: event-log sessions first, then whatever the
// agents found on disk, sorted by last activity.
func (d *Deps) summaries(ctx context.Context, limit int, source, status string) []Summary {
	s, ok := d.store()
	if !ok {
		return nil
	}
	index := relayIndex(s.Events(), s.Replies())

	var candidates []candidate
	seen := map[string]bool{}
	for key, entry := range index {
		seen[key] = true
		candidates = append(candidates, candidate{key: key, relay: entry, lastTS: entry.lastTS})
	}
	for _, a := range agent.All(ctx) {
		if source != "" && a.ID() != source {
			continue
		}
		// Node lists every session an agent can find and truncates at the end;
		// the cap only protects against a runaway scan.
		rows, err := a.ListSessions(ctx, 1000)
		if err != nil {
			continue
		}
		for _, row := range rows {
			key := row.Source + ":" + row.SessionID
			if seen[key] {
				continue
			}
			seen[key] = true
			candidates = append(candidates, candidate{key: key, row: row, lastTS: row.LastTS})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].lastTS > candidates[j].lastTS })

	wideLimit := limit
	if wideLimit < 200 {
		wideLimit = 200 // keep room to filter by status before truncating
	}
	out := make([]Summary, 0, limit)
	for _, c := range candidates {
		if len(out) >= wideLimit {
			break
		}
		summary, ok := d.buildSummary(ctx, c)
		if !ok {
			continue
		}
		if source != "" && summary.Source != source {
			continue
		}
		if status != "" && summary.Status != status {
			continue
		}
		out = append(out, summary)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return deref(out[i].LastTS) > deref(out[j].LastTS)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// buildSummary turns one candidate into the payload the API publishes.
func (d *Deps) buildSummary(ctx context.Context, c candidate) (Summary, bool) {
	var (
		sessionID   string
		source      string
		cwd         string
		lastTS      string
		lastPreview string
		eventCount  int
		unread      int
		lastKind    string
		pending     bool
		nativeState string
	)
	switch {
	case c.relay != nil:
		sessionID, source = c.relay.sessionID, c.relay.source
		cwd, lastTS, lastPreview = c.relay.cwd, c.relay.lastTS, c.relay.lastPreview
		eventCount, unread = c.relay.eventCount, c.relay.unread
		lastKind, pending = c.relay.lastKind, c.relay.pendingInject
	case c.row.SessionID != "":
		sessionID, source = c.row.SessionID, c.row.Source
		cwd, lastTS, lastPreview = c.row.Cwd, c.row.LastTS, c.row.LastPreview
		nativeState = c.row.NativeState
	default:
		return Summary{}, false
	}

	meta := d.metaFor(ctx, source, sessionID, cwd)
	summary := Summary{
		SessionID:      sessionID,
		Source:         source,
		Title:          meta.Title,
		TitleSource:    meta.TitleSource,
		Workspace:      optional(meta.Workspace),
		Project:        optional(meta.Project),
		Branch:         optional(meta.Branch),
		Cwd:            optional(firstNonEmpty(cwd, meta.Cwd)),
		Model:          optional(meta.Model),
		Status:         scanlib.Status(lastKind, pending, lastTS, firstNonEmpty(nativeState, meta.NativeState), runningWindow),
		Unread:         unread,
		LastTS:         optional(lastTS),
		LastPreview:    optional(lastPreview),
		EventCount:     eventCount,
		Effort:         optional(meta.Effort),
		PermissionMode: optional(meta.PermissionMode),
		ContextUsage:   meta.ContextUsage,
	}
	summary.Running = summary.Status == "running"
	if summary.Running {
		summary.RunningSince = summary.LastTS
	}
	state := d.storeState(source, sessionID)
	summary.Read, summary.Pinned, summary.Muted, summary.Archived =
		state.Read, state.Pinned, state.Muted, state.Archived
	summary.IsTemp = scanlib.TempCwd(deref(summary.Cwd))
	summary.IsEmpty = d.isEmptySession(ctx, source, sessionID, summary)
	return summary, true
}

func (d *Deps) storeState(source, sessionID string) store.State {
	if s, ok := d.store(); ok {
		return s.SessionState(source, sessionID)
	}
	return store.State{}
}

// isEmptyChecker is the optional agent capability that answers "any real
// message in this session?".
type isEmptyChecker interface {
	IsEmptySession(sessionID string) bool
}

// isEmptySession prefers the agent's own answer; without one it falls back to
// "this session has an event or a preview or it is empty".
//
// A session the relay has events for is never empty. Node asks the scanner
// first, which lets a scanner that cannot find the transcript hide a session the
// phone is actively using; the event count is the cheaper and safer answer.
func (d *Deps) isEmptySession(ctx context.Context, source, sessionID string, summary Summary) bool {
	if summary.EventCount > 0 {
		return false
	}
	if a, ok := agent.Get(ctx, source); ok {
		if checker, ok := a.(isEmptyChecker); ok {
			return checker.IsEmptySession(sessionID)
		}
	}
	return summary.LastPreview == nil
}

// findSummary looks up one session without scanning everything: any agent that
// knows the id wins, then the event log is checked.
func (d *Deps) findSummary(ctx context.Context, sessionID string) (Summary, bool) {
	s, ok := d.store()
	if !ok {
		return Summary{}, false
	}
	events := s.Events()
	matched := make([]store.Event, 0, 4)
	for _, event := range events {
		if event.SessionID == sessionID {
			matched = append(matched, event)
		}
	}
	if len(matched) > 0 {
		index := relayIndex(events, s.Replies())
		source := matched[len(matched)-1].Source
		entry := index[source+":"+sessionID]
		return d.buildSummary(ctx, candidate{key: source + ":" + sessionID, relay: entry, lastTS: entry.lastTS})
	}
	for _, a := range agent.All(ctx) {
		// Node lists every session an agent can find and truncates at the end;
		// the cap only protects against a runaway scan.
		rows, err := a.ListSessions(ctx, 1000)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if row.SessionID == sessionID {
				return d.buildSummary(ctx, candidate{key: row.Source + ":" + sessionID, row: row, lastTS: row.LastTS})
			}
		}
	}
	return Summary{}, false
}

// sourceFor finds which agent a session id belongs to (Node: sessionSourceFor).
// Node falls back to scanning every agent's full session index, so the limit here is
// effectively "all" — an old session must not turn into "unknown source".
func (d *Deps) sourceFor(ctx context.Context, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	if s, ok := d.store(); ok {
		events := s.Events()
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].SessionID == sessionID {
				return events[i].Source
			}
		}
	}
	for _, a := range agent.All(ctx) {
		rows, err := a.ListSessions(ctx, 100000)
		if err != nil {
			continue
		}
		for _, row := range rows {
			if row.SessionID == sessionID {
				return row.Source
			}
		}
	}
	return ""
}

// ---- handlers ------------------------------------------------------------

func init() {
	Register("sessions", 20, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/sessions", d.guard(d.handleSessions))
		mux.HandleFunc("GET /api/sessions/{id}", d.guard(d.handleSession))
		mux.HandleFunc("PATCH /api/sessions/{id}", d.guard(d.handlePatchSession))
		mux.HandleFunc("GET /api/sessions/{id}/items", d.guard(d.handleSessionItems))
	})
}

func (d *Deps) handleSessions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := clampInt(query.Get("limit"), 100, 1, 500)
	source := query.Get("source")
	if source == "all" {
		source = ""
	}
	status := query.Get("status")
	if status == "any" {
		status = ""
	}
	sessions := d.summaries(ctxOf(r), limit, source, status)
	if query.Get("include_empty") != "1" {
		kept := sessions[:0]
		for _, session := range sessions {
			if !session.IsEmpty {
				kept = append(kept, session)
			}
		}
		sessions = kept
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"sessions":     sessions,
		"count":        len(sessions),
		"next_cursor":  nil,
		"generated_at": nowISO(),
	})
}

func (d *Deps) handleSession(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathParam(w, r)
	if !ok {
		return
	}
	summary, found := d.findSummary(ctxOf(r), sessionID)
	if !found {
		WriteJSON(w, http.StatusNotFound, map[string]any{"error": "会话不存在"})
		return
	}
	WriteJSON(w, http.StatusOK, summary)
}

func (d *Deps) handlePatchSession(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathParam(w, r)
	if !ok {
		return
	}
	body := readBody(r)
	patch := map[string]bool{}
	for _, field := range []string{"read", "pinned", "muted", "archived"} {
		if value, present := body[field]; present {
			patch[field] = value == true
		}
	}
	if len(patch) == 0 {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "body 需含 read / pinned / muted / archived 之一"})
		return
	}
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	source := d.sourceFor(ctxOf(r), sessionID)
	if source == "" {
		source = "unknown"
	}
	state, err := s.PatchSession(source, sessionID, patch)
	if err != nil {
		d.Log.Error("patch session", "session", sessionID, "err", err)
	}
	summary, found := d.findSummary(ctxOf(r), sessionID)
	payload := map[string]any{
		"session_id": sessionID,
		"source":     source,
		"state":      state,
	}
	if found {
		payload["session"] = summary
	} else {
		payload["session"] = nil
	}
	WriteJSON(w, http.StatusOK, payload)
}

// handleSessionItems publishes a session transcript. Agents that can read their
// own entries answer directly; the rest say why there are none.
func (d *Deps) handleSessionItems(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := pathParam(w, r)
	if !ok {
		return
	}
	limit := clampInt(r.URL.Query().Get("limit"), 200, 1, 1000)
	source := d.sourceFor(ctxOf(r), sessionID)
	if source == "" {
		WriteJSON(w, http.StatusOK, map[string]any{
			"session_id": sessionID, "source": nil, "count": 0,
			"items": []any{}, "turns": []any{}, "reason": "未知会话来源",
		})
		return
	}
	a, found := agent.Get(ctxOf(r), source)
	if !found {
		WriteJSON(w, http.StatusOK, map[string]any{
			"session_id": sessionID, "source": source, "count": 0,
			"items": []any{}, "turns": []any{}, "reason": "未知会话来源",
		})
		return
	}
	reader, ok := a.(agent.ItemReader)
	if !ok {
		WriteJSON(w, http.StatusOK, map[string]any{
			"session_id": sessionID, "source": source, "count": 0,
			"items": []any{}, "turns": []any{}, "reason": "该来源的对话条目还没在 Go 版里解析",
		})
		return
	}
	items, err := reader.ReadItems(ctxOf(r), sessionID, limit)
	if err != nil {
		WriteJSON(w, http.StatusOK, map[string]any{
			"session_id": sessionID, "source": source, "count": 0,
			"items": []any{}, "turns": []any{}, "reason": "解析失败",
		})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"source":     source,
		"count":      len(items),
		"items":      items,
		"turns":      []any{},
		"reason":     nil,
	})
}

// ---- small helpers -------------------------------------------------------

// pathParam reads and decodes the {id} path segment.
func pathParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := r.PathValue("id")
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "session_id 编码非法"})
		return "", false
	}
	return decoded, true
}

func clampInt(raw string, fallback, low, high int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// optional turns "" into a JSON null, like the Node payload.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }
