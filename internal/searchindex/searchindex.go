// Package searchindex keeps the opt-in full-text index for session transcripts.
//
// It stores no credentials and never leaves the data directory. Only user and
// assistant text is indexed; tool output can contain secrets and is deliberately
// excluded. Sessions are replaced transactionally when their source timestamp
// changes, so a reader never sees half an update.
package searchindex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// MaxSessionBytes bounds one session's searchable transcript. Recent items
	// are preferred when a long transcript exceeds the budget.
	MaxSessionBytes = 200 << 10
)

// Document is one searchable transcript item.
type Document struct {
	ItemID string
	Seq    int
	Role   string
	Body   string
}

// Hit is one FTS match. Body is returned so the HTTP layer can build the same
// before/match/after snippet it uses for title hits.
type Hit struct {
	Source    string
	SessionID string
	ItemID    string
	Seq       int
	Role      string
	Body      string
}

// Stats is the payload for the console's index status card.
type Stats struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes"`
	Sessions  int    `json:"sessions"`
	Items     int    `json:"items"`
	Trigram   bool   `json:"trigram"`
	UpdatedAt string `json:"updated_at,omitempty"`
	OpenError string `json:"open_error,omitempty"`
}

// Index is safe for concurrent use. SQLite is still single-writer, so writes
// and reads share one serialized handle; this index is a personal cache, not a
// high-throughput server database.
type Index struct {
	path string
	db   *sql.DB
	mu   sync.Mutex
}

// Open creates or opens index.db. The file is forced to 0600 even on platforms
// where sqlite's default umask would make it group-readable.
func Open(path string) (*Index, error) {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return nil, errors.New("search index path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", clean)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	idx := &Index{path: clean, db: db}
	if err := idx.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(clean, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	return idx, nil
}

// Path is the database file this index owns.
func (i *Index) Path() string {
	if i == nil {
		return ""
	}
	return i.path
}

func (i *Index) init() error {
	if i == nil || i.db == nil {
		return errors.New("search index is closed")
	}
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=3000`,
		`CREATE TABLE IF NOT EXISTS indexed_sessions (
			source TEXT NOT NULL,
			session_id TEXT NOT NULL,
			version TEXT NOT NULL,
			item_count INTEGER NOT NULL DEFAULT 0,
			byte_count INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (source, session_id)
		)`,
		`CREATE TABLE IF NOT EXISTS indexed_items (
			id INTEGER PRIMARY KEY,
			item_key TEXT NOT NULL UNIQUE,
			source TEXT NOT NULL,
			session_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			body TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS indexed_items_session
			ON indexed_items(source, session_id)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS item_fts
			USING fts5(body, content='', contentless_delete=1, tokenize='trigram')`,
	}
	for _, statement := range statements {
		if _, err := i.db.Exec(statement); err != nil {
			return fmt.Errorf("prepare search index: %w", err)
		}
	}
	if err := i.ensureDeletableFTS(); err != nil {
		return err
	}
	return nil
}

// Early development builds created a classic contentless FTS table. Those
// tables cannot handle the DELETE used when a transcript changes. SQLite's
// contentless_delete option keeps the body out of the FTS table while allowing
// rows to be removed by rowid. Existing databases are migrated once from the
// authoritative indexed_items table.
func (i *Index) ensureDeletableFTS() error {
	var schema string
	if err := i.db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'item_fts'`,
	).Scan(&schema); err != nil {
		return fmt.Errorf("inspect search index: %w", err)
	}
	if strings.Contains(strings.ToLower(schema), "contentless_delete") {
		return nil
	}

	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DROP TABLE item_fts`); err != nil {
		return fmt.Errorf("migrate search index: %w", err)
	}
	if _, err := tx.Exec(
		`CREATE VIRTUAL TABLE item_fts
		 USING fts5(body, content='', contentless_delete=1, tokenize='trigram')`,
	); err != nil {
		return fmt.Errorf("migrate search index: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO item_fts(rowid, body)
		 SELECT id, body FROM indexed_items`,
	); err != nil {
		return fmt.Errorf("rebuild search index: %w", err)
	}
	return tx.Commit()
}

// Close releases the sqlite handle.
func (i *Index) Close() error {
	if i == nil || i.db == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	db := i.db
	i.db = nil
	return db.Close()
}

// NeedsUpdate reports whether the session is missing or its source timestamp
// differs from what is already indexed.
func (i *Index) NeedsUpdate(ctx context.Context, source, sessionID, version string) (bool, error) {
	if i == nil || i.db == nil {
		return false, errors.New("search index is closed")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	var stored string
	err := i.db.QueryRowContext(ctx,
		`SELECT version FROM indexed_sessions WHERE source = ? AND session_id = ?`,
		source, sessionID,
	).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return stored != version, nil
}

// Versions returns the indexed version for every session. The synchronizer
// uses one query per request instead of one query per session summary.
func (i *Index) Versions(ctx context.Context) (map[string]string, error) {
	if i == nil || i.db == nil {
		return nil, errors.New("search index is closed")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	rows, err := i.db.QueryContext(ctx, `SELECT source, session_id, version FROM indexed_sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var source, sessionID, version string
		if err := rows.Scan(&source, &sessionID, &version); err != nil {
			return nil, err
		}
		out[source+"\x00"+sessionID] = version
	}
	return out, rows.Err()
}

// ReplaceSession atomically swaps one session's rows. An empty document list
// still records the version, which prevents re-reading a transcript with no
// searchable messages on every request.
func (i *Index) ReplaceSession(
	ctx context.Context,
	source, sessionID, version string,
	documents []Document,
) error {
	if i == nil || i.db == nil {
		return errors.New("search index is closed")
	}
	i.mu.Lock()
	defer i.mu.Unlock()

	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM indexed_items WHERE source = ? AND session_id = ?`,
		source, sessionID,
	)
	if err != nil {
		return err
	}
	var oldIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		oldIDs = append(oldIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range oldIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_fts WHERE rowid = ?`, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM indexed_items WHERE source = ? AND session_id = ?`,
		source, sessionID,
	); err != nil {
		return err
	}

	var byteCount int
	for index, document := range documents {
		body := strings.TrimSpace(document.Body)
		if body == "" {
			continue
		}
		itemKey := source + "\x00" + sessionID + "\x00" +
			firstNonEmpty(document.ItemID, fmt.Sprintf("seq-%d", document.Seq), fmt.Sprintf("n-%d", index))
		result, err := tx.ExecContext(ctx,
			`INSERT INTO indexed_items
				(item_key, source, session_id, item_id, seq, role, body)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			itemKey, source, sessionID, document.ItemID, document.Seq, document.Role, body,
		)
		if err != nil {
			return err
		}
		rowID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_fts(rowid, body) VALUES (?, ?)`, rowID, body,
		); err != nil {
			return err
		}
		byteCount += len(body)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO indexed_sessions
			(source, session_id, version, item_count, byte_count, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(source, session_id) DO UPDATE SET
			version = excluded.version,
			item_count = excluded.item_count,
			byte_count = excluded.byte_count,
			updated_at = excluded.updated_at`,
		source, sessionID, version, len(documents), byteCount, now,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// Search finds transcript bodies. Queries shorter than three runes cannot use
// the trigram tokenizer, so they fall back to an escaped SQL LIKE scan. The
// boolean result reports that fallback for diagnostics.
func (i *Index) Search(ctx context.Context, query, source string, limit int) ([]Hit, bool, error) {
	if i == nil || i.db == nil {
		return nil, false, errors.New("search index is closed")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []Hit{}, false, nil
	}
	if limit <= 0 {
		limit = 30
	}
	i.mu.Lock()
	defer i.mu.Unlock()

	fallback := len([]rune(query)) < 3
	var (
		rows *sql.Rows
		err  error
	)
	if fallback {
		pattern := "%" + escapeLike(query) + "%"
		if source == "" {
			rows, err = i.db.QueryContext(ctx,
				`SELECT source, session_id, item_id, seq, role, body
				   FROM indexed_items
				  WHERE body LIKE ? ESCAPE '\'
				  ORDER BY id DESC LIMIT ?`,
				pattern, limit,
			)
		} else {
			rows, err = i.db.QueryContext(ctx,
				`SELECT source, session_id, item_id, seq, role, body
				   FROM indexed_items
				  WHERE source = ? AND body LIKE ? ESCAPE '\'
				  ORDER BY id DESC LIMIT ?`,
				source, pattern, limit,
			)
		}
	} else {
		match := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
		if source == "" {
			rows, err = i.db.QueryContext(ctx,
				`SELECT i.source, i.session_id, i.item_id, i.seq, i.role, i.body
				   FROM item_fts f JOIN indexed_items i ON i.id = f.rowid
				  WHERE item_fts MATCH ?
				  ORDER BY bm25(item_fts), i.id DESC LIMIT ?`,
				match, limit,
			)
		} else {
			rows, err = i.db.QueryContext(ctx,
				`SELECT i.source, i.session_id, i.item_id, i.seq, i.role, i.body
				   FROM item_fts f JOIN indexed_items i ON i.id = f.rowid
				  WHERE item_fts MATCH ? AND i.source = ?
				  ORDER BY bm25(item_fts), i.id DESC LIMIT ?`,
				match, source, limit,
			)
		}
	}
	if err != nil && !fallback {
		// FTS syntax and old builds can both fail; a bounded LIKE scan is a
		// safer answer than a 500 for a search box.
		return i.searchLIKE(ctx, query, source, limit)
	}
	if err != nil {
		return nil, fallback, err
	}
	defer rows.Close()

	hits := make([]Hit, 0, limit)
	for rows.Next() {
		var hit Hit
		if err := rows.Scan(
			&hit.Source, &hit.SessionID, &hit.ItemID, &hit.Seq, &hit.Role, &hit.Body,
		); err != nil {
			return nil, fallback, err
		}
		hits = append(hits, hit)
	}
	return hits, fallback, rows.Err()
}

func (i *Index) searchLIKE(ctx context.Context, query, source string, limit int) ([]Hit, bool, error) {
	pattern := "%" + escapeLike(query) + "%"
	var (
		rows *sql.Rows
		err  error
	)
	if source == "" {
		rows, err = i.db.QueryContext(ctx,
			`SELECT source, session_id, item_id, seq, role, body
			   FROM indexed_items
			  WHERE body LIKE ? ESCAPE '\'
			  ORDER BY id DESC LIMIT ?`,
			pattern, limit,
		)
	} else {
		rows, err = i.db.QueryContext(ctx,
			`SELECT source, session_id, item_id, seq, role, body
			   FROM indexed_items
			  WHERE source = ? AND body LIKE ? ESCAPE '\'
			  ORDER BY id DESC LIMIT ?`,
			source, pattern, limit,
		)
	}
	if err != nil {
		return nil, true, err
	}
	defer rows.Close()
	hits := make([]Hit, 0, limit)
	for rows.Next() {
		var hit Hit
		if err := rows.Scan(
			&hit.Source, &hit.SessionID, &hit.ItemID, &hit.Seq, &hit.Role, &hit.Body,
		); err != nil {
			return nil, true, err
		}
		hits = append(hits, hit)
	}
	return hits, true, rows.Err()
}

// Stats reports the index's size and row counts for settings and diagnostics.
func (i *Index) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{Trigram: true}
	if i == nil {
		return stats, errors.New("search index is closed")
	}
	stats.Path = i.path
	if info, err := os.Stat(i.path); err == nil {
		stats.Exists = true
		stats.SizeBytes = info.Size()
	}
	if i.db == nil {
		return stats, errors.New("search index is closed")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM indexed_sessions`).Scan(&stats.Sessions); err != nil {
		return stats, err
	}
	if err := i.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM indexed_items`).Scan(&stats.Items); err != nil {
		return stats, err
	}
	_ = i.db.QueryRowContext(ctx,
		`SELECT updated_at FROM indexed_sessions ORDER BY updated_at DESC LIMIT 1`,
	).Scan(&stats.UpdatedAt)
	return stats, nil
}

// Clear removes every row but keeps the database file and its schema.
func (i *Index) Clear(ctx context.Context) error {
	if i == nil || i.db == nil {
		return errors.New("search index is closed")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`DELETE FROM item_fts`,
		`DELETE FROM indexed_items`,
		`DELETE FROM indexed_sessions`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Remove closes the index and deletes its database, WAL and shared-memory sidecars.
func Remove(path string) error {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		err := os.Remove(path + suffix)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
