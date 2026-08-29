// Package ui renders deterministic human and machine command reports.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/jmcampanini/esheep/internal/install"
	"github.com/jmcampanini/esheep/internal/manage"
)

// ShouldColor reports whether human output should contain terminal styling.
func ShouldColor(writer io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// WriteList writes a human known-skill table.
func WriteList(writer io.Writer, report manage.ListReport, color bool) error {
	rows := make([][]string, 0, len(report.Skills))
	for _, known := range report.Skills {
		rows = append(rows, []string{
			clean(known.Source), clean(known.Directory), string(known.Readiness), clean(known.Description),
		})
	}
	return writeTable(writer, []string{"SOURCE", "SKILL", "READINESS", "DESCRIPTION"}, rows, color)
}

// WriteListJSON writes one complete known-skill JSON document.
func WriteListJSON(writer io.Writer, report manage.ListReport) error {
	if report.Diagnostics == nil {
		report.Diagnostics = []manage.Diagnostic{}
	}
	if report.Skills == nil {
		report.Skills = []manage.KnownSkill{}
	}
	return writeJSON(writer, report)
}

// WriteStatus writes a human deployment-status table.
func WriteStatus(writer io.Writer, report manage.StatusReport, color bool) error {
	rows := make([][]string, 0, len(report.Skills))
	for _, status := range report.Skills {
		rows = append(rows, []string{
			clean(status.Source),
			clean(status.Directory),
			string(status.Readiness),
			targetState(status, "claude"),
			targetState(status, "pi"),
			targetState(status, "codex"),
			targetState(status, "agents"),
		})
	}
	return writeTable(writer, []string{"SOURCE", "SKILL", "READINESS", "CLAUDE", "PI", "CODEX", "AGENTS"}, rows, color)
}

// WriteStatusJSON writes one complete deployment-status JSON document.
func WriteStatusJSON(writer io.Writer, report manage.StatusReport) error {
	if report.Diagnostics == nil {
		report.Diagnostics = []manage.Diagnostic{}
	}
	if report.Skills == nil {
		report.Skills = []manage.SkillStatus{}
	}
	return writeJSON(writer, report)
}

// WriteSync writes synchronization actions and their aggregate summary.
func WriteSync(writer io.Writer, report manage.SyncReport, color bool) error {
	rows := make([][]string, 0, len(report.Actions))
	for _, result := range report.Actions {
		target := string(result.Identity.Target)
		if target == "" {
			target = "-"
		}
		skill := result.Identity.Skill
		if skill == "" {
			skill = "-"
		}
		source := result.Identity.Source
		if source == "" {
			source = "-"
		}
		rows = append(rows, []string{string(result.Action), clean(source), clean(skill), target, clean(result.Detail)})
	}
	if err := writeTable(writer, []string{"ACTION", "SOURCE", "SKILL", "TARGET", "DETAIL"}, rows, color); err != nil {
		return err
	}
	_, err := fmt.Fprintf(
		writer,
		"Summary: installed=%d repaired=%d unchanged=%d pruned=%d disabled=%d blocked=%d failed=%d\n",
		report.Summary.Installed,
		report.Summary.Repaired,
		report.Summary.Unchanged,
		report.Summary.Pruned,
		report.Summary.Disabled,
		report.Summary.Blocked,
		report.Summary.Failed,
	)
	return err
}

// WriteDiagnostics writes actionable human diagnostics.
func WriteDiagnostics(writer io.Writer, diagnostics []manage.Diagnostic) error {
	for _, diagnostic := range diagnostics {
		location := diagnostic.Path
		if location == "" {
			location = diagnostic.Source
		}
		if diagnostic.Target != "" {
			location += " [" + diagnostic.Target + "]"
		}
		message := diagnostic.Message
		if message == "" {
			message = diagnostic.Detail
		}
		if message == "" {
			message = diagnostic.Code
		}
		if diagnostic.Field != "" {
			message = diagnostic.Field + ": " + message
		}
		if _, err := fmt.Fprintf(writer, "%s: %s: %s\n", clean(location), diagnostic.Code, clean(message)); err != nil {
			return err
		}
	}
	return nil
}

func writeTable(writer io.Writer, headers []string, rows [][]string, color bool) error {
	tabular := table.New().
		Headers(headers...).
		Rows(rows...).
		Border(lipgloss.HiddenBorder()).
		BorderBottom(false).
		BorderColumn(false).
		BorderHeader(false).
		BorderLeft(false).
		BorderRight(false).
		BorderRow(false).
		BorderTop(false).
		StyleFunc(tableStyles(color, rows))
	_, err := fmt.Fprintln(writer, tabular.Render())
	return err
}

func tableStyles(color bool, rows [][]string) table.StyleFunc {
	return func(row, column int) lipgloss.Style {
		style := lipgloss.NewStyle().Padding(0, 1)
		if !color {
			return style
		}
		if row == table.HeaderRow {
			return style.Bold(true).Foreground(lipgloss.Color("63"))
		}
		if row < 0 || row >= len(rows) || column < 0 || column >= len(rows[row]) {
			return style
		}
		switch rows[row][column] {
		case string(install.ActionInstalled), string(install.ActionPruned), string(install.ActionRepaired), string(install.StateSynced), string(manage.ReadinessReady):
			return style.Foreground(lipgloss.Color("42"))
		case string(install.ActionBlocked), string(install.ActionFailed), string(manage.ReadinessCollision), string(manage.ReadinessInvalid):
			return style.Foreground(lipgloss.Color("196"))
		case string(install.StateDrifted), string(install.StateMissing):
			return style.Foreground(lipgloss.Color("214"))
		case string(install.ActionDisabled):
			return style.Faint(true)
		default:
			return style
		}
	}
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func targetState(status manage.SkillStatus, target string) string {
	state, exists := status.Targets[target]
	if !exists {
		return "-"
	}
	return string(state)
}

func clean(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	return strings.Join(strings.Fields(value), " ")
}
