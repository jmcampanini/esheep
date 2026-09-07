package session

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"
)

func TestCodexAndWorkProjectsIncludeWorkspaceRootsAcrossSavedTurns(t *testing.T) {
	for _, harness := range []Harness{HarnessCodex, HarnessChatGPTWork} {
		t.Run(string(harness), func(t *testing.T) {
			root := t.TempDir()
			originator := "Codex Desktop"
			if harness == HarnessChatGPTWork {
				originator = "codex_work_desktop"
			}
			lines := []string{
				fmt.Sprintf(`{"type":"session_meta","payload":{"id":"main","originator":%q,"cwd":"/header/fallback","timestamp":"2026-09-06T10:00:00Z"}}`, originator),
				`{"type":"turn_context","payload":{"workspace_roots":["/work/api","/work/docs","/work/api",""]}}`,
				`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"migration plan"}]}}`,
			}
			for range 600 {
				lines = append(lines, `{"type":"event_msg","payload":{"type":"token_count"}}`)
			}
			lines = append(lines,
				`{"type":"turn_context","payload":{"workspace_roots":["/work/api","/work/web"]}}`,
				`{"type":"turn_context","payload":{"workspace_roots":[]}}`,
			)
			path := filepath.Join(root, "main.jsonl")
			writeTranscript(t, path, time.Now(), lines...)
			roots := Roots{CodexSessions: root}

			for _, project := range []string{"", "DOCS", "web", "api", "fallback"} {
				filter := Filter{Harnesses: []Harness{harness}, Project: project}
				list := List(context.Background(), roots, filter)
				search := Search(context.Background(), roots, filter, SearchQuery{Pattern: regexp.MustCompile("migration")})

				wantCount := 1
				if project == "fallback" {
					wantCount = 0
				}
				if !list.Complete || !search.Complete || len(list.Diagnostics) != 0 || len(search.Diagnostics) != 0 || len(list.Sessions) != wantCount || len(search.Sessions) != wantCount {
					t.Fatalf("project %q: List = %+v, Search = %+v", project, list, search)
				}
				if wantCount == 0 {
					continue
				}
				want := []string{"/work/api", "/work/docs", "/work/web"}
				if !slices.Equal(list.Sessions[0].Projects, want) || !slices.Equal(search.Sessions[0].Projects, want) {
					t.Errorf("projects = %v / %v, want %v", list.Sessions[0].Projects, search.Sessions[0].Projects, want)
				}
				if len(search.Sessions[0].Hits) != 1 || search.Sessions[0].Hits[0].Line != 3 {
					t.Errorf("hits = %+v, want original message line", search.Sessions[0].Hits)
				}
			}
		})
	}
}

func TestCodexProjectFallbackAndAbsentPaths(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		cwd  string
		name string
	}{
		{name: "fallback", cwd: "/work/fallback"},
		{name: "absent"},
	} {
		writeTranscript(t, filepath.Join(root, test.name+".jsonl"), time.Now(),
			fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q}}`, test.name, test.cwd),
			`{"type":"turn_context","payload":{"workspace_roots":[]}}`,
			`{"type":"turn_context","payload":{"workspace_roots":"unrecognized"}}`,
		)
	}

	report := List(context.Background(), Roots{CodexSessions: root}, Filter{})

	if !report.Complete || len(report.Sessions) != 2 {
		t.Fatalf("List = %+v", report)
	}
	for _, entry := range report.Sessions {
		if entry.ID == "fallback" && !slices.Equal(entry.Projects, []string{"/work/fallback"}) {
			t.Errorf("fallback projects = %v", entry.Projects)
		}
		if entry.ID == "absent" && (entry.Projects == nil || len(entry.Projects) != 0) {
			t.Errorf("absent projects = %#v, want empty array", entry.Projects)
		}
	}
}
