package session

import (
	"strings"
	"testing"
	"time"
)

func TestResolveSearchResolvesFilterAndQuery(t *testing.T) {
	since := time.Date(2026, 9, 6, 21, 47, 6, 0, time.FixedZone("EDT", -4*3600))
	requestFilter := RequestFilter{
		ArchiveState: "archived",
		Harnesses:    []string{"claude", "pi"},
		IDs:          []string{`a"b,c`, "second"},
		Project:      "esheep",
		Since:        &since,
		Subagents:    true,
	}
	requestQuery := RequestQuery{Pattern: `time(out)?\b`, Role: "tool", Tool: "Bash"}

	filter, query, err := ResolveSearch(requestFilter, requestQuery)

	if err != nil {
		t.Fatalf("ResolveSearch() error = %v", err)
	}
	if filter.ArchiveState != ArchiveArchived || !filter.IncludeSubagents || filter.Project != "esheep" {
		t.Errorf("filter = %+v", filter)
	}
	if len(filter.Harnesses) != 2 || filter.Harnesses[0] != HarnessClaude || filter.Harnesses[1] != HarnessPi {
		t.Errorf("harnesses = %v", filter.Harnesses)
	}
	if len(filter.IDs) != 2 || filter.IDs[0] != `a"b,c` {
		t.Errorf("IDs = %q", filter.IDs)
	}
	if !filter.Since.Equal(since) || !filter.Until.IsZero() {
		t.Errorf("since = %v, until = %v", filter.Since, filter.Until)
	}
	if query.Pattern == nil || !query.Pattern.MatchString("TIMEOUT reached") || query.Role != RoleTool || query.Tool != "Bash" {
		t.Errorf("query = %+v", query)
	}
}

func TestRequestFilterPreservesPeriodInstants(t *testing.T) {
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	until := time.Date(2026, 11, 2, 0, 0, 0, 0, time.FixedZone("EST", -5*3600))
	requestFilter := RequestFilter{ArchiveState: "all", Since: &since, Until: &until}

	filter, err := requestFilter.Resolve()

	if err != nil || !filter.Since.Equal(since) || !filter.Until.Equal(until) {
		t.Fatalf("resolved filter = %+v, error = %v; want original instants", filter, err)
	}
}

func TestRequestResolveRejectsInvalidRequests(t *testing.T) {
	since := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	earlier := since.Add(-time.Hour)
	tests := []struct {
		name   string
		filter RequestFilter
		list   bool
		query  RequestQuery
		want   string
	}{
		{name: "equal bounds", filter: RequestFilter{ArchiveState: "all", Since: &since, Until: &since}, list: true, want: "--since 2026-09-14T00:00:00Z must be before --until 2026-09-14T00:00:00Z"},
		{name: "reversed bounds", filter: RequestFilter{ArchiveState: "all", Since: &since, Until: &earlier}, want: "--since 2026-09-14T00:00:00Z must be before --until 2026-09-13T23:00:00Z"},
		{name: "raw with since", filter: RequestFilter{ArchiveState: "all", Since: &since}, query: RequestQuery{Pattern: "x", Raw: true}, want: "--raw cannot combine with --since or --until"},
		{name: "raw with until", filter: RequestFilter{ArchiveState: "all", Until: &since}, query: RequestQuery{Pattern: "x", Raw: true}, want: "--raw cannot combine with --since or --until"},
		{name: "unknown archive state", filter: RequestFilter{ArchiveState: "old"}, list: true, want: "unknown archive state"},
		{name: "unknown harness", filter: RequestFilter{ArchiveState: "all", Harnesses: []string{"emacs"}}, list: true, want: "unknown harness"},
		{name: "empty ID", filter: RequestFilter{ArchiveState: "all", IDs: []string{"one", ""}}, list: true, want: "--id must not contain empty IDs"},
		{name: "search without criteria", filter: RequestFilter{ArchiveState: "all"}, want: "search requires a pattern, --tool, or --errors"},
		{name: "unknown role", filter: RequestFilter{ArchiveState: "all"}, query: RequestQuery{Pattern: "x", Role: "system"}, want: "unknown role"},
		{name: "bad pattern", filter: RequestFilter{ArchiveState: "all"}, query: RequestQuery{Pattern: "(unclosed"}, want: "invalid pattern"},
		{name: "raw with structural filter", filter: RequestFilter{ArchiveState: "all"}, query: RequestQuery{Pattern: "x", Raw: true, Tool: "Bash"}, want: "--raw cannot combine"},
		{name: "raw without pattern", filter: RequestFilter{ArchiveState: "all"}, query: RequestQuery{Raw: true, Errors: true}, want: "--raw cannot combine"},
		{name: "non-tool role with tool filter", filter: RequestFilter{ArchiveState: "all"}, query: RequestQuery{Pattern: "x", Role: "user", Tool: "Bash"}, want: "--role user or assistant cannot combine"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var err error
			if test.list {
				_, err = test.filter.Resolve()
			} else {
				_, _, err = ResolveSearch(test.filter, test.query)
			}

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("resolve error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRequestPeekResolve(t *testing.T) {
	maxBytes := 900
	base := RequestPeek{ID: "abc", Lines: []string{"4", "9-12"}}

	query, err := RequestPeek{Context: 2, ID: "abc", Lines: base.Lines, MaxBytes: &maxBytes, Subagents: true}.Resolve()

	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if query.Context != 2 || !query.IncludeSubagents || query.ID != "abc" || query.MaxBytes != 900 || len(query.Lines) != 2 || query.Lines[0] != (LineRange{4, 4}) || query.Lines[1] != (LineRange{9, 12}) {
		t.Errorf("query = %+v", query)
	}
	if capped, err := base.Resolve(); err != nil || capped.MaxBytes != DefaultPeekBytes {
		t.Errorf("default cap = %d, error %v; want %d", capped.MaxBytes, err, DefaultPeekBytes)
	}
	if uncapped, err := (RequestPeek{All: true, ID: "abc", Lines: base.Lines}).Resolve(); err != nil || uncapped.MaxBytes != 0 {
		t.Errorf("--all cap = %d, error %v; want none", uncapped.MaxBytes, err)
	}

	zero := 0
	for _, test := range []struct {
		name    string
		request RequestPeek
		want    string
	}{
		{name: "no ID", request: RequestPeek{Lines: []string{"4"}}, want: "peek requires --id"},
		{name: "no lines", request: RequestPeek{ID: "abc"}, want: "peek requires --line"},
		{name: "negative context", request: RequestPeek{Context: -1, ID: "abc", Lines: []string{"4"}}, want: "--context must not be negative"},
		{name: "all with a cap", request: RequestPeek{All: true, ID: "abc", Lines: []string{"4"}, MaxBytes: &maxBytes}, want: "--all cannot combine with --max-bytes"},
		{name: "zero cap", request: RequestPeek{ID: "abc", Lines: []string{"4"}, MaxBytes: &zero}, want: "--max-bytes must be at least 1"},
		{name: "descending range", request: RequestPeek{ID: "abc", Lines: []string{"9-4"}}, want: `--line: session: line "9-4"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.request.Resolve()

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Resolve() error = %v, want containing %q", err, test.want)
			}
		})
	}
}
