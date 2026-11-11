package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/api"
	"github.com/tomato-hug/homing/internal/driver"
)

// EventSink receives the events custom agents push to /api/ingest. The store and
// notify packages are not wired yet, so by default events are only logged; the
// owner of those packages sets this from their init() or serve setup.
var (
	sinkMu    sync.RWMutex
	EventSink func(agent.Event)
)

// SetEventSink installs the receiver for ingested events.
func SetEventSink(fn func(agent.Event)) {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	EventSink = fn
}

func emitEvent(ev agent.Event) {
	sinkMu.RLock()
	fn := EventSink
	sinkMu.RUnlock()
	if fn != nil {
		fn(ev)
	}
}

// ---- 运行时状态 -----------------------------------------------------------

var (
	runtimeMu  sync.Mutex
	runtimeAt  time.Time
	manifests  []*ManifestAgent
	manErrs    []ManifestError
	scripts    []*ScriptAgent
	scrErrs    []ManifestError
	ingest     *IngestStore
	ingestOnce sync.Once
)

// refresh reloads manifests and scripts at most every 15 seconds.
func refresh() {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if time.Since(runtimeAt) < 15*time.Second && (manifests != nil || scripts != nil) {
		return
	}
	manifests, manErrs = LoadManifests(ManifestDir())
	scripts, scrErrs = LoadScripts(ScriptDir())
	syncDriveSpecs(manifests)
	runtimeAt = time.Now()
}

func syncDriveSpecs(manifests []*ManifestAgent) {
	specs := make([]driver.Spec, 0)
	for _, agent := range manifests {
		manifest := agent.Manifest()
		if manifest == nil || manifest.Drive == nil {
			continue
		}
		if _, builtin := driver.BuiltinSpecFor(manifest.ID); builtin {
			// The catalog can persist enabled state for an id that already has
			// a verified built-in ACP command. The manifest still makes the
			// capability visible, but the built-in command remains authoritative.
			continue
		}
		dispatch := true
		if manifest.Drive.Dispatch != nil {
			dispatch = *manifest.Drive.Dispatch
		}
		specs = append(specs, driver.Spec{
			ID:        manifest.ID,
			Label:     firstNonEmptyValue(manifest.Name, manifest.ID),
			Command:   append([]string(nil), manifest.Drive.Command...),
			Env:       manifest.Drive.Env,
			Dispatch:  dispatch,
			Verified:  true,
			Check:     append([]string(nil), manifest.Drive.Check...),
			CheckFail: append([]string(nil), manifest.Drive.CheckFail...),
			Notes:     "由 " + manifest.SourceFile + " 的 drive 段啟用",
		})
	}
	if err := driver.ReplaceDynamicSpecs(specs); err != nil {
		manErrs = append(manErrs, ManifestError{ID: "drive", Problems: []string{err.Error()}})
	}
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func invalidate() {
	runtimeMu.Lock()
	runtimeAt = time.Time{}
	runtimeMu.Unlock()
}

// CustomAgents returns every custom agent (manifests first, then scripts).
func CustomAgents(ctx context.Context) []agent.Agent {
	refresh()
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	out := make([]agent.Agent, 0, len(manifests)+len(scripts))
	for _, m := range manifests {
		out = append(out, m)
	}
	for _, s := range scripts {
		out = append(out, s)
	}
	return out
}

// Errors reports manifests and scripts that could not be loaded.
func Errors() []ManifestError {
	refresh()
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	out := append([]ManifestError(nil), manErrs...)
	return append(out, scrErrs...)
}

// ---- 路由 ---------------------------------------------------------------

func init() {
	agent.RegisterProvider("adapter", func(ctx context.Context) []agent.Agent {
		return CustomAgents(ctx)
	})
	api.Register("adapter", 50, mount)
}

func mount(mux *http.ServeMux, d *api.Deps) {
	ingestOnce.Do(func() {
		ingest = NewIngestStore(d.DataDir)
	})
	registerIngestReplyTransport()
	mountCatalogRoutes(mux, d)

	// GET /api/adapters
	mux.HandleFunc("GET /api/adapters", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		api.WriteJSON(w, http.StatusOK, consoleAdapterPayload(d))
	})

	// POST /api/adapters —— 保存清单
	mux.HandleFunc("POST /api/adapters", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		body, err := readJSONBody(r)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		yamlText, err := manifestYAMLFromRequest(body)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		m, problems, err := LoadManifestFromString(yamlText)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if len(problems) > 0 {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "清单校验未通过", "errors": problems})
			return
		}
		dir := ManifestDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		file := filepath.Join(dir, m.ID+".yaml")
		var backup string
		if _, err := os.Stat(file); err == nil {
			backup = fmt.Sprintf("%s.bak-%d", file, time.Now().Unix())
			_ = os.WriteFile(backup, mustRead(file), 0o600)
		}
		if err := os.WriteFile(file, []byte(yamlText), 0o600); err != nil {
			api.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		invalidate()
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"ok": true, "id": m.ID, "file": file, "backup": backup,
		})
	})

	// POST /api/adapters/test —— 试读
	mux.HandleFunc("POST /api/adapters/test", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		body, err := readJSONBody(r)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		yamlText, _ := body["yaml"].(string)
		if strings.TrimSpace(yamlText) == "" {
			path, _ := body["path"].(string)
			format, _ := body["format"].(string)
			if format == "" {
				format = "jsonl"
			}
			api.WriteJSON(w, http.StatusOK, adapterTestRead(path, format))
			return
		}
		m, problems, err := LoadManifestFromString(yamlText)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if len(problems) > 0 {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "清单校验未通过", "errors": problems})
			return
		}
		ma := NewManifestAgent(m)
		det := ma.Detect(r.Context())
		sessions, _ := ma.ListSessions(r.Context(), 5)
		models, def, _ := ma.Models(r.Context())
		api.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":            true,
			"id":            ma.ID(),
			"detect":        det,
			"session_count": len(sessions),
			"sessions":      sessions,
			"models":        len(models),
			"default_model": def,
			"reply":         m.Reply != nil,
		})
	})

	// DELETE /api/adapters/{id}
	mux.HandleFunc("DELETE /api/adapters/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		id := r.PathValue("id")
		if !validID(id) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "adapter id 不合法"})
			return
		}
		var removed []map[string]string
		for _, dir := range []string{ManifestDir(), ScriptDir()} {
			for _, name := range []string{id + ".yaml", id + ".yml", id + ".mjs"} {
				file := filepath.Join(dir, name)
				if _, err := os.Stat(file); err != nil {
					continue
				}
				trash := fmt.Sprintf("%s.deleted-%d", file, time.Now().Unix())
				if os.Rename(file, trash) == nil {
					removed = append(removed, map[string]string{"file": file, "trash": trash})
				}
			}
		}
		if len(removed) == 0 {
			api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "没有 id 为 " + id + " 的适配器"})
			return
		}
		invalidate()
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": removed})
	})

	// POST /api/ingest
	mux.HandleFunc("POST /api/ingest", func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r) {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "只接受 127.0.0.1 来源"})
			return
		}
		token := bearer(r)
		rec := ingest.Lookup(token)
		if rec == nil {
			api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "令牌无效或已吊销"})
			return
		}
		var body IngestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "body 不是 JSON"})
			return
		}
		if body.Agent == "" {
			body.Agent = rec.Agent
		}
		if body.Agent != rec.Agent {
			api.WriteJSON(w, http.StatusForbidden, map[string]any{"error": "令牌与 agent 不匹配"})
			return
		}
		if body.SessionID == "" {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少 session_id"})
			return
		}
		if body.Kind == "" {
			body.Kind = "message"
		}
		if !IngestKinds[body.Kind] {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "kind 不在允许的取值里"})
			return
		}
		ev := agent.Event{
			Source:    body.Agent,
			SessionID: body.SessionID,
			Kind:      agent.CanonicalEventKind(body.Kind),
			Title:     body.Title,
			Text:      body.Text,
			At:        time.Now().UTC(),
		}
		if body.Cwd != "" {
			ev.Data = map[string]any{"cwd": body.Cwd}
		}
		emitEvent(ev)
		ingest.Touch(body.Agent)
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// POST /api/ingest/tokens
	mux.HandleFunc("POST /api/ingest/tokens", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		body, err := readJSONBody(r)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		agentID, _ := body["agent"].(string)
		replyURL, _ := body["reply_url"].(string)
		timeout := 10000
		if v, ok := body["reply_timeout_ms"].(float64); ok && v > 0 {
			timeout = int(v)
		}
		token, err := ingest.Issue(strings.TrimSpace(agentID), replyURL, timeout)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "agent": agentID, "token": token})
	})

	// DELETE /api/ingest/tokens
	mux.HandleFunc("DELETE /api/ingest/tokens", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) {
			return
		}
		body, err := readJSONBody(r)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		key := firstNonEmpty(strAny(body["token"]), strAny(body["agent"]))
		if key == "" {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "需要 token 或 agent"})
			return
		}
		if !ingest.Revoke(key) {
			api.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "没有匹配的令牌"})
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

// ---- 辅助 ---------------------------------------------------------------

func adapterAuthorized(d *api.Deps, w http.ResponseWriter, r *http.Request) bool {
	if d.Authorized(r) {
		return true
	}
	api.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
	return false
}

// LoadManifestFromString validates YAML text without touching disk (used by
// POST /api/adapters and POST /api/adapters/test, and by `homing agents test`).
func LoadManifestFromString(yamlText string) (*Manifest, []string, error) {
	var m Manifest
	if err := yamlUnmarshal([]byte(yamlText), &m); err != nil {
		return nil, nil, fmt.Errorf("YAML 解析失败: %w", err)
	}
	if problems := m.Validate(); len(problems) > 0 {
		return &m, problems, nil
	}
	return &m, nil, nil
}

func readJSONBody(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("body 不是 JSON")
	}
	return out, nil
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// isLoopback keeps /api/ingest local: the phone talks to /api/*, but only the
// agent on this machine may push events.
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func mustRead(path string) []byte {
	raw, _ := os.ReadFile(path)
	return raw
}

func strAny(v any) string {
	s, _ := v.(string)
	return s
}
