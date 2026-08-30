package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/manage"
)

func TestHumanOutputRemovesTerminalControlsFromSourceData(t *testing.T) {
	t.Parallel()
	report := manage.ListReport{
		Complete: true,
		Skills: []manage.KnownSkill{{
			Description: "safe\x1b[31m\nnext\u202espoof",
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
	if !strings.Contains(output.String(), "safe [31m next") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestWriteStatusNamesEffectiveProfilesAndShowsProfileGateColumn(t *testing.T) {
	t.Parallel()
	report := manage.StatusReport{
		EffectiveProfiles: []string{"work", "client"},
		Healthy:           true,
		Skills: []manage.SkillStatus{
			{Directory: "everywhere", HasManifest: true, Readiness: manage.ReadinessReady, Source: "local"},
			{Directory: "gated", HasManifest: true, ProfileGate: []string{"work"}, Readiness: manage.ReadinessReady, Source: "local"},
		},
	}
	var output bytes.Buffer

	if err := WriteStatus(&output, report, false); err != nil {
		t.Fatal(err)
	}

	text := output.String()
	if !strings.HasPrefix(text, "Effective profiles: work, client\n\n") {
		t.Fatalf("output = %q, want leading effective-profile line", text)
	}
	if !strings.Contains(text, "PROFILE GATE") || !strings.Contains(text, "all") || !strings.Contains(text, "work") {
		t.Fatalf("output = %q, want PROFILE GATE column with all and work", text)
	}
}

func TestProfileGateCellDistinguishesUniversalFromMissingManifest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		profiles    []string
		hasManifest bool
		want        string
	}{
		{name: "missing manifest", want: "-"},
		{name: "universal", hasManifest: true, want: "all"},
		{name: "gated", profiles: []string{"client", "work"}, hasManifest: true, want: "client, work"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := profileGateCell(test.profiles, test.hasManifest); got != test.want {
				t.Fatalf("profileGateCell(%v, %t) = %q, want %q", test.profiles, test.hasManifest, got, test.want)
			}
		})
	}
}

func TestListAndStatusJSONNameEffectiveProfilesAndProfileGate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		write func(*bytes.Buffer) error
	}{
		{
			name: "list",
			write: func(output *bytes.Buffer) error {
				return WriteListJSON(output, manage.ListReport{
					Complete:          true,
					EffectiveProfiles: []string{"work"},
					Skills:            []manage.KnownSkill{{Directory: "gated", ProfileGate: []string{"work"}, Readiness: manage.ReadinessReady, Source: "local"}},
				})
			},
		},
		{
			name: "status",
			write: func(output *bytes.Buffer) error {
				return WriteStatusJSON(output, manage.StatusReport{
					EffectiveProfiles: []string{"work"},
					Healthy:           true,
					Skills:            []manage.SkillStatus{{Directory: "gated", ProfileGate: []string{"work"}, Readiness: manage.ReadinessReady, Source: "local"}},
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
				EffectiveProfiles []string `json:"effective_profiles"`
				Skills            []struct {
					ProfileGate []string `json:"profile_gate"`
				} `json:"skills"`
			}
			if err := json.Unmarshal(output.Bytes(), &document); err != nil {
				t.Fatal(err)
			}
			if len(document.EffectiveProfiles) != 1 || document.EffectiveProfiles[0] != "work" ||
				len(document.Skills) != 1 || len(document.Skills[0].ProfileGate) != 1 || document.Skills[0].ProfileGate[0] != "work" {
				t.Fatalf("document = %#v", document)
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

func TestWriteProfilesReportsEffectiveAndReferenced(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer

	if err := WriteProfiles(&output, manage.ProfilesReport{Referenced: []string{"client", "work"}}); err != nil {
		t.Fatal(err)
	}

	want := "Effective: (none)\nReferenced: client, work\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}
