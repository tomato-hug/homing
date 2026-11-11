package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// PublishCheck is the local preflight shown before a resource is packaged or
// shared. It never signs, uploads, or executes commands declared by the
// manifest.
type PublishCheck struct {
	OK           bool       `json:"ok"`
	Inspection   Inspection `json:"inspection"`
	Manifest     *Manifest  `json:"manifest,omitempty"`
	ManifestPath string     `json:"manifest_path,omitempty"`
	Warnings     []string   `json:"warnings,omitempty"`
	Problems     []string   `json:"problems,omitempty"`
	Note         string     `json:"note"`
}

// CheckPublish validates one local directory as a shareable resource. Missing
// or invalid manifests are blocking; review notes such as network access or
// declared commands are returned separately from parse failures.
func CheckPublish(rawPath, kind string) (PublishCheck, error) {
	inspection, err := Inspect(rawPath, kind)
	if err != nil {
		return PublishCheck{}, err
	}
	check := PublishCheck{
		Inspection: inspection,
		Note:       "本机预检只验证 manifest、checksum、文件清单与符号链接；签名、公开市集和来源撤销尚未启用。",
	}
	for _, warning := range inspection.Warnings {
		check.Warnings = appendUnique(check.Warnings, warning)
	}
	if strings.TrimSpace(inspection.Checksum) == "" {
		check.Problems = appendUnique(check.Problems, "无法生成内容 checksum；不能作为可分享版本")
	}

	info, statErr := os.Stat(inspection.Path)
	if statErr != nil {
		return PublishCheck{}, statErr
	}
	if !info.IsDir() {
		check.Problems = appendUnique(check.Problems, "发布检查需要资源目录，并包含 homing.resource.yaml")
		check.OK = false
		return check, nil
	}

	manifest, manifestErr := ReadManifest(inspection.Path)
	switch {
	case errors.Is(manifestErr, ErrManifestMissing):
		check.Problems = appendUnique(check.Problems, "缺少 homing.resource.yaml；不能作为可分享版本")
	case manifestErr != nil:
		check.Problems = appendUnique(check.Problems, "manifest 校验失败："+manifestErr.Error())
	default:
		check.Manifest = &manifest
		check.ManifestPath = filepath.Join(inspection.Path, ManifestFile)
		for _, warning := range ManifestWarnings(manifest) {
			if strings.Contains(warning, "metadata.checksum") {
				check.Problems = appendUnique(check.Problems, warning)
			} else {
				check.Warnings = appendUnique(check.Warnings, warning)
			}
		}
		if manifestKind := normalizeKind(manifest.Kind); manifestKind != "" && manifestKind != inspection.Kind {
			check.Warnings = appendUnique(
				check.Warnings,
				"manifest kind="+manifestKind+" 与检查类型 "+inspection.Kind+" 不一致",
			)
		}
	}

	check.OK = len(check.Problems) == 0 && len(check.Warnings) == 0
	return check, nil
}
