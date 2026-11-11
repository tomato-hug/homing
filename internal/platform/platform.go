// Package platform holds the per-OS paths and startup glue: data directory,
// where each agent keeps its files (including WSL homes on Windows), LAN
// addresses and launch-at-login.
//
// It is a straight port of relay/platform/index.mjs + homes.mjs, so the Go
// server reads exactly the same files as the Node relay while both run.
package platform

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Version and Product are reported by /api/connection and `homing version`.
const (
	Version = "0.7.0"
	Product = "Homing"
)

// DataDir is where homing keeps its own state:
//
//	macOS   ~/Library/Application Support/Homing
//	Windows %APPDATA%\Homing
//	Linux   $XDG_DATA_HOME/homing (usually ~/.local/share/homing)
//
// HOMING_DATA_DIR overrides it (tests, parity runs, Docker).
func DataDir() (string, error) {
	if d := os.Getenv("HOMING_DATA_DIR"); d != "" {
		return filepath.Abs(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Homing"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Homing"), nil
	default:
		xdg := os.Getenv("XDG_DATA_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(xdg, "homing"), nil
	}
}

// Paths lists every file homing owns inside the data directory.
type Paths struct {
	DataDir      string
	Logs         string
	Console      string
	Config       string
	Devices      string
	Pause        string
	SessionState string
	Events       string
	Replies      string
	Dispatches   string
	AwayPeriods  string
	Audit        string
	History      string
	Index        string
	Log          string
	App          string
}

// NewPaths derives the file layout for one data directory and creates the
// directories (mode 0700) the way the Node relay does on startup.
func NewPaths(dataDir string) Paths {
	p := Paths{
		DataDir:      dataDir,
		Logs:         filepath.Join(dataDir, "logs"),
		Console:      filepath.Join(dataDir, "console"),
		Config:       filepath.Join(dataDir, "config.json"),
		Devices:      filepath.Join(dataDir, "devices.json"),
		Pause:        filepath.Join(dataDir, "push-pause.json"),
		SessionState: filepath.Join(dataDir, "session-state.json"),
		Events:       filepath.Join(dataDir, "events.jsonl"),
		Replies:      filepath.Join(dataDir, "replies.jsonl"),
		Dispatches:   filepath.Join(dataDir, "dispatches.jsonl"),
		AwayPeriods:  filepath.Join(dataDir, "away-periods.jsonl"),
		Audit:        filepath.Join(dataDir, "audit.jsonl"),
		History:      filepath.Join(dataDir, "history.db"),
		Index:        filepath.Join(dataDir, "index.db"),
		App:          filepath.Join(dataDir, "app.json"),
	}
	p.Log = os.Getenv("HOMING_LOG_PATH")
	if p.Log == "" {
		p.Log = filepath.Join(dataDir, "homingd.log")
	}
	for _, dir := range []string{p.DataDir, p.Logs, p.Console} {
		_ = os.MkdirAll(dir, 0o700)
	}
	return p
}

// ReadJSON decodes file into v. A missing or damaged file is not an error for
// callers (v keeps its zero value), same as the Node helper returning fallback.
func ReadJSON(file string, v any) bool {
	raw, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// WriteJSON writes atomically (temp file + rename) with mode 0600, so a crash
// never leaves half a JSON document behind.
func WriteJSON(file string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFile(file, append(raw, '\n'))
}

// WriteFile writes raw bytes atomically.
func WriteFile(file string, data []byte) error {
	return WriteFileWithMode(file, data, 0o600)
}

// HostName is shown on the phone under "add a computer".
func HostName() string {
	name, err := os.Hostname()
	if err != nil {
		return "本机"
	}
	return strings.TrimSuffix(name, ".local")
}

// PlatformLabel is the machine-readable OS name (Mac / Windows / Linux).
func PlatformLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "Mac"
	case "windows":
		return "Windows"
	default:
		return "Linux"
	}
}

// LANAddresses returns this machine's IPv4 addresses, best candidate first.
// 198.18.0.0/15 is what proxy TUN interfaces hand out (phones can't reach it);
// like the Node relay, those addresses are omitted. 169.254.x.x means DHCP
// failed and sorts last.
func LANAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	type candidate struct {
		addr string
		rank int
	}
	var found []candidate
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ip := ipnet.IP.To4().String()
			if strings.HasPrefix(ip, "198.18.") || strings.HasPrefix(ip, "198.19.") {
				continue
			}
			found = append(found, candidate{ip, rankOf(ip)})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].rank < found[j].rank })
	out := make([]string, 0, len(found))
	for _, c := range found {
		out = append(out, c.addr)
	}
	return out
}

// rankOf scores an IPv4 address: home / office ranges first, link-local last.
func rankOf(ip string) int {
	home := []string{"192.168.", "10.", "172.16.", "172.17.", "172.18.", "172.19.", "172.2", "172.30.", "172.31."}
	for _, prefix := range home {
		if strings.HasPrefix(ip, prefix) {
			return 0
		}
	}
	if strings.HasPrefix(ip, "169.254.") {
		return 2
	}
	return 1
}

// LANURLs is what goes into the pairing QR code.
func LANURLs(port int) []string {
	addrs := LANAddresses()
	out := make([]string, 0, len(addrs))
	for _, ip := range addrs {
		out = append(out, "http://"+net.JoinHostPort(ip, strconv.Itoa(port)))
	}
	return out
}

// LocalURL is the loopback base URL.
func LocalURL(port int) string { return "http://127.0.0.1:" + strconv.Itoa(port) }

// ---- agent homes ---------------------------------------------------------

// agentHome describes where an agent keeps its files relative to a home dir.
type agentHome struct {
	label string
	paths []string
	probe []string
}

var agentHomes = map[string]agentHome{
	"codex":     {"Codex", []string{".codex"}, []string{".codex/session_index.jsonl", ".codex/sessions", ".codex/hooks.json"}},
	"claude":    {"Claude Code", []string{".claude"}, []string{".claude/projects", ".claude/settings.json"}},
	"workbuddy": {"WorkBuddy", []string{".workbuddy"}, []string{".workbuddy"}},
	"kimi":      {"Kimi Code", []string{".kimi-code"}, []string{".kimi-code/sessions", ".kimi-code/config.toml"}},
	"opencode":  {"OpenCode", []string{".local/share/opencode"}, []string{".local/share/opencode/opencode.db"}},
	"zcode":     {"ZCode", []string{".zcode"}, []string{".zcode/cli/db/db.sqlite"}},
}

// UserAgentsDir is where people drop custom agent manifests (*.yaml);
// UserAdaptersDir where they drop script adapters (*.mjs). Same on every OS.
func UserAgentsDir() string   { return filepath.Join(Home(), ".homing", "agents") }
func UserAdaptersDir() string { return filepath.Join(Home(), ".homing", "adapters") }

// Home is the primary (native) home directory.
func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// DataPathFor is the path shown in the UI for one agent.
func DataPathFor(agentID, home string) string {
	spec, ok := agentHomes[agentID]
	if !ok {
		return home
	}
	return filepath.Join(home, filepath.FromSlash(spec.paths[0]))
}

// ExistsIn reports whether the agent has data under home.
func ExistsIn(agentID, home string) bool {
	spec, ok := agentHomes[agentID]
	if !ok {
		return false
	}
	for _, rel := range spec.probe {
		if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(rel))); err == nil {
			return true
		}
	}
	return false
}

// WSLHome is one home directory inside a WSL distro (Windows only).
type WSLHome struct {
	Dir    string `json:"dir"`
	Distro string `json:"distro"`
	User   string `json:"user"`
}

// WSLHomes lists \\wsl$\<distro>\home\<user> (and \root) on Windows.
func WSLHomes() []WSLHome {
	if runtime.GOOS != "windows" {
		return nil
	}
	var out []WSLHome
	for _, root := range []string{`\\wsl$\`, `\\wsl.localhost\`} {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, distro := range entries {
			if !distro.IsDir() {
				continue
			}
			users, err := os.ReadDir(root + distro.Name() + `\home`)
			if err != nil {
				out = append(out, WSLHome{Dir: root + distro.Name() + `\root`, Distro: distro.Name(), User: "root"})
				continue
			}
			for _, user := range users {
				if !user.IsDir() {
					continue
				}
				out = append(out, WSLHome{
					Dir:    root + distro.Name() + `\home\` + user.Name(),
					Distro: distro.Name(),
					User:   user.Name(),
				})
			}
			if len(out) > 8 {
				return out
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return out
}

// HomeInfo is the /api/agent-paths row for one agent.
type HomeInfo struct {
	Path string    `json:"path"`
	WSL  []WSLHome `json:"wsl"`
}

// DescribeHome lists the native path plus any WSL copies that actually exist.
func DescribeHome(agentID string) HomeInfo {
	info := HomeInfo{Path: DataPathFor(agentID, Home()), WSL: []WSLHome{}}
	for _, h := range WSLHomes() {
		if ExistsIn(agentID, h.Dir) {
			info.WSL = append(info.WSL, WSLHome{Dir: h.Dir, Distro: h.Distro, User: h.User})
		}
	}
	return info
}

// MigrateLegacy copies config.json and relay/data/* into the data dir when the
// target file does not exist yet. It never overwrites and never deletes, so
// already-paired phones survive the switch from Node to Go.
func MigrateLegacy(legacyRelayDir string, logf func(string)) []string {
	if legacyRelayDir == "" {
		return nil
	}
	dataDir, err := DataDir()
	if err != nil {
		return nil
	}
	p := NewPaths(dataDir)
	pairs := [][2]string{
		{filepath.Join(legacyRelayDir, "config.json"), p.Config},
		{filepath.Join(legacyRelayDir, "data", "events.jsonl"), p.Events},
		{filepath.Join(legacyRelayDir, "data", "replies.jsonl"), p.Replies},
		{filepath.Join(legacyRelayDir, "data", "session-state.json"), p.SessionState},
		{filepath.Join(legacyRelayDir, "data", "devices.json"), p.Devices},
	}
	var copied []string
	for _, pair := range pairs {
		from, to := pair[0], pair[1]
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if _, err := os.Stat(to); err == nil {
			continue
		}
		raw, err := os.ReadFile(from)
		if err != nil {
			if logf != nil {
				logf("迁移 " + filepath.Base(to) + " 失败：" + err.Error())
			}
			continue
		}
		if err := WriteFile(to, raw); err != nil {
			if logf != nil {
				logf("迁移 " + filepath.Base(to) + " 失败：" + err.Error())
			}
			continue
		}
		copied = append(copied, filepath.Base(to))
	}
	return copied
}
