package e2e

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisabledReadinessInHumanAndJSONReports(t *testing.T) {
	root := filepath.Join(workDir, "disabled-reporting")
	source := filepath.Join(root, "source")
	claude := filepath.Join(root, "claude")
	settings := filepath.Join(root, "config", "esheep", "esheep.toml")
	writeE2ESkill(t, filepath.Join(source, "skills"), "demo", "Use when asked", "esheep-disabled: true\n", nil)
	writeVariablesSettings(t, settings, [][2]string{{"source", source}}, claude)
	environment := map[string]string{"HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": filepath.Join(root, "config")}
	sourceBefore := snapshotTree(t, source)

	for _, command := range []string{"list", "status"} {
		human := runEsheep(t, environment, "skills", command)
		assertSuccess(t, human)
		if !strings.Contains(human.stdout, "READINESS") || !strings.Contains(human.stdout, "disabled") || !strings.Contains(human.stdout, "demo") {
			t.Fatalf("%s human report = %q, want disabled skill readiness", command, human.stdout)
		}
		result := runEsheep(t, environment, "skills", command, "--json")
		assertSuccess(t, result)
		var document struct {
			Skills []struct {
				Readiness string `json:"readiness"`
			} `json:"skills"`
		}
		if err := json.Unmarshal([]byte(result.stdout), &document); err != nil {
			t.Fatal(err)
		}
		if len(document.Skills) != 1 || document.Skills[0].Readiness != "disabled" {
			t.Fatalf("%s JSON report = %#v, want disabled skill readiness", command, document)
		}
		if command == "status" {
			assertStatusHealth(t, result.stdout, true)
		}
	}
	result := runEsheep(t, environment, "sync")
	assertSuccess(t, result)
	if !strings.Contains(result.stdout, "skill disabled") || !strings.Contains(result.stdout, "disabled=3") {
		t.Fatalf("sync report = %q, want explicit skill disabling", result.stdout)
	}
	if got := snapshotTree(t, source); got != sourceBefore {
		t.Fatal("reporting or synchronization changed source files")
	}
}
