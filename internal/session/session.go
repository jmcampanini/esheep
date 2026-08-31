// Package session reads historical harness session transcripts in place.
//
// Transcripts are harness-owned, read-only inputs. The package never creates,
// updates, or deletes anything under a session root and keeps no derived
// state; every result points back at the canonical transcript file.
package session

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Harness identifies one supported coding harness.
type Harness string

// Supported harnesses.
const (
	HarnessClaude Harness = "claude"
	HarnessCodex  Harness = "codex"
	HarnessPi     Harness = "pi"
)

// ParseHarness converts a user-supplied harness name.
func ParseHarness(value string) (Harness, error) {
	switch Harness(value) {
	case HarnessClaude, HarnessCodex, HarnessPi:
		return Harness(value), nil
	default:
		return "", fmt.Errorf("session: unknown harness %q (expected claude, codex, or pi)", value)
	}
}

// Role classifies who produced a transcript event.
type Role string

// Event roles. RoleTool covers both tool calls and tool results; RoleRaw
// marks hits from raw byte-level searches.
const (
	RoleAssistant Role = "assistant"
	RoleRaw       Role = "raw"
	RoleTool      Role = "tool"
	RoleUser      Role = "user"
)

// ParseRole converts a user-supplied role name.
func ParseRole(value string) (Role, error) {
	switch Role(value) {
	case RoleAssistant, RoleTool, RoleUser:
		return Role(value), nil
	default:
		return "", fmt.Errorf("session: unknown role %q (expected user, assistant, or tool)", value)
	}
}

// event is one interpreted transcript record component.
type event struct {
	failed    bool
	line      int
	role      Role
	text      string
	timestamp time.Time
	tool      string
}

// Session is metadata about one historical session transcript. Path is the
// canonical transcript file; best-effort fields may be zero when a grammar
// does not record them.
type Session struct {
	Harness    Harness   `json:"harness"`
	ID         string    `json:"id"`
	ModifiedAt time.Time `json:"modified_at"`
	Path       string    `json:"path"`
	Project    string    `json:"project,omitempty"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	Subagent   bool      `json:"subagent"`
	Title      string    `json:"title,omitempty"`
}

// Hit is one matching event addressed by its transcript line number.
type Hit struct {
	Error     bool      `json:"error,omitempty"`
	Excerpt   string    `json:"excerpt"`
	Line      int       `json:"line"`
	Role      Role      `json:"role"`
	Timestamp time.Time `json:"timestamp,omitzero"`
	Tool      string    `json:"tool,omitempty"`
}

// Match is one session together with its matching events.
type Match struct {
	Session
	Hits []Hit `json:"hits"`
}

// Diagnostic is a stable session-reading failure record.
type Diagnostic struct {
	Code    string  `json:"code"`
	Harness Harness `json:"harness,omitempty"`
	Message string  `json:"message,omitempty"`
	Path    string  `json:"path,omitempty"`
}

// Diagnostic codes.
const (
	codeMalformedLines = "malformed-lines"
	codeRootMissing    = "root-missing"
	codeRootUnusable   = "root-unusable"
	codeTranscriptRead = "transcript-read"
	codeWalk           = "walk"
)

// affectsCompleteness reports whether a diagnostic means results may be
// missing sessions or hits. A missing root is a skipped harness, and
// malformed lines are tolerated, so neither makes a report incomplete.
func affectsCompleteness(code string) bool {
	return code == codeRootUnusable || code == codeTranscriptRead || code == codeWalk
}

// ListReport is the historical session inventory.
type ListReport struct {
	Complete    bool         `json:"complete"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Sessions    []Session    `json:"sessions"`
}

// SearchReport contains the sessions whose transcripts matched a query.
type SearchReport struct {
	Complete    bool         `json:"complete"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Sessions    []Match      `json:"sessions"`
}

// Roots locates the per-harness session transcript directories.
type Roots struct {
	Claude string
	Codex  string
	Pi     string
}

// Filter selects sessions by metadata. A zero Filter selects every main
// session; subagent transcripts require IncludeSubagents.
type Filter struct {
	Harnesses        []Harness
	IncludeSubagents bool
	Project          string
	Since            time.Time
	Until            time.Time
}

// SearchQuery selects events within a session transcript. A nil Pattern
// matches every event that passes the structural criteria. Raw matches
// Pattern against undecoded transcript lines and ignores the structural
// criteria.
type SearchQuery struct {
	ErrorsOnly bool
	Pattern    *regexp.Regexp
	Raw        bool
	Role       Role
	Tool       string
}

// ParseTimeFlag interprets a --since or --until value as a day count
// ("7d"), a Go duration ("36h") subtracted from now, or a local calendar
// date ("2026-08-01").
func ParseTimeFlag(value string, now time.Time) (time.Time, error) {
	if days, ok := strings.CutSuffix(value, "d"); ok {
		if n, err := time.ParseDuration(days + "h"); err == nil && !strings.ContainsAny(days, ".-") {
			return now.Add(-24 * n), nil
		}
	}
	if duration, err := time.ParseDuration(value); err == nil {
		if duration < 0 {
			return time.Time{}, fmt.Errorf("session: time %q must not be negative", value)
		}
		return now.Add(-duration), nil
	}
	if date, err := time.ParseInLocation("2006-01-02", value, now.Location()); err == nil {
		return date, nil
	}
	return time.Time{}, fmt.Errorf("session: time %q is not a day count (7d), duration (36h), or date (2026-01-02)", value)
}

// adapter interprets one harness grammar. Implementations own every fact
// about their format: layout, subagent marking, roles, and error flags.
type adapter interface {
	// discover returns transcript references under root in walk order.
	discover(root string, includeSubagents bool) ([]transcript, []Diagnostic)
	// meta extracts cheap best-effort metadata for one transcript.
	meta(t transcript) (Session, error)
	// scan interprets the transcript and calls visit once per event,
	// returning the count of unparseable lines.
	scan(path string, visit func(event)) (int, error)
}

// transcript is one discovered session file.
type transcript struct {
	modTime  time.Time
	path     string
	subagent bool
}

type located struct {
	adapter adapter
	session Session
}

// List inventories historical sessions under the configured roots, most
// recently started first.
func List(ctx context.Context, roots Roots, filter Filter) ListReport {
	sessions, diagnostics, complete := collect(ctx, roots, filter)
	report := ListReport{Complete: complete, Diagnostics: diagnostics}
	for _, entry := range sessions {
		report.Sessions = append(report.Sessions, entry.session)
	}
	return report
}

// Search scans matching sessions' transcripts and reports the sessions with
// at least one matching event, most recently started first.
func Search(ctx context.Context, roots Roots, filter Filter, query SearchQuery) SearchReport {
	sessions, diagnostics, complete := collect(ctx, roots, filter)
	report := SearchReport{Complete: complete, Diagnostics: diagnostics}

	type scanResult struct {
		diagnostics []Diagnostic
		hits        []Hit
	}
	results := parallelMap(ctx, sessions, func(entry located) scanResult {
		hits, malformed, err := scanSession(entry, query)
		result := scanResult{hits: hits}
		if err != nil {
			result.diagnostics = append(result.diagnostics, Diagnostic{
				Code: codeTranscriptRead, Harness: entry.session.Harness, Message: err.Error(), Path: entry.session.Path,
			})
		}
		if malformed > 0 && !query.Raw {
			result.diagnostics = append(result.diagnostics, Diagnostic{
				Code:    codeMalformedLines,
				Harness: entry.session.Harness,
				Message: fmt.Sprintf("skipped %d unparseable lines", malformed),
				Path:    entry.session.Path,
			})
		}
		return result
	})
	for index, result := range results {
		for _, diagnostic := range result.diagnostics {
			report.Diagnostics = append(report.Diagnostics, diagnostic)
			if affectsCompleteness(diagnostic.Code) {
				report.Complete = false
			}
		}
		if len(result.hits) != 0 {
			report.Sessions = append(report.Sessions, Match{Session: sessions[index].session, Hits: result.hits})
		}
	}
	return report
}

func scanSession(entry located, query SearchQuery) ([]Hit, int, error) {
	if query.Raw {
		hits, err := scanRaw(entry.session.Path, query.Pattern)
		return hits, 0, err
	}
	var hits []Hit
	malformed, err := entry.adapter.scan(entry.session.Path, func(e event) {
		if hit, ok := matchEvent(e, query); ok {
			hits = append(hits, hit)
		}
	})
	return hits, malformed, err
}

func scanRaw(path string, pattern *regexp.Regexp) ([]Hit, error) {
	var hits []Hit
	err := forEachLine(path, func(line int, data []byte) bool {
		loc := pattern.FindIndex(data)
		if loc == nil {
			return true
		}
		hits = append(hits, Hit{Excerpt: excerpt(string(data), []int{loc[0], loc[1]}), Line: line, Role: RoleRaw})
		return true
	})
	return hits, err
}

func matchEvent(e event, query SearchQuery) (Hit, bool) {
	if query.Role != "" && e.role != query.Role {
		return Hit{}, false
	}
	if query.Tool != "" && !strings.EqualFold(e.tool, query.Tool) {
		return Hit{}, false
	}
	if query.ErrorsOnly && !e.failed {
		return Hit{}, false
	}
	var loc []int
	if query.Pattern != nil {
		loc = query.Pattern.FindStringIndex(e.text)
		if loc == nil {
			return Hit{}, false
		}
	}
	return Hit{
		Error:     e.failed,
		Excerpt:   excerpt(e.text, loc),
		Line:      e.line,
		Role:      e.role,
		Timestamp: e.timestamp,
		Tool:      e.tool,
	}, true
}

// collect discovers, describes, filters, and orders the sessions in scope.
func collect(ctx context.Context, roots Roots, filter Filter) ([]located, []Diagnostic, bool) {
	var diagnostics []Diagnostic
	complete := true
	var sessions []located
	for _, root := range harnessRoots(roots, filter.Harnesses) {
		transcripts, rootDiagnostics := discoverRoot(root, filter.IncludeSubagents)
		diagnostics = append(diagnostics, rootDiagnostics...)

		kept := transcripts[:0]
		for _, t := range transcripts {
			if t.subagent && !filter.IncludeSubagents {
				continue
			}
			if !filter.Since.IsZero() && t.modTime.Before(filter.Since) {
				continue
			}
			kept = append(kept, t)
		}

		metas := parallelMap(ctx, kept, func(t transcript) describedSession {
			s, err := root.adapter.meta(t)
			return describedSession{err: err, session: s}
		})
		for _, meta := range metas {
			if meta.err != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Code: codeTranscriptRead, Harness: root.harness, Message: meta.err.Error(), Path: meta.session.Path,
				})
			}
			if meta.session.Path == "" || (meta.session.Subagent && !filter.IncludeSubagents) {
				continue
			}
			if filter.matchesSession(meta.session) {
				sessions = append(sessions, located{adapter: root.adapter, session: meta.session})
			}
		}
	}
	for _, diagnostic := range diagnostics {
		if affectsCompleteness(diagnostic.Code) {
			complete = false
		}
	}
	if ctx.Err() != nil {
		complete = false
	}
	sort.SliceStable(sessions, func(left, right int) bool {
		l, r := sessions[left].session, sessions[right].session
		lt, rt := l.sortTime(), r.sortTime()
		if !lt.Equal(rt) {
			return lt.After(rt)
		}
		return l.Path < r.Path
	})
	sort.SliceStable(diagnostics, func(left, right int) bool {
		return diagnostics[left].Path < diagnostics[right].Path
	})
	return sessions, diagnostics, complete
}

type describedSession struct {
	err     error
	session Session
}

type harnessRoot struct {
	adapter adapter
	harness Harness
	root    string
}

func harnessRoots(roots Roots, harnesses []Harness) []harnessRoot {
	all := []harnessRoot{
		{adapter: claudeAdapter{}, harness: HarnessClaude, root: roots.Claude},
		{adapter: codexAdapter{}, harness: HarnessCodex, root: roots.Codex},
		{adapter: piAdapter{}, harness: HarnessPi, root: roots.Pi},
	}
	if len(harnesses) == 0 {
		return all
	}
	wanted := make(map[Harness]struct{}, len(harnesses))
	for _, harness := range harnesses {
		wanted[harness] = struct{}{}
	}
	selected := make([]harnessRoot, 0, len(all))
	for _, root := range all {
		if _, ok := wanted[root.harness]; ok {
			selected = append(selected, root)
		}
	}
	return selected
}

func discoverRoot(root harnessRoot, includeSubagents bool) ([]transcript, []Diagnostic) {
	info, err := os.Stat(root.root)
	switch {
	case err != nil && os.IsNotExist(err):
		return nil, []Diagnostic{{
			Code: codeRootMissing, Harness: root.harness, Message: "session root does not exist; harness skipped", Path: root.root,
		}}
	case err != nil:
		return nil, []Diagnostic{{Code: codeRootUnusable, Harness: root.harness, Message: err.Error(), Path: root.root}}
	case !info.IsDir():
		return nil, []Diagnostic{{
			Code: codeRootUnusable, Harness: root.harness, Message: "session root is not a directory", Path: root.root,
		}}
	}
	transcripts, diagnostics := root.adapter.discover(root.root, includeSubagents)
	for index := range diagnostics {
		diagnostics[index].Harness = root.harness
	}
	return transcripts, diagnostics
}

func (f Filter) matchesSession(s Session) bool {
	if !f.Until.IsZero() && s.sortTime().After(f.Until) {
		return false
	}
	if f.Project != "" && !strings.Contains(strings.ToLower(s.Project), strings.ToLower(f.Project)) {
		return false
	}
	return true
}

// sortTime prefers the recorded start and falls back to the file
// modification time for transcripts whose grammar hid the start.
func (s Session) sortTime() time.Time {
	if !s.StartedAt.IsZero() {
		return s.StartedAt
	}
	return s.ModifiedAt
}

// excerpt returns a single-line window of text around the match location,
// with whitespace collapsed. A nil location windows the beginning.
func excerpt(text string, loc []int) string {
	const before, width = 40, 160
	start := 0
	if loc != nil && loc[0] > before {
		start = loc[0] - before
	}
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	end := min(start+width, len(text))
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	window := strings.Join(strings.Fields(text[start:end]), " ")
	if start > 0 {
		window = "…" + window
	}
	if end < len(text) {
		window += "…"
	}
	return window
}

// parallelMap applies fn to every item on a bounded worker pool and returns
// results in input order. Cancellation leaves unprocessed results zero.
func parallelMap[T, R any](ctx context.Context, items []T, fn func(T) R) []R {
	results := make([]R, len(items))
	workers := min(runtime.GOMAXPROCS(0), len(items))
	if workers <= 1 {
		for index, item := range items {
			if ctx.Err() != nil {
				break
			}
			results[index] = fn(item)
		}
		return results
	}
	indexes := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range indexes {
				results[index] = fn(items[index])
			}
		}()
	}
	for index := range items {
		if ctx.Err() != nil {
			break
		}
		indexes <- index
	}
	close(indexes)
	wg.Wait()
	return results
}
