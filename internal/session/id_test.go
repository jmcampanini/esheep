package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestIDFilterPreservesMatchingSessions(t *testing.T) {
	base := t.TempDir()
	roots := Roots{
		Claude: filepath.Join(base, "claude"), ClaudeCowork: filepath.Join(base, "cowork"),
		CodexArchivedSessions: filepath.Join(base, "archive"), CodexSessions: filepath.Join(base, "codex"),
		Pi: filepath.Join(base, "pi"),
	}
	started := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	modified := started.Add(time.Hour)
	paths := map[string]string{
		"claude":      filepath.Join(roots.Claude, "project", "Shared.jsonl"),
		"child":       filepath.Join(roots.Claude, "project", "parent", "subagents", "Shared.jsonl"),
		"codex":       filepath.Join(roots.CodexSessions, "filename-id.jsonl"),
		"work":        filepath.Join(roots.CodexArchivedSessions, "work.jsonl"),
		"pi":          filepath.Join(roots.Pi, "pi.jsonl"),
		"cowork-a":    filepath.Join(roots.ClaudeCowork, "scope-a", "group", "local_Shared", "audit.jsonl"),
		"cowork-b":    filepath.Join(roots.ClaudeCowork, "scope-b", "group", "local_Shared", "audit.jsonl"),
		"fallback":    filepath.Join(roots.CodexSessions, "fallback.jsonl"),
		"pi-fallback": filepath.Join(roots.Pi, "pi-fallback.jsonl"),
	}
	claudeMessage := `{"type":"user","timestamp":"2026-09-06T10:00:00Z","cwd":"/project","message":{"content":"needle"}}`
	for _, name := range []string{"claude", "child", "cowork-a", "cowork-b"} {
		writeTranscript(t, paths[name], modified, claudeMessage, `{"type":"ai-title","aiTitle":"Session title"}`)
	}
	for _, name := range []string{"codex", "work"} {
		originator := "cli"
		if name == "work" {
			originator = "codex_work_desktop"
		}
		writeTranscript(t, paths[name], modified,
			fmt.Sprintf(`{"type":"session_meta","payload":{"id":"Shared","timestamp":"2026-09-06T10:00:00Z","cwd":"/project","originator":%q}}`, originator),
			`{"type":"event_msg","payload":{"type":"user_message","message":"needle"}}`,
			strings.Repeat("{}\n", 600)+`{"type":"turn_context","payload":{"workspace_roots":["/project","/late-project"]}}`)
	}
	writeTranscript(t, paths["pi"], modified,
		`{"type":"session","id":"Shared","timestamp":"2026-09-06T10:00:00Z","cwd":"/project"}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"needle"}]}}`,
		strings.Repeat("{}\n", 50)+`{"type":"session_info","name":"Late title"}`)
	writeTranscript(t, paths["fallback"], modified, `{"type":"event_msg","payload":{"type":"user_message","message":"needle"}}`)
	writeTranscript(t, paths["pi-fallback"], modified, `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"needle"}]}}`)
	writeTranscript(t, filepath.Join(roots.CodexSessions, "empty-work.jsonl"), modified,
		`{"type":"session_meta","payload":{"id":"Empty","originator":"codex_work_desktop"}}`,
		`{"type":"session_info","payload":{"title":"needle"}}`)
	writeTranscript(t, filepath.Join(roots.ClaudeCowork, "scope", "group", "local_Empty", "audit.jsonl"), modified,
		`{"type":"user","isMeta":true,"message":{"content":"needle"}}`)
	query := SearchQuery{Pattern: regexp.MustCompile("needle"), Role: RoleUser}
	all := List(context.Background(), roots, Filter{IncludeSubagents: true})
	allHits := Search(context.Background(), roots, Filter{IncludeSubagents: true}, query)
	if !all.Complete || len(all.Diagnostics) != 0 || len(all.Sessions) != len(paths) ||
		!allHits.Complete || len(allHits.Diagnostics) != 0 || len(allHits.Sessions) != len(paths) {
		t.Fatalf("fixture inventory = %+v, search = %+v", all, allHits)
	}

	for _, test := range []struct {
		filter Filter
		name   string
		want   []string
	}{
		{name: "across harnesses and archives", filter: Filter{IDs: []string{"Shared"}}, want: []string{"claude", "codex", "work", "pi"}},
		{name: "union and repeated IDs", filter: Filter{IDs: []string{"Shared", "local_Shared", "Shared"}}, want: []string{"claude", "codex", "work", "pi", "cowork-a", "cowork-b"}},
		{name: "case sensitive", filter: Filter{IDs: []string{"shared"}}},
		{name: "no prefixes", filter: Filter{IDs: []string{"Shar"}}},
		{name: "no substrings", filter: Filter{IDs: []string{"hared"}}},
		{name: "unknown ID", filter: Filter{IDs: []string{"missing"}}},
		{name: "header overrides filename", filter: Filter{IDs: []string{"filename-id"}}},
		{name: "Codex filename fallback", filter: Filter{IDs: []string{"fallback"}}, want: []string{"fallback"}},
		{name: "Pi filename fallback", filter: Filter{IDs: []string{"pi-fallback.jsonl"}}, want: []string{"pi-fallback"}},
		{name: "Cowork short ID", filter: Filter{IDs: []string{"local_Shared"}}, want: []string{"cowork-a", "cowork-b"}},
		{name: "Cowork scoped ID", filter: Filter{IDs: []string{"scope-a/group/local_Shared"}}, want: []string{"cowork-a"}},
		{name: "Cowork scoped and short overlap", filter: Filter{IDs: []string{"scope-a/group/local_Shared", "local_Shared"}}, want: []string{"cowork-a", "cowork-b"}},
		{name: "Work and Cowork eligibility", filter: Filter{IDs: []string{"Empty", "local_Empty"}}},
		{name: "harness intersection", filter: Filter{Harnesses: []Harness{HarnessPi}, IDs: []string{"Shared"}}, want: []string{"pi"}},
		{name: "archive intersection", filter: Filter{ArchiveState: ArchiveArchived, IDs: []string{"Shared"}}, want: []string{"work"}},
		{name: "late project intersection", filter: Filter{IDs: []string{"Shared"}, Project: "LATE-PROJECT"}, want: []string{"codex", "work"}},
		{name: "since intersection", filter: Filter{IDs: []string{"Shared"}, Since: modified.Add(time.Second)}},
		{name: "until intersection", filter: Filter{IDs: []string{"Shared"}, Until: started.Add(-time.Second)}},
		{name: "subagents on request", filter: Filter{IDs: []string{"Shared"}, IncludeSubagents: true}, want: []string{"claude", "child", "codex", "work", "pi"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var wantedPaths []string
			for _, name := range test.want {
				wantedPaths = append(wantedPaths, paths[name])
			}
			var wantSessions []Session
			for _, entry := range all.Sessions {
				if slices.Contains(wantedPaths, entry.Path) {
					wantSessions = append(wantSessions, entry)
				}
			}
			var wantHits []Match
			for _, entry := range allHits.Sessions {
				if slices.Contains(wantedPaths, entry.Path) {
					wantHits = append(wantHits, entry)
				}
			}

			list := List(context.Background(), roots, test.filter)
			search := Search(context.Background(), roots, test.filter, query)

			if !list.Complete || len(list.Diagnostics) != 0 || !reflect.DeepEqual(list.Sessions, wantSessions) {
				t.Errorf("List = %+v, want sessions %+v without diagnostics", list, wantSessions)
			}
			if !search.Complete || len(search.Diagnostics) != 0 || !reflect.DeepEqual(search.Sessions, wantHits) {
				t.Errorf("Search = %+v, want matches %+v without diagnostics", search, wantHits)
			}
		})
	}
}

func TestIDFilterSkipsUnrelatedCoworkCompanions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scope", "group", "local_broken", "audit.jsonl")
	writeTranscript(t, path, time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC), `{"type":"user","message":{"content":"needle"}}`)
	if err := os.Mkdir(filepath.Dir(path)+".json", 0o700); err != nil {
		t.Fatal(err)
	}

	unrelated := List(context.Background(), Roots{ClaudeCowork: root}, Filter{IDs: []string{"local_other"}})
	matching := List(context.Background(), Roots{ClaudeCowork: root}, Filter{IDs: []string{"local_broken"}})

	if !unrelated.Complete || len(unrelated.Diagnostics) != 0 || len(unrelated.Sessions) != 0 {
		t.Errorf("unrelated lookup = %+v, want complete empty result without companion diagnostics", unrelated)
	}
	if matching.Complete || len(matching.Diagnostics) != 1 || matching.Diagnostics[0].Code != codeMetadataRead || len(matching.Sessions) != 1 {
		t.Errorf("matching lookup = %+v, want readable session and incomplete companion diagnostic", matching)
	}
}

func TestIDFilterReportsRequiredReadFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failures require an unprivileged user")
	}
	root := t.TempDir()
	path := filepath.Join(root, "unreadable.jsonl")
	writeTranscript(t, path, time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC), `{"type":"session_meta","payload":{"id":"header-id"}}`)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	for _, test := range []struct {
		complete bool
		id       string
		name     string
		roots    Roots
	}{
		{name: "unrelated Claude path", roots: Roots{Claude: root}, id: "other", complete: true},
		{name: "matching Claude path", roots: Roots{Claude: root}, id: "unreadable"},
		{name: "Codex requires header", roots: Roots{CodexSessions: root}, id: "other"},
		{name: "Pi requires metadata", roots: Roots{Pi: root}, id: "other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := List(context.Background(), test.roots, Filter{IDs: []string{test.id}})

			if report.Complete != test.complete || len(report.Sessions) != 0 {
				t.Errorf("List = %+v, want complete %v and no sessions", report, test.complete)
			}
			if test.complete && len(report.Diagnostics) != 0 {
				t.Errorf("diagnostics = %+v, want none for excluded path", report.Diagnostics)
			}
			if !test.complete && (len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != codeTranscriptRead) {
				t.Errorf("diagnostics = %+v, want required transcript read failure", report.Diagnostics)
			}
		})
	}
}
