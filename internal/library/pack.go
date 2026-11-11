package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	PackManifestFile = "homing.pack.yaml"
	packPlanTTL      = 30 * time.Minute
	maxPackResources = 40
	maxPackFiles     = 20000
	maxPackBytes     = 300 << 20
)

var (
	ErrPackPlanMissing = errors.New("library: pack plan expired or missing")
	ErrPackPlanStale   = errors.New("library: pack source changed after planning")
	ErrPackConflict    = errors.New("library: pack destination already exists")
)

// PackManifest describes a local bundle whose members are installed as one
// transaction. Resource paths are relative to the pack root and never fetched
// or executed by the library.
type PackManifest struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Metadata   struct {
		ID          string   `yaml:"id" json:"id"`
		Name        string   `yaml:"name" json:"name"`
		Version     string   `yaml:"version" json:"version"`
		Description string   `yaml:"description,omitempty" json:"description,omitempty"`
		Source      string   `yaml:"source,omitempty" json:"source,omitempty"`
		Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	} `yaml:"metadata" json:"metadata"`
	Spec struct {
		Resources []PackResourceSpec `yaml:"resources" json:"resources"`
	} `yaml:"spec" json:"spec"`
	Path string `yaml:"-" json:"path,omitempty"`
}

// PackTarget is the portable target declaration used by both pack members and
// resource manifests. Only verified layout adapters are accepted.
type PackTarget struct {
	Agent string `yaml:"agent" json:"agent"`
	Mode  string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type PackResourceSpec struct {
	ID       string       `yaml:"id" json:"id"`
	Path     string       `yaml:"path" json:"path"`
	Kind     string       `yaml:"kind" json:"kind"`
	Name     string       `yaml:"name,omitempty" json:"name,omitempty"`
	Targets  []PackTarget `yaml:"targets" json:"targets"`
	Requires []string     `yaml:"requires,omitempty" json:"requires,omitempty"`
}

type PackTargetPlan struct {
	Agent       string       `json:"agent"`
	Mode        string       `json:"mode"`
	Destination string       `json:"destination"`
	Actions     []PlanAction `json:"actions,omitempty"`
}

// PackMemberPlan is one fully validated resource and all of its verified
// Agent layouts.
type PackMemberPlan struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Kind      string           `json:"kind"`
	Source    string           `json:"source"`
	Targets   []PackTargetPlan `json:"targets"`
	Requires  []string         `json:"requires,omitempty"`
	FileCount int              `json:"file_count"`
	SizeBytes int64            `json:"size_bytes"`
	Checksum  string           `json:"checksum"`
	Warnings  []string         `json:"warnings,omitempty"`
}

// PackPlan is a dry-run transaction. Installing consumes PlanID and revalidates
// every source checksum before touching a destination.
type PackPlan struct {
	OK           bool             `json:"ok"`
	PlanID       string           `json:"plan_id"`
	Pack         PackManifest     `json:"pack"`
	Root         string           `json:"root"`
	Members      []PackMemberPlan `json:"members"`
	Order        []string         `json:"order"`
	Warnings     []string         `json:"warnings,omitempty"`
	CreatedAt    string           `json:"created_at"`
	ExpiresAt    string           `json:"expires_at"`
	TotalFiles   int              `json:"total_files"`
	TotalBytes   int64            `json:"total_bytes"`
	Destinations []string         `json:"destinations"`
}

type PackInstalledTarget struct {
	Agent       string `json:"agent"`
	Destination string `json:"destination"`
}

type PackInstalledMember struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Kind     string                `json:"kind"`
	Checksum string                `json:"checksum"`
	Targets  []PackInstalledTarget `json:"targets"`
}

// PackInstallation is the append-only transaction record. State is installed
// or rolled_back; individual member files are never included in the audit.
type PackInstallation struct {
	ID           string                `json:"id"`
	PlanID       string                `json:"plan_id"`
	PackID       string                `json:"pack_id"`
	Name         string                `json:"name"`
	Version      string                `json:"version,omitempty"`
	Description  string                `json:"description,omitempty"`
	Source       string                `json:"source,omitempty"`
	Root         string                `json:"root"`
	State        string                `json:"state"`
	InstalledAt  string                `json:"installed_at"`
	RolledBackAt string                `json:"rolled_back_at,omitempty"`
	TrashPath    string                `json:"trash_path,omitempty"`
	Missing      []string              `json:"missing,omitempty"`
	Members      []PackInstalledMember `json:"members"`
}

// ReadPackManifest loads and strictly validates homing.pack.yaml.
func ReadPackManifest(root string) (PackManifest, error) {
	path := filepath.Join(root, PackManifestFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return PackManifest{}, errors.New("pack 缺少 " + PackManifestFile)
	}
	if err != nil {
		return PackManifest{}, err
	}
	if len(raw) > 256<<10 {
		return PackManifest{}, errors.New("pack manifest 超过 256 KB")
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	var manifest PackManifest
	if err := decoder.Decode(&manifest); err != nil {
		return PackManifest{}, fmt.Errorf("parse %s: %w", PackManifestFile, err)
	}
	manifest.Path = path
	if err := ValidatePackManifest(manifest); err != nil {
		return PackManifest{}, err
	}
	return manifest, nil
}

func ValidatePackManifest(manifest PackManifest) error {
	var problems []string
	if !strings.HasPrefix(strings.TrimSpace(manifest.APIVersion), "homing.dev/") {
		problems = append(problems, "apiVersion 必须是 homing.dev/*")
	}
	kind := strings.ToLower(strings.TrimSpace(manifest.Kind))
	if kind != "agentpack" && kind != "pack" {
		problems = append(problems, "kind 必须是 AgentPack")
	}
	if strings.TrimSpace(manifest.Metadata.ID) == "" {
		problems = append(problems, "metadata.id 不能为空")
	}
	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		problems = append(problems, "metadata.name 不能为空")
	}
	if strings.TrimSpace(manifest.Metadata.Version) == "" {
		problems = append(problems, "metadata.version 不能为空")
	}
	if len(manifest.Spec.Resources) == 0 {
		problems = append(problems, "spec.resources 至少需要一个资源")
	}
	if len(manifest.Spec.Resources) > maxPackResources {
		problems = append(problems, fmt.Sprintf("spec.resources 最多 %d 个", maxPackResources))
	}
	seen := map[string]bool{}
	for index, resource := range manifest.Spec.Resources {
		prefix := fmt.Sprintf("resources[%d]", index)
		id := strings.ToLower(strings.TrimSpace(resource.ID))
		if id == "" {
			problems = append(problems, prefix+".id 不能为空")
		} else if seen[id] {
			problems = append(problems, prefix+".id 重复："+id)
		}
		seen[id] = true
		if strings.TrimSpace(resource.Path) == "" {
			problems = append(problems, prefix+".path 不能为空")
		}
		if normalizeKind(resource.Kind) == "" {
			problems = append(problems, prefix+".kind 不受支持")
		}
		if len(resource.Targets) == 0 {
			problems = append(problems, prefix+".targets 不能为空")
		}
	}
	if len(problems) > 0 {
		return errors.New("pack manifest 无效：" + strings.Join(problems, "；"))
	}
	return nil
}

// PlanPack validates a local pack and writes only a plan token under the
// library cache. No destination or Agent configuration is changed.
func PlanPack(home, rawPath string) (PackPlan, error) {
	root, err := expandPath(rawPath)
	if err != nil {
		return PackPlan{}, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return PackPlan{}, fmt.Errorf("pack 路径不存在：%w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return PackPlan{}, fmt.Errorf("%w: pack 根目录必须是普通目录", ErrUnsafePath)
	}
	manifest, err := ReadPackManifest(root)
	if err != nil {
		return PackPlan{}, err
	}
	order, err := packResourceOrder(manifest.Spec.Resources)
	if err != nil {
		return PackPlan{}, err
	}
	byID := make(map[string]PackResourceSpec, len(manifest.Spec.Resources))
	for _, resource := range manifest.Spec.Resources {
		byID[strings.ToLower(strings.TrimSpace(resource.ID))] = resource
	}

	plan := PackPlan{
		OK:           true,
		Pack:         manifest,
		Root:         root,
		Members:      make([]PackMemberPlan, 0, len(order)),
		Order:        make([]string, 0, len(order)),
		CreatedAt:    nowRFC3339(),
		Destinations: []string{},
	}
	seenDestinations := map[string]string{}
	seenResourceIDs := map[string]bool{}
	for _, id := range order {
		resource := byID[id]
		plan.Order = append(plan.Order, id)
		member, planErr := planPackMember(home, root, manifest, resource)
		if planErr != nil {
			return PackPlan{}, planErr
		}
		for _, dependency := range member.Requires {
			if !seenResourceIDs[dependency] {
				return PackPlan{}, fmt.Errorf("pack 依赖顺序无效：%s 需要先安装 %s", member.ID, dependency)
			}
		}
		for _, target := range member.Targets {
			if previous, exists := seenDestinations[target.Destination]; exists {
				return PackPlan{}, fmt.Errorf(
					"pack 资源 %s 与 %s 指向同一目录 %s",
					member.ID, previous, target.Destination,
				)
			}
			seenDestinations[target.Destination] = member.ID
		}
		seenResourceIDs[member.ID] = true
		plan.Members = append(plan.Members, member)
		for _, target := range member.Targets {
			plan.Destinations = append(plan.Destinations, target.Destination)
		}
		plan.TotalFiles += member.FileCount
		plan.TotalBytes += member.SizeBytes
		if plan.TotalFiles > maxPackFiles {
			return PackPlan{}, fmt.Errorf("pack 文件数超过 %d 上限", maxPackFiles)
		}
		if plan.TotalBytes > maxPackBytes {
			return PackPlan{}, fmt.Errorf("pack 总大小超过 %d MB 上限", maxPackBytes>>20)
		}
		plan.Warnings = mergeStrings(plan.Warnings, member.Warnings)
	}

	planID, err := randomID("packplan")
	if err != nil {
		return PackPlan{}, err
	}
	plan.PlanID = planID
	plan.ExpiresAt = time.Now().UTC().Add(packPlanTTL).Format(time.RFC3339)
	cacheDir := packPlanDir(home, planID)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return PackPlan{}, err
	}
	if err := writeJSONAtomic(filepath.Join(cacheDir, "plan.json"), plan); err != nil {
		_ = os.RemoveAll(cacheDir)
		return PackPlan{}, err
	}
	return plan, nil
}

func planPackMember(
	home, root string,
	manifest PackManifest,
	resource PackResourceSpec,
) (PackMemberPlan, error) {
	id := strings.ToLower(strings.TrimSpace(resource.ID))
	source := strings.TrimSpace(resource.Path)
	if filepath.IsAbs(source) {
		return PackMemberPlan{}, fmt.Errorf("%w: %s 的 path 不能是绝对路径", ErrUnsafePath, id)
	}
	source = filepath.Clean(filepath.Join(root, filepath.FromSlash(source)))
	if source == root || !isWithin(source, root) {
		return PackMemberPlan{}, fmt.Errorf("%w: %s 的 path 越出 pack 根目录", ErrUnsafePath, id)
	}
	info, err := os.Lstat(source)
	if err != nil {
		return PackMemberPlan{}, fmt.Errorf("pack 资源 %s 不存在：%w", id, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return PackMemberPlan{}, fmt.Errorf("%w: %s 必须是普通目录，不能是符号链接", ErrUnsafePath, id)
	}
	kind := normalizeKind(resource.Kind)
	inspection, err := Inspect(source, kind)
	if err != nil {
		return PackMemberPlan{}, fmt.Errorf("pack 资源 %s：%w", id, err)
	}
	name := firstNonEmpty(strings.TrimSpace(resource.Name), inspection.Name)
	if name == "" {
		name = titleName(filepath.Base(source))
	}
	targets := resource.Targets
	if len(targets) == 0 {
		targets = []PackTarget{{Agent: "homing", Mode: "managed"}}
	}

	member := PackMemberPlan{
		ID: id, Name: truncateRunes(name, 100), Kind: kind, Source: source,
		Requires:  normalizePackIDs(resource.Requires),
		FileCount: inspection.FileCount, SizeBytes: inspection.SizeBytes,
		Checksum: inspection.Checksum, Warnings: append([]string(nil), inspection.Warnings...),
	}
	for _, target := range targets {
		agent := strings.ToLower(strings.TrimSpace(target.Agent))
		mode := strings.ToLower(strings.TrimSpace(target.Mode))
		destination, layoutErr := packLayoutDestination(home, kind, member.Name, agent, mode)
		if layoutErr != nil {
			return PackMemberPlan{}, fmt.Errorf("pack 资源 %s：%w", id, layoutErr)
		}
		destination, err := uniquePackDestination(member.Targets, destination)
		if err != nil {
			return PackMemberPlan{}, fmt.Errorf("pack 资源 %s：%w", id, err)
		}
		if _, err := os.Lstat(destination); err == nil {
			return PackMemberPlan{}, fmt.Errorf("%w: %s", ErrPackConflict, destination)
		} else if !errors.Is(err, os.ErrNotExist) {
			return PackMemberPlan{}, err
		}
		member.Targets = append(member.Targets, PackTargetPlan{
			Agent: agent, Mode: mode, Destination: destination,
			Actions: []PlanAction{{
				Action: "stage-copy", From: member.Source, To: destination,
				FileCount: member.FileCount,
			}},
		})
	}
	if manifest.Metadata.Version != "" {
		member.Warnings = appendUnique(member.Warnings, "所属套件版本："+manifest.Metadata.Version)
	}
	return member, nil
}

func uniquePackDestination(targets []PackTargetPlan, destination string) (string, error) {
	for _, target := range targets {
		if filepath.Clean(target.Destination) == filepath.Clean(destination) {
			return "", errors.New("同一个资源不能重复投放到同一目录")
		}
	}
	return destination, nil
}

func normalizePackIDs(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.ToLower(strings.TrimSpace(value))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// packResourceOrder is a deterministic topological sort. Missing dependencies,
// self references and cycles are rejected before any path is inspected.
func packResourceOrder(resources []PackResourceSpec) ([]string, error) {
	known := make(map[string]bool, len(resources))
	dependencies := make(map[string][]string, len(resources))
	orderHint := make([]string, 0, len(resources))
	for _, resource := range resources {
		id := strings.ToLower(strings.TrimSpace(resource.ID))
		known[id] = true
		dependencies[id] = normalizePackIDs(resource.Requires)
		orderHint = append(orderHint, id)
	}
	for _, id := range orderHint {
		for _, dependency := range dependencies[id] {
			if !known[dependency] {
				return nil, fmt.Errorf("pack 资源 %s 依赖不存在的资源 %s", id, dependency)
			}
			if dependency == id {
				return nil, fmt.Errorf("pack 资源 %s 不能依赖自己", id)
			}
		}
	}
	state := map[string]int{}
	order := make([]string, 0, len(resources))
	var visit func(string, []string) error
	visit = func(id string, stack []string) error {
		switch state[id] {
		case 1:
			return errors.New("pack 依赖存在循环：" + strings.Join(append(stack, id), " → "))
		case 2:
			return nil
		}
		state[id] = 1
		for _, dependency := range dependencies[id] {
			if err := visit(dependency, append(stack, id)); err != nil {
				return err
			}
		}
		state[id] = 2
		order = append(order, id)
		return nil
	}
	for _, id := range orderHint {
		if err := visit(id, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// packLayoutDestination is the verified adapter table. New agents can be added
// here without changing the console or creating per-vendor install branches in
// the HTTP layer.
func packLayoutDestination(home, kind, name, agent, mode string) (string, error) {
	agent = strings.ToLower(strings.TrimSpace(agent))
	mode = strings.ToLower(strings.TrimSpace(mode))
	if agent == "" {
		return "", errors.New("target.agent 不能为空")
	}
	if agent == "homing" {
		if mode != "" && mode != "managed" && mode != "copy" {
			return "", fmt.Errorf("homing target 不支持 mode=%q", mode)
		}
		return destinationFor(home, kind, name)
	}
	if kind != KindSkill {
		return "", fmt.Errorf(
			"%s 的 %s layout 尚未核实；非技能资源目前只能投放到 homing 管理目录",
			agent, kind,
		)
	}
	if mode != "" && mode != "native-skill" {
		return "", fmt.Errorf("%s 的 skill target 不支持 mode=%q", agent, mode)
	}
	slug := safeSlug(name)
	if slug == "" {
		return "", errors.New("资源名称无法组成目录名")
	}
	roots := map[string]string{
		"claude":      filepath.Join(home, ".claude", "skills"),
		"codex":       filepath.Join(home, ".codex", "skills"),
		"workbuddy":   filepath.Join(home, ".workbuddy", "skills"),
		"kimi":        filepath.Join(home, ".kimi-code", "skills"),
		"gemini-cli":  filepath.Join(home, ".gemini", "skills"),
		"antigravity": filepath.Join(home, ".gemini", "antigravity", "skills"),
		"opencode":    filepath.Join(home, ".config", "opencode", "skills"),
		"agents":      filepath.Join(home, ".agents", "skills"),
	}
	root, ok := roots[agent]
	if !ok {
		return "", fmt.Errorf("Agent %s 还没有已核实的技能 layout", agent)
	}
	return filepath.Join(root, slug), nil
}

func packPlanDir(home, planID string) string {
	return filepath.Join(home, ".homing", "cache", "library", "packs", planID)
}

func loadPackPlan(home, planID string) (PackPlan, string, error) {
	planID = strings.TrimSpace(planID)
	if !safeID(planID) || !strings.HasPrefix(planID, "packplan_") {
		return PackPlan{}, "", errors.New("pack plan_id 无效")
	}
	dir := packPlanDir(home, planID)
	raw, err := os.ReadFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		return PackPlan{}, "", ErrPackPlanMissing
	}
	var plan PackPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return PackPlan{}, "", err
	}
	expires, err := time.Parse(time.RFC3339, plan.ExpiresAt)
	if err != nil || time.Now().After(expires) {
		_ = os.RemoveAll(dir)
		return PackPlan{}, "", ErrPackPlanMissing
	}
	if !plan.OK || plan.PlanID != planID || len(plan.Members) == 0 {
		return PackPlan{}, "", errors.New("pack plan 内容无效")
	}
	if err := ValidatePackManifest(plan.Pack); err != nil {
		return PackPlan{}, "", errors.New("pack plan 的 manifest 已失效")
	}
	rootInfo, rootErr := os.Lstat(plan.Root)
	if rootErr != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return PackPlan{}, "", ErrPackPlanStale
	}
	if len(plan.Order) != len(plan.Members) {
		return PackPlan{}, "", errors.New("pack plan 的资源顺序无效")
	}
	seenResources := map[string]bool{}
	seenDestinations := map[string]bool{}
	for index, member := range plan.Members {
		if member.ID == "" || plan.Order[index] != member.ID || seenResources[member.ID] {
			return PackPlan{}, "", errors.New("pack plan 的资源顺序无效")
		}
		for _, dependency := range member.Requires {
			if !seenResources[dependency] {
				return PackPlan{}, "", errors.New("pack plan 的依赖顺序无效")
			}
		}
		seenResources[member.ID] = true
		if len(member.Targets) == 0 {
			return PackPlan{}, "", errors.New("pack plan 的资源没有目标目录")
		}
		if !isWithin(member.Source, plan.Root) || member.Source == plan.Root {
			return PackPlan{}, "", ErrPackPlanStale
		}
		for _, target := range member.Targets {
			if !managedPackDestination(home, target.Destination) {
				return PackPlan{}, "", errors.New("pack plan 目的地越界，拒绝安装")
			}
			cleanDestination := filepath.Clean(target.Destination)
			if seenDestinations[cleanDestination] {
				return PackPlan{}, "", errors.New("pack plan 含重复目标目录")
			}
			seenDestinations[cleanDestination] = true
		}
	}
	return plan, dir, nil
}

// InstallPack consumes one plan and installs every member. Each member is
// staged in the plan cache first; destinations are renamed only after all
// sources have passed checksum validation and copy.
func InstallPack(home, planID string) (PackInstallation, error) {
	plan, planDir, err := loadPackPlan(home, planID)
	if err != nil {
		return PackInstallation{}, err
	}
	for _, member := range plan.Members {
		inspection, inspectErr := Inspect(member.Source, member.Kind)
		if inspectErr != nil {
			return PackInstallation{}, fmt.Errorf("%w: %s", ErrPackPlanStale, member.ID)
		}
		if member.Checksum == "" || inspection.Checksum != member.Checksum {
			return PackInstallation{}, fmt.Errorf("%w: %s checksum changed", ErrPackPlanStale, member.ID)
		}
		for _, target := range member.Targets {
			expectedDestination, layoutErr := packLayoutDestination(
				home, member.Kind, member.Name, target.Agent, target.Mode,
			)
			if layoutErr != nil || filepath.Clean(expectedDestination) != filepath.Clean(target.Destination) {
				return PackInstallation{}, errors.New("pack plan 的 layout 已失效")
			}
			if _, statErr := os.Lstat(target.Destination); statErr == nil {
				return PackInstallation{}, fmt.Errorf("%w: %s", ErrPackConflict, target.Destination)
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return PackInstallation{}, statErr
			}
		}
	}

	type destinationStage struct {
		memberID    string
		destination string
		stage       string
	}
	var destinations []destinationStage
	for _, member := range plan.Members {
		for _, target := range member.Targets {
			destinations = append(destinations, destinationStage{
				memberID: member.ID, destination: target.Destination,
			})
		}
	}
	stageRoot := filepath.Join(planDir, "stage")
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return PackInstallation{}, err
	}
	defer os.RemoveAll(stageRoot)
	for index := range destinations {
		memberID := destinations[index].memberID
		var member PackMemberPlan
		for _, candidate := range plan.Members {
			if candidate.ID == memberID {
				member = candidate
				break
			}
		}
		stage := filepath.Join(stageRoot, fmt.Sprintf("%02d-%s", index, filepath.Base(destinations[index].destination)))
		if err := copyTree(member.Source, stage); err != nil {
			return PackInstallation{}, fmt.Errorf("stage %s: %w", member.ID, err)
		}
		destinations[index].stage = stage
	}

	installedDestinations := make([]string, 0, len(plan.Members))
	for _, entry := range destinations {
		if err := os.MkdirAll(filepath.Dir(entry.destination), 0o700); err != nil {
			removePackDestinations(installedDestinations)
			return PackInstallation{}, err
		}
		if err := os.Rename(entry.stage, entry.destination); err != nil {
			removePackDestinations(installedDestinations)
			return PackInstallation{}, fmt.Errorf("commit %s: %w", entry.memberID, err)
		}
		installedDestinations = append(installedDestinations, entry.destination)
	}

	record := PackInstallation{
		ID:          mustRandomID("packinst"),
		PlanID:      plan.PlanID,
		PackID:      plan.Pack.Metadata.ID,
		Name:        plan.Pack.Metadata.Name,
		Version:     plan.Pack.Metadata.Version,
		Description: plan.Pack.Metadata.Description,
		Source:      plan.Pack.Metadata.Source,
		Root:        plan.Root,
		State:       "installed",
		InstalledAt: nowRFC3339(),
		Members:     make([]PackInstalledMember, 0, len(plan.Members)),
	}
	for _, member := range plan.Members {
		installed := PackInstalledMember{
			ID: member.ID, Name: member.Name, Kind: member.Kind, Checksum: member.Checksum,
		}
		for _, target := range member.Targets {
			installed.Targets = append(installed.Targets, PackInstalledTarget{
				Agent: target.Agent, Destination: target.Destination,
			})
		}
		record.Members = append(record.Members, installed)
	}
	if err := appendPackInstallation(home, record); err != nil {
		removePackDestinations(installedDestinations)
		return PackInstallation{}, err
	}
	_ = os.RemoveAll(planDir)
	return record, nil
}

func removePackDestinations(destinations []string) {
	for index := len(destinations) - 1; index >= 0; index-- {
		_ = os.RemoveAll(destinations[index])
	}
}

func managedPackDestination(home, destination string) bool {
	path, err := filepath.Abs(destination)
	if err != nil {
		return false
	}
	home, _ = filepath.Abs(home)
	allowedRoots := []string{
		filepath.Join(home, ".homing", "skills"),
		filepath.Join(home, ".homing", "instructions"),
		filepath.Join(home, ".homing", "resources"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(home, ".workbuddy", "skills"),
		filepath.Join(home, ".kimi-code", "skills"),
		filepath.Join(home, ".gemini", "skills"),
		filepath.Join(home, ".gemini", "antigravity", "skills"),
		filepath.Join(home, ".config", "opencode", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
	for _, root := range allowedRoots {
		if isWithin(path, root) && path != root {
			return true
		}
	}
	return false
}

// RollbackPack moves only the directories created by this pack. Native Agent
// roots themselves are never moved or removed.
func RollbackPack(home, installationID string) (PackInstallation, string, []string, error) {
	record, ok, err := findPackInstallation(home, installationID)
	if err != nil {
		return PackInstallation{}, "", nil, err
	}
	if !ok {
		return PackInstallation{}, "", nil, errors.New("套件安装记录不存在")
	}
	if record.State != "installed" {
		return record, "", nil, fmt.Errorf("套件状态是 %s，不能重复回滚", record.State)
	}
	for _, member := range record.Members {
		for _, target := range member.Targets {
			if !managedPackDestination(home, target.Destination) {
				return record, "", nil, errors.New("套件安装目录越界，拒绝回滚")
			}
		}
	}
	trashRoot := filepath.Join(
		home, ".homing", "trash",
		time.Now().UTC().Format("20060102T150405")+"-pack-"+safeSlug(record.Name),
	)
	if err := os.MkdirAll(trashRoot, 0o700); err != nil {
		return record, "", nil, err
	}
	var moved []struct{ from, to string }
	var missing []string
	var installedTargets []PackInstalledTarget
	for _, member := range record.Members {
		installedTargets = append(installedTargets, member.Targets...)
	}
	for index, targetPlan := range installedTargets {
		info, statErr := os.Lstat(targetPlan.Destination)
		if errors.Is(statErr, os.ErrNotExist) {
			missing = append(missing, targetPlan.Destination)
			continue
		}
		if statErr != nil {
			restorePackMoves(moved)
			return record, "", nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			restorePackMoves(moved)
			return record, "", nil, errors.New("套件目的地不是普通目录，拒绝回滚")
		}
		target := filepath.Join(trashRoot, fmt.Sprintf("%02d-%s", index, filepath.Base(targetPlan.Destination)))
		if err := os.Rename(targetPlan.Destination, target); err != nil {
			restorePackMoves(moved)
			return record, "", nil, err
		}
		moved = append(moved, struct{ from, to string }{targetPlan.Destination, target})
	}
	if len(moved) == 0 {
		_ = os.RemoveAll(trashRoot)
		return record, "", missing, errors.New("套件成员目录都不存在，未做任何改动")
	}
	record.State = "rolled_back"
	record.RolledBackAt = nowRFC3339()
	record.TrashPath = trashRoot
	record.Missing = missing
	if err := appendPackInstallation(home, record); err != nil {
		restorePackMoves(moved)
		_ = os.RemoveAll(trashRoot)
		return record, "", nil, err
	}
	return record, trashRoot, missing, nil
}

func restorePackMoves(moved []struct{ from, to string }) {
	for index := len(moved) - 1; index >= 0; index-- {
		_ = os.Rename(moved[index].to, moved[index].from)
	}
}
