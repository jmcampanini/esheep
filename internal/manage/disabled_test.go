package manage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/install"
	"github.com/jmcampanini/esheep/internal/render"
)

func TestSyncDisablesAndReenablesManagedSkillAcrossTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	claude, pi, codex := filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex")
	loaded := testConfig(source, "", claude, pi, codex)
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
	loaded.Config.Targets.Codex.Enabled = true
	writeSourceSkill(t, source, "demo", "Use when asked", "disable-model-invocation: true\n")
	if report := Sync(t.Context(), loaded); report.Summary.Installed != 3 || report.Summary.Failed != 0 {
		t.Fatalf("initial sync = %#v", report)
	}
	writeSourceSkill(t, source, "demo", "Use when asked", "esheep-disabled: true\n")
	loaded.Config.Targets.Codex.Enabled = false

	known := findKnownSkill(t, List(t.Context(), loaded), "source", "demo")
	status := Status(t.Context(), loaded)
	row := findStatus(t, status, "source", "demo")
	if known.Readiness != ReadinessDisabled || row.Readiness != ReadinessDisabled || !status.Healthy {
		t.Fatalf("disabled list = %#v, status = %#v", known, status)
	}
	for _, target := range []string{"claude", "pi", "codex"} {
		if row.Targets[target] != install.StateDisabled {
			t.Errorf("status target %q = %q, want disabled", target, row.Targets[target])
		}
	}
	if report := Sync(t.Context(), loaded); report.Summary.Pruned != 2 || report.Summary.Disabled != 3 || report.Summary.Failed != 0 {
		t.Fatalf("disable sync = %#v", report)
	}
	for _, target := range []string{claude, pi} {
		if _, err := os.Stat(filepath.Join(target, "demo")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("disabled installation in %q remains: %v", target, err)
		}
	}
	assertManifestContains(t, filepath.Join(codex, "demo", "SKILL.md"), "Use when asked")

	loaded.Config.Targets.Codex.Enabled = true
	if report := Sync(t.Context(), loaded); report.Summary.Pruned != 1 || report.Summary.Failed != 0 {
		t.Fatalf("enabled Codex cleanup = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(codex, "demo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled Codex installation remains: %v", err)
	}
	writeSourceSkill(t, source, "demo", "Use when asked", "esheep-disabled: false\n")

	if report := Sync(t.Context(), loaded); report.Summary.Installed != 3 || report.Summary.Failed != 0 {
		t.Fatalf("re-enable sync = %#v", report)
	}
	status = Status(t.Context(), loaded)
	if !status.Healthy || findStatus(t, status, "source", "demo").Readiness != ReadinessReady {
		t.Fatalf("re-enabled status = %#v", status)
	}
}

func TestDisabledFlagBelongsToSelectedManifest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		baseFields    string
		name          string
		profiles      []string
		readiness     Readiness
		trigger       string
		variantFields string
	}{
		{name: "disabled base selected", baseFields: "esheep-disabled: true\n", readiness: ReadinessDisabled, trigger: "Base"},
		{name: "variant does not inherit", baseFields: "esheep-disabled: true\n", profiles: []string{"work"}, readiness: ReadinessReady, trigger: "Work"},
		{name: "disabled variant does not fall back", variantFields: "esheep-disabled: true\n", profiles: []string{"work"}, readiness: ReadinessDisabled, trigger: "Work"},
		{name: "unselected disabled variant", variantFields: "esheep-disabled: true\n", readiness: ReadinessReady, trigger: "Base"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeSourceSkill(t, source, "demo", "Work", test.variantFields)
			skillRoot := filepath.Join(source, "skills", "demo")
			if err := os.Rename(filepath.Join(skillRoot, "SKILL.md"), filepath.Join(skillRoot, "SKILL.work.md")); err != nil {
				t.Fatal(err)
			}
			writeSourceSkill(t, source, "demo", "Base", test.baseFields)
			claude := filepath.Join(root, "claude")
			writeOwnedSkill(t, claude, install.Marker{Source: "source", Skill: "demo", Target: render.TargetClaude})
			loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"))
			loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
			loaded.Config.Targets.Pi.Enabled = false
			loaded.EffectiveProfiles = test.profiles

			report := Sync(t.Context(), loaded)
			known := findKnownSkill(t, List(t.Context(), loaded), "source", "demo")
			if report.Summary.Failed != 0 || known.Readiness != test.readiness || known.Trigger != test.trigger {
				t.Fatalf("sync = %#v, list = %#v, want %q and trigger %q", report, known, test.readiness, test.trigger)
			}
			if test.readiness == ReadinessDisabled {
				if report.Summary.Pruned != 1 {
					t.Fatalf("disabled selection sync = %#v, want one pruned installation", report)
				}
				if _, err := os.Stat(filepath.Join(claude, "demo")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("disabled selection remains installed: %v", err)
				}
			} else {
				assertManifestContains(t, filepath.Join(claude, "demo", "SKILL.md"), test.trigger)
			}
		})
	}
}

func TestDisabledSkillPreservesSourceFailureProtections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		readiness Readiness
		setup     func(*testing.T, string)
	}{
		{name: "required field", readiness: ReadinessInvalid, setup: func(t *testing.T, source string) {
			writeSourceSkill(t, source, "demo", " ", "esheep-disabled: true\n")
		}},
		{name: "supporting tree", readiness: ReadinessInvalid, setup: func(t *testing.T, source string) {
			if err := os.Symlink("missing", filepath.Join(source, "skills", "demo", "support")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "profile conflict", readiness: ReadinessConflict, setup: func(t *testing.T, source string) {
			root := filepath.Join(source, "skills", "demo")
			if err := os.Rename(filepath.Join(root, "SKILL.md"), filepath.Join(root, "SKILL.work.md")); err != nil {
				t.Fatal(err)
			}
			writeVariantManifest(t, source, "demo", "client", "Client")
		}},
		{name: "name collision", readiness: ReadinessCollision, setup: func(t *testing.T, source string) {
			writeSourceSkill(t, source+"-second", "demo", "Duplicate", "")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := filepath.Join(root, "source")
			if err := os.Mkdir(source+"-second", 0o755); err != nil {
				t.Fatal(err)
			}
			writeSourceSkill(t, source, "demo", "Disabled", "esheep-disabled: true\n")
			test.setup(t, source)
			claude := filepath.Join(root, "claude")
			writeOwnedSkill(t, claude, install.Marker{Source: "first", Skill: "demo", Target: render.TargetClaude})
			loaded := testConfig(source, source+"-second", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"))
			loaded.Config.Targets.Pi.Enabled = false
			loaded.EffectiveProfiles = []string{"work", "client"}

			report := Sync(t.Context(), loaded)
			status := Status(t.Context(), loaded)
			row := findStatus(t, status, "first", "demo")
			if report.Summary.Pruned != 0 || report.Summary.Failed == 0 || status.Healthy || row.Readiness != test.readiness {
				t.Fatalf("sync = %#v, status = %#v, want preserved %q skill", report, status, test.readiness)
			}
			if _, err := os.Stat(filepath.Join(claude, "demo", install.MarkerName)); err != nil {
				t.Fatalf("protected installation changed: %v", err)
			}
		})
	}
}

func TestDisabledSkillLeavesUnownedInstallationsUntouched(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "unmarked", setup: func(t *testing.T, destination string) {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "mismatched marker", setup: func(t *testing.T, destination string) {
			writeOwnedSkill(t, filepath.Dir(destination), install.Marker{Source: "source", Skill: "demo", Target: render.TargetPi})
		}},
		{name: "symlink", setup: func(t *testing.T, destination string) {
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), destination); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeSourceSkill(t, source, "demo", "Disabled", "esheep-disabled: true\n")
			claude := filepath.Join(root, "claude")
			destination := filepath.Join(claude, "demo")
			test.setup(t, destination)
			sentinel := filepath.Join(destination, "user-data")
			if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"))
			loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
			loaded.Config.Targets.Pi.Enabled = false

			report := Sync(t.Context(), loaded)

			if report.Summary.Pruned != 0 || report.Summary.Installed != 0 || report.Summary.Failed != 0 {
				t.Fatalf("sync = %#v, want unowned installation untouched", report)
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
				t.Fatalf("unowned data = %q, %v, want keep", data, err)
			}
		})
	}
}
