// Package ui renders deterministic human and machine command reports and
// holds the help fragments that describe those reports.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/jmcampanini/esheep/internal/session"
)

// WriteSessionList writes a human session inventory table.
func WriteSessionList(writer io.Writer, report session.ListReport, color bool) error {
	headers := []string{"HARNESS", "ID", "STARTED", "ARCHIVE STATE", "ORIGIN", "PROJECTS", "TITLE", "PATH"}
	rows := make([][]string, 0, len(report.Sessions))
	for _, entry := range report.Sessions {
		rows = append(rows, []string{
			string(entry.Harness),
			dashIfEmpty(clean(entry.ID)),
			sessionTime(entry),
			sessionArchiveState(entry),
			formatOrigin(string(entry.Origin.Kind), entry.Origin.Evidence),
			dashIfEmpty(clean(strings.Join(entry.Projects, ", "))),
			dashIfEmpty(clean(entry.Title)),
			clean(entry.Path),
		})
	}
	return writeTable(writer, headers, rows, color)
}

// WriteSessionListJSON writes one complete session inventory JSON document.
func WriteSessionListJSON(writer io.Writer, report session.ListReport) error {
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
		if len(entry.Projects) != 0 {
			header = append(header, clean(strings.Join(entry.Projects, ", ")))
		}
		if entry.Title != "" {
			header = append(header, clean(entry.Title))
		}
		header = append(header, sessionArchiveState(entry.Session))
		header = append(header, "origin="+formatOrigin(string(entry.Origin.Kind), entry.Origin.Evidence))
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
			metadata := "origin=" + formatOrigin(string(hit.Origin.Kind), hit.Origin.Evidence)
			if hit.CallID != "" {
				metadata += " call_id=" + clean(hit.CallID)
			}
			if hit.ParentCallID != "" {
				metadata += " parent_call_id=" + clean(hit.ParentCallID)
			}
			timestamp := "-"
			if !hit.Timestamp.IsZero() {
				timestamp = hit.Timestamp.Local().Format("2006-01-02 15:04:05")
			}
			if _, err := fmt.Fprintf(writer, "  :%-6d %-*s  %s  %s  %s%s\n", hit.Line, width, hitRole(hit), timestamp, metadata, marker, clean(hit.Excerpt)); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteSessionSearchJSON writes one complete search JSON document.
func WriteSessionSearchJSON(writer io.Writer, report session.SearchReport) error {
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

func sessionArchiveState(entry session.Session) string {
	if entry.Archived {
		return "archived"
	}
	return "active"
}

func hitRole(hit session.Hit) string {
	tool := "?"
	if hit.Tool != "" {
		tool = clean(hit.Tool)
	}
	switch hit.Kind {
	case session.KindToolCall:
		return "call:" + tool
	case session.KindToolResult:
		return "result:" + tool
	}
	return clean(string(hit.Role))
}

func formatOrigin(kind string, evidence map[string]string) string {
	parts := []string{clean(kind)}
	for _, key := range slices.Sorted(maps.Keys(evidence)) {
		parts = append(parts, clean(key)+"="+clean(evidence[key]))
	}
	return strings.Join(parts, " ")
}

func dashIfEmpty(value string) string {
	if value == "" {
		return "-"
	}
	return value
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
		StyleFunc(func(row, _ int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1)
			if color && row == table.HeaderRow {
				return style.Bold(true).Foreground(lipgloss.Color("63"))
			}
			return style
		})
	_, err := fmt.Fprintln(writer, tabular.Render())
	return err
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
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
