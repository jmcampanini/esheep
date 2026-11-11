package session

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultPeekBytes caps the events a peek returns, measured as their compact
// JSON encoding, when the request sets neither a cap nor all.
const DefaultPeekBytes = 24000

// Event is one decoded transcript event carrying its recorded text.
// Line addresses the event's transcript line, which several events can share.
// Position is the event's 1-based place among every event decoded from the
// transcript, so consecutive positions mean adjacent events. OmittedContent
// names the recorded types of a tool result's content blocks that hold no
// text and so are absent from Text. TextCut marks text a peek shortened to
// fit its cap, and TextBytes is then the size of the whole recorded text.
// CallID and ParentCallID relate events as they do on a Hit.
type Event struct {
	CallID         string      `json:"call_id,omitempty"`
	Error          bool        `json:"error,omitempty"`
	Kind           EventKind   `json:"kind"`
	Line           int         `json:"line"`
	OmittedContent []string    `json:"omitted_content,omitempty"`
	Origin         EventOrigin `json:"origin"`
	ParentCallID   string      `json:"parent_call_id,omitempty"`
	Position       int         `json:"position"`
	Role           Role        `json:"role"`
	Text           string      `json:"text"`
	TextBytes      int         `json:"text_bytes,omitempty"`
	TextCut        bool        `json:"text_cut,omitempty"`
	Timestamp      time.Time   `json:"timestamp,omitzero"`
	Tool           string      `json:"tool,omitempty"`
}

// LineRange selects the transcript lines First through Last, 1-based and
// inclusive.
type LineRange struct {
	First int
	Last  int
}

// ParseLineRange converts a user-supplied line number ("12") or inclusive
// range ("12-20").
func ParseLineRange(value string) (LineRange, error) {
	first, last, isRange := strings.Cut(value, "-")
	if !isRange {
		last = first
	}
	lines := LineRange{}
	var err error
	if lines.First, err = strconv.Atoi(first); err == nil {
		lines.Last, err = strconv.Atoi(last)
	}
	if err != nil || lines.First < 1 || lines.Last < lines.First {
		return LineRange{}, fmt.Errorf("session: line %q is not a line number (12) or ascending range (12-20)", value)
	}
	return lines, nil
}

func (r LineRange) String() string {
	if r.First == r.Last {
		return strconv.Itoa(r.First)
	}
	return fmt.Sprintf("%d-%d", r.First, r.Last)
}

// PeekQuery selects events from the one session ID names. Lines selects the
// events on those transcript lines and Context adds that many returned events
// before and after them. MaxBytes caps the returned events' total size as
// compact JSON; zero removes the cap. IncludeSubagents returns embedded child activity, which is
// otherwise counted but left out.
type PeekQuery struct {
	Context          int
	ID               string
	IncludeSubagents bool
	Lines            []LineRange
	MaxBytes         int
}

// PeekReport holds the selected events of one session in transcript order.
// Complete is false when a session root or candidate transcript could not be
// read, so the ID may also name a transcript the report does not know of.
// TotalEvents counts every event decoded from the transcript. OmittedEvents
// counts the selected events the cap left out, and NextLine is the line of
// the first event on a selected line among them, nil when the cap left out
// none. Its JSON form omits Diagnostics, as ListReport's does.
type PeekReport struct {
	Complete      bool         `json:"complete"`
	Diagnostics   []Diagnostic `json:"-"`
	Events        []Event      `json:"events"`
	NextLine      *int         `json:"next_line"`
	OmittedEvents int          `json:"omitted_events"`
	Session       Session      `json:"session"`
	TotalEvents   int          `json:"total_events"`
}

// Peek reads the events the query selects from the one session its ID names,
// looking among subagent transcripts too. It fails without events when the ID
// names no session or several, the transcript cannot be read to its end, or a
// selected line range holds no returned event. A report whose Complete is
// false carries the events of the one session found.
func Peek(ctx context.Context, roots Roots, query PeekQuery) (PeekReport, error) {
	sessions, _, diagnostics, complete := collect(ctx, roots, Filter{IDs: []string{query.ID}, IncludeSubagents: true})
	report := PeekReport{Complete: complete, Diagnostics: diagnostics}
	if len(sessions) == 0 {
		return report, fmt.Errorf("no session has ID %q; 'sessions list --subagents' prints every ID", query.ID)
	}
	if len(sessions) > 1 {
		paths := make([]string, 0, len(sessions))
		for _, entry := range sessions {
			paths = append(paths, entry.session.Path)
		}
		return report, fmt.Errorf("ID %q names %d transcripts, so peek cannot choose one: %s", query.ID, len(sessions), strings.Join(paths, ", "))
	}
	entry := sessions[0]
	report.Session = entry.session

	var events []Event
	malformed, err := entry.adapter.scan(entry.session.Path, func(e event) bool {
		report.TotalEvents++
		if e.subagent && !query.IncludeSubagents {
			return true
		}
		events = append(events, Event{
			CallID:         e.callID,
			Error:          e.failed,
			Kind:           e.kind,
			Line:           e.line,
			OmittedContent: e.textless,
			Origin:         e.origin,
			ParentCallID:   e.parentCallID,
			Position:       report.TotalEvents,
			Role:           e.role,
			Text:           e.text,
			Timestamp:      e.timestamp,
			Tool:           e.tool,
		})
		return true
	})
	if err != nil {
		return report, fmt.Errorf("read transcript %q: %w", entry.session.Path, err)
	}
	if malformed > 0 {
		report.Diagnostics = append(report.Diagnostics, Diagnostic{
			Code: codeMalformedLines, Harness: entry.session.Harness,
			Message: fmt.Sprintf("skipped %d unparseable lines", malformed), Path: entry.session.Path,
		})
	}

	requested, around, err := query.selectEvents(events)
	if err != nil {
		return report, err
	}
	returned := query.fit(events, requested, around)
	for index := range events {
		if returned[index] {
			report.Events = append(report.Events, events[index])
		}
	}
	report.OmittedEvents = len(requested) + len(around) - len(report.Events)
	if first := slices.IndexFunc(requested, func(index int) bool { return !returned[index] }); first >= 0 {
		report.NextLine = &events[requested[first]].Line
	}
	return report, nil
}

// selectEvents returns the indexes of the events on the selected lines, in
// transcript order, and of their context, nearest first. Events arrive in
// transcript order, so each line range covers one run of them.
func (q PeekQuery) selectEvents(events []Event) (requested, around []int, err error) {
	const far = -1
	distance := make([]int, len(events))
	for index := range distance {
		distance[index] = far
	}
	var unmatched []string
	for _, lines := range q.Lines {
		first := slices.IndexFunc(events, func(e Event) bool { return e.Line >= lines.First })
		if first < 0 || events[first].Line > lines.Last {
			unmatched = append(unmatched, lines.String())
			continue
		}
		end := first
		for end < len(events) && events[end].Line <= lines.Last {
			end++
		}
		for index := max(0, first-q.Context); index < min(len(events), end+q.Context); index++ {
			gap := max(0, first-index, index-end+1)
			if distance[index] == far || gap < distance[index] {
				distance[index] = gap
			}
		}
	}
	if len(unmatched) != 0 {
		return nil, nil, fmt.Errorf("no event to return on line %s; such a line is past the end, undecoded, unparseable, or child activity that needs --subagents",
			strings.Join(unmatched, ", "))
	}

	for index, gap := range distance {
		switch {
		case gap == 0:
			requested = append(requested, index)
		case gap != far:
			around = append(around, index)
		}
	}
	slices.SortStableFunc(around, func(left, right int) int { return cmp.Compare(distance[left], distance[right]) })
	return requested, around, nil
}

// fit reports which selected events the cap admits, cutting text in events
// where it must. Requested events are admitted a transcript line at a time
// until a line does not fit. The first line is always admitted, with its text
// cut as far as the cap needs, so a peek that resumes at NextLine always
// advances. Context is admitted nearest first, and only once every requested
// event fits whole.
func (q PeekQuery) fit(events []Event, requested, around []int) []bool {
	returned := make([]bool, len(events))
	if q.MaxBytes == 0 {
		for _, index := range slices.Concat(requested, around) {
			returned[index] = true
		}
		return returned
	}

	remaining := q.MaxBytes
	for start := 0; start < len(requested); {
		end, size := start, 0
		for end < len(requested) && events[requested[end]].Line == events[requested[start]].Line {
			size += encodedSize(events[requested[end]])
			end++
		}
		line := requested[start:end]
		if size > remaining {
			if start == 0 {
				cutLineToFit(events, line, remaining)
				for _, index := range line {
					returned[index] = true
				}
			}
			return returned
		}
		for _, index := range line {
			returned[index] = true
		}
		remaining -= size
		start = end
	}

	for _, index := range around {
		size := encodedSize(events[index])
		if size > remaining {
			break
		}
		returned[index] = true
		remaining -= size
	}
	return returned
}

// encodedSize is the size of the event's compact JSON encoding, the unit a
// peek's cap counts.
func encodedSize(e Event) int {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(e)
	return encoded.Len() - len("\n")
}

// cutLineToFit cuts the text of one line's events so that together they
// encode within limit bytes where cutting can achieve that. Each event keeps
// as much text as the limit allows once the events after it are left the
// least room they can take.
func cutLineToFit(events []Event, line []int, limit int) {
	least := make([]int, len(line))
	reserved := 0
	for n, index := range line {
		emptied := events[index]
		least[n] = cutToFit(&emptied, 0)
		reserved += least[n]
	}
	for n, index := range line {
		reserved -= least[n]
		limit -= cutToFit(&events[index], limit-reserved)
	}
}

// cutToFit shortens the event's text at a UTF-8 boundary, recording the cut,
// until the event encodes within limit bytes or no text is left. An event
// that cutting cannot make smaller stays whole. It returns the event's
// encoded size.
func cutToFit(e *Event, limit int) int {
	whole := *e
	size := encodedSize(whole)
	if size <= limit || whole.Text == "" {
		return size
	}

	e.TextBytes, e.TextCut = len(whole.Text), true
	fits := func(length int) bool {
		for length > 0 && length < len(whole.Text) && !utf8.RuneStart(whole.Text[length]) {
			length--
		}
		e.Text = whole.Text[:length]
		return encodedSize(*e) <= limit
	}
	kept := sort.Search(len(whole.Text), func(length int) bool { return !fits(length + 1) })
	fits(kept)
	if cut := encodedSize(*e); cut < size {
		return cut
	}
	*e = whole
	return size
}
