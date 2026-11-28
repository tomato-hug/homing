package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	filepaths "github.com/tomato-hug/homing/internal/files"
)

const (
	fsReadMaxBytes = 512 << 10
	fsRawMaxBytes  = 20 << 20
)

var allowedRawPreviewExt = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".svg":  true,
	".bmp":  true,
	".ico":  true,
	".avif": true,
	".pdf":  true,
	".html": true,
	".htm":  true,
}

func init() {
	Register("fs-read", 21, func(mux *http.ServeMux, d *Deps) {
		mux.HandleFunc("GET /api/fs/read", d.guard(d.handleFSRead))
		mux.HandleFunc("GET /api/fs/raw", d.guard(d.handleFSRaw))
	})
}

func (d *Deps) resolveProjectFile(w http.ResponseWriter, r *http.Request) (string, int, os.FileInfo, bool) {
	query := r.URL.Query()
	rawPath := strings.TrimSpace(query.Get("path"))
	rawCwd := strings.TrimSpace(query.Get("cwd"))
	if rawPath == "" {
		WriteError(w, http.StatusBadRequest, "path_required", "path 不能为空")
		return "", 0, nil, false
	}
	rawPath, line := splitPathLineSuffix(rawPath)
	if strings.HasPrefix(rawPath, "file://") {
		rawPath = strings.TrimPrefix(rawPath, "file://")
	}
	if strings.HasPrefix(rawPath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			rawPath = filepath.Join(home, rawPath[2:])
		}
	}

	roots := d.knownProjectRoots(ctxOf(r), false)
	if len(roots) == 0 {
		roots = d.knownProjectRoots(ctxOf(r), true)
	}
	if len(roots) == 0 {
		WriteError(w, http.StatusForbidden, "no_known_projects", "没有已知项目目录")
		return "", 0, nil, false
	}

	var candidate string
	if filepath.IsAbs(rawPath) {
		candidate = filepath.Clean(rawPath)
	} else {
		base := ""
		if rawCwd != "" {
			if abs, err := filepath.Abs(rawCwd); err == nil {
				base = abs
			}
		}
		if base == "" {
			WriteError(w, http.StatusBadRequest, "cwd_required", "相对路径需要提供 cwd")
			return "", 0, nil, false
		}
		candidate = filepath.Clean(filepath.Join(base, rawPath))
	}

	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		WriteError(w, http.StatusNotFound, "file_not_found", "文件不存在")
		return "", 0, nil, false
	}
	if !pathWithinRoots(real, roots) {
		// Event history covers most projects; native session scans are slower,
		// so they are consulted only when the cheap check fails.
		roots = d.knownProjectRoots(ctxOf(r), true)
	}
	if !pathWithinRoots(real, roots) {
		WriteError(w, http.StatusForbidden, "outside_project", "文件不在已知项目目录内")
		return "", 0, nil, false
	}
	if sensitiveFile(real) {
		WriteError(w, http.StatusForbidden, "sensitive_file", "出于安全考虑，不在手机上显示密钥或凭据类文件")
		return "", 0, nil, false
	}
	info, err := os.Stat(real)
	if err != nil {
		WriteError(w, http.StatusNotFound, "file_not_found", "文件不存在")
		return "", 0, nil, false
	}
	if info.IsDir() {
		WriteError(w, http.StatusBadRequest, "is_directory", "这是一个目录")
		return "", 0, nil, false
	}
	return real, line, info, true
}

// handleFSRead returns the text of one file inside a project an agent has
// worked in. It powers the phone's code / text viewer for file paths that
// appear in conversations. It never reads outside known project directories,
// never follows a symlink out of them, refuses credential-like files and
// binaries, and caps the payload.
func (d *Deps) handleFSRead(w http.ResponseWriter, r *http.Request) {
	real, line, info, ok := d.resolveProjectFile(w, r)
	if !ok {
		return
	}
	file, err := os.Open(real)
	if err != nil {
		WriteError(w, http.StatusNotFound, "file_not_found", "文件无法读取")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, fsReadMaxBytes+1))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "read_failed", "读取失败")
		return
	}
	truncated := len(raw) > fsReadMaxBytes
	if truncated {
		raw = raw[:fsReadMaxBytes]
		for len(raw) > 0 && !utf8.Valid(raw) {
			raw = raw[:len(raw)-1]
		}
	}
	probe := raw
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if bytes.IndexByte(probe, 0) >= 0 || !utf8.Valid(probe[:validPrefixLen(probe)]) {
		WriteError(w, http.StatusUnsupportedMediaType, "binary_file", "二进制文件无法以文本方式查看")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"path":      real,
		"name":      filepath.Base(real),
		"size":      info.Size(),
		"truncated": truncated,
		"line":      line,
		"language":  strings.TrimPrefix(strings.ToLower(filepath.Ext(real)), "."),
		"content":   string(raw),
	})
}

// handleFSRaw serves safe previewable project media (images, SVG, HTML, PDF)
// inside known project roots, guarding size and file type so Android can load
// inline images referenced by relative or workspace paths over LAN or Cloud RPC.
func (d *Deps) handleFSRaw(w http.ResponseWriter, r *http.Request) {
	real, _, info, ok := d.resolveProjectFile(w, r)
	if !ok {
		return
	}
	ext := strings.ToLower(filepath.Ext(real))
	if !allowedRawPreviewExt[ext] {
		WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "仅支持在手机上预览常见图片、SVG、HTML 或 PDF 文件")
		return
	}
	if info.Size() > fsRawMaxBytes {
		WriteError(w, http.StatusRequestEntityTooLarge, "file_too_large", "文件超过 20 MB 预览上限")
		return
	}
	file, err := os.Open(real)
	if err != nil {
		WriteError(w, http.StatusNotFound, "file_not_found", "文件无法读取")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", filepaths.MimeFor(real))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=60")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// validPrefixLen trims an incomplete trailing UTF-8 sequence from the probe so
// cutting at 8 KiB does not misclassify a text file as binary.
func validPrefixLen(b []byte) int {
	n := len(b)
	for i := 0; i < 4 && n > 0; i++ {
		if utf8.Valid(b[:n]) {
			return n
		}
		n--
	}
	return n
}

// splitPathLineSuffix turns "src/a.go:12:4" into ("src/a.go", 12).
func splitPathLineSuffix(raw string) (string, int) {
	parts := strings.Split(raw, ":")
	if len(parts) < 2 {
		return raw, 0
	}
	line := 0
	cut := len(parts)
	for i := len(parts) - 1; i >= 1 && i >= len(parts)-2; i-- {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n <= 0 {
			break
		}
		line = n
		cut = i
	}
	if cut == len(parts) {
		return raw, 0
	}
	return strings.Join(parts[:cut], ":"), line
}

func (d *Deps) knownProjectRoots(ctx context.Context, includeNativeSessions bool) []string {
	s, ok := d.store()
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var roots []string
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return
		}
		if real == "/" || seen[real] {
			return
		}
		if home, err := os.UserHomeDir(); err == nil && real == home {
			return
		}
		seen[real] = true
		roots = append(roots, real)
	}
	for _, event := range s.Events() {
		add(event.Cwd)
	}
	if includeNativeSessions {
		for _, summary := range d.summaries(ctx, 300, "", "") {
			if summary.Cwd != nil {
				add(*summary.Cwd)
			}
		}
	}
	return roots
}

func pathWithinRoots(path string, roots []string) bool {
	for _, root := range roots {
		if path == root {
			return true
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func sensitiveFile(path string) bool {
	lower := strings.ToLower(path)
	for _, segment := range strings.Split(lower, string(filepath.Separator)) {
		switch segment {
		case ".ssh", ".gnupg", ".aws", ".git", ".docker", ".kube":
			return true
		}
	}
	base := filepath.Base(lower)
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == ".npmrc" || base == ".netrc" ||
		base == ".pypirc" || base == "credentials" || base == "credentials.json" {
		if base != ".env.example" && base != ".env.sample" && base != ".env.template" {
			return true
		}
	}
	if strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") || strings.HasPrefix(base, "id_ecdsa") {
		return true
	}
	switch filepath.Ext(base) {
	case ".pem", ".key", ".p12", ".pfx", ".keystore", ".jks", ".mobileprovision":
		return true
	}
	return false
}
