package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func eventTexts(events []Event) []string {
	texts := make([]string, 0, len(events))
	for _, e := range events {
		texts = append(texts, e.Text)
	}
	return texts
}

func eventPositions(events []Event) []int {
	positions := make([]int, 0, len(events))
	for _, e := range events {
		positions = append(positions, e.Position)
	}
	return positions
}

func TestPeekReturnsCompleteTextOfEveryEvent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "-proj", "session.jsonl")
	reply := "Retry temporary failures up to three times. " + strings.Repeat("Back off between attempts. ", 12) + "Do not retry authentication failures."
	command := `cat <<'EOF' > notes.md\n` + strings.Repeat(`a line of notes\n`, 20) + `EOF\ngh pr edit 7 --body-file notes.md`
	writeTranscript(t, path, time.Now(),
		`{"type":"user","timestamp":"2026-08-20T10:00:00Z","message":{"content":"Which failures should we retry?"}}`,
		`{"type":"assistant","timestamp":"2026-08-20T10:00:05Z","message":{"content":[{"type":"text","text":"`+reply+`"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"`+command+`"}}]}}`,
		`{"type":"user","timestamp":"2026-08-20T10:00:09Z","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"first line\nsecond line"}],"is_error":true}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_2","content":[{"type":"image","source":{}}]}]}}`,
	)

	report, err := Peek(context.Background(), Roots{Claude: root}, PeekQuery{ID: "session", Lines: []LineRange{{1, 4}}})

	if err != nil {
		t.Fatalf("Peek() error = %v", err)
	}
	want := []Event{
		{Kind: KindMessage, Line: 1, Position: 1, Role: RoleUser, Text: "Which failures should we retry?"},
		{Kind: KindMessage, Line: 2, Position: 2, Role: RoleAssistant, Text: reply},
		{Kind: KindToolCall, Line: 2, Position: 3, Role: RoleTool, Text: `{"command":"` + command + `"}`, CallID: "toolu_1", Tool: "Bash"},
		{Kind: KindToolResult, Line: 3, Position: 4, Role: RoleTool, Text: "first line\nsecond line", CallID: "toolu_1", Tool: "Bash", Error: true},
		{Kind: KindToolResult, Line: 4, Position: 5, Role: RoleTool, Text: "", CallID: "toolu_2", OmittedContent: []string{"image"}},
	}
	if len(report.Events) != len(want) || report.TotalEvents != len(want) {
		t.Fatalf("Peek() = %d events of %d, want %d of %d", len(report.Events), report.TotalEvents, len(want), len(want))
	}
	for index, got := range report.Events {
		expected := want[index]
		if got.Kind != expected.Kind || got.Line != expected.Line || got.Position != expected.Position || got.Role != expected.Role ||
			got.Text != expected.Text || got.CallID != expected.CallID || got.Tool != expected.Tool || got.Error != expected.Error ||
			!slices.Equal(got.OmittedContent, expected.OmittedContent) || got.TextCut {
			t.Errorf("event %d = %+v, want %+v", index, got, expected)
		}
	}
	if started := report.Events[0].Timestamp; !started.Equal(time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)) || !report.Events[4].Timestamp.IsZero() {
		t.Errorf("timestamps = %v and %v, want the recorded time and none", started, report.Events[4].Timestamp)
	}
	if report.Events[0].Origin.Kind != EventOriginUnknown || report.Events[2].Origin.Kind != EventOriginAgent || report.Events[3].Origin.Kind != EventOriginTool {
		t.Errorf("origins = %+v", report.Events)
	}
}

func TestPeekSelectsLinesWithEventContext(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "-proj", "session.jsonl")
	writeTranscript(t, path, time.Now(),
		`{"type":"user","message":{"content":"one"}}`,
		`{"type":"mode","mode":"normal"}`,
		`{"type":"user","message":{"content":"two"}}`,
		`{"type":"user","message":{"content":"three"}}`,
		`this line is not json`,
		`{"type":"user","message":{"content":"four"}}`,
		`{"type":"user","message":{"content":"five"}}`,
		`{"type":"user","message":{"content":"six"}}`,
	)

	for _, test := range []struct {
		context   int
		lines     []LineRange
		name      string
		positions []int
		texts     []string
	}{
		{name: "every line", lines: []LineRange{{1, 8}}, texts: []string{"one", "two", "three", "four", "five", "six"}, positions: []int{1, 2, 3, 4, 5, 6}},
		{name: "one line", lines: []LineRange{{4, 4}}, texts: []string{"three"}, positions: []int{3}},
		{name: "context counts events across undecoded lines", lines: []LineRange{{4, 4}}, context: 1, texts: []string{"two", "three", "four"}, positions: []int{2, 3, 4}},
		{name: "range with gaps inside", lines: []LineRange{{2, 5}}, texts: []string{"two", "three"}, positions: []int{2, 3}},
		{name: "separate lines keep their gap", lines: []LineRange{{8, 8}, {1, 1}}, texts: []string{"one", "six"}, positions: []int{1, 6}},
		{name: "overlapping context returns each event once", lines: []LineRange{{3, 4}, {7, 7}}, context: 1, texts: []string{"one", "two", "three", "four", "five", "six"}, positions: []int{1, 2, 3, 4, 5, 6}},
		{name: "context stops at the transcript ends", lines: []LineRange{{1, 1}}, context: 9, texts: []string{"one", "two", "three", "four", "five", "six"}, positions: []int{1, 2, 3, 4, 5, 6}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := Peek(context.Background(), Roots{Claude: root}, PeekQuery{Context: test.context, ID: "session", Lines: test.lines})

			if err != nil {
				t.Fatalf("Peek() error = %v", err)
			}
			if got := eventTexts(report.Events); !slices.Equal(got, test.texts) {
				t.Errorf("texts = %q, want %q", got, test.texts)
			}
			if report.OmittedEvents != 0 || report.NextLine != nil {
				t.Errorf("omitted = %d, next line = %v, want nothing left out without a cap", report.OmittedEvents, report.NextLine)
			}
			if got := eventPositions(report.Events); !slices.Equal(got, test.positions) {
				t.Errorf("positions = %v, want %v", got, test.positions)
			}
			if report.TotalEvents != 6 {
				t.Errorf("TotalEvents = %d, want 6", report.TotalEvents)
			}
			if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != codeMalformedLines || report.Diagnostics[0].Path != path {
				t.Errorf("diagnostics = %+v, want one malformed-lines diagnostic", report.Diagnostics)
			}
		})
	}
}

func TestPeekFailsWhenASelectedLineReturnsNoEvent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "-proj", "session.jsonl")
	writeTranscript(t, path, time.Now(),
		`{"type":"user","message":{"content":"one"}}`,
		`{"type":"mode","mode":"normal"}`,
		`this line is not json`,
	)

	for _, test := range []struct {
		lines []LineRange
		name  string
		want  string
	}{
		{name: "undecoded line", lines: []LineRange{{2, 2}}, want: "line 2;"},
		{name: "unparseable line", lines: []LineRange{{3, 3}}, want: "line 3;"},
		{name: "past the end", lines: []LineRange{{4, 9}}, want: "line 4-9;"},
		{name: "one of several", lines: []LineRange{{1, 1}, {2, 3}, {7, 7}}, want: "line 2-3, 7;"},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := Peek(context.Background(), Roots{Claude: root}, PeekQuery{Context: 2, ID: "session", Lines: test.lines})

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Peek() error = %v, want containing %q", err, test.want)
			}
			if len(report.Events) != 0 {
				t.Errorf("events = %+v, want none beside a failure", report.Events)
			}
		})
	}
}

func TestPeekResolvesOneSessionByID(t *testing.T) {
	roots := fixtureRoots(t)
	every := []LineRange{{1, 99}}

	claude, err := Peek(context.Background(), roots, PeekQuery{ID: "11111111-aaaa-bbbb-cccc-222222222222", Lines: every})
	if err != nil {
		t.Fatalf("Peek(claude) error = %v", err)
	}
	if !claude.Complete || claude.Session.Harness != HarnessClaude || claude.Session.Title != "Debug permissions" || len(claude.Events) != 4 {
		t.Errorf("Peek(claude) = %+v, complete %t, with %d events; want the titled Claude session, complete, and 4 events", claude.Session, claude.Complete, len(claude.Events))
	}

	child, err := Peek(context.Background(), roots, PeekQuery{ID: "agent-abc", Lines: every})
	if err != nil {
		t.Fatalf("Peek(subagent transcript) error = %v", err)
	}
	if !child.Session.Subagent || !slices.Equal(eventTexts(child.Events), []string{"subagent prompt about GOMODCACHE"}) {
		t.Errorf("Peek(subagent transcript) = %+v, %q", child.Session, eventTexts(child.Events))
	}

	rollout, err := Peek(context.Background(), roots, PeekQuery{ID: "33333333-dddd-eeee-ffff-444444444444", Lines: []LineRange{{9, 9}}})
	if err != nil {
		t.Fatalf("Peek(codex) error = %v", err)
	}
	if rollout.Session.Harness != HarnessCodex || !slices.Equal(eventTexts(rollout.Events), []string{"Wall time: 2 seconds\nOutput:\nFAIL"}) {
		t.Errorf("Peek(codex) = %+v, %q", rollout.Session, eventTexts(rollout.Events))
	}
}

func TestPeekRejectsIDsThatDoNotNameOneSession(t *testing.T) {
	roots := fixtureRoots(t)
	first := filepath.Join(roots.Claude, "-Users-u-proj", "11111111-aaaa-bbbb-cccc-222222222222.jsonl")
	second := filepath.Join(roots.Claude, "-Users-u-copy", "11111111-aaaa-bbbb-cccc-222222222222.jsonl")
	writeTranscript(t, second, time.Now(), `{"type":"user","message":{"content":"copied"}}`)

	absent, err := Peek(context.Background(), roots, PeekQuery{ID: "11111111-aaaa", Lines: []LineRange{{1, 1}}})
	if err == nil || !strings.Contains(err.Error(), `no session has ID "11111111-aaaa"`) || len(absent.Events) != 0 {
		t.Errorf("Peek(ID prefix) = %d events, error %v; want a no-session failure", len(absent.Events), err)
	}

	shared, err := Peek(context.Background(), roots, PeekQuery{ID: "11111111-aaaa-bbbb-cccc-222222222222", Lines: []LineRange{{1, 1}}})
	if err == nil || !strings.Contains(err.Error(), "names 2 transcripts") || !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) || len(shared.Events) != 0 {
		t.Errorf("Peek(shared ID) = %d events, error %v; want a failure naming both transcripts", len(shared.Events), err)
	}
}

func TestPeekIsIncompleteWhenATranscriptThatCouldCarryTheIDIsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failures require an unprivileged user")
	}
	root := t.TempDir()
	writeTranscript(t, filepath.Join(root, "-proj", "session.jsonl"), time.Now(), `{"type":"user","message":{"content":"readable"}}`)
	unreadable := filepath.Join(root, "-copy", "session.jsonl")
	writeTranscript(t, unreadable, time.Now(), `{"type":"user","message":{"content":"unreadable"}}`)
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })

	report, err := Peek(context.Background(), Roots{Claude: root}, PeekQuery{ID: "session", Lines: []LineRange{{1, 1}}})

	if err != nil {
		t.Fatalf("Peek() error = %v", err)
	}
	if report.Complete || !slices.Equal(eventTexts(report.Events), []string{"readable"}) {
		t.Errorf("Peek() complete = %t, texts = %q; want an incomplete report of the readable session", report.Complete, eventTexts(report.Events))
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != codeTranscriptRead || report.Diagnostics[0].Path != unreadable {
		t.Errorf("diagnostics = %+v, want one transcript-read diagnostic for the unreadable transcript", report.Diagnostics)
	}
}

func TestPeekCountsButWithholdsCoworkChildActivity(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, filepath.Join(root, "scope", "group", "local_main", "audit.jsonl"), time.Now(),
		`{"type":"user","message":{"content":"primary prompt"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"call","name":"Agent","input":{"prompt":"delegate"}}]}}`,
		`{"type":"user","parent_tool_use_id":"call","message":{"content":"child prompt"}}`,
		`{"type":"assistant","parent_tool_use_id":"call","message":{"content":[{"type":"text","text":"child reply"}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"call","content":"child returned"}]}}`,
	)
	roots := Roots{ClaudeCowork: root}

	primary, err := Peek(context.Background(), roots, PeekQuery{ID: "local_main", Lines: []LineRange{{1, 5}}})
	if err != nil {
		t.Fatalf("Peek() error = %v", err)
	}
	if got := eventPositions(primary.Events); !slices.Equal(got, []int{1, 2, 5}) || primary.TotalEvents != 5 {
		t.Errorf("primary positions = %v of %d, want 1, 2, 5 of 5", got, primary.TotalEvents)
	}

	if _, err := Peek(context.Background(), roots, PeekQuery{ID: "local_main", Lines: []LineRange{{3, 3}}}); err == nil || !strings.Contains(err.Error(), "--subagents") {
		t.Errorf("Peek(child line) error = %v, want a failure naming --subagents", err)
	}

	children, err := Peek(context.Background(), roots, PeekQuery{ID: "scope/group/local_main", IncludeSubagents: true, Lines: []LineRange{{3, 4}}})
	if err != nil {
		t.Fatalf("Peek(subagents) error = %v", err)
	}
	if got := eventTexts(children.Events); !slices.Equal(got, []string{"child prompt", "child reply"}) || children.Events[0].ParentCallID != "call" {
		t.Errorf("child events = %q with parent %q, want both child events under call", got, children.Events[0].ParentCallID)
	}
}

func TestPeekNamesToolResultContentWithoutText(t *testing.T) {
	base := t.TempDir()
	roots := Roots{Claude: filepath.Join(base, "claude"), CodexSessions: filepath.Join(base, "codex"), Pi: filepath.Join(base, "pi")}
	writeTranscript(t, filepath.Join(roots.Claude, "-proj", "session.jsonl"), time.Now(),
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":[{"type":"tool_reference","tool_name":"x"},{"type":"text","text":"caption"},{"type":"image"},{"type":"tool_reference","tool_name":"y"}]}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b","content":[{"type":"text","text":""}]}]}}`,
	)
	writeTranscript(t, filepath.Join(roots.CodexSessions, "rollout-2026-08-25T09-00-00-33333333-dddd-eeee-ffff-444444444444.jsonl"), time.Now(),
		`{"type":"session_meta","payload":{"id":"33333333-dddd-eeee-ffff-444444444444"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"c","output":[{"type":"input_text","text":"Script completed"},{"type":"input_image","image_url":"data:"}]}}`,
	)
	writeTranscript(t, filepath.Join(roots.Pi, "--proj--", "session.jsonl"), time.Now(),
		`{"type":"session","id":"pi-session"}`,
		`{"type":"message","message":{"role":"toolResult","toolCallId":"d","toolName":"read","content":[{"type":"image","data":"","mimeType":"image/png"}]}}`,
	)

	for _, test := range []struct {
		id      string
		omitted [][]string
		texts   []string
	}{
		{id: "session", texts: []string{"caption", ""}, omitted: [][]string{{"tool_reference", "image"}, nil}},
		{id: "33333333-dddd-eeee-ffff-444444444444", texts: []string{"Script completed"}, omitted: [][]string{{"input_image"}}},
		{id: "pi-session", texts: []string{""}, omitted: [][]string{{"image"}}},
	} {
		report, err := Peek(context.Background(), roots, PeekQuery{ID: test.id, Lines: []LineRange{{1, 2}}})

		if err != nil {
			t.Fatalf("Peek(%q) error = %v", test.id, err)
		}
		if got := eventTexts(report.Events); !slices.Equal(got, test.texts) {
			t.Fatalf("Peek(%q) texts = %q, want %q", test.id, got, test.texts)
		}
		for index, e := range report.Events {
			if !slices.Equal(e.OmittedContent, test.omitted[index]) {
				t.Errorf("Peek(%q) event %d omitted content = %q, want %q", test.id, index, e.OmittedContent, test.omitted[index])
			}
		}
	}
}

func TestPeekCapAdmitsSelectedLinesThenNearestContext(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, filepath.Join(root, "-proj", "session.jsonl"), time.Now(),
		`{"type":"user","message":{"content":"aaaa"}}`,
		`{"type":"user","message":{"content":"`+strings.Repeat("b", 100)+`"}}`,
		`{"type":"user","message":{"content":"cc"}}`,
		`{"type":"user","message":{"content":"dddddd"}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"h`+strings.Repeat("é", 30)+`"},{"type":"tool_use","id":"t","name":"Bash","input":{"key":"`+strings.Repeat("v", 60)+`"}}]}}`,
	)
	peek := func(maxBytes, context int, lines ...LineRange) PeekReport {
		t.Helper()
		report, err := Peek(t.Context(), Roots{Claude: root}, PeekQuery{Context: context, ID: "session", Lines: lines, MaxBytes: maxBytes})
		if err != nil {
			t.Fatalf("Peek() error = %v", err)
		}
		return report
	}
	encoded := func(events ...Event) int {
		t.Helper()
		total := 0
		for _, e := range events {
			data, err := json.Marshal(e)
			if err != nil {
				t.Fatalf("Marshal(%+v): %v", e, err)
			}
			total += len(data)
		}
		return total
	}
	whole := peek(0, 0, LineRange{1, 5}).Events
	if len(whole) != 6 {
		t.Fatalf("uncapped peek = %d events, want 6", len(whole))
	}
	a, b, c, d, hello, call := whole[0], whole[1], whole[2], whole[3], whole[4], whole[5]
	line := func(n int) *int { return &n }

	for _, test := range []struct {
		context   int
		lines     []LineRange
		maxBytes  int
		name      string
		nextLine  *int
		omitted   int
		positions []int
	}{
		{name: "admits whole lines up to the cap", lines: []LineRange{{1, 5}}, maxBytes: encoded(a, b, c), positions: []int{1, 2, 3}, omitted: 3, nextLine: line(4)},
		{name: "stops before the line that does not fit", lines: []LineRange{{1, 5}}, maxBytes: encoded(a, b, c, d) - 1, positions: []int{1, 2, 3}, omitted: 3, nextLine: line(4)},
		{name: "resumes at the next line", lines: []LineRange{{4, 5}}, maxBytes: encoded(d, hello, call), positions: []int{4, 5, 6}},
		{name: "keeps a shared line together", lines: []LineRange{{4, 5}}, maxBytes: encoded(d, hello), positions: []int{4}, omitted: 2, nextLine: line(5)},
		{name: "does not skip ahead to a smaller line", lines: []LineRange{{2, 3}}, maxBytes: encoded(b) + 1, positions: []int{2}, omitted: 1, nextLine: line(3)},
		{name: "admits nearest context that fits", lines: []LineRange{{3, 3}}, context: 2, maxBytes: encoded(c, b), positions: []int{2, 3}, omitted: 3},
		{name: "stops context at the first event that does not fit", lines: []LineRange{{3, 3}}, context: 2, maxBytes: encoded(c, b) - 1, positions: []int{3}, omitted: 4},
		{name: "gives context nothing when a selected line is left out", lines: []LineRange{{1, 1}, {4, 4}}, context: 1, maxBytes: encoded(a, d) - 1, positions: []int{1}, omitted: 4, nextLine: line(4)},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := peek(test.maxBytes, test.context, test.lines...)

			if got := eventPositions(report.Events); !slices.Equal(got, test.positions) {
				t.Fatalf("positions = %v, want %v", got, test.positions)
			}
			for _, e := range report.Events {
				if e.TextCut || e.TextBytes != 0 || e.Text != whole[e.Position-1].Text {
					t.Errorf("event at position %d = %q cut %t, want its whole text", e.Position, e.Text, e.TextCut)
				}
			}
			if report.OmittedEvents != test.omitted {
				t.Errorf("OmittedEvents = %d, want %d", report.OmittedEvents, test.omitted)
			}
			if (report.NextLine == nil) != (test.nextLine == nil) || (report.NextLine != nil && *report.NextLine != *test.nextLine) {
				t.Errorf("NextLine = %v, want %v", report.NextLine, test.nextLine)
			}
		})
	}

	t.Run("cuts the first line to the cap", func(t *testing.T) {
		maxBytes := encoded(b) - 10
		report := peek(maxBytes, 0, LineRange{2, 3})

		if len(report.Events) != 1 || report.OmittedEvents != 1 || report.NextLine == nil || *report.NextLine != 3 {
			t.Fatalf("report = %d events, %d omitted, next line %v; want the cut event alone and line 3 next", len(report.Events), report.OmittedEvents, report.NextLine)
		}
		cut := report.Events[0]
		if !cut.TextCut || cut.TextBytes != 100 || cut.Text == "" || !strings.HasPrefix(b.Text, cut.Text) || encoded(cut) != maxBytes {
			t.Errorf("cut event = %q, cut %t of %d bytes, encoded in %d; want a prefix of the 100 bytes filling the %d-byte cap", cut.Text, cut.TextCut, cut.TextBytes, encoded(cut), maxBytes)
		}
	})

	t.Run("cuts a shared first line across its events", func(t *testing.T) {
		maxBytes := encoded(hello, call) - 20
		report := peek(maxBytes, 0, LineRange{5, 5})

		if len(report.Events) != 2 || report.OmittedEvents != 0 || report.NextLine != nil {
			t.Fatalf("report = %d events, %d omitted, next line %v; want both events of the line", len(report.Events), report.OmittedEvents, report.NextLine)
		}
		first, second := report.Events[0], report.Events[1]
		if first.TextCut || first.Text != hello.Text || !second.TextCut || second.TextBytes != len(call.Text) || !strings.HasPrefix(call.Text, second.Text) || encoded(first, second) != maxBytes {
			t.Errorf("events = %+v and %+v in %d bytes; want the first whole and the second cut to fill %d", first, second, encoded(first, second), maxBytes)
		}
	})

	t.Run("leaves room for the later events of a shared first line", func(t *testing.T) {
		maxBytes := encoded(hello, call) - 50
		report := peek(maxBytes, 0, LineRange{5, 5})

		if len(report.Events) != 2 || !report.Events[0].TextCut || !report.Events[1].TextCut {
			t.Fatalf("events = %+v, want both events of the line with their text cut", report.Events)
		}
		if got := encoded(report.Events...); got > maxBytes {
			t.Errorf("events encode in %d bytes, over the %d-byte cap that cutting their text can meet", got, maxBytes)
		}
	})

	t.Run("cuts text at a character boundary", func(t *testing.T) {
		kept, emptied := hello, call
		kept.Text, kept.TextBytes, kept.TextCut = "h", len(hello.Text), true
		emptied.Text, emptied.TextBytes, emptied.TextCut = "", len(call.Text), true
		report := peek(encoded(kept, emptied)+1, 0, LineRange{5, 5})

		if got := eventTexts(report.Events); !slices.Equal(got, []string{"h", "{"}) {
			t.Errorf("texts = %q, want the first cut before its two-byte character, leaving the spare byte to the second", got)
		}
	})

	t.Run("keeps an event whole when cutting cannot shrink it", func(t *testing.T) {
		report := peek(1, 0, LineRange{3, 3})

		if got := eventTexts(report.Events); !slices.Equal(got, []string{"cc"}) || report.Events[0].TextCut {
			t.Errorf("texts = %q, cut %t; want the short event whole", got, report.Events[0].TextCut)
		}
	})
}

func TestParseLineRange(t *testing.T) {
	for value, want := range map[string]LineRange{"12": {12, 12}, "12-20": {12, 20}, "7-7": {7, 7}} {
		got, err := ParseLineRange(value)
		if err != nil || got != want {
			t.Errorf("ParseLineRange(%q) = %v, %v, want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"", "0", "-3", "5-2", "3-", "1-2-3", "x", " 4", "4.5"} {
		if got, err := ParseLineRange(value); err == nil {
			t.Errorf("ParseLineRange(%q) = %v, want an error", value, got)
		}
	}
}
