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
	"time"
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

// WriteSessionSearch writes a summary of the whole search, then the page's
// matching sessions grouped with their hits, each hit addressed as a line
// number within the canonical transcript path.
func WriteSessionSearch(writer io.Writer, report session.SearchReport) error {
	if err := writeSearchSummary(writer, report); err != nil {
		return err
	}
	for _, entry := range report.Sessions {
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
		if err := writeSessionHeader(writer, entry.Session); err != nil {
			return err
		}
		width := 0
		for _, hit := range entry.Hits {
			width = max(width, len(eventLabel(hit.Kind, hit.Role, hit.Tool)))
		}
		for _, hit := range entry.Hits {
			marker := ""
			if hit.Error {
				marker = "error  "
			}
			if _, err := fmt.Fprintf(writer, "  :%-6d %-*s  %s  %s  %s%s\n", hit.Line, width, eventLabel(hit.Kind, hit.Role, hit.Tool),
				eventTime(hit.Timestamp), eventMetadata(hit.Origin, hit.CallID, hit.ParentCallID), marker, clean(hit.Excerpt)); err != nil {
				return err
			}
		}
		if entry.OmittedHits > 0 {
			if _, err := fmt.Fprintf(writer, "  %d more %s in this session\n", entry.OmittedHits, plural(entry.OmittedHits, "hit", "hits")); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteSessionPeek writes one session's selected events with their text,
// each addressed as a line number within the canonical transcript path. A
// count stands in wherever the session's other events are not shown, and the
// last line reports what the size cap left out.
func WriteSessionPeek(writer io.Writer, report session.PeekReport) error {
	if err := writeSessionHeader(writer, report.Session); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Showing %d of %d %s.\n\n", len(report.Events), report.TotalEvents, plural(report.TotalEvents, "event", "events")); err != nil {
		return err
	}

	shown := 0
	for _, event := range report.Events {
		if err := writeEventsNotShown(writer, event.Position-shown-1); err != nil {
			return err
		}
		shown = event.Position
		if err := writePeekEvent(writer, event); err != nil {
			return err
		}
	}
	if err := writeEventsNotShown(writer, report.TotalEvents-shown); err != nil {
		return err
	}

	if report.OmittedEvents == 0 {
		return nil
	}
	resume := ""
	if report.NextLine != nil {
		resume = fmt.Sprintf(", resume at --line %d", *report.NextLine)
	}
	_, err := fmt.Fprintf(writer, "\n%d selected %s left out by the size cap; raise --max-bytes%s, or pass --all.\n",
		report.OmittedEvents, plural(report.OmittedEvents, "event", "events"), resume)
	return err
}

func writePeekEvent(writer io.Writer, event session.Event) error {
	markers := ""
	if len(event.OmittedContent) != 0 {
		markers += " omitted_content=" + clean(strings.Join(event.OmittedContent, ","))
	}
	if event.Error {
		markers += "  error"
	}
	if _, err := fmt.Fprintf(writer, ":%d  %s  %s  %s%s\n", event.Line, eventLabel(event.Kind, event.Role, event.Tool),
		eventTime(event.Timestamp), eventMetadata(event.Origin, event.CallID, event.ParentCallID), markers); err != nil {
		return err
	}
	if event.Text != "" {
		for _, line := range strings.Split(event.Text, "\n") {
			if _, err := fmt.Fprintln(writer, strings.TrimRight("    "+strings.Map(printableKeepingTabs, line), " ")); err != nil {
				return err
			}
		}
	}
	if !event.TextCut {
		return nil
	}
	_, err := fmt.Fprintf(writer, "    (text cut: %d of %d bytes shown)\n", len(event.Text), event.TextBytes)
	return err
}

func writeEventsNotShown(writer io.Writer, count int) error {
	if count == 0 {
		return nil
	}
	_, err := fmt.Fprintf(writer, "  (%d %s not shown)\n", count, plural(count, "event", "events"))
	return err
}

// WriteSessionPeekJSON writes one complete session events JSON document.
func WriteSessionPeekJSON(writer io.Writer, report session.PeekReport) error {
	if report.Events == nil {
		report.Events = []session.Event{}
	}
	return writeJSON(writer, report)
}

func writeSessionHeader(writer io.Writer, entry session.Session) error {
	header := []string{string(entry.Harness), sessionTime(entry)}
	if len(entry.Projects) != 0 {
		header = append(header, clean(strings.Join(entry.Projects, ", ")))
	}
	if entry.Title != "" {
		header = append(header, clean(entry.Title))
	}
	header = append(header, sessionArchiveState(entry))
	header = append(header, "origin="+formatOrigin(string(entry.Origin.Kind), entry.Origin.Evidence))
	_, err := fmt.Fprintf(writer, "%s\n%s\n", strings.Join(header, "  "), clean(entry.Path))
	return err
}

func writeSearchSummary(writer io.Writer, report session.SearchReport) error {
	returnedHits := 0
	for _, entry := range report.Sessions {
		returnedHits += len(entry.Hits)
	}
	lines := []string{
		fmt.Sprintf("Matched %d %s and %d %s; showing %d %s and %d %s.",
			report.MatchedSessions, plural(report.MatchedSessions, "session", "sessions"),
			report.MatchedHits, plural(report.MatchedHits, "hit", "hits"),
			len(report.Sessions), plural(len(report.Sessions), "session", "sessions"),
			returnedHits, plural(returnedHits, "hit", "hits")),
		"Sources: " + formatSources(report.Sources) + ".",
	}
	if report.NextCursor != nil {
		lines = append(lines, fmt.Sprintf("%d more %s; next page: --cursor %s",
			report.RemainingSessions, plural(report.RemainingSessions, "session", "sessions"), *report.NextCursor))
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}
	return nil
}

// formatSources lists searched roots as matched-session counts per harness,
// then every other root with its status.
func formatSources(sources []session.Source) string {
	var searched []session.Harness
	counts := make(map[session.Harness]int)
	var other []string
	for _, source := range sources {
		if source.Status != session.SourceSearched {
			other = append(other, strings.TrimSpace(fmt.Sprintf("%s %s %s", source.Harness, source.Status, clean(source.Path))))
			continue
		}
		if _, seen := counts[source.Harness]; !seen {
			searched = append(searched, source.Harness)
		}
		counts[source.Harness] += source.MatchedSessions
	}
	parts := make([]string, 0, len(searched)+len(other))
	for _, harness := range searched {
		parts = append(parts, fmt.Sprintf("%s %d", harness, counts[harness]))
	}
	parts = append(parts, other...)
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func plural(count int, singular, many string) string {
	if count == 1 {
		return singular
	}
	return many
}

// WriteSessionSearchJSON writes one complete search JSON document.
func WriteSessionSearchJSON(writer io.Writer, report session.SearchReport) error {
	if report.Sessions == nil {
		report.Sessions = []session.Match{}
	}
	if report.Sources == nil {
		report.Sources = []session.Source{}
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

func eventLabel(kind session.EventKind, role session.Role, tool string) string {
	if tool == "" {
		tool = "?"
	}
	switch kind {
	case session.KindToolCall:
		return "call:" + clean(tool)
	case session.KindToolResult:
		return "result:" + clean(tool)
	}
	return clean(string(role))
}

func eventTime(timestamp time.Time) string {
	if timestamp.IsZero() {
		return "-"
	}
	return timestamp.Local().Format("2006-01-02 15:04:05")
}

func eventMetadata(origin session.EventOrigin, callID, parentCallID string) string {
	metadata := "origin=" + formatOrigin(string(origin.Kind), origin.Evidence)
	if callID != "" {
		metadata += " call_id=" + clean(callID)
	}
	if parentCallID != "" {
		metadata += " parent_call_id=" + clean(parentCallID)
	}
	return metadata
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
	return strings.Join(strings.Fields(strings.Map(printable, value)), " ")
}

func printableKeepingTabs(character rune) rune {
	if character == '\t' {
		return character
	}
	return printable(character)
}

// printable replaces terminal control and format characters with a space.
func printable(character rune) rune {
	if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
		return ' '
	}
	return character
}
