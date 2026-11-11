package ui

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmcampanini/esheep/internal/session"
)

func TestHumanOutputRemovesTerminalControlsFromTranscriptData(t *testing.T) {
	t.Parallel()
	report := session.ListReport{
		Complete: true,
		Sessions: []session.Session{{
			Harness: session.HarnessClaude,
			ID:      "demo\rsession",
			Path:    "/roots/claude/p/demo.jsonl",
			Title:   "safe\x1b[31m\nnext\u202espoof",
		}},
	}
	var output bytes.Buffer

	if err := WriteSessionList(&output, report, false); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(output.String(), '\x1b') || strings.ContainsRune(output.String(), '\r') || strings.ContainsRune(output.String(), '\u202e') {
		t.Fatalf("output contains terminal controls: %q", output.String())
	}
	if !strings.Contains(output.String(), "TITLE") || !strings.Contains(output.String(), "safe [31m next") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestSearchDisplaysKindsOriginsAndRelationships(t *testing.T) {
	report := session.SearchReport{Sessions: []session.Match{{
		Session: session.Session{Harness: session.HarnessCodex, Path: "/events.jsonl", Origin: session.Origin{Kind: session.OriginAutomation, Evidence: map[string]string{"thread_source": "automation\n\x1b[31m", "source": "exec"}}},
		Hits: []session.Hit{
			{Kind: session.KindToolCall, Role: session.RoleTool, Tool: "shell", CallID: "id\r\u202e", Origin: session.EventOrigin{Kind: session.EventOriginAgent}, Excerpt: "command"},
			{Kind: session.KindToolResult, Role: session.RoleTool, ParentCallID: "parent\x1b", Origin: session.EventOrigin{Kind: session.EventOriginTool}, Excerpt: "output"},
			{Kind: session.KindMessage, Role: session.RoleUser, Origin: session.EventOrigin{Kind: session.EventOriginHarness, Evidence: map[string]string{"origin.kind\n": "task-notification\t"}}, Excerpt: "notice"},
		},
	}}}
	var output bytes.Buffer

	if err := WriteSessionSearch(&output, report); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"origin=automation source=exec thread_source=automation", "call:shell", "result:?", "origin=agent call_id=id", "origin=tool parent_call_id=parent", "origin=harness origin.kind=task-notification"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
	for _, control := range []rune{'\r', '\x1b', '\u202e', '\t'} {
		if strings.ContainsRune(output.String(), control) {
			t.Errorf("output contains control %U: %q", control, output.String())
		}
	}
}

func TestPeekTextKeepsEventLinesAndReportsWhatIsNotShown(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 20, 10, 0, 5, 0, time.Local)
	nextLine := 31
	report := session.PeekReport{
		Session:       session.Session{Harness: session.HarnessClaude, Path: "/roots/claude/p/abc.jsonl", Projects: []string{"/proj"}, StartedAt: at, Origin: session.Origin{Kind: session.OriginUnknown}},
		TotalEvents:   9,
		OmittedEvents: 1,
		NextLine:      &nextLine,
		Events: []session.Event{
			{Kind: session.KindToolCall, Line: 12, Position: 3, Role: session.RoleTool, Tool: "Bash", CallID: "toolu_1", Origin: session.EventOrigin{Kind: session.EventOriginAgent}, Timestamp: at, Text: `{"command":"make"}`},
			{Kind: session.KindToolResult, Line: 13, Position: 4, Role: session.RoleTool, Tool: "Bash", CallID: "toolu_1", Origin: session.EventOrigin{Kind: session.EventOriginTool}, Error: true, Text: "func main() {\n\treturn\x1b[31m\r\n}\n"},
			{Kind: session.KindToolResult, Line: 20, Position: 7, Role: session.RoleTool, Origin: session.EventOrigin{Kind: session.EventOriginTool}, OmittedContent: []string{"image", "tool_reference"}},
			{Kind: session.KindMessage, Line: 21, Position: 8, Role: session.RoleUser, Origin: session.EventOrigin{Kind: session.EventOriginUnknown}, Text: "long", TextCut: true, TextBytes: 4000},
		},
	}
	var output bytes.Buffer

	if err := WriteSessionPeek(&output, report); err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{
		"claude  2026-08-20 10:00  /proj  active  origin=unknown",
		"/roots/claude/p/abc.jsonl",
		"Showing 4 of 9 events.",
		"",
		"  (2 events not shown)",
		":12  call:Bash  2026-08-20 10:00:05  origin=agent call_id=toolu_1",
		`    {"command":"make"}`,
		":13  result:Bash  -  origin=tool call_id=toolu_1  error",
		"    func main() {",
		"    \treturn [31m",
		"    }",
		"",
		"  (2 events not shown)",
		":20  result:?  -  origin=tool omitted_content=image,tool_reference",
		":21  user  -  origin=unknown",
		"    long",
		"    (text cut: 4 of 4000 bytes shown)",
		"  (1 event not shown)",
		"",
		"1 selected event left out by the size cap; raise --max-bytes, resume at --line 31, or pass --all.",
		"",
	}, "\n")
	if output.String() != want {
		t.Errorf("WriteSessionPeek() =\n%s\nwant\n%s", output.String(), want)
	}
}

func TestSearchTextSummarizesThePageFirst(t *testing.T) {
	t.Parallel()
	cursor := "CURSOR"
	report := session.SearchReport{
		HasMore:           true,
		MatchedHits:       9,
		MatchedSessions:   4,
		NextCursor:        &cursor,
		RemainingSessions: 3,
		Sessions: []session.Match{{
			Session:     session.Session{Harness: session.HarnessClaude, Path: "/claude/a.jsonl"},
			Hits:        []session.Hit{{Excerpt: "first", Line: 1}},
			MatchedHits: 4,
			OmittedHits: 3,
		}},
		Sources: []session.Source{
			{Harness: session.HarnessClaude, MatchedSessions: 3, Path: "/claude", Status: session.SourceSearched},
			{Harness: session.HarnessClaudeCowork, Status: session.SourceDisabled},
			{Harness: session.HarnessCodex, Path: "/codex/archived", Status: session.SourceMissing},
			{Harness: session.HarnessCodex, MatchedSessions: 1, Path: "/codex/sessions", Status: session.SourceSearched},
		},
	}
	var output bytes.Buffer

	if err := WriteSessionSearch(&output, report); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(output.String(), "\n")
	wantFirst := []string{
		"Matched 4 sessions and 9 hits; showing 1 session and 1 hit.",
		"Sources: claude 3, codex 1, claude-cowork disabled, codex missing /codex/archived.",
		"3 more sessions; next page: --cursor CURSOR",
		"",
	}
	if len(lines) < len(wantFirst) || !reflect.DeepEqual(lines[:len(wantFirst)], wantFirst) {
		t.Fatalf("output begins %q, want %q", lines, wantFirst)
	}
	if !strings.Contains(output.String(), "  3 more hits in this session\n") {
		t.Errorf("output lacks the omitted-hit line:\n%s", output.String())
	}
}

func TestWriteSessionListRendersPlaceholdersAndTimes(t *testing.T) {
	t.Parallel()
	report := session.ListReport{
		Complete: true,
		Sessions: []session.Session{
			{
				Harness:   session.HarnessClaude,
				ID:        "abc",
				Path:      "/roots/claude/p/abc.jsonl",
				Projects:  []string{"/Users/u/proj"},
				StartedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local),
				Title:     "Debug permissions",
			},
			{
				Harness:    session.HarnessCodex,
				ID:         "header-def",
				ModifiedAt: time.Date(2026, 8, 25, 9, 30, 0, 0, time.Local),
				Path:       "/roots/codex/rollout-def.jsonl",
			},
		},
	}
	var output bytes.Buffer

	if err := WriteSessionList(&output, report, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HARNESS", "2026-08-20 10:00", "Debug permissions", "2026-08-25 09:30", "/roots/codex/rollout-def.jsonl"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
	codexRow := ""
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.Contains(line, "codex") {
			codexRow = line
		}
	}
	if !strings.Contains(codexRow, "-") {
		t.Errorf("codex row = %q, want dash placeholders for absent metadata", codexRow)
	}
	if fields := strings.Fields(codexRow); len(fields) < 2 || fields[1] != "header-def" {
		t.Errorf("codex row = %q, want the session ID from its header", codexRow)
	}
}

func TestJSONWritersEmitPayloadDocuments(t *testing.T) {
	t.Parallel()
	sessionDiagnostics := []session.Diagnostic{{Code: "walk", Message: "permission denied"}}
	for _, test := range []struct {
		name  string
		want  string
		write func(*bytes.Buffer) error
	}{
		{name: "sessions list", want: `{"complete":false,"period":{"since":null,"until":null},"sessions":[]}`, write: func(output *bytes.Buffer) error {
			return WriteSessionListJSON(output, session.ListReport{Diagnostics: sessionDiagnostics})
		}},
		{name: "sessions search", want: `{"complete":false,"has_more":false,"matched_hits":0,"matched_sessions":0,"next_cursor":null,"period":{"since":null,"until":null},"remaining_sessions":0,"sessions":[],"sources":[]}`, write: func(output *bytes.Buffer) error {
			return WriteSessionSearchJSON(output, session.SearchReport{Diagnostics: sessionDiagnostics})
		}},
		{name: "sessions peek", want: `{"complete":false,"events":[],"next_line":null,"omitted_events":0,"session":{"archived":false,"harness":"pi","id":"abc","modified_at":"0001-01-01T00:00:00Z","origin":{"kind":""},"path":"/t.jsonl","projects":null,"subagent":false},"total_events":0}`, write: func(output *bytes.Buffer) error {
			return WriteSessionPeekJSON(output, session.PeekReport{Diagnostics: sessionDiagnostics, Session: session.Session{Harness: session.HarnessPi, ID: "abc", Path: "/t.jsonl"}})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer

			if err := test.write(&output); err != nil {
				t.Fatal(err)
			}

			var got, want any
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(test.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("JSON = %s, want %s", output.String(), test.want)
			}
		})
	}
}

func TestWriteSessionDiagnosticsNamesHarness(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer

	err := WriteSessionDiagnostics(&output, []session.Diagnostic{{
		Code:    "root-missing",
		Harness: session.HarnessPi,
		Message: "session root does not exist; harness skipped",
		Path:    "/roots/pi",
	}})

	if err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "/roots/pi [pi]: root-missing: session root does not exist; harness skipped\n" {
		t.Fatalf("output = %q", got)
	}
}
