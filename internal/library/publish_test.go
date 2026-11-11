package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePublishFixture(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("# Demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(root, ManifestFile), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCheckPublishAcceptsCompleteManifest(t *testing.T) {
	root := writePublishFixture(t, `apiVersion: homing.dev/v1alpha1
kind: Skill
metadata:
  id: demo.publish
  name: Demo Publish
  version: 1.0.0
  source: https://example.invalid/demo
  checksum: sha256:demo
spec:
  targets:
    - agent: claude
`)
	check, err := CheckPublish(root, KindSkill)
	if err != nil {
		t.Fatal(err)
	}
	if !check.OK || len(check.Problems) != 0 || len(check.Warnings) != 0 {
		t.Fatalf("complete manifest rejected: %+v", check)
	}
	if check.Manifest == nil || check.Manifest.Metadata.ID != "demo.publish" {
		t.Fatalf("manifest not returned: %+v", check.Manifest)
	}
	if check.Inspection.Checksum == "" {
		t.Fatal("missing content checksum")
	}
}

func TestCheckPublishBlocksDraftManifest(t *testing.T) {
	root := writePublishFixture(t, `apiVersion: homing.dev/v1alpha1
kind: Skill
metadata:
  id: demo.draft
  name: Demo Draft
  version: 0.1.0
  source: local
spec:
  targets:
    - agent: claude
`)
	check, err := CheckPublish(root, KindSkill)
	if err != nil {
		t.Fatal(err)
	}
	if check.OK {
		t.Fatalf("draft manifest should not pass publish check: %+v", check)
	}
	if !strings.Contains(strings.Join(check.Problems, "\n"), "metadata.checksum") {
		t.Fatalf("missing checksum should block publish: %+v", check.Problems)
	}
}

func TestCheckPublishBlocksInvalidOrMissingManifest(t *testing.T) {
	missing := writePublishFixture(t, "")
	check, err := CheckPublish(missing, KindSkill)
	if err != nil {
		t.Fatal(err)
	}
	if check.OK || !strings.Contains(strings.Join(check.Problems, "\n"), ManifestFile) {
		t.Fatalf("missing manifest was not blocked: %+v", check)
	}

	invalid := writePublishFixture(t, "kind: Skill\nmetadata: []\n")
	check, err = CheckPublish(invalid, KindSkill)
	if err != nil {
		t.Fatal(err)
	}
	if check.OK || len(check.Problems) == 0 {
		t.Fatalf("invalid manifest was not blocked: %+v", check)
	}
}

func TestCheckPublishFlagsKindMismatch(t *testing.T) {
	root := writePublishFixture(t, `apiVersion: homing.dev/v1alpha1
kind: Hook
metadata:
  id: demo.hook
  name: Demo Hook
  version: 1.0.0
  source: https://example.invalid/demo
  checksum: sha256:demo
spec:
  targets:
    - agent: claude
`)
	check, err := CheckPublish(root, KindSkill)
	if err != nil {
		t.Fatal(err)
	}
	if check.OK || !strings.Contains(strings.Join(check.Warnings, "\n"), "不一致") {
		t.Fatalf("kind mismatch not surfaced: %+v", check)
	}
}
