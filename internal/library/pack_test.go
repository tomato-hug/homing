package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPackManifest = `apiVersion: homing.dev/v1alpha1
kind: AgentPack
metadata:
  id: demo.frontend
  name: Frontend Workflow
  version: 1.0.0
  description: Review and test a frontend change
  source: local://demo
spec:
  resources:
    - id: instructions
      path: resources/instructions
      kind: Instructions
      name: Frontend Rules
      targets:
        - agent: homing
          mode: managed
    - id: tests
      path: resources/tests
      kind: Skill
      name: Frontend Tests
      requires:
        - review
      targets:
        - agent: codex
          mode: native-skill
    - id: review
      path: resources/review
      kind: Skill
      name: Frontend Review
      targets:
        - agent: homing
          mode: managed
        - agent: claude
          mode: native-skill
`

func testPack(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "frontend-pack")
	writeTestFile(t, filepath.Join(root, PackManifestFile), testPackManifest)
	writeTestSkill(t, filepath.Join(root, "resources", "review"), "Frontend Review", "Review layout")
	writeTestSkill(t, filepath.Join(root, "resources", "tests"), "Frontend Tests", "Run tests")
	writeTestFile(
		t,
		filepath.Join(root, "resources", "instructions", "frontend.md"),
		"# Frontend rules\n\nCheck keyboard access.\n",
	)
	return root
}

func TestPlanPackValidatesDependenciesAndLayoutsWithoutWriting(t *testing.T) {
	home := t.TempDir()
	root := testPack(t)
	plan, err := PlanPack(home, root)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK || plan.PlanID == "" || len(plan.Members) != 3 {
		t.Fatalf("plan = %+v", plan)
	}
	if indexOf(plan.Order, "review") > indexOf(plan.Order, "tests") {
		t.Fatalf("dependency order does not put review before tests: %v", plan.Order)
	}
	for _, destination := range plan.Destinations {
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatalf("dry-run created %s", destination)
		}
	}
	var review PackMemberPlan
	for _, member := range plan.Members {
		if member.ID == "review" {
			review = member
		}
	}
	if len(review.Targets) != 2 {
		t.Fatalf("review targets = %+v", review.Targets)
	}
	if review.Targets[0].Destination != filepath.Join(home, ".homing", "skills", "frontend-review") {
		t.Fatalf("Homing destination = %s", review.Targets[0].Destination)
	}
	if review.Targets[1].Destination != filepath.Join(home, ".claude", "skills", "frontend-review") {
		t.Fatalf("Claude destination = %s", review.Targets[1].Destination)
	}
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func TestInstallPackIsTransactionalAndRollbackOnlyMovesItsMembers(t *testing.T) {
	home := t.TempDir()
	root := testPack(t)
	plan, err := PlanPack(home, root)
	if err != nil {
		t.Fatal(err)
	}
	record, err := InstallPack(home, plan.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != "installed" || record.PackID != "demo.frontend" || len(record.Members) != 3 {
		t.Fatalf("record = %+v", record)
	}
	for _, member := range record.Members {
		for _, target := range member.Targets {
			info, err := os.Stat(target.Destination)
			if err != nil || !info.IsDir() {
				t.Fatalf("installed destination %s: %v", target.Destination, err)
			}
		}
	}
	claudeSkill := filepath.Join(home, ".claude", "skills", "frontend-review", "SKILL.md")
	if _, err := os.Stat(claudeSkill); err != nil {
		t.Fatalf("Claude layout missing: %v", err)
	}
	all, err := PackInstallations(home)
	if err != nil || len(all) != 1 || all[0].ID != record.ID {
		t.Fatalf("installations = %+v err=%v", all, err)
	}

	rolledBack, trash, missing, err := RollbackPack(home, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.State != "rolled_back" || trash == "" || len(missing) != 0 {
		t.Fatalf("rollback = %+v trash=%q missing=%v", rolledBack, trash, missing)
	}
	for _, member := range record.Members {
		for _, target := range member.Targets {
			if _, err := os.Stat(target.Destination); !os.IsNotExist(err) {
				t.Fatalf("rollback left %s", target.Destination)
			}
		}
	}
	for _, agentRoot := range []string{
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".codex", "skills"),
		filepath.Join(home, ".homing", "skills"),
	} {
		if _, err := os.Stat(agentRoot); err != nil {
			t.Fatalf("rollback removed Agent root %s: %v", agentRoot, err)
		}
	}
	if _, err := os.Stat(trash); err != nil {
		t.Fatalf("trash missing: %v", err)
	}
}

func TestInstallPackRejectsStaleSourceWithoutPartialWrites(t *testing.T) {
	home := t.TempDir()
	root := testPack(t)
	plan, err := PlanPack(home, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "resources", "review", "SKILL.md"),
		[]byte("---\nname: Frontend Review\ndescription: changed\n---\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallPack(home, plan.PlanID); !errors.Is(err, ErrPackPlanStale) {
		t.Fatalf("install error = %v", err)
	}
	for _, destination := range plan.Destinations {
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatalf("stale plan wrote %s", destination)
		}
	}
}

func TestPlanPackRejectsConflictsAndUnsafeTargets(t *testing.T) {
	home := t.TempDir()
	root := testPack(t)
	conflict := filepath.Join(home, ".claude", "skills", "frontend-review")
	if err := os.MkdirAll(conflict, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanPack(home, root); !errors.Is(err, ErrPackConflict) {
		t.Fatalf("conflict error = %v", err)
	}

	home = t.TempDir()
	root = testPack(t)
	manifest, err := os.ReadFile(filepath.Join(root, PackManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	unsafe := strings.Replace(string(manifest), "agent: claude", "agent: unknown-ide", 1)
	if err := os.WriteFile(filepath.Join(root, PackManifestFile), []byte(unsafe), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanPack(home, root); err == nil || !strings.Contains(err.Error(), "layout") {
		t.Fatalf("unsafe target error = %v", err)
	}
}

func TestPackResourceOrderRejectsMissingAndCycles(t *testing.T) {
	_, err := packResourceOrder([]PackResourceSpec{
		{ID: "review", Requires: []string{"missing"}},
	})
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("missing dependency error = %v", err)
	}
	_, err = packResourceOrder([]PackResourceSpec{
		{ID: "a", Requires: []string{"b"}},
		{ID: "b", Requires: []string{"a"}},
	})
	if err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestPackRejectsPathTraversal(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "unsafe-pack")
	writeTestFile(t, filepath.Join(root, PackManifestFile), `apiVersion: homing.dev/v1alpha1
kind: AgentPack
metadata:
  id: demo.unsafe
  name: Unsafe
  version: 1.0.0
spec:
  resources:
    - id: escape
      path: ../outside
      kind: Skill
      targets:
        - agent: homing
`)
	if _, err := PlanPack(home, root); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("traversal error = %v", err)
	}
}
