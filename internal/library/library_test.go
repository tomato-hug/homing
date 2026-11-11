package library

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanGroupsSkillAcrossAgents(t *testing.T) {
	home := t.TempDir()
	writeTestSkill(t, filepath.Join(home, ".codex", "skills", "review"), "Review", "Check code")
	writeTestSkill(t, filepath.Join(home, ".claude", "skills", "review"), "Review", "Check code")
	writeTestSkill(t, filepath.Join(home, ".agents", "skills", "testing"), "Testing", "Run tests")

	snapshot := Scan(home)
	if snapshot.Total != 2 {
		t.Fatalf("total = %d, want 2: %#v", snapshot.Total, snapshot.Resources)
	}
	var review Resource
	for _, item := range snapshot.Resources {
		if item.Name == "Review" {
			review = item
		}
	}
	if len(review.Targets) != 2 {
		t.Fatalf("review targets = %d, want 2", len(review.Targets))
	}
	if review.FileCount != 2 {
		t.Fatalf("file_count = %d, want 2", review.FileCount)
	}
}

func TestScanIncludesBroaderAgentResourceRoots(t *testing.T) {
	home := t.TempDir()
	writeTestSkill(t, filepath.Join(home, ".workbuddy", "skills", "frontend"), "Frontend", "WorkBuddy skill")
	writeTestSkill(t, filepath.Join(home, ".gemini", "skills", "research"), "Research", "Gemini skill")
	writeTestSkill(t, filepath.Join(home, ".gemini", "antigravity", "skills", "review"), "Antigravity Review", "Antigravity skill")
	writeTestSkill(t, filepath.Join(home, ".config", "opencode", "skills", "testing"), "Testing", "OpenCode skill")
	writeTestFile(t, filepath.Join(home, ".codex", "rules", "default.rules"), "rule body")
	writeTestFile(t, filepath.Join(home, ".dsh", ".agent-presets", "redteam", "preset.yml"), "name: Red Team\n")
	writeTestFile(t, filepath.Join(home, ".claude", "agents", "reviewer", "reviewer.md"), "# Reviewer\n")

	snapshot := Scan(home)
	want := map[string]string{
		"Frontend":           "workbuddy",
		"Research":           "gemini-cli",
		"Antigravity Review": "antigravity",
		"Testing":            "opencode",
		"Default":            "codex",
		"Redteam":            "dsh",
		"Reviewer":           "claude",
	}
	for name, agent := range want {
		found := false
		for _, resource := range snapshot.Resources {
			if resource.Name != name {
				continue
			}
			for _, target := range resource.Targets {
				if target.Agent == agent {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("resource %q for %s not found in %#v", name, agent, snapshot.Resources)
		}
	}
}

func TestScanShowsSymlinkedSkillsAsReadOnlyTargets(t *testing.T) {
	home := t.TempDir()
	canonical := filepath.Join(home, ".claude", "skills", "figma")
	writeTestSkill(t, canonical, "Figma", "Shared skill")
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".gemini", "skills", "figma")
	if err := os.Symlink(canonical, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	snapshot := Scan(home)
	for _, resource := range snapshot.Resources {
		if resource.Name != "Figma" {
			continue
		}
		var linked bool
		for _, target := range resource.Targets {
			if target.Agent == "gemini-cli" && target.Path == link {
				linked = true
			}
		}
		if !linked {
			t.Fatalf("symlinked Gemini target missing: %+v", resource.Targets)
		}
		if resource.Removable {
			t.Fatalf("symlinked-only resource was marked removable: %+v", resource)
		}
		return
	}
	t.Fatalf("Figma resource not found: %+v", snapshot.Resources)
}

func TestInstallPlanAndRecoverableRemove(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "review-skill")
	writeTestSkill(t, source, "Review Skill", "Review changes")

	plan, err := BuildImportPlan(home, source, KindSkill, "Review Skill")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Destination != filepath.Join(home, ".homing", "skills", "review-skill") {
		t.Fatalf("destination = %s", plan.Destination)
	}
	if _, err := os.Stat(plan.Destination); !os.IsNotExist(err) {
		t.Fatal("dry-run created the destination")
	}

	if _, err := Install(home, source, KindSkill, "Review Skill"); err != nil {
		t.Fatal(err)
	}
	snapshot := Scan(home)
	var installed *Resource
	for i := range snapshot.Resources {
		if snapshot.Resources[i].Name == "Review Skill" {
			installed = &snapshot.Resources[i]
		}
	}
	if installed == nil || !installed.Removable {
		t.Fatalf("installed resource = %#v", installed)
	}
	if _, err := Install(home, source, KindSkill, "Review Skill"); err == nil {
		t.Fatal("second install should conflict")
	}
	result, err := Remove(home, installed.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.TrashPath); err != nil {
		t.Fatalf("trash path missing: %v", err)
	}
	if _, err := os.Stat(plan.Destination); !os.IsNotExist(err) {
		t.Fatal("removal left the original resource in place")
	}
}

func TestInspectRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Inspect(link, KindSkill); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestHiddenFilesAreExcludedFromInspectAndInstall(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "hidden-skill")
	writeTestSkill(t, source, "Hidden Skill", "Visible files only")
	if err := os.WriteFile(filepath.Join(source, ".secret"), []byte("do not import"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".env"), []byte("TOKEN=do-not-copy"), 0o600); err != nil {
		t.Fatal(err)
	}

	inspection, err := Inspect(source, KindSkill)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.FileCount != 1 {
		t.Fatalf("inspection file_count = %d, want 1", inspection.FileCount)
	}
	for _, file := range inspection.Files {
		if file.Path == ".secret" || file.Path == ".env" {
			t.Fatalf("hidden file leaked into preview: %s", file.Path)
		}
	}

	plan, err := Install(home, source, KindSkill, "Hidden Skill")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(plan.Destination, ".secret")); !os.IsNotExist(err) {
		t.Fatal("hidden file was copied")
	}
	if _, err := os.Stat(filepath.Join(plan.Destination, "SKILL.md")); err != nil {
		t.Fatalf("visible file missing after install: %v", err)
	}
	snapshot := Scan(home)
	for _, item := range snapshot.Resources {
		if item.Name == "Hidden Skill" && item.FileCount != 1 {
			t.Fatalf("scan file_count = %d, want 1", item.FileCount)
		}
	}
}

func writeTestSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
