package manage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmcampanini/esheep/internal/agentsfile"
	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/install"
)

func TestAgentsIncludesRenderSelectedProfilePerTargetAndTrackIncludeDrift(t *testing.T) {
	t.Parallel()
	loaded, source := agentsIncludeConfig(t)
	loaded.EffectiveProfiles = []string{"client", "work"}
	writeSourceAgentsFile(t, source, "AGENTS.md", "base\n{{esheep.include-by-harness \"body\"}}")
	writeSourceAgentsFile(t, source, "AGENTS.work.md", "profile\n{{esheep.sources}}\n{{esheep.include-by-harness \"body\"}}")
	writeSourceAgentsFile(t, source, "esheep-body-claude.md", "Claude\n{{esheep.include-by-harness \"nested\"}}")
	writeSourceAgentsFile(t, source, "esheep-body-pi.md", "Pi\n{{esheep.include-by-harness \"nested\"}}")
	writeSourceAgentsFile(t, source, "esheep-nested-claude.md", "{{esheep.sources}}")
	writeSourceAgentsFile(t, source, "esheep-nested-pi.md", "instructions")
	if err := os.WriteFile(filepath.Join(source, "esheep-body-pi.md"), []byte("wrong container root"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := Sync(t.Context(), loaded)
	if report.Summary.Installed != 2 || report.Summary.Failed != 0 {
		t.Fatalf("initial sync summary = %#v", report.Summary)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, "profile\n- "+source+"\nClaude\n- "+source)
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Pi.AgentsMD, "profile\n- "+source+"\nPi\ninstructions")
	status := Status(t.Context(), loaded)
	if !status.Healthy || status.AgentsFile == nil || status.AgentsFile.Profile != "work" ||
		status.AgentsFile.Source != "source" || status.AgentsFile.Path != filepath.Join(source, agentsfile.DirName, "AGENTS.work.md") {
		t.Fatalf("selected profile status = %#v", status)
	}

	writeSourceAgentsFile(t, source, "esheep-nested-pi.md", "revised instructions")
	status = Status(t.Context(), loaded)
	if status.Healthy || status.AgentsFile.Targets["pi"] != agentsfile.StateStale || status.AgentsFile.Targets["claude"] != agentsfile.StateSynced {
		t.Fatalf("include-only drift status = %#v", status)
	}
	report = Sync(t.Context(), loaded)
	if report.Summary.Repaired != 1 || report.Summary.Unchanged != 1 || report.Summary.Failed != 0 {
		t.Fatalf("include repair summary = %#v", report.Summary)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Pi.AgentsMD, "profile\n- "+source+"\nPi\nrevised instructions")

	loaded.EffectiveProfiles = nil
	report = Sync(t.Context(), loaded)
	if report.Summary.Repaired != 2 || report.Summary.Failed != 0 {
		t.Fatalf("base selection sync summary = %#v", report.Summary)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, "base\nClaude\n- "+source)
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Pi.AgentsMD, "base\nPi\nrevised instructions")
}

func TestAgentsIncludesKeepAgentsDirectoryRootThroughSymlinks(t *testing.T) {
	t.Parallel()
	loaded, source := agentsIncludeConfig(t)
	loaded.Config.Targets.Pi.Enabled = false
	external := t.TempDir()
	writeSourceAgentsFile(t, source, "esheep-nested-claude.md", "agents root")
	for name, content := range map[string]string{
		"selected.md":             "{{esheep.include-by-harness \"body\"}}",
		"body.md":                 "{{esheep.include-by-harness \"nested\"}}",
		"esheep-body-claude.md":   "wrong selected-file root",
		"esheep-nested-claude.md": "wrong nested-file root",
	} {
		if err := os.WriteFile(filepath.Join(external, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{"AGENTS.md": "selected.md", "esheep-body-claude.md": "body.md"} {
		if err := os.Symlink(filepath.Join(external, target), filepath.Join(source, agentsfile.DirName, name)); err != nil {
			t.Fatal(err)
		}
	}

	report := Sync(t.Context(), loaded)
	if report.Summary.Installed != 1 || report.Summary.Failed != 0 {
		t.Fatalf("symlinked includes sync summary = %#v", report.Summary)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, "agents root")
	if status := Status(t.Context(), loaded); !status.Healthy {
		t.Fatalf("symlinked includes status = %#v", status)
	}
}

func TestAgentsOptionalIncludesAppearDisappearAndInstallEmptyOutput(t *testing.T) {
	t.Parallel()
	loaded, source := agentsIncludeConfig(t)
	writeSourceAgentsFile(t, source, "AGENTS.md", "{{esheep.include-by-harness-optional \"extras\"}}")
	writeSourceAgentsFile(t, source, "esheep-extras.md", "not a fallback")
	writeSourceAgentsFile(t, source, "esheep-extras-pi.md", "saved Pi instructions")

	report := Sync(t.Context(), loaded)
	if report.Summary.Installed != 2 || report.Summary.Failed != 0 || len(report.Diagnostics) != 0 {
		t.Fatalf("optional include initial sync = %#v", report)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, "")
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Pi.AgentsMD, "saved Pi instructions")
	if status := Status(t.Context(), loaded); !status.Healthy || len(status.Diagnostics) != 0 {
		t.Fatalf("optional include initial status = %#v", status)
	}

	writeSourceAgentsFile(t, source, "esheep-extras-claude.md", "new Claude instructions")
	if err := os.Remove(filepath.Join(source, agentsfile.DirName, "esheep-extras-pi.md")); err != nil {
		t.Fatal(err)
	}
	status := Status(t.Context(), loaded)
	if status.Healthy || status.AgentsFile.Targets["claude"] != agentsfile.StateStale || status.AgentsFile.Targets["pi"] != agentsfile.StateStale || len(status.Diagnostics) != 0 {
		t.Fatalf("optional appearance/disappearance status = %#v", status)
	}
	report = Sync(t.Context(), loaded)
	if report.Summary.Repaired != 2 || report.Summary.Failed != 0 || len(report.Diagnostics) != 0 {
		t.Fatalf("optional appearance/disappearance sync = %#v", report)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, "new Claude instructions")
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Pi.AgentsMD, "")
	if status := Status(t.Context(), loaded); !status.Healthy {
		t.Fatalf("optional include repaired status = %#v", status)
	}

	if err := os.Remove(filepath.Join(source, agentsfile.DirName, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	report = Sync(t.Context(), loaded)
	if report.Summary.Failed != 0 || report.Summary.Pruned != 0 {
		t.Fatalf("no selected agents file sync = %#v", report)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, "new Claude instructions")
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Pi.AgentsMD, "")
}

func TestAgentsIncludeFailurePreservesOutputAndContinuesTargetsAndSkills(t *testing.T) {
	t.Parallel()
	loaded, source := agentsIncludeConfig(t)
	writeSourceAgentsFile(t, source, "AGENTS.md", "{{esheep.include-by-harness \"body\"}}")
	writeSourceAgentsFile(t, source, "esheep-body-claude.md", "saved Claude instructions")
	writeSourceAgentsFile(t, source, "esheep-body-pi.md", "saved Pi instructions")
	if report := Sync(t.Context(), loaded); report.Summary.Failed != 0 {
		t.Fatalf("initial sync summary = %#v", report.Summary)
	}
	if err := os.Remove(filepath.Join(source, agentsfile.DirName, "esheep-body-claude.md")); err != nil {
		t.Fatal(err)
	}
	writeSourceAgentsFile(t, source, "esheep-body-pi.md", "updated Pi instructions")
	writeSourceSkill(t, source, "unrelated", "Unrelated skill", "")

	report := Sync(t.Context(), loaded)
	if report.Summary.Failed != 1 || report.Summary.Repaired != 1 || report.Summary.Installed != 2 || report.Summary.Pruned != 0 {
		t.Fatalf("failed include sync summary = %#v", report.Summary)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, "saved Claude instructions")
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Pi.AgentsMD, "updated Pi instructions")
	status := Status(t.Context(), loaded)
	if status.Healthy || status.AgentsFile.Targets["claude"] != agentsfile.StateBlocked || status.AgentsFile.Targets["pi"] != agentsfile.StateSynced {
		t.Fatalf("failed include status = %#v", status)
	}
	row := findStatus(t, status, "source", "unrelated")
	if row.Targets["claude"] != install.StateSynced || row.Targets["pi"] != install.StateSynced {
		t.Fatalf("unrelated skill status = %#v", row)
	}

	loaded.Config.Targets.Claude.Enabled = false
	report = Sync(t.Context(), loaded)
	if report.Summary.Failed != 0 || report.Summary.Pruned != 0 {
		t.Fatalf("disabled failed target sync summary = %#v", report.Summary)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, "saved Claude instructions")
	status = Status(t.Context(), loaded)
	if !status.Healthy || status.AgentsFile.Targets["claude"] != agentsfile.StateDisabled {
		t.Fatalf("disabled failed target status = %#v", status)
	}
}

func TestAgentsOptionalDanglingIncludeBlocksAbsentDestination(t *testing.T) {
	t.Parallel()
	loaded, source := agentsIncludeConfig(t)
	writeSourceAgentsFile(t, source, "AGENTS.md", "{{esheep.include-by-harness-optional \"extras\"}}")
	if err := os.Symlink("missing.md", filepath.Join(source, agentsfile.DirName, "esheep-extras-claude.md")); err != nil {
		t.Fatal(err)
	}

	status := Status(t.Context(), loaded)
	if status.Healthy || status.AgentsFile == nil || status.AgentsFile.Targets["claude"] != agentsfile.StateBlocked || status.AgentsFile.Targets["pi"] != agentsfile.StateMissing {
		t.Fatalf("dangling optional include status = %#v", status)
	}
	report := Sync(t.Context(), loaded)
	if report.Summary.Failed != 1 || report.Summary.Installed != 1 {
		t.Fatalf("dangling optional include sync summary = %#v", report.Summary)
	}
	if _, err := os.Stat(loaded.ResolvedTargets.Claude.AgentsMD); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed target destination exists: %v", err)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Pi.AgentsMD, "")
}

func TestAgentsVariablesApplyThroughoutTextWithoutParsingFrontmatter(t *testing.T) {
	t.Parallel()
	loaded, source := agentsIncludeConfig(t)
	loaded.Config.Targets.Pi.Enabled = false
	writeSourceAgentsFile(t, source, "AGENTS.md", "---\nunclosed: [\n{{esheep.sources}}\n---\nbody\n")
	want := "---\nunclosed: [\n- " + source + "\n---\nbody\n"

	report := Sync(t.Context(), loaded)
	if report.Summary.Installed != 1 || report.Summary.Failed != 0 {
		t.Fatalf("agents file with uninterpreted frontmatter sync = %#v", report)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, want)

	writeSourceAgentsFile(t, source, "AGENTS.md", "---\n{{esheep.unknown}}\n---\nbody\n")
	report = Sync(t.Context(), loaded)
	if report.Summary.Failed != 1 || len(report.Diagnostics) != 1 {
		t.Fatalf("agents file with invalid variable sync = %#v", report)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, want)
	status := Status(t.Context(), loaded)
	if status.Healthy || status.AgentsFile == nil || status.AgentsFile.Targets["claude"] != agentsfile.StateBlocked {
		t.Fatalf("agents file with invalid variable status = %#v", status)
	}
}

func agentsIncludeConfig(t *testing.T) (config.LoadResult, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	loaded := testConfig(source, "", filepath.Join(root, "claude"), filepath.Join(root, "pi"), filepath.Join(root, "codex"))
	loaded.ResolvedSources = []config.ResolvedSource{{Name: "source", Path: source}}
	return loaded, source
}

func assertAgentsIncludeContent(t *testing.T, path, want string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Errorf("agents file %q = %q, want %q", path, content, want)
	}
}
