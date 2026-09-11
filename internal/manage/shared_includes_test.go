package manage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmcampanini/esheep/internal/agentsfile"
	"github.com/jmcampanini/esheep/internal/install"
)

func TestSyncSharedIncludesTrackDriftAndPreserveFailedDestinations(t *testing.T) {
	t.Parallel()
	loaded, skillRoot := personalIncludeConfig(t)
	loaded.Config.Targets.Claude.Enabled = false
	source := loaded.ResolvedSources[0].Path
	manifest := "---\nname: fable-review\nesheep-trigger: Review\nesheep-targets: [pi, codex]\n---\n{{esheep.include \"body\"}}\n{{esheep.include-optional \"extras\"}}"
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.personal.md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSourceAgentsFile(t, source, "AGENTS.personal.md", "{{esheep.include \"body\"}}\n{{esheep.include-optional \"extras\"}}")
	writeSourceAgentsFile(t, source, "esheep-body.md", "Shared agents instructions")
	skillInclude := filepath.Join(skillRoot, "esheep-body.md")
	if err := os.WriteFile(skillInclude, []byte("Shared skill instructions"), 0o600); err != nil {
		t.Fatal(err)
	}
	if report := Sync(t.Context(), loaded); report.Summary.Installed != 4 || report.Summary.Failed != 0 {
		t.Fatalf("initial sync = %#v", report)
	}
	for _, target := range []struct {
		agents string
		skills string
	}{
		{agents: loaded.ResolvedTargets.Pi.AgentsMD, skills: loaded.ResolvedTargets.Pi.Skills},
		{agents: loaded.ResolvedTargets.Codex.AgentsMD, skills: loaded.ResolvedTargets.Codex.Skills},
	} {
		assertManifestContains(t, filepath.Join(target.skills, "fable-review", "SKILL.md"), "Shared skill instructions\n")
		assertAgentsIncludeContent(t, target.agents, "Shared agents instructions\n")
	}

	writeSourceAgentsFile(t, source, "esheep-extras.md", "Shared agents extras")
	skillExtras := filepath.Join(skillRoot, "esheep-extras.md")
	if err := os.WriteFile(skillExtras, []byte("Shared skill extras"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := Status(t.Context(), loaded)
	row := findStatus(t, status, "source", "fable-review")
	for _, target := range []string{"pi", "codex"} {
		if row.Targets[target] != install.StateDrifted || status.AgentsFile.Targets[target] != agentsfile.StateStale {
			t.Fatalf("shared include appearance status = %#v", status)
		}
	}
	if report := Sync(t.Context(), loaded); report.Summary.Repaired != 4 || report.Summary.Failed != 0 {
		t.Fatalf("shared include repair = %#v", report)
	}
	if status := Status(t.Context(), loaded); !status.Healthy {
		t.Fatalf("status after repair = %#v", status)
	}

	for _, path := range []string{skillInclude, filepath.Join(source, agentsfile.DirName, "esheep-body.md")} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if report := Sync(t.Context(), loaded); report.Summary.Failed != 4 || report.Summary.Pruned != 0 {
		t.Fatalf("missing required shared include sync = %#v", report)
	}
	status = Status(t.Context(), loaded)
	row = findStatus(t, status, "source", "fable-review")
	for _, target := range []struct {
		agents string
		name   string
		skills string
	}{
		{agents: loaded.ResolvedTargets.Pi.AgentsMD, name: "pi", skills: loaded.ResolvedTargets.Pi.Skills},
		{agents: loaded.ResolvedTargets.Codex.AgentsMD, name: "codex", skills: loaded.ResolvedTargets.Codex.Skills},
	} {
		if row.Targets[target.name] != install.StateBlocked || status.AgentsFile.Targets[target.name] != agentsfile.StateBlocked {
			t.Fatalf("missing required shared include status = %#v", status)
		}
		assertManifestContains(t, filepath.Join(target.skills, "fable-review", "SKILL.md"), "Shared skill instructions\nShared skill extras")
		assertAgentsIncludeContent(t, target.agents, "Shared agents instructions\nShared agents extras")
	}
}

func TestSyncSharedOptionalAgentsIncludeDisappearanceInstallsEmptyOutput(t *testing.T) {
	t.Parallel()
	loaded, source := agentsIncludeConfig(t)
	writeSourceAgentsFile(t, source, "AGENTS.md", "{{esheep.include-optional \"body\"}}")
	writeSourceAgentsFile(t, source, "esheep-body.md", "Shared instructions")
	if report := Sync(t.Context(), loaded); report.Summary.Installed != 2 || report.Summary.Failed != 0 {
		t.Fatalf("initial sync = %#v", report)
	}
	if err := os.Remove(filepath.Join(source, agentsfile.DirName, "esheep-body.md")); err != nil {
		t.Fatal(err)
	}

	if report := Sync(t.Context(), loaded); report.Summary.Repaired != 2 || report.Summary.Failed != 0 {
		t.Fatalf("optional shared include disappearance sync = %#v", report)
	}
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Claude.AgentsMD, "")
	assertAgentsIncludeContent(t, loaded.ResolvedTargets.Pi.AgentsMD, "")
	if status := Status(t.Context(), loaded); !status.Healthy {
		t.Fatalf("empty agents status = %#v", status)
	}
}
