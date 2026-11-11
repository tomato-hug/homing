package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomato-hug/homing/internal/agentconfig"
	"github.com/tomato-hug/homing/internal/driver"
	"gopkg.in/yaml.v3"
)

var (
	ErrDriveUnsupported  = errors.New("catalog: this agent has no verified ACP drive")
	ErrDriveNotInstalled = errors.New("catalog: the agent executable is not installed")
	ErrDriveNotEnabled   = errors.New("catalog: ACP drive is not enabled")
)

// CatalogDrive is the truthful L3 state shown next to one catalog entry.
// Enabled means the user completed a successful handshake and Homing wrote the
// drive block; it is not inferred merely from a binary being on PATH.
type CatalogDrive struct {
	Supported    bool   `json:"supported"`
	Enabled      bool   `json:"enabled"`
	Installed    bool   `json:"installed"`
	Ready        bool   `json:"ready"`
	Kind         string `json:"kind"`
	Command      string `json:"command,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Experimental bool   `json:"experimental,omitempty"`
	Verified     bool   `json:"verified"`
}

// CatalogDriveResult is returned after a handshake and manifest write. Probe is
// protocol metadata only; it never includes a prompt or transcript.
type CatalogDriveResult struct {
	OK       bool                 `json:"ok"`
	ID       string               `json:"id"`
	File     string               `json:"file"`
	Backup   string               `json:"backup,omitempty"`
	Drive    CatalogDrive         `json:"drive"`
	Probe    driver.ProbeResult   `json:"probe"`
	Settings agentconfig.Settings `json:"settings"`
}

// CatalogDriveRemoveResult reports whether the whole drive-only manifest was
// moved aside or only its drive block was removed from a larger manifest.
type CatalogDriveRemoveResult struct {
	OK              bool         `json:"ok"`
	ID              string       `json:"id"`
	File            string       `json:"file"`
	ManifestRemoved bool         `json:"manifest_removed"`
	Trash           string       `json:"trash,omitempty"`
	Drive           CatalogDrive `json:"drive"`
}

func catalogDriveFor(id string, liveManifest *Manifest) *CatalogDrive {
	catalogSpec, exists := catalogSpecByID(id)
	if !exists || catalogSpec.Readiness == ReadinessBuiltin {
		// Built-in scanners own their capability table. A manifest with the same
		// id is skipped by the registry, so offering a manifest-based ACP toggle
		// here would be a false promise.
		return nil
	}
	spec, ok := driver.SpecFor(id)
	if !ok {
		return nil
	}
	readiness := driver.PeekReadiness(spec)
	enabled := liveManifest != nil && liveManifest.Drive != nil
	if !enabled {
		enabled = manifestHasDrive(id)
	}
	return &CatalogDrive{
		Supported:    true,
		Enabled:      enabled,
		Installed:    readiness.Installed,
		Ready:        readiness.Ready,
		Kind:         "acp",
		Command:      spec.CommandString(),
		Detail:       readiness.Detail,
		Experimental: spec.Experimental,
		Verified:     spec.Verified,
	}
}

func manifestHasDrive(id string) bool {
	path := manifestPathFor(id)
	if path == "" {
		return false
	}
	m, problems, err := LoadManifest(path)
	return err == nil && len(problems) == 0 && m != nil && m.Drive != nil
}

// EnableCatalogDrive performs the handshake in a new temporary directory and
// only then writes the drive block. A failed or timed-out handshake has no
// effect on the user's manifest.
func EnableCatalogDrive(ctx context.Context, id, dataDir string) (CatalogDriveResult, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !validID(id) {
		return CatalogDriveResult{}, ErrCatalogNotFound
	}
	spec, ok := catalogSpecByID(id)
	if !ok {
		return CatalogDriveResult{}, ErrCatalogNotFound
	}
	if spec.Readiness == ReadinessBuiltin {
		return CatalogDriveResult{}, ErrDriveUnsupported
	}
	driveSpec, ok := driver.SpecFor(spec.ID)
	if !ok {
		return CatalogDriveResult{}, ErrDriveUnsupported
	}
	readiness := driver.CheckReadiness(driveSpec)
	if !readiness.Installed {
		return CatalogDriveResult{}, fmt.Errorf("%w: %s", ErrDriveNotInstalled, readiness.Detail)
	}

	probeDir, err := os.MkdirTemp("", "homing-drive-"+spec.ID+"-*")
	if err != nil {
		return CatalogDriveResult{}, fmt.Errorf("create probe directory: %w", err)
	}
	defer os.RemoveAll(probeDir)

	probe, probeErr := driver.Probe(ctx, driveSpec, probeDir)
	if probeErr != nil {
		detail := strings.TrimSpace(probeErr.Error())
		driver.MarkReadiness(driveSpec, false, detail)
		return CatalogDriveResult{}, fmt.Errorf("ACP handshake failed: %w", probeErr)
	}
	driver.MarkReadiness(driveSpec, true, "")

	file, backup, err := writeCatalogDriveManifest(spec, driveSpec)
	if err != nil {
		return CatalogDriveResult{}, err
	}
	invalidate()
	settings, settingsErr := agentconfig.NewStore(dataDir).Patch(spec.ID, map[string]any{
		"connected":     true,
		"push_to_phone": true,
	})
	driveState := catalogDriveFor(spec.ID, nil)
	if driveState == nil {
		driveState = &CatalogDrive{Supported: true, Enabled: true, Installed: true, Ready: true, Kind: "acp"}
	}
	result := CatalogDriveResult{
		OK: true, ID: spec.ID, File: file, Backup: backup,
		Drive: *driveState, Probe: probe, Settings: settings,
	}
	if settingsErr != nil {
		return result, fmt.Errorf("drive enabled, but saving settings failed: %w", settingsErr)
	}
	return result, nil
}

// RemoveCatalogDrive is the reversible counterpart. A manifest created only
// for the drive is moved to .deleted-*, while a manifest that also has a
// verified session reader only loses its drive block.
func RemoveCatalogDrive(id, dataDir string) (CatalogDriveRemoveResult, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !validID(id) {
		return CatalogDriveRemoveResult{}, ErrCatalogNotFound
	}
	spec, ok := catalogSpecByID(id)
	if !ok {
		return CatalogDriveRemoveResult{}, ErrCatalogNotFound
	}
	file := manifestPathFor(spec.ID)
	if file == "" || !manifestHasDrive(spec.ID) {
		return CatalogDriveRemoveResult{}, ErrDriveNotEnabled
	}

	root, body, _, err := readManifestNode(file)
	if err != nil {
		return CatalogDriveRemoveResult{}, err
	}
	removeYAMLField(body, "drive")
	next := rawFromNode(root)
	if len(next) == 0 {
		return CatalogDriveRemoveResult{}, errors.New("manifest encoding produced no data")
	}
	remaining, problems, parseErr := LoadManifestFromString(string(next))
	if parseErr != nil {
		return CatalogDriveRemoveResult{}, parseErr
	}

	result := CatalogDriveRemoveResult{OK: true, ID: spec.ID, File: file}
	if len(problems) > 0 || remaining == nil || (remaining.Sessions.Source == "" && remaining.Reply == nil && remaining.Models == nil) {
		trash := fmt.Sprintf("%s.deleted-drive-%d", file, time.Now().UnixNano())
		if err := os.Rename(file, trash); err != nil {
			return CatalogDriveRemoveResult{}, fmt.Errorf("move drive-only manifest aside: %w", err)
		}
		result.ManifestRemoved = true
		result.Trash = trash
	} else {
		if err := writeManifestBytes(file, next); err != nil {
			return CatalogDriveRemoveResult{}, err
		}
	}
	invalidate()
	if result.ManifestRemoved {
		_, settingsErr := agentconfig.NewStore(dataDir).Patch(spec.ID, map[string]any{
			"connected":     false,
			"push_to_phone": false,
		})
		if settingsErr != nil {
			return result, fmt.Errorf("drive removed, but saving settings failed: %w", settingsErr)
		}
	}
	if state := catalogDriveFor(spec.ID, nil); state != nil {
		result.Drive = *state
	}
	return result, nil
}

func writeCatalogDriveManifest(spec catalogSpec, driveSpec driver.Spec) (string, string, error) {
	dir := ManifestDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create manifest directory: %w", err)
	}
	file := manifestPathFor(spec.ID)
	backup := ""
	var root, body *yaml.Node
	var raw []byte
	var err error
	if file == "" {
		file = filepath.Join(dir, spec.ID+".yaml")
		root, body = newManifestDocument()
	} else {
		root, body, raw, err = readManifestNode(file)
		if err != nil {
			return "", "", err
		}
		if len(raw) > 0 {
			backup = fmt.Sprintf("%s.bak-%d", file, time.Now().UnixNano())
			if err := os.WriteFile(backup, raw, 0o600); err != nil {
				return "", "", fmt.Errorf("backup manifest: %w", err)
			}
		}
	}

	ensureYAMLField(body, "id", spec.ID)
	ensureYAMLField(body, "name", spec.Name)
	if spec.Icon != "" {
		ensureYAMLField(body, "icon", spec.Icon)
	}
	if len(spec.DetectBins) > 0 && yamlField(body, "detect") == nil {
		setYAMLField(body, "detect", map[string]any{"bin": append([]string(nil), spec.DetectBins...)})
	}
	drive := map[string]any{
		"kind":     "acp",
		"command":  append([]string(nil), driveSpec.Command...),
		"dispatch": driveSpec.Dispatch,
	}
	if len(driveSpec.Check) > 0 {
		drive["check"] = append([]string(nil), driveSpec.Check...)
	}
	if len(driveSpec.CheckFail) > 0 {
		drive["check_fail"] = append([]string(nil), driveSpec.CheckFail...)
	}
	if len(driveSpec.Env) > 0 {
		drive["env"] = driveSpec.Env
	}
	setYAMLField(body, "drive", drive)

	next := rawFromNode(root)
	m, problems, err := LoadManifestFromString(string(next))
	if err != nil {
		return "", "", err
	}
	if len(problems) > 0 {
		return "", "", fmt.Errorf("drive manifest validation failed: %s", joinProblems(problems))
	}
	if m.ID != spec.ID {
		return "", "", fmt.Errorf("drive manifest id mismatch: %s != %s", m.ID, spec.ID)
	}
	if err := writeManifestBytes(file, next); err != nil {
		return "", "", err
	}
	return file, backup, nil
}

func newManifestDocument() (*yaml.Node, *yaml.Node) {
	body := &yaml.Node{Kind: yaml.MappingNode}
	root := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{body}}
	return root, body
}

func readManifestNode(path string) (*yaml.Node, *yaml.Node, []byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, nil, nil, errors.New("manifest must be a regular file, not a symlink")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, nil, nil, errors.New("manifest root must be a mapping")
	}
	return &root, root.Content[0], raw, nil
}

func rawFromNode(root *yaml.Node) []byte {
	raw, err := yaml.Marshal(root)
	if err != nil {
		return nil
	}
	return raw
}

func writeManifestBytes(path string, next []byte) error {
	if len(next) == 0 {
		return errors.New("manifest encoding produced no data")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary manifest: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(next); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary manifest: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	return nil
}

func yamlField(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func removeYAMLField(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

func ensureYAMLField(mapping *yaml.Node, key string, value any) {
	if yamlField(mapping, key) == nil {
		setYAMLField(mapping, key, value)
	}
}

func setYAMLField(mapping *yaml.Node, key string, value any) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	var encoded yaml.Node
	if err := encoded.Encode(value); err != nil {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = &encoded
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&encoded,
	)
}
