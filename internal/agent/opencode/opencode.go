// Package opencode reads OpenCode sessions.
//
// Source 1 (always available): ~/.local/share/opencode/opencode.db ($OPENCODE_DB
// overrides), read-only — see internal/agent/opencodedb.
// Source 2 (optional): the `opencode serve` HTTP API (default 127.0.0.1:4096,
// HTTP Basic auth). With serve reachable the push signal becomes http_events,
// replies go to POST /session/:id/prompt_async and models come from
// GET /config/providers; without it we fall back to polling the database and
// reply is unsupported.
//
// Ported from relay/scanners/opencode.mjs.
//
// Owner: WorkBuddy.
package opencode

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/agent/opencodedb"
	"github.com/tomato-hug/homing/internal/platform"
)

const id = "opencode"

// Icon is the monochrome 16×16 glyph shared with the console.
const Icon = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"><path d="M2.4 3.2h6.2l5 5v4.6H7.4l-5-5z"/><path d="M6.4 7.6 9 10.2"/></svg>`

// ServeTTL is how long a serve probe stays fresh.
const ServeTTL = 15 * time.Second

func init() { agent.Register(Agent{}) }

// Agent implements agent.Agent plus ItemReader, EmptyChecker, Replier,
// ModelLister and Watcher.
type Agent struct{}

func (Agent) ID() string   { return id }
func (Agent) Name() string { return "OpenCode" }
func (Agent) Icon() string { return Icon }
func (Agent) PushSignal() agent.PushSignal {
	if serveURL() != "" {
		return agent.PushHTTPEvents
	}
	return agent.PushPoll
}

func (Agent) Capabilities() agent.Capabilities {
	return agent.Capabilities{Reply: serveURL() != "", Sessions: true, Models: true}
}

func (Agent) Meta(sessionID, cwdHint string) agent.Meta { return metaFor(sessionID, cwdHint) }

// ---- paths ---------------------------------------------------------------

// DataDir is where OpenCode keeps its database.
func DataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode")
	}
	return filepath.Join(platform.Home(), ".local", "share", "opencode")
}

// DBPath resolves $OPENCODE_DB, then the default location.
func DBPath() string {
	base := DataDir()
	if override := os.Getenv("OPENCODE_DB"); override != "" && override != ":memory:" {
		if filepath.IsAbs(override) {
			return override
		}
		return filepath.Join(base, override)
	}
	return filepath.Join(base, "opencode.db")
}

func configPath() string {
	base := filepath.Join(platform.Home(), ".config")
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		base = xdg
	}
	return filepath.Join(base, "opencode", "opencode.json")
}

// servicePassword is the Basic-auth password OpenCode stores for `serve`.
func servicePassword() string {
	var payload struct {
		Password string `json:"password"`
	}
	if platform.ReadJSON(filepath.Join(filepath.Dir(configPath()), "service.json"), &payload) {
		return payload.Password
	}
	return ""
}

// ---- serve probe ---------------------------------------------------------

var serveState = struct {
	mu      sync.Mutex
	url     string
	version string
	at      time.Time
}{}

func serveURL() string {
	serveState.mu.Lock()
	defer serveState.mu.Unlock()
	return serveState.url
}

func serveCandidates() []string {
	var out []string
	if url := os.Getenv("OPENCODE_SERVER_URL"); url != "" {
		out = append(out, strings.TrimRight(url, "/"))
	}
	return append(out, "http://127.0.0.1:4096")
}

func basicAuth() string {
	password := os.Getenv("OPENCODE_SERVER_PASSWORD")
	if password == "" {
		password = servicePassword()
	}
	if password == "" {
		return ""
	}
	user := os.Getenv("OPENCODE_SERVER_USERNAME")
	if user == "" {
		user = "opencode"
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

var client = &http.Client{Timeout: 2 * time.Second}

func probeServe(force bool) string {
	serveState.mu.Lock()
	if !force && time.Since(serveState.at) < ServeTTL {
		url := serveState.url
		serveState.mu.Unlock()
		return url
	}
	serveState.at = time.Now()
	serveState.url, serveState.version = "", ""
	serveState.mu.Unlock()

	for _, base := range serveCandidates() {
		req, err := http.NewRequest(http.MethodGet, base+"/global/health", nil)
		if err != nil {
			continue
		}
		if header := basicAuth(); header != "" {
			req.Header.Set("Authorization", header)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		resp, err := client.Do(req.WithContext(ctx))
		if err != nil {
			cancel()
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		cancel()
		if resp.StatusCode/100 != 2 {
			continue
		}
		var health struct {
			Version string `json:"version"`
		}
		_ = json.Unmarshal(body, &health)
		serveState.mu.Lock()
		serveState.url, serveState.version = base, health.Version
		serveState.mu.Unlock()
		return base
	}
	return ""
}

// ---- detection -----------------------------------------------------------

// Detect reports the database and, when present, the running serve endpoint.
func (Agent) Detect(context.Context) agent.Detection {
	path := DBPath()
	installed := fileExists(path)
	ok, sessions := false, 0
	if installed {
		ok, sessions = opencodedb.Probe(path)
	}
	served := probeServe(false) != ""
	det := agent.Detection{Installed: installed || served}
	if version := serveVersion(); version != "" {
		det.Version = &version
	}
	var bits []string
	if ok {
		bits = append(bits, fmt.Sprintf("%d sessions", sessions))
	}
	if served {
		bits = append(bits, "serve "+serveURL())
	}
	det.Detail = strings.Join(bits, " · ")
	return det
}

func serveVersion() string {
	serveState.mu.Lock()
	defer serveState.mu.Unlock()
	return serveState.version
}

// ---- sessions ------------------------------------------------------------

func metaFor(sessionID, cwdHint string) agent.Meta {
	out := agent.Meta{Cwd: cwdHint, TitleSource: "agent"}
	row, ok := opencodedb.OneSession(DBPath(), sessionID)
	if !ok {
		return out
	}
	out.Cwd = firstNonEmpty(out.Cwd, row.Cwd)
	out.NativeTitle = row.Title
	out.Model = row.Model
	return out
}

// ListSessions returns the sessions in the database, newest first.
func (Agent) ListSessions(_ context.Context, limit int) ([]agent.Session, error) {
	path := DBPath()
	if !fileExists(path) {
		return nil, nil
	}
	rows := opencodedb.ListSessions(path, limit)
	out := make([]agent.Session, 0, len(rows))
	for _, r := range rows {
		session := agent.Session{
			SessionID:   r.SessionID,
			Source:      id,
			Title:       r.Title,
			TitleSource: "agent",
			LastTS:      r.LastTS,
			Cwd:         r.Cwd,
			Model:       r.Model,
		}
		if r.ArchivedNative {
			session.NativeState = "archived"
		}
		out = append(out, session)
	}
	return out, nil
}

// ReadItems returns the last entries of one session transcript.
func (Agent) ReadItems(_ context.Context, sessionID string, limit int) ([]agent.Item, error) {
	path := DBPath()
	if !fileExists(path) {
		return nil, nil
	}
	return opencodedb.ReadItems(path, sessionID, limit), nil
}

// IsEmptySession reports a session with no user or assistant entries.
func (Agent) IsEmptySession(sessionID string) bool {
	path := DBPath()
	if !fileExists(path) {
		return false
	}
	return !opencodedb.HasConversation(path, sessionID)
}

// ---- reply injection -----------------------------------------------------

// Reply posts the message to the running serve; it only works while serve is up.
// It uses prompt_async because the synchronous endpoint waits for the model.
func (Agent) Reply(ctx context.Context, sessionID, text string, opts agent.ReplyOpts) error {
	base := probeServe(false)
	if base == "" {
		return errors.New("opencode serve 未运行，无法注入（请先 `opencode serve`）")
	}
	body := map[string]any{"parts": []map[string]any{{"type": "text", "text": text}}}
	if opts.Model != "" {
		if provider, rest, ok := strings.Cut(opts.Model, "/"); ok && provider != "" && rest != "" {
			body["model"] = map[string]string{"providerID": provider, "modelID": rest}
		}
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/session/"+urlPathEscape(sessionID)+"/prompt_async", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if header := basicAuth(); header != "" {
		req.Header.Set("Authorization", header)
	}
	callClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := callClient.Do(req)
	if err != nil {
		return fmt.Errorf("注入失败: %w", err)
	}
	defer resp.Body.Close()
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode/100 == 2 {
		return nil
	}
	message := strings.TrimSpace(string(detail))
	if len(message) > 200 {
		message = message[:200]
	}
	if message != "" {
		return fmt.Errorf("opencode serve 返回 %d: %s", resp.StatusCode, message)
	}
	return fmt.Errorf("opencode serve 返回 %d", resp.StatusCode)
}

// ---- models --------------------------------------------------------------

// Models prefers the running serve, then the local config plus the models.dev
// catalog cached in the database.
func (Agent) Models(ctx context.Context) ([]agent.Model, string, error) {
	if base := probeServe(false); base != "" {
		if models, def, ok := modelsFromServe(ctx, base); ok && len(models) > 0 {
			return models, def, nil
		}
	}
	models, def := modelsFromConfig(DBPath())
	return models, def, nil
}

func modelsFromServe(ctx context.Context, base string) ([]agent.Model, string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/config/providers", nil)
	if err != nil {
		return nil, "", false
	}
	if header := basicAuth(); header != "" {
		req.Header.Set("Authorization", header)
	}
	callClient := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := callClient.Do(req)
	if err != nil {
		return nil, "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, "", false
	}
	var payload struct {
		Providers []struct {
			ID         string                     `json:"id"`
			ProviderID string                     `json:"providerID"`
			Models     map[string]json.RawMessage `json:"models"`
		} `json:"providers"`
		Default map[string]string `json:"default"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) != nil {
		return nil, "", false
	}
	var out []agent.Model
	for _, p := range payload.Providers {
		provider := p.ID
		if provider == "" {
			provider = p.ProviderID
		}
		for modelID := range p.Models {
			out = append(out, agent.Model{ID: provider + "/" + modelID, Name: modelID})
		}
	}
	def := ""
	for _, v := range payload.Default {
		def = v
		break
	}
	return out, def, true
}

func modelsFromConfig(dbPath string) ([]agent.Model, string) {
	var cfg struct {
		Provider map[string]struct {
			Models map[string]json.RawMessage `json:"models"`
		} `json:"provider"`
		Model      string `json:"model"`
		SmallModel string `json:"small_model"`
	}
	if !platform.ReadJSON(configPath(), &cfg) {
		return nil, ""
	}
	catalog := opencodedb.Catalog(dbPath)
	var out []agent.Model
	for providerID, provider := range cfg.Provider {
		known := make([]string, 0, len(provider.Models))
		for modelID := range provider.Models {
			known = append(known, modelID)
		}
		if len(known) == 0 {
			if entry, ok := catalog[providerID].(map[string]any); ok {
				if models, ok := entry["models"].(map[string]any); ok {
					for modelID := range models {
						known = append(known, modelID)
					}
				}
			}
		}
		for _, modelID := range known {
			out = append(out, agent.Model{ID: providerID + "/" + modelID, Name: modelID})
		}
	}
	def := cfg.Model
	if def == "" {
		def = cfg.SmallModel
	}
	return out, def
}

// ---- event stream --------------------------------------------------------

var classifyPatterns = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`(?i)permission`), "approval"},
	{regexp.MustCompile(`(?i)idle|completed|finish|done`), "done"},
	{regexp.MustCompile(`(?i)error`), "error"},
}

func classify(kind string) string {
	for _, p := range classifyPatterns {
		if p.re.MatchString(kind) {
			return p.kind
		}
	}
	return ""
}

// Watch subscribes to the serve event stream while it is up, re-probing every
// 30 seconds so starting or stopping `opencode serve` is picked up without a
// restart of homing.
func (Agent) Watch(ctx context.Context, emit func(agent.Event)) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var streamOnce sync.Once
	startStream := func(base string) {
		streamOnce.Do(func() { go readStream(streamCtx, base, emit) })
	}
	if base := probeServe(true); base != "" {
		startStream(base)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			base := probeServe(true)
			if base != "" {
				startStream(base)
			}
		}
	}
}

func readStream(ctx context.Context, base string, emit func(agent.Event)) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/event", nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	if header := basicAuth(); header != "" {
		req.Header.Set("Authorization", header)
	}
	streamer := &http.Client{} // no timeout: the stream stays open
	resp, err := streamer.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return
	}
	// SSE frames: "data: {json}" lines separated by a blank line; "[DONE]" ends
	// a frame without content. Anything else (event:, id:, retry:) is skipped.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" || raw == "[DONE]" {
			continue
		}
		if event, ok := eventFromPayload(json.RawMessage(raw)); ok {
			emit(event)
		}
	}
}

func eventFromPayload(raw json.RawMessage) (agent.Event, bool) {
	var payload struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return agent.Event{}, false
	}
	kind := classify(payload.Type)
	if kind == "" {
		if other, _ := payload.Properties["type"].(string); other != "" {
			kind = classify(other)
		}
	}
	if kind == "" {
		return agent.Event{}, false
	}
	props := payload.Properties
	sessionID, _ := props["sessionID"].(string)
	if sessionID == "" {
		sessionID, _ = props["session_id"].(string)
	}
	info, _ := props["info"].(map[string]any)
	if sessionID == "" && info != nil {
		sessionID, _ = info["sessionID"].(string)
	}
	if sessionID == "" {
		return agent.Event{}, false
	}
	event := agent.Event{Source: id, SessionID: sessionID, Kind: kind, At: time.Now()}
	if info != nil {
		event.Title, _ = info["title"].(string)
		event.Data = map[string]any{"cwd": info["directory"]}
	}
	if part, ok := props["part"].(map[string]any); ok {
		event.Text, _ = part["text"].(string)
	}
	if event.Text == "" {
		event.Text, _ = props["message"].(string)
	}
	return event, true
}

// Stats is the opencode row of GET /api/agents (serve / sse / last_event_at).
func Stats() map[string]any {
	return map[string]any{"serve": serveURL()}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

// urlPathEscape escapes one path segment (session ids contain no slashes, but
// they may contain characters that would otherwise be read as path syntax).
func urlPathEscape(value string) string { return url.PathEscape(value) }
