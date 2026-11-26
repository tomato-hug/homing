package platform

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Autostart labels match relay/platform/autostart.mjs. The legacy plist belongs
// to the Node relay; the desktop app checks for it so it does not start a
// second service.
const (
	MacAutostartLabel     = "dev.homing.homingd"
	LegacyAutostartLabel  = "dev.agentpush.relay"
	WindowsAutostartKey   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	WindowsAutostartValue = "Homing"
)

// AutostartState is the subset of Node's autostartInfo() the console reads.
type AutostartState struct {
	Enabled bool    `json:"enabled"`
	Backend string  `json:"backend"`
	Target  *string `json:"target"`
	Legacy  *string `json:"legacy"`
}

// AutostartInfo reports the current launch-at-login state. It only inspects
// files/registry values; it never loads or starts the service.
func AutostartInfo() AutostartState {
	home, _ := os.UserHomeDir()
	return autostartInfoForHome(home)
}

func autostartInfoForHome(home string) AutostartState {
	legacy := existingPath(filepath.Join(home, "Library", "LaunchAgents", LegacyAutostartLabel+".plist"))
	switch runtime.GOOS {
	case "darwin":
		target := filepath.Join(home, "Library", "LaunchAgents", MacAutostartLabel+".plist")
		if _, err := os.Stat(target); err == nil {
			return AutostartState{Enabled: true, Backend: "launchagent", Target: &target, Legacy: legacy}
		}
		if legacy != nil {
			return AutostartState{Enabled: true, Backend: "launchagent-legacy", Target: &target, Legacy: legacy}
		}
		return AutostartState{Enabled: false, Backend: "launchagent", Target: &target}
	case "windows":
		key := WindowsAutostartKey
		cmd := exec.Command("reg", "query", key, "/v", WindowsAutostartValue)
		HideChildWindow(cmd)
		out, err := cmd.Output()
		if err == nil {
			target := key
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 3 && fields[0] == WindowsAutostartValue {
					target = strings.Join(fields[2:], " ")
					break
				}
			}
			return AutostartState{Enabled: true, Backend: "run-key", Target: &target, Legacy: legacy}
		}
		return AutostartState{Enabled: false, Backend: "run-key", Target: &key, Legacy: legacy}
	default:
		return AutostartState{Enabled: false, Backend: "none", Legacy: legacy}
	}
}

// SetAutostart writes or removes the per-user launch-at-login entry. It does
// not load the LaunchAgent immediately, because that would race the running
// service for its port; the entry takes effect on the next login (same as Node).
func SetAutostart(enabled bool, execPath, dataDir string, port int) (AutostartState, error) {
	if port <= 0 || port > 65535 {
		return AutostartState{}, fmt.Errorf("invalid port %d", port)
	}
	if enabled && strings.TrimSpace(execPath) == "" {
		return AutostartState{}, errors.New("找不到 homing 可执行文件")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return AutostartState{}, err
	}
	switch runtime.GOOS {
	case "darwin":
		path := filepath.Join(home, "Library", "LaunchAgents", MacAutostartLabel+".plist")
		if !enabled {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return AutostartState{}, fmt.Errorf("移除 LaunchAgent 失败：%w", err)
			}
			return autostartInfoForHome(home), nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return AutostartState{}, fmt.Errorf("创建 LaunchAgents 目录失败：%w", err)
		}
		body := macPlist(execPath, dataDir, port)
		if err := WriteFileWithMode(path, []byte(body), 0o644); err != nil {
			return AutostartState{}, fmt.Errorf("写入 LaunchAgent 失败：%w", err)
		}
		return autostartInfoForHome(home), nil
	case "windows":
		baseLower := strings.ToLower(filepath.Base(execPath))
		var command string
		if strings.Contains(baseLower, "cli") || strings.Contains(baseLower, "serve") {
			command = `"` + execPath + `" serve --addr 0.0.0.0:` + strconv.Itoa(port)
		} else {
			command = `"` + execPath + `" --background --addr 0.0.0.0:` + strconv.Itoa(port)
		}
		var args []string
		if enabled {
			args = []string{"add", WindowsAutostartKey, "/v", WindowsAutostartValue, "/t", "REG_SZ", "/d", command, "/f"}
		} else {
			args = []string{"delete", WindowsAutostartKey, "/v", WindowsAutostartValue, "/f"}
		}
		regCmd := exec.Command("reg", args...)
		HideChildWindow(regCmd)
		if out, err := regCmd.CombinedOutput(); err != nil {
			if !enabled {
				return autostartInfoForHome(home), nil
			}
			return AutostartState{}, fmt.Errorf("写注册表失败：%s", strings.TrimSpace(string(out)))
		}
		return autostartInfoForHome(home), nil
	default:
		return AutostartState{}, errors.New("暂不支持在当前系统上设置开机自启")
	}
}

func macPlist(execPath, dataDir string, port int) string {
	e := xmlEscape
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + e(MacAutostartLabel) + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + e(execPath) + `</string>
		<string>serve</string>
		<string>--addr</string>
		<string>0.0.0.0:` + strconv.Itoa(port) + `</string>
	</array>
	<key>WorkingDirectory</key>
	<string>` + e(dataDir) + `</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>` + e(filepath.Join(dataDir, "homingd.log")) + `</string>
	<key>StandardErrorPath</key>
	<string>` + e(filepath.Join(dataDir, "homingd.log")) + `</string>
</dict>
</plist>
`
}

func xmlEscape(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func existingPath(path string) *string {
	if _, err := os.Stat(path); err == nil {
		return &path
	}
	return nil
}

// WriteFileWithMode is the atomic writer for files that must be readable by
// launchd/other users (the LaunchAgent plist); the rest of Homing uses 0600.
func WriteFileWithMode(file string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, file)
}
