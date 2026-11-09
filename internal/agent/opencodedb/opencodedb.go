// Package opencodedb reads the SQLite database that OpenCode and ZCode share
// (same table layout): session_v2 for sessions, session_message for entries, kv
// for the models.dev catalog cache.
//
// Ported from relay/scanners/lib/opencode-db.mjs. Everything here is read-only:
// the database is opened with mode=ro and a busy timeout, never written, never
// vacuumed, never locked for writing.
//
// Owner: WorkBuddy.
package opencodedb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/scanlib"
)

// SessionRow is one row of session_v2, with the tags the API publishes.
type SessionRow struct {
	SessionID      string `json:"session_id"`
	Title          string `json:"title"`
	Cwd            string `json:"cwd,omitempty"`
	Model          string `json:"model,omitempty"`
	ParentID       string `json:"parent_id,omitempty"`
	ArchivedNative bool   `json:"archived_native"`
	LastTS         string `json:"last_ts,omitempty"`
}

func open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(2000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Probe reports whether the file looks like an OpenCode-family database.
func Probe(path string) (ok bool, sessions int) {
	db, err := open(path)
	if err != nil {
		return false, 0
	}
	defer db.Close()
	var name string
	if err := db.QueryRow(`select name from sqlite_master where type='table' and name='session_v2'`).Scan(&name); err != nil {
		return false, 0
	}
	_ = db.QueryRow(`select count(*) from session_v2`).Scan(&sessions)
	return true, sessions
}

func iso(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return scanlib.ISO(time.UnixMilli(ms))
}

// ListSessions returns the newest sessions, most recently updated first.
func ListSessions(path string, limit int) []SessionRow {
	db, err := open(path)
	if err != nil {
		return nil
	}
	defer db.Close()
	if limit <= 0 {
		limit = 200
	}
	rows, err := db.Query(
		`select id, title, directory, model, parent_id, time_updated, time_archived
		   from session_v2
		  order by time_updated desc
		  limit ?`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []SessionRow
	for rows.Next() {
		var (
			id, title, dir, model, parent sql.NullString
			updated, archived             sql.NullInt64
		)
		if rows.Scan(&id, &title, &dir, &model, &parent, &updated, &archived) != nil {
			continue
		}
		out = append(out, SessionRow{
			SessionID:      id.String,
			Title:          title.String,
			Cwd:            dir.String,
			Model:          model.String,
			ParentID:       parent.String,
			ArchivedNative: archived.Int64 > 0,
			LastTS:         iso(updated.Int64),
		})
	}
	return out
}

// OneSession looks up a single session row.
func OneSession(path, sessionID string) (SessionRow, bool) {
	db, err := open(path)
	if err != nil {
		return SessionRow{}, false
	}
	defer db.Close()
	var (
		id, title, dir, model, parent sql.NullString
		updated, archived             sql.NullInt64
		in, outTok                    sql.NullInt64
		cost                          sql.NullFloat64
	)
	err = db.QueryRow(
		`select id, title, directory, model, parent_id, time_updated, time_archived,
		        tokens_input, tokens_output, cost
		   from session_v2 where id = ? limit 1`, sessionID).
		Scan(&id, &title, &dir, &model, &parent, &updated, &archived, &in, &outTok, &cost)
	if err != nil {
		return SessionRow{}, false
	}
	return SessionRow{
		SessionID:      id.String,
		Title:          title.String,
		Cwd:            dir.String,
		Model:          model.String,
		ParentID:       parent.String,
		ArchivedNative: archived.Int64 > 0,
		LastTS:         iso(updated.Int64),
	}, true
}

// ReadItems returns the last limit entries of a session, oldest first.
func ReadItems(path, sessionID string, limit int) []agent.Item {
	db, err := open(path)
	if err != nil {
		return nil
	}
	defer db.Close()
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(
		`select id, type, seq, time_created, data
		   from session_message
		  where session_id = ?
		  order by seq desc
		  limit ?`, sessionID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var items []agent.Item
	for rows.Next() {
		var (
			rowID, kind, data sql.NullString
			seq, created      sql.NullInt64
		)
		if rows.Scan(&rowID, &kind, &seq, &created, &data) != nil {
			continue
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(data.String), &payload)
		// Node keeps user/assistant and passes any other row type through as-is.
		role := kind.String
		if role == "user" || role == "assistant" {
			role = strings.ToLower(role)
		}
		items = append(items, agent.Item{
			ID:    rowID.String,
			Role:  role,
			Type: kind.String,
			Seq:   int(seq.Int64),
			TS: iso(created.Int64),
			Text:  scanlib.Truncate(itemText(role, payload), 4000),
			Error: errorText(payload),
		})
	}
	// SQL returned newest first; the API publishes oldest first.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items
}

func itemText(role string, payload map[string]any) string {
	switch role {
	case "user":
		text, _ := payload["text"].(string)
		return text
	case "assistant":
		switch content := payload["content"].(type) {
		case string:
			return content
		case []any:
			var b strings.Builder
			for _, block := range content {
				switch v := block.(type) {
				case string:
					b.WriteString(v)
				case map[string]any:
					if text, ok := v["text"].(string); ok {
						b.WriteString(text)
					}
				}
			}
			return b.String()
		}
	}
	return ""
}

func errorText(payload map[string]any) string {
	errObj, ok := payload["error"].(map[string]any)
	if !ok {
		return ""
	}
	message, _ := errObj["message"].(string)
	return message
}

// HasConversation reports whether the session has any user or assistant entry.
func HasConversation(path, sessionID string) bool {
	db, err := open(path)
	if err != nil {
		return false
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(
		`select count(*) from session_message where session_id = ? and type in ('user','assistant')`,
		sessionID).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// Catalog reads the models.dev catalog cache out of the kv table.
func Catalog(path string) map[string]any {
	db, err := open(path)
	if err != nil {
		return nil
	}
	defer db.Close()
	var value string
	if err := db.QueryRow(`select value from kv where key='models-dev:catalog'`).Scan(&value); err != nil {
		return nil
	}
	var outer struct {
		Body string `json:"body"`
	}
	if json.Unmarshal([]byte(value), &outer) != nil || outer.Body == "" {
		return nil
	}
	var catalog map[string]any
	if json.Unmarshal([]byte(outer.Body), &catalog) != nil {
		return nil
	}
	return catalog
}
