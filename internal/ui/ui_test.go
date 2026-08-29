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
			Description: "safe\x1b[31m\nnext",
			Directory:   "demo\rskill",
			Readiness:   manage.ReadinessReady,
			Source:      "local",
		}},
	}
	var output bytes.Buffer

	if err := WriteList(&output, report, false); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(output.String(), '\x1b') || strings.ContainsRune(output.String(), '\r') {
		t.Fatalf("output contains terminal controls: %q", output.String())
	}
	if !strings.Contains(output.String(), "safe [31m next") {
		t.Fatalf("output = %q", output.String())
	}
}
