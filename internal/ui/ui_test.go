package ui

import (
	"bytes"
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

func TestWriteStatusLeadsWithProfilesAndShowsGateColumn(t *testing.T) {
	t.Parallel()
	report := manage.StatusReport{
		Healthy:  true,
		Profiles: []string{"work", "client"},
		Skills: []manage.SkillStatus{
			{Directory: "everywhere", Readiness: manage.ReadinessReady, Source: "local"},
			{Directory: "gated", Profiles: []string{"work"}, Readiness: manage.ReadinessReady, Source: "local"},
		},
	}
	var output bytes.Buffer

	if err := WriteStatus(&output, report, false); err != nil {
		t.Fatal(err)
	}

	text := output.String()
	if !strings.HasPrefix(text, "Profiles: work, client\n\n") {
		t.Fatalf("output = %q, want leading effective-profile line", text)
	}
	if !strings.Contains(text, "PROFILES") || !strings.Contains(text, "all") || !strings.Contains(text, "work") {
		t.Fatalf("output = %q, want PROFILES column with all and work", text)
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
