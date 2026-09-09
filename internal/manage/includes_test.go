package manage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/install"
	"github.com/jmcampanini/esheep/internal/render"
)

func TestSyncHarnessIncludesPreservesIdentityAndTracksTargetDrift(t *testing.T) {
	t.Parallel()
	loaded, skillRoot := personalIncludeConfig(t)
	writePersonalInclude(t, skillRoot, "pi", "Pi instructions")
	writePersonalInclude(t, skillRoot, "codex", "Codex instructions")

	report := Sync(t.Context(), loaded)
	if report.Summary.Installed != 2 || report.Summary.Failed != 0 {
		t.Fatalf("initial sync summary = %#v", report.Summary)
	}
	for _, target := range []struct {
		name render.Target
		root string
		body string
	}{
		{name: render.TargetPi, root: loaded.ResolvedTargets.Pi.Skills, body: "Pi instructions"},
		{name: render.TargetCodex, root: loaded.ResolvedTargets.Codex.Skills, body: "Codex instructions"},
	} {
		destination := filepath.Join(target.root, "fable-review")
		assertManifestContains(t, filepath.Join(destination, "SKILL.md"), target.body)
		data, err := os.ReadFile(filepath.Join(destination, install.MarkerName))
		if err != nil {
			t.Fatal(err)
		}
		marker, err := install.ParseMarker(data)
		if err != nil || marker != (install.Marker{Source: "source", Skill: "fable-review", Target: target.name}) {
			t.Fatalf("marker = %#v, %v", marker, err)
		}
	}

	writePersonalInclude(t, skillRoot, "pi", "Revised Pi instructions")
	status := Status(t.Context(), loaded)
	row := findStatus(t, status, "source", "fable-review")
	if row.Targets["pi"] != install.StateDrifted || row.Targets["codex"] != install.StateSynced {
		t.Fatalf("target states after Pi source edit = %#v", row.Targets)
	}
	report = Sync(t.Context(), loaded)
	if report.Summary.Repaired != 1 || report.Summary.Unchanged != 1 || report.Summary.Failed != 0 {
		t.Fatalf("repair summary = %#v", report.Summary)
	}
	if status := Status(t.Context(), loaded); !status.Healthy {
		t.Fatalf("status after repair = %#v", status)
	}

	loaded.EffectiveProfiles = nil
	report = Sync(t.Context(), loaded)
	if report.Summary.Pruned != 2 || report.Summary.Failed != 0 {
		t.Fatalf("inactive personal profile summary = %#v", report.Summary)
	}
	for _, root := range []string{loaded.ResolvedTargets.Pi.Skills, loaded.ResolvedTargets.Codex.Skills} {
		if _, err := os.Stat(filepath.Join(root, "fable-review")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("inactive personal skill remains in %q: %v", root, err)
		}
	}
}

func TestSyncMissingHarnessIncludePreservesOutputAndContinuesOtherTargets(t *testing.T) {
	t.Parallel()
	loaded, skillRoot := personalIncludeConfig(t)
	writePersonalInclude(t, skillRoot, "pi", "Saved Pi instructions")
	writePersonalInclude(t, skillRoot, "codex", "Saved Codex instructions")
	if report := Sync(t.Context(), loaded); report.Summary.Failed != 0 {
		t.Fatalf("initial sync summary = %#v", report.Summary)
	}
	piManifest := filepath.Join(loaded.ResolvedTargets.Pi.Skills, "fable-review", "SKILL.md")
	before, err := os.ReadFile(piManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(skillRoot, "esheep-body-pi.md")); err != nil {
		t.Fatal(err)
	}
	writePersonalInclude(t, skillRoot, "codex", "Updated Codex instructions")

	report := Sync(t.Context(), loaded)
	if report.Summary.Failed != 1 || report.Summary.Repaired != 1 || report.Summary.Pruned != 0 {
		t.Fatalf("sync with missing Pi include = %#v", report.Summary)
	}
	after, err := os.ReadFile(piManifest)
	if err != nil || string(after) != string(before) {
		t.Fatalf("Pi manifest after failure = %q, %v, want %q", after, err, before)
	}
	assertManifestContains(t, filepath.Join(loaded.ResolvedTargets.Codex.Skills, "fable-review", "SKILL.md"), "Updated Codex instructions")
	status := Status(t.Context(), loaded)
	row := findStatus(t, status, "source", "fable-review")
	if status.Healthy || row.Targets["pi"] != install.StateBlocked || row.Targets["codex"] != install.StateSynced {
		t.Fatalf("status after include failure = %#v", status)
	}

	loaded.Config.Targets.Pi.Enabled = false
	report = Sync(t.Context(), loaded)
	if report.Summary.Failed != 0 || report.Summary.Pruned != 0 {
		t.Fatalf("disabled Pi target required an include: %#v", report.Summary)
	}
	after, err = os.ReadFile(piManifest)
	if err != nil || string(after) != string(before) {
		t.Fatalf("disabled Pi installation changed: %q, %v", after, err)
	}
}

func TestDisabledManifestDoesNotRequireHarnessIncludes(t *testing.T) {
	t.Parallel()
	loaded, skillRoot := personalIncludeConfig(t)
	manifestPath := filepath.Join(skillRoot, "SKILL.personal.md")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest = []byte(strings.Replace(string(manifest), "name: fable-review", "name: fable-review\nesheep-disabled: true", 1))
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}

	report := Sync(t.Context(), loaded)
	status := Status(t.Context(), loaded)
	if report.Summary.Disabled != 3 || report.Summary.Failed != 0 || !status.Healthy {
		t.Fatalf("disabled manifest without includes: sync = %#v, status = %#v", report, status)
	}
	for _, root := range []string{loaded.ResolvedTargets.Claude.Skills, loaded.ResolvedTargets.Pi.Skills, loaded.ResolvedTargets.Codex.Skills} {
		if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("disabled manifest created target %q: %v", root, err)
		}
	}
}

func personalIncludeConfig(t *testing.T) (config.LoadResult, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	skillRoot := filepath.Join(source, "skills", "fable-review")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: fable-review\nesheep-trigger: Use for an independent review\nesheep-targets: [pi, codex]\n---\n{{esheep.include-by-harness \"body\"}}"
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.personal.md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := testConfig(source, "", filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"))
	loaded.Config.Targets.Codex.Enabled = true
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
	loaded.EffectiveProfiles = []string{"personal"}
	return loaded, skillRoot
}

func writePersonalInclude(t *testing.T, root, harness, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "esheep-body-"+harness+".md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
