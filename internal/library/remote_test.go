package library

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemotePlanInstallAndRollback(t *testing.T) {
	archive := testArchive(t, map[string]string{
		"review-pack/homing.resource.yaml": `apiVersion: homing.dev/v1alpha1
kind: Skill
metadata:
  id: demo.review
  name: Review Pack
  version: 1.2.0
  description: Review changes
  source: https://example.test/review
  checksum: sha256:0000000000000000000000000000000000000000000000000000000000000000
spec:
  targets:
    - agent: homing
      mode: native-skill
  permissions:
    network: false
  install:
    strategy: copy
    backup: true
`,
		"review-pack/SKILL.md": "---\nname: Review Pack\ndescription: Review changes\n---\n\n# Review\n",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	home := t.TempDir()

	plan, err := PlanRemote(context.Background(), home, RemoteRequest{URL: server.URL + "/pack"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || plan.Manifest == nil || plan.Plan.Name != "Review Pack" {
		t.Fatalf("plan = %+v", plan)
	}
	if !strings.HasPrefix(plan.DownloadSHA256, "sha256:") || plan.Trust != "verified-source" {
		t.Fatalf("pin = %q trust=%q", plan.DownloadSHA256, plan.Trust)
	}
	if _, err := os.Stat(plan.Plan.Destination); !os.IsNotExist(err) {
		t.Fatal("remote plan wrote the destination")
	}

	installed, record, err := InstallRemote(home, plan.FetchID)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Plan.Destination == "" || record.State != "installed" {
		t.Fatalf("installed = %+v record=%+v", installed, record)
	}
	if _, err := os.Stat(filepath.Join(record.Destination, "SKILL.md")); err != nil {
		t.Fatalf("installed files missing: %v", err)
	}
	all, err := Installations(home)
	if err != nil || len(all) != 1 {
		t.Fatalf("installations = %#v err=%v", all, err)
	}

	rolledBack, trash, err := RollbackInstallation(home, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.State != "rolled_back" || trash == "" {
		t.Fatalf("rollback = %+v trash=%q", rolledBack, trash)
	}
	if _, err := os.Stat(trash); err != nil {
		t.Fatalf("trash missing: %v", err)
	}
	if _, err := os.Stat(record.Destination); !os.IsNotExist(err) {
		t.Fatal("rollback left the installed resource in place")
	}
}

func TestRemoteUpdateKeepsAndRestoresPreviousVersion(t *testing.T) {
	version := "1.0.0"
	body := "version one"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		archive := testArchive(t, map[string]string{
			"review-pack/homing.resource.yaml": `apiVersion: homing.dev/v1alpha1
kind: Skill
metadata:
  id: demo.review
  name: Review Pack
  version: ` + version + `
  description: Review changes
  source: https://example.test/review
spec:
  targets:
    - agent: homing
      mode: native-skill
`,
			"review-pack/SKILL.md": body,
		})
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	home := t.TempDir()

	first, err := PlanRemote(context.Background(), home, RemoteRequest{URL: server.URL + "/pack"})
	if err != nil {
		t.Fatal(err)
	}
	_, installed, err := InstallRemote(home, first.FetchID)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != "1.0.0" {
		t.Fatalf("installed version = %q", installed.Version)
	}

	version = "2.0.0"
	body = "version two"
	update, err := PlanRemoteUpdate(context.Background(), home, installed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !update.UpdateAvailable || update.AvailableVersion != "2.0.0" || update.CurrentVersion != "1.0.0" {
		t.Fatalf("update plan = %+v", update)
	}
	before, err := os.ReadFile(filepath.Join(installed.Destination, "SKILL.md"))
	if err != nil || string(before) != "version one" {
		t.Fatalf("dry-run changed destination: %q err=%v", before, err)
	}

	_, upgraded, err := InstallRemoteUpdate(home, installed.ID, update.Plan.FetchID)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Version != "2.0.0" || upgraded.BackupPath == "" {
		t.Fatalf("upgraded = %+v", upgraded)
	}
	if upgraded.PreviousVersion != "1.0.0" ||
		upgraded.PreviousDownloadSHA256 != installed.DownloadSHA256 ||
		upgraded.PreviousInstalledAt != installed.InstalledAt {
		t.Fatalf("upgraded previous metadata = %+v", upgraded)
	}
	after, err := os.ReadFile(filepath.Join(installed.Destination, "SKILL.md"))
	if err != nil || string(after) != "version two" {
		t.Fatalf("upgraded content = %q err=%v", after, err)
	}
	if _, err := os.Stat(upgraded.BackupPath); err != nil {
		t.Fatalf("backup missing: %v", err)
	}

	rolledBack, trash, err := RollbackInstallation(home, installed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.State != "installed" || rolledBack.Version != "1.0.0" ||
		rolledBack.BackupPath != "" || rolledBack.RolledBackAt == "" || trash == "" {
		t.Fatalf("rollback = %+v trash=%q", rolledBack, trash)
	}
	if rolledBack.PreviousVersion != "" || rolledBack.PreviousDownloadSHA256 != "" {
		t.Fatalf("rollback kept previous-version fields: %+v", rolledBack)
	}
	restored, err := os.ReadFile(filepath.Join(installed.Destination, "SKILL.md"))
	if err != nil || string(restored) != "version one" {
		t.Fatalf("restored content = %q err=%v", restored, err)
	}
}

func TestRemoteUpdateRejectsNoOp(t *testing.T) {
	archive := testArchive(t, map[string]string{
		"same/homing.resource.yaml": `apiVersion: homing.dev/v1alpha1
kind: Skill
metadata:
  id: demo.same
  name: Same Pack
  version: 1.0.0
  source: https://example.test/same
spec:
  targets:
    - agent: homing
`,
		"same/SKILL.md": "same",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	home := t.TempDir()
	first, err := PlanRemote(context.Background(), home, RemoteRequest{URL: server.URL + "/same"})
	if err != nil {
		t.Fatal(err)
	}
	_, installed, err := InstallRemote(home, first.FetchID)
	if err != nil {
		t.Fatal(err)
	}
	update, err := PlanRemoteUpdate(context.Background(), home, installed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if update.UpdateAvailable {
		t.Fatalf("same content reported an update: %+v", update)
	}
	if update.Plan.FetchID != "" {
		t.Fatalf("no-op update left a usable fetch token: %q", update.Plan.FetchID)
	}
}

func TestExtractZipRejectsTraversal(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create("../outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("escape"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(archive, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractArchive(archive, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestManifestValidation(t *testing.T) {
	root := t.TempDir()
	manifest := `apiVersion: homing.dev/v1alpha1
kind: Skill
metadata:
  id: demo.skill
  name: Demo
  version: 1.0.0
  source: https://example.test/demo
spec:
  targets:
    - agent: claude
`
	if err := os.WriteFile(filepath.Join(root, ManifestFile), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Metadata.ID != "demo.skill" || len(ManifestWarnings(parsed)) == 0 {
		t.Fatalf("manifest = %+v", parsed)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFile), []byte("kind: Skill\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(root); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}

func testArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, body := range files {
		header := &tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
