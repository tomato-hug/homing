package adapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomato-hug/homing/internal/agent/scanlib"
	"github.com/tomato-hug/homing/internal/api"
)

// One-click install of the agent itself (not the Homing hook). The command
// always comes from the catalog by index: the request cannot carry its own
// shell text, and only the console on this computer may start it.

const (
	setupTimeout = 15 * time.Minute
	setupLogMax  = 64 * 1024
)

// SetupJob is one run of an install command, polled by the console.
type SetupJob struct {
	ID        string     `json:"id"`
	AgentID   string     `json:"agent_id"`
	AgentName string     `json:"agent_name"`
	Label     string     `json:"label"`
	Command   string     `json:"command"`
	Status    string     `json:"status"` // running | ok | failed
	ExitCode  *int       `json:"exit_code,omitempty"`
	Error     string     `json:"error,omitempty"`
	Log       string     `json:"log"`
	Detected  bool       `json:"detected"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

type setupRegistry struct {
	mu   sync.Mutex
	jobs map[string]*setupJobState
}

type setupJobState struct {
	mu  sync.Mutex
	job SetupJob
	log []byte
}

var setupJobs = &setupRegistry{jobs: map[string]*setupJobState{}}

func (s *setupJobState) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, p...)
	if len(s.log) > setupLogMax {
		s.log = append([]byte("…（前面的输出已省略）\n"), s.log[len(s.log)-setupLogMax:]...)
	}
	return len(p), nil
}

func (s *setupJobState) snapshot() SetupJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.job
	out.Log = string(s.log)
	return out
}

// setupCommandsFor returns the install commands this computer can run now.
// Windows is left out until the catalog carries PowerShell variants.
func setupCommandsFor(spec catalogSpec) []CatalogCommand {
	if runtime.GOOS == "windows" {
		return nil
	}
	out := make([]CatalogCommand, 0, len(spec.InstallCommands))
	for _, command := range spec.InstallCommands {
		if setupBinaryAvailable(firstCommandWord(command.Command)) {
			out = append(out, command)
		}
	}
	return out
}

func firstCommandWord(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

var (
	setupBinMu    sync.Mutex
	setupBinCache = map[string]setupBinEntry{}
)

type setupBinEntry struct {
	ok bool
	at time.Time
}

func setupBinaryAvailable(name string) bool {
	if name == "" || strings.ContainsAny(name, "/\\") {
		return false
	}
	setupBinMu.Lock()
	cached, ok := setupBinCache[name]
	setupBinMu.Unlock()
	if ok && time.Since(cached.at) < time.Minute {
		return cached.ok
	}
	found := scanlib.ResolveBinary(name, setupExtraBins(name)...) != ""
	setupBinMu.Lock()
	setupBinCache[name] = setupBinEntry{ok: found, at: time.Now()}
	setupBinMu.Unlock()
	return found
}

// setupExtraBins covers node managers that the LaunchAgent PATH never sees.
func setupExtraBins(name string) []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	out := []string{filepath.Join(home, ".volta", "bin", name), filepath.Join(home, ".bun", "bin", name)}
	if matches, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", name)); len(matches) > 0 {
		sort.Strings(matches)
		out = append(out, matches[len(matches)-1])
	}
	return out
}

// setupPATH is the PATH handed to the login shell, so a brew or npm that the
// user installed is found even when homing runs as a LaunchAgent.
func setupPATH() string {
	home, _ := os.UserHomeDir()
	parts := []string{
		"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin",
		filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npm-global", "bin"),
		filepath.Join(home, ".volta", "bin"), filepath.Join(home, ".bun", "bin"),
	}
	if matches, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); len(matches) > 0 {
		sort.Strings(matches)
		parts = append(parts, matches[len(matches)-1])
	}
	if current := os.Getenv("PATH"); current != "" {
		parts = append(parts, current)
	}
	parts = append(parts, "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	return strings.Join(parts, string(os.PathListSeparator))
}

func setupShell() (string, []string) {
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/bin/zsh"); err == nil {
			return "/bin/zsh", []string{"-lc"}
		}
	}
	return "/bin/bash", []string{"-lc"}
}

// consoleOnly allows a request only from the console page on this computer:
// loopback socket, no cloud RPC marker, and (when the browser sends one) an
// Origin on this host. Phones and other machines never install software here.
func consoleOnly(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	allowed := ip != nil && ip.IsLoopback() && r.Header.Get("X-Homing-RPC") == ""
	if allowed {
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && origin != "null" {
			parsed, err := url.Parse(origin)
			hostname := ""
			if err == nil {
				hostname = parsed.Hostname()
			}
			allowed = hostname == "127.0.0.1" || hostname == "localhost" || hostname == "::1"
		}
	}
	if !allowed {
		api.WriteJSON(w, http.StatusForbidden, map[string]any{
			"ok":    false,
			"error": "只能在这台电脑的控制台里安装 Agent",
			"code":  "console_only",
		})
		return false
	}
	return true
}

func newSetupID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func (r *setupRegistry) running(agentID string) *setupJobState {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, state := range r.jobs {
		snap := state.snapshot()
		if snap.AgentID == agentID && snap.Status == "running" {
			return state
		}
	}
	return nil
}

func (r *setupRegistry) add(state *setupJobState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Keep the registry small: drop finished jobs older than an hour.
	for id, existing := range r.jobs {
		snap := existing.snapshot()
		if snap.EndedAt != nil && time.Since(*snap.EndedAt) > time.Hour {
			delete(r.jobs, id)
		}
	}
	r.jobs[state.job.ID] = state
}

func (r *setupRegistry) get(id string) *setupJobState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[id]
}

// StartCatalogSetup runs install command #index of agent id in the background.
func StartCatalogSetup(id string, index int) (SetupJob, int, string) {
	spec, ok := catalogSpecByID(id)
	if !ok {
		return SetupJob{}, http.StatusNotFound, "没有这个 Agent"
	}
	if runtime.GOOS == "windows" {
		return SetupJob{}, http.StatusBadRequest, "Windows 上暂不支持一键安装，请复制命令到 PowerShell 运行"
	}
	if index < 0 || index >= len(spec.InstallCommands) {
		return SetupJob{}, http.StatusBadRequest, "安装方式不存在"
	}
	command := spec.InstallCommands[index]
	if !setupBinaryAvailable(firstCommandWord(command.Command)) {
		return SetupJob{}, http.StatusBadRequest, "这台电脑没有 " + firstCommandWord(command.Command) + "，换一种安装方式"
	}
	if existing := setupJobs.running(id); existing != nil {
		return existing.snapshot(), http.StatusOK, ""
	}
	state := &setupJobState{job: SetupJob{
		ID:        newSetupID(),
		AgentID:   spec.ID,
		AgentName: spec.Name,
		Label:     command.Label,
		Command:   command.Command,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}}
	_, _ = state.Write([]byte("$ " + command.Command + "\n"))
	setupJobs.add(state)
	go runSetup(state, spec)
	return state.snapshot(), http.StatusAccepted, ""
}

func runSetup(state *setupJobState, spec catalogSpec) {
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	shell, args := setupShell()
	cmd := exec.CommandContext(ctx, shell, append(args, state.job.Command)...)
	home, _ := os.UserHomeDir()
	cmd.Dir = home
	cmd.Env = append(os.Environ(), "PATH="+setupPATH(), "CI=1", "NONINTERACTIVE=1", "HOMEBREW_NO_AUTO_UPDATE=1")
	cmd.Stdin = nil
	cmd.Stdout = state
	cmd.Stderr = state
	err := cmd.Run()

	// The install just changed PATH entries: forget cached lookups.
	setupBinMu.Lock()
	setupBinCache = map[string]setupBinEntry{}
	setupBinMu.Unlock()

	ended := time.Now().UTC()
	code := 0
	errText := ""
	if err != nil {
		code = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
		errText = err.Error()
		if ctx.Err() == context.DeadlineExceeded {
			errText = "安装超过 " + strconv.Itoa(int(setupTimeout/time.Minute)) + " 分钟，已停止"
		}
	}
	detected := detectCatalogSpec(spec)
	state.mu.Lock()
	state.job.EndedAt = &ended
	state.job.ExitCode = &code
	state.job.Detected = detected
	if err == nil {
		state.job.Status = "ok"
	} else {
		state.job.Status = "failed"
		state.job.Error = errText
	}
	state.mu.Unlock()
	if err == nil {
		_, _ = state.Write([]byte("\n安装完成。\n"))
	} else {
		_, _ = state.Write([]byte("\n安装失败：" + errText + "\n"))
	}
}

func mountCatalogSetupRoutes(mux *http.ServeMux, d *api.Deps) {
	mux.HandleFunc("POST /api/agent-catalog/{id}/setup", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) || !consoleOnly(w, r) {
			return
		}
		id := r.PathValue("id")
		if !validID(id) {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "agent id 不合法"})
			return
		}
		body, err := readJSONBody(r)
		if err != nil {
			api.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请求格式不对"})
			return
		}
		index := 0
		if value, ok := body["index"].(float64); ok {
			index = int(value)
		}
		job, status, problem := StartCatalogSetup(id, index)
		if problem != "" {
			api.WriteJSON(w, status, map[string]any{"ok": false, "error": problem})
			return
		}
		d.Audit(r, "agent_setup", map[string]any{"target": id, "kind": job.Label})
		api.WriteJSON(w, status, map[string]any{"ok": true, "job": job})
	})

	mux.HandleFunc("GET /api/agent-setup/{job}", func(w http.ResponseWriter, r *http.Request) {
		if !adapterAuthorized(d, w, r) || !consoleOnly(w, r) {
			return
		}
		state := setupJobs.get(r.PathValue("job"))
		if state == nil {
			api.WriteJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "安装任务不存在或已过期"})
			return
		}
		api.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "job": state.snapshot()})
	})
}
