package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/session"
)

func sessionLoader(t *testing.T) configLoader {
	t.Helper()
	return func(config.LoadOptions) (config.LoadResult, error) {
		return config.LoadResult{ResolvedSessions: config.ResolvedSessions{
			Claude: "/roots/claude",
			Codex:  "/roots/codex",
			Pi:     "/roots/pi",
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
		"sessions", "list", "--harness", "claude,pi", "--project", "esheep", "--since", "7d", "--subagents")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if gotRoots.Claude != "/roots/claude" || gotRoots.Codex != "/roots/codex" || gotRoots.Pi != "/roots/pi" {
		t.Errorf("roots = %+v", gotRoots)
	}
	if len(gotFilter.Harnesses) != 2 || gotFilter.Harnesses[0] != session.HarnessClaude || gotFilter.Harnesses[1] != session.HarnessPi {
		t.Errorf("harnesses = %v", gotFilter.Harnesses)
	}
	if !gotFilter.IncludeSubagents || gotFilter.Project != "esheep" {
		t.Errorf("filter = %+v", gotFilter)
	}
	want := time.Now().AddDate(0, 0, -7)
	if gotFilter.Since.IsZero() || gotFilter.Since.Sub(want).Abs() > time.Minute {
		t.Errorf("since = %v, want about %v", gotFilter.Since, want)
	}
}

func TestSessionsSearchPassesQuery(t *testing.T) {
	var gotQuery session.SearchQuery
	operations := commandOperations{
		sessionSearch: func(_ context.Context, _ session.Roots, _ session.Filter, query session.SearchQuery) session.SearchReport {
			gotQuery = query
			return session.SearchReport{Complete: true}
		},
	}

	code, _, stderr := runCommandWithOperations(t, sessionLoader(t), operations,
		"sessions", "search", "GOMODCACHE", "--role", "user", "--tool", "Bash", "--errors")

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if gotQuery.Pattern == nil || !gotQuery.Pattern.MatchString("gomodcache lives here") {
		t.Errorf("pattern = %v, want case-insensitive match", gotQuery.Pattern)
	}
	if gotQuery.Role != session.RoleUser || gotQuery.Tool != "Bash" || !gotQuery.ErrorsOnly || gotQuery.Raw {
		t.Errorf("query = %+v", gotQuery)
	}
}

func TestSessionsUsageErrorsDoNotLoadConfiguration(t *testing.T) {
	tests := []struct {
		args []string
		name string
	}{
		{name: "search without criteria", args: []string{"sessions", "search"}},
		{name: "raw with structural filter", args: []string{"sessions", "search", "x", "--raw", "--tool", "Bash"}},
		{name: "unknown role", args: []string{"sessions", "search", "x", "--role", "system"}},
		{name: "unknown harness", args: []string{"sessions", "list", "--harness", "emacs"}},
		{name: "bad since", args: []string{"sessions", "list", "--since", "yesterday"}},
		{name: "bad pattern", args: []string{"sessions", "search", "(unclosed"}},
		{name: "extra operand", args: []string{"sessions", "list", "extra"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			load := func(config.LoadOptions) (config.LoadResult, error) {
				calls++
				return config.LoadResult{}, errors.New("configuration was loaded")
			}

			code, stdout, stderr := runCommandWithOperations(t, load, commandOperations{}, test.args...)

			if code != 2 {
				t.Fatalf("exit code = %d, want 2 (stderr %q)", code, stderr)
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
	if stderr != "" {
		t.Fatalf("json stderr = %q, want empty", stderr)
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
				Project:   "/Users/u/proj",
				StartedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local),
				Title:     "Debug permissions",
			},
			Hits: []session.Hit{
				{Excerpt: "find the GOMODCACHE bug", Line: 2, Role: session.RoleUser, Timestamp: time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local)},
				{Error: true, Excerpt: "permission denied", Line: 5, Role: session.RoleTool, Tool: "Bash"},
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
		"tool:Bash",
		"error  permission denied",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}
