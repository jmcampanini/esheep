package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderedFieldInInactiveVariantBlocksSyncAndProtectsOutput(t *testing.T) {
	root := filepath.Join(workDir, "trigger-validation")
	source := filepath.Join(root, "source")
	claude := filepath.Join(root, "claude")
	settings := filepath.Join(root, "config", "esheep", "esheep.toml")
	writeE2ESkill(t, filepath.Join(source, "skills"), "demo", "Use when asked", "", nil)
	writeVariablesSettings(t, settings, [][2]string{{"source", source}}, claude)
	environment := map[string]string{"HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": filepath.Join(root, "config")}
	assertSuccess(t, runEsheep(t, environment, "sync"))
	installedBefore := snapshotTree(t, claude)
	variantPath := filepath.Join(source, "skills", "demo", "SKILL.work.md")
	const variant = "---\nname: demo\nesheep-trigger: Use for work\ndescription: conflicting text\nesheep-targets: [claude]\n---\nbody\n"
	if err := os.WriteFile(variantPath, []byte(variant), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceBefore := snapshotTree(t, source)

	listed := runEsheep(t, environment, "skills", "list", "--json")
	status := runEsheep(t, environment, "skills", "status", "--json")
	synced := runEsheep(t, environment, "sync")

	assertSuccess(t, listed)
	var inventory struct {
		Complete    bool `json:"complete"`
		Diagnostics []struct {
			Code  string `json:"code"`
			Field string `json:"field"`
			Path  string `json:"path"`
		} `json:"diagnostics"`
		Skills []struct {
			Readiness string `json:"readiness"`
			Trigger   string `json:"trigger"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(listed.stdout), &inventory); err != nil {
		t.Fatal(err)
	}
	if !inventory.Complete || len(inventory.Skills) != 1 || inventory.Skills[0].Readiness != "invalid" || inventory.Skills[0].Trigger != "Use when asked" {
		t.Fatalf("inventory = %#v", inventory)
	}
	if len(inventory.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one rejected field", inventory.Diagnostics)
	}
	diagnostic := inventory.Diagnostics[0]
	if diagnostic.Code != "unknown-field" || diagnostic.Field != "description" || diagnostic.Path != resolvePath(t, variantPath) {
		t.Fatalf("diagnostic = %#v, want rendered field in inactive variant", diagnostic)
	}
	if status.exitCode != 1 || synced.exitCode != 1 {
		t.Fatalf("status = %#v, sync = %#v, want application failures", status, synced)
	}
	assertStatusHealth(t, status.stdout, false)
	if got := snapshotTree(t, claude); got != installedBefore {
		t.Fatal("invalid source changed installed output")
	}
	if got := snapshotTree(t, source); got != sourceBefore {
		t.Fatal("validation changed source files")
	}
}
