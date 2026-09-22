package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestPeriodDatesAndElapsedBounds(t *testing.T) {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, zone)
	for _, test := range []struct {
		value string
		since time.Time
		until time.Time
	}{
		{value: "2026-11-01", since: time.Date(2026, 11, 1, 0, 0, 0, 0, zone), until: time.Date(2026, 11, 2, 0, 0, 0, 0, zone)},
		{value: "2026-03-08", since: time.Date(2026, 3, 8, 0, 0, 0, 0, zone), until: time.Date(2026, 3, 9, 0, 0, 0, 0, zone)},
		{value: "7d", since: now.Add(-168 * time.Hour), until: now.Add(-168 * time.Hour)},
		{value: "36h", since: now.Add(-36 * time.Hour), until: now.Add(-36 * time.Hour)},
	} {
		t.Run(test.value, func(t *testing.T) {
			since, sinceErr := ParseSince(test.value, now)
			until, untilErr := ParseUntil(test.value, now)

			if sinceErr != nil || untilErr != nil || !since.Equal(test.since) || !until.Equal(test.until) {
				t.Fatalf("bounds = %v, %v (%v, %v), want %v, %v", since, until, sinceErr, untilErr, test.since, test.until)
			}
		})
	}
	if _, err := ParseUntil("yesterday", now); err == nil {
		t.Fatal("ParseUntil accepted an invalid value")
	}
}

func TestPeriodContainsHalfOpenBounds(t *testing.T) {
	since := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	for _, test := range []struct {
		filter Filter
		name   string
		stamp  time.Time
		want   bool
	}{
		{name: "open", stamp: since, want: true},
		{name: "undated", want: false},
		{name: "lower inclusive", filter: Filter{Since: since, Until: until}, stamp: since, want: true},
		{name: "before lower", filter: Filter{Since: since}, stamp: since.Add(-time.Nanosecond)},
		{name: "upper exclusive", filter: Filter{Until: until}, stamp: until},
		{name: "before upper", filter: Filter{Until: until}, stamp: until.Add(-time.Nanosecond), want: true},
		{name: "open upper", filter: Filter{Since: since}, stamp: until, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.filter.contains(test.stamp); got != test.want {
				t.Errorf("contains(%v) = %t, want %t", test.stamp, got, test.want)
			}
		})
	}
}

func TestPeriodMembershipIgnoresFileAndSessionTimes(t *testing.T) {
	since := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	filter := Filter{Since: since, Until: until}
	for _, start := range []string{"2026-08-20T10:00:00Z", "2026-09-22T00:00:00Z"} {
		t.Run(start, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "period.jsonl")
			writeTranscript(t, path, since.Add(-24*time.Hour),
				fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"content":"retry outside"}}`, start),
				`{"type":"user","timestamp":"2026-09-18T12:00:00Z","message":{"content":"retry inside"}}`,
			)
			roots := Roots{Claude: root}
			query := SearchQuery{Pattern: regexp.MustCompile("retry")}
			for _, modTime := range []time.Time{since.Add(-24 * time.Hour), until.Add(24 * time.Hour)} {
				if err := os.Chtimes(path, modTime, modTime); err != nil {
					t.Fatal(err)
				}

				inventory := List(context.Background(), roots, filter)
				matches := Search(context.Background(), roots, filter, query)

				if !inventory.Complete || len(inventory.Sessions) != 1 || len(inventory.Diagnostics) != 0 {
					t.Fatalf("inventory = %+v, want one qualifying session", inventory)
				}
				if !matches.Complete || len(matches.Sessions) != 1 || len(matches.Sessions[0].Hits) != 1 || matches.Sessions[0].Hits[0].Line != 2 {
					t.Fatalf("search = %+v, want only the September 18 hit", matches)
				}
				if matches.Period.Since == nil || !matches.Period.Since.Equal(since) || matches.Period.Until == nil || !matches.Period.Until.Equal(until) {
					t.Errorf("period = %+v, want resolved bounds", matches.Period)
				}
			}
			unbounded := Search(context.Background(), roots, Filter{}, query)
			if len(unbounded.Sessions) != 1 || len(unbounded.Sessions[0].Hits) != 2 {
				t.Fatalf("unbounded search = %+v, want both events", unbounded)
			}
		})
	}
}

func TestPeriodUndatedEventsAndHeaderlessTranscript(t *testing.T) {
	root := t.TempDir()
	since := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	until := since.Add(8 * 24 * time.Hour)
	path := filepath.Join(root, "undated.jsonl")
	writeTranscript(t, path, until.Add(24*time.Hour),
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"retry missing"}]}}`,
		`{"type":"response_item","timestamp":"invalid","payload":{"type":"message","role":"assistant","content":[{"text":"retry invalid"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"unrelated"}]}}`,
	)
	roots := Roots{CodexSessions: root}
	filter := Filter{Since: since, Until: until}

	inventory := List(context.Background(), roots, filter)
	matches := Search(context.Background(), roots, filter, SearchQuery{Pattern: regexp.MustCompile("retry")})

	if !inventory.Complete || len(inventory.Sessions) != 0 || len(inventory.Diagnostics) != 1 || inventory.Diagnostics[0].Message != "excluded session: 3 events without a usable timestamp and none dated within the period" {
		t.Fatalf("inventory = %+v, want a diagnostic for three undated events", inventory)
	}
	if !matches.Complete || len(matches.Sessions) != 0 || len(matches.Diagnostics) != 1 {
		t.Fatalf("search = %+v, want a complete empty result with a diagnostic", matches)
	}
	diagnostic := matches.Diagnostics[0]
	if diagnostic.Code != codeUndatedEvents || diagnostic.Path != path || diagnostic.Harness != HarnessCodex || diagnostic.Message != "excluded 2 matching events without a usable timestamp" {
		t.Errorf("diagnostic = %+v", diagnostic)
	}
	if report := List(context.Background(), roots, Filter{}); len(report.Sessions) != 1 || len(report.Diagnostics) != 0 {
		t.Errorf("unbounded list = %+v, want the undated session", report)
	}
	if report := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("retry")}); len(report.Sessions) != 1 || len(report.Sessions[0].Hits) != 2 || len(report.Diagnostics) != 0 {
		t.Errorf("unbounded search = %+v, want both undated matches", report)
	}
	writeTranscript(t, filepath.Join(root, "dated.jsonl"), until.Add(24*time.Hour),
		`{"type":"event_msg","timestamp":"2026-09-18T12:00:00Z","payload":{"type":"user_message","message":"retry dated"}}`,
	)
	if report := List(context.Background(), roots, filter); len(report.Sessions) != 1 || report.Sessions[0].ID != "dated" {
		t.Errorf("headerless list = %+v, want dated session despite modification time", report)
	}
}

func TestPeriodListReportsOnlyScannedMalformedLines(t *testing.T) {
	for _, qualifying := range []bool{false, true} {
		t.Run(fmt.Sprint(qualifying), func(t *testing.T) {
			root := t.TempDir()
			stamp := "2026-08-01T00:00:00Z"
			if qualifying {
				stamp = "2026-09-18T12:00:00Z"
			}
			writeTranscript(t, filepath.Join(root, "session.jsonl"), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
				"malformed before",
				fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"content":"retry"}}`, stamp),
				"malformed after",
			)

			report := List(context.Background(), Roots{Claude: root}, Filter{Since: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)})

			count, sessions := 2, 0
			if qualifying {
				count, sessions = 1, 1
			}
			if !report.Complete || len(report.Sessions) != sessions || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != codeMalformedLines || report.Diagnostics[0].Message != fmt.Sprintf("skipped %d unparseable lines", count) {
				t.Fatalf("report = %+v, want %d sessions and %d malformed lines", report, sessions, count)
			}
		})
	}
}

func TestPeriodJSONAlwaysIncludesOpenBounds(t *testing.T) {
	for _, report := range []any{List(context.Background(), Roots{}, Filter{}), Search(context.Background(), Roots{}, Filter{}, SearchQuery{})} {
		encoded, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"period":{"since":null,"until":null}`) {
			t.Errorf("JSON = %s, want explicit open bounds", encoded)
		}
	}
}
