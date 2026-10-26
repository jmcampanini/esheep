package ui

import (
	"bytes"
	"os"
	"testing"
)

func TestColorEnabledFollowsModeThenDetection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		mode     ColorMode
		terminal bool
		noColor  string
		term     string
		want     bool
	}{
		{name: "terminal with NO_COLOR unset", mode: ColorAuto, terminal: true, term: "xterm-256color", want: true},
		{name: "nonempty NO_COLOR disables", mode: ColorAuto, terminal: true, noColor: "1", term: "xterm-256color"},
		{name: "dumb terminal disables", mode: ColorAuto, terminal: true, term: "dumb"},
		{name: "non-terminal stream disables", mode: ColorAuto, term: "xterm-256color"},
		{name: "always overrides every input", mode: ColorAlways, noColor: "1", term: "dumb", want: true},
		{name: "never overrides a terminal", mode: ColorNever, terminal: true, term: "xterm-256color"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := colorEnabled(test.mode, test.terminal, test.noColor, test.term); got != test.want {
				t.Fatalf("colorEnabled(%q, %t, %q, %q) = %t, want %t", test.mode, test.terminal, test.noColor, test.term, got, test.want)
			}
		})
	}
}

func TestUseColorJudgesTheDestinationStream(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	if UseColor(ColorAuto, file) {
		t.Error("UseColor(auto, regular file) = true, want false")
	}
	if UseColor(ColorAuto, &bytes.Buffer{}) {
		t.Error("UseColor(auto, buffer) = true, want false")
	}
	if !UseColor(ColorAlways, &bytes.Buffer{}) {
		t.Error("UseColor(always, buffer) = false, want true")
	}
}

func TestColorModeSetRejectsUnknownValues(t *testing.T) {
	t.Parallel()
	mode := ColorAuto

	err := mode.Set("sometimes")

	if err == nil || mode != ColorAuto {
		t.Fatalf("Set(%q) = %v, mode = %q, want an error and an unchanged mode", "sometimes", err, mode)
	}
	if got := err.Error(); got != "must be auto, always, or never" {
		t.Fatalf("error = %q", got)
	}
	if err := mode.Set("never"); err != nil || mode != ColorNever {
		t.Fatalf("Set(never) = %v, mode = %q", err, mode)
	}
}
