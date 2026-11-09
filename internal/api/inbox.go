package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/store"
)

// InboxSession is the session blurb an inbox item carries.
type InboxSession struct {
	SessionID   string  `json:"session_id"`
	Source      string  `json:"source"`
	Title       string  `json:"title"`
	TitleSource string  `json:"title_source"`
	Project     *string `json:"project"`
	Workspace   *string `json:"workspace"`
	Branch      *string `json:"branch"`
	Model       *string `json:"model"`
}

// InboxItem is one row of waiting / running / recent_done.
type InboxItem struct {
	Session InboxSession `json:"session"`
	EventID int64        `json:"event_id"`
	Kind    string       `json:"kind"`
	Title   string       `json:"title"`
	Summary string       `json:"summary"`
	At      string       `json:"at"`
	Pinned  bool         `json:"pinned"`
	Read    bool         `json:"read"`
}

// Inbox is the GET /api/inbox payload.
type Inbox struct {
	Waiting     []InboxItem    `json:"waiting"`
	Running     []InboxItem    `json:"running"`
	RecentDone  []InboxItem    `json:"recent_done"`
	Counts      map[string]int `json:"counts"`
	GeneratedAt string         `json:"generated_at"`
}

var (
	waitingKinds = map[string]bool{"permission": true, "needs_input": true, "error": true}
	doneKinds    = map[string]bool{"reply": true, "done": true, "stop": true, "turn_complete": true, "completed": true}
)

func init() {
	Register("inbox", 30, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/inbox", d.guard(d.handleInbox))
		mux.HandleFunc("GET /api/projects", d.guard(d.handleProjects))
	})
}

// buildInbox groups the latest event per session into the three buckets.
func (d *Deps) buildInbox(r *http.Request) Inbox {
	now := time.Now()
	inbox := Inbox{
		Waiting: []InboxItem{}, Running: []InboxItem{}, RecentDone: []InboxItem{},
		Counts: map[string]int{}, GeneratedAt: nowISO(),
	}
	s, ok := d.store()
	if !ok {
		return inbox
	}

	latest := map[string]store.Event{}
	for _, event := range s.Events() {
		if event.SessionID == "" {
			continue
		}
		latest[event.Source+":"+event.SessionID] = event
	}

	for _, event := range latest {
		state := s.SessionState(event.Source, event.SessionID)
		if state.Archived {
			continue
		}
		meta := d.metaFor(ctxOf(r), event.Source, event.SessionID, event.Cwd)
		item := InboxItem{
			Session: InboxSession{
				SessionID:   event.SessionID,
				Source:      event.Source,
				Title:       meta.Title,
				TitleSource: meta.TitleSource,
				Project:     optional(meta.Project),
				Workspace:   optional(meta.Workspace),
				Branch:      optional(meta.Branch),
				Model:       optional(meta.Model),
			},
			EventID: event.ID,
			Kind:    event.Kind,
			Title:   firstNonEmpty(meta.Title, event.Title),
			Summary: scanlib.Truncate(collapseSpaces(event.Text), 160),
			At:      event.Ts,
			Pinned:  state.Pinned,
			Read:    state.Read,
		}
		if waitingKinds[event.Kind] {
			if state.Muted || state.Read {
				continue
			}
			switch event.Kind {
			case "permission":
				item.Kind = "approval"
			case "needs_input":
				item.Kind = "needs_reply"
			}
			inbox.Waiting = append(inbox.Waiting, item)
			continue
		}
		at, err := time.Parse(time.RFC3339, event.Ts)
		if err != nil {
			continue
		}
		age := now.Sub(at)
		if age <= 5*time.Minute {
			inbox.Running = append(inbox.Running, item)
			continue
		}
		if doneKinds[event.Kind] && age <= 24*time.Hour {
			inbox.RecentDone = append(inbox.RecentDone, item)
		}
	}

	byNewest := func(a, b InboxItem) bool { return a.At > b.At }
	pinFirst := func(a, b InboxItem) bool {
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		return byNewest(a, b)
	}
	sort.SliceStable(inbox.Waiting, func(i, j int) bool { return pinFirst(inbox.Waiting[i], inbox.Waiting[j]) })
	sort.SliceStable(inbox.Running, func(i, j int) bool { return pinFirst(inbox.Running[i], inbox.Running[j]) })
	sort.SliceStable(inbox.RecentDone, func(i, j int) bool { return byNewest(inbox.RecentDone[i], inbox.RecentDone[j]) })
	inbox.Counts["waiting"] = len(inbox.Waiting)
	inbox.Counts["running"] = len(inbox.Running)
	return inbox
}

func (d *Deps) handleInbox(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, d.buildInbox(r))
}

// Project is one row of GET /api/projects.
type Project struct {
	Cwd     string   `json:"cwd"`
	Project string   `json:"project"`
	Count   int      `json:"count"`
	LastTS  string   `json:"last_ts"`
	Sources []string `json:"sources"`
}

// handleProjects groups the event log by cwd.
func (d *Deps) handleProjects(w http.ResponseWriter, r *http.Request) {
	s, ok := d.store()
	if !ok {
		WriteJSON(w, http.StatusOK, map[string]any{"count": 0, "projects": []Project{}})
		return
	}
	type bucket struct {
		project string
		count   int
		lastTS  string
		sources []string // first-seen order, like the Node Set iteration
		seen    map[string]bool
	}
	byCwd := map[string]*bucket{}
	for _, event := range s.Events() {
		cwd := strings.TrimSpace(event.Cwd)
		if cwd == "" {
			continue
		}
		entry := byCwd[cwd]
		if entry == nil {
			entry = &bucket{
				project: firstNonEmpty(event.Project, scanlib.ProjectFromCwd(cwd)),
				seen:    map[string]bool{},
			}
			byCwd[cwd] = entry
		}
		entry.count++
		if event.Source != "" && !entry.seen[event.Source] {
			entry.seen[event.Source] = true
			entry.sources = append(entry.sources, event.Source)
		}
		if event.Ts > entry.lastTS {
			entry.lastTS = event.Ts
		}
	}
	projects := make([]Project, 0, len(byCwd))
	for cwd, entry := range byCwd {
		projects = append(projects, Project{
			Cwd: cwd, Project: entry.project, Count: entry.count, LastTS: entry.lastTS, Sources: entry.sources,
		})
	}
	sort.SliceStable(projects, func(i, j int) bool { return projects[i].LastTS > projects[j].LastTS })
	WriteJSON(w, http.StatusOK, map[string]any{"count": len(projects), "projects": projects})
}

// collapseSpaces squeezes the whitespace runs Node trims out of an inbox summary.
func collapseSpaces(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
