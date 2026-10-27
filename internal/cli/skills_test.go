package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/install"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/jmcampanini/esheep/internal/render"
)

func TestSkillsListReportsAllKnownSkillsWithoutFailingOnValidation(t *testing.T) {
	t.Parallel()
	operations := stubOperations()
	operations.list = func(context.Context, config.LoadResult) manage.ListReport {
		return manage.ListReport{
			Complete: true,
			Diagnostics: []manage.Diagnostic{{
				Code: "required-field", Field: "esheep-trigger", Message: "value is required",
				Path: "/source/broken/SKILL.md", Skill: "broken", Source: "local",
			}},
			Skills: []manage.KnownSkill{
				{Trigger: "Ready skill", Directory: "ready", HasManifest: true, Path: "/source/ready", Readiness: manage.ReadinessReady, Source: "local"},
				{Directory: "broken", HasManifest: true, Path: "/source/broken", Readiness: manage.ReadinessInvalid, Source: "local"},
			},
		}
	}

	code, stdout, stderr := runCommandWithOperations(t, successfulLoad, operations, "skills", "list")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	for _, content := range []string{"SOURCE", "READINESS", "ready", "broken", "invalid"} {
		if !strings.Contains(stdout, content) {
			t.Fatalf("stdout missing %q:\n%s", content, stdout)
		}
	}
	if !strings.Contains(stderr, "/source/broken/SKILL.md: required-field: esheep-trigger: value is required") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestSkillsListJSONReportsIncompleteInventoryOnStderr(t *testing.T) {
	t.Parallel()
	operations := stubOperations()
	operations.list = func(context.Context, config.LoadResult) manage.ListReport {
		return manage.ListReport{
			Diagnostics: []manage.Diagnostic{{Code: "source-unavailable", Message: "not found", Path: "/source", Source: "local"}},
		}
	}

	code, stdout, stderr := runCommandWithOperations(t, successfulLoad, operations, "skills", "list", "--json")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if want := "/source: source-unavailable: not found\nesheep: skills inventory is incomplete\n"; stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if _, exists := document["diagnostics"]; exists {
		t.Fatalf("document carries diagnostics:\n%s", stdout)
	}
	if string(document["complete"]) != "false" || string(document["skills"]) != "[]" {
		t.Fatalf("document = %s", stdout)
	}
}

func TestSkillsStatusIsAHealthCheckInHumanAndJSONModes(t *testing.T) {
	t.Parallel()
	operations := stubOperations()
	operations.status = func(context.Context, config.LoadResult) manage.StatusReport {
		return manage.StatusReport{
			Healthy: false,
			Skills: []manage.SkillStatus{{
				Directory: "demo", HasManifest: true, Path: "/source/demo", Readiness: manage.ReadinessReady, Source: "local",
				Targets: map[string]install.State{
					"claude": install.StateSynced,
					"codex":  install.StateMissing,
					"pi":     install.StateDrifted,
				},
			}},
		}
	}

	code, stdout, stderr := runCommandWithOperations(t, successfulLoad, operations, "skills", "status")
	if code != 1 || !strings.Contains(stdout, "drifted") || !strings.Contains(stdout, "missing") {
		t.Fatalf("human result: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stderr != "esheep: skills status is unhealthy\n" {
		t.Fatalf("stderr = %q", stderr)
	}

	code, stdout, stderr = runCommandWithOperations(t, successfulLoad, operations, "skills", "status", "--json")
	if code != 1 || stderr != "esheep: skills status is unhealthy\n" {
		t.Fatalf("JSON result: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var document manage.StatusReport
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if document.Healthy || document.Skills[0].Targets["codex"] != install.StateMissing {
		t.Fatalf("document = %#v", document)
	}
}

func TestSyncPrintsAggregateActionsAndReturnsApplicationFailure(t *testing.T) {
	t.Parallel()
	operations := stubOperations()
	operations.sync = func(context.Context, config.LoadResult) manage.SyncReport {
		return manage.SyncReport{
			Actions: []install.Result{
				{Action: install.ActionInstalled, Detail: "new installation", Identity: install.Identity{Source: "local", Skill: "one", Target: render.TargetClaude}},
				{Action: install.ActionBlocked, Detail: "destination is not esheep-owned", Identity: install.Identity{Source: "local", Skill: "two", Target: render.TargetPi}},
			},
			Diagnostics: []manage.Diagnostic{{Code: "synchronization", Message: "destination is not esheep-owned", Source: "local", Skill: "two", Target: "pi"}},
			Summary:     manage.Summary{Blocked: 1, Failed: 1, Installed: 1},
		}
	}

	code, stdout, stderr := runCommandWithOperations(t, successfulLoad, operations, "sync")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	for _, content := range []string{"installed", "blocked", "installed=1", "blocked=1", "failed=1"} {
		if !strings.Contains(stdout, content) {
			t.Fatalf("stdout missing %q:\n%s", content, stdout)
		}
	}
	if !strings.Contains(stderr, "synchronization: destination is not esheep-owned") || !strings.Contains(stderr, "esheep: sync completed with 1 failure(s)") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestLayersReportsEffectiveAndReferencedInHumanAndJSONModes(t *testing.T) {
	t.Parallel()
	operations := stubOperations()
	operations.layers = func(context.Context, config.LoadResult) manage.LayersReport {
		return manage.LayersReport{Complete: true, Effective: []string{"work"}, Referenced: []string{"client", "work"}}
	}

	code, stdout, stderr := runCommandWithOperations(t, successfulLoad, operations, "layers")
	if code != 0 || stderr != "" {
		t.Fatalf("human result: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "Effective: work\nReferenced: client, work\n" {
		t.Fatalf("stdout = %q", stdout)
	}

	code, stdout, stderr = runCommandWithOperations(t, successfulLoad, operations, "layers", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("JSON result: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var document manage.LayersReport
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if !document.Complete || len(document.Effective) != 1 || len(document.Referenced) != 2 {
		t.Fatalf("document = %#v", document)
	}
}

func TestLayersExitsNonzeroWhenDiscoveryIsIncomplete(t *testing.T) {
	t.Parallel()
	operations := stubOperations()
	operations.layers = func(context.Context, config.LoadResult) manage.LayersReport {
		return manage.LayersReport{Diagnostics: []manage.Diagnostic{{Code: "source-unavailable", Message: "not found", Path: "/source", Source: "local"}}}
	}

	for _, args := range [][]string{{"layers"}, {"layers", "--json"}} {
		code, stdout, stderr := runCommandWithOperations(t, successfulLoad, operations, args...)
		if code != 1 {
			t.Fatalf("%v: exit code = %d, want 1: stdout=%q stderr=%q", args, code, stdout, stderr)
		}
		if want := "/source: source-unavailable: not found\nesheep: layers inventory is incomplete\n"; stderr != want {
			t.Fatalf("%v: stderr = %q, want %q", args, stderr, want)
		}
	}
}

func successfulLoad(config.LoadOptions) (config.LoadResult, error) {
	return config.LoadResult{}, nil
}

func stubOperations() commandOperations {
	return commandOperations{
		list: func(context.Context, config.LoadResult) manage.ListReport {
			return manage.ListReport{Complete: true}
		},
		layers: func(context.Context, config.LoadResult) manage.LayersReport {
			return manage.LayersReport{Complete: true}
		},
		status: func(context.Context, config.LoadResult) manage.StatusReport {
			return manage.StatusReport{Healthy: true}
		},
		sync: func(context.Context, config.LoadResult) manage.SyncReport {
			return manage.SyncReport{}
		},
	}
}
