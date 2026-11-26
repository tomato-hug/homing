package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ExtraBinDirs returns common user-level CLI installation directories that
// macOS launchd / Finder .app bundles and Linux systemd user services omit from
// their default PATH (e.g. ~/.npm-global/bin, ~/.local/bin, /opt/homebrew/bin,
// ~/.opencode/bin, ~/.bun/bin, ~/.cargo/bin, ~/.kimi/bin, ~/.nvm/...).
func ExtraBinDirs() []string {
	home := Home()
	var dirs []string
	if runtime.GOOS == "darwin" {
		dirs = append(dirs, "/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin")
	} else if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" && home != "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" && home != "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		if appData != "" {
			dirs = append(dirs, filepath.Join(appData, "npm"))
		}
		if localAppData != "" {
			dirs = append(dirs,
				filepath.Join(localAppData, "Microsoft", "WinGet", "Links"),
				filepath.Join(localAppData, "Programs", "claude-code"),
				filepath.Join(localAppData, "AnthropicClaude"),
				filepath.Join(localAppData, "Programs", "Cursor", "resources", "app", "bin"),
				filepath.Join(localAppData, "Programs", "Windsurf", "bin"),
				filepath.Join(localAppData, "Programs", "Trae", "bin"),
				filepath.Join(localAppData, "Programs", "WorkBuddy"),
				filepath.Join(localAppData, "Programs", "nodejs"),
			)
		}
		if nvmSym := strings.TrimSpace(os.Getenv("NVM_SYMLINK")); nvmSym != "" {
			dirs = append(dirs, nvmSym)
		}
		if pf := strings.TrimSpace(os.Getenv("ProgramFiles")); pf != "" {
			dirs = append(dirs,
				filepath.Join(pf, "nodejs"),
				filepath.Join(pf, "Git", "cmd"),
			)
		}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "scoop", "shims"))
		}
	} else {
		dirs = append(dirs, "/usr/local/bin", "/snap/bin")
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".kimi", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, "bin"),
		)
		if nvmMatches, err := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); err == nil && len(nvmMatches) > 0 {
			sort.Strings(nvmMatches)
			for i := len(nvmMatches) - 1; i >= 0; i-- {
				dirs = append(dirs, nvmMatches[i])
			}
		}
	}
	return dirs
}

// AugmentedPATH merges os.Getenv("PATH") with ExtraBinDirs() without duplicates.
func AugmentedPATH() string {
	sep := string(os.PathListSeparator)
	current := os.Getenv("PATH")
	seen := map[string]bool{}
	var parts []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		parts = append(parts, dir)
	}
	for _, p := range filepath.SplitList(current) {
		add(p)
	}
	for _, p := range ExtraBinDirs() {
		add(p)
	}
	return strings.Join(parts, sep)
}

// EnvWithAugmentedPATH returns os.Environ() with PATH replaced by AugmentedPATH()
// so child CLIs using `#!/usr/bin/env node` or `python3` succeed under launchd.
func EnvWithAugmentedPATH(extra map[string]string) []string {
	aug := AugmentedPATH()
	base := os.Environ()
	out := make([]string, 0, len(base)+len(extra)+1)
	replacedPath := false
	for _, kv := range base {
		if strings.HasPrefix(kv, "PATH=") || (runtime.GOOS == "windows" && strings.HasPrefix(strings.ToUpper(kv), "PATH=")) {
			out = append(out, "PATH="+aug)
			replacedPath = true
			continue
		}
		out = append(out, kv)
	}
	if !replacedPath {
		out = append(out, "PATH="+aug)
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

// ResolveBinary locates an executable on PATH or inside ExtraBinDirs().
func ResolveBinary(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{".exe", ".cmd", ".bat", ""}
	}
	for _, dir := range ExtraBinDirs() {
		for _, ext := range exts {
			candidate := filepath.Join(dir, name+ext)
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				if runtime.GOOS == "windows" || st.Mode()&0o111 != 0 {
					return candidate
				}
			}
		}
	}
	return ""
}
