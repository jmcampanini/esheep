package session

import (
	"math"
	"slices"
	"testing"
	"time"
)

func pagedReport(hitsPerSession ...int) SearchReport {
	start := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	report := SearchReport{Complete: true, Sources: []Source{{Harness: HarnessClaude, Path: "/claude", Status: SourceSearched}}}
	for index, count := range hitsPerSession {
		match := Match{Session: Session{Path: string(rune('a'+index)) + ".jsonl", StartedAt: start.Add(-time.Duration(index) * time.Hour)}}
		for line := 1; line <= count; line++ {
			match.Hits = append(match.Hits, Hit{Line: line})
		}
		match.MatchedHits = count
		report.Sessions = append(report.Sessions, match)
		report.MatchedHits += count
	}
	report.MatchedSessions = len(report.Sessions)
	return report
}

func pagePaths(report SearchReport) []string {
	var paths []string
	for _, match := range report.Sessions {
		paths = append(paths, match.Path)
	}
	return paths
}

func TestPaginateBoundsSessionsAndHits(t *testing.T) {
	report := pagedReport(3, 1, 2)

	page := report.Paginate(Page{HitLimit: 2, Limit: 2})

	if got, want := pagePaths(page), []string{"a.jsonl", "b.jsonl"}; !slices.Equal(got, want) {
		t.Fatalf("Paginate sessions = %v, want %v", got, want)
	}
	first := page.Sessions[0]
	if len(first.Hits) != 2 || first.Hits[1].Line != 2 || first.MatchedHits != 3 || first.OmittedHits != 1 {
		t.Errorf("first session = %d hits ending at line %d, matched %d, omitted %d; want lines 1-2 of 3, 1 omitted",
			len(first.Hits), first.Hits[len(first.Hits)-1].Line, first.MatchedHits, first.OmittedHits)
	}
	if second := page.Sessions[1]; len(second.Hits) != 1 || second.OmittedHits != 0 {
		t.Errorf("second session = %d hits, %d omitted; want 1 and 0", len(second.Hits), second.OmittedHits)
	}
	if page.MatchedSessions != 3 || page.MatchedHits != 6 || page.RemainingSessions != 1 || !page.HasMore || page.NextCursor == nil {
		t.Errorf("Paginate counts = matched %d/%d, remaining %d, has_more %v, cursor %v; want 3/6, 1, true, set",
			page.MatchedSessions, page.MatchedHits, page.RemainingSessions, page.HasMore, page.NextCursor)
	}
	if len(report.Sessions[0].Hits) != 3 {
		t.Errorf("Paginate changed its receiver: first session has %d hits, want 3", len(report.Sessions[0].Hits))
	}
}

func TestPaginateCursorWalksEverySessionOnce(t *testing.T) {
	report := pagedReport(1, 1, 1, 1, 1)

	var seen []string
	page := Page{Limit: 2}
	for range 5 {
		result := report.Paginate(page)
		seen = append(seen, pagePaths(result)...)
		if result.NextCursor == nil {
			if result.HasMore || result.RemainingSessions != 0 {
				t.Fatalf("last page has_more %v with %d remaining, want false and 0", result.HasMore, result.RemainingSessions)
			}
			break
		}
		cursor, err := ParseCursor(*result.NextCursor)
		if err != nil {
			t.Fatalf("ParseCursor(%q): %v", *result.NextCursor, err)
		}
		page.After = cursor
	}

	if want := []string{"a.jsonl", "b.jsonl", "c.jsonl", "d.jsonl", "e.jsonl"}; !slices.Equal(seen, want) {
		t.Fatalf("sessions across pages = %v, want %v", seen, want)
	}
}

func TestPaginateCursorOrdersEqualTimesByPath(t *testing.T) {
	report := pagedReport(1, 1, 1)
	for index := range report.Sessions {
		report.Sessions[index].StartedAt = report.Sessions[0].StartedAt
	}

	first := report.Paginate(Page{Limit: 1})
	cursor, err := ParseCursor(*first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	second := report.Paginate(Page{After: cursor, Limit: 1})

	if got := pagePaths(second); !slices.Equal(got, []string{"b.jsonl"}) {
		t.Fatalf("page after a.jsonl at a shared start time = %v, want [b.jsonl]", got)
	}
}

func TestPaginateHugeLimitAfterCursorReturnsTheRest(t *testing.T) {
	report := pagedReport(1, 1, 1)
	first := report.Sessions[0].Session

	page := report.Paginate(Page{After: Cursor{Path: first.Path, SortTime: first.SortTime()}, Limit: math.MaxInt})

	if got := pagePaths(page); !slices.Equal(got, []string{"b.jsonl", "c.jsonl"}) || page.HasMore {
		t.Fatalf("Paginate with the largest limit = %v, has_more %v; want [b.jsonl c.jsonl], false", got, page.HasMore)
	}
}

func TestPaginateCursorPastTheEndReturnsEmptyPage(t *testing.T) {
	report := pagedReport(1, 1)
	last := report.Sessions[1].Session

	page := report.Paginate(Page{After: Cursor{Path: last.Path, SortTime: last.SortTime()}})

	if len(page.Sessions) != 0 || page.HasMore || page.NextCursor != nil || page.MatchedSessions != 2 {
		t.Fatalf("Paginate after the last session = %d sessions, has_more %v, cursor %v, matched %d; want 0, false, nil, 2",
			len(page.Sessions), page.HasMore, page.NextCursor, page.MatchedSessions)
	}
}

func TestParseCursorRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"not base64!", "bm8tbmV3bGluZQ", "eWVzdGVyZGF5Ci9wYXRo", "MjAyNi0wOC0yMFQxMjowMDowMFoK"} {
		if _, err := ParseCursor(value); err == nil {
			t.Errorf("ParseCursor(%q) succeeded, want error", value)
		}
	}
}

func TestRequestPageResolve(t *testing.T) {
	limit, hitLimit, zero := 3, 7, 0
	cursor := Cursor{Path: "/p.jsonl", SortTime: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}.String()
	bad := "!"
	tests := []struct {
		name    string
		request RequestPage
		want    Page
		wantErr bool
	}{
		{name: "defaults", want: Page{HitLimit: 5, Limit: 10}},
		{name: "explicit limits", request: RequestPage{HitLimit: &hitLimit, Limit: &limit}, want: Page{HitLimit: 7, Limit: 3}},
		{name: "all", request: RequestPage{All: true}, want: Page{}},
		{name: "cursor", request: RequestPage{Cursor: &cursor}, want: Page{After: Cursor{Path: "/p.jsonl", SortTime: time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)}, HitLimit: 5, Limit: 10}},
		{name: "all with limit", request: RequestPage{All: true, Limit: &limit}, wantErr: true},
		{name: "all with hit limit", request: RequestPage{All: true, HitLimit: &hitLimit}, wantErr: true},
		{name: "zero limit", request: RequestPage{Limit: &zero}, wantErr: true},
		{name: "zero hit limit", request: RequestPage{HitLimit: &zero}, wantErr: true},
		{name: "malformed cursor", request: RequestPage{Cursor: &bad}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.request.Resolve()

			if test.wantErr {
				if err == nil {
					t.Fatalf("Resolve() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(): %v", err)
			}
			if got.Limit != test.want.Limit || got.HitLimit != test.want.HitLimit || got.After.Path != test.want.After.Path || !got.After.SortTime.Equal(test.want.After.SortTime) {
				t.Errorf("Resolve() = %+v, want %+v", got, test.want)
			}
		})
	}
}
