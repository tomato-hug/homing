package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const ManifestFile = "homing.resource.yaml"

var ErrManifestMissing = errors.New("homing.resource.yaml not found")

// Manifest is the portable description of one installable capability. L2
// validates it but never runs the optional commands it declares.
type Manifest struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Metadata   struct {
		ID          string   `yaml:"id" json:"id"`
		Name        string   `yaml:"name" json:"name"`
		Version     string   `yaml:"version" json:"version"`
		Description string   `yaml:"description,omitempty" json:"description,omitempty"`
		License     string   `yaml:"license,omitempty" json:"license,omitempty"`
		Source      string   `yaml:"source" json:"source"`
		Checksum    string   `yaml:"checksum" json:"checksum"`
		Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	} `yaml:"metadata" json:"metadata"`
	Spec struct {
		Targets []struct {
			Agent string `yaml:"agent" json:"agent"`
			Mode  string `yaml:"mode,omitempty" json:"mode,omitempty"`
		} `yaml:"targets" json:"targets"`
		Permissions struct {
			Filesystem struct {
				Read  []string `yaml:"read,omitempty" json:"read,omitempty"`
				Write []string `yaml:"write,omitempty" json:"write,omitempty"`
			} `yaml:"filesystem,omitempty" json:"filesystem,omitempty"`
			Network  bool     `yaml:"network,omitempty" json:"network,omitempty"`
			Commands []string `yaml:"commands,omitempty" json:"commands,omitempty"`
		} `yaml:"permissions,omitempty" json:"permissions,omitempty"`
		Install struct {
			Strategy string `yaml:"strategy,omitempty" json:"strategy,omitempty"`
			Backup   bool   `yaml:"backup,omitempty" json:"backup,omitempty"`
		} `yaml:"install,omitempty" json:"install,omitempty"`
		Uninstall struct {
			Strategy string `yaml:"strategy,omitempty" json:"strategy,omitempty"`
		} `yaml:"uninstall,omitempty" json:"uninstall,omitempty"`
	} `yaml:"spec" json:"spec"`
	Path string `yaml:"-" json:"path,omitempty"`
}

// ReadManifest loads and validates <root>/homing.resource.yaml.
func ReadManifest(root string) (Manifest, error) {
	path := filepath.Join(root, ManifestFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, ErrManifestMissing
	}
	if err != nil {
		return Manifest{}, err
	}
	if len(raw) > 256<<10 {
		return Manifest{}, errors.New("manifest is larger than 256 KB")
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse %s: %w", ManifestFile, err)
	}
	manifest.Path = path
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ValidateManifest enforces the fields needed for a reproducible import. It
// accepts the missing checksum only so callers can surface a useful warning;
// publish checks treat it as an error.
func ValidateManifest(manifest Manifest) error {
	var fields []string
	if !strings.HasPrefix(strings.TrimSpace(manifest.APIVersion), "homing.dev/") {
		fields = append(fields, "apiVersion")
	}
	if normalizeKind(manifest.Kind) == "" {
		fields = append(fields, "kind")
	}
	if strings.TrimSpace(manifest.Metadata.ID) == "" {
		fields = append(fields, "metadata.id")
	}
	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		fields = append(fields, "metadata.name")
	}
	if strings.TrimSpace(manifest.Metadata.Version) == "" {
		fields = append(fields, "metadata.version")
	}
	if strings.TrimSpace(manifest.Metadata.Source) == "" {
		fields = append(fields, "metadata.source")
	}
	if len(manifest.Spec.Targets) == 0 {
		fields = append(fields, "spec.targets")
	}
	if len(fields) > 0 {
		return fmt.Errorf("manifest 缺少或无效字段：%s", strings.Join(fields, ", "))
	}
	if checksum := strings.TrimSpace(manifest.Metadata.Checksum); checksum != "" &&
		!strings.HasPrefix(checksum, "sha256:") {
		return errors.New("manifest metadata.checksum 必须是 sha256:<hex>")
	}
	for _, command := range manifest.Spec.Permissions.Commands {
		if strings.TrimSpace(command) == "" {
			return errors.New("manifest permissions.commands 不能有空项")
		}
	}
	if strategy := strings.TrimSpace(manifest.Spec.Install.Strategy); strategy != "" && strategy != "copy" {
		return fmt.Errorf("L2 只支持 install.strategy=copy，收到 %q", strategy)
	}
	return nil
}

// ManifestWarnings adds non-fatal review notes shown before installation.
func ManifestWarnings(manifest Manifest) []string {
	var warnings []string
	if strings.TrimSpace(manifest.Metadata.Checksum) == "" {
		warnings = append(warnings, "manifest 没有 metadata.checksum；只能作为本机草稿")
	}
	if len(manifest.Spec.Permissions.Commands) > 0 {
		warnings = append(warnings, "此资源声明了命令，但 Homing 不会自动执行；请自行审阅")
	}
	if manifest.Spec.Permissions.Network {
		warnings = append(warnings, "此资源声明需要网络访问")
	}
	return warnings
}
