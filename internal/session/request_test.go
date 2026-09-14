package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRequestRoundTripsThroughJSON(t *testing.T) {
	since := time.Date(2026, 9, 6, 21, 47, 6, 0, time.FixedZone("EDT", -4*3600))
	request := Request{
		Filter: RequestFilter{
			ArchiveState: "archived",
			Harnesses:    []string{"claude", "pi"},
			IDs:          []string{`a"b,c`, "second"},
			Project:      "esheep",
			Since:        &since,
			Subagents:    true,
		},
		Mode:  ModeSearch,
		Query: RequestQuery{Pattern: `time(out)?\b`, Role: "tool", Tool: "Bash"},
	}

	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	filter, query, err := decoded.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if !strings.Contains(string(encoded), `"until":null`) || !strings.Contains(string(encoded), `"since":"2026-09-06T21:47:06-04:00"`) {
		t.Errorf("encoded = %s, want null until and RFC 3339 since", encoded)
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

func TestRequestResolveListIgnoresQuery(t *testing.T) {
	request := Request{Filter: RequestFilter{ArchiveState: "all"}, Mode: ModeList, Query: RequestQuery{Raw: true, Tool: "Bash"}}

	filter, query, err := request.Resolve()

	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if filter.ArchiveState != ArchiveAll || query.Raw || query.Tool != "" {
		t.Errorf("filter = %+v, query = %+v, want unrestricted filter and zero query", filter, query)
	}
}

func TestRequestResolveRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		want    string
	}{
		{name: "unknown mode", request: Request{Filter: RequestFilter{ArchiveState: "all"}, Mode: "count"}, want: "unknown mode"},
		{name: "unknown archive state", request: Request{Filter: RequestFilter{ArchiveState: "old"}, Mode: ModeList}, want: "unknown archive state"},
		{name: "unknown harness", request: Request{Filter: RequestFilter{ArchiveState: "all", Harnesses: []string{"emacs"}}, Mode: ModeList}, want: "unknown harness"},
		{name: "empty ID", request: Request{Filter: RequestFilter{ArchiveState: "all", IDs: []string{"one", ""}}, Mode: ModeList}, want: "--id must not contain empty IDs"},
		{name: "search without criteria", request: Request{Filter: RequestFilter{ArchiveState: "all"}, Mode: ModeSearch}, want: "search requires a pattern, --tool, or --errors"},
		{name: "unknown role", request: Request{Filter: RequestFilter{ArchiveState: "all"}, Mode: ModeSearch, Query: RequestQuery{Pattern: "x", Role: "system"}}, want: "unknown role"},
		{name: "bad pattern", request: Request{Filter: RequestFilter{ArchiveState: "all"}, Mode: ModeSearch, Query: RequestQuery{Pattern: "(unclosed"}}, want: "invalid pattern"},
		{name: "raw with structural filter", request: Request{Filter: RequestFilter{ArchiveState: "all"}, Mode: ModeSearch, Query: RequestQuery{Pattern: "x", Raw: true, Tool: "Bash"}}, want: "--raw cannot combine"},
		{name: "raw without pattern", request: Request{Filter: RequestFilter{ArchiveState: "all"}, Mode: ModeSearch, Query: RequestQuery{Raw: true, Errors: true}}, want: "--raw cannot combine"},
		{name: "non-tool role with tool filter", request: Request{Filter: RequestFilter{ArchiveState: "all"}, Mode: ModeSearch, Query: RequestQuery{Pattern: "x", Role: "user", Tool: "Bash"}}, want: "--role user or assistant cannot combine"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := test.request.Resolve()

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Resolve() error = %v, want containing %q", err, test.want)
			}
		})
	}
}
