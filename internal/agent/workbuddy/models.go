package workbuddy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tomato-hug/homing/internal/agent"
)

// Models implements agent.ModelLister: the models seen in the 15 most recently
// written transcripts under ~/.workbuddy/projects. WorkBuddy keeps no model file,
// so this mirrors the Node relay's scan of the newest files.
func (Agent) Models(context.Context) ([]agent.Model, string, error) {
	root := filepath.Join(home(), "projects")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, "", nil
	}
	type fileInfo struct {
		path string
		mod  int64
	}
	var files []fileInfo
	for _, dir := range entries {
		if !dir.IsDir() {
			continue
		}
		dirPath := filepath.Join(root, dir.Name())
		children, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, f := range children {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			full := filepath.Join(dirPath, f.Name())
			st, err := os.Stat(full)
			if err != nil {
				continue
			}
			files = append(files, fileInfo{path: full, mod: st.ModTime().UnixMilli()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod > files[j].mod })

	var out []agent.Model
	seen := map[string]bool{}
	defaultModel := ""
	for i, fi := range files {
		if i >= 15 {
			break
		}
		for _, m := range modelsInFile(fi.path) {
			if seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, agent.Model{ID: m})
			if defaultModel == "" {
				defaultModel = m
			}
		}
	}
	return out, defaultModel, nil
}

// modelsInFile reads the last 256 KB and collects providerData.model values,
// newest file first, which is what the Node relay does.
func modelsInFile(path string) []string {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	window := int64(256 * 1024)
	start := st.Size() - window
	if start < 0 {
		start = 0
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Seek(start, 0); err != nil {
		return nil
	}
	buf := make([]byte, st.Size()-start)
	n, _ := f.Read(buf)
	var out []string
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "providerData") {
			continue
		}
		var row struct {
			ProviderData struct {
				Model string `json:"model"`
			} `json:"providerData"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &row) != nil {
			continue
		}
		m := firstNonEmptyStr(row.ProviderData.Model, row.Message.Model)
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
