package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	accountSessionCookie = "homing_account_session"
	adminSessionCookie   = "homing_admin_session"
	githubOAuthStateCk   = "homing_gh_state"
)

// EmailSender sends one verification-code email.
type EmailSender interface {
	SendLoginCode(ctx context.Context, toEmail, code string) error
}

type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

func (c SMTPConfig) Configured() bool {
	return strings.TrimSpace(c.Host) != "" && strings.TrimSpace(c.From) != ""
}

type SMTPEmailSender struct {
	cfg SMTPConfig
}

func NewSMTPEmailSender(cfg SMTPConfig) *SMTPEmailSender {
	if cfg.Port <= 0 {
		cfg.Port = 465
	}
	return &SMTPEmailSender{cfg: cfg}
}

func (s *SMTPEmailSender) SendLoginCode(ctx context.Context, toEmail, code string) error {
	if !s.cfg.Configured() {
		return errors.New("SMTP 发信服务尚未配置")
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	subject := "Homing 官方中继登录验证码：" + code
	body := fmt.Sprintf(
		"你好，\r\n\r\n你的 Homing 官方中继验证码为：%s（10 分钟内有效）。\r\n如非本人操作请忽略本邮件。\r\n",
		code,
	)
	msg := strings.Join([]string{
		"From: " + s.cfg.From,
		"To: " + toEmail,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var auth smtp.Auth
	if s.cfg.User != "" {
		auth = smtp.PlainAuth("", s.cfg.User, s.cfg.Pass, s.cfg.Host)
	}

	if s.cfg.Port == 465 {
		rawConn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return err
		}
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return err
		}
		client, err := smtp.NewClient(tlsConn, s.cfg.Host)
		if err != nil {
			_ = tlsConn.Close()
			return err
		}
		defer client.Close()
		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
		if err := client.Mail(extractBareEmail(s.cfg.From)); err != nil {
			return err
		}
		if err := client.Rcpt(toEmail); err != nil {
			return err
		}
		wc, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := wc.Write([]byte(msg)); err != nil {
			_ = wc.Close()
			return err
		}
		if err := wc.Close(); err != nil {
			return err
		}
		return client.Quit()
	}

	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(rawConn, s.cfg.Host)
	if err != nil {
		_ = rawConn.Close()
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(extractBareEmail(s.cfg.From)); err != nil {
		return err
	}
	if err := client.Rcpt(toEmail); err != nil {
		return err
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write([]byte(msg)); err != nil {
		_ = wc.Close()
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func extractBareEmail(from string) string {
	if parsed, err := mail.ParseAddress(from); err == nil && parsed.Address != "" {
		return parsed.Address
	}
	return strings.TrimSpace(from)
}

type GitHubOAuthConfig struct {
	ClientID     string
	ClientSecret string
	PublicBase   string
	AuthorizeURL string // overridable in tests
	TokenURL     string // overridable in tests
	APIBaseURL   string // overridable in tests
}

func (c GitHubOAuthConfig) Configured() bool {
	return strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != ""
}

func (s *Server) isManagedMode() bool {
	return strings.EqualFold(strings.TrimSpace(s.cfg.AuthMode), AuthModeManaged)
}

func (s *Server) registrationMode(ctx context.Context) string {
	fallback, ok := NormalizeRegistrationMode(s.cfg.RegistrationMode)
	if !ok {
		fallback = RegModeOpen
	}
	if val, ok := NormalizeRegistrationMode(s.db.GetSetting(ctx, "registration_mode", fallback)); ok {
		return val
	}
	return fallback
}

// limitsFor applies the server's weekly allowance to an account's tier limits.
func (s *Server) limitsFor(acc Account) TierLimits {
	limits := acc.EffectiveLimits()
	if acc.WeeklyMinutes > 0 {
		return limits
	}
	if acc.Tier == TierSponsor {
		limits.WeeklyActiveMinutes = s.cfg.SponsorWeeklyMinutes
	} else {
		limits.WeeklyActiveMinutes = s.cfg.FreeWeeklyMinutes
	}
	if limits.WeeklyActiveMinutes < 0 {
		limits.WeeklyActiveMinutes = 0
	}
	return limits
}

func (s *Server) weeklyUsage(ctx context.Context, acc Account) WeeklyUsage {
	now := s.now()
	used, _ := s.db.GetAccountWeeklyMinutes(ctx, acc.ID, now)
	return WeeklyUsage{
		Week:          weekKey(now),
		ActiveMinutes: used,
		LimitMinutes:  s.limitsFor(acc).WeeklyActiveMinutes,
		ResetsAt:      weekResetAt(now).UnixMilli(),
	}
}

func formatQuotaMinutes(minutes int) string {
	if minutes%60 == 0 {
		return fmt.Sprintf("%d 小时", minutes/60)
	}
	return fmt.Sprintf("%d 分钟", minutes)
}

// consumeWeeklyMinute counts the current minute of phone-originated relay
// traffic. Every envelope within an already counted minute is free, so a
// conversation is never cut off half way through a minute.
func (s *Server) consumeWeeklyMinute(conn *clientConn, acc Account, limit int, now time.Time) bool {
	minute := now.Unix() / 60
	s.usageMu.Lock()
	counted := s.activeMinute[acc.ID] == minute
	s.usageMu.Unlock()
	if counted {
		return true
	}
	if limit > 0 {
		used, err := s.db.GetAccountWeeklyMinutes(s.ctx, acc.ID, now)
		if err == nil && used >= limit {
			reset := weekResetAt(now).In(weeklyQuotaZone).Format("01-02 15:04")
			conn.sendError(s.ctx, "weekly_time_exceeded", fmt.Sprintf(
				"本周官方中继远程操作时长（%s）已用完，北京时间 %s 自动重置。期间仍可接收电脑推送；也可改用局域网直连、自建中继，或兑换赞助额度。",
				formatQuotaMinutes(limit), reset))
			return false
		}
	}
	s.usageMu.Lock()
	if s.activeMinute[acc.ID] == minute {
		s.usageMu.Unlock()
		return true
	}
	s.activeMinute[acc.ID] = minute
	s.usageMu.Unlock()
	if _, err := s.db.AddAccountActiveMinute(s.ctx, acc.ID, now); err != nil {
		s.log.Warn("record weekly usage failed", "account", acc.ID, "err", err)
	}
	return true
}

// rateBlocked reports whether key has used up max attempts in its window.
func (s *Server) rateBlocked(key string, max int) bool {
	now := s.now()
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	entry := s.rates[key]
	return entry != nil && entry.resetAt.After(now) && entry.count >= max
}

// rateHit records one attempt for key within window.
func (s *Server) rateHit(key string, window time.Duration) {
	now := s.now()
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	entry := s.rates[key]
	if entry == nil || !entry.resetAt.After(now) {
		entry = &codeAttempt{resetAt: now.Add(window)}
		s.rates[key] = entry
	}
	entry.count++
}

func (s *Server) pruneRates(now time.Time) {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	for key, entry := range s.rates {
		if !entry.resetAt.After(now) {
			delete(s.rates, key)
		}
	}
	s.usageMu.Lock()
	minute := now.Unix() / 60
	for id, last := range s.activeMinute {
		if minute-last > 5 {
			delete(s.activeMinute, id)
		}
	}
	s.usageMu.Unlock()
}

const (
	adminFailLimit   = 10
	adminFailWindow  = 15 * time.Minute
	mailSendLimit    = 10
	mailSendWindow   = time.Hour
	unboundSocketTTL = pairTTL + time.Minute
)

func extractBearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func writeServerJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func isHTTPSRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) adminCookieValue() string {
	mac := hmac.New(sha256.New, []byte(s.cfg.AdminToken))
	_, _ = mac.Write([]byte("homing-admin-cookie-v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) isAdminAuthorized(r *http.Request) bool {
	adminTok := strings.TrimSpace(s.cfg.AdminToken)
	if adminTok == "" {
		return false
	}
	if bearer := extractBearerToken(r); bearer != "" {
		return subtle.ConstantTimeCompare([]byte(bearer), []byte(adminTok)) == 1
	}
	if ck, err := r.Cookie(adminSessionCookie); err == nil && ck.Value != "" {
		expected := s.adminCookieValue()
		return subtle.ConstantTimeCompare([]byte(ck.Value), []byte(expected)) == 1
	}
	return false
}

func (s *Server) currentAccountFromRequest(r *http.Request) (Account, string, bool) {
	if bearer := extractBearerToken(r); strings.HasPrefix(bearer, "hses_") {
		if acc, ok, err := s.db.AuthenticateAccountSession(r.Context(), bearer, s.now()); err == nil && ok {
			return acc, bearer, true
		}
	}
	if ck, err := r.Cookie(accountSessionCookie); err == nil && strings.HasPrefix(ck.Value, "hses_") {
		if acc, ok, err := s.db.AuthenticateAccountSession(r.Context(), ck.Value, s.now()); err == nil && ok {
			return acc, ck.Value, true
		}
	}
	return Account{}, "", false
}

func (s *Server) setAccountCookie(w http.ResponseWriter, r *http.Request, sessionToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     accountSessionCookie,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPSRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(accountSessionTTL / time.Second),
	})
}

func (s *Server) handleManagedHTTP(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && (path == "/account" || path == "/account/"):
		s.serveAccountPortalHTML(w, r)
		return true
	case r.Method == http.MethodGet && path == "/v1/account/config":
		writeServerJSON(w, http.StatusOK, map[string]any{
			"ok":                  true,
			"auth_mode":           s.effectiveAuthMode(),
			"registration_mode":   s.registrationMode(r.Context()),
			"email_enabled":       s.emailAuthAvailable(),
			"github_enabled":      s.cfg.GitHubOAuth.Configured(),
			"ws_url":              s.publicWSURL(r),
			"free_weekly_minutes": s.cfg.FreeWeeklyMinutes,
		})
		return true
	case r.Method == http.MethodPost && path == "/v1/account/email/send-code":
		s.handleAccountSendEmailCode(w, r)
		return true
	case r.Method == http.MethodPost && path == "/v1/account/email/verify":
		s.handleAccountVerifyEmailCode(w, r)
		return true
	case r.Method == http.MethodGet && path == "/v1/account/github/start":
		s.handleGitHubOAuthStart(w, r)
		return true
	case r.Method == http.MethodGet && path == "/v1/account/github/callback":
		s.handleGitHubOAuthCallback(w, r)
		return true
	case r.Method == http.MethodPost && path == "/v1/account/logout":
		if _, tok, ok := s.currentAccountFromRequest(r); ok {
			_ = s.db.DeleteAccountSession(r.Context(), tok)
		}
		http.SetCookie(w, &http.Cookie{Name: accountSessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
		writeServerJSON(w, http.StatusOK, map[string]any{"ok": true})
		return true
	case r.Method == http.MethodGet && path == "/v1/account/me":
		s.handleAccountMe(w, r)
		return true
	case r.Method == http.MethodPost && path == "/v1/account/keys":
		s.handleAccountCreateKey(w, r)
		return true
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/v1/account/keys/"):
		s.handleAccountRevokeKey(w, r, strings.TrimPrefix(path, "/v1/account/keys/"))
		return true
	case r.Method == http.MethodPost && path == "/v1/account/redeem":
		s.handleAccountRedeem(w, r)
		return true

	case r.Method == http.MethodGet && (path == "/admin" || path == "/admin/"):
		s.serveAdminPortalHTML(w, r)
		return true
	case r.Method == http.MethodPost && path == "/v1/admin/login":
		s.handleAdminLogin(w, r)
		return true
	case r.Method == http.MethodPost && path == "/v1/admin/logout":
		http.SetCookie(w, &http.Cookie{Name: adminSessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
		writeServerJSON(w, http.StatusOK, map[string]any{"ok": true})
		return true
	case strings.HasPrefix(path, "/v1/admin/"):
		s.handleAdminAPI(w, r)
		return true
	}
	return false
}

func (s *Server) effectiveAuthMode() string {
	if s.isManagedMode() {
		return AuthModeManaged
	}
	return AuthModeOpen
}

func (s *Server) emailAuthAvailable() bool {
	return s.emailSender != nil || s.cfg.SMTP.Configured()
}

func (s *Server) publicBaseURL(r *http.Request) string {
	if base := strings.TrimRight(strings.TrimSpace(s.cfg.GitHubOAuth.PublicBase), "/"); base != "" {
		return base
	}
	scheme := "http"
	if isHTTPSRequest(r) {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = s.cfg.Addr
	}
	return scheme + "://" + host
}

func (s *Server) publicWSURL(r *http.Request) string {
	base := s.publicBaseURL(r)
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://") + "/v1/ws"
	}
	return "ws://" + strings.TrimPrefix(base, "http://") + "/v1/ws"
}

func (s *Server) handleAccountSendEmailCode(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var body struct {
		Email      string `json:"email"`
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求格式无效"})
		return
	}
	parsed, err := mail.ParseAddress(strings.TrimSpace(body.Email))
	if err != nil || !strings.Contains(parsed.Address, "@") {
		writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请输入有效的邮箱地址"})
		return
	}
	email := strings.ToLower(parsed.Address)
	sender := s.emailSender
	if sender == nil && s.cfg.SMTP.Configured() {
		sender = NewSMTPEmailSender(s.cfg.SMTP)
	}
	if sender == nil {
		writeServerJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": "当前服务器尚未配置邮箱发信（HOMING_SMTP_*），请使用 GitHub 登录或联系管理员",
		})
		return
	}
	mailKey := "mail:" + s.clientAddress(r)
	if s.rateBlocked(mailKey, mailSendLimit) {
		writeServerJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "该网络发送验证码过于频繁，请 1 小时后再试"})
		return
	}

	_, exists, err := s.db.GetAccountByEmail(r.Context(), email)
	if err != nil {
		writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "读取账号失败"})
		return
	}
	if !exists {
		regMode := s.registrationMode(r.Context())
		if regMode == RegModeClosed {
			writeServerJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "当前官方中继暂未开放新用户注册"})
			return
		}
		if regMode == RegModeInvite && !s.db.PeekValidInviteCode(r.Context(), body.InviteCode, s.now()) {
			writeServerJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "当前为邀请制注册，请填写有效的邀请码或赞助兑换码"})
			return
		}
	}

	code, err := s.db.IssueEmailCode(r.Context(), email, s.now())
	if err != nil {
		if err.Error() == "cooldown" {
			writeServerJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "发送过于频繁，请 60 秒后再试"})
			return
		}
		writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "生成验证码失败"})
		return
	}
	s.rateHit(mailKey, mailSendWindow)
	sendCtx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if err := sender.SendLoginCode(sendCtx, email, code); err != nil {
		s.log.Warn("send login code failed", "email", email, "err", err)
		writeServerJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "验证码邮件发送失败，请稍后重试"})
		return
	}
	writeServerJSON(w, http.StatusOK, map[string]any{"ok": true, "expires_in": int(emailCodeTTL / time.Second)})
}

func (s *Server) handleAccountVerifyEmailCode(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var body struct {
		Email      string `json:"email"`
		Code       string `json:"code"`
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求格式无效"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	valid, err := s.db.VerifyEmailCode(r.Context(), email, body.Code, s.now())
	if err != nil {
		writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "校验失败"})
		return
	}
	if !valid {
		writeServerJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "验证码错误或已过期"})
		return
	}

	_, exists, _ := s.db.GetAccountByEmail(r.Context(), email)
	regMode := s.registrationMode(r.Context())
	allowCreate := regMode == RegModeOpen || (regMode == RegModeInvite && s.db.PeekValidInviteCode(r.Context(), body.InviteCode, s.now()))
	acc, created, err := s.db.CreateOrLoginEmailAccount(r.Context(), email, allowCreate || exists, s.now())
	if err != nil {
		writeServerJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "当前未开放直接注册，请填写有效邀请码"})
		return
	}
	if acc.Status != AccountStatusActive {
		writeServerJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "该账号已被停用，请联系管理员"})
		return
	}
	if strings.TrimSpace(body.InviteCode) != "" {
		if upgraded, rErr := s.db.RedeemCodeForAccount(r.Context(), acc.ID, body.InviteCode, s.now()); rErr == nil {
			acc = upgraded
		}
	}
	var issuedKey string
	if created {
		if rawKey, _, kErr := s.db.CreateRelayKey(r.Context(), acc.ID, "默认电脑密钥", s.now()); kErr == nil {
			issuedKey = rawKey
		}
	}
	sessionToken, err := s.db.CreateAccountSession(r.Context(), acc.ID, s.now())
	if err != nil {
		writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "创建登录会话失败"})
		return
	}
	s.setAccountCookie(w, r, sessionToken)
	writeServerJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"created":       created,
		"session_token": sessionToken,
		"initial_key":   issuedKey,
		"account":       acc,
	})
}

func (s *Server) handleGitHubOAuthStart(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.GitHubOAuth.Configured() {
		http.Error(w, "GitHub OAuth not configured", http.StatusNotFound)
		return
	}
	state, err := randomHex(16)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	invite := strings.TrimSpace(r.URL.Query().Get("invite"))
	cookieVal := state
	if invite != "" {
		cookieVal = state + "|" + invite
	}
	http.SetCookie(w, &http.Cookie{
		Name:     githubOAuthStateCk,
		Value:    cookieVal,
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPSRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	authBase := s.cfg.GitHubOAuth.AuthorizeURL
	if authBase == "" {
		authBase = "https://github.com/login/oauth/authorize"
	}
	q := url.Values{}
	q.Set("client_id", s.cfg.GitHubOAuth.ClientID)
	q.Set("redirect_uri", s.publicBaseURL(r)+"/v1/account/github/callback")
	q.Set("scope", "read:user user:email")
	q.Set("state", state)
	http.Redirect(w, r, authBase+"?"+q.Encode(), http.StatusFound)
}

func (s *Server) handleGitHubOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.GitHubOAuth.Configured() {
		http.Redirect(w, r, "/account?error=github_disabled", http.StatusFound)
		return
	}
	ck, err := r.Cookie(githubOAuthStateCk)
	if err != nil || ck.Value == "" {
		http.Redirect(w, r, "/account?error=invalid_oauth_state", http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: githubOAuthStateCk, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	parts := strings.SplitN(ck.Value, "|", 2)
	expectedState := parts[0]
	invite := ""
	if len(parts) == 2 {
		invite = parts[1]
	}
	if subtle.ConstantTimeCompare([]byte(expectedState), []byte(r.URL.Query().Get("state"))) != 1 {
		http.Redirect(w, r, "/account?error=invalid_oauth_state", http.StatusFound)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Redirect(w, r, "/account?error=oauth_cancelled", http.StatusFound)
		return
	}

	ghID, ghLogin, ghEmail, err := s.exchangeGitHubCode(r, code)
	if err != nil {
		s.log.Warn("github oauth exchange failed", "err", err)
		http.Redirect(w, r, "/account?error=github_exchange_failed", http.StatusFound)
		return
	}

	regMode := s.registrationMode(r.Context())
	allowCreate := regMode == RegModeOpen || (regMode == RegModeInvite && s.db.PeekValidInviteCode(r.Context(), invite, s.now()))
	acc, created, err := s.db.CreateOrLoginGitHubAccount(r.Context(), ghID, ghLogin, ghEmail, allowCreate, s.now())
	if err != nil {
		http.Redirect(w, r, "/account?error=registration_closed", http.StatusFound)
		return
	}
	if acc.Status != AccountStatusActive {
		http.Redirect(w, r, "/account?error=account_banned", http.StatusFound)
		return
	}
	if invite != "" {
		_, _ = s.db.RedeemCodeForAccount(r.Context(), acc.ID, invite, s.now())
	}
	redirectTarget := "/account"
	if created {
		if rawKey, _, kErr := s.db.CreateRelayKey(r.Context(), acc.ID, "GitHub 电脑密钥", s.now()); kErr == nil {
			redirectTarget = "/account#new_key=" + url.QueryEscape(rawKey)
		}
	}
	sessionToken, err := s.db.CreateAccountSession(r.Context(), acc.ID, s.now())
	if err != nil {
		http.Redirect(w, r, "/account?error=session_failed", http.StatusFound)
		return
	}
	s.setAccountCookie(w, r, sessionToken)
	http.Redirect(w, r, redirectTarget, http.StatusFound)
}

func (s *Server) exchangeGitHubCode(r *http.Request, code string) (id, login, email string, err error) {
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	tokenURL := s.cfg.GitHubOAuth.TokenURL
	if tokenURL == "" {
		tokenURL = "https://github.com/login/oauth/access_token"
	}
	apiBase := strings.TrimRight(s.cfg.GitHubOAuth.APIBaseURL, "/")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}

	form := url.Values{}
	form.Set("client_id", s.cfg.GitHubOAuth.ClientID)
	form.Set("client_secret", s.cfg.GitHubOAuth.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", s.publicBaseURL(r)+"/v1/account/github/callback")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	rawTok, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	var tokPayload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(rawTok, &tokPayload); err != nil || tokPayload.AccessToken == "" {
		return "", "", "", fmt.Errorf("oauth token empty (%s)", tokPayload.Error)
	}

	uReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/user", nil)
	if err != nil {
		return "", "", "", err
	}
	uReq.Header.Set("Authorization", "Bearer "+tokPayload.AccessToken)
	uReq.Header.Set("Accept", "application/vnd.github+json")
	uResp, err := client.Do(uReq)
	if err != nil {
		return "", "", "", err
	}
	defer uResp.Body.Close()
	rawUser, _ := io.ReadAll(io.LimitReader(uResp.Body, 32<<10))
	var ghUser struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(rawUser, &ghUser); err != nil || ghUser.ID == 0 {
		return "", "", "", errors.New("invalid github user response")
	}
	email = strings.ToLower(strings.TrimSpace(ghUser.Email))
	if email == "" {
		eReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/user/emails", nil)
		eReq.Header.Set("Authorization", "Bearer "+tokPayload.AccessToken)
		eReq.Header.Set("Accept", "application/vnd.github+json")
		if eResp, eErr := client.Do(eReq); eErr == nil {
			defer eResp.Body.Close()
			rawEmails, _ := io.ReadAll(io.LimitReader(eResp.Body, 32<<10))
			var items []struct {
				Email    string `json:"email"`
				Primary  bool   `json:"primary"`
				Verified bool   `json:"verified"`
			}
			if json.Unmarshal(rawEmails, &items) == nil {
				for _, item := range items {
					if item.Verified && item.Primary {
						email = strings.ToLower(strings.TrimSpace(item.Email))
						break
					}
				}
			}
		}
	}
	return strconv.FormatInt(ghUser.ID, 10), ghUser.Login, email, nil
}

func (s *Server) handleAccountMe(w http.ResponseWriter, r *http.Request) {
	acc, _, ok := s.currentAccountFromRequest(r)
	if !ok {
		writeServerJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	keys, _ := s.db.ListRelayKeys(r.Context(), acc.ID)
	devices, _ := s.db.ListAccountDevices(r.Context(), acc.ID)
	for i := range devices {
		devices[i].Online = s.isOnline(devices[i].DeviceID)
	}
	traffic, _ := s.db.GetAccountTrafficToday(r.Context(), acc.ID, s.now())
	writeServerJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"account": acc,
		"limits":  s.limitsFor(acc),
		"keys":    keys,
		"devices": devices,
		"traffic": traffic,
		"weekly":  s.weeklyUsage(r.Context(), acc),
		"ws_url":  s.publicWSURL(r),
	})
}

func (s *Server) handleAccountCreateKey(w http.ResponseWriter, r *http.Request) {
	acc, _, ok := s.currentAccountFromRequest(r)
	if !ok {
		writeServerJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	if acc.Status != AccountStatusActive {
		writeServerJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "账号已被停用"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var body struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	rawKey, rec, err := s.db.CreateRelayKey(r.Context(), acc.ID, body.Label, s.now())
	if err != nil {
		writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "生成密钥失败"})
		return
	}
	writeServerJSON(w, http.StatusOK, map[string]any{"ok": true, "relay_key": rawKey, "record": rec})
}

func (s *Server) handleAccountRevokeKey(w http.ResponseWriter, r *http.Request, keyID string) {
	acc, _, ok := s.currentAccountFromRequest(r)
	if !ok {
		writeServerJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	if err := s.db.RevokeRelayKey(r.Context(), acc.ID, strings.TrimSpace(keyID), s.now()); err != nil {
		writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "吊销失败"})
		return
	}
	writeServerJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAccountRedeem(w http.ResponseWriter, r *http.Request) {
	acc, _, ok := s.currentAccountFromRequest(r)
	if !ok {
		writeServerJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求无效"})
		return
	}
	updated, err := s.db.RedeemCodeForAccount(r.Context(), acc.ID, body.Code, s.now())
	if err != nil {
		writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "兑换码无效、已使用或已过期"})
		return
	}
	writeServerJSON(w, http.StatusOK, map[string]any{"ok": true, "account": updated, "limits": s.limitsFor(updated)})
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	adminTok := strings.TrimSpace(s.cfg.AdminToken)
	if adminTok == "" {
		writeServerJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "未配置 HOMING_ADMIN_TOKEN"})
		return
	}
	failKey := "admin:" + s.clientAddress(r)
	if s.rateBlocked(failKey, adminFailLimit) {
		writeServerJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "管理员令牌错误次数过多，请 15 分钟后再试"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求格式无效"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(body.Token)), []byte(adminTok)) != 1 {
		s.rateHit(failKey, adminFailWindow)
		writeServerJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "管理员令牌错误"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    s.adminCookieValue(),
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPSRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 3600,
	})
	writeServerJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	failKey := "admin:" + s.clientAddress(r)
	if s.rateBlocked(failKey, adminFailLimit) {
		writeServerJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "admin_rate_limited"})
		return
	}
	if !s.isAdminAuthorized(r) {
		if extractBearerToken(r) != "" {
			s.rateHit(failKey, adminFailWindow)
		}
		writeServerJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "admin_unauthorized"})
		return
	}
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/v1/admin/overview":
		overview, err := s.db.GetAdminOverview(r.Context(), s.now())
		if err != nil {
			writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		onlineDevices, onlineConns := s.onlineCounts()
		writeServerJSON(w, http.StatusOK, map[string]any{
			"ok":                     true,
			"auth_mode":              s.effectiveAuthMode(),
			"registration_mode":      s.registrationMode(r.Context()),
			"online_devices":         onlineDevices,
			"online_sockets":         onlineConns,
			"free_weekly_minutes":    s.cfg.FreeWeeklyMinutes,
			"sponsor_weekly_minutes": s.cfg.SponsorWeeklyMinutes,
			"stats":                  overview,
		})

	case r.Method == http.MethodPut && path == "/v1/admin/settings":
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var body struct {
			RegistrationMode string `json:"registration_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
			return
		}
		mode, ok := NormalizeRegistrationMode(body.RegistrationMode)
		if !ok {
			writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_registration_mode"})
			return
		}
		if err := s.db.SetSetting(r.Context(), "registration_mode", mode); err != nil {
			writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeServerJSON(w, http.StatusOK, map[string]any{"ok": true, "registration_mode": mode})

	case r.Method == http.MethodGet && path == "/v1/admin/accounts":
		accounts, err := s.db.ListAdminAccounts(r.Context(), r.URL.Query().Get("q"), s.now())
		if err != nil {
			writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		for i := range accounts {
			accounts[i].Limits = s.limitsFor(accounts[i].Account)
			accounts[i].Weekly.LimitMinutes = accounts[i].Limits.WeeklyActiveMinutes
		}
		writeServerJSON(w, http.StatusOK, map[string]any{"ok": true, "accounts": accounts})

	case r.Method == http.MethodPatch && strings.HasPrefix(path, "/v1/admin/accounts/"):
		accountID := strings.TrimSpace(strings.TrimPrefix(path, "/v1/admin/accounts/"))
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		var body struct {
			Tier            *string `json:"tier"`
			Status          *string `json:"status"`
			SponsorBadge    *bool   `json:"sponsor_badge"`
			MaxHosts        *int    `json:"max_hosts"`
			MaxDevices      *int    `json:"max_devices"`
			MaxMsgBytes     *int64  `json:"max_msg_bytes"`
			DailyQuotaBytes *int64  `json:"daily_quota_bytes"`
			WeeklyMinutes   *int    `json:"weekly_minutes"`
			Note            *string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
			return
		}
		updated, err := s.db.UpdateAdminAccount(
			r.Context(), accountID,
			body.Tier, body.Status, body.SponsorBadge,
			body.MaxHosts, body.MaxDevices, body.MaxMsgBytes, body.DailyQuotaBytes, body.WeeklyMinutes, body.Note,
		)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			writeServerJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if updated.Status == AccountStatusBanned {
			s.kickAccountDevices(updated.ID)
		}
		writeServerJSON(w, http.StatusOK, map[string]any{"ok": true, "account": updated, "limits": s.limitsFor(updated)})

	case r.Method == http.MethodGet && path == "/v1/admin/redeem-codes":
		codes, err := s.db.ListRedeemCodes(r.Context())
		if err != nil {
			writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeServerJSON(w, http.StatusOK, map[string]any{"ok": true, "codes": codes})

	case r.Method == http.MethodPost && path == "/v1/admin/redeem-codes":
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		var body struct {
			Count        int    `json:"count"`
			Tier         string `json:"tier"`
			SponsorBadge *bool  `json:"sponsor_badge"`
			BonusDailyMB int64  `json:"bonus_daily_mb"`
			MaxUses      int    `json:"max_uses"`
			Note         string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeServerJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid_json"})
			return
		}
		badge := true
		if body.SponsorBadge != nil {
			badge = *body.SponsorBadge
		}
		codes, err := s.db.CreateRedeemCodes(r.Context(), body.Count, body.Tier, badge, body.BonusDailyMB, body.MaxUses, body.Note, s.now())
		if err != nil {
			writeServerJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeServerJSON(w, http.StatusOK, map[string]any{"ok": true, "codes": codes})

	default:
		writeServerJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not_found"})
	}
}

func (s *Server) onlineCounts() (devices int, sockets int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns), len(s.all)
}

func (s *Server) kickAccountDevices(accountID string) {
	devices, err := s.db.ListAccountDevices(s.ctx, accountID)
	if err != nil {
		return
	}
	s.mu.Lock()
	var targets []*clientConn
	for _, dev := range devices {
		for conn := range s.conns[dev.DeviceID] {
			targets = append(targets, conn)
		}
	}
	s.mu.Unlock()
	for _, conn := range targets {
		conn.sendError(s.ctx, "account_suspended", "该官方中继账号已被管理员停用")
		conn.close(websocket.StatusPolicyViolation, "account suspended")
	}
}

// authorizeManagedHello binds or verifies an incoming WebSocket device when
// AuthMode == "managed". Returns (allowHello, flushOfflineQueue).
func (s *Server) authorizeManagedHello(conn *clientConn, message map[string]any, deviceID string) (bool, bool) {
	if !s.isManagedMode() {
		return true, true
	}
	rawKey := strings.TrimSpace(conn.initialRelayKey)
	if rawKey == "" {
		rawKey = strings.TrimSpace(stringField(message, "access_key"))
	}
	now := s.now()

	if rawKey != "" {
		acc, err := s.db.AuthenticateRelayKey(s.ctx, rawKey, now)
		if err != nil {
			if errors.Is(err, ErrAccountBanned) {
				conn.sendError(s.ctx, "account_suspended", "该官方中继账号已被停用")
			} else {
				conn.sendError(s.ctx, "invalid_relay_key", "官方中继密钥（Relay Key）无效或已吊销")
			}
			conn.close(websocket.StatusPolicyViolation, "invalid relay key")
			return false, false
		}
		if err := s.db.BindAccountDevice(s.ctx, acc, deviceID, "host", stringField(message, "name"), now); err != nil {
			if errors.Is(err, ErrHostQuotaExceeded) {
				conn.sendError(s.ctx, "host_quota_exceeded", fmt.Sprintf("当前账号最多可绑定 %d 台电脑，可在 /account 升级或解绑旧设备", acc.EffectiveLimits().MaxHosts))
			} else {
				conn.sendError(s.ctx, "device_bind_failed", "绑定电脑设备失败")
			}
			conn.close(websocket.StatusPolicyViolation, "host quota exceeded")
			return false, false
		}
		conn.setAccount(acc.ID, "host")
		return true, true
	}

	// No Relay Key presented: check if this device is already a paired phone/client
	// under an active account.
	acc, role, found, err := s.db.GetDeviceAccount(s.ctx, deviceID)
	if err == nil && found {
		if acc.Status != AccountStatusActive {
			conn.sendError(s.ctx, "account_suspended", "所属官方中继账号已被停用")
			conn.close(websocket.StatusPolicyViolation, "account suspended")
			return false, false
		}
		pairCount, _ := s.db.CountPairingsForDevice(s.ctx, deviceID)
		if role == "client" && pairCount > 0 {
			conn.setAccount(acc.ID, "client")
			return true, true
		}
	}

	// Unbound device without a Relay Key: allow hello ONLY so a brand-new phone
	// can immediately call pair.join with a valid 5-minute QR token / 6-digit code.
	// Close it once the pairing window has passed so anonymous sockets cannot
	// sit on the relay indefinitely.
	conn.setAccount("", "")
	time.AfterFunc(unboundSocketTTL, func() {
		if id, _ := conn.account(); id == "" && !conn.closed.Load() {
			conn.sendError(s.ctx, "account_required", "该设备尚未绑定官方中继账号：电脑端请填写 Relay Key，手机请扫电脑上的配对二维码")
			conn.close(websocket.StatusPolicyViolation, "account required")
		}
	})
	return true, false
}

func (s *Server) checkManagedPairStart(conn *clientConn) bool {
	if !s.isManagedMode() {
		return true
	}
	accountID, role := conn.account()
	if accountID == "" || role != "host" {
		conn.sendError(s.ctx, "account_required", "使用官方中继请先在电脑端配置 Relay Key（前往中继 /account 页面注册获取）")
		return false
	}
	acc, found, err := s.db.GetAccount(s.ctx, accountID)
	if err != nil || !found || acc.Status != AccountStatusActive {
		conn.sendError(s.ctx, "account_suspended", "官方中继账号不可用")
		return false
	}
	return true
}

func (s *Server) bindManagedPairedClient(hostConn *clientConn, clientID, label string) bool {
	if !s.isManagedMode() {
		return true
	}
	hostAccountID, _ := hostConn.account()
	acc, found, err := s.db.GetAccount(s.ctx, hostAccountID)
	if err != nil || !found || acc.Status != AccountStatusActive {
		hostConn.sendError(s.ctx, "account_suspended", "官方中继账号不可用")
		return false
	}
	if err := s.db.BindAccountDevice(s.ctx, acc, clientID, "client", label, s.now()); err != nil {
		if errors.Is(err, ErrDeviceQuotaExceeded) {
			hostConn.sendError(s.ctx, "device_quota_exceeded", fmt.Sprintf("当前账号最多可配对 %d 台移动设备", acc.EffectiveLimits().MaxDevices))
		} else {
			hostConn.sendError(s.ctx, "device_bind_failed", "绑定移动设备失败")
		}
		return false
	}
	s.mu.Lock()
	targets := make([]*clientConn, 0, len(s.conns[clientID]))
	for c := range s.conns[clientID] {
		targets = append(targets, c)
	}
	s.mu.Unlock()
	for _, c := range targets {
		c.setAccount(acc.ID, "client")
	}
	return true
}

func (s *Server) checkManagedEnvelopeQuota(conn *clientConn, rawSize int64) bool {
	if !s.isManagedMode() {
		return true
	}
	accountID, role := conn.account()
	if accountID == "" {
		conn.sendError(s.ctx, "account_required", "设备尚未关联有效的官方中继账号")
		return false
	}
	now := s.now()
	acc, found, err := s.db.GetAccount(s.ctx, accountID)
	if err != nil || !found || acc.Status != AccountStatusActive {
		conn.sendError(s.ctx, "account_suspended", "官方中继账号已被停用")
		conn.close(websocket.StatusPolicyViolation, "account suspended")
		return false
	}
	limits := s.limitsFor(acc)
	if rawSize > limits.MaxMsgBytes {
		conn.sendError(s.ctx, "payload_too_large_for_tier", fmt.Sprintf("单条中转消息超出当前套餐上限（%d KB），大文件建议局域网直连、自建中继或升级赞助额度", limits.MaxMsgBytes/1024))
		return false
	}
	today, err := s.db.GetAccountTrafficToday(s.ctx, acc.ID, now)
	if err == nil && today.BytesRelayed+rawSize > limits.DailyQuotaBytes {
		conn.sendError(s.ctx, "daily_quota_exceeded", fmt.Sprintf("今日官方中继流量额度（%d MB）已用尽，UTC 次日自动重置", limits.DailyQuotaBytes/(1024*1024)))
		return false
	}
	// Only phone-originated traffic counts as remote usage time. The desktop's
	// pushes and live updates keep flowing so notifications never stop.
	if role == "client" && !s.consumeWeeklyMinute(conn, acc, limits.WeeklyActiveMinutes, now) {
		return false
	}
	_, _ = s.db.RecordAccountTraffic(s.ctx, acc.ID, rawSize, now)
	return true
}
