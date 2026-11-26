package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AuthModeOpen    = "open"
	AuthModeManaged = "managed"

	RegModeOpen   = "open"
	RegModeInvite = "invite"
	RegModeClosed = "closed"

	TierFree    = "free"
	TierSponsor = "sponsor"

	AccountStatusActive = "active"
	AccountStatusBanned = "banned"

	// Default tier quotas (when per-account override column is 0).
	defaultFreeMaxHosts        = 2
	defaultFreeMaxDevices      = 4
	defaultFreeMaxMsgBytes     = 512 * 1024       // 512 KB per message on free official relay
	defaultFreeDailyQuotaBytes = 64 * 1024 * 1024 // 64 MB / day on free official relay

	defaultSponsorMaxHosts        = 8
	defaultSponsorMaxDevices      = 20
	defaultSponsorMaxMsgBytes     = maxMessageSize         // 25 MB
	defaultSponsorDailyQuotaBytes = 2 * 1024 * 1024 * 1024 // 2 GB / day

	// DefaultFreeWeeklyMinutes is the weekly remote-usage allowance for free
	// accounts: minutes in which a paired phone actively sends encrypted
	// traffic through the official relay (20 hours / week). Desktop idle
	// connections and pushes to the phone never consume it.
	DefaultFreeWeeklyMinutes = 20 * 60

	emailCodeTTL      = 10 * time.Minute
	emailCodeCooldown = 60 * time.Second
	maxEmailCodeTries = 5
	accountSessionTTL = 30 * 24 * time.Hour
)

// weeklyQuotaZone anchors the weekly reset to Monday 00:00 China Standard
// Time, which is when most users of the official relay start their week.
var weeklyQuotaZone = time.FixedZone("UTC+8", 8*3600)

var (
	ErrAccountBanned       = errors.New("account is suspended")
	ErrHostQuotaExceeded   = errors.New("host quota exceeded for account")
	ErrDeviceQuotaExceeded = errors.New("paired device quota exceeded for account")
	ErrInvalidRelayKey     = errors.New("invalid or revoked relay key")
	ErrInvalidRedeemCode   = errors.New("invalid, expired, or exhausted redeem code")
)

// NormalizeRegistrationMode accepts the canonical values plus the
// "invite_only" alias used by older desktop builds.
func NormalizeRegistrationMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case RegModeOpen:
		return RegModeOpen, true
	case RegModeInvite, "invite_only", "invite-only":
		return RegModeInvite, true
	case RegModeClosed:
		return RegModeClosed, true
	}
	return "", false
}

type TierLimits struct {
	MaxHosts        int   `json:"max_hosts"`
	MaxDevices      int   `json:"max_devices"`
	MaxMsgBytes     int64 `json:"max_msg_bytes"`
	DailyQuotaBytes int64 `json:"daily_quota_bytes"`
	// WeeklyActiveMinutes is 0 when the account has no weekly time limit.
	WeeklyActiveMinutes int `json:"weekly_active_minutes"`
}

type Account struct {
	ID              string `json:"id"`
	Email           string `json:"email,omitempty"`
	GitHubID        string `json:"github_id,omitempty"`
	GitHubLogin     string `json:"github_login,omitempty"`
	DisplayName     string `json:"display_name"`
	Tier            string `json:"tier"`
	Status          string `json:"status"`
	SponsorBadge    bool   `json:"sponsor_badge"`
	MaxHosts        int    `json:"max_hosts"`
	MaxDevices      int    `json:"max_devices"`
	MaxMsgBytes     int64  `json:"max_msg_bytes"`
	DailyQuotaBytes int64  `json:"daily_quota_bytes"`
	// WeeklyMinutes overrides the tier's weekly allowance when > 0.
	WeeklyMinutes int    `json:"weekly_minutes"`
	CreatedAt     int64  `json:"created_at"`
	LastSeenAt    int64  `json:"last_seen_at"`
	Note          string `json:"note,omitempty"`
}

// WeeklyUsage reports the remote-usage minutes consumed in the current week.
type WeeklyUsage struct {
	Week          string `json:"week"`
	ActiveMinutes int    `json:"active_minutes"`
	LimitMinutes  int    `json:"limit_minutes"`
	ResetsAt      int64  `json:"resets_at"`
}

func weekStart(now time.Time) time.Time {
	local := now.In(weeklyQuotaZone)
	offset := (int(local.Weekday()) + 6) % 7 // Monday = 0
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, weeklyQuotaZone)
	return day.AddDate(0, 0, -offset)
}

func weekKey(now time.Time) string {
	return weekStart(now).Format("2006-01-02")
}

func weekResetAt(now time.Time) time.Time {
	return weekStart(now).AddDate(0, 0, 7)
}

func (a Account) EffectiveLimits() TierLimits {
	base := TierLimits{
		MaxHosts:        defaultFreeMaxHosts,
		MaxDevices:      defaultFreeMaxDevices,
		MaxMsgBytes:     defaultFreeMaxMsgBytes,
		DailyQuotaBytes: defaultFreeDailyQuotaBytes,
	}
	if a.Tier == TierSponsor {
		base = TierLimits{
			MaxHosts:        defaultSponsorMaxHosts,
			MaxDevices:      defaultSponsorMaxDevices,
			MaxMsgBytes:     defaultSponsorMaxMsgBytes,
			DailyQuotaBytes: defaultSponsorDailyQuotaBytes,
		}
	}
	if a.MaxHosts > 0 {
		base.MaxHosts = a.MaxHosts
	}
	if a.MaxDevices > 0 {
		base.MaxDevices = a.MaxDevices
	}
	if a.MaxMsgBytes > 0 {
		base.MaxMsgBytes = a.MaxMsgBytes
	}
	if a.DailyQuotaBytes > 0 {
		base.DailyQuotaBytes = a.DailyQuotaBytes
	}
	if a.WeeklyMinutes > 0 {
		base.WeeklyActiveMinutes = a.WeeklyMinutes
	}
	return base
}

type RelayKeyRecord struct {
	ID         string `json:"id"`
	AccountID  string `json:"account_id"`
	KeyPrefix  string `json:"key_prefix"`
	Label      string `json:"label"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
	RevokedAt  int64  `json:"revoked_at,omitempty"`
}

type AccountDevice struct {
	DeviceID   string `json:"device_id"`
	AccountID  string `json:"account_id"`
	Role       string `json:"role"` // "host" | "client"
	Label      string `json:"label,omitempty"`
	BoundAt    int64  `json:"bound_at"`
	LastSeenAt int64  `json:"last_seen_at"`
	Online     bool   `json:"online,omitempty"`
}

type DailyTraffic struct {
	Day          string `json:"day"`
	MsgCount     int64  `json:"msg_count"`
	BytesRelayed int64  `json:"bytes_relayed"`
}

type RedeemCode struct {
	Code         string `json:"code"`
	Tier         string `json:"tier"`
	SponsorBadge bool   `json:"sponsor_badge"`
	BonusDailyMB int64  `json:"bonus_daily_mb"`
	MaxUses      int    `json:"max_uses"`
	UsedCount    int    `json:"used_count"`
	Note         string `json:"note,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

func (d *DB) migrateManaged() error {
	_, err := d.sql.Exec(`
		CREATE TABLE IF NOT EXISTS server_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS accounts (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL DEFAULT '',
			github_id TEXT NOT NULL DEFAULT '',
			github_login TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL,
			tier TEXT NOT NULL DEFAULT 'free',
			status TEXT NOT NULL DEFAULT 'active',
			sponsor_badge INTEGER NOT NULL DEFAULT 0,
			max_hosts INTEGER NOT NULL DEFAULT 0,
			max_devices INTEGER NOT NULL DEFAULT 0,
			max_msg_bytes INTEGER NOT NULL DEFAULT 0,
			daily_quota_bytes INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL,
			note TEXT NOT NULL DEFAULT ''
		);
		CREATE UNIQUE INDEX IF NOT EXISTS accounts_email_uq
			ON accounts (email) WHERE email <> '';
		CREATE UNIQUE INDEX IF NOT EXISTS accounts_github_uq
			ON accounts (github_id) WHERE github_id <> '';

		CREATE TABLE IF NOT EXISTS account_sessions (
			token_hash TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS relay_keys (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			key_prefix TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			label TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER NOT NULL DEFAULT 0,
			revoked_at INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS relay_keys_account ON relay_keys (account_id);

		CREATE TABLE IF NOT EXISTS account_devices (
			device_id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL,
			role TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			bound_at INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS account_devices_account ON account_devices (account_id);

		CREATE TABLE IF NOT EXISTS account_traffic_daily (
			account_id TEXT NOT NULL,
			day TEXT NOT NULL,
			msg_count INTEGER NOT NULL DEFAULT 0,
			bytes_relayed INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (account_id, day)
		);

		CREATE TABLE IF NOT EXISTS email_codes (
			email TEXT PRIMARY KEY,
			code_hash TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			sent_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS redeem_codes (
			code TEXT PRIMARY KEY,
			tier TEXT NOT NULL DEFAULT 'sponsor',
			sponsor_badge INTEGER NOT NULL DEFAULT 1,
			bonus_daily_mb INTEGER NOT NULL DEFAULT 0,
			max_uses INTEGER NOT NULL DEFAULT 1,
			used_count INTEGER NOT NULL DEFAULT 0,
			note TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS redeem_redemptions (
			code TEXT NOT NULL,
			account_id TEXT NOT NULL,
			redeemed_at INTEGER NOT NULL,
			PRIMARY KEY (code, account_id)
		);

		CREATE TABLE IF NOT EXISTS account_usage_weekly (
			account_id TEXT NOT NULL,
			week TEXT NOT NULL,
			active_minutes INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (account_id, week)
		);
	`)
	if err != nil {
		return err
	}
	return d.ensureAccountColumn("weekly_minutes", "INTEGER NOT NULL DEFAULT 0")
}

// ensureAccountColumn adds a column to databases created by older releases.
func (d *DB) ensureAccountColumn(name, definition string) error {
	rows, err := d.sql.Query(`PRAGMA table_info(accounts)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var (
			cid        int
			colName    string
			colType    string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &defaultVal, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if strings.EqualFold(colName, name) {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = d.sql.Exec(`ALTER TABLE accounts ADD COLUMN ` + name + ` ` + definition)
	return err
}

func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randomHex(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomDigits6() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	n := (uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])) % 1000000
	return fmt.Sprintf("%06d", n), nil
}

func utcDayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func (d *DB) GetSetting(ctx context.Context, key, fallback string) string {
	var val string
	err := d.sql.QueryRowContext(ctx, `SELECT value FROM server_settings WHERE key = ?`, key).Scan(&val)
	if err != nil || strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

func (d *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO server_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, strings.TrimSpace(value))
	return err
}

func scanAccount(row interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var badge int
	err := row.Scan(
		&a.ID, &a.Email, &a.GitHubID, &a.GitHubLogin, &a.DisplayName,
		&a.Tier, &a.Status, &badge,
		&a.MaxHosts, &a.MaxDevices, &a.MaxMsgBytes, &a.DailyQuotaBytes,
		&a.WeeklyMinutes,
		&a.CreatedAt, &a.LastSeenAt, &a.Note,
	)
	a.SponsorBadge = badge != 0
	return a, err
}

const accountColumns = `id, email, github_id, github_login, display_name, tier, status, sponsor_badge, max_hosts, max_devices, max_msg_bytes, daily_quota_bytes, weekly_minutes, created_at, last_seen_at, note`

func (d *DB) GetAccount(ctx context.Context, id string) (Account, bool, error) {
	a, err := scanAccount(d.sql.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	return a, true, nil
}

func (d *DB) GetAccountByEmail(ctx context.Context, email string) (Account, bool, error) {
	norm := strings.ToLower(strings.TrimSpace(email))
	if norm == "" {
		return Account{}, false, nil
	}
	a, err := scanAccount(d.sql.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE email = ?`, norm))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	return a, true, nil
}

func (d *DB) GetAccountByGitHub(ctx context.Context, githubID string) (Account, bool, error) {
	if strings.TrimSpace(githubID) == "" {
		return Account{}, false, nil
	}
	a, err := scanAccount(d.sql.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE github_id = ?`, githubID))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	return a, true, nil
}

func (d *DB) CreateOrLoginEmailAccount(ctx context.Context, email string, allowCreate bool, now time.Time) (Account, bool, error) {
	norm := strings.ToLower(strings.TrimSpace(email))
	existing, found, err := d.GetAccountByEmail(ctx, norm)
	if err != nil {
		return Account{}, false, err
	}
	ms := now.UnixMilli()
	if found {
		_, _ = d.sql.ExecContext(ctx, `UPDATE accounts SET last_seen_at = ? WHERE id = ?`, ms, existing.ID)
		existing.LastSeenAt = ms
		return existing, false, nil
	}
	if !allowCreate {
		return Account{}, false, errors.New("registration_closed")
	}
	rawID, err := randomHex(10)
	if err != nil {
		return Account{}, false, err
	}
	id := "acc_" + rawID
	display := norm
	if idx := strings.IndexByte(norm, '@'); idx > 0 {
		display = norm[:idx]
	}
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO accounts (id, email, display_name, tier, status, created_at, last_seen_at)
		VALUES (?, ?, ?, 'free', 'active', ?, ?)
	`, id, norm, display, ms, ms)
	if err != nil {
		return Account{}, false, err
	}
	acc, _, err := d.GetAccount(ctx, id)
	return acc, true, err
}

func (d *DB) CreateOrLoginGitHubAccount(ctx context.Context, githubID, githubLogin, email string, allowCreate bool, now time.Time) (Account, bool, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	ms := now.UnixMilli()
	if existing, found, err := d.GetAccountByGitHub(ctx, githubID); err != nil {
		return Account{}, false, err
	} else if found {
		_, _ = d.sql.ExecContext(ctx, `UPDATE accounts SET github_login = ?, last_seen_at = ? WHERE id = ?`, githubLogin, ms, existing.ID)
		existing.GitHubLogin = githubLogin
		existing.LastSeenAt = ms
		return existing, false, nil
	}
	if normEmail != "" {
		if byEmail, found, err := d.GetAccountByEmail(ctx, normEmail); err != nil {
			return Account{}, false, err
		} else if found {
			_, _ = d.sql.ExecContext(ctx, `UPDATE accounts SET github_id = ?, github_login = ?, last_seen_at = ? WHERE id = ?`,
				githubID, githubLogin, ms, byEmail.ID)
			byEmail.GitHubID = githubID
			byEmail.GitHubLogin = githubLogin
			byEmail.LastSeenAt = ms
			return byEmail, false, nil
		}
	}
	if !allowCreate {
		return Account{}, false, errors.New("registration_closed")
	}
	rawID, err := randomHex(10)
	if err != nil {
		return Account{}, false, err
	}
	id := "acc_" + rawID
	display := githubLogin
	if display == "" {
		display = normEmail
	}
	if display == "" {
		display = id
	}
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO accounts (id, email, github_id, github_login, display_name, tier, status, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, 'free', 'active', ?, ?)
	`, id, normEmail, githubID, githubLogin, display, ms, ms)
	if err != nil {
		return Account{}, false, err
	}
	acc, _, err := d.GetAccount(ctx, id)
	return acc, true, err
}

func (d *DB) IssueEmailCode(ctx context.Context, email string, now time.Time) (string, error) {
	norm := strings.ToLower(strings.TrimSpace(email))
	ms := now.UnixMilli()
	var prevSent int64
	err := d.sql.QueryRowContext(ctx, `SELECT sent_at FROM email_codes WHERE email = ?`, norm).Scan(&prevSent)
	if err == nil && now.Sub(time.UnixMilli(prevSent)) < emailCodeCooldown {
		return "", errors.New("cooldown")
	}
	code, err := randomDigits6()
	if err != nil {
		return "", err
	}
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO email_codes (email, code_hash, attempts, sent_at, expires_at)
		VALUES (?, ?, 0, ?, ?)
		ON CONFLICT(email) DO UPDATE SET
			code_hash = excluded.code_hash,
			attempts = 0,
			sent_at = excluded.sent_at,
			expires_at = excluded.expires_at
	`, norm, sha256Hex(norm+":"+code), ms, now.Add(emailCodeTTL).UnixMilli())
	if err != nil {
		return "", err
	}
	return code, nil
}

func (d *DB) VerifyEmailCode(ctx context.Context, email, code string, now time.Time) (bool, error) {
	norm := strings.ToLower(strings.TrimSpace(email))
	cleanCode := strings.TrimSpace(code)
	var storedHash string
	var attempts int
	var expiresAt int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT code_hash, attempts, expires_at FROM email_codes WHERE email = ?`, norm,
	).Scan(&storedHash, &attempts, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if now.UnixMilli() > expiresAt || attempts >= maxEmailCodeTries {
		_, _ = d.sql.ExecContext(ctx, `DELETE FROM email_codes WHERE email = ?`, norm)
		return false, nil
	}
	expected := sha256Hex(norm + ":" + cleanCode)
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(expected)) != 1 {
		_, _ = d.sql.ExecContext(ctx, `UPDATE email_codes SET attempts = attempts + 1 WHERE email = ?`, norm)
		return false, nil
	}
	_, _ = d.sql.ExecContext(ctx, `DELETE FROM email_codes WHERE email = ?`, norm)
	return true, nil
}

func (d *DB) CreateAccountSession(ctx context.Context, accountID string, now time.Time) (string, error) {
	raw, err := randomHex(24)
	if err != nil {
		return "", err
	}
	token := "hses_" + raw
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO account_sessions (token_hash, account_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)
	`, sha256Hex(token), accountID, now.UnixMilli(), now.Add(accountSessionTTL).UnixMilli())
	if err != nil {
		return "", err
	}
	return token, nil
}

func (d *DB) AuthenticateAccountSession(ctx context.Context, token string, now time.Time) (Account, bool, error) {
	if !strings.HasPrefix(token, "hses_") {
		return Account{}, false, nil
	}
	var accountID string
	var expiresAt int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT account_id, expires_at FROM account_sessions WHERE token_hash = ?`, sha256Hex(token),
	).Scan(&accountID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, false, nil
	}
	if err != nil {
		return Account{}, false, err
	}
	if now.UnixMilli() > expiresAt {
		_, _ = d.sql.ExecContext(ctx, `DELETE FROM account_sessions WHERE token_hash = ?`, sha256Hex(token))
		return Account{}, false, nil
	}
	return d.GetAccount(ctx, accountID)
}

func (d *DB) DeleteAccountSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	_, err := d.sql.ExecContext(ctx, `DELETE FROM account_sessions WHERE token_hash = ?`, sha256Hex(token))
	return err
}

func (d *DB) CreateRelayKey(ctx context.Context, accountID, label string, now time.Time) (string, RelayKeyRecord, error) {
	keyIDHex, err := randomHex(8)
	if err != nil {
		return "", RelayKeyRecord{}, err
	}
	secretHex, err := randomHex(20)
	if err != nil {
		return "", RelayKeyRecord{}, err
	}
	rawKey := "hrk_" + secretHex
	prefix := rawKey[:12] + "••••"
	cleanLabel := strings.TrimSpace(label)
	if cleanLabel == "" {
		cleanLabel = "我的电脑"
	}
	rec := RelayKeyRecord{
		ID:        "key_" + keyIDHex,
		AccountID: accountID,
		KeyPrefix: prefix,
		Label:     truncate(cleanLabel, 60),
		CreatedAt: now.UnixMilli(),
	}
	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO relay_keys (id, account_id, key_prefix, key_hash, label, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.AccountID, rec.KeyPrefix, sha256Hex(rawKey), rec.Label, rec.CreatedAt)
	if err != nil {
		return "", RelayKeyRecord{}, err
	}
	return rawKey, rec, nil
}

func (d *DB) ListRelayKeys(ctx context.Context, accountID string) ([]RelayKeyRecord, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT id, account_id, key_prefix, label, created_at, last_used_at, revoked_at
		FROM relay_keys WHERE account_id = ? AND revoked_at = 0
		ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RelayKeyRecord
	for rows.Next() {
		var r RelayKeyRecord
		if err := rows.Scan(&r.ID, &r.AccountID, &r.KeyPrefix, &r.Label, &r.CreatedAt, &r.LastUsedAt, &r.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) RevokeRelayKey(ctx context.Context, accountID, keyID string, now time.Time) error {
	_, err := d.sql.ExecContext(ctx, `
		UPDATE relay_keys SET revoked_at = ? WHERE id = ? AND account_id = ?
	`, now.UnixMilli(), keyID, accountID)
	return err
}

func (d *DB) AuthenticateRelayKey(ctx context.Context, rawKey string, now time.Time) (Account, error) {
	clean := strings.TrimSpace(rawKey)
	if !strings.HasPrefix(clean, "hrk_") {
		return Account{}, ErrInvalidRelayKey
	}
	hash := sha256Hex(clean)
	var keyID, accountID string
	var revokedAt int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT id, account_id, revoked_at FROM relay_keys WHERE key_hash = ?`, hash,
	).Scan(&keyID, &accountID, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) || revokedAt > 0 {
		return Account{}, ErrInvalidRelayKey
	}
	if err != nil {
		return Account{}, err
	}
	acc, found, err := d.GetAccount(ctx, accountID)
	if err != nil {
		return Account{}, err
	}
	if !found {
		return Account{}, ErrInvalidRelayKey
	}
	if acc.Status != AccountStatusActive {
		return acc, ErrAccountBanned
	}
	ms := now.UnixMilli()
	_, _ = d.sql.ExecContext(ctx, `UPDATE relay_keys SET last_used_at = ? WHERE id = ?`, ms, keyID)
	_, _ = d.sql.ExecContext(ctx, `UPDATE accounts SET last_seen_at = ? WHERE id = ?`, ms, acc.ID)
	return acc, nil
}

func (d *DB) BindAccountDevice(ctx context.Context, acc Account, deviceID, role, label string, now time.Time) error {
	limits := acc.EffectiveLimits()
	ms := now.UnixMilli()

	var existingAcc, existingRole string
	err := d.sql.QueryRowContext(ctx,
		`SELECT account_id, role FROM account_devices WHERE device_id = ?`, deviceID,
	).Scan(&existingAcc, &existingRole)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existingAcc == acc.ID {
		nextRole := existingRole
		if role == "host" {
			nextRole = "host"
		}
		_, err = d.sql.ExecContext(ctx,
			`UPDATE account_devices SET role = ?, label = CASE WHEN ? <> '' THEN ? ELSE label END, last_seen_at = ? WHERE device_id = ?`,
			nextRole, label, label, ms, deviceID)
		return err
	}

	if role == "host" {
		var hostCount int
		if err := d.sql.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM account_devices WHERE account_id = ? AND role = 'host' AND device_id <> ?`,
			acc.ID, deviceID,
		).Scan(&hostCount); err != nil {
			return err
		}
		if hostCount >= limits.MaxHosts {
			return ErrHostQuotaExceeded
		}
	} else {
		var clientCount int
		if err := d.sql.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM account_devices WHERE account_id = ? AND role = 'client' AND device_id <> ?`,
			acc.ID, deviceID,
		).Scan(&clientCount); err != nil {
			return err
		}
		if clientCount >= limits.MaxDevices {
			return ErrDeviceQuotaExceeded
		}
	}

	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO account_devices (device_id, account_id, role, label, bound_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET
			account_id = excluded.account_id,
			role = excluded.role,
			label = CASE WHEN excluded.label <> '' THEN excluded.label ELSE account_devices.label END,
			last_seen_at = excluded.last_seen_at
	`, deviceID, acc.ID, role, truncate(label, 80), ms, ms)
	return err
}

func (d *DB) GetDeviceAccount(ctx context.Context, deviceID string) (Account, string, bool, error) {
	var accountID, role string
	err := d.sql.QueryRowContext(ctx,
		`SELECT account_id, role FROM account_devices WHERE device_id = ?`, deviceID,
	).Scan(&accountID, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, "", false, nil
	}
	if err != nil {
		return Account{}, "", false, err
	}
	acc, found, err := d.GetAccount(ctx, accountID)
	if err != nil || !found {
		return Account{}, "", false, err
	}
	return acc, role, true, nil
}

func (d *DB) CountPairingsForDevice(ctx context.Context, deviceID string) (int, error) {
	var count int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pairings WHERE device_a = ? OR device_b = ?`, deviceID, deviceID,
	).Scan(&count)
	return count, err
}

func (d *DB) UnbindDevice(ctx context.Context, deviceID string) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM account_devices WHERE device_id = ?`, deviceID)
	return err
}

func (d *DB) ListAccountDevices(ctx context.Context, accountID string) ([]AccountDevice, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT device_id, account_id, role, label, bound_at, last_seen_at
		FROM account_devices WHERE account_id = ?
		ORDER BY role DESC, last_seen_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountDevice
	for rows.Next() {
		var item AccountDevice
		if err := rows.Scan(&item.DeviceID, &item.AccountID, &item.Role, &item.Label, &item.BoundAt, &item.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) GetAccountTrafficToday(ctx context.Context, accountID string, now time.Time) (DailyTraffic, error) {
	day := utcDayKey(now)
	out := DailyTraffic{Day: day}
	err := d.sql.QueryRowContext(ctx, `
		SELECT msg_count, bytes_relayed FROM account_traffic_daily
		WHERE account_id = ? AND day = ?
	`, accountID, day).Scan(&out.MsgCount, &out.BytesRelayed)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}

func (d *DB) RecordAccountTraffic(ctx context.Context, accountID string, bytesCount int64, now time.Time) (DailyTraffic, error) {
	day := utcDayKey(now)
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO account_traffic_daily (account_id, day, msg_count, bytes_relayed)
		VALUES (?, ?, 1, ?)
		ON CONFLICT(account_id, day) DO UPDATE SET
			msg_count = account_traffic_daily.msg_count + 1,
			bytes_relayed = account_traffic_daily.bytes_relayed + excluded.bytes_relayed
	`, accountID, day, bytesCount)
	if err != nil {
		return DailyTraffic{}, err
	}
	return d.GetAccountTrafficToday(ctx, accountID, now)
}

// GetAccountWeeklyMinutes returns the active minutes recorded this week.
func (d *DB) GetAccountWeeklyMinutes(ctx context.Context, accountID string, now time.Time) (int, error) {
	var minutes int
	err := d.sql.QueryRowContext(ctx, `
		SELECT active_minutes FROM account_usage_weekly WHERE account_id = ? AND week = ?
	`, accountID, weekKey(now)).Scan(&minutes)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return minutes, err
}

// AddAccountActiveMinute counts one more active minute for this week and
// prunes rows older than five weeks.
func (d *DB) AddAccountActiveMinute(ctx context.Context, accountID string, now time.Time) (int, error) {
	week := weekKey(now)
	if _, err := d.sql.ExecContext(ctx, `
		INSERT INTO account_usage_weekly (account_id, week, active_minutes)
		VALUES (?, ?, 1)
		ON CONFLICT(account_id, week) DO UPDATE SET
			active_minutes = account_usage_weekly.active_minutes + 1
	`, accountID, week); err != nil {
		return 0, err
	}
	cutoff := weekStart(now).AddDate(0, 0, -35).Format("2006-01-02")
	_, _ = d.sql.ExecContext(ctx, `DELETE FROM account_usage_weekly WHERE account_id = ? AND week < ?`, accountID, cutoff)
	return d.GetAccountWeeklyMinutes(ctx, accountID, now)
}

func (d *DB) CreateRedeemCodes(ctx context.Context, count int, tier string, sponsorBadge bool, bonusDailyMB int64, maxUses int, note string, now time.Time) ([]RedeemCode, error) {
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	if maxUses < 1 {
		maxUses = 1
	}
	if tier != TierSponsor && tier != TierFree {
		tier = TierSponsor
	}
	badgeInt := 0
	if sponsorBadge {
		badgeInt = 1
	}
	ms := now.UnixMilli()
	var created []RedeemCode
	for i := 0; i < count; i++ {
		raw, err := randomHex(5)
		if err != nil {
			return nil, err
		}
		code := "HOMING-" + strings.ToUpper(raw[:4]) + "-" + strings.ToUpper(raw[4:])
		_, err = d.sql.ExecContext(ctx, `
			INSERT INTO redeem_codes (code, tier, sponsor_badge, bonus_daily_mb, max_uses, used_count, note, created_at)
			VALUES (?, ?, ?, ?, ?, 0, ?, ?)
		`, code, tier, badgeInt, bonusDailyMB, maxUses, truncate(note, 100), ms)
		if err != nil {
			return nil, err
		}
		created = append(created, RedeemCode{
			Code:         code,
			Tier:         tier,
			SponsorBadge: sponsorBadge,
			BonusDailyMB: bonusDailyMB,
			MaxUses:      maxUses,
			Note:         truncate(note, 100),
			CreatedAt:    ms,
		})
	}
	return created, nil
}

func (d *DB) RedeemCodeForAccount(ctx context.Context, accountID, rawCode string, now time.Time) (Account, error) {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	if code == "" {
		return Account{}, ErrInvalidRedeemCode
	}
	var rc RedeemCode
	var badge int
	err := d.sql.QueryRowContext(ctx, `
		SELECT code, tier, sponsor_badge, bonus_daily_mb, max_uses, used_count, note, created_at, expires_at
		FROM redeem_codes WHERE code = ?
	`, code).Scan(&rc.Code, &rc.Tier, &badge, &rc.BonusDailyMB, &rc.MaxUses, &rc.UsedCount, &rc.Note, &rc.CreatedAt, &rc.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrInvalidRedeemCode
	}
	if err != nil {
		return Account{}, err
	}
	rc.SponsorBadge = badge != 0
	ms := now.UnixMilli()
	if (rc.ExpiresAt > 0 && ms > rc.ExpiresAt) || rc.UsedCount >= rc.MaxUses {
		return Account{}, ErrInvalidRedeemCode
	}
	if accountID != "" {
		var already int
		if err := d.sql.QueryRowContext(ctx,
			`SELECT 1 FROM redeem_redemptions WHERE code = ? AND account_id = ?`, code, accountID,
		).Scan(&already); err == nil {
			return Account{}, ErrInvalidRedeemCode
		}
	}
	res, err := d.sql.ExecContext(ctx, `UPDATE redeem_codes SET used_count = used_count + 1 WHERE code = ? AND used_count < max_uses`, code)
	if err != nil {
		return Account{}, err
	}
	if affected, aErr := res.RowsAffected(); aErr == nil && affected == 0 {
		// Another request consumed the last use between the read and the update.
		return Account{}, ErrInvalidRedeemCode
	}
	if accountID == "" {
		return Account{Tier: rc.Tier, SponsorBadge: rc.SponsorBadge}, nil
	}
	_, _ = d.sql.ExecContext(ctx, `INSERT INTO redeem_redemptions (code, account_id, redeemed_at) VALUES (?, ?, ?)`, code, accountID, ms)
	bonusBytes := rc.BonusDailyMB * 1024 * 1024
	_, err = d.sql.ExecContext(ctx, `
		UPDATE accounts SET
			tier = CASE WHEN ? = 'sponsor' THEN 'sponsor' ELSE tier END,
			sponsor_badge = CASE WHEN ? = 1 THEN 1 ELSE sponsor_badge END,
			daily_quota_bytes = CASE WHEN ? > 0 THEN daily_quota_bytes + ? ELSE daily_quota_bytes END
		WHERE id = ?
	`, rc.Tier, badge, bonusBytes, bonusBytes, accountID)
	if err != nil {
		return Account{}, err
	}
	acc, _, err := d.GetAccount(ctx, accountID)
	return acc, err
}

func (d *DB) PeekValidInviteCode(ctx context.Context, rawCode string, now time.Time) bool {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	if code == "" {
		return false
	}
	var maxUses, usedCount int
	var expiresAt int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT max_uses, used_count, expires_at FROM redeem_codes WHERE code = ?`, code,
	).Scan(&maxUses, &usedCount, &expiresAt)
	if err != nil {
		return false
	}
	if expiresAt > 0 && now.UnixMilli() > expiresAt {
		return false
	}
	return usedCount < maxUses
}

type AdminAccountRow struct {
	Account
	Limits       TierLimits   `json:"limits"`
	HostCount    int          `json:"host_count"`
	ClientCount  int          `json:"client_count"`
	KeyCount     int          `json:"key_count"`
	TodayTraffic DailyTraffic `json:"today_traffic"`
	Weekly       WeeklyUsage  `json:"weekly"`
}

func (d *DB) ListAdminAccounts(ctx context.Context, query string, now time.Time) ([]AdminAccountRow, error) {
	q := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	day := utcDayKey(now)
	week := weekKey(now)
	rows, err := d.sql.QueryContext(ctx, `
		SELECT
			a.id, a.email, a.github_id, a.github_login, a.display_name,
			a.tier, a.status, a.sponsor_badge,
			a.max_hosts, a.max_devices, a.max_msg_bytes, a.daily_quota_bytes, a.weekly_minutes,
			a.created_at, a.last_seen_at, a.note,
			(SELECT COUNT(*) FROM account_devices ad WHERE ad.account_id = a.id AND ad.role = 'host'),
			(SELECT COUNT(*) FROM account_devices ad WHERE ad.account_id = a.id AND ad.role = 'client'),
			(SELECT COUNT(*) FROM relay_keys rk WHERE rk.account_id = a.id AND rk.revoked_at = 0),
			COALESCE(t.msg_count, 0),
			COALESCE(t.bytes_relayed, 0),
			COALESCE((SELECT active_minutes FROM account_usage_weekly uw WHERE uw.account_id = a.id AND uw.week = ?), 0)
		FROM accounts a
		LEFT JOIN account_traffic_daily t ON t.account_id = a.id AND t.day = ?
		WHERE (? = '%%' OR LOWER(a.email) LIKE ? OR LOWER(a.github_login) LIKE ? OR LOWER(a.display_name) LIKE ? OR LOWER(a.id) LIKE ?)
		ORDER BY a.created_at DESC
		LIMIT 200
	`, week, day, q, q, q, q, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminAccountRow
	for rows.Next() {
		var r AdminAccountRow
		var badge int
		r.TodayTraffic.Day = day
		r.Weekly.Week = week
		r.Weekly.ResetsAt = weekResetAt(now).UnixMilli()
		if err := rows.Scan(
			&r.ID, &r.Email, &r.GitHubID, &r.GitHubLogin, &r.DisplayName,
			&r.Tier, &r.Status, &badge,
			&r.MaxHosts, &r.MaxDevices, &r.MaxMsgBytes, &r.DailyQuotaBytes, &r.WeeklyMinutes,
			&r.CreatedAt, &r.LastSeenAt, &r.Note,
			&r.HostCount, &r.ClientCount, &r.KeyCount,
			&r.TodayTraffic.MsgCount, &r.TodayTraffic.BytesRelayed,
			&r.Weekly.ActiveMinutes,
		); err != nil {
			return nil, err
		}
		r.SponsorBadge = badge != 0
		r.Limits = r.Account.EffectiveLimits()
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) UpdateAdminAccount(ctx context.Context, id string, tier, status *string, sponsorBadge *bool, maxHosts, maxDevices *int, maxMsgBytes, dailyQuotaBytes *int64, weeklyMinutes *int, note *string) (Account, error) {
	acc, found, err := d.GetAccount(ctx, id)
	if err != nil {
		return Account{}, err
	}
	if !found {
		return Account{}, sql.ErrNoRows
	}
	if tier != nil && (*tier == TierFree || *tier == TierSponsor) {
		acc.Tier = *tier
	}
	if status != nil && (*status == AccountStatusActive || *status == AccountStatusBanned) {
		acc.Status = *status
	}
	if sponsorBadge != nil {
		acc.SponsorBadge = *sponsorBadge
	}
	if maxHosts != nil && *maxHosts >= 0 {
		acc.MaxHosts = *maxHosts
	}
	if maxDevices != nil && *maxDevices >= 0 {
		acc.MaxDevices = *maxDevices
	}
	if maxMsgBytes != nil && *maxMsgBytes >= 0 {
		acc.MaxMsgBytes = *maxMsgBytes
	}
	if dailyQuotaBytes != nil && *dailyQuotaBytes >= 0 {
		acc.DailyQuotaBytes = *dailyQuotaBytes
	}
	if weeklyMinutes != nil && *weeklyMinutes >= 0 {
		acc.WeeklyMinutes = *weeklyMinutes
	}
	if note != nil {
		acc.Note = truncate(*note, 200)
	}
	badge := 0
	if acc.SponsorBadge {
		badge = 1
	}
	_, err = d.sql.ExecContext(ctx, `
		UPDATE accounts SET
			tier = ?, status = ?, sponsor_badge = ?,
			max_hosts = ?, max_devices = ?, max_msg_bytes = ?, daily_quota_bytes = ?, weekly_minutes = ?, note = ?
		WHERE id = ?
	`, acc.Tier, acc.Status, badge, acc.MaxHosts, acc.MaxDevices, acc.MaxMsgBytes, acc.DailyQuotaBytes, acc.WeeklyMinutes, acc.Note, acc.ID)
	return acc, err
}

func (d *DB) ListRedeemCodes(ctx context.Context) ([]RedeemCode, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT code, tier, sponsor_badge, bonus_daily_mb, max_uses, used_count, note, created_at, expires_at
		FROM redeem_codes ORDER BY created_at DESC LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RedeemCode
	for rows.Next() {
		var r RedeemCode
		var badge int
		if err := rows.Scan(&r.Code, &r.Tier, &badge, &r.BonusDailyMB, &r.MaxUses, &r.UsedCount, &r.Note, &r.CreatedAt, &r.ExpiresAt); err != nil {
			return nil, err
		}
		r.SponsorBadge = badge != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

type AdminOverviewStats struct {
	TotalAccounts      int   `json:"total_accounts"`
	SponsorAccounts    int   `json:"sponsor_accounts"`
	BannedAccounts     int   `json:"banned_accounts"`
	TotalDevices       int   `json:"total_devices"`
	QueuedEnvelopes    int   `json:"queued_envelopes"`
	TodayMessages      int64 `json:"today_messages"`
	TodayBytes         int64 `json:"today_bytes"`
	WeekActiveAccounts int   `json:"week_active_accounts"`
	WeekActiveMinutes  int64 `json:"week_active_minutes"`
}

func (d *DB) GetAdminOverview(ctx context.Context, now time.Time) (AdminOverviewStats, error) {
	var st AdminOverviewStats
	day := utcDayKey(now)
	_ = d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&st.TotalAccounts)
	_ = d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE tier = 'sponsor' OR sponsor_badge = 1`).Scan(&st.SponsorAccounts)
	_ = d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE status = 'banned'`).Scan(&st.BannedAccounts)
	_ = d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&st.TotalDevices)
	_ = d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM queue`).Scan(&st.QueuedEnvelopes)
	_ = d.sql.QueryRowContext(ctx, `SELECT COALESCE(SUM(msg_count), 0), COALESCE(SUM(bytes_relayed), 0) FROM account_traffic_daily WHERE day = ?`, day).Scan(&st.TodayMessages, &st.TodayBytes)
	_ = d.sql.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(active_minutes), 0) FROM account_usage_weekly WHERE week = ?`, weekKey(now)).Scan(&st.WeekActiveAccounts, &st.WeekActiveMinutes)
	return st, nil
}
