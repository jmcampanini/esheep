package ui

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmcampanini/esheep/internal/agentsfile"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/jmcampanini/esheep/internal/session"
)

func TestHumanOutputRemovesTerminalControlsFromSourceData(t *testing.T) {
	t.Parallel()
	report := manage.ListReport{
		Complete: true,
		Skills: []manage.KnownSkill{{
			Trigger:     "safe\x1b[31m\nnext\u202espoof",
			Directory:   "demo\rskill",
			HasManifest: true,
			Readiness:   manage.ReadinessReady,
			Source:      "local",
		}},
	}
	var output bytes.Buffer

	if err := WriteList(&output, report, false); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(output.String(), '\x1b') || strings.ContainsRune(output.String(), '\r') || strings.ContainsRune(output.String(), '\u202e') {
		t.Fatalf("output contains terminal controls: %q", output.String())
	}
	if !strings.Contains(output.String(), "TRIGGER") || !strings.Contains(output.String(), "safe [31m next") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestWriteStatusNamesEffectiveLayersAndShowsLayerGateColumn(t *testing.T) {
	t.Parallel()
	report := manage.StatusReport{
		EffectiveLayers: []string{"work", "client"},
		Healthy:         true,
		Skills: []manage.SkillStatus{
			{Directory: "everywhere", HasManifest: true, Readiness: manage.ReadinessReady, Source: "local"},
			{Directory: "gated", HasManifest: true, LayerGate: []string{"work"}, Readiness: manage.ReadinessReady, Source: "local"},
		},
	}
	var output bytes.Buffer

	if err := WriteStatus(&output, report, false); err != nil {
		t.Fatal(err)
	}

	text := output.String()
	if !strings.HasPrefix(text, "Effective layers: work, client\n\n") {
		t.Fatalf("output = %q, want leading effective-layer line", text)
	}
	if !strings.Contains(text, "LAYER GATE") || !strings.Contains(text, "all") || !strings.Contains(text, "work") {
		t.Fatalf("output = %q, want LAYER GATE column with all and work", text)
	}
}

func TestWriteStatusRendersAgentsFileSection(t *testing.T) {
	t.Parallel()
	report := manage.StatusReport{
		AgentsFile: &manage.AgentsFileStatus{
			Path:   "/sources/personal/AGENTS.work.md",
			Layer:  "work",
			Source: "personal",
			Targets: map[string]agentsfile.State{
				"claude": agentsfile.StateSynced,
				"pi":     agentsfile.StateStale,
				"codex":  agentsfile.StateDisabled,
			},
		},
	}
	var output bytes.Buffer

	if err := WriteStatus(&output, report, false); err != nil {
		t.Fatal(err)
	}

	text := output.String()
	for _, want := range []string{"AGENTS FILE", "AGENTS.work.md", "personal", "stale"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output = %q, want it to contain %q", text, want)
		}
	}
}

func TestLayerGateCellDistinguishesUniversalFromMissingManifest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		layers      []string
		hasManifest bool
		want        string
	}{
		{name: "missing manifest", want: "-"},
		{name: "universal", hasManifest: true, want: "all"},
		{name: "gated", layers: []string{"client", "work"}, hasManifest: true, want: "client, work"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := layerGateCell(test.layers, test.hasManifest); got != test.want {
				t.Fatalf("layerGateCell(%v, %t) = %q, want %q", test.layers, test.hasManifest, got, test.want)
			}
		})
	}
}

func TestListAndStatusJSONReportSourceFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(*bytes.Buffer) error
	}{
		{
			name: "list",
			write: func(output *bytes.Buffer) error {
				return WriteListJSON(output, manage.ListReport{
					Complete:        true,
					EffectiveLayers: []string{"work"},
					Skills:          []manage.KnownSkill{{Directory: "gated", LayerGate: []string{"work"}, Readiness: manage.ReadinessReady, Source: "local", Trigger: "Use when asked"}},
				})
			},
		},
		{
			name: "status",
			write: func(output *bytes.Buffer) error {
				return WriteStatusJSON(output, manage.StatusReport{
					EffectiveLayers: []string{"work"},
					Healthy:         true,
					Skills:          []manage.SkillStatus{{Directory: "gated", LayerGate: []string{"work"}, Readiness: manage.ReadinessReady, Source: "local", Trigger: "Use when asked"}},
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer

			if err := test.write(&output); err != nil {
				t.Fatal(err)
			}

			var document struct {
				EffectiveLayers []string `json:"effective_layers"`
				Skills          []struct {
					LayerGate []string `json:"layer_gate"`
					Trigger   string   `json:"trigger"`
				} `json:"skills"`
			}
			if err := json.Unmarshal(output.Bytes(), &document); err != nil {
				t.Fatal(err)
			}
			if len(document.EffectiveLayers) != 1 || document.EffectiveLayers[0] != "work" ||
				len(document.Skills) != 1 || len(document.Skills[0].LayerGate) != 1 || document.Skills[0].LayerGate[0] != "work" {
				t.Fatalf("document = %#v", document)
			}
			if document.Skills[0].Trigger != "Use when asked" {
				t.Fatalf("trigger = %q, want invocation text", document.Skills[0].Trigger)
			}
		})
	}
}

func TestWriteSyncSummaryCountsInactive(t *testing.T) {
	t.Parallel()
	report := manage.SyncReport{Summary: manage.Summary{Inactive: 2, Unchanged: 1}}
	var output bytes.Buffer

	if err := WriteSync(&output, report, false); err != nil {
		t.Fatal(err)
	}

	want := "Summary: installed=0 repaired=0 unchanged=1 pruned=0 inactive=2 disabled=0 blocked=0 failed=0\n"
	if !strings.HasSuffix(output.String(), want) {
		t.Fatalf("output = %q, want summary %q", output.String(), want)
	}
}

func TestWriteLayersReportsEffectiveAndReferenced(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer

	if err := WriteLayers(&output, manage.LayersReport{Referenced: []string{"client", "work"}}); err != nil {
		t.Fatal(err)
	}

	want := "Effective: (none)\nReferenced: client, work\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestWriteSessionListRendersPlaceholdersAndTimes(t *testing.T) {
	t.Parallel()
	report := session.ListReport{
		Complete: true,
		Sessions: []session.Session{
			{
				Harness:   session.HarnessClaude,
				ID:        "abc",
				Path:      "/roots/claude/p/abc.jsonl",
				Projects:  []string{"/Users/u/proj"},
				StartedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local),
				Title:     "Debug permissions",
			},
			{
				Harness:    session.HarnessCodex,
				ID:         "header-def",
				ModifiedAt: time.Date(2026, 8, 25, 9, 30, 0, 0, time.Local),
				Path:       "/roots/codex/rollout-def.jsonl",
			},
		},
	}
	var output bytes.Buffer

	if err := WriteSessionList(&output, report, false, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HARNESS", "2026-08-20 10:00", "Debug permissions", "2026-08-25 09:30", "/roots/codex/rollout-def.jsonl"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
	codexRow := ""
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.Contains(line, "codex") {
			codexRow = line
		}
	}
	if !strings.Contains(codexRow, "-") {
		t.Errorf("codex row = %q, want dash placeholders for absent metadata", codexRow)
	}
	if fields := strings.Fields(codexRow); len(fields) < 2 || fields[1] != "header-def" {
		t.Errorf("codex row = %q, want the session ID from its header", codexRow)
	}
}

func TestJSONWritersEmitPayloadDocuments(t *testing.T) {
	t.Parallel()
	managementDiagnostics := []manage.Diagnostic{{Code: "source-unavailable", Message: "permission denied"}}
	sessionDiagnostics := []session.Diagnostic{{Code: "walk", Message: "permission denied"}}
	for _, test := range []struct {
		name  string
		want  string
		write func(*bytes.Buffer) error
	}{
		{name: "skills list", want: `{"complete":false,"effective_layers":[],"skills":[]}`, write: func(output *bytes.Buffer) error {
			return WriteListJSON(output, manage.ListReport{Diagnostics: managementDiagnostics})
		}},
		{name: "skills status", want: `{"effective_layers":[],"healthy":false,"skills":[]}`, write: func(output *bytes.Buffer) error {
			return WriteStatusJSON(output, manage.StatusReport{Diagnostics: managementDiagnostics})
		}},
		{name: "layers", want: `{"complete":false,"effective":[],"referenced":[]}`, write: func(output *bytes.Buffer) error {
			return WriteLayersJSON(output, manage.LayersReport{Diagnostics: managementDiagnostics})
		}},
		{name: "sessions list", want: `{"complete":false,"period":{"since":null,"until":null},"sessions":[]}`, write: func(output *bytes.Buffer) error {
			return WriteSessionListJSON(output, session.ListReport{Diagnostics: sessionDiagnostics})
		}},
		{name: "sessions search", want: `{"complete":false,"period":{"since":null,"until":null},"sessions":[]}`, write: func(output *bytes.Buffer) error {
			return WriteSessionSearchJSON(output, session.SearchReport{Diagnostics: sessionDiagnostics})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer

			if err := test.write(&output); err != nil {
				t.Fatal(err)
			}

			var got, want any
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(test.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("JSON = %s, want %s", output.String(), test.want)
			}
		})
	}
}

func TestWriteSessionDiagnosticsNamesHarness(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer

	err := WriteSessionDiagnostics(&output, []session.Diagnostic{{
		Code:    "root-missing",
		Harness: session.HarnessPi,
		Message: "session root does not exist; harness skipped",
		Path:    "/roots/pi",
	}}, false)

	if err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "/roots/pi [pi]: root-missing: session root does not exist; harness skipped\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestSessionWritersNameMachinesOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	entry := session.Session{Harness: session.HarnessClaude, ID: "abc", Machine: "nas", Path: "/home/j/.claude/abc.jsonl", StartedAt: time.Date(2026, 9, 12, 9, 14, 0, 0, time.Local)}
	diagnostics := []session.Diagnostic{
		{Code: "machine-timeout", Machine: "laptop", Message: "no reply within 1m0s"},
		{Code: "root-missing", Harness: session.HarnessPi, Machine: "nas", Message: "skipped", Path: "/home/j/.pi"},
	}

	var list, search, stderr bytes.Buffer
	if err := WriteSessionList(&list, session.ListReport{Sessions: []session.Session{entry}}, false, true); err != nil {
		t.Fatal(err)
	}
	if err := WriteSessionSearch(&search, session.SearchReport{Sessions: []session.Match{{Session: entry, Hits: []session.Hit{{Line: 1, Role: session.RoleUser}}}}}, true); err != nil {
		t.Fatal(err)
	}
	if err := WriteSessionDiagnostics(&stderr, diagnostics, true); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(list.String(), "MACHINE") || !strings.HasPrefix(strings.TrimSpace(strings.Split(list.String(), "\n")[1]), "nas") {
		t.Errorf("list output = %q, want a leading MACHINE column", list.String())
	}
	if !strings.HasPrefix(search.String(), "nas  claude  2026-09-12 09:14") {
		t.Errorf("search output = %q, want the machine leading the header", search.String())
	}
	want := "laptop: machine-timeout: no reply within 1m0s\nnas: /home/j/.pi [pi]: root-missing: skipped\n"
	if stderr.String() != want {
		t.Errorf("diagnostics = %q, want %q", stderr.String(), want)
	}

	var local bytes.Buffer
	if err := WriteSessionSearch(&local, session.SearchReport{Sessions: []session.Match{{Session: entry}}}, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(local.String(), "nas") {
		t.Errorf("local search output = %q, want no machine name", local.String())
	}
}
