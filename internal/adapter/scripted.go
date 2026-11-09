package adapter

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent"
)

// runnerSource is the child-process entry point, kept inside the binary so a
// script adapter needs nothing but node (or bun) on PATH.
//
//go:embed runner.mjs
var runnerSource []byte

// Runtime is what a script adapter calls back into. The framework picks node, then
// bun; with neither, the adapter is reported unusable rather than silently empty.
type scriptRunner struct {
	mu     sync.Mutex
	path   string // extracted runner.mjs
	bin    string // node or bun
	binErr error
	once   sync.Once
}

var runner = &scriptRunner{}

// runtimeFor finds node, else bun. Done once per process.
func (r *scriptRunner) prepare(dataDir string) (string, error) {
	r.once.Do(func() {
		if p, err := exec.LookPath("node"); err == nil {
			r.bin = p
		} else if p, err := exec.LookPath("bun"); err == nil {
			r.bin = p
		} else {
			r.binErr = errors.New("没有找到 node 或 bun，脚本适配器不可用（装一个即可：brew install node）")
			return
		}
		dir := dataDir
		if dir == "" {
			dir = os.TempDir()
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			r.binErr = fmt.Errorf("无法创建运行目录: %w", err)
			return
		}
		p := filepath.Join(dir, "adapter-runner.mjs")
		if err := os.WriteFile(p, runnerSource, 0o644); err != nil {
			r.binErr = fmt.Errorf("无法写入 runner.mjs: %w", err)
			return
		}
		r.path = p
	})
	return r.bin, r.binErr
}

func (r *scriptRunner) binPath() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bin
}

// call runs one operation in a child process. One JSON line in, one JSON line out.
func (r *scriptRunner) call(ctx context.Context, adapterFile, op string, args map[string]any, timeout time.Duration) (map[string]any, error) {
	if _, err := r.prepare(""); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, err := json.Marshal(map[string]any{"op": op, "args": args})
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')

	argv := []string{
		"--permission",
		"--allow-fs-read=" + adapterFile,
		r.path,
		adapterFile,
	}
	cmd := exec.CommandContext(cctx, r.binPath(), argv...)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "HOMING_ADAPTER_NO_NET=1")

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("适配器执行失败: %s", tailString(msg, 500))
	}
	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out); err != nil {
		return nil, fmt.Errorf("适配器输出不是 JSON: %s", tailString(stdout.String(), 200))
	}
	return out, nil
}

// ScriptAgent is one ~/.homing/adapters/*.mjs wrapped as agent.Agent.
type ScriptAgent struct {
	file string

	mu         sync.Mutex
	meta       map[string]any
	metaErr    error
	loaded     bool
	cacheAt    time.Time
	cacheRows  []agent.Session
	id         string
	name       string
	icon       string
	capability agent.Capabilities
	push       agent.PushSignal
}

// NewScriptAgent wraps one file; meta is fetched lazily.
func NewScriptAgent(file string) *ScriptAgent {
	return &ScriptAgent{file: file, push: agent.PushPoll}
}

func (s *ScriptAgent) loadMeta() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return
	}
	s.loaded = true
	res, err := runner.call(context.Background(), s.file, "meta", nil, 10*time.Second)
	if err != nil {
		s.metaErr = err
		s.id = baseWithoutExt(filepath.Base(s.file), ".mjs")
		s.name = s.id
		s.icon = "generic"
		return
	}
	s.meta = res
	s.id = strField(res, "id")
	if s.id == "" {
		s.id = baseWithoutExt(filepath.Base(s.file), ".mjs")
	}
	s.name = strField(res, "name")
	if s.name == "" {
		s.name = s.id
	}
	s.icon = strField(res, "icon")
	if s.icon == "" {
		s.icon = "generic"
	}
	switch strField(res, "pushSignal") {
	case "hook":
		s.push = agent.PushHook
	case "http_events":
		s.push = agent.PushHTTPEvents
	default:
		s.push = agent.PushPoll
	}
	if caps, ok := res["capabilities"].(map[string]any); ok {
		s.capability = agent.Capabilities{
			Reply:    truthy(caps["reply"]),
			Sessions: caps["sessions"] != false,
			Models:   truthy(caps["models"]),
		}
	} else {
		s.capability = agent.Capabilities{Sessions: true}
	}
}

// MetaError reports why this adapter failed to load (nil when it is fine).
func (s *ScriptAgent) MetaError() error {
	s.loadMeta()
	return s.metaErr
}

func (s *ScriptAgent) ID() string   { s.loadMeta(); return s.id }
func (s *ScriptAgent) Name() string { s.loadMeta(); return s.name }
func (s *ScriptAgent) Icon() string { s.loadMeta(); return s.icon }

func (s *ScriptAgent) PushSignal() agent.PushSignal {
	s.loadMeta()
	return s.push
}

func (s *ScriptAgent) Capabilities() agent.Capabilities {
	s.loadMeta()
	return s.capability
}

func (s *ScriptAgent) Detect(ctx context.Context) agent.Detection {
	res, err := runner.call(ctx, s.file, "detect", nil, 10*time.Second)
	if err != nil {
		return agent.Detection{Installed: false, Detail: err.Error()}
	}
	d := agent.Detection{Installed: truthy(res["installed"])}
	if v := strField(res, "version"); v != "" {
		d.Version = &v
	}
	d.Detail = strField(res, "detail")
	return d
}

func (s *ScriptAgent) ListSessions(ctx context.Context, limit int) ([]agent.Session, error) {
	if limit <= 0 {
		limit = 200
	}
	res, err := runner.call(ctx, s.file, "listSessions", map[string]any{"limit": limit}, 20*time.Second)
	if err != nil {
		return nil, err
	}
	raw, _ := res["sessions"].([]any)
	out := make([]agent.Session, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, agent.Session{
			SessionID:   strField(m, "session_id"),
			Source:      s.ID(),
			Title:       strField(m, "title"),
			TitleSource: firstNonEmpty(strField(m, "title_source"), "first_prompt"),
			LastTS:      strField(m, "last_ts"),
			NativeState: strField(m, "native_state"),
			Cwd:         strField(m, "cwd"),
			Model:       strField(m, "model"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastTS > out[j].LastTS })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *ScriptAgent) Meta(sessionID, cwdHint string) agent.Meta {
	// 同步接口：用最近一次列表的结果，避免在 HTTP 处理里阻塞等子进程
	rows := s.cachedRows()
	for _, r := range rows {
		if r.SessionID == sessionID {
			cwd := r.Cwd
			if cwd == "" {
				cwd = cwdHint
			}
			return agent.Meta{Cwd: cwd, TitleSource: r.TitleSource,
				NativeTitle: r.Title}
		}
	}
	return agent.Meta{Cwd: cwdHint, TitleSource: "first_prompt"}
}

func (s *ScriptAgent) cachedRows() []agent.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.cacheAt) < 20*time.Second && s.cacheRows != nil {
		return s.cacheRows
	}
	s.mu.Unlock()
	rows, _ := s.ListSessions(context.Background(), 200)
	s.mu.Lock()
	s.cacheRows, s.cacheAt = rows, time.Now()
	return rows
}

func (s *ScriptAgent) ReadItems(ctx context.Context, sessionID string, limit int) ([]agent.Item, error) {
	if limit <= 0 {
		limit = 100
	}
	res, err := runner.call(ctx, s.file, "readItems",
		map[string]any{"sessionID": sessionID, "limit": limit}, 20*time.Second)
	if err != nil {
		return nil, err
	}
	raw, _ := res["items"].([]any)
	out := make([]agent.Item, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, agent.Item{
			ID:   fmt.Sprintf("%s-%d", sessionID, i),
			Role: strField(m, "role"),
			Kind: firstNonEmpty(strField(m, "kind"), "message"),
			Seq:  i,
			At:   strField(m, "at"),
			Text: strField(m, "text"),
		})
	}
	return out, nil
}

func (s *ScriptAgent) Reply(ctx context.Context, sessionID, text string, opts agent.ReplyOpts) error {
	if !s.Capabilities().Reply {
		return agent.ErrUnsupported
	}
	_, err := runner.call(ctx, s.file, "reply", map[string]any{
		"sessionID": sessionID,
		"text":      text,
		"opts":      map[string]any{"model": opts.Model},
	}, 60*time.Second)
	return err
}

func (s *ScriptAgent) Models(ctx context.Context) ([]agent.Model, string, error) {
	res, err := runner.call(ctx, s.file, "models", nil, 15*time.Second)
	if err != nil {
		return nil, "", err
	}
	raw, _ := res["models"].([]any)
	out := make([]agent.Model, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			if id := strField(m, "id"); id != "" {
				out = append(out, agent.Model{ID: id, Name: strField(m, "name")})
			}
		}
	}
	return out, strField(res, "defaultModel"), nil
}

// LoadScripts reads every *.mjs in dir. Adapters that cannot start are returned
// as errors with their reason (usually "no node or bun").
func LoadScripts(dir string) (agents []*ScriptAgent, errs []ManifestError) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".mjs") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		file := filepath.Join(dir, n)
		sa := NewScriptAgent(file)
		if err := sa.MetaError(); err != nil {
			errs = append(errs, ManifestError{File: file, ID: baseWithoutExt(n, ".mjs"), Problems: []string{err.Error()}})
			continue
		}
		agents = append(agents, sa)
	}
	return agents, errs
}

// RunnerAvailable reports whether script adapters can run at all.
func RunnerAvailable() (string, error) {
	return runner.prepare("")
}

func strField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return asString(m[key])
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	case float64:
		return t != 0
	}
	return false
}
