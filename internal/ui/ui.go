// Package ui renders deterministic human and machine command reports.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/jmcampanini/esheep/internal/agentsfile"
	"github.com/jmcampanini/esheep/internal/doctor"
	"github.com/jmcampanini/esheep/internal/install"
	"github.com/jmcampanini/esheep/internal/manage"
	"github.com/jmcampanini/esheep/internal/session"
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
			clean(known.Source), clean(known.Directory), string(known.Readiness), profileGateCell(known.ProfileGate, known.HasManifest), clean(known.Description),
		})
	}
	return writeTable(writer, []string{"SOURCE", "SKILL", "READINESS", "PROFILE GATE", "DESCRIPTION"}, rows, color)
}

// WriteListJSON writes one complete known-skill JSON document.
func WriteListJSON(writer io.Writer, report manage.ListReport) error {
	if report.Diagnostics == nil {
		report.Diagnostics = []manage.Diagnostic{}
	}
	if report.EffectiveProfiles == nil {
		report.EffectiveProfiles = []string{}
	}
	if report.Skills == nil {
		report.Skills = []manage.KnownSkill{}
	}
	return writeJSON(writer, report)
}

// WriteStatus writes a human deployment-status table headed by the effective
// profile list.
func WriteStatus(writer io.Writer, report manage.StatusReport, color bool) error {
	if _, err := fmt.Fprintf(writer, "Effective profiles: %s\n\n", profilesLine(report.EffectiveProfiles)); err != nil {
		return err
	}
	rows := make([][]string, 0, len(report.Skills))
	for _, status := range report.Skills {
		rows = append(rows, []string{
			clean(status.Source),
			clean(status.Directory),
			string(status.Readiness),
			profileGateCell(status.ProfileGate, status.HasManifest),
			targetState(status, "claude"),
			targetState(status, "pi"),
			targetState(status, "codex"),
		})
	}
	if err := writeTable(writer, []string{"SOURCE", "SKILL", "READINESS", "PROFILE GATE", "CLAUDE", "PI", "CODEX"}, rows, color); err != nil {
		return err
	}
	if report.AgentsFile == nil {
		return nil
	}
	agentsRow := []string{
		clean(report.AgentsFile.Source),
		clean(filepath.Base(report.AgentsFile.Path)),
		agentsFileState(report.AgentsFile, "claude"),
		agentsFileState(report.AgentsFile, "pi"),
		agentsFileState(report.AgentsFile, "codex"),
	}
	return writeTable(writer, []string{"SOURCE", "AGENTS FILE", "CLAUDE", "PI", "CODEX"}, [][]string{agentsRow}, color)
}

// WriteStatusJSON writes one complete deployment-status JSON document.
func WriteStatusJSON(writer io.Writer, report manage.StatusReport) error {
	if report.Diagnostics == nil {
		report.Diagnostics = []manage.Diagnostic{}
	}
	if report.EffectiveProfiles == nil {
		report.EffectiveProfiles = []string{}
	}
	if report.Skills == nil {
		report.Skills = []manage.SkillStatus{}
	}
	return writeJSON(writer, report)
}

// WriteProfiles writes the effective and referenced profile lists.
func WriteProfiles(writer io.Writer, report manage.ProfilesReport) error {
	_, err := fmt.Fprintf(writer, "Effective: %s\nReferenced: %s\n", profilesLine(report.Effective), profilesLine(report.Referenced))
	return err
}

// WriteProfilesJSON writes one complete profiles JSON document.
func WriteProfilesJSON(writer io.Writer, report manage.ProfilesReport) error {
	if report.Diagnostics == nil {
		report.Diagnostics = []manage.Diagnostic{}
	}
	if report.Effective == nil {
		report.Effective = []string{}
	}
	if report.Referenced == nil {
		report.Referenced = []string{}
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
		"Summary: installed=%d repaired=%d unchanged=%d pruned=%d inactive=%d disabled=%d blocked=%d failed=%d\n",
		report.Summary.Installed,
		report.Summary.Repaired,
		report.Summary.Unchanged,
		report.Summary.Pruned,
		report.Summary.Inactive,
		report.Summary.Disabled,
		report.Summary.Blocked,
		report.Summary.Failed,
	)
	return err
}

// WriteDoctor writes a human environment-check table.
func WriteDoctor(writer io.Writer, report doctor.Report, color bool) error {
	rows := make([][]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		rows = append(rows, []string{clean(check.Name), string(check.Status), clean(check.Detail)})
	}
	return writeTable(writer, []string{"CHECK", "RESULT", "DETAIL"}, rows, color)
}

// WriteSessionList writes a human session inventory table.
func WriteSessionList(writer io.Writer, report session.ListReport, color bool) error {
	rows := make([][]string, 0, len(report.Sessions))
	for _, entry := range report.Sessions {
		rows = append(rows, []string{
			string(entry.Harness),
			sessionTime(entry),
			dashIfEmpty(clean(entry.Project)),
			dashIfEmpty(clean(entry.Title)),
			clean(entry.Path),
		})
	}
	return writeTable(writer, []string{"HARNESS", "STARTED", "PROJECT", "TITLE", "PATH"}, rows, color)
}

// WriteSessionListJSON writes one complete session inventory JSON document.
func WriteSessionListJSON(writer io.Writer, report session.ListReport) error {
	if report.Diagnostics == nil {
		report.Diagnostics = []session.Diagnostic{}
	}
	if report.Sessions == nil {
		report.Sessions = []session.Session{}
	}
	return writeJSON(writer, report)
}

// WriteSessionSearch writes matching sessions grouped with their hits, each
// hit addressed as a line number within the canonical transcript path.
func WriteSessionSearch(writer io.Writer, report session.SearchReport) error {
	for index, entry := range report.Sessions {
		if index > 0 {
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
		header := []string{string(entry.Harness), sessionTime(entry.Session)}
		if entry.Project != "" {
			header = append(header, clean(entry.Project))
		}
		if entry.Title != "" {
			header = append(header, clean(entry.Title))
		}
		if _, err := fmt.Fprintln(writer, strings.Join(header, "  ")); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(writer, clean(entry.Path)); err != nil {
			return err
		}
		width := 0
		for _, hit := range entry.Hits {
			width = max(width, len(hitRole(hit)))
		}
		for _, hit := range entry.Hits {
			marker := ""
			if hit.Error {
				marker = "error  "
			}
			timestamp := "-"
			if !hit.Timestamp.IsZero() {
				timestamp = hit.Timestamp.Local().Format("15:04:05")
			}
			if _, err := fmt.Fprintf(writer, "  :%-6d %-*s  %s  %s%s\n", hit.Line, width, hitRole(hit), timestamp, marker, clean(hit.Excerpt)); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteSessionSearchJSON writes one complete search JSON document.
func WriteSessionSearchJSON(writer io.Writer, report session.SearchReport) error {
	if report.Diagnostics == nil {
		report.Diagnostics = []session.Diagnostic{}
	}
	if report.Sessions == nil {
		report.Sessions = []session.Match{}
	}
	return writeJSON(writer, report)
}

// WriteSessionDiagnostics writes actionable human session diagnostics.
func WriteSessionDiagnostics(writer io.Writer, diagnostics []session.Diagnostic) error {
	for _, diagnostic := range diagnostics {
		location := diagnostic.Path
		if diagnostic.Harness != "" {
			location += " [" + string(diagnostic.Harness) + "]"
		}
		if _, err := fmt.Fprintf(writer, "%s: %s: %s\n", clean(location), diagnostic.Code, clean(diagnostic.Message)); err != nil {
			return err
		}
	}
	return nil
}

func sessionTime(entry session.Session) string {
	when := entry.StartedAt
	if when.IsZero() {
		when = entry.ModifiedAt
	}
	if when.IsZero() {
		return "-"
	}
	return when.Local().Format("2006-01-02 15:04")
}

func hitRole(hit session.Hit) string {
	if hit.Tool != "" {
		return "tool:" + clean(hit.Tool)
	}
	return string(hit.Role)
}

func dashIfEmpty(value string) string {
	if value == "" {
		return "-"
	}
	return value
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
		case string(doctor.StatusPass), string(install.ActionInstalled), string(install.ActionPruned), string(install.ActionRepaired), string(install.StateSynced), string(manage.ReadinessReady):
			return style.Foreground(lipgloss.Color("42"))
		case string(doctor.StatusFail), string(install.ActionBlocked), string(install.ActionFailed), string(manage.ReadinessCollision), string(manage.ReadinessConflict), string(manage.ReadinessInvalid):
			return style.Foreground(lipgloss.Color("196"))
		case string(install.StateDrifted), string(install.StateMissing), string(agentsfile.StateStale):
			return style.Foreground(lipgloss.Color("214"))
		case string(doctor.StatusSkipped), string(install.ActionDisabled), string(install.ActionInactive):
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

func agentsFileState(status *manage.AgentsFileStatus, target string) string {
	state, exists := status.Targets[target]
	if !exists {
		return "-"
	}
	return string(state)
}

func profileGateCell(profiles []string, hasManifest bool) string {
	if !hasManifest {
		return "-"
	}
	if len(profiles) == 0 {
		return "all"
	}
	return clean(strings.Join(profiles, ", "))
}

func profilesLine(profiles []string) string {
	if len(profiles) == 0 {
		return "(none)"
	}
	return clean(strings.Join(profiles, ", "))
}

func clean(value string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return ' '
		}
		return character
	}, value)
	return strings.Join(strings.Fields(value), " ")
}
