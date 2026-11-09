package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const (
	maxQueue     = 500
	queueTTL     = 7 * 24 * time.Hour
	sqliteDriver = "sqlite"
)

// DB is homing-server's storage. It only ever holds device ids, public keys,
// ciphertext envelopes and push tokens — never plaintext payloads.
type DB struct {
	sql *sql.DB
}

var memoryDBCounter atomic.Uint64

// OpenDB opens (and creates if needed) the SQLite database at path. Use
// ":memory:" for tests; it is pinned to a single connection so every query sees
// the same in-memory database.
func OpenDB(path string) (*DB, error) {
	dsn := path
	inMemory := path == ":memory:" || path == ""
	if inMemory {
		// A unique name keeps two servers opened with ":memory:" in the same
		// process from sharing one database.
		dsn = fmt.Sprintf("file:homing-%d-%d?mode=memory&cache=shared",
			os.Getpid(), memoryDBCounter.Add(1))
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("server: create database directory: %w", err)
		}
	}
	sqlDB, err := sql.Open(sqliteDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("server: open database: %w", err)
	}
	if inMemory {
		sqlDB.SetMaxOpenConns(1)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode = DELETE",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("server: %s: %w", pragma, err)
		}
	}
	db := &DB{sql: sqlDB}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) migrate() error {
	_, err := d.sql.Exec(`
		CREATE TABLE IF NOT EXISTS devices (
			device_id TEXT PRIMARY KEY,
			pub TEXT NOT NULL,
			first_seen INTEGER NOT NULL,
			last_seen INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS pairings (
			device_a TEXT NOT NULL,
			device_b TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (device_a, device_b)
		);
		CREATE TABLE IF NOT EXISTS queue (
			id TEXT PRIMARY KEY,
			from_id TEXT NOT NULL,
			to_id TEXT NOT NULL,
			envelope TEXT NOT NULL,
			ts INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS queue_to_ts ON queue (to_id, ts);
		CREATE TABLE IF NOT EXISTS push_tokens (
			device_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			token TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (device_id, provider)
		);
	`)
	if err != nil {
		return fmt.Errorf("server: migrate database: %w", err)
	}
	return nil
}

func (d *DB) Close() error {
	return d.sql.Close()
}

type Device struct {
	DeviceID string `json:"device_id"`
	Pub      string `json:"pub"`
}

func (d *DB) GetDevice(ctx context.Context, deviceID string) (Device, bool, error) {
	var device Device
	err := d.sql.QueryRowContext(ctx,
		`SELECT device_id, pub FROM devices WHERE device_id = ?`, deviceID,
	).Scan(&device.DeviceID, &device.Pub)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, false, nil
	}
	if err != nil {
		return Device{}, false, fmt.Errorf("server: read device: %w", err)
	}
	return device, true, nil
}

// RegisterDevice records a device on first hello, or refreshes last_seen when
// the public key matches. It returns false when the device id is already bound
// to a different key.
func (d *DB) RegisterDevice(ctx context.Context, deviceID, pub string, now time.Time) (bool, error) {
	existing, found, err := d.GetDevice(ctx, deviceID)
	if err != nil {
		return false, err
	}
	ms := now.UnixMilli()
	if !found {
		_, err := d.sql.ExecContext(ctx,
			`INSERT INTO devices (device_id, pub, first_seen, last_seen) VALUES (?, ?, ?, ?)`,
			deviceID, pub, ms, ms)
		if err != nil {
			return false, fmt.Errorf("server: register device: %w", err)
		}
		return true, nil
	}
	if existing.Pub != pub {
		return false, nil
	}
	if _, err := d.sql.ExecContext(ctx,
		`UPDATE devices SET last_seen = ? WHERE device_id = ?`, ms, deviceID); err != nil {
		return false, fmt.Errorf("server: touch device: %w", err)
	}
	return true, nil
}

func pairKey(a, b string) (string, string) {
	if a > b {
		a, b = b, a
	}
	return a, b
}

func (d *DB) IsPaired(ctx context.Context, a, b string) (bool, error) {
	left, right := pairKey(a, b)
	var one int
	err := d.sql.QueryRowContext(ctx,
		`SELECT 1 FROM pairings WHERE device_a = ? AND device_b = ?`, left, right).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("server: read pairing: %w", err)
	}
	return true, nil
}

func (d *DB) CreatePairing(ctx context.Context, a, b string, now time.Time) error {
	left, right := pairKey(a, b)
	_, err := d.sql.ExecContext(ctx,
		`INSERT OR REPLACE INTO pairings (device_a, device_b, created_at) VALUES (?, ?, ?)`,
		left, right, now.UnixMilli())
	if err != nil {
		return fmt.Errorf("server: create pairing: %w", err)
	}
	return nil
}

// RevokePairing removes the pairing and every queued envelope between the two
// devices, in both directions.
func (d *DB) RevokePairing(ctx context.Context, a, b string) error {
	left, right := pairKey(a, b)
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("server: revoke pairing: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM pairings WHERE device_a = ? AND device_b = ?`, left, right); err != nil {
		return fmt.Errorf("server: revoke pairing: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM queue WHERE (from_id = ? AND to_id = ?) OR (from_id = ? AND to_id = ?)`,
		a, b, b, a); err != nil {
		return fmt.Errorf("server: revoke queued messages: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("server: revoke pairing: %w", err)
	}
	return nil
}

// Enqueue stores an encrypted envelope for an offline device, pruning entries
// older than seven days and keeping at most maxQueue envelopes per device.
func (d *DB) Enqueue(ctx context.Context, envelope json.RawMessage, id, from, to string, now time.Time) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("server: enqueue: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	nowMS := now.UnixMilli()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM queue WHERE to_id = ? AND ts < ?`, to, now.Add(-queueTTL).UnixMilli()); err != nil {
		return fmt.Errorf("server: prune queue: %w", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM queue WHERE to_id = ?`, to).Scan(&count); err != nil {
		return fmt.Errorf("server: count queue: %w", err)
	}
	if count >= maxQueue {
		rows, err := tx.QueryContext(ctx,
			`SELECT id FROM queue WHERE to_id = ? ORDER BY ts ASC LIMIT ?`,
			to, count-maxQueue+1)
		if err != nil {
			return fmt.Errorf("server: prune queue: %w", err)
		}
		var ids []string
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				_ = rows.Close()
				return fmt.Errorf("server: prune queue: %w", err)
			}
			ids = append(ids, value)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("server: prune queue: %w", err)
		}
		_ = rows.Close()
		for _, value := range ids {
			if _, err := tx.ExecContext(ctx, `DELETE FROM queue WHERE id = ?`, value); err != nil {
				return fmt.Errorf("server: prune queue: %w", err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO queue (id, from_id, to_id, envelope, ts) VALUES (?, ?, ?, ?, ?)`,
		id, from, to, string(envelope), nowMS); err != nil {
		return fmt.Errorf("server: enqueue: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("server: enqueue: %w", err)
	}
	return nil
}

// QueuedFor returns the envelopes waiting for a device, oldest first.
func (d *DB) QueuedFor(ctx context.Context, deviceID string) ([]json.RawMessage, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT envelope FROM queue WHERE to_id = ? ORDER BY ts ASC LIMIT ?`, deviceID, maxQueue)
	if err != nil {
		return nil, fmt.Errorf("server: read queue: %w", err)
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("server: read queue: %w", err)
		}
		out = append(out, json.RawMessage(raw))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("server: read queue: %w", err)
	}
	return out, nil
}

func (d *DB) Ack(ctx context.Context, deviceID, id string) (bool, error) {
	result, err := d.sql.ExecContext(ctx,
		`DELETE FROM queue WHERE id = ? AND to_id = ?`, id, deviceID)
	if err != nil {
		return false, fmt.Errorf("server: ack: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("server: ack: %w", err)
	}
	return affected > 0, nil
}

func (d *DB) QueueCount(ctx context.Context, deviceID string) (int, error) {
	var count int
	if err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM queue WHERE to_id = ?`, deviceID).Scan(&count); err != nil {
		return 0, fmt.Errorf("server: count queue: %w", err)
	}
	return count, nil
}

func (d *DB) SavePushToken(ctx context.Context, deviceID, provider, token string, now time.Time) error {
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO push_tokens (device_id, provider, token, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(device_id, provider) DO UPDATE SET token = excluded.token, updated_at = excluded.updated_at`,
		deviceID, provider, token, now.UnixMilli())
	if err != nil {
		return fmt.Errorf("server: save push token: %w", err)
	}
	return nil
}

// DeletePushToken removes a token the push provider reported as unregistered.
func (d *DB) DeletePushToken(ctx context.Context, deviceID, provider string) error {
	_, err := d.sql.ExecContext(ctx,
		`DELETE FROM push_tokens WHERE device_id = ? AND provider = ?`, deviceID, provider)
	if err != nil {
		return fmt.Errorf("server: delete push token: %w", err)
	}
	return nil
}

type PushToken struct {
	Provider string
	Token    string
}

func (d *DB) PushTokens(ctx context.Context, deviceID string) ([]PushToken, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT provider, token FROM push_tokens WHERE device_id = ?`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("server: read push tokens: %w", err)
	}
	defer rows.Close()
	var out []PushToken
	for rows.Next() {
		var item PushToken
		if err := rows.Scan(&item.Provider, &item.Token); err != nil {
			return nil, fmt.Errorf("server: read push tokens: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("server: read push tokens: %w", err)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out, nil
}
