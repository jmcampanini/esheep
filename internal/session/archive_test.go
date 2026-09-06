package session

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestArchivesPreserveHarnessFiltersAndTranscriptLines(t *testing.T) {
	base := t.TempDir()
	roots := Roots{
		Claude:        filepath.Join(base, "claude"),
		Codex:         filepath.Join(base, "sessions"),
		CodexArchived: filepath.Join(base, "archived_sessions"),
		Pi:            filepath.Join(base, "pi"),
	}
	for _, fixture := range []struct {
		body       string
		name       string
		originator string
		root       string
		source     string
	}{
		{name: "active-codex", originator: "Codex Desktop", root: roots.Codex},
		{name: "active-work", originator: "codex_work_desktop", root: roots.Codex},
		{name: "archived-codex", originator: "Codex Desktop", root: roots.CodexArchived},
		{name: "archived-work", originator: "codex_work_desktop", root: roots.CodexArchived,
			body: `{"type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"a","arguments":"needle"}}`},
		{name: "archived-subagent", originator: "codex_work_desktop", root: roots.CodexArchived, source: `{"subagent":"review"}`},
		{name: "title-only", originator: "codex_work_desktop", root: roots.CodexArchived,
			body: `{"type":"session_info","payload":{"title":"needle"}}`},
		{name: "context-only", originator: "codex_work_desktop", root: roots.CodexArchived,
			body: `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"needle"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["plugins.recommendations"]}}}`},
	} {
		source := fixture.source
		if source == "" {
			source = `"exec"`
		}
		body := fixture.body
		if body == "" {
			body = `{"type":"event_msg","payload":{"type":"user_message","message":"needle"}}`
		}
		writeTranscript(t, filepath.Join(fixture.root, fixture.name+".jsonl"), time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
			fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"originator":%q,"source":%s,"history_mode":"paginated"}}`, fixture.name, fixture.originator, source), body)
	}
	writeTranscript(t, filepath.Join(roots.Claude, "p", "claude.jsonl"), time.Now(),
		`{"type":"user","message":{"role":"user","content":"needle"}}`)
	writeTranscript(t, filepath.Join(roots.Pi, "p", "pi.jsonl"), time.Now(),
		`{"type":"session","id":"pi"}`, `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"needle"}]}}`)
	before := sessionTreeSnapshot(t, base)

	for _, test := range []struct {
		filter Filter
		name   string
		want   []string
	}{
		{name: "unfiltered", want: []string{"active-codex", "active-work", "archived-codex", "archived-work", "claude", "pi"}},
		{name: "active", filter: Filter{ArchiveState: ArchiveActive}, want: []string{"active-codex", "active-work", "claude", "pi"}},
		{name: "archived", filter: Filter{ArchiveState: ArchiveArchived}, want: []string{"archived-codex", "archived-work"}},
		{name: "Codex", filter: Filter{Harnesses: []Harness{HarnessCodex}}, want: []string{"active-codex", "archived-codex"}},
		{name: "active Codex", filter: Filter{Harnesses: []Harness{HarnessCodex}, ArchiveState: ArchiveActive}, want: []string{"active-codex"}},
		{name: "archived Codex", filter: Filter{Harnesses: []Harness{HarnessCodex}, ArchiveState: ArchiveArchived}, want: []string{"archived-codex"}},
		{name: "Work", filter: Filter{Harnesses: []Harness{HarnessChatGPTWork}}, want: []string{"active-work", "archived-work"}},
		{name: "active Work", filter: Filter{Harnesses: []Harness{HarnessChatGPTWork}, ArchiveState: ArchiveActive}, want: []string{"active-work"}},
		{name: "archived Work", filter: Filter{Harnesses: []Harness{HarnessChatGPTWork}, ArchiveState: ArchiveArchived}, want: []string{"archived-work"}},
		{name: "combined", filter: Filter{Harnesses: []Harness{HarnessCodex, HarnessChatGPTWork}}, want: []string{"active-codex", "active-work", "archived-codex", "archived-work"}},
		{name: "archived subagents", filter: Filter{ArchiveState: ArchiveArchived, IncludeSubagents: true}, want: []string{"archived-codex", "archived-subagent", "archived-work"}},
		{name: "archived other harnesses", filter: Filter{ArchiveState: ArchiveArchived, Harnesses: []Harness{HarnessClaude, HarnessPi}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			list := List(context.Background(), roots, test.filter)
			search := Search(context.Background(), roots, test.filter, SearchQuery{Pattern: regexp.MustCompile("needle")})

			if !list.Complete || !search.Complete || len(list.Diagnostics) != 0 || len(search.Diagnostics) != 0 {
				t.Fatalf("List = %+v, Search = %+v", list, search)
			}
			var listed, matched []string
			for _, entry := range list.Sessions {
				listed = append(listed, entry.ID)
				if entry.Archived != strings.HasPrefix(entry.ID, "archived-") || entry.Subagent != (entry.ID == "archived-subagent") {
					t.Errorf("archive/subagent classification = %+v", entry)
				}
			}
			for _, entry := range search.Sessions {
				matched = append(matched, entry.ID)
				contents, err := os.ReadFile(entry.Path)
				if err != nil {
					t.Fatal(err)
				}
				if len(entry.Hits) != 1 {
					t.Fatalf("hits = %+v, want one", entry.Hits)
				}
				line := entry.Hits[0].Line
				lines := strings.Split(string(contents), "\n")
				if line < 1 || line > len(lines) || !strings.Contains(lines[line-1], "needle") {
					t.Errorf("hit = %+v does not address matching transcript line", entry)
				}
				if entry.ID == "archived-work" && (entry.Harness != HarnessChatGPTWork || !entry.Archived || entry.Hits[0].Role != RoleTool) {
					t.Errorf("tool-only Work archive = %+v", entry)
				}
			}
			slices.Sort(listed)
			slices.Sort(matched)
			if !slices.Equal(listed, test.want) || !slices.Equal(matched, test.want) {
				t.Errorf("listed = %v, matched = %v, want %v", listed, matched, test.want)
			}
		})
	}

	if after := sessionTreeSnapshot(t, base); !reflect.DeepEqual(before, after) {
		t.Errorf("session inputs changed: before %+v, after %+v", before, after)
	}
}

type sessionFileSnapshot struct {
	content  string
	mode     fs.FileMode
	modified time.Time
}

func sessionTreeSnapshot(t *testing.T, root string) map[string]sessionFileSnapshot {
	t.Helper()
	snapshot := make(map[string]sessionFileSnapshot)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		var contents []byte
		if info.Mode().IsRegular() {
			contents, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		snapshot[path] = sessionFileSnapshot{content: string(contents), mode: info.Mode(), modified: info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestArchiveDuplicatesPreferArchivePath(t *testing.T) {
	for _, layout := range []string{"same root", "nested root", "hard link", "distinct files with same ID"} {
		t.Run(layout, func(t *testing.T) {
			base := t.TempDir()
			active := filepath.Join(base, "sessions")
			archive := filepath.Join(base, "archived_sessions")
			switch layout {
			case "same root":
				active = archive
			case "nested root":
				archive = filepath.Join(active, "archive")
			}
			archivePath := filepath.Join(archive, "archive.jsonl")
			writeTranscript(t, archivePath, time.Now(),
				`{"type":"session_meta","payload":{"id":"same-id","originator":"codex_work_desktop"}}`,
				`{"type":"event_msg","payload":{"type":"user_message","message":"needle"}}`)
			if err := os.MkdirAll(active, 0o755); err != nil {
				t.Fatal(err)
			}
			activePath := filepath.Join(active, "active.jsonl")
			wantAll, wantActive := 1, 0
			switch layout {
			case "hard link":
				if err := os.Link(archivePath, activePath); err != nil {
					t.Fatal(err)
				}
			case "distinct files with same ID":
				writeTranscript(t, activePath, time.Now(),
					`{"type":"session_meta","payload":{"id":"same-id","originator":"codex_work_desktop"}}`,
					`{"type":"event_msg","payload":{"type":"user_message","message":"another needle"}}`)
				wantAll, wantActive = 2, 1
			}
			roots := Roots{Codex: active, CodexArchived: archive}
			for _, test := range []struct {
				state ArchiveState
				want  int
			}{{state: ArchiveAll, want: wantAll}, {state: ArchiveActive, want: wantActive}, {state: ArchiveArchived, want: 1}} {
				filter := Filter{ArchiveState: test.state, Harnesses: []Harness{HarnessCodex, HarnessChatGPTWork}}

				list := List(context.Background(), roots, filter)
				search := Search(context.Background(), roots, filter, SearchQuery{Pattern: regexp.MustCompile("needle")})

				if !list.Complete || !search.Complete || len(list.Sessions) != test.want || len(search.Sessions) != test.want {
					t.Fatalf("state %q: List = %+v, Search = %+v, want %d", test.state, list, search, test.want)
				}
				for _, entry := range list.Sessions {
					if entry.Archived && entry.Path != archivePath {
						t.Errorf("archive path = %q, want %q", entry.Path, archivePath)
					}
					if test.state == ArchiveArchived && !entry.Archived {
						t.Errorf("archive-only result = %+v", entry)
					}
				}
			}
		})
	}
}

func TestArchiveReadFailuresAffectCompleteness(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission failures require an unprivileged user")
	}
	for _, failure := range []string{"missing", "not a directory", "unreadable directory", "unreadable transcript"} {
		t.Run(failure, func(t *testing.T) {
			base := t.TempDir()
			active := filepath.Join(base, "sessions")
			archive := filepath.Join(base, "archived_sessions")
			if err := os.Mkdir(active, 0o755); err != nil {
				t.Fatal(err)
			}
			code := codeRootMissing
			switch failure {
			case "not a directory":
				if err := os.WriteFile(archive, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				code = codeRootUnusable
			case "unreadable directory":
				if err := os.Mkdir(archive, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(archive, 0o755) })
				code = codeWalk
			case "unreadable transcript":
				path := filepath.Join(archive, "unreadable.jsonl")
				writeTranscript(t, path, time.Now(), `{"type":"session_meta"}`)
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
				code = codeTranscriptRead
			}
			roots := Roots{Codex: active, CodexArchived: archive}
			filter := Filter{Harnesses: []Harness{HarnessCodex, HarnessChatGPTWork}}

			list := List(context.Background(), roots, filter)
			search := Search(context.Background(), roots, filter, SearchQuery{Pattern: regexp.MustCompile("needle")})

			wantComplete := failure == "missing"
			if list.Complete != wantComplete || search.Complete != wantComplete || len(list.Sessions) != 0 || len(search.Sessions) != 0 {
				t.Errorf("List = %+v, Search = %+v, want complete %v and no sessions", list, search, wantComplete)
			}
			for _, diagnostics := range [][]Diagnostic{list.Diagnostics, search.Diagnostics} {
				if len(diagnostics) != 1 || diagnostics[0].Code != code || diagnostics[0].Harness != HarnessCodex || !strings.HasPrefix(diagnostics[0].Path, archive) {
					t.Errorf("diagnostics = %+v, want %s for archive", diagnostics, code)
				}
			}
		})
	}
}
