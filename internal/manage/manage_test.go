package manage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/install"
	"github.com/jmcampanini/esheep/internal/render"
)

func TestStatusListsEveryKnownSkillWithoutCreatingDisabledOrMissingTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeSourceSkill(t, first, "ready", "Ready", "pi:\n  disabled: true\n")
	writeSourceSkill(t, first, "broken", "   ", "")
	writeSourceSkill(t, first, "same", "First", "")
	writeSourceSkill(t, second, "same", "Second", "")
	claude := filepath.Join(root, "targets", "claude")
	pi := filepath.Join(root, "targets", "pi")
	codex := filepath.Join(root, "targets", "codex")
	agents := filepath.Join(root, "targets", "agents")
	loaded := testConfig(first, second, claude, pi, codex, agents)

	report := Status(context.Background(), loaded)
	if report.Healthy {
		t.Fatal("Status() is healthy with invalid, colliding, and missing skills")
	}
	if len(report.Skills) != 4 {
		t.Fatalf("skill count = %d, want 4", len(report.Skills))
	}
	ready := findStatus(t, report, "first", "ready")
	if ready.Readiness != ReadinessReady || ready.Targets["claude"] != install.StateMissing ||
		ready.Targets["pi"] != install.StateDisabled || ready.Targets["codex"] != install.StateDisabled ||
		ready.Targets["agents"] != install.StateDisabled {
		t.Fatalf("ready status = %#v", ready)
	}
	if broken := findStatus(t, report, "first", "broken"); broken.Readiness != ReadinessInvalid || len(broken.Targets) != 0 {
		t.Fatalf("broken status = %#v", broken)
	}
	for _, source := range []string{"first", "second"} {
		if same := findStatus(t, report, source, "same"); same.Readiness != ReadinessCollision || len(same.Targets) != 0 {
			t.Fatalf("collision status = %#v", same)
		}
	}
	for _, target := range []string{claude, pi, codex, agents} {
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("status created target %q: %v", target, err)
		}
	}
}

func TestStatusReportsInspectionFailureAsBlocked(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceSkill(t, source, "demo", "Ready", "")
	claude := filepath.Join(root, "claude")
	if err := os.WriteFile(claude, []byte("not a target directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}

	report := Status(context.Background(), loaded)
	status := findStatus(t, report, "source", "demo")
	if report.Healthy || status.Targets["claude"] != install.StateBlocked || len(report.Diagnostics) != 1 {
		t.Fatalf("report = %#v", report)
	}
	wantPath := loaded.ResolvedTargets.Claude
	if diagnostic := report.Diagnostics[0]; diagnostic.Path != wantPath || diagnostic.Target != "claude" {
		t.Fatalf("diagnostic = %#v, want target root %q", diagnostic, wantPath)
	}
}

func TestStatusInspectsEnabledTargetsWithoutSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(root, "claude")
	if err := os.WriteFile(claude, []byte("not a target directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}

	report := Status(context.Background(), loaded)
	if report.Healthy || len(report.Skills) != 0 || len(report.Diagnostics) != 1 {
		t.Fatalf("report = %#v", report)
	}
	wantPath := loaded.ResolvedTargets.Claude
	if diagnostic := report.Diagnostics[0]; diagnostic.Code != "target-inspection" || diagnostic.Path != wantPath || diagnostic.Target != "claude" {
		t.Fatalf("diagnostic = %#v, want blocked target root %q", diagnostic, wantPath)
	}
}

func TestSyncReportsTargetDestinationPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceSkill(t, source, "demo", "Ready", "")
	claude := filepath.Join(root, "claude")
	if err := os.MkdirAll(filepath.Join(claude, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}

	report := Sync(context.Background(), loaded)
	if report.Summary.Blocked != 1 || len(report.Diagnostics) != 1 {
		t.Fatalf("report = %#v", report)
	}
	wantPath := filepath.Join(loaded.ResolvedTargets.Claude, "demo")
	if diagnostic := report.Diagnostics[0]; diagnostic.Code != "synchronization" || diagnostic.Path != wantPath {
		t.Fatalf("diagnostic = %#v, want destination %q", diagnostic, wantPath)
	}
}

func TestSyncReportsTargetRootPathOnce(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceSkill(t, source, "demo", "Ready", "")
	claude := filepath.Join(root, "claude")
	if err := os.WriteFile(claude, []byte("not a target directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}

	report := Sync(context.Background(), loaded)
	if report.Summary.Failed != 1 || len(report.Diagnostics) != 1 {
		t.Fatalf("report = %#v", report)
	}
	wantPath := loaded.ResolvedTargets.Claude
	if diagnostic := report.Diagnostics[0]; diagnostic.Code != "target-inspection" || diagnostic.Path != wantPath || diagnostic.Target != "claude" {
		t.Fatalf("diagnostic = %#v, want target root %q", diagnostic, wantPath)
	}
}

func TestSyncReportsTargetRootWriteFailureOnce(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceSkill(t, source, "first", "First", "")
	writeSourceSkill(t, source, "second", "Second", "")
	claude := filepath.Join(root, "claude")
	if err := os.Mkdir(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(claude, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(claude, 0o755) })
	probe := filepath.Join(claude, "write-probe")
	if err := os.Mkdir(probe, 0o755); err == nil {
		_ = os.Remove(probe)
		t.Skip("process privileges bypass target directory permissions")
	} else if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("probe target write permission: %v", err)
	}
	loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}

	report := Sync(context.Background(), loaded)
	if report.Summary.Failed != 1 || len(report.Diagnostics) != 1 {
		t.Fatalf("report = %#v", report)
	}
	wantPath := loaded.ResolvedTargets.Claude
	if diagnostic := report.Diagnostics[0]; diagnostic.Code != "synchronization" || diagnostic.Path != wantPath || diagnostic.Target != "claude" {
		t.Fatalf("diagnostic = %#v, want target root %q", diagnostic, wantPath)
	}
}

func TestSyncContinuesUnrelatedInstallationAfterCollision(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeSourceSkill(t, first, "same", "First", "")
	writeSourceSkill(t, first, "unrelated", "Ready", "")
	writeSourceSkill(t, second, "same", "Second", "")
	claude := filepath.Join(root, "claude")
	loaded := testConfig(first, second, claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false

	report := Sync(context.Background(), loaded)
	if report.Summary.Installed != 1 || report.Summary.Failed != 2 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if _, err := os.Stat(filepath.Join(claude, "unrelated", install.MarkerName)); err != nil {
		t.Fatalf("unrelated skill was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(claude, "same")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("colliding skill was installed: %v", err)
	}
}

func TestSyncPrunesOnlyDefinitivelyStaleOwnership(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	personal := filepath.Join(root, "personal")
	offline := filepath.Join(root, "offline")
	writeSourceSkill(t, personal, "keep", "Ready", "")
	writeSourceSkill(t, personal, "broken", "   ", "")
	claude := filepath.Join(root, "claude")
	writeOwnedSkill(t, claude, install.Marker{Source: "personal", Skill: "removed", Target: render.TargetClaude})
	writeOwnedSkill(t, claude, install.Marker{Source: "personal", Skill: "broken", Target: render.TargetClaude})
	writeOwnedSkill(t, claude, install.Marker{Source: "offline", Skill: "held", Target: render.TargetClaude})
	writeOwnedSkill(t, claude, install.Marker{Source: "old", Skill: "orphan", Target: render.TargetClaude})
	loaded := testConfig(personal, offline, claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "personal", Path: personal}, {Name: "offline", Path: offline}}

	report := Sync(context.Background(), loaded)
	if report.Summary.Pruned != 2 || report.Summary.Installed != 1 || report.Summary.Failed != 2 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	for _, skill := range []string{"removed", "orphan"} {
		if _, err := os.Stat(filepath.Join(claude, skill)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale skill %q remains: %v", skill, err)
		}
	}
	for _, skill := range []string{"broken", "held", "keep"} {
		if _, err := os.Stat(filepath.Join(claude, skill)); err != nil {
			t.Fatalf("protected or installed skill %q: %v", skill, err)
		}
	}
}

func TestSyncPrunesSkillDisabledForEnabledTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceSkill(t, source, "demo", "Ready", "claude:\n  disabled: true\n")
	claude := filepath.Join(root, "claude")
	writeOwnedSkill(t, claude, install.Marker{Source: "source", Skill: "demo", Target: render.TargetClaude})
	loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}

	report := Sync(context.Background(), loaded)
	if report.Summary.Pruned != 1 || report.Summary.Disabled != 4 || report.Summary.Failed != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if _, err := os.Stat(filepath.Join(claude, "demo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target-disabled skill remains: %v", err)
	}
	for _, target := range []string{loaded.ResolvedTargets.Pi, loaded.ResolvedTargets.Codex, loaded.ResolvedTargets.Agents} {
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("sync created disabled target %q: %v", target, err)
		}
	}
}

func testConfig(first, second, claude, pi, codex, agents string) config.LoadResult {
	claude = canonicalManagedTestPath(claude)
	pi = canonicalManagedTestPath(pi)
	codex = canonicalManagedTestPath(codex)
	agents = canonicalManagedTestPath(agents)
	return config.LoadResult{
		Config: config.Config{
			Sources: []config.Source{{Name: "first", Path: first}, {Name: "second", Path: second}},
			Targets: config.Targets{
				Claude: config.ClaudeTarget{Enabled: true, Path: claude},
				Pi:     config.PiTarget{Enabled: true, Path: pi},
				Codex:  config.CodexTarget{Path: codex},
				Agents: config.AgentsTarget{Path: agents},
			},
		},
		ResolvedSources: []config.ResolvedSource{{Name: "first", Path: first}, {Name: "second", Path: second}},
		ResolvedTargets: config.ResolvedTargets{Claude: claude, Pi: pi, Codex: codex, Agents: agents},
	}
}

func canonicalManagedTestPath(path string) string {
	current := filepath.Clean(path)
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved
		}
		missing = append(missing, filepath.Base(current))
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		current = parent
	}
}

func writeSourceSkill(t *testing.T, source, name, description, extra string) {
	t.Helper()
	root := filepath.Join(source, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: " + name + "\ndescription: '" + description + "'\n" + extra + "---\nbody\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeOwnedSkill(t *testing.T, root string, marker install.Marker) {
	t.Helper()
	directory := filepath.Join(root, marker.Skill)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := install.MarshalMarker(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, install.MarkerName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func findStatus(t *testing.T, report StatusReport, source, skill string) SkillStatus {
	t.Helper()
	for _, status := range report.Skills {
		if status.Source == source && status.Directory == skill {
			return status
		}
	}
	t.Fatalf("status %s/%s not found in %#v", source, skill, report.Skills)
	return SkillStatus{}
}
