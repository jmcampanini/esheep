package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/session"
)

func fleetLoader(t *testing.T) configLoader {
	t.Helper()
	return func(options config.LoadOptions) (config.LoadResult, error) {
		loaded, err := sessionLoader(t)(options)
		loaded.ResolvedMachines = []config.ResolvedMachine{
			{Command: "esheep", Host: "nas", Name: "nas", Timeout: time.Minute},
			{Command: "esheep", Host: "laptop.example", Name: "laptop", Timeout: time.Minute},
		}
		return loaded, err
	}
}

// fakeSSHOnPath places an ssh script first on PATH that answers every host
// with one canned list or search document and exits 0.
func fakeSSHOnPath(t *testing.T, document string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + document + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSessionsRemoteSelectionUsageErrorsExitTwo(t *testing.T) {
	tests := []struct {
		args []string
		name string
		want string
	}{
		{name: "unknown machine", args: []string{"sessions", "list", "--remote", "laptop9"}, want: `unknown machine "laptop9" (configured: nas, laptop)`},
		{name: "own hostname", args: []string{"sessions", "search", "x", "--remote", "laptop"}, want: `machine "laptop" is this machine`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operations := commandOperations{hostname: func() (string, error) { return "laptop.local", nil }}

			code, stdout, stderr := runCommandWithOperations(t, fleetLoader(t), operations, test.args...)

			if code != 2 || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q, want usage error containing %q", code, stdout, stderr, test.want)
			}
		})
	}
}

func TestSessionsListMergesRemoteMachinesIntoTextAndJSON(t *testing.T) {
	fakeSSHOnPath(t, `{"complete":true,"diagnostics":[],"sessions":[{"harness":"pi","id":"remote-1","path":"/home/j/.pi/one.jsonl","projects":[],"started_at":"2026-09-11T22:40:00Z"}]}`)
	var gotFilter session.Filter
	operations := commandOperations{
		hostname: func() (string, error) { return "laptop.local", nil },
		sessionList: func(_ context.Context, _ session.Roots, filter session.Filter) session.ListReport {
			gotFilter = filter
			return session.ListReport{Complete: true, Sessions: []session.Session{
				{Harness: session.HarnessClaude, ID: "local-1", Path: "/Users/j/.claude/one.jsonl", StartedAt: time.Date(2026, 9, 12, 9, 14, 0, 0, time.UTC)},
			}}
		},
	}

	code, stdout, stderr := runCommandWithOperations(t, fleetLoader(t), operations, "sessions", "list", "--remote", "nas", "--project", "esheep")

	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if gotFilter.Project != "esheep" {
		t.Errorf("local filter = %+v, want the project forwarded", gotFilter)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "MACHINE") ||
		!strings.HasPrefix(strings.TrimSpace(lines[1]), "laptop") || !strings.HasPrefix(strings.TrimSpace(lines[2]), "nas") {
		t.Errorf("stdout = %q, want a MACHINE column with laptop before nas", stdout)
	}

	code, stdout, stderr = runCommandWithOperations(t, fleetLoader(t), operations, "sessions", "list", "--remote", "nas", "--no-local", "--json")

	if code != 0 || stderr != "" {
		t.Fatalf("json exit = %d, stderr = %q", code, stderr)
	}
	var report session.ListReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Complete || len(report.Sessions) != 1 || report.Sessions[0].Machine != "nas" || report.Sessions[0].ID != "remote-1" {
		t.Errorf("report = %+v, want only the remote session stamped nas", report)
	}
}

func TestSessionsSearchReportsUnreachableMachineAndKeepsLocalHits(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte("#!/bin/sh\necho 'ssh: Could not resolve hostname nas' >&2\nexit 255\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	operations := commandOperations{
		hostname: func() (string, error) { return "laptop", nil },
		sessionSearch: func(context.Context, session.Roots, session.Filter, session.SearchQuery) session.SearchReport {
			return session.SearchReport{Complete: true, Sessions: []session.Match{{
				Session: session.Session{Harness: session.HarnessClaude, ID: "local-1", Path: "/Users/j/.claude/one.jsonl"},
				Hits:    []session.Hit{{Excerpt: "timeout", Line: 4, Role: session.RoleUser}},
			}}}
		},
	}

	code, stdout, stderr := runCommandWithOperations(t, fleetLoader(t), operations, "sessions", "search", "timeout", "--remote", "nas")

	if code != 1 {
		t.Fatalf("exit = %d, want 1 for an incomplete search (stderr %q)", code, stderr)
	}
	if !strings.HasPrefix(stdout, "laptop  claude") || !strings.Contains(stdout, ":4") {
		t.Errorf("stdout = %q, want the local hit under a machine-led header", stdout)
	}
	if !strings.Contains(stderr, "nas: machine-unreachable: ssh: Could not resolve hostname nas") || !strings.Contains(stderr, "session search is incomplete") {
		t.Errorf("stderr = %q, want the machine diagnostic and the incomplete error", stderr)
	}
}

func TestSessionsRemoteAllWithoutMachinesScansLocally(t *testing.T) {
	operations := commandOperations{
		hostname: func() (string, error) { return "laptop", nil },
		sessionList: func(context.Context, session.Roots, session.Filter) session.ListReport {
			return session.ListReport{Complete: true}
		},
	}

	code, stdout, stderr := runCommandWithOperations(t, sessionLoader(t), operations, "sessions", "list", "--remote", "all", "--json")

	if want := "laptop: no-machines: --remote all selected no other machine; add [[machines]] entries to config.toml\n"; code != 0 || stderr != want {
		t.Fatalf("exit = %d, stderr = %q, want 0 and %q", code, stderr, want)
	}
	var report session.ListReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Complete || strings.Contains(stdout, "no-machines") {
		t.Errorf("stdout = %q, want a complete report without the diagnostic", stdout)
	}
}

func TestSessionsListJSONPrintsRemoteDiagnosticsOnStderr(t *testing.T) {
	fakeSSHOnPath(t, `{"complete":false,"diagnostics":[{"code":"walk","harness":"pi","message":"permission denied","path":"/home/j/.pi"}],"sessions":[]}`)
	operations := commandOperations{hostname: func() (string, error) { return "laptop.local", nil }}

	code, stdout, stderr := runCommandWithOperations(t, fleetLoader(t), operations, "sessions", "list", "--remote", "nas", "--no-local", "--json")

	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr %q)", code, stderr)
	}
	if want := "nas: /home/j/.pi [pi]: walk: permission denied\nesheep: session inventory is incomplete\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if strings.Contains(stdout, "diagnostics") || !strings.Contains(stdout, `"complete": false`) {
		t.Errorf("stdout = %q, want an incomplete document without diagnostics", stdout)
	}
}

func TestSessionsQueryAnswersWithTheFlagDrivenDocument(t *testing.T) {
	var gotFilter session.Filter
	var gotQuery session.SearchQuery
	operations := commandOperations{
		hostname: func() (string, error) { return "nas.home", nil },
		sessionSearch: func(_ context.Context, _ session.Roots, filter session.Filter, query session.SearchQuery) session.SearchReport {
			gotFilter = filter
			gotQuery = query
			return session.SearchReport{Complete: true, Sessions: []session.Match{{
				Session: session.Session{Harness: session.HarnessPi, ID: "one", Path: "/home/j/.pi/one.jsonl"},
				Hits:    []session.Hit{{Excerpt: "timeout", Line: 2, Role: session.RoleUser}},
			}}}
		},
	}
	request := `{"mode":"search","filter":{"archive_state":"archived","harnesses":["pi"],"ids":["one"],"project":"","since":"2026-09-06T21:47:06-04:00","until":null,"subagents":true},"query":{"pattern":"timeout","role":"user","tool":"","errors":false,"raw":false}}`

	code, stdout, stderr := runCommandWithInput(t, fleetLoader(t), operations, request, "sessions", "query")

	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if gotFilter.ArchiveState != session.ArchiveArchived || !gotFilter.IncludeSubagents || len(gotFilter.IDs) != 1 || gotFilter.Since.IsZero() {
		t.Errorf("filter = %+v", gotFilter)
	}
	if gotQuery.Pattern == nil || !gotQuery.Pattern.MatchString("TIMEOUT") || gotQuery.Role != session.RoleUser {
		t.Errorf("query = %+v", gotQuery)
	}
	var report session.SearchReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Sessions) != 1 || report.Sessions[0].Machine != "nas" || len(report.Sessions[0].Hits) != 1 {
		t.Errorf("report = %+v, want the hit stamped with this machine's configured name", report)
	}
}

func TestSessionsQueryIncompleteReplyCarriesDiagnosticsAndAFailureLine(t *testing.T) {
	diagnostics := []session.Diagnostic{{Code: "walk", Path: "/roots/pi"}}
	operations := commandOperations{
		hostname: func() (string, error) { return "box", nil },
		sessionList: func(context.Context, session.Roots, session.Filter) session.ListReport {
			return session.ListReport{Diagnostics: diagnostics}
		},
		sessionSearch: func(context.Context, session.Roots, session.Filter, session.SearchQuery) session.SearchReport {
			return session.SearchReport{Diagnostics: diagnostics}
		},
	}
	for _, test := range []struct {
		mode    string
		failure string
	}{
		{mode: "list", failure: "session inventory is incomplete"},
		{mode: "search", failure: "session search is incomplete"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			request := `{"mode":"` + test.mode + `","filter":{"archive_state":"all"},"query":{"pattern":"needle"}}`
			code, stdout, stderr := runCommandWithInput(t, sessionLoader(t), operations, request, "sessions", "query")

			if want := "esheep: " + test.failure + "\n"; code != 1 || stderr != want {
				t.Fatalf("exit = %d, stderr = %q, want 1 and %q", code, stderr, want)
			}
			var reply struct {
				Complete    bool                 `json:"complete"`
				Diagnostics []session.Diagnostic `json:"diagnostics"`
			}
			if err := json.Unmarshal([]byte(stdout), &reply); err != nil {
				t.Fatal(err)
			}
			if reply.Complete || len(reply.Diagnostics) != 1 || reply.Diagnostics[0].Code != "walk" || reply.Diagnostics[0].Machine != "box" {
				t.Errorf("stdout = %q, want the incomplete reply carrying the walk diagnostic stamped box", stdout)
			}
		})
	}
}

func TestSessionsQueryRejectsInvalidRequestsBeforeConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		request string
		want    string
	}{
		{name: "not JSON", request: "hello", want: "decode request"},
		{name: "unknown field", request: `{"mode":"list","unexpected":1}`, want: `unknown field "unexpected"`},
		{name: "unknown nested field", request: `{"mode":"list","filter":{"unexpected":1}}`, want: `unknown field "unexpected"`},
		{name: "second document", request: `{"mode":"list"} {}`, want: "exactly one JSON document"},
		{name: "unknown mode", request: `{"mode":"count","filter":{"archive_state":"all"}}`, want: "unknown mode"},
		{name: "search without criteria", request: `{"mode":"search","filter":{"archive_state":"all"}}`, want: "search requires a pattern, --tool, or --errors"},
		{name: "empty ID", request: `{"mode":"list","filter":{"archive_state":"all","ids":[""]}}`, want: "--id must not contain empty IDs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			load := func(config.LoadOptions) (config.LoadResult, error) {
				return config.LoadResult{}, errors.New("configuration was loaded")
			}

			code, stdout, stderr := runCommandWithInput(t, load, commandOperations{}, test.request, "sessions", "query")

			if code != 2 || stdout != "" || !strings.Contains(stderr, test.want) {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q, want usage error containing %q", code, stdout, stderr, test.want)
			}
		})
	}
}
