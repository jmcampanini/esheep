package ui

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
)

// ColorMode selects when human output carries terminal styling. It
// implements the flag value interface so --color rejects unknown modes
// where they are parsed.
type ColorMode string

// Color modes accepted by --color.
const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

// String returns the mode as typed on the command line.
func (m *ColorMode) String() string { return string(*m) }

// Set accepts auto, always, or never and rejects any other value.
func (m *ColorMode) Set(value string) error {
	switch ColorMode(value) {
	case ColorAuto, ColorAlways, ColorNever:
		*m = ColorMode(value)
		return nil
	default:
		return fmt.Errorf("must be auto, always, or never")
	}
}

// Type names the value in flag usage.
func (*ColorMode) Type() string { return "string" }

// UseColor reports whether human output written to writer should carry
// terminal styling. always and never decide outright. auto styles only a
// terminal, and only while NO_COLOR is unset or empty and TERM is not
// dumb; each stream is judged on its own.
func UseColor(mode ColorMode, writer io.Writer) bool {
	file, ok := writer.(interface{ Fd() uintptr })
	terminal := ok && term.IsTerminal(file.Fd())
	return colorEnabled(mode, terminal, os.Getenv("NO_COLOR"), os.Getenv("TERM"))
}

func colorEnabled(mode ColorMode, terminal bool, noColor, termName string) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	default:
		return terminal && noColor == "" && termName != "dumb"
	}
}
