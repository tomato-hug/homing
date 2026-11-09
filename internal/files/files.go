// Package files owns the two filesystem-facing pieces of the local relay:
// short-lived capability links (/f/<token>/...) and the file list carried by a
// phone reply. The capability format is deliberately the same JSON file the
// Node relay writes, so a data directory can be switched between the two.
package files

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	MaxAttachmentBytes = 50 * 1024 * 1024
	MaxAttachments     = 20
	CapabilityTTL      = 7 * 24 * time.Hour
)

var (
	ErrNotFound  = errors.New("文件不存在")
	ErrForbidden = errors.New("禁止访问")
	ErrExpired   = errors.New("链接已失效")
)

// Attachment is the object embedded in an event. Keep the field names and
// explicit values identical to relay/server.mjs.
type Attachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

// Capability is one entry in capabilities.json.
type Capability struct {
	Root      string `json:"root"`
	Entry     string `json:"entry"`
	ExpiresAt int64  `json:"expiresAt"`
	Created   string `json:"created"`
}

// Manager reads and writes capabilities.json for one data directory.
type Manager struct {
	path string

	mu   sync.Mutex
	caps map[string]Capability
}

var managers sync.Map // data directory -> *Manager

// ManagerFor returns the process-wide capability manager for dataDir.
func ManagerFor(dataDir string) *Manager {
	clean := filepath.Clean(dataDir)
	if existing, ok := managers.Load(clean); ok {
		return existing.(*Manager)
	}
	m := &Manager{
		path: filepath.Join(clean, "capabilities.json"),
		caps: map[string]Capability{},
	}
	m.load()
	actual, _ := managers.LoadOrStore(clean, m)
	return actual.(*Manager)
}

func (m *Manager) load() {
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var stored map[string]Capability
	if json.Unmarshal(raw, &stored) != nil {
		return
	}
	now := time.Now().UnixMilli()
	for token, cap := range stored {
		if cap.Root == "" || cap.Entry == "" || cap.ExpiresAt <= now {
			continue
		}
		m.caps[strings.ToLower(token)] = cap
	}
}

func (m *Manager) saveLocked() {
	raw, err := json.MarshalIndent(m.caps, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), "capabilities.json.tmp-*")
	if err != nil {
		return
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(name, m.path)
}

// Lookup returns a live capability. Expired entries are removed on access.
func (m *Manager) Lookup(token string) (Capability, bool) {
	token = strings.ToLower(strings.TrimSpace(token))
	if !validToken(token) {
		return Capability{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cap, ok := m.caps[token]
	if !ok {
		return Capability{}, false
	}
	if cap.ExpiresAt <= time.Now().UnixMilli() {
		delete(m.caps, token)
		m.saveLocked()
		return Capability{}, false
	}
	return cap, true
}

// Create reuses the token for the same root+entry and creates a fresh one
// otherwise, like the Node relay's createCapability.
func (m *Manager) Create(root, entry string) (string, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", ErrNotFound
	}
	entry = filepath.Base(entry)
	if entry == "" || entry == "." {
		return "", ErrNotFound
	}
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	for token, cap := range m.caps {
		if cap.Root == root && cap.Entry == entry && cap.ExpiresAt > now.UnixMilli() {
			return token, nil
		}
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	m.caps[token] = Capability{
		Root:      root,
		Entry:     entry,
		ExpiresAt: now.Add(CapabilityTTL).UnixMilli(),
		Created:   now.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	m.saveLocked()
	return token, nil
}

// Resolve maps a public /f path to a real file, refusing traversal and
// symlinks that leave the capability root.
func (m *Manager) Resolve(token, relative string) (string, error) {
	cap, ok := m.Lookup(token)
	if !ok {
		// 与 Node 一致：未知或已过期的链接是 404（链接已失效），不是 403。
		return "", ErrExpired
	}
	if relative == "" {
		relative = cap.Entry
	}
	if strings.ContainsRune(relative, '\x00') || strings.Contains(relative, `\`) {
		return "", ErrForbidden
	}
	if decoded, err := url.PathUnescape(relative); err == nil {
		relative = decoded
	}
	parts := strings.FieldsFunc(relative, func(r rune) bool { return r == '/' })
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", ErrForbidden
		}
		clean = append(clean, part)
	}
	target := filepath.Join(append([]string{cap.Root}, clean...)...)
	if !within(cap.Root, target) {
		return "", ErrForbidden
	}
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", ErrNotFound
	}
	if !within(cap.Root, real) {
		return "", ErrForbidden
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", ErrNotFound
	}
	if info.IsDir() {
		index := filepath.Join(real, "index.html")
		if stat, err := os.Stat(index); err == nil && stat.Mode().IsRegular() {
			return index, nil
		}
		return "", ErrNotFound
	}
	if !info.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return real, nil
}

// Build extracts files referenced by an agent message and creates capability
// links for each. Invalid/missing paths are ignored; this must never make an
// incoming hook fail.
func (m *Manager) Build(text, cwd string) []Attachment {
	if strings.TrimSpace(text) == "" {
		return []Attachment{}
	}
	out := make([]Attachment, 0, 4)
	seen := map[string]bool{}
	roots := allowedRoots(cwd)
	for _, candidate := range candidates(text) {
		if len(out) >= MaxAttachments {
			break
		}
		expanded := expand(candidate)
		if expanded == "" {
			continue
		}
		if !filepath.IsAbs(expanded) {
			base := strings.TrimSpace(cwd)
			if base == "" {
				base, _ = os.UserHomeDir()
			}
			expanded = filepath.Join(base, expanded)
		}
		real, err := filepath.EvalSymlinks(expanded)
		if err != nil {
			continue
		}
		info, err := os.Stat(real)
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxAttachmentBytes {
			continue
		}
		if !allowed(roots, real) || blocked(real) || seen[real] {
			continue
		}
		seen[real] = true
		name := filepath.Base(real)
		token, err := m.Create(filepath.Dir(real), name)
		if err != nil {
			continue
		}
		id := make([]byte, 8)
		if _, err := rand.Read(id); err != nil {
			continue
		}
		out = append(out, Attachment{
			ID:   hex.EncodeToString(id),
			Name: name,
			Mime: MimeFor(name),
			Size: info.Size(),
			Kind: KindFor(name),
			URL:  "/f/" + token + "/" + url.PathEscape(name),
		})
	}
	return out
}

// NormalizeFiles turns the phone's file list into existing absolute paths.
func NormalizeFiles(input []string) []string {
	out := make([]string, 0, len(input))
	seen := map[string]bool{}
	for _, raw := range input {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		absolute, err := filepath.Abs(value)
		if err != nil {
			continue
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.Mode().IsRegular() || seen[absolute] {
			continue
		}
		seen[absolute] = true
		out = append(out, absolute)
		if len(out) >= MaxAttachments {
			break
		}
	}
	return out
}

// DeliveryText appends the same human-readable file note the Node relay sends
// to the agent.
func DeliveryText(text string, files []string) string {
	if len(files) == 0 {
		return text
	}
	lines := make([]string, 0, len(files))
	for _, file := range files {
		lines = append(lines, "- "+file)
	}
	return text + "\n\n用户从手机上传了文件：\n" + strings.Join(lines, "\n")
}

// MimeFor maps an extension to the content type used by /f and /api/upload.
func MimeFor(name string) string {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "svg":
		return "image/svg+xml"
	case "bmp":
		return "image/bmp"
	case "ico":
		return "image/x-icon"
	case "avif":
		return "image/avif"
	case "html", "htm":
		return "text/html; charset=utf-8"
	case "css":
		return "text/css; charset=utf-8"
	case "js", "mjs":
		return "text/javascript; charset=utf-8"
	case "json", "jsonl":
		return "application/json; charset=utf-8"
	case "txt":
		return "text/plain; charset=utf-8"
	case "md":
		return "text/markdown; charset=utf-8"
	case "csv":
		return "text/csv; charset=utf-8"
	case "xml":
		return "application/xml; charset=utf-8"
	case "yml", "yaml":
		return "text/yaml; charset=utf-8"
	case "pdf":
		return "application/pdf"
	case "mp4":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "zip":
		return "application/zip"
	}
	return "application/octet-stream"
}

// KindFor is the coarse bucket the Android attachment card switches on.
func KindFor(name string) string {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif":
		return "image"
	case "html", "htm":
		return "html"
	case "pdf":
		return "pdf"
	}
	return "file"
}

// SanitizeName removes path components/control characters and caps the name at
// 120 runes. Unicode letters are kept so Chinese file names work.
func SanitizeName(raw string) string {
	raw = strings.ReplaceAll(raw, `\`, "/")
	base := filepath.Base(raw)
	base = strings.Map(func(r rune) rune {
		if r < 0x20 {
			return -1
		}
		return r
	}, base)
	var b strings.Builder
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := strings.TrimLeft(b.String(), ".")
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	return name
}

func validToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, r := range token {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func within(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	return target == root || strings.HasPrefix(target, root+string(filepath.Separator))
}

func allowedRoots(cwd string) []string {
	var out []string
	for _, candidate := range []string{strings.TrimSpace(cwd), home()} {
		if candidate == "" {
			continue
		}
		real, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(real); err != nil || !info.IsDir() {
			continue
		}
		duplicate := false
		for _, existing := range out {
			if existing == real {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, real)
		}
	}
	return out
}

func allowed(roots []string, file string) bool {
	for _, root := range roots {
		if within(root, file) {
			return true
		}
	}
	return false
}

func home() string {
	if value := strings.TrimSpace(os.Getenv("HOME")); value != "" {
		return value
	}
	value, _ := os.UserHomeDir()
	return value
}

var blockedDirs = map[string]bool{
	".ssh": true, ".gnupg": true, ".aws": true, ".azure": true,
	".kube": true, ".docker": true, "keychains": true,
}

func blocked(file string) bool {
	lower := strings.ToLower(filepath.Clean(file))
	base := filepath.Base(lower)
	for _, word := range []string{"token", "secret", "key", "credential"} {
		if strings.Contains(base, word) {
			return true
		}
	}
	for _, part := range strings.Split(lower, string(filepath.Separator)) {
		if blockedDirs[part] {
			return true
		}
	}
	if strings.HasSuffix(lower, filepath.Join(".codex", "auth.json")) {
		return true
	}
	if strings.Contains(lower, string(filepath.Separator)+".claude"+string(filepath.Separator)) {
		for _, word := range []string{"credential", "auth", "secret", "token", "key", ".env"} {
			if strings.Contains(base, word) {
				return true
			}
		}
	}
	return false
}

var candidatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`),
	regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`),
	regexp.MustCompile("`([^`\\n]{1,300})`"),
	regexp.MustCompile(`(?:^|[\s（(：:"'` + "`" + `])((?:~|\.{1,2})?/[^\s"'` + "`" + `()<>\[\]，。；、）]+)`),
	regexp.MustCompile(`(?:^|[\s（(：:"'` + "`" + `])([A-Za-z0-9._-]+(?:/[A-Za-z0-9._ -]+)*\.[A-Za-z0-9]{1,8})(?:$|[\s"'` + "`" + `)，。；、）])`),
}

func candidates(text string) []string {
	found := map[string]bool{}
	var out []string
	for _, pattern := range candidatePatterns {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			if len(match) < 2 {
				continue
			}
			value := strings.TrimSpace(match[1])
			value = strings.Trim(value, `<>()"'`+"`")
			value = strings.TrimRight(value, `>)"'.,;:，。；：）`)
			if value == "" || found[value] {
				continue
			}
			found[value] = true
			out = append(out, value)
		}
	}
	return out
}

func expand(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "<>")
	lower := strings.ToLower(value)
	for _, prefix := range []string{"http:", "https:", "data:", "mailto:", "javascript:", "#", "//"} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	if strings.HasPrefix(lower, "file://") {
		value = value[len("file://"):]
	}
	if index := strings.IndexAny(value, "#?"); index >= 0 {
		value = value[:index]
	}
	if value == "" {
		return ""
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		value = filepath.Join(home(), strings.TrimPrefix(value, "~/"))
	}
	return value
}
