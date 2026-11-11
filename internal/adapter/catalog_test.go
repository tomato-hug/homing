package adapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomato-hug/homing/internal/agentconfig"
	"gopkg.in/yaml.v3"
)

func TestCatalogSpecsAreUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	allowed := map[string]bool{
		ReadinessBuiltin:    true,
		ReadinessAdapter:    true,
		ReadinessHook:       true,
		ReadinessUnverified: true,
	}
	for _, spec := range catalogSpecs {
		if spec.ID == "" || spec.Name == "" {
			t.Fatalf("catalog entry missing id/name: %+v", spec)
		}
		if seen[spec.ID] {
			t.Fatalf("duplicate catalog id %q", spec.ID)
		}
		seen[spec.ID] = true
		if !allowed[spec.Readiness] {
			t.Fatalf("%s: unknown readiness %q", spec.ID, spec.Readiness)
		}
		if spec.Readiness == ReadinessAdapter {
			if spec.Manifest == "" {
				t.Fatalf("%s: adapter has no embedded manifest", spec.ID)
			}
			raw, ok := CatalogManifestYAML(spec.ID)
			if !ok || len(raw) == 0 {
				t.Fatalf("%s: embedded manifest missing", spec.ID)
			}
			manifest, problems, err := LoadManifestFromString(string(raw))
			if err != nil {
				t.Fatalf("%s: %v", spec.ID, err)
			}
			if len(problems) > 0 {
				t.Fatalf("%s: invalid manifest: %v", spec.ID, problems)
			}
			if manifest.ID != spec.ID {
				t.Fatalf("%s: manifest id is %q", spec.ID, manifest.ID)
			}
		}
	}
	for _, id := range []string{"codex", "claude", "workbuddy", "kimi", "dsh", "openclaw", "cursor", "goose"} {
		if !seen[id] {
			t.Errorf("catalog is missing %s", id)
		}
	}
	for _, id := range []string{"kilo", "nanobot", "kimi-desktop", "deveco-code", "penguin", "omo", "omp", "atomcode", "snow"} {
		if !seen[id] {
			t.Errorf("catalog is missing Magpie agent %s", id)
		}
	}
	if len(catalogSpecs) < 57 {
		t.Fatalf("catalog has %d agents, want at least 57", len(catalogSpecs))
	}
}

func TestCatalogMagpieSnapshotHasProvenance(t *testing.T) {
	snapshot := mustLoadCatalogSnapshot()
	if len(snapshot.Agents) < 27 {
		t.Fatalf("snapshot has %d agents, want at least 27", len(snapshot.Agents))
	}
	for _, spec := range snapshot.Agents {
		if spec.CatalogSource != "magpie" && spec.CatalogSource != "magpie-community" {
			t.Errorf("%s: catalog_source = %q", spec.ID, spec.CatalogSource)
		}
		if !strings.HasPrefix(spec.SourceURL, "https://") {
			t.Errorf("%s: source_url = %q", spec.ID, spec.SourceURL)
		}
		if strings.TrimSpace(spec.Tagline) == "" {
			t.Errorf("%s: missing tagline", spec.ID)
		}
	}
}

func TestCatalogManifestMatchesRepoAdapters(t *testing.T) {
	for _, spec := range catalogSpecs {
		if spec.Manifest == "" {
			continue
		}
		embedded, ok := CatalogManifestYAML(spec.ID)
		if !ok {
			t.Fatalf("%s: embedded manifest missing", spec.ID)
		}
		repo, err := os.ReadFile(filepath.Join("..", "..", "adapters", spec.Manifest))
		if err != nil {
			t.Fatalf("%s: read repo manifest: %v", spec.ID, err)
		}
		if string(embedded) != string(repo) {
			t.Errorf("%s: embedded copy drifted from adapters/%s", spec.ID, spec.Manifest)
		}
	}
}

func TestCatalogInstallAndRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := t.TempDir()

	result, err := InstallCatalogAgent("dsh", dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.File == "" {
		t.Fatalf("result = %+v", result)
	}
	want := filepath.Join(home, ".homing", "agents", "dsh.yaml")
	if result.File != want {
		t.Fatalf("file = %q, want %q", result.File, want)
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(mustCatalogYAML(t, "dsh")) {
		t.Fatal("installed manifest differs from embedded manifest")
	}
	settings := agentconfig.NewStore(dataDir).Read("dsh")
	if !settings.Connected || !settings.PushToPhone {
		t.Fatalf("settings = %+v", settings)
	}
	invalidate()
	found := false
	for _, item := range CustomAgents(context.Background()) {
		if item.ID() == "dsh" {
			found = true
		}
	}
	if !found {
		t.Fatal("installed manifest did not load")
	}

	removed, err := RemoveCatalogAgent("dsh", dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.OK || len(removed.Removed) != 1 {
		t.Fatalf("remove result = %+v", removed)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("manifest still exists: %v", err)
	}
	if agentconfig.NewStore(dataDir).Read("dsh").Connected {
		t.Fatal("settings should be disconnected after removal")
	}
	invalidate()
	for _, item := range CustomAgents(context.Background()) {
		if item.ID() == "dsh" {
			t.Fatal("removed manifest still loads")
		}
	}
}

func TestCatalogKitsRenderForEveryAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	nodePath, nodeErr := exec.LookPath("node")

	for _, spec := range catalogSpecs {
		result, err := InstallCatalogKit(spec.ID)
		if err != nil {
			t.Fatalf("%s: install kit: %v", spec.ID, err)
		}
		if !result.OK || result.Dir == "" {
			t.Fatalf("%s: result = %+v", spec.ID, result)
		}
		for _, file := range []string{result.Hook, result.Wrapper, result.ManifestDraft, result.AdapterDraft, result.Readme} {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%s: read %s: %v", spec.ID, file, err)
			}
			if strings.Contains(string(raw), "{{") {
				t.Errorf("%s: %s still has a template placeholder", spec.ID, file)
			}
		}

		raw, err := os.ReadFile(result.ManifestDraft)
		if err != nil {
			t.Fatal(err)
		}
		var draft map[string]any
		if err := yaml.Unmarshal(raw, &draft); err != nil {
			t.Errorf("%s: manifest draft is invalid YAML: %v", spec.ID, err)
		}
		if draft["id"] != spec.ID {
			t.Errorf("%s: draft id = %v", spec.ID, draft["id"])
		}

		hook, err := os.ReadFile(result.Hook)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(hook), `const agent = "`+spec.ID+`";`) {
			t.Errorf("%s: hook.mjs did not render the agent id", spec.ID)
		}
		if nodeErr == nil {
			cmd := exec.Command(nodePath, "--check", result.Hook)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s: node --check hook.mjs: %v\n%s", spec.ID, err, output)
			}
		}
	}
}

func TestCatalogKitIsIdempotentAndHandlesEmptyPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	first, err := InstallCatalogKit("minimax-design")
	if err != nil {
		t.Fatal(err)
	}
	custom := []byte("# user edit\n")
	if err := os.WriteFile(first.Hook, custom, 0o700); err != nil {
		t.Fatal(err)
	}
	second, err := InstallCatalogKit("minimax-design")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Existing {
		t.Fatal("second install should report Existing")
	}
	raw, err := os.ReadFile(first.Hook)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(custom) {
		t.Fatal("second install overwrote a user-edited file")
	}
	manifest, err := os.ReadFile(first.ManifestDraft)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "paths: []") || !strings.Contains(string(manifest), "sessions: null") {
		t.Fatalf("empty detect path was not rendered safely:\n%s", manifest)
	}
	adapter, err := os.ReadFile(first.AdapterDraft)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(adapter), "reads: []") {
		t.Fatalf("empty adapter path was not rendered safely:\n%s", adapter)
	}

	if _, err := InstallCatalogKit("../escape"); err != ErrCatalogNotFound {
		t.Fatalf("unsafe id error = %v", err)
	}
}

func TestCatalogQuickConnectInstallTestAndRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := t.TempDir()

	result, err := QuickConnectCatalogAgent("dsh", dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.ManifestInstalled || !result.KitInstalled {
		t.Fatalf("quick connect result = %+v", result)
	}
	if result.ManifestFile == "" || result.KitDir == "" || result.Hook == "" || result.Wrapper == "" {
		t.Fatalf("quick connect omitted generated paths: %+v", result)
	}
	if !result.Settings.Connected || !result.Settings.PushToPhone {
		t.Fatalf("quick connect settings = %+v", result.Settings)
	}
	for _, file := range []string{result.ManifestFile, result.Hook, result.Wrapper} {
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("generated file %s: %v", file, err)
		}
	}

	payload := BuildCatalog(context.Background(), agentconfig.NewStore(dataDir))
	var connected *CatalogEntry
	for i := range payload.Agents {
		if payload.Agents[i].ID == "dsh" {
			connected = &payload.Agents[i]
			break
		}
	}
	if connected == nil {
		t.Fatal("dsh missing from catalog after quick connect")
	}
	if !connected.Connected || !connected.ManifestInstalled || !connected.KitInstalled {
		t.Fatalf("catalog row after quick connect = %+v", connected)
	}
	if !containsString(connected.PushSignals, "hook") {
		t.Fatalf("quick-connected agent cannot use its generated hook: %+v", connected.PushSignals)
	}

	removedKit, err := RemoveCatalogKit("dsh")
	if err != nil {
		t.Fatal(err)
	}
	if !removedKit.OK || removedKit.Trash == "" {
		t.Fatalf("remove kit result = %+v", removedKit)
	}
	if _, err := os.Stat(removedKit.Dir); !os.IsNotExist(err) {
		t.Fatalf("kit directory still exists: %v", err)
	}
	if _, err := os.Stat(removedKit.Trash); err != nil {
		t.Fatalf("trash directory missing: %v", err)
	}
	if _, err := RemoveCatalogKit("dsh"); err != ErrCatalogKitNotInstalled {
		t.Fatalf("second kit removal error = %v", err)
	}

	removed, err := RemoveCatalogAgent("dsh", dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.OK || removed.Settings.Connected {
		t.Fatalf("remove agent result = %+v", removed)
	}
}

func TestCatalogQuickConnectSupportsEveryEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dataDir := t.TempDir()

	for _, spec := range catalogSpecs {
		result, err := QuickConnectCatalogAgent(spec.ID, dataDir)
		if err != nil {
			t.Fatalf("%s: quick connect: %v", spec.ID, err)
		}
		if !result.OK || !result.KitInstalled || result.KitDir == "" {
			t.Fatalf("%s: incomplete quick connect result: %+v", spec.ID, result)
		}
		if spec.Readiness == ReadinessAdapter && (!result.ManifestInstalled || result.ManifestFile == "") {
			t.Fatalf("%s: adapter did not install its manifest: %+v", spec.ID, result)
		}
		if !result.Settings.Connected || !result.Settings.PushToPhone {
			t.Fatalf("%s: quick connect did not enable push: %+v", spec.ID, result.Settings)
		}
		if _, err := os.Stat(result.Hook); err != nil {
			t.Fatalf("%s: generated hook missing: %v", spec.ID, err)
		}
	}
}

func mustCatalogYAML(t *testing.T, id string) []byte {
	t.Helper()
	raw, ok := CatalogManifestYAML(id)
	if !ok {
		t.Fatalf("no embedded manifest for %s", id)
	}
	return raw
}
