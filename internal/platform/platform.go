// Package platform holds the per-OS paths and startup glue: data directory,
// where each agent keeps its files (including WSL homes on Windows), and
// launch-at-login.
package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// DataDir is where homing keeps its own state:
// macOS ~/Library/Application Support/Homing, Windows %APPDATA%\Homing,
// Linux $XDG_CONFIG_HOME/homing (usually ~/.config/homing).
// HOMING_DATA_DIR overrides it (tests, parity runs, Docker).
func DataDir() (string, error) {
	if d := os.Getenv("HOMING_DATA_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	name := "Homing"
	if runtime.GOOS == "linux" {
		name = "homing"
	}
	return filepath.Join(base, name), nil
}

// UserAgentsDir is where people drop custom agent manifests (*.yaml) and
// UserAdaptersDir where they drop script adapters (*.mjs). Same on every OS so
// docs can give one path: ~/.homing/agents and ~/.homing/adapters.
func UserAgentsDir() string   { return filepath.Join(home(), ".homing", "agents") }
func UserAdaptersDir() string { return filepath.Join(home(), ".homing", "adapters") }

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}
