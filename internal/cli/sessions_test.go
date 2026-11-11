package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/session"
)

func TestCoworkCommandsUseConfiguredStorageAndSharedOutput(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "cowork")
	for _, name := range []string{"local_main", "local_partial"} {
		directory := filepath.Join(root, "scope", "group", name)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "audit.jsonl"), []byte(`{"type":"user","message":{"content":"migration plan"}}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "scope", "group", "local_main", "audit.jsonl")
	if err := os.WriteFile(filepath.Dir(path)+".json", []byte(`{"sessionId":"local_main","title":"Migration","isArchived":true,"userSelectedFolders":["/work/api","/work/docs"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	load := func(options config.LoadOptions) (config.LoadResult, error) {
		options.Env = map[string]string{"HOME": base, "XDG_CONFIG_HOME": filepath.Join(base, "config")}
		return config.Load(options)
	}
	operations := commandOperations{sessionList: session.List, sessionSearch: session.Search}

	code, stdout, stderr := runCommandWithOperations(t, load, operations,
		"sessions", "list", "--harness", "claude-cowork", "--claude-cowork-sessions-path", root, "--json")

	if code != 0 || stderr != "" {
		t.Fatalf("list exit = %d, stderr = %q", code, stderr)
	}
	var inventory session.ListReport
	if err := json.Unmarshal([]byte(stdout), &inventory); err != nil {
		t.Fatal(err)
	}
	if !inventory.Complete || len(inventory.Diagnostics) != 0 || len(inventory.Sessions) != 2 {
		t.Fatalf("inventory = %+v", inventory)
	}
	for _, entry := range inventory.Sessions {
		if entry.Harness != session.HarnessClaudeCowork || entry.Projects == nil {
			t.Errorf("session = %+v, want Cowork with a projects array", entry)
		}
	}
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"sessions", "search", "migration", "--harness", "claude-cowork", "--claude-cowork-sessions-path", root, "--project", "DOCS", "--archive-state", "archived"}
		if jsonOutput {
			args = append(args, "--json")
		}

		code, stdout, stderr := runCommandWithOperations(t, load, operations, args...)

		if code != 0 || stderr != "" {
			t.Fatalf("search exit = %d, stderr = %q", code, stderr)
		}
		if !jsonOutput {
			for _, want := range []string{"claude-cowork", "/work/api", "/work/docs", "Migration", "archived", path, ":1"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("search output missing %q: %s", want, stdout)
				}
			}
			continue
		}
		var report session.SearchReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatal(err)
		}
		if !report.Complete || len(report.Diagnostics) != 0 || len(report.Sessions) != 1 {
			t.Fatalf("search report = %+v", report)
		}
		entry := report.Sessions[0]
		if !slices.Equal(entry.Projects, []string{"/work/api", "/work/docs"}) || entry.Path != path || !entry.Archived ||
			len(entry.Hits) != 1 || entry.Hits[0].Line != 1 || entry.Hits[0].Role != session.RoleUser {
			t.Errorf("search match = %+v", entry)
		}
	}
}

func TestDisabledCoworkCommandsReportConfigurationWithoutFailing(t *testing.T) {
	base := t.TempDir()
	load := func(options config.LoadOptions) (config.LoadResult, error) {
		options.Env = map[string]string{"HOME": base, "XDG_CONFIG_HOME": filepath.Join(base, "config")}
		return config.Load(options)
	}
	operations := commandOperations{sessionList: session.List, sessionSearch: session.Search}
	for _, command := range [][]string{{"list"}, {"search", "needle"}} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", command[0], jsonOutput), func(t *testing.T) {
				args := append([]string{"sessions"}, command...)
				args = append(args, "--harness", "claude-cowork", "--claude-cowork-sessions-path", "")
				if jsonOutput {
					args = append(args, "--json")
				}

				code, stdout, stderr := runCommandWithOperations(t, load, operations, args...)

				if code != 0 {
					t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
				}
				if !strings.Contains(stderr, "root-disabled") || !strings.Contains(stderr, "[sessions.claude-cowork].path") || strings.Contains(stdout, "root-disabled") {
					t.Errorf("stdout = %q, stderr = %q, want configuration diagnostic only on stderr", stdout, stderr)
				}
				if !jsonOutput {
					return
				}
				var report map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stdout), &report); err != nil {
					t.Fatal(err)
				}
				if _, exists := report["diagnostics"]; exists || string(report["complete"]) != "true" {
					t.Errorf("JSON report = %s, want a complete report without diagnostics", stdout)
				}
			})
		}
	}
}

func sessionLoader(t *testing.T) configLoader {
	t.Helper()
	return func(config.LoadOptions) (config.LoadResult, error) {
		return config.LoadResult{ResolvedSessions: config.ResolvedSessions{
			Claude: "/roots/claude",
			Codex: config.ResolvedCodexSessions{
				ArchivedSessions: "/roots/archive",
				Sessions:         "/roots/codex",
			},
			Pi: "/roots/pi",
		}}, nil
	}
}

func TestSessionsListPassesFilterAndRoots(t *testing.T) {
	var gotFilter session.Filter
	var gotRoots session.Roots
	operations := commandOperations{
		sessionList: func(_ context.Context, roots session.Roots, filter session.Filter) session.ListReport {
			gotFilter = filter
			gotRoots = roots
			return session.ListReport{Complete: true}
		},
	}

	code, _, stderr := runCommandWithOperations(t, sessionLoader(t), operations,
		"sessions", "list", "--harness", "claude,pi", "--project", "esheep", "--since", "7d", "--subagents", "--archive-state", "active",
		"--id", "first,second", "--id", "third")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if gotRoots.Claude != "/roots/claude" || gotRoots.CodexSessions != "/roots/codex" || gotRoots.Pi != "/roots/pi" || gotRoots.CodexArchivedSessions != "/roots/archive" {
		t.Errorf("roots = %+v", gotRoots)
	}
	if len(gotFilter.Harnesses) != 2 || gotFilter.Harnesses[0] != session.HarnessClaude || gotFilter.Harnesses[1] != session.HarnessPi {
		t.Errorf("harnesses = %v", gotFilter.Harnesses)
	}
	if !gotFilter.IncludeSubagents || gotFilter.Project != "esheep" || gotFilter.ArchiveState != session.ArchiveActive {
		t.Errorf("filter = %+v", gotFilter)
	}
	if !slices.Equal(gotFilter.IDs, []string{"first", "second", "third"}) {
		t.Errorf("IDs = %v, want first, second, third", gotFilter.IDs)
	}
	want := time.Now().Add(-7 * 24 * time.Hour)
	if gotFilter.Since.IsZero() || gotFilter.Since.Sub(want).Abs() > time.Minute {
		t.Errorf("since = %v, want about %v", gotFilter.Since, want)
	}
}

func TestSessionsSearchPassesQuery(t *testing.T) {
	var gotFilter session.Filter
	var gotQuery session.SearchQuery
	operations := commandOperations{
		sessionSearch: func(_ context.Context, _ session.Roots, filter session.Filter, query session.SearchQuery) session.SearchReport {
			gotFilter = filter
			gotQuery = query
			return session.SearchReport{Complete: true}
		},
	}

	code, _, stderr := runCommandWithOperations(t, sessionLoader(t), operations,
		"sessions", "search", "GOMODCACHE", "--role", "tool", "--tool", "Bash", "--errors",
		"--id", "first", "--id", "second,third")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if gotQuery.Pattern == nil || !gotQuery.Pattern.MatchString("gomodcache lives here") {
		t.Errorf("pattern = %v, want case-insensitive match", gotQuery.Pattern)
	}
	if gotQuery.Role != session.RoleTool || gotQuery.Tool != "Bash" || !gotQuery.ErrorsOnly || gotQuery.Raw {
		t.Errorf("query = %+v", gotQuery)
	}
	if !slices.Equal(gotFilter.IDs, []string{"first", "second", "third"}) {
		t.Errorf("IDs = %v, want first, second, third", gotFilter.IDs)
	}
}

func TestSessionsSearchReturnsOnePageByDefault(t *testing.T) {
	var full session.SearchReport
	for index := range 12 {
		hits := make([]session.Hit, 6)
		full.Sessions = append(full.Sessions, session.Match{
			Session:     session.Session{Harness: session.HarnessClaude, Path: fmt.Sprintf("/claude/%02d.jsonl", index), StartedAt: time.Date(2026, 8, 20, 12-index, 0, 0, 0, time.UTC)},
			Hits:        hits,
			MatchedHits: len(hits),
		})
	}
	full.Complete, full.MatchedSessions, full.MatchedHits = true, 12, 72
	operations := commandOperations{
		sessionSearch: func(context.Context, session.Roots, session.Filter, session.SearchQuery) session.SearchReport {
			return full
		},
	}

	code, stdout, stderr := runCommandWithOperations(t, sessionLoader(t), operations, "sessions", "search", "x", "--json")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	var page session.SearchReport
	if err := json.Unmarshal([]byte(stdout), &page); err != nil {
		t.Fatalf("decode page: %v\n%s", err, stdout)
	}
	if len(page.Sessions) != 10 || len(page.Sessions[0].Hits) != 5 || page.Sessions[0].OmittedHits != 1 {
		t.Errorf("page = %d sessions, first with %d hits and %d omitted; want 10 sessions of 5 hits, 1 omitted", len(page.Sessions), len(page.Sessions[0].Hits), page.Sessions[0].OmittedHits)
	}
	if page.MatchedSessions != 12 || page.RemainingSessions != 2 || !page.HasMore || page.NextCursor == nil {
		t.Errorf("page = matched %d, remaining %d, has_more %v, cursor %v; want 12, 2, true, set", page.MatchedSessions, page.RemainingSessions, page.HasMore, page.NextCursor)
	}

	code, stdout, stderr = runCommandWithOperations(t, sessionLoader(t), operations, "sessions", "search", "x", "--json", "--cursor", *page.NextCursor)

	if code != 0 {
		t.Fatalf("cursor exit code = %d, stderr = %q", code, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &page); err != nil {
		t.Fatalf("decode next page: %v\n%s", err, stdout)
	}
	if len(page.Sessions) != 2 || page.Sessions[0].Path != "/claude/10.jsonl" || page.HasMore {
		t.Errorf("next page = %d sessions starting at %q, has_more %v; want 10.jsonl and 11.jsonl, false", len(page.Sessions), page.Sessions[0].Path, page.HasMore)
	}
}

func TestSessionsUntilDateEndsAtNextLocalMidnight(t *testing.T) {
	var got session.Filter
	operations := commandOperations{
		sessionList: func(_ context.Context, _ session.Roots, filter session.Filter) session.ListReport {
			got = filter
			return session.ListReport{Complete: true}
		},
	}

	code, _, stderr := runCommandWithOperations(t, sessionLoader(t), operations,
		"sessions", "list", "--until", "2026-09-21")

	want := time.Date(2026, 9, 22, 0, 0, 0, 0, time.Local)
	if code != 0 || !got.Until.Equal(want) {
		t.Fatalf("exit = %d, until = %v, stderr = %q; want %v", code, got.Until, stderr, want)
	}
}

func TestSessionsInvalidValuesFailBeforeConfiguration(t *testing.T) {
	tests := []struct {
		args []string
		name string
	}{
		{name: "search without criteria", args: []string{"sessions", "search"}},
		{name: "ID without event criteria", args: []string{"sessions", "search", "--id", "session"}},
		{name: "empty ID", args: []string{"sessions", "list", "--id", ""}},
		{name: "empty repeated ID", args: []string{"sessions", "list", "--id", "session", "--id", ""}},
		{name: "empty comma-separated ID", args: []string{"sessions", "search", "needle", "--id", "session,,other"}},
		{name: "empty trailing ID", args: []string{"sessions", "list", "--id", "session,"}},
		{name: "multiline ID", args: []string{"sessions", "list", "--id", "first\nsecond"}},
		{name: "carriage return in ID", args: []string{"sessions", "search", "needle", "--id", "first\rsecond"}},
		{name: "raw with structural filter", args: []string{"sessions", "search", "x", "--raw", "--tool", "Bash"}},
		{name: "non-tool role with tool filter", args: []string{"sessions", "search", "x", "--role", "user", "--tool", "Bash"}},
		{name: "unknown role", args: []string{"sessions", "search", "x", "--role", "system"}},
		{name: "unknown harness", args: []string{"sessions", "list", "--harness", "emacs"}},
		{name: "raw with period", args: []string{"sessions", "search", "x", "--raw", "--since", "7d"}},
		{name: "reversed period", args: []string{"sessions", "list", "--since", "2026-09-21", "--until", "2026-09-14"}},
		{name: "bad since", args: []string{"sessions", "list", "--since", "yesterday"}},
		{name: "bad pattern", args: []string{"sessions", "search", "(unclosed"}},
		{name: "all with limit", args: []string{"sessions", "search", "x", "--all", "--limit", "3"}},
		{name: "zero hit limit", args: []string{"sessions", "search", "x", "--hit-limit", "0"}},
		{name: "malformed cursor", args: []string{"sessions", "search", "x", "--cursor", "!"}},
		{name: "peek without ID", args: []string{"sessions", "peek", "--line", "4"}},
		{name: "peek without line", args: []string{"sessions", "peek", "--id", "session"}},
		{name: "peek descending line range", args: []string{"sessions", "peek", "--id", "session", "--line", "9-4"}},
		{name: "peek all with a cap", args: []string{"sessions", "peek", "--id", "session", "--line", "4", "--all", "--max-bytes", "9"}},
		{name: "peek zero cap", args: []string{"sessions", "peek", "--id", "session", "--line", "4", "--max-bytes", "0"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			load := func(config.LoadOptions) (config.LoadResult, error) {
				calls++
				return config.LoadResult{}, errors.New("configuration was loaded")
			}

			code, stdout, stderr := runCommandWithOperations(t, load, commandOperations{}, test.args...)

			if code != 1 {
				t.Fatalf("exit code = %d, want 1 (stderr %q)", code, stderr)
			}
			if calls != 0 {
				t.Fatalf("configuration loads = %d, want 0", calls)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
		})
	}
}

func TestSessionsListIncompleteExitsNonzero(t *testing.T) {
	operations := commandOperations{
		sessionList: func(context.Context, session.Roots, session.Filter) session.ListReport {
			return session.ListReport{Complete: false}
		},
	}

	code, _, stderr := runCommandWithOperations(t, sessionLoader(t), operations, "sessions", "list")
	if code != 1 || !strings.Contains(stderr, "session inventory is incomplete") {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}

	code, stdout, stderr := runCommandWithOperations(t, sessionLoader(t), operations, "sessions", "list", "--json")
	if code != 1 {
		t.Fatalf("json exit code = %d", code)
	}
	if want := "esheep: session inventory is incomplete\n"; stderr != want {
		t.Fatalf("json stderr = %q, want %q", stderr, want)
	}
	if !strings.Contains(stdout, `"complete": false`) {
		t.Fatalf("json stdout = %q, want complete false", stdout)
	}
}

func TestSessionsSearchWritesHitsGroupedBySession(t *testing.T) {
	report := session.SearchReport{
		Complete: true,
		Sessions: []session.Match{{
			Session: session.Session{
				Harness:   session.HarnessClaude,
				ID:        "abc",
				Path:      "/roots/claude/p/abc.jsonl",
				Projects:  []string{"/Users/u/proj"},
				StartedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local),
				Title:     "Debug permissions",
			},
			Hits: []session.Hit{
				{Excerpt: "find the GOMODCACHE bug", Line: 2, Role: session.RoleUser, Timestamp: time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local)},
				{Error: true, Excerpt: "permission denied", Kind: session.KindToolResult, Line: 5, Origin: session.EventOrigin{Kind: session.EventOriginTool}, Role: session.RoleTool, Tool: "Bash"},
			},
		}},
	}
	operations := commandOperations{
		sessionSearch: func(context.Context, session.Roots, session.Filter, session.SearchQuery) session.SearchReport {
			return report
		},
	}

	code, stdout, stderr := runCommandWithOperations(t, sessionLoader(t), operations, "sessions", "search", "gomodcache")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"claude  2026-08-20 10:00  /Users/u/proj  Debug permissions",
		"/roots/claude/p/abc.jsonl",
		":2",
		"result:Bash",
		"2026-08-20 10:00:00",
		"error  permission denied",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestSessionsPeekPassesQueryAndWritesEvents(t *testing.T) {
	var gotQuery session.PeekQuery
	var gotRoots session.Roots
	nextLine := 12
	operations := commandOperations{
		sessionPeek: func(_ context.Context, roots session.Roots, query session.PeekQuery) (session.PeekReport, error) {
			gotQuery = query
			gotRoots = roots
			return session.PeekReport{
				Complete:      true,
				Diagnostics:   []session.Diagnostic{{Code: "malformed-lines", Message: "skipped 1 unparseable lines", Path: "/roots/claude/p/abc.jsonl"}},
				Events:        []session.Event{{Kind: session.KindToolResult, Line: 9, Position: 4, Role: session.RoleTool, Text: "permission denied", Tool: "Bash"}},
				NextLine:      &nextLine,
				OmittedEvents: 2,
				Session:       session.Session{Harness: session.HarnessClaude, ID: "abc", Path: "/roots/claude/p/abc.jsonl"},
				TotalEvents:   7,
			}, nil
		},
	}

	code, stdout, stderr := runCommandWithOperations(t, sessionLoader(t), operations,
		"sessions", "peek", "--id", "abc", "--line", "4,9-12", "--line", "20", "--context", "2", "--max-bytes", "900", "--subagents")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	wantLines := []session.LineRange{{First: 4, Last: 4}, {First: 9, Last: 12}, {First: 20, Last: 20}}
	if gotQuery.ID != "abc" || gotQuery.Context != 2 || gotQuery.MaxBytes != 900 || !gotQuery.IncludeSubagents || !slices.Equal(gotQuery.Lines, wantLines) {
		t.Errorf("query = %+v", gotQuery)
	}
	if gotRoots.Claude != "/roots/claude" || gotRoots.Pi != "/roots/pi" {
		t.Errorf("roots = %+v", gotRoots)
	}
	for _, want := range []string{":9  result:Bash", "    permission denied", "2 selected events left out by the size cap", "resume at --line 12"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if want := "/roots/claude/p/abc.jsonl: malformed-lines: skipped 1 unparseable lines\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}

	code, stdout, _ = runCommandWithOperations(t, sessionLoader(t), operations, "sessions", "peek", "--id", "abc", "--line", "9", "--json")

	var document session.PeekReport
	if err := json.Unmarshal([]byte(stdout), &document); code != 0 || err != nil {
		t.Fatalf("json exit code = %d, decode error = %v\n%s", code, err, stdout)
	}
	if gotQuery.MaxBytes != session.DefaultPeekBytes {
		t.Errorf("default cap = %d, want %d", gotQuery.MaxBytes, session.DefaultPeekBytes)
	}
	if document.TotalEvents != 7 || document.OmittedEvents != 2 || document.NextLine == nil || *document.NextLine != 12 || len(document.Events) != 1 || document.Events[0].Text != "permission denied" {
		t.Errorf("document = %+v", document)
	}
}

func TestSessionsPeekIncompleteKeepsEventsAndExitsNonzero(t *testing.T) {
	operations := commandOperations{
		sessionPeek: func(context.Context, session.Roots, session.PeekQuery) (session.PeekReport, error) {
			return session.PeekReport{Events: []session.Event{{Line: 1, Position: 1, Role: session.RoleUser, Text: "readable"}}, TotalEvents: 1}, nil
		},
	}

	code, stdout, stderr := runCommandWithOperations(t, sessionLoader(t), operations, "sessions", "peek", "--id", "abc", "--line", "1", "--json")

	if code != 1 || !strings.Contains(stdout, `"complete": false`) || !strings.Contains(stdout, `"text": "readable"`) {
		t.Errorf("exit = %d, stdout = %q; want exit 1 with the incomplete report's events", code, stdout)
	}
	if want := "esheep: session peek is incomplete\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestSessionsPeekFailureKeepsDiagnosticsAndPrintsNoEvents(t *testing.T) {
	operations := commandOperations{
		sessionPeek: func(context.Context, session.Roots, session.PeekQuery) (session.PeekReport, error) {
			return session.PeekReport{
				Diagnostics: []session.Diagnostic{{Code: "walk", Message: "permission denied", Path: "/roots/claude"}},
				Events:      []session.Event{{Line: 1, Text: "partial"}},
			}, errors.New("no session has that ID")
		},
	}

	for _, mode := range [][]string{nil, {"--json"}} {
		code, stdout, stderr := runCommandWithOperations(t, sessionLoader(t), operations, append([]string{"sessions", "peek", "--id", "abc", "--line", "1"}, mode...)...)

		if code != 1 || stdout != "" {
			t.Errorf("peek %v: exit = %d, stdout = %q, want exit 1 and no payload", mode, code, stdout)
		}
		if want := "/roots/claude: walk: permission denied\nesheep: no session has that ID\n"; stderr != want {
			t.Errorf("peek %v: stderr = %q, want %q", mode, stderr, want)
		}
	}
}

func TestWorkCommandsUseSharedHomeOverridesAndLabelOutput(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "codex-home")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "archived_sessions", "work.jsonl")
	content := `{"type":"session_meta","payload":{"id":"work","originator":"codex_work_desktop","history_mode":"paginated"}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"migration plan"}]}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"toml", "environment", "flag"} {
		t.Run(source, func(t *testing.T) {
			env := map[string]string{"HOME": base, "XDG_CONFIG_HOME": filepath.Join(base, "config")}
			configuredRoot := filepath.Join(base, "missing")
			var flags []string
			switch source {
			case "toml":
				configuredRoot = root
			case "environment":
				env["ESHEEP_CODEX_HOME"] = root
			case "flag":
				env["ESHEEP_CODEX_HOME"] = filepath.Join(base, "missing-env")
				flags = []string{"--codex-home", root}
			}
			configPath := filepath.Join(base, source+".toml")
			if err := os.WriteFile(configPath, fmt.Appendf(nil, "[sessions.codex]\nhome = %q\n", configuredRoot), 0o600); err != nil {
				t.Fatal(err)
			}
			load := func(options config.LoadOptions) (config.LoadResult, error) {
				options.Env = env
				return config.Load(options)
			}
			for _, test := range []struct {
				args []string
				json bool
				name string
			}{
				{name: "list", args: []string{"sessions", "list"}},
				{name: "list JSON", args: []string{"sessions", "list", "--json"}, json: true},
				{name: "search", args: []string{"sessions", "search", "migration"}},
				{name: "search JSON", args: []string{"sessions", "search", "migration", "--json"}, json: true},
			} {
				t.Run(test.name, func(t *testing.T) {
					args := append([]string{"--config", configPath, "--harness", "chatgpt-work", "--archive-state", "archived"}, flags...)
					args = append(args, test.args...)

					code, stdout, stderr := runCommandWithOperations(t, load, commandOperations{sessionList: session.List, sessionSearch: session.Search}, args...)

					if code != 0 || stderr != "" {
						t.Fatalf("exit code = %d, stderr = %q", code, stderr)
					}
					if !test.json {
						if !strings.Contains(stdout, "chatgpt-work") || !strings.Contains(stdout, path) || !strings.Contains(stdout, "archived") {
							t.Errorf("stdout = %q, want Work label and canonical path", stdout)
						}
						return
					}
					var report session.SearchReport
					if err := json.Unmarshal([]byte(stdout), &report); err != nil {
						t.Fatal(err)
					}
					if !report.Complete || len(report.Diagnostics) != 0 || len(report.Sessions) != 1 {
						t.Fatalf("JSON report = %+v", report)
					}
					entry := report.Sessions[0]
					if entry.Harness != session.HarnessChatGPTWork || entry.Path != path || entry.ID != "work" || !entry.Archived {
						t.Errorf("JSON session = %+v", entry)
					}
					if strings.HasPrefix(test.name, "search") && (len(entry.Hits) != 1 || entry.Hits[0].Line != 2 || entry.Hits[0].Role != session.RoleAssistant) {
						t.Errorf("JSON hits = %+v, want assistant text on line 2", entry.Hits)
					}
				})
			}
		})
	}
}
