package session

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// Mode selects the operation a Request performs.
type Mode string

// Request modes.
const (
	ModeList   Mode = "list"
	ModeSearch Mode = "search"
)

// Request is the unvalidated form of one list or search invocation. It
// mirrors the command flags one-to-one and carries times as resolved
// instants.
type Request struct {
	Filter RequestFilter
	Mode   Mode
	Query  RequestQuery
}

// RequestFilter is the unvalidated form of Filter. A nil Since or Until
// leaves that bound open.
type RequestFilter struct {
	ArchiveState string
	Harnesses    []string
	IDs          []string
	Project      string
	Since        *time.Time
	Subagents    bool
	Until        *time.Time
}

// RequestQuery is the unvalidated form of SearchQuery. Pattern is the user's
// regular expression source before case folding is applied; it is ignored
// for list requests.
type RequestQuery struct {
	Errors  bool
	Pattern string
	Raw     bool
	Role    string
	Tool    string
}

// Resolve validates the request and converts it into the filter and query
// List and Search accept. Every rule the command flags enforce applies here
// with the flag's own message.
func (r Request) Resolve() (Filter, SearchQuery, error) {
	filter, err := r.Filter.resolve()
	if err != nil {
		return Filter{}, SearchQuery{}, err
	}

	switch r.Mode {
	case ModeList:
		return filter, SearchQuery{}, nil
	case ModeSearch:
		query, err := r.Query.resolve()
		if err != nil {
			return Filter{}, SearchQuery{}, err
		}
		if query.Raw && filter.hasPeriod() {
			return Filter{}, SearchQuery{}, errors.New("--raw cannot combine with --since or --until")
		}
		return filter, query, nil
	default:
		return Filter{}, SearchQuery{}, fmt.Errorf("session: unknown mode %q (expected list or search)", r.Mode)
	}
}

func (f RequestFilter) resolve() (Filter, error) {
	archiveState, err := ParseArchiveState(f.ArchiveState)
	if err != nil {
		return Filter{}, err
	}
	if slices.Contains(f.IDs, "") {
		return Filter{}, errors.New("--id must not contain empty IDs")
	}

	filter := Filter{ArchiveState: archiveState, IDs: slices.Clone(f.IDs), IncludeSubagents: f.Subagents, Project: f.Project}
	for _, name := range f.Harnesses {
		harness, err := ParseHarness(name)
		if err != nil {
			return Filter{}, err
		}
		filter.Harnesses = append(filter.Harnesses, harness)
	}
	if f.Since != nil {
		filter.Since = *f.Since
	}
	if f.Until != nil {
		filter.Until = *f.Until
	}
	if !filter.Since.IsZero() && !filter.Until.IsZero() && !filter.Since.Before(filter.Until) {
		return Filter{}, fmt.Errorf("--since %s must be before --until %s", filter.Since.Format(time.RFC3339Nano), filter.Until.Format(time.RFC3339Nano))
	}
	return filter, nil
}

func (q RequestQuery) resolve() (SearchQuery, error) {
	query := SearchQuery{ErrorsOnly: q.Errors, Raw: q.Raw, Tool: q.Tool}
	if q.Role != "" {
		role, err := ParseRole(q.Role)
		if err != nil {
			return SearchQuery{}, err
		}
		query.Role = role
	}
	if q.Pattern != "" {
		pattern, err := regexp.Compile("(?i)" + q.Pattern)
		if err != nil {
			return SearchQuery{}, fmt.Errorf("invalid pattern: %w", err)
		}
		query.Pattern = pattern
	}

	if query.Pattern == nil && query.Tool == "" && !query.ErrorsOnly {
		return SearchQuery{}, errors.New("search requires a pattern, --tool, or --errors")
	}
	if query.Raw && (query.Role != "" || query.Tool != "" || query.ErrorsOnly) {
		return SearchQuery{}, errors.New("--raw cannot combine with --role, --tool, or --errors")
	}
	if query.Raw && query.Pattern == nil {
		return SearchQuery{}, errors.New("--raw requires a pattern")
	}
	if query.Role != "" && query.Role != RoleTool && (query.Tool != "" || query.ErrorsOnly) {
		return SearchQuery{}, errors.New("--role user or assistant cannot combine with --tool or --errors")
	}
	return query, nil
}
