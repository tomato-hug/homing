package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// PrepareRuntimeEnvironment supplies the command search paths Finder and
// launchd omit. It preserves explicit PATH precedence and never runs a login
// shell or imports credentials from shell startup files.
func PrepareRuntimeEnvironment() {
	if runtime.GOOS != "darwin" {
		return
	}
	userHome, _ := os.UserHomeDir()
	_ = os.Setenv("PATH", macRuntimePath(os.Getenv("PATH"), userHome))
}

func macRuntimePath(current, userHome string) string {
	parts := filepath.SplitList(current)
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		seen[filepath.Clean(part)] = true
	}
	add := func(dir string) {
		if !filepath.IsAbs(dir) || seen[filepath.Clean(dir)] {
			return
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return
		}
		parts = append(parts, dir)
		seen[filepath.Clean(dir)] = true
	}
	if filepath.IsAbs(userHome) {
		for _, rel := range []string{".local/bin", ".npm-global/bin", ".volta/bin", ".bun/bin", ".cargo/bin", ".opencode/bin", ".asdf/shims", ".local/share/mise/shims"} {
			add(filepath.Join(userHome, rel))
		}
		if st, err := os.Stat(filepath.Join(userHome, ".nvm/current/bin")); err == nil && st.IsDir() {
			add(filepath.Join(userHome, ".nvm/current/bin"))
		} else {
			bins, _ := filepath.Glob(filepath.Join(userHome, ".nvm/versions/node/v*/bin"))
			sort.Slice(bins, func(i, j int) bool {
				a := strings.Split(strings.TrimPrefix(filepath.Base(filepath.Dir(bins[i])), "v"), ".")
				b := strings.Split(strings.TrimPrefix(filepath.Base(filepath.Dir(bins[j])), "v"), ".")
				for k := 0; k < 3; k++ {
					av, bv := 0, 0
					if k < len(a) {
						av, _ = strconv.Atoi(a[k])
					}
					if k < len(b) {
						bv, _ = strconv.Atoi(b[k])
					}
					if av != bv {
						return av > bv
					}
				}
				return bins[i] > bins[j]
			})
			if len(bins) > 0 {
				add(bins[0])
			}
		}
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/local/sbin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		add(dir)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}
