package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/tomato-hug/homing/internal/platform"
)

// historyDB is the durable queryable store for Homing's own append-only data.
// The JSONL files remain the compatibility/export format: every write first
// lands there, then in SQLite. On startup the latest JSONL state is replayed
// into SQLite, which also repairs a crash between the two writes.
//
// The database deliberately keeps event read flags separately from the JSON
// payload. MarkRead only changes local display state, so a later JSONL replay
// must not reset an event that was already seen.
type historyDB struct {
	db *sql.DB
}

func openHistory(path string) (*historyDB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("history database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	history := &historyDB{db: db}
	if err := history.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	// SQLite creates -wal and -shm beside the database with the process umask.
	// The data directory is 0700, but keep the sidecars private as well.
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Stat(sidecar); err == nil {
			if err := os.Chmod(sidecar, 0o600); err != nil {
				_ = db.Close()
				return nil, err
			}
		}
	}
	return history, nil
}

func (h *historyDB) init() error {
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=3000`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY,
			payload TEXT NOT NULL,
			read INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS replies (
			id INTEGER PRIMARY KEY,
			payload TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS dispatches (
			id TEXT PRIMARY KEY,
			payload TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS away_periods (
			id TEXT PRIMARY KEY,
			payload TEXT NOT NULL,
			started_at TEXT NOT NULL
		)`,
	} {
		if _, err := h.db.Exec(statement); err != nil {
			return fmt.Errorf("prepare history database: %w", err)
		}
	}
	return nil
}

// syncJSONL replays the current compatibility files into SQLite. The JSONL
// files are written before the database in every mutation, so their last line
// for an id is the newest recoverable copy.
func (h *historyDB) syncJSONL(paths platform.Paths) error {
	events := readJSONL[Event](paths.Events)
	replies := latestReplies(readJSONL[Reply](paths.Replies))
	dispatches := latestDispatches(readJSONL[Dispatch](paths.Dispatches))
	away := latestAwayPeriods(readJSONL[AwayPeriod](paths.AwayPeriods))

	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, event := range events {
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		read := 0
		if event.Read {
			read = 1
		}
		// Preserve a read flag already stored in SQLite. JSONL does not rewrite
		// old event lines when the reader marks the inbox as seen.
		if _, err := tx.Exec(
			`INSERT INTO events(id, payload, read) VALUES (?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`,
			event.ID, string(payload), read,
		); err != nil {
			return err
		}
	}
	for _, reply := range replies {
		payload, err := json.Marshal(reply)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO replies(id, payload) VALUES (?, ?)
			 ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`,
			reply.ID, string(payload),
		); err != nil {
			return err
		}
	}
	for _, dispatch := range dispatches {
		if strings.TrimSpace(dispatch.ID) == "" {
			continue
		}
		payload, err := json.Marshal(dispatch)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO dispatches(id, payload) VALUES (?, ?)
			 ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`,
			dispatch.ID, string(payload),
		); err != nil {
			return err
		}
	}
	for _, period := range away {
		if strings.TrimSpace(period.ID) == "" {
			continue
		}
		payload, err := json.Marshal(period)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO away_periods(id, payload, started_at) VALUES (?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
				payload = excluded.payload,
				started_at = excluded.started_at`,
			period.ID, string(payload), period.StartedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (h *historyDB) loadEvents(limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 5000
	}
	rows, err := h.db.Query(
		`SELECT payload, read FROM events ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var (
			payload string
			read    int
		)
		if err := rows.Scan(&payload, &read); err != nil {
			return nil, err
		}
		var event Event
		if json.Unmarshal([]byte(payload), &event) != nil {
			continue
		}
		event.Read = read != 0
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (h *historyDB) loadReplies() ([]Reply, error) {
	rows, err := h.db.Query(`SELECT payload FROM replies ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reply
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var reply Reply
		if json.Unmarshal([]byte(payload), &reply) != nil {
			continue
		}
		out = append(out, normalizeReply(reply))
	}
	return out, rows.Err()
}

func (h *historyDB) loadDispatches() ([]Dispatch, error) {
	rows, err := h.db.Query(`SELECT payload FROM dispatches ORDER BY rowid ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dispatch
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var dispatch Dispatch
		if json.Unmarshal([]byte(payload), &dispatch) != nil {
			continue
		}
		out = append(out, normalizeDispatch(dispatch))
	}
	return out, rows.Err()
}

func (h *historyDB) loadAwayPeriods() ([]AwayPeriod, error) {
	rows, err := h.db.Query(
		`SELECT payload FROM away_periods ORDER BY started_at ASC, rowid ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AwayPeriod
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var period AwayPeriod
		if json.Unmarshal([]byte(payload), &period) == nil && period.ID != "" {
			out = append(out, period)
		}
	}
	return out, rows.Err()
}

func (h *historyDB) putEvent(event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	read := 0
	if event.Read {
		read = 1
	}
	_, err = h.db.Exec(
		`INSERT INTO events(id, payload, read) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			payload = excluded.payload,
			read = excluded.read`,
		event.ID, string(payload), read,
	)
	return err
}

func (h *historyDB) markRead(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE events SET read = 1 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (h *historyDB) putReply(reply Reply) error {
	payload, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	_, err = h.db.Exec(
		`INSERT INTO replies(id, payload) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`,
		reply.ID, string(payload),
	)
	return err
}

func (h *historyDB) putDispatch(dispatch Dispatch) error {
	if strings.TrimSpace(dispatch.ID) == "" {
		return errors.New("dispatch id is empty")
	}
	payload, err := json.Marshal(dispatch)
	if err != nil {
		return err
	}
	_, err = h.db.Exec(
		`INSERT INTO dispatches(id, payload) VALUES (?, ?)
		 ON CONFLICT(id) DO UPDATE SET payload = excluded.payload`,
		dispatch.ID, string(payload),
	)
	return err
}

func (h *historyDB) putAwayPeriod(period AwayPeriod) error {
	if strings.TrimSpace(period.ID) == "" {
		return errors.New("away period id is empty")
	}
	payload, err := json.Marshal(period)
	if err != nil {
		return err
	}
	_, err = h.db.Exec(
		`INSERT INTO away_periods(id, payload, started_at) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			payload = excluded.payload,
			started_at = excluded.started_at`,
		period.ID, string(payload), period.StartedAt,
	)
	return err
}

func (h *historyDB) Close() error {
	if h == nil || h.db == nil {
		return nil
	}
	db := h.db
	h.db = nil
	return db.Close()
}

func latestReplies(items []Reply) []Reply {
	out := make([]Reply, 0, len(items))
	index := map[int64]int{}
	for _, reply := range items {
		reply = normalizeReply(reply)
		if at, exists := index[reply.ID]; exists {
			out[at] = mergeReplyRevision(out[at], reply)
			continue
		}
		index[reply.ID] = len(out)
		out = append(out, reply)
	}
	return out
}

func mergeReplyRevision(previous, next Reply) Reply {
	if next.Status == "queued" && replyStatusIsDelivered(previous.Status) {
		next.Status = previous.Status
		next.Message = previous.Message
		next.Injected = previous.Injected
	}
	return next
}

func replyStatusIsDelivered(status string) bool {
	return status == "injected" || status == "held"
}

func latestDispatches(items []Dispatch) []Dispatch {
	out := make([]Dispatch, 0, len(items))
	index := map[string]int{}
	for _, dispatch := range items {
		dispatch = normalizeDispatch(dispatch)
		if strings.TrimSpace(dispatch.ID) == "" {
			continue
		}
		if at, exists := index[dispatch.ID]; exists {
			out[at] = dispatch
			continue
		}
		index[dispatch.ID] = len(out)
		out = append(out, dispatch)
	}
	return out
}

func latestAwayPeriods(items []AwayPeriod) []AwayPeriod {
	out := make([]AwayPeriod, 0, len(items))
	index := map[string]int{}
	for _, period := range items {
		if strings.TrimSpace(period.ID) == "" {
			continue
		}
		if at, exists := index[period.ID]; exists {
			out[at] = period
			continue
		}
		index[period.ID] = len(out)
		out = append(out, period)
	}
	return out
}

func normalizeReply(reply Reply) Reply {
	if reply.Status == "" {
		reply.Status = "queued"
	}
	if reply.Files == nil {
		reply.Files = []string{}
	}
	return reply
}

func normalizeDispatch(dispatch Dispatch) Dispatch {
	if dispatch.Status == "" {
		dispatch.Status = "starting"
	}
	if dispatch.Mode == "" {
		dispatch.Mode = "parallel"
	}
	return dispatch
}
