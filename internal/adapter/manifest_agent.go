package adapter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
	"github.com/tomato-hug/homing/internal/driver"
	"github.com/tomato-hug/homing/internal/platform"
	_ "modernc.org/sqlite"
)

// ManifestAgent is a manifest turned into an agent.Agent. It keeps the same
// behaviour as the Node implementation: files are globbed and cached briefly, the
// format reader does the parsing, and reply runs a command array without a shell.
type ManifestAgent struct {
	m *Manifest

	mu    sync.Mutex
	files []string
	at    time.Time
	rows  []FormatSession
}

// NewManifestAgent wraps a validated manifest.
func NewManifestAgent(m *Manifest) *ManifestAgent {
	return &ManifestAgent{m: m}
}

func (a *ManifestAgent) ID() string { return a.m.ID }
func (a *ManifestAgent) Manifest() *Manifest {
	return a.m
}
func (a *ManifestAgent) Name() string {
	if a.m.Name != "" {
		return a.m.Name
	}
	return a.m.ID
}
func (a *ManifestAgent) Icon() string {
	if a.m.Icon != "" {
		return a.m.Icon
	}
	return "generic"
}

func (a *ManifestAgent) PushSignal() agent.PushSignal {
	switch a.m.Push {
	case "watch":
		return agent.PushPoll // 文件监听尚未接，按轮询上报更老实
	case "hook":
		return agent.PushHook
	}
	return agent.PushPoll
}

func (a *ManifestAgent) Capabilities() agent.Capabilities {
	drive := a.m.Drive != nil
	dispatch := drive
	if drive && a.m.Drive.Dispatch != nil {
		dispatch = *a.m.Drive.Dispatch
	}
	return agent.Capabilities{
		Reply:      a.m.Reply != nil,
		Sessions:   a.m.Sessions.Source != "",
		Models:     a.m.Models != nil,
		NewSession: drive,
		Stop:       drive,
		Approve:    drive,
		Live:       drive,
		Dispatch:   dispatch,
	}
}

// StartSession delegates to the ACP driver registered from this manifest's
// drive block. The manifest remains data: no per-agent Go switch is needed.
func (a *ManifestAgent) StartSession(ctx context.Context, req agent.StartRequest) (agent.StartResult, error) {
	if a.m.Drive == nil {
		return agent.StartResult{}, agent.ErrUnsupported
	}
	sessionID, err := driver.Default().Start(ctx, a.m.ID, req.Cwd, req.Text, nil)
	if err != nil {
		return agent.StartResult{}, err
	}
	return agent.StartResult{SessionID: sessionID, Status: "started"}, nil
}

// StopSession stops only sessions owned by the ACP manager.
func (a *ManifestAgent) StopSession(_ context.Context, sessionID string) error {
	if driver.Default().Stop(sessionID) {
		return nil
	}
	return agent.ErrUnsupported
}

// ---- 文件与缓存 ----------------------------------------------------------

func (a *ManifestAgent) glob() []string {
	pattern := a.m.Sessions.Glob
	if pattern == "" {
		if a.m.Sessions.Dir == "" {
			return nil
		}
		pattern = filepath.Join(expandHome(a.m.Sessions.Dir), "*")
	}
	matches, err := filepath.Glob(expandHome(pattern))
	if err != nil {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool {
		si, ei := os.Stat(matches[i])
		sj, ej := os.Stat(matches[j])
		if ei != nil || ej != nil {
			return i < j
		}
		return si.ModTime().After(sj.ModTime())
	})
	return matches
}

func (a *ManifestAgent) filesCached() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Since(a.at) < 20*time.Second && a.files != nil {
		return a.files
	}
	a.files = a.glob()
	a.at = time.Now()
	a.rows = nil
	return a.files
}

func (a *ManifestAgent) format() Format {
	f, err := FormatFor(a.m.Sessions.Format, a.m.Sessions.FormatOptions)
	if err != nil {
		return nil
	}
	return f
}

// rowsCached parses every file once per cache window; list, meta and items all
// read from the same result so a phone browsing sessions does not rescan.
func (a *ManifestAgent) rowsCached() []FormatSession {
	a.mu.Lock()
	if a.rows != nil && time.Since(a.at) < 20*time.Second {
		rows := a.rows
		a.mu.Unlock()
		return rows
	}
	a.mu.Unlock()

	files := a.filesCached()
	f := a.format()
	var rows []FormatSession
	if f != nil {
		rows, _ = f.ListFiles(files)
	}
	a.mu.Lock()
	a.rows = rows
	a.mu.Unlock()
	return rows
}

func (a *ManifestAgent) find(sessionID string) *FormatSession {
	for _, r := range a.rowsCached() {
		if r.SessionID == sessionID {
			rr := r
			return &rr
		}
	}
	return nil
}

// ---- agent.Agent ---------------------------------------------------------

func (a *ManifestAgent) Detect(ctx context.Context) agent.Detection {
	var bits []string
	installed := false
	for _, p := range a.m.Detect.Paths {
		if _, err := os.Stat(expandHome(p)); err == nil {
			installed = true
			bits = append(bits, expandHome(p))
		}
	}
	if !installed {
		for _, bin := range a.m.Detect.Bin {
			if p := platform.ResolveBinary(bin); p != "" {
				installed = true
				bits = append(bits, p)
				break
			}
		}
	}
	if !installed && a.m.Sessions.Glob != "" {
		if n := len(a.filesCached()); n > 0 {
			installed = true
			bits = append(bits, fmt.Sprintf("%d files", n))
		}
	}
	if !installed && a.m.Drive != nil {
		for _, bin := range a.m.Drive.Command {
			if strings.TrimSpace(bin) == "" {
				continue
			}
			if path := platform.ResolveBinary(bin); path != "" {
				installed = true
				bits = append(bits, path)
			}
			// Only the executable is looked up; arguments are not commands.
			break
		}
	}
	d := agent.Detection{Installed: installed}
	if len(bits) > 0 {
		d.Detail = strings.Join(bits, " · ")
	}
	return d
}

func (a *ManifestAgent) ListSessions(ctx context.Context, limit int) ([]agent.Session, error) {
	if limit <= 0 {
		limit = 200
	}
	if a.m.Sessions.Source == "" {
		return nil, nil
	}
	if a.m.Sessions.Source == "sqlite" {
		return a.sqliteSessions(limit)
	}
	rows := a.rowsCached()
	sort.Slice(rows, func(i, j int) bool { return rows[i].LastTS > rows[j].LastTS })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]agent.Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, agent.Session{
			SessionID:   r.SessionID,
			Source:      a.m.ID,
			Title:       r.Title,
			TitleSource: r.TitleSource,
			LastTS:      r.LastTS,
			Cwd:         r.Cwd,
			Model:       r.Model,
		})
	}
	return out, nil
}

func (a *ManifestAgent) Meta(sessionID, cwdHint string) agent.Meta {
	if a.m.Sessions.Source == "sqlite" {
		rows, _ := a.sqliteSessions(500)
		for _, row := range rows {
			if row.SessionID == sessionID {
				cwd := cwdHint
				if cwd == "" {
					cwd = row.Cwd
				}
				return agent.Meta{
					Cwd:         cwd,
					NativeTitle: row.Title,
					TitleSource: "agent",
					Model:       row.Model,
				}
			}
		}
		return agent.Meta{Cwd: cwdHint, TitleSource: "agent"}
	}
	r := a.find(sessionID)
	if r == nil {
		return agent.Meta{Cwd: cwdHint, TitleSource: "first_prompt"}
	}
	cwd := cwdHint
	if cwd == "" {
		cwd = r.Cwd
	}
	return agent.Meta{
		Cwd:         cwd,
		NativeTitle: r.Title,
		TitleSource: r.TitleSource,
		Branch:      r.Branch,
		Model:       r.Model,
	}
}

func (a *ManifestAgent) ReadItems(ctx context.Context, sessionID string, limit int) ([]agent.Item, error) {
	if a.m.Sessions.Source == "sqlite" {
		return a.sqliteItems(sessionID, limit), nil
	}
	r := a.find(sessionID)
	if r == nil {
		return nil, nil
	}
	f := a.format()
	if f == nil {
		return nil, nil
	}
	items, err := f.ReadItems(r.Path, limit)
	if err != nil {
		return nil, err
	}
	out := make([]agent.Item, 0, len(items))
	for i, it := range items {
		out = append(out, agent.Item{ID: fmt.Sprintf("%s-%d", sessionID, i), Role: it.Role, Type: it.Kind, Seq: i, TS: it.At, Text: it.Text})
	}
	return out, nil
}

func (a *ManifestAgent) IsEmptySession(sessionID string) bool {
	if a.m.Sessions.Source == "sqlite" {
		if strings.TrimSpace(a.m.Sessions.ItemsQuery) != "" {
			return len(a.sqliteItems(sessionID, 1)) == 0
		}
		return false
	}
	r := a.find(sessionID)
	if r == nil {
		return false
	}
	f := a.format()
	if f == nil {
		return false
	}
	return !f.HasConversation(r.Path)
}

func (a *ManifestAgent) Models(ctx context.Context) ([]agent.Model, string, error) {
	if a.m.Models == nil || a.m.Models.File == "" {
		return nil, "", nil
	}
	return readModelsFile(expandHome(a.m.Models.File))
}

// ---- sqlite --------------------------------------------------------------

// openDB opens SQLite in read-only mode WITHOUT `immutable=1` first so WAL-mode
// databases (e.g. Hermes state.db-wal) remain fully visible; falls back to
// `immutable=1` only when the parent directory does not permit WAL/SHM reads.
func (a *ManifestAgent) openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(2000)")
	if err == nil {
		db.SetMaxOpenConns(1)
		if pingErr := db.Ping(); pingErr == nil {
			return db, nil
		}
		_ = db.Close()
	}
	return sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
}

func (a *ManifestAgent) sqliteItems(sessionID string, limit int) []agent.Item {
	query := strings.TrimSpace(a.m.Sessions.ItemsQuery)
	if query == "" || sessionID == "" {
		return nil
	}
	if limit <= 0 {
		limit = 100
	}
	var paths []string
	if a.m.Sessions.DBGlob != "" {
		matches, _ := filepath.Glob(expandHome(a.m.Sessions.DBGlob))
		paths = matches
	} else if a.m.Sessions.DB != "" {
		p := expandHome(a.m.Sessions.DB)
		if _, err := os.Stat(p); err == nil {
			paths = []string{p}
		}
	}
	for _, dbPath := range paths {
		db, err := a.openDB(dbPath)
		if err != nil {
			continue
		}
		qCount := strings.Count(query, "?")
		var args []any
		switch {
		case qCount >= 2:
			args = []any{sessionID, limit}
		case qCount == 1:
			args = []any{sessionID}
		}
		rows, err := db.Query(query, args...)
		if err != nil {
			_ = db.Close()
			continue
		}
		cols, cErr := rows.Columns()
		if cErr != nil {
			rows.Close()
			_ = db.Close()
			continue
		}
		var out []agent.Item
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if rows.Scan(ptrs...) != nil {
				continue
			}
			get := func(name string) string {
				for i, c := range cols {
					if strings.EqualFold(c, name) {
						return dbValue(vals[i])
					}
				}
				return ""
			}
			role := strings.ToLower(firstNonEmpty(get("role"), get("type"), "assistant"))
			text := firstNonEmpty(get("text"), get("content"))
			if strings.TrimSpace(text) == "" {
				continue
			}
			itemID := firstNonEmpty(get("id"), fmt.Sprintf("%s-%d", sessionID, len(out)+1))
			out = append(out, agent.Item{
				ID:   itemID,
				Role: role,
				Type: role,
				Seq:  len(out) + 1,
				TS:   isoFromAny(firstNonEmpty(get("ts"), get("timestamp"), get("created_at"))),
				Text: text,
			})
			if len(out) >= limit {
				break
			}
		}
		rows.Close()
		_ = db.Close()
		if len(out) > 0 {
			if strings.Contains(strings.ToLower(query), "desc") {
				for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
					out[i], out[j] = out[j], out[i]
				}
				for i := range out {
					out[i].Seq = i + 1
				}
			}
			return out
		}
	}
	return nil
}

func (a *ManifestAgent) sqliteSessions(limit int) ([]agent.Session, error) {
	m := &a.m.Sessions
	var paths []string
	if m.DBGlob != "" {
		matches, err := filepath.Glob(expandHome(m.DBGlob))
		if err != nil {
			return nil, nil
		}
		sort.Strings(matches)
		paths = matches
	} else if m.DB != "" {
		p := expandHome(m.DB)
		if _, err := os.Stat(p); err != nil {
			return nil, nil
		}
		paths = []string{p}
	}
	var out []agent.Session
	for _, p := range paths {
		out = append(out, a.queryOne(p, limit)...)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (a *ManifestAgent) queryOne(dbPath string, limit int) []agent.Session {
	db, err := a.openDB(dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(a.m.Sessions.Query)
	if err != nil {
		return nil
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil
	}
	var out []agent.Session
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if rows.Scan(ptrs...) != nil {
			continue
		}
		get := func(name string) string {
			for i, c := range cols {
				if strings.EqualFold(c, name) {
					return dbValue(vals[i])
				}
			}
			return ""
		}
		id := firstNonEmpty(get("session_id"), get("id"))
		if id == "" && a.m.Sessions.DBGlob != "" {
			id = baseWithoutExt(filepath.Base(dbPath), ".db")
		}
		if id == "" {
			continue
		}
		out = append(out, agent.Session{
			SessionID:   id,
			Source:      a.m.ID,
			Title:       get("title"),
			TitleSource: "agent",
			LastTS:      isoFromAny(get("last_ts")),
			Cwd:         get("cwd"),
			Model:       get("model"),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func dbValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		return fmt.Sprintf("%v", t)
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("%v", v)
}

// ---- reply --------------------------------------------------------------

// Reply runs the manifest's command array. No shell is involved: each element is
// one argv entry, and a placeholder that is the whole argument becomes one value.
func (a *ManifestAgent) Reply(ctx context.Context, sessionID, text string, opts agent.ReplyOpts) error {
	if a.m.Reply == nil || len(a.m.Reply.Command) == 0 {
		return agent.ErrUnsupported
	}
	r := a.find(sessionID)
	cwd := ""
	if r != nil {
		cwd = r.Cwd
	}
	vars := map[string]string{
		"session_id": sessionID,
		"text":       text,
		"cwd":        cwd,
		"title":      titleOf(r),
	}
	args := make([]string, 0, len(a.m.Reply.Command))
	for _, a0 := range a.m.Reply.Command {
		v, err := substitute(a0, vars)
		if err != nil {
			return err
		}
		args = append(args, v)
	}
	timeout := a.m.Reply.TimeoutMS
	if timeout <= 0 {
		timeout = 30000
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	binPath := platform.ResolveBinary(args[0])
	if binPath == "" {
		binPath = args[0]
	}
	cmd := exec.CommandContext(cctx, binPath, args[1:]...)
	cmd.Env = platform.EnvWithAugmentedPATH(nil)
	if wd, err := substitute(a.m.Reply.Cwd, vars); err == nil && wd != "" {
		if st, serr := os.Stat(wd); serr == nil && st.IsDir() {
			cmd.Dir = wd
		}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("回复命令失败: %s", tailString(stderr.String(), 400))
		}
		return fmt.Errorf("回复命令失败: %w", err)
	}
	return nil
}

func titleOf(r *FormatSession) string {
	if r == nil {
		return ""
	}
	return r.Title
}

// substitute replaces {name}. A whole-argument placeholder becomes exactly one
// argument; an embedded one is plain text. Either way no shell parses it.
func substitute(arg string, vars map[string]string) (string, error) {
	trimmed := strings.TrimSpace(arg)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") && strings.Count(trimmed, "{") == 1 {
		key := trimmed[1 : len(trimmed)-1]
		v, ok := vars[key]
		if !ok {
			return "", fmt.Errorf("未知占位符 {%s}", key)
		}
		return v, nil
	}
	out := arg
	for _, k := range []string{"session_id", "text", "cwd", "title"} {
		if v, ok := vars[k]; ok {
			out = strings.ReplaceAll(out, "{"+k+"}", v)
		}
	}
	return out, nil
}

func tailString(s string, n int) string {
	if len(s) <= n {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(s[len(s)-n:])
}

// jsonUnmarshalLoose 解析可能带 BOM 或尾随空白的 JSON 配置。
func jsonUnmarshalLoose(raw []byte, v any) error {
	return json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(raw, []byte("\ufeff"))), v)
}

// ---- models --------------------------------------------------------------

func readModelsFile(path string) ([]agent.Model, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	var doc struct {
		Model    string `json:"model"`
		Provider map[string]struct {
			Models map[string]any `json:"models"`
		} `json:"provider"`
	}
	if err := jsonUnmarshalLoose(raw, &doc); err != nil {
		return nil, "", err
	}
	var out []agent.Model
	for pid, p := range doc.Provider {
		for mid := range p.Models {
			out = append(out, agent.Model{ID: pid + "/" + mid})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, doc.Model, nil
}
