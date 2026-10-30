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
		{name: "sessions search", want: `{"complete":false,"period":{"since":null,"until":null},"sessions":[]}`, write: func(output *bytes.Buffer) error {
			return WriteSessionSearchJSON(output, session.SearchReport{Diagnostics: sessionDiagnostics})
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
