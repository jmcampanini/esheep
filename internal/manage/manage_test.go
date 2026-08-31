package manage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/agentsfile"
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/install"
	"github.com/jmcampanini/esheep/internal/render"
)

func TestStatusListsEveryKnownSkillWithoutCreatingDisabledOrMissingTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeSourceSkill(t, first, "ready", "Ready", "esheep-targets: [claude, codex, agents]\n")
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
	wantPath := loaded.ResolvedTargets.Claude.Skills
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
	wantPath := loaded.ResolvedTargets.Claude.Skills
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
	wantPath := filepath.Join(loaded.ResolvedTargets.Claude.Skills, "demo")
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
	wantPath := loaded.ResolvedTargets.Claude.Skills
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
	wantPath := loaded.ResolvedTargets.Claude.Skills
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
	writeSourceSkill(t, source, "demo", "Ready", "esheep-targets: [pi, codex, agents]\n")
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
	for _, target := range []string{loaded.ResolvedTargets.Pi.Skills, loaded.ResolvedTargets.Codex.Skills, loaded.ResolvedTargets.Agents.Skills} {
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("sync created disabled target %q: %v", target, err)
		}
	}
}

func TestSyncSelectsVariantsAndPrunesInactiveProfileSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceSkill(t, source, "everywhere", "Everywhere", "")
	writeSourceSkill(t, source, "gated", "Gated", "esheep-only-profiles: [work]\n")
	writeSourceSkill(t, source, "variant", "Base", "")
	writeVariantManifest(t, source, "variant", "work", "Work")
	claude := filepath.Join(root, "claude")
	loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
	loaded.EffectiveProfiles = []string{"work"}

	report := Sync(context.Background(), loaded)
	if report.Summary.Installed != 3 || report.Summary.Failed != 0 || report.Summary.Inactive != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	assertManifestContains(t, filepath.Join(claude, "variant", "SKILL.md"), "Work")

	loaded.EffectiveProfiles = nil
	report = Sync(context.Background(), loaded)
	if report.Summary.Pruned != 1 || report.Summary.Inactive != 1 || report.Summary.Repaired != 1 ||
		report.Summary.Unchanged != 1 || report.Summary.Failed != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if _, err := os.Stat(filepath.Join(claude, "gated")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inactive skill remains installed: %v", err)
	}
	assertManifestContains(t, filepath.Join(claude, "variant", "SKILL.md"), "Base")
}

func TestSyncBlocksProfileConflictAndProtectsInstalledOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeVariantManifest(t, source, "both", "work", "Work")
	writeVariantManifest(t, source, "both", "client", "Client")
	claude := filepath.Join(root, "claude")
	writeOwnedSkill(t, claude, install.Marker{Source: "source", Skill: "both", Target: render.TargetClaude})
	loaded := testConfig(source, "", claude, filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
	loaded.EffectiveProfiles = []string{"client", "work"}

	report := Sync(context.Background(), loaded)

	if report.Summary.Failed != 1 || report.Summary.Pruned != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	var conflictDetail string
	for _, action := range report.Actions {
		if action.Action == install.ActionFailed && action.Identity.Skill == "both" {
			conflictDetail = action.Detail
		}
	}
	if conflictDetail != "active profiles select multiple manifests" {
		t.Fatalf("conflict detail = %q", conflictDetail)
	}
	if _, err := os.Stat(filepath.Join(claude, "both")); err != nil {
		t.Fatalf("conflicting skill output was not protected: %v", err)
	}
}

func TestListTracksWhetherAnyManifestLoaded(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceSkill(t, source, "everywhere", "Everywhere", "")
	writeVariantManifest(t, source, "invalid", "base", "Invalid")
	loaded := testConfig(source, "", filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}

	report := List(context.Background(), loaded)

	everywhere := findKnownSkill(t, report, "source", "everywhere")
	if !everywhere.HasManifest || len(everywhere.ProfileGate) != 0 {
		t.Fatalf("universal skill = %#v, want loaded manifest and empty gate", everywhere)
	}
	invalid := findKnownSkill(t, report, "source", "invalid")
	if invalid.HasManifest || invalid.Readiness != ReadinessInvalid {
		t.Fatalf("invalid skill = %#v, want no loaded manifest", invalid)
	}
}

func TestStatusReportsInactiveSkillsAsHealthy(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceSkill(t, source, "gated", "Gated", "esheep-only-profiles: [work]\n")
	loaded := testConfig(source, "", filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}

	report := Status(context.Background(), loaded)

	if !report.Healthy {
		t.Fatalf("report = %#v, want healthy with inactive skill", report)
	}
	status := findStatus(t, report, "source", "gated")
	if status.Readiness != ReadinessReady || status.Targets["claude"] != install.StateInactive {
		t.Fatalf("status = %#v", status)
	}
	if len(status.ProfileGate) != 1 || status.ProfileGate[0] != "work" {
		t.Fatalf("profile gate = %#v, want [work]", status.ProfileGate)
	}
}

func TestProfilesUnionsReferencedGates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceSkill(t, source, "everywhere", "Everywhere", "")
	writeSourceSkill(t, source, "gated", "Gated", "esheep-only-profiles: [client]\n")
	writeSourceSkill(t, source, "invalid", "Invalid", "esheep-only-profiles: [Work]\n")
	writeSourceSkill(t, source, "variant", "Base", "")
	writeVariantManifest(t, source, "variant", "work", "Work")
	writeSourceAgentsFile(t, source, "AGENTS.ops.md", "ops instructions\n")
	loaded := testConfig(source, "", filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
	loaded.EffectiveProfiles = []string{"work"}

	report := Profiles(context.Background(), loaded)

	if !report.Complete {
		t.Fatalf("report = %#v, want complete", report)
	}
	if len(report.Effective) != 1 || report.Effective[0] != "work" {
		t.Fatalf("effective = %#v, want [work]", report.Effective)
	}
	if len(report.Referenced) != 3 || report.Referenced[0] != "client" || report.Referenced[1] != "ops" || report.Referenced[2] != "work" {
		t.Fatalf("referenced = %#v, want [client ops work]", report.Referenced)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "invalid-profile" {
		t.Fatalf("diagnostics = %#v, want invalid-profile", report.Diagnostics)
	}
}

func TestSyncDeploysAgentsFileWriteOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceAgentsFile(t, source, "AGENTS.md", "global instructions\n")
	loaded := testConfig(source, "", filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
	destination := loaded.ResolvedTargets.Claude.AgentsMD

	report := Sync(context.Background(), loaded)
	if report.Summary.Installed != 1 || report.Summary.Disabled != 3 || report.Summary.Failed != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	assertManifestContains(t, destination, "global instructions")

	report = Sync(context.Background(), loaded)
	if report.Summary.Unchanged != 1 || report.Summary.Failed != 0 {
		t.Fatalf("second summary = %#v", report.Summary)
	}

	if err := os.WriteFile(destination, []byte("hand edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report = Sync(context.Background(), loaded)
	if report.Summary.Repaired != 1 || report.Summary.Failed != 0 {
		t.Fatalf("repair summary = %#v", report.Summary)
	}
	assertManifestContains(t, destination, "global instructions")

	if err := os.Remove(filepath.Join(source, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	report = Sync(context.Background(), loaded)
	if len(report.Actions) != 0 || report.Summary.Failed != 0 {
		t.Fatalf("withdrawn summary = %#v actions = %#v", report.Summary, report.Actions)
	}
	assertManifestContains(t, destination, "global instructions")
}

func TestSyncSelectsAgentsFileByProfileOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceAgentsFile(t, source, "AGENTS.md", "base instructions\n")
	writeSourceAgentsFile(t, source, "AGENTS.work.md", "work instructions\n")
	loaded := testConfig(source, "", filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
	loaded.EffectiveProfiles = []string{"client", "work"}
	destination := loaded.ResolvedTargets.Claude.AgentsMD

	report := Sync(context.Background(), loaded)
	if report.Summary.Installed != 1 || report.Summary.Failed != 0 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	assertManifestContains(t, destination, "work instructions")

	loaded.EffectiveProfiles = nil
	report = Sync(context.Background(), loaded)
	if report.Summary.Repaired != 1 || report.Summary.Failed != 0 {
		t.Fatalf("base summary = %#v", report.Summary)
	}
	assertManifestContains(t, destination, "base instructions")
}

func TestSyncFailsAgentsFileTierCollisionWithoutWriting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	writeSourceAgentsFile(t, first, "AGENTS.md", "first\n")
	writeSourceAgentsFile(t, second, "AGENTS.md", "second\n")
	loaded := testConfig(first, second, filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false

	report := Sync(context.Background(), loaded)

	if report.Summary.Failed != 1 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "agents-file-selection" {
		t.Fatalf("report = %#v", report)
	}
	if _, err := os.Stat(loaded.ResolvedTargets.Claude.AgentsMD); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("colliding agents file was written: %v", err)
	}
}

func TestSyncSkipsAgentsFileWhileAnySourceIsUnavailable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	first := filepath.Join(root, "first")
	writeSourceAgentsFile(t, first, "AGENTS.md", "first\n")
	loaded := testConfig(first, filepath.Join(root, "missing"), filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false

	report := Sync(context.Background(), loaded)

	if report.Summary.Failed != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	if _, err := os.Stat(loaded.ResolvedTargets.Claude.AgentsMD); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("agents file was written with an unavailable source: %v", err)
	}
}

func TestStatusReportsAgentsFileStates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeSourceAgentsFile(t, source, "AGENTS.md", "instructions\n")
	loaded := testConfig(source, "", filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"), filepath.Join(root, "agents"))
	loaded.Config.Targets.Pi.Enabled = false
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
	destination := loaded.ResolvedTargets.Claude.AgentsMD

	report := Status(context.Background(), loaded)
	if report.Healthy || report.AgentsFile == nil {
		t.Fatalf("report = %#v, want unhealthy with agents file section", report)
	}
	row := report.AgentsFile
	if row.Source != "source" || row.Profile != "" || row.Path != filepath.Join(source, "AGENTS.md") {
		t.Fatalf("agents file row = %#v", row)
	}
	if row.Targets["claude"] != agentsfile.StateMissing || row.Targets["pi"] != agentsfile.StateDisabled ||
		row.Targets["codex"] != agentsfile.StateDisabled || row.Targets["agents"] != agentsfile.StateDisabled {
		t.Fatalf("agents file targets = %#v", row.Targets)
	}

	if err := os.WriteFile(destination, []byte("instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report = Status(context.Background(), loaded)
	if !report.Healthy || report.AgentsFile.Targets["claude"] != agentsfile.StateSynced {
		t.Fatalf("synced report = %#v", report)
	}

	if err := os.WriteFile(destination, []byte("drifted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report = Status(context.Background(), loaded)
	if report.Healthy || report.AgentsFile.Targets["claude"] != agentsfile.StateStale {
		t.Fatalf("stale report = %#v", report)
	}
}

func testConfig(first, second, claude, pi, codex, agents string) config.LoadResult {
	claude = canonicalManagedTestPath(claude)
	pi = canonicalManagedTestPath(pi)
	codex = canonicalManagedTestPath(codex)
	agents = canonicalManagedTestPath(agents)
	resolved := func(skills string) config.ResolvedTarget {
		return config.ResolvedTarget{Skills: skills, AgentsMD: skills + "-AGENTS.md"}
	}
	return config.LoadResult{
		Config: config.Config{
			Sources: []config.Source{{Name: "first", Path: first}, {Name: "second", Path: second}},
			Targets: config.Targets{
				Claude: config.ClaudeTarget{Enabled: true, SkillsPath: claude, AgentsMDPath: claude + "-AGENTS.md"},
				Pi:     config.PiTarget{Enabled: true, SkillsPath: pi, AgentsMDPath: pi + "-AGENTS.md"},
				Codex:  config.CodexTarget{SkillsPath: codex, AgentsMDPath: codex + "-AGENTS.md"},
				Agents: config.AgentsTarget{SkillsPath: agents, AgentsMDPath: agents + "-AGENTS.md"},
			},
		},
		ResolvedSources: []config.ResolvedSource{{Name: "first", Path: first}, {Name: "second", Path: second}},
		ResolvedTargets: config.ResolvedTargets{Claude: resolved(claude), Pi: resolved(pi), Codex: resolved(codex), Agents: resolved(agents)},
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

func writeSourceAgentsFile(t *testing.T, source, name, content string) {
	t.Helper()
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeSourceSkill(t *testing.T, source, name, description, extra string) {
	t.Helper()
	root := filepath.Join(source, "skills", name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: " + name + "\ndescription: '" + description + "'\n"
	if !strings.Contains(extra, "esheep-targets:") {
		manifest += "esheep-targets: [claude, pi, codex, agents]\n"
	}
	manifest += extra + "---\nbody\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeVariantManifest(t *testing.T, source, name, profile, description string) {
	t.Helper()
	root := filepath.Join(source, "skills", name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: " + name + "\ndescription: '" + description + "'\nesheep-targets: [claude, pi, codex, agents]\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL."+profile+".md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertManifestContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("manifest %q = %q, want it to contain %q", path, data, want)
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

func findKnownSkill(t *testing.T, report ListReport, source, skill string) KnownSkill {
	t.Helper()
	for _, known := range report.Skills {
		if known.Source == source && known.Directory == skill {
			return known
		}
	}
	t.Fatalf("skill %s/%s not found in %#v", source, skill, report.Skills)
	return KnownSkill{}
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
