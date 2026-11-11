// Package library implements Homing's local capability inventory. It scans
// skill and integration directories owned by the user and by supported agents,
// groups identical names across agents, and imports local folders into Homing's
// own managed library with a plan/dry-run and recoverable removal.
//
// L1 deliberately does not fetch remote packs or execute install scripts. A
// resource is only shown as installed when its files already exist on disk.
package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	KindSkill        = "skill"
	KindInstructions = "instructions"
	KindMCP          = "mcp"
	KindHook         = "hook"
	KindWorkflow     = "workflow"
	KindPlugin       = "plugin"
	KindContext      = "context"
	KindPack         = "pack"

	TrustLocalDraft = "local-draft"

	maxInspectFiles = 5000
	maxInspectBytes = 100 << 20
	maxHashBytes    = 32 << 20
)

var (
	ErrNotFound    = errors.New("library resource not found")
	ErrUnsafePath  = errors.New("unsafe library path")
	ErrDestination = errors.New("library destination already exists")
)

// Target is one agent layout containing a resource. Managed is true only for
// paths Homing may move to its recoverable trash directory.
type Target struct {
	Agent   string `json:"agent"`
	Label   string `json:"label"`
	Path    string `json:"path"`
	Entry   string `json:"entry,omitempty"`
	Managed bool   `json:"managed"`
}

// Resource is one logical capability, even when the same skill is installed in
// several agents. IDs are stable across rescans and do not expose an absolute
// path in URLs.
type Resource struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Source      string   `json:"source"`
	Trust       string   `json:"trust"`
	Scope       string   `json:"scope"`
	State       string   `json:"state"`
	Targets     []Target `json:"targets"`
	FileCount   int      `json:"file_count"`
	SizeBytes   int64    `json:"size_bytes"`
	ModifiedAt  string   `json:"modified_at"`
	Removable   bool     `json:"removable"`
	Warnings    []string `json:"warnings,omitempty"`
}

// ResourceFile is a bounded preview of the files in one resource.
type ResourceFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"modified_at"`
}

// ResourceDetail adds a file preview to the inventory row.
type ResourceDetail struct {
	Resource
	Files []ResourceFile `json:"files"`
}

// Source describes a scanned root. Existing false is useful in the console: it
// explains that a detected agent simply has no local skills yet.
type Source struct {
	ID      string `json:"id"`
	Agent   string `json:"agent"`
	Label   string `json:"label"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Managed bool   `json:"managed"`
	Exists  bool   `json:"exists"`
	Count   int    `json:"count"`
	Note    string `json:"note,omitempty"`
}

// Snapshot is what GET /api/library returns.
type Snapshot struct {
	Resources   []Resource     `json:"resources"`
	Sources     []Source       `json:"sources"`
	Counts      map[string]int `json:"counts"`
	Total       int            `json:"total"`
	Managed     int            `json:"managed"`
	GeneratedAt string         `json:"generated_at"`
}

type root struct {
	id      string
	agent   string
	label   string
	path    string
	kind    string
	managed bool
	note    string
}

func roots(home string) []root {
	return []root{
		{id: "homing-skills", agent: "homing", label: "Homing", path: filepath.Join(home, ".homing", "skills"), kind: KindSkill, managed: true, note: "Homing 管理的技能，可安全移入回收區。"},
		{id: "homing-instructions", agent: "homing", label: "Homing", path: filepath.Join(home, ".homing", "instructions"), kind: KindInstructions, managed: true, note: "Homing 管理的指令片段。"},
		{id: "homing-mcp", agent: "homing", label: "Homing", path: filepath.Join(home, ".homing", "resources", KindMCP), kind: KindMCP, managed: true},
		{id: "homing-hooks", agent: "homing", label: "Homing", path: filepath.Join(home, ".homing", "resources", KindHook), kind: KindHook, managed: true},
		{id: "homing-workflows", agent: "homing", label: "Homing", path: filepath.Join(home, ".homing", "resources", KindWorkflow), kind: KindWorkflow, managed: true},
		{id: "homing-plugins", agent: "homing", label: "Homing", path: filepath.Join(home, ".homing", "resources", KindPlugin), kind: KindPlugin, managed: true},
		{id: "homing-context", agent: "homing", label: "Homing", path: filepath.Join(home, ".homing", "resources", KindContext), kind: KindContext, managed: true},
		{id: "homing-packs", agent: "homing", label: "Homing", path: filepath.Join(home, ".homing", "resources", KindPack), kind: KindPack, managed: true},
		{id: "codex-rules", agent: "codex", label: "Codex", path: filepath.Join(home, ".codex", "rules"), kind: KindInstructions, note: "Codex 原生規則檔；只讀掃描，不自動改寫。"},
		{id: "claude-agents", agent: "claude", label: "Claude Code", path: filepath.Join(home, ".claude", "agents"), kind: KindWorkflow, note: "Claude Code 子代理定義；只讀掃描。"},
		{id: "codex-skills", agent: "codex", label: "Codex", path: filepath.Join(home, ".codex", "skills"), kind: KindSkill, note: "Codex 原生技能目錄。"},
		{id: "claude-skills", agent: "claude", label: "Claude Code", path: filepath.Join(home, ".claude", "skills"), kind: KindSkill, note: "Claude Code 原生技能目錄。"},
		{id: "workbuddy-skills", agent: "workbuddy", label: "WorkBuddy", path: filepath.Join(home, ".workbuddy", "skills"), kind: KindSkill, note: "WorkBuddy 原生技能目錄；只讀掃描。"},
		{id: "agents-skills", agent: "agents", label: "Agent Skills", path: filepath.Join(home, ".agents", "skills"), kind: KindSkill, note: "跨 Agent 共用的 Agent Skills 目錄。"},
		{id: "kimi-skills", agent: "kimi", label: "Kimi Code", path: filepath.Join(home, ".kimi-code", "skills"), kind: KindSkill},
		{id: "gemini-skills", agent: "gemini-cli", label: "Gemini CLI", path: filepath.Join(home, ".gemini", "skills"), kind: KindSkill},
		{id: "antigravity-skills", agent: "antigravity", label: "Google Antigravity", path: filepath.Join(home, ".gemini", "antigravity", "skills"), kind: KindSkill},
		{id: "opencode-skills", agent: "opencode", label: "OpenCode", path: filepath.Join(home, ".opencode", "skills"), kind: KindSkill},
		{id: "opencode-config-skills", agent: "opencode", label: "OpenCode", path: filepath.Join(home, ".config", "opencode", "skills"), kind: KindSkill, note: "OpenCode 新式設定目錄；相容舊版 ~/.opencode。"},
		{id: "dsh-presets", agent: "dsh", label: "DeepSeek Harness", path: filepath.Join(home, ".dsh", ".agent-presets"), kind: KindWorkflow, note: "DSH agent preset；只讀掃描，不執行 preset。"},
		{id: "homing-integrations", agent: "homing", label: "Homing 接入套件", path: filepath.Join(home, ".homing", "integrations"), kind: KindHook, managed: true, note: "為各家 Agent 產生的 hook、命令包裝器與適配器骨架。"},
	}
}

// Scan builds the library inventory. Missing roots are reported as absent,
// never treated as an error.
func Scan(home string) Snapshot {
	out := Snapshot{
		Resources:   []Resource{},
		Sources:     []Source{},
		Counts:      map[string]int{},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	grouped := map[string]*Resource{}
	managedSets := map[string][]Target{}

	for _, r := range roots(home) {
		src := Source{
			ID: r.id, Agent: r.agent, Label: r.label, Path: r.path,
			Kind: r.kind, Managed: r.managed, Note: r.note,
		}
		if info, err := os.Stat(r.path); err == nil && info.IsDir() {
			src.Exists = true
			found := scanRoot(r)
			src.Count = len(found)
			for _, item := range found {
				key := resourceKey(item.Kind, item.Name)
				if existing := grouped[key]; existing != nil {
					existing.Targets = append(existing.Targets, item.Targets...)
					existing.FileCount += item.FileCount
					existing.SizeBytes += item.SizeBytes
					if item.ModifiedAt > existing.ModifiedAt {
						existing.ModifiedAt = item.ModifiedAt
					}
					existing.Warnings = mergeStrings(existing.Warnings, item.Warnings)
					if item.Description != "" && existing.Description == "" {
						existing.Description = item.Description
					}
					if item.Version != "" && existing.Version == "" {
						existing.Version = item.Version
					}
					continue
				}
				item.ID = resourceID(item.Kind, item.Name)
				grouped[key] = &item
			}
		}
		out.Sources = append(out.Sources, src)
	}

	for _, item := range grouped {
		sort.SliceStable(item.Targets, func(i, j int) bool {
			if item.Targets[i].Agent != item.Targets[j].Agent {
				return item.Targets[i].Agent < item.Targets[j].Agent
			}
			return item.Targets[i].Path < item.Targets[j].Path
		})
		item.Targets = dedupeTargets(item.Targets)
		for _, target := range item.Targets {
			if target.Managed {
				item.Removable = true
				managedSets[item.ID] = append(managedSets[item.ID], target)
			}
		}
		if item.State == "" {
			item.State = "ready"
		}
		out.Resources = append(out.Resources, *item)
	}
	sort.SliceStable(out.Resources, func(i, j int) bool {
		a, b := out.Resources[i], out.Resources[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.ID < b.ID
	})
	out.Total = len(out.Resources)
	for _, resource := range out.Resources {
		out.Counts[resource.Kind]++
		if resource.Removable {
			out.Managed++
		}
	}
	return out
}

func scanRoot(r root) []Resource {
	entries, err := os.ReadDir(r.path)
	if err != nil {
		return nil
	}
	if r.kind == KindInstructions {
		var out []Resource
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext != ".md" && ext != ".txt" && ext != ".rules" {
				continue
			}
			path := filepath.Join(r.path, entry.Name())
			if item, ok := resourceFromFile(r, path); ok {
				out = append(out, item)
			}
		}
		return out
	}
	var out []Resource
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(r.path, entry.Name())
		isDir := entry.IsDir()
		linked := false
		if !isDir && entry.Type()&fs.ModeSymlink != 0 {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				isDir = true
				linked = true
			}
		}
		if !isDir {
			continue
		}
		if item, ok := resourceFromDir(r, path, entry.Name()); ok {
			if linked {
				item.Warnings = appendUnique(item.Warnings, "这是一个符号链接；资源庫只读展示，不会跟随链接删除目标")
			}
			out = append(out, item)
		}
	}
	return out
}

func resourceFromDir(r root, path, fallbackName string) (Resource, bool) {
	entry, meta := resourceEntry(path)
	if entry == "" {
		return Resource{}, false
	}
	item := Resource{
		Kind: r.kind, Name: firstNonEmpty(meta.Name, titleName(fallbackName)),
		Description: meta.Description, Version: meta.Version,
		Source: sourceForRoot(r), Trust: TrustLocalDraft, Scope: "global",
		State: "ready",
		Targets: []Target{{
			Agent: r.agent, Label: r.label, Path: path,
			Entry: filepath.Base(entry), Managed: r.managed,
		}},
	}
	scanPath := path
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
			scanPath = resolved
		}
	}
	item.FileCount, item.SizeBytes, item.ModifiedAt, item.Warnings = statTree(scanPath)
	if r.kind == KindHook && strings.HasSuffix(path, filepath.Join("integrations", filepath.Base(path))) {
		item.nameFromKit(path)
	}
	return item, true
}

func resourceFromFile(r root, path string) (Resource, bool) {
	meta := readMetadata(path)
	item := Resource{
		Kind: r.kind, Name: firstNonEmpty(meta.Name, titleName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))),
		Description: meta.Description, Version: meta.Version,
		Source: sourceForRoot(r), Trust: TrustLocalDraft, Scope: "global",
		State: "ready",
		Targets: []Target{{
			Agent: r.agent, Label: r.label, Path: path,
			Entry: filepath.Base(path), Managed: r.managed,
		}},
	}
	info, err := os.Stat(path)
	if err != nil {
		return Resource{}, false
	}
	item.FileCount = 1
	item.SizeBytes = info.Size()
	item.ModifiedAt = info.ModTime().UTC().Format(time.RFC3339)
	return item, true
}

func (r *Resource) nameFromKit(path string) {
	readme := filepath.Join(path, "README.md")
	raw, err := os.ReadFile(readme)
	if err != nil {
		return
	}
	lines := strings.Split(string(raw), "\n")
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, ">") ||
			strings.HasPrefix(trimmed, "-") ||
			strings.HasPrefix(trimmed, "*") ||
			strings.HasPrefix(trimmed, "+") ||
			strings.HasPrefix(trimmed, "|") ||
			strings.HasPrefix(trimmed, "![") ||
			strings.HasPrefix(trimmed, "[![") {
			continue
		}
		if len([]rune(trimmed)) > 12 {
			r.Description = truncateRunes(trimmed, 180)
		}
		break
	}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "# ") {
		first := strings.SplitN(strings.TrimSpace(string(raw)), "\n", 2)[0]
		r.Name = strings.TrimSpace(strings.TrimPrefix(first, "# "))
	}
}

func sourceForRoot(r root) string {
	if r.managed {
		return "Homing 本機"
	}
	return r.label + " 本機"
}

type metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
}

func readMetadata(path string) metadata {
	file := path
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		file = filepath.Join(path, "SKILL.md")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return metadata{}
	}
	if len(raw) > 128<<10 {
		raw = raw[:128<<10]
	}
	text := strings.TrimSpace(strings.ReplaceAll(string(raw), "\r\n", "\n"))
	if !strings.HasPrefix(text, "---\n") {
		return metadata{}
	}
	rest := strings.TrimPrefix(text, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return metadata{}
	}
	var meta metadata
	if yaml.Unmarshal([]byte(rest[:end]), &meta) != nil {
		return metadata{}
	}
	meta.Name = truncateRunes(strings.TrimSpace(meta.Name), 100)
	meta.Description = truncateRunes(strings.TrimSpace(meta.Description), 240)
	meta.Version = truncateRunes(strings.TrimSpace(meta.Version), 40)
	return meta
}

func resourceEntry(dir string) (string, metadata) {
	for _, candidate := range []string{
		"SKILL.md", "README.md", "homing.resource.yaml", "manifest.yaml",
		"preset.yml", "preset.yaml", "agent.yml", "agent.yaml",
	} {
		path := filepath.Join(dir, candidate)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, readMetadata(path)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", metadata{}
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			path := filepath.Join(dir, entry.Name())
			return path, readMetadata(path)
		}
	}
	return "", metadata{}
}

func statTree(path string) (files int, bytes int64, modified string, warnings []string) {
	var latest time.Time
	walkErr := filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			warnings = appendUnique(warnings, "部分文件无法读取："+err.Error())
			return nil
		}
		if current != path && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			warnings = appendUnique(warnings, "包含符号链接，扫描与导入不会跟随")
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		files++
		bytes += info.Size()
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		if files > maxInspectFiles {
			return fs.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		warnings = appendUnique(warnings, walkErr.Error())
	}
	if latest.IsZero() {
		latest = time.Now()
	}
	return files, bytes, latest.UTC().Format(time.RFC3339), warnings
}

// Inspection is the JSON returned before an import. Checksum is SHA-256 over
// relative paths and file bytes; it is informational in L1 and becomes the
// source lock when remote installs arrive in L2.
type Inspection struct {
	Path        string         `json:"path"`
	Name        string         `json:"name"`
	Kind        string         `json:"kind"`
	Description string         `json:"description,omitempty"`
	Version     string         `json:"version,omitempty"`
	FileCount   int            `json:"file_count"`
	SizeBytes   int64          `json:"size_bytes"`
	Checksum    string         `json:"checksum,omitempty"`
	Files       []ResourceFile `json:"files"`
	Warnings    []string       `json:"warnings,omitempty"`
	SuggestedID string         `json:"suggested_id"`
}

// Inspect validates one local file or folder without writing anything.
func Inspect(rawPath, kind string) (Inspection, error) {
	path, err := expandPath(rawPath)
	if err != nil {
		return Inspection{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Inspection{}, fmt.Errorf("路径不存在：%w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Inspection{}, fmt.Errorf("%w: 不导入符号链接", ErrUnsafePath)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return Inspection{}, fmt.Errorf("%w: 只支持普通文件或目录", ErrUnsafePath)
	}
	kind = normalizeKind(kind)
	if kind == "" {
		kind = detectKind(path)
	}
	meta := metadata{}
	files, size, checksum, warnings, err := inspectFiles(path)
	if err != nil {
		return Inspection{}, err
	}
	if info.IsDir() {
		entry, _ := resourceEntry(path)
		if entry != "" {
			meta = readMetadata(entry)
		}
		if entry == "" {
			warnings = appendUnique(warnings, "目录里没有 SKILL.md、README.md 或说明文件")
		}
	} else {
		meta = readMetadata(path)
	}
	name := firstNonEmpty(meta.Name, titleName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))))
	if info.IsDir() {
		name = firstNonEmpty(meta.Name, titleName(filepath.Base(path)))
	}
	return Inspection{
		Path: path, Name: name, Kind: kind, Description: meta.Description,
		Version: meta.Version, FileCount: files, SizeBytes: size,
		Checksum: checksum, Files: previewFiles(path), Warnings: warnings,
		SuggestedID: resourceID(kind, name),
	}, nil
}

// PlanAction is one file operation in an import plan.
type PlanAction struct {
	Action    string `json:"action"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	FileCount int    `json:"file_count,omitempty"`
}

// ImportPlan is the dry-run representation shown before an import.
type ImportPlan struct {
	OK          bool         `json:"ok"`
	Source      string       `json:"source"`
	Destination string       `json:"destination"`
	Kind        string       `json:"kind"`
	Name        string       `json:"name"`
	Actions     []PlanAction `json:"actions"`
	Warnings    []string     `json:"warnings,omitempty"`
	Inspection  Inspection   `json:"inspection"`
}

// BuildImportPlan validates the source and destination without creating files.
func BuildImportPlan(home, rawPath, kind, name string) (ImportPlan, error) {
	return buildImportPlan(home, rawPath, kind, name, false)
}

func buildImportPlan(home, rawPath, kind, name string, allowExisting bool) (ImportPlan, error) {
	inspection, err := Inspect(rawPath, kind)
	if err != nil {
		return ImportPlan{}, err
	}
	if strings.TrimSpace(name) != "" {
		inspection.Name = truncateRunes(strings.TrimSpace(name), 100)
	}
	if inspection.Name == "" {
		return ImportPlan{}, errors.New("资源名称不能为空")
	}
	destination, err := destinationFor(home, inspection.Kind, inspection.Name)
	if err != nil {
		return ImportPlan{}, err
	}
	if _, err := os.Stat(destination); err == nil {
		if !allowExisting {
			return ImportPlan{}, fmt.Errorf("%w: %s", ErrDestination, destination)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ImportPlan{}, err
	}
	if isWithin(inspection.Path, filepath.Dir(destination)) {
		return ImportPlan{}, fmt.Errorf("%w: 资源已在本机资源库内", ErrDestination)
	}
	return ImportPlan{
		OK: true, Source: inspection.Path, Destination: destination,
		Kind: inspection.Kind, Name: inspection.Name,
		Actions: []PlanAction{{
			Action: "copy", From: inspection.Path, To: destination,
			FileCount: inspection.FileCount,
		}},
		Warnings: inspection.Warnings, Inspection: inspection,
	}, nil
}

// Install copies an inspected local file/folder into the managed library. The
// copy is staged beside the destination and renamed only after the full tree
// has been written, so a failure never leaves a half-installed resource.
func Install(home, rawPath, kind, name string) (ImportPlan, error) {
	plan, err := BuildImportPlan(home, rawPath, kind, name)
	if err != nil {
		return plan, err
	}
	tmp, err := stageCopy(plan.Source, plan.Destination)
	if err != nil {
		return plan, err
	}
	defer os.RemoveAll(tmp)
	if err := os.Rename(tmp, plan.Destination); err != nil {
		return plan, err
	}
	plan.Actions = append(plan.Actions, PlanAction{Action: "commit", To: plan.Destination})
	return plan, nil
}

// stageCopy writes a complete source tree beside its final destination. The
// caller renames the returned directory into place only after the copy and
// validation succeed.
func stageCopy(source, destination string) (string, error) {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, ".import-*")
	if err != nil {
		return "", err
	}
	info, err := os.Stat(source)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	if info.IsDir() {
		err = copyTree(source, tmp)
	} else {
		err = copyFile(source, filepath.Join(tmp, filepath.Base(source)), info.Mode().Perm())
	}
	if err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

// RemoveResult is returned after a recoverable removal.
type RemoveResult struct {
	OK        bool     `json:"ok"`
	ID        string   `json:"id"`
	Moved     []string `json:"moved"`
	TrashPath string   `json:"trash_path"`
}

// Remove moves one managed target to ~/.homing/trash. External agent
// directories are never touched.
func Remove(home, id, rawTarget string) (RemoveResult, error) {
	snapshot := Scan(home)
	var found *Resource
	for i := range snapshot.Resources {
		if snapshot.Resources[i].ID == id {
			found = &snapshot.Resources[i]
			break
		}
	}
	if found == nil {
		return RemoveResult{}, ErrNotFound
	}
	var target *Target
	for i := range found.Targets {
		candidate := &found.Targets[i]
		if !candidate.Managed {
			continue
		}
		if rawTarget == "" || candidate.Path == rawTarget {
			target = candidate
			break
		}
	}
	if target == nil {
		return RemoveResult{}, fmt.Errorf("%w: 该资源只有 Agent 原生目录，Homing 不会直接删除", ErrUnsafePath)
	}
	path, err := filepath.Abs(target.Path)
	if err != nil {
		return RemoveResult{}, err
	}
	homeAbs, _ := filepath.Abs(home)
	managedRoots := []string{
		filepath.Join(homeAbs, ".homing", "skills"),
		filepath.Join(homeAbs, ".homing", "instructions"),
		filepath.Join(homeAbs, ".homing", "resources"),
		filepath.Join(homeAbs, ".homing", "integrations"),
	}
	allowed := false
	for _, root := range managedRoots {
		if isWithin(path, root) && path != root {
			allowed = true
			break
		}
	}
	if !allowed {
		return RemoveResult{}, fmt.Errorf("%w: 路径不在 Homing 管理的资源目录内", ErrUnsafePath)
	}
	trashDir := filepath.Join(homeAbs, ".homing", "trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		return RemoveResult{}, err
	}
	trash := filepath.Join(trashDir, time.Now().UTC().Format("20060102T150405")+"-"+filepath.Base(path))
	if err := os.Rename(path, trash); err != nil {
		return RemoveResult{}, err
	}
	return RemoveResult{OK: true, ID: id, Moved: []string{path}, TrashPath: trash}, nil
}

// Detail returns one resource plus a bounded file listing.
func Detail(home, id string) (ResourceDetail, error) {
	snapshot := Scan(home)
	for _, item := range snapshot.Resources {
		if item.ID != id {
			continue
		}
		detail := ResourceDetail{Resource: item, Files: []ResourceFile{}}
		for _, target := range item.Targets {
			info, err := os.Stat(target.Path)
			if err != nil {
				continue
			}
			if info.IsDir() {
				detail.Files = append(detail.Files, previewFiles(target.Path)...)
				if len(detail.Files) >= 80 {
					detail.Files = detail.Files[:80]
					break
				}
			} else {
				detail.Files = append(detail.Files, ResourceFile{
					Path: filepath.Base(target.Path), Size: info.Size(),
					ModTime: info.ModTime().UTC().Format(time.RFC3339),
				})
			}
		}
		sort.SliceStable(detail.Files, func(i, j int) bool { return detail.Files[i].Path < detail.Files[j].Path })
		return detail, nil
	}
	return ResourceDetail{}, ErrNotFound
}

func previewFiles(path string) []ResourceFile {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
			path = resolved
		}
	}
	var out []ResourceFile
	_ = filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if current != path && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(path, current)
		out = append(out, ResourceFile{
			Path: filepath.ToSlash(rel), Size: info.Size(),
			ModTime: info.ModTime().UTC().Format(time.RFC3339),
		})
		if len(out) >= 80 {
			return fs.SkipAll
		}
		return nil
	})
	return out
}

func inspectFiles(path string) (int, int64, string, []string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, "", nil, err
	}
	if !info.IsDir() {
		if info.Size() > maxInspectBytes {
			return 0, 0, "", nil, errors.New("资源超过 100 MB 上限")
		}
		sum, err := hashFile(path)
		if err != nil {
			return 0, 0, "", nil, err
		}
		return 1, info.Size(), "sha256:" + sum, nil, nil
	}
	hash := sha256.New()
	files := 0
	var size int64
	var warnings []string
	err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			warnings = appendUnique(warnings, "部分文件无法读取："+walkErr.Error())
			return nil
		}
		if current != path && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			warnings = appendUnique(warnings, "包含符号链接；导入会拒绝，不会跟随")
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			warnings = appendUnique(warnings, "包含非普通文件，导入会跳过")
			return nil
		}
		files++
		size += info.Size()
		if files > maxInspectFiles {
			return errors.New("资源文件数超过 5000 上限")
		}
		if size > maxInspectBytes {
			return errors.New("资源超过 100 MB 上限")
		}
		rel, _ := filepath.Rel(path, current)
		_, _ = io.WriteString(hash, filepath.ToSlash(rel))
		_, _ = io.WriteString(hash, "\x00")
		if size <= maxHashBytes {
			raw, readErr := os.ReadFile(current)
			if readErr == nil {
				_, _ = hash.Write(raw)
			}
		}
		return nil
	})
	if err != nil {
		return 0, 0, "", warnings, err
	}
	return files, size, "sha256:" + hex.EncodeToString(hash.Sum(nil)), warnings, nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxHashBytes+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: 不复制符号链接 %s", ErrUnsafePath, rel)
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: 不复制特殊文件 %s", ErrUnsafePath, rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		mode := info.Mode().Perm()
		if mode&0o111 == 0 {
			mode |= 0o600
		}
		return copyFile(current, target, mode)
	})
}

func copyFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func destinationFor(home, kind, name string) (string, error) {
	slug := safeSlug(name)
	if slug == "" {
		return "", errors.New("资源名称无法组成目录名")
	}
	switch normalizeKind(kind) {
	case KindSkill:
		return filepath.Join(home, ".homing", "skills", slug), nil
	case KindInstructions:
		return filepath.Join(home, ".homing", "instructions", slug), nil
	case KindMCP, KindHook, KindWorkflow, KindPlugin, KindContext, KindPack:
		return filepath.Join(home, ".homing", "resources", normalizeKind(kind), slug), nil
	default:
		return "", errors.New("不支持的资源类型")
	}
}

func detectKind(path string) string {
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		if strings.EqualFold(filepath.Ext(path), ".md") {
			return KindInstructions
		}
		return KindSkill
	}
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(base, "mcp"):
		return KindMCP
	case strings.Contains(base, "hook"):
		return KindHook
	case strings.Contains(base, "skill"):
		return KindSkill
	case strings.Contains(base, "prompt"), strings.Contains(base, "instruction"):
		return KindInstructions
	}
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil {
		return KindSkill
	}
	return KindSkill
}

func normalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindSkill:
		return KindSkill
	case KindInstructions, "instruction", "rule", "rules":
		return KindInstructions
	case KindMCP:
		return KindMCP
	case KindHook, "hooks":
		return KindHook
	case KindWorkflow, "subagent", "workflow/subagent":
		return KindWorkflow
	case KindPlugin, "ui-plugin":
		return KindPlugin
	case KindContext, "memory":
		return KindContext
	case KindPack:
		return KindPack
	default:
		return ""
	}
}

func expandPath(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("path 不能为空")
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return absolute, nil
}

func resourceKey(kind, name string) string {
	return normalizeKind(kind) + "|" + strings.ToLower(strings.TrimSpace(name))
}

func resourceID(kind, name string) string {
	sum := sha256.Sum256([]byte(resourceKey(kind, name)))
	return "res_" + hex.EncodeToString(sum[:8])
}

func safeSlug(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ', r == '/', r == '\\':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-._")
}

func titleName(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, filepath.Ext(value)))
	if value == "" {
		return "未命名资源"
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '-' || r == '_' })
	for i, part := range parts {
		runes := []rune(part)
		if len(runes) > 0 {
			parts[i] = strings.ToUpper(string(runes[0])) + string(runes[1:])
		}
	}
	return truncateRunes(strings.Join(parts, " "), 100)
}

func isWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func dedupeTargets(in []Target) []Target {
	seen := map[string]bool{}
	out := make([]Target, 0, len(in))
	for _, target := range in {
		key := target.Agent + "\x00" + target.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, target)
	}
	return out
}

func mergeStrings(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, value := range b {
		out = appendUnique(out, value)
	}
	return out
}

func appendUnique(list []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return list
	}
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
