package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/store"
)

// Home is GET /api/home. It is additive: /api/inbox keeps its old shape.
type Home struct {
	Away        HomeAway        `json:"away"`
	Attention   []HomeAttention `json:"attention"`
	Running     []Summary       `json:"running"`
	Resume      []Summary       `json:"resume"`
	Earlier     HomeEarlier     `json:"earlier"`
	Counts      HomeCounts      `json:"counts"`
	Settings    HomeSettings    `json:"settings"`
	GeneratedAt string          `json:"generated_at"`
}

type HomeAway struct {
	Enabled bool              `json:"enabled"`
	Current *store.AwayPeriod `json:"current,omitempty"`
	Period  *store.AwayPeriod `json:"period,omitempty"`
}

type HomeAttention struct {
	Session     Summary       `json:"session"`
	Reason      string        `json:"reason"`
	SinceAway   bool          `json:"since_away"`
	EventID     int64         `json:"event_id"`
	UnseenCount int           `json:"unseen_count"`
	Headline    string        `json:"headline"`
	Summary     string        `json:"summary"`
	At          string        `json:"at"`
	Actions     []string      `json:"actions"`
	Approval    *HomeApproval `json:"approval,omitempty"`
	Collab      *HomeCollab   `json:"collab,omitempty"`
}

type HomeApproval struct {
	Command string           `json:"command"`
	Options []map[string]any `json:"options"`
}

type HomeCollab struct {
	Children int `json:"children"`
	Running  int `json:"running"`
	NeedsYou int `json:"needs_you"`
}

type HomeEarlier struct {
	Count      int    `json:"count"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type HomeCounts struct {
	NeedsYou int `json:"needs_you"`
	AwayNew  int `json:"away_new"`
	Running  int `json:"running"`
	Earlier  int `json:"earlier"`
}

type HomeSettings struct {
	NewActivity  string `json:"new_activity"`
	CollabReturn string `json:"collab_return"`
}

var attentionRank = map[string]int{
	"approval":      0,
	"needs_reply":   1,
	"error":         2,
	"collab_result": 3,
	"done":          4,
	"progress":      5,
}

func init() {
	Register("home", 31, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/home", d.guard(d.handleHome))
		mux.HandleFunc("GET /api/home/settings", d.guard(d.handleHomeSettings))
		mux.HandleFunc("PATCH /api/home/settings", d.guard(d.handleHomeSettings))
	})
}

func (d *Deps) handleHome(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, d.buildHome(r))
}

func (d *Deps) handleHomeSettings(w http.ResponseWriter, r *http.Request) {
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "数据目录不可用"})
		return
	}
	if r.Method == http.MethodPatch {
		body := readBody(r)
		changed := false
		if raw, exists := body["new_activity"]; exists {
			scope, isString := raw.(string)
			if !isString || (scope != "away" && scope != "unread") {
				WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "new_activity 只能是 away 或 unread"})
				return
			}
			if err := s.SetHomeNewActivity(scope); err != nil {
				WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存设置失败"})
				return
			}
			changed = true
		}
		if raw, exists := body["collab_return"]; exists {
			mode, isString := raw.(string)
			if !isString || (mode != "card" && mode != "inject") {
				WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "collab_return 只能是 card 或 inject"})
				return
			}
			if err := s.SetCollabReturn(mode); err != nil {
				WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存设置失败"})
				return
			}
			changed = true
		}
		if !changed {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "没有可更新的设置"})
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"settings": HomeSettings{
			NewActivity:  s.HomeNewActivity(),
			CollabReturn: s.CollabReturn(),
		},
	})
}

func (d *Deps) buildHome(r *http.Request) Home {
	now := time.Now().UTC()
	out := Home{
		Attention:   []HomeAttention{},
		Running:     []Summary{},
		Resume:      []Summary{},
		Earlier:     HomeEarlier{},
		GeneratedAt: now.Format("2006-01-02T15:04:05.000Z"),
		Settings:    HomeSettings{NewActivity: "away", CollabReturn: "card"},
	}
	s, ok := d.store()
	if !ok {
		return out
	}
	out.Settings.NewActivity = s.HomeNewActivity()
	out.Settings.CollabReturn = s.CollabReturn()
	out.Away = buildHomeAway(s, now)
	scopeUnread := out.Settings.NewActivity == "unread"

	events := s.Events()
	bySession := map[string][]store.Event{}
	maxID := map[string]int64{}
	for _, event := range events {
		if event.SessionID == "" {
			continue
		}
		key := event.Source + ":" + event.SessionID
		bySession[key] = append(bySession[key], event)
		if event.ID > maxID[key] {
			maxID[key] = event.ID
		}
	}

	summaries := d.summaries(ctxOf(r), 500, "", "")
	attentionKeys := map[string]bool{}
	all := make([]Summary, 0, len(summaries))
	byKey := map[string]Summary{}
	for _, summary := range summaries {
		if summary.Archived || summary.IsEmpty {
			continue
		}
		all = append(all, summary)
		byKey[summary.Source+":"+summary.SessionID] = summary
		if summary.Collab != nil && summary.Collab.Role == "child" {
			continue
		}
		key := summary.Source + ":" + summary.SessionID
		state := s.SessionState(summary.Source, summary.SessionID)
		seen := state.SeenEventID
		if state.SeenAt == "" && state.Read {
			seen = maxID[key]
		}
		sessionEvents := bySession[key]
		unseen := unseenEvents(sessionEvents, seen)
		sinceAway := false
		if out.Away.Period != nil && out.Away.Period.ClearedAt == "" {
			start := out.Away.Period.StartedAt
			for _, event := range unseen {
				if event.Ts >= start {
					sinceAway = true
					break
				}
			}
		}
		reason, event, approval := attentionFor(s, summary, sessionEvents, unseen, sinceAway, scopeUnread)
		if reason == "" {
			continue
		}
		if summary.Muted {
			continue
		}
		attentionKeys[key] = true
		item := HomeAttention{
			Session:     summary,
			Reason:      reason,
			SinceAway:   sinceAway,
			EventID:     event.ID,
			UnseenCount: len(unseen),
			Headline:    firstNonEmpty(event.Title, summary.Title),
			Summary:     scanlib.Truncate(collapseSpaces(event.Text), 160),
			At:          firstNonEmpty(event.Ts, deref(summary.LastTS)),
			Actions:     actionsFor(reason),
			Approval:    approval,
			Collab:      homeCollab(summary.Collab),
		}
		out.Attention = append(out.Attention, item)
	}
	for _, dispatch := range s.Dispatches() {
		if dispatch.Status != "needs_you" {
			continue
		}
		key := dispatch.ParentSource + ":" + dispatch.ParentSessionID
		if attentionKeys[key] {
			continue
		}
		parent, ok := byKey[key]
		if !ok {
			continue
		}
		attentionKeys[key] = true
		eventID := parent.SeenEventID
		if tail := maxID[key]; tail > eventID {
			eventID = tail
		}
		out.Attention = append(out.Attention, HomeAttention{
			Session:     parent,
			Reason:      "needs_reply",
			EventID:     eventID,
			UnseenCount: 0,
			Headline:    "@" + dispatch.TargetAgent + " 的子任务在等你",
			Summary:     scanlib.Truncate(collapseSpaces(dispatch.Text), 160),
			At:          dispatch.UpdatedAt,
			Actions:     []string{"open", "mark_seen"},
			Collab:      homeCollab(parent.Collab),
		})
	}
	sort.SliceStable(out.Attention, func(i, j int) bool {
		a, b := out.Attention[i], out.Attention[j]
		ra, rb := attentionRank[a.Reason], attentionRank[b.Reason]
		if ra != rb {
			return ra < rb
		}
		if a.Session.Pinned != b.Session.Pinned {
			return a.Session.Pinned
		}
		return a.At > b.At
	})

	resumeCandidates := make([]Summary, 0, len(all))
	for _, summary := range all {
		key := summary.Source + ":" + summary.SessionID
		if summary.Running {
			out.Running = append(out.Running, summary)
		}
		if attentionKeys[key] || summary.IsTemp {
			continue
		}
		resumeCandidates = append(resumeCandidates, summary)
	}
	sort.SliceStable(resumeCandidates, func(i, j int) bool {
		if resumeCandidates[i].Pinned != resumeCandidates[j].Pinned {
			return resumeCandidates[i].Pinned
		}
		return resumeActivityTS(resumeCandidates[i]) > resumeActivityTS(resumeCandidates[j])
	})
	resumeKeys := map[string]bool{}
	for _, summary := range resumeCandidates {
		if len(out.Resume) >= 5 {
			break
		}
		out.Resume = append(out.Resume, summary)
		resumeKeys[summary.Source+":"+summary.SessionID] = true
	}
	for _, summary := range all {
		key := summary.Source + ":" + summary.SessionID
		if attentionKeys[key] || resumeKeys[key] {
			continue
		}
		out.Earlier.Count++
	}
	if out.Earlier.Count > 0 {
		out.Earlier.NextCursor = "20"
	}
	out.Counts.Running = len(out.Running)
	out.Counts.Earlier = out.Earlier.Count
	for _, item := range out.Attention {
		switch item.Reason {
		case "approval", "needs_reply", "error":
			out.Counts.NeedsYou++
		}
		if item.SinceAway {
			out.Counts.AwayNew++
		}
	}
	return out
}

func resumeActivityTS(summary Summary) string {
	if summary.LastUserTS != nil && *summary.LastUserTS != "" {
		return *summary.LastUserTS
	}
	return deref(summary.LastTS)
}

func buildHomeAway(s *store.Store, now time.Time) HomeAway {
	out := HomeAway{Enabled: s.Away()}
	if current, ok := s.CurrentAwayPeriod(); ok {
		out.Current = &current
		out.Period = &current
		return out
	}
	last, ok := s.LastAwayPeriod()
	if !ok {
		return out
	}
	if last.ClearedAt != "" {
		return out
	}
	out.Period = &last
	if ended, err := time.Parse(time.RFC3339, last.EndedAt); err == nil {
		if now.Sub(ended) > 12*time.Hour {
			out.Period = nil
		}
	}
	return out
}

func unseenEvents(events []store.Event, seen int64) []store.Event {
	out := make([]store.Event, 0, len(events))
	for _, event := range events {
		if event.ID > seen {
			out = append(out, event)
		}
	}
	return out
}

func attentionFor(
	s *store.Store,
	summary Summary,
	events []store.Event,
	unseen []store.Event,
	sinceAway bool,
	scopeUnread bool,
) (string, store.Event, *HomeApproval) {
	for i := len(unseen) - 1; i >= 0; i-- {
		event := unseen[i]
		if event.Kind != "permission" && event.Kind != "needs_input" && event.Kind != "error" {
			continue
		}
		if replyResolved(s, event) {
			continue
		}
		if laterResolution(events, event) {
			continue
		}
		reason := "error"
		switch event.Kind {
		case "permission":
			reason = "approval"
		case "needs_input":
			reason = "needs_reply"
		}
		return reason, event, approvalFor(event)
	}

	var collabResult store.Event
	for i := len(unseen) - 1; i >= 0; i-- {
		if unseen[i].Kind == "collab_result" {
			collabResult = unseen[i]
			break
		}
	}
	if collabResult.ID > 0 {
		return "collab_result", collabResult, nil
	}

	if len(unseen) == 0 {
		return "", store.Event{}, nil
	}
	if !sinceAway && !scopeUnread {
		return "", store.Event{}, nil
	}
	event := unseen[len(unseen)-1]
	if doneKinds[event.Kind] {
		return "done", event, nil
	}
	_ = summary
	return "progress", event, nil
}

func replyResolved(s *store.Store, event store.Event) bool {
	for _, reply := range s.Replies() {
		if reply.EventID == nil || *reply.EventID != event.ID {
			continue
		}
		if reply.Status == "injected" || reply.Status == "held" || reply.Status == "queued" {
			return true
		}
	}
	return false
}

func laterResolution(events []store.Event, event store.Event) bool {
	for _, later := range events {
		if later.ID <= event.ID {
			continue
		}
		if doneKinds[later.Kind] || later.Kind == "message" || later.Kind == "collab_result" {
			return true
		}
	}
	return false
}

func approvalFor(event store.Event) *HomeApproval {
	command := scanlib.Truncate(collapseSpaces(event.Text), 400)
	if command == "" {
		command = "Agent 请求授权"
	}
	options := []map[string]any{
		{"id": "allow_once", "label": "允许", "decision": "allow"},
		{"id": "reject_once", "label": "拒绝", "decision": "deny"},
	}
	if acp, ok := event.SessionMeta["acp"].(map[string]any); ok {
		if rawOptions, ok := acp["options"].([]any); ok {
			translated := make([]map[string]any, 0, len(rawOptions))
			for _, raw := range rawOptions {
				option, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				id := firstStringValue(option["id"], option["option_id"])
				if id == "" {
					continue
				}
				kind := strings.ToLower(firstStringValue(option["kind"]))
				decision := "allow"
				if strings.Contains(kind, "reject") || strings.Contains(kind, "deny") {
					decision = "deny"
				}
				label := firstStringValue(option["label"], option["name"], option["kind"], id)
				translated = append(translated, map[string]any{
					"id":       id,
					"label":    label,
					"decision": decision,
					"kind":     kind,
				})
			}
			if len(translated) > 0 {
				options = translated
			}
		}
	}
	return &HomeApproval{
		Command: command,
		Options: options,
	}
}

func firstStringValue(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		}
	}
	return ""
}

func actionsFor(reason string) []string {
	switch reason {
	case "approval":
		return []string{"approve", "deny", "reply", "open", "mark_seen"}
	case "needs_reply", "error":
		return []string{"reply", "open", "mark_seen"}
	case "collab_result":
		return []string{"open", "forward", "mark_seen"}
	default:
		return []string{"open", "mark_seen"}
	}
}

func homeCollab(info *CollabInfo) *HomeCollab {
	if info == nil || info.Role != "parent" || len(info.Children) == 0 {
		return nil
	}
	out := HomeCollab{Children: len(info.Children)}
	for _, child := range info.Children {
		switch child.Status {
		case "starting", "waiting_session", "started", "running":
			out.Running++
		case "needs_you":
			out.NeedsYou++
		}
	}
	return &out
}
