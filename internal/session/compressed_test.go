package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func compressTranscript(t *testing.T, lines ...string) []byte {
	t.Helper()
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	return encoder.EncodeAll([]byte(strings.Join(lines, "\n")+"\n"), nil)
}

func writeCompressedTranscript(t *testing.T, path string, modTime time.Time, lines ...string) {
	t.Helper()
	writeFile(t, path, modTime, compressTranscript(t, lines...))
}

func writeFile(t *testing.T, path string, modTime time.Time, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("Chtimes(%q): %v", path, err)
	}
}

// compressedRollout is a Codex rollout whose decompressed content is known
// line by line, so hits can be checked against decompressed line numbers.
var compressedRollout = []string{
	`{"timestamp":"2026-10-03T12:00:00Z","type":"session_meta","payload":{"id":"11111111-1111-4111-8111-111111111111","timestamp":"2026-10-03T12:00:00Z","cwd":"/Users/u/header","thread_source":"user","source":"cli"}}`,
	`{"timestamp":"2026-10-03T12:00:00Z","type":"turn_context","payload":{"workspace_roots":["/Users/u/workspace"]}}`,
	`{"timestamp":"2026-10-03T12:00:02Z","type":"event_msg","payload":{"type":"user_message","message":"compressed-session-probe"}}`,
	`not json`,
	`{"timestamp":"2026-10-03T12:00:04Z","type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"call1","arguments":"{\"command\":[\"go\",\"test\"]}"}}`,
	`{"timestamp":"2026-10-03T12:00:07Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call1","output":"compressed tool output"}}`,
}

func TestCompressedCodexRolloutsAreListedAndSearched(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(boolName(archived, "archived", "active"), func(t *testing.T) {
			base := t.TempDir()
			roots := Roots{CodexSessions: filepath.Join(base, "sessions"), CodexArchivedSessions: filepath.Join(base, "archived_sessions")}
			root := roots.CodexSessions
			if archived {
				root = roots.CodexArchivedSessions
			}
			path := filepath.Join(root, "2026", "10", "03", "rollout-2026-10-03T12-00-00-11111111-1111-4111-8111-111111111111.jsonl.zst")
			modified := time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)
			writeCompressedTranscript(t, path, modified, compressedRollout...)
			for _, other := range []string{roots.CodexSessions, roots.CodexArchivedSessions} {
				if err := os.MkdirAll(other, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			before := sessionTreeSnapshot(t, base)

			list := List(context.Background(), roots, Filter{})
			search := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("compressed")})
			raw := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile(`"call_id":"call1","output"`), Raw: true})

			if !list.Complete || len(list.Sessions) != 1 {
				t.Fatalf("List = %+v", list)
			}
			want := Session{
				Archived:   archived,
				Harness:    HarnessCodex,
				ID:         "11111111-1111-4111-8111-111111111111",
				ModifiedAt: modified,
				Origin:     Origin{Kind: OriginHuman, Evidence: map[string]string{"source": "cli", "thread_source": "user"}},
				Path:       path,
				Projects:   []string{"/Users/u/workspace"},
				StartedAt:  time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
			}
			got := list.Sessions[0]
			got.ModifiedAt = got.ModifiedAt.UTC()
			if !reflect.DeepEqual(got, want) {
				t.Errorf("session = %+v, want %+v", got, want)
			}
			if !search.Complete || len(search.Sessions) != 1 || search.Sessions[0].Path != path {
				t.Fatalf("Search = %+v", search)
			}
			var lines []int
			for _, hit := range search.Sessions[0].Hits {
				lines = append(lines, hit.Line)
			}
			if !reflect.DeepEqual(lines, []int{3, 6}) {
				t.Errorf("hit lines = %v, want decompressed lines 3 and 6", lines)
			}
			if hit := search.Sessions[0].Hits[0]; hit.Role != RoleUser || hit.Excerpt != "compressed-session-probe" || !hit.Timestamp.Equal(time.Date(2026, 10, 3, 12, 0, 2, 0, time.UTC)) {
				t.Errorf("user hit = %+v", hit)
			}
			if hit := search.Sessions[0].Hits[1]; hit.Kind != KindToolResult || hit.Tool != "shell" || hit.CallID != "call1" {
				t.Errorf("tool hit = %+v", hit)
			}
			if len(search.Diagnostics) != 1 || search.Diagnostics[0].Code != codeMalformedLines || search.Diagnostics[0].Path != path {
				t.Errorf("diagnostics = %+v, want malformed-lines for the compressed path", search.Diagnostics)
			}
			if len(raw.Sessions) != 1 || len(raw.Sessions[0].Hits) != 1 || raw.Sessions[0].Hits[0].Line != 6 || raw.Sessions[0].Hits[0].Role != RoleRaw {
				t.Errorf("raw search = %+v, want one raw hit on decompressed line 6", raw.Sessions)
			}
			if after := sessionTreeSnapshot(t, base); !reflect.DeepEqual(before, after) {
				t.Errorf("session inputs changed: before %+v, after %+v", before, after)
			}
		})
	}
}

func TestCompressedCodexRolloutsHonorFilters(t *testing.T) {
	base := t.TempDir()
	roots := Roots{CodexSessions: filepath.Join(base, "sessions"), CodexArchivedSessions: filepath.Join(base, "archived_sessions")}
	modified := time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)
	main := filepath.Join(roots.CodexSessions, "main.jsonl.zst")
	writeCompressedTranscript(t, main, modified, compressedRollout...)
	subagent := filepath.Join(roots.CodexSessions, "subagent.jsonl.zst")
	writeCompressedTranscript(t, subagent, modified,
		`{"timestamp":"2026-10-03T12:30:00Z","type":"session_meta","payload":{"id":"child","cwd":"/Users/u/workspace","source":{"subagent":"review"}}}`,
		`{"timestamp":"2026-10-03T12:30:01Z","type":"event_msg","payload":{"type":"user_message","message":"compressed child prompt"}}`)
	headerless := filepath.Join(roots.CodexArchivedSessions, "rollout-2026-10-03T12-45-00-22222222-2222-4222-8222-222222222222.jsonl.zst")
	writeCompressedTranscript(t, headerless, modified,
		`{"timestamp":"2026-10-03T12:45:01Z","type":"event_msg","payload":{"type":"user_message","message":"compressed headerless prompt"}}`)

	for _, test := range []struct {
		filter Filter
		name   string
		want   []string
	}{
		{name: "main sessions only", want: []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}},
		{name: "subagents", filter: Filter{IncludeSubagents: true}, want: []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "child"}},
		{name: "header ID", filter: Filter{IDs: []string{"11111111-1111-4111-8111-111111111111"}}, want: []string{"11111111-1111-4111-8111-111111111111"}},
		{name: "filename ID", filter: Filter{IDs: []string{"22222222-2222-4222-8222-222222222222"}}, want: []string{"22222222-2222-4222-8222-222222222222"}},
		{name: "subagent ID without subagents", filter: Filter{IDs: []string{"child"}}},
		{name: "archived", filter: Filter{ArchiveState: ArchiveArchived}, want: []string{"22222222-2222-4222-8222-222222222222"}},
		{name: "project", filter: Filter{Project: "workspace"}, want: []string{"11111111-1111-4111-8111-111111111111"}},
		{name: "period", filter: Filter{Since: time.Date(2026, 10, 3, 12, 40, 0, 0, time.UTC), Until: time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)}, want: []string{"22222222-2222-4222-8222-222222222222"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			list := List(context.Background(), roots, test.filter)
			search := Search(context.Background(), roots, test.filter, SearchQuery{Pattern: regexp.MustCompile("compressed")})

			if !list.Complete || !search.Complete {
				t.Fatalf("List = %+v, Search = %+v", list, search)
			}
			var listed, matched []string
			for _, entry := range list.Sessions {
				listed = append(listed, entry.ID)
			}
			for _, entry := range search.Sessions {
				matched = append(matched, entry.ID)
			}
			slices.Sort(listed)
			slices.Sort(matched)
			if !slices.Equal(listed, test.want) || !slices.Equal(matched, test.want) {
				t.Errorf("listed = %v, matched = %v, want %v", listed, matched, test.want)
			}
		})
	}
}

func TestCompressedAndPlainCopiesOfOneRolloutRemainSeparate(t *testing.T) {
	base := t.TempDir()
	roots := Roots{CodexSessions: filepath.Join(base, "sessions")}
	modified := time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)
	plain := filepath.Join(roots.CodexSessions, "rollout-2026-10-03T12-00-00-11111111-1111-4111-8111-111111111111.jsonl")
	writeTranscript(t, plain, modified, compressedRollout...)
	writeCompressedTranscript(t, plain+".zst", modified, compressedRollout...)

	report := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("compressed-session-probe")})

	if !report.Complete || len(report.Sessions) != 2 {
		t.Fatalf("Search = %+v, want both copies as separate sessions", report)
	}
	var paths []string
	for _, entry := range report.Sessions {
		paths = append(paths, entry.Path)
		if entry.ID != "11111111-1111-4111-8111-111111111111" || len(entry.Hits) != 1 || entry.Hits[0].Line != 3 {
			t.Errorf("copy = %+v, want the shared ID and a hit on line 3", entry)
		}
	}
	slices.Sort(paths)
	if !slices.Equal(paths, []string{plain, plain + ".zst"}) {
		t.Errorf("paths = %v", paths)
	}
}

func TestUndecodableCompressedRolloutsAreDiagnosed(t *testing.T) {
	valid := compressTranscript(t, compressedRollout...)
	for _, test := range []struct {
		data []byte
		name string
	}{
		{name: "empty file"},
		{name: "not zstd", data: []byte("{\"type\":\"session_meta\"}\n")},
		{name: "truncated frame", data: valid[:len(valid)/2]},
		{name: "corrupt payload", data: bytes.Replace(valid, valid[len(valid)-12:], bytes.Repeat([]byte{0xff}, 12), 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			roots := Roots{CodexSessions: filepath.Join(base, "sessions")}
			path := filepath.Join(roots.CodexSessions, "broken.jsonl.zst")
			writeFile(t, path, time.Now(), test.data)
			writeTranscript(t, filepath.Join(roots.CodexSessions, "intact.jsonl"), time.Now(), compressedRollout...)
			filter := Filter{Harnesses: []Harness{HarnessCodex}}

			list := List(context.Background(), roots, filter)
			search := Search(context.Background(), roots, filter, SearchQuery{Pattern: regexp.MustCompile("compressed-session-probe")})

			for name, report := range map[string]struct {
				complete    bool
				diagnostics []Diagnostic
				sessions    int
			}{
				"List":   {complete: list.Complete, diagnostics: list.Diagnostics, sessions: len(list.Sessions)},
				"Search": {complete: search.Complete, diagnostics: search.Diagnostics, sessions: len(search.Sessions)},
			} {
				if report.complete || report.sessions != 1 {
					t.Errorf("%s complete = %v with %d sessions, want incomplete with only the intact rollout", name, report.complete, report.sessions)
				}
				var reads []Diagnostic
				for _, diagnostic := range report.diagnostics {
					if diagnostic.Code == codeTranscriptRead {
						reads = append(reads, diagnostic)
					}
				}
				if len(reads) != 1 || reads[0].Path != path || reads[0].Harness != HarnessCodex || !strings.Contains(reads[0].Message, "decompress") {
					t.Errorf("%s transcript-read diagnostics = %+v, want one naming %q", name, reads, path)
				}
			}
		})
	}
}

func TestCompressedFrameWithEmptyContentListsLikeEmptyRollout(t *testing.T) {
	base := t.TempDir()
	roots := Roots{CodexSessions: filepath.Join(base, "sessions")}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(roots.CodexSessions, "rollout-2026-10-03T12-00-00-11111111-1111-4111-8111-111111111111.jsonl.zst"), time.Now(), encoder.EncodeAll(nil, nil))

	report := List(context.Background(), roots, Filter{})

	if !report.Complete || len(report.Diagnostics) != 0 || len(report.Sessions) != 1 || report.Sessions[0].ID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("List = %+v, want one complete headerless session", report)
	}
}

func TestOnlyCodexDiscoversCompressedTranscripts(t *testing.T) {
	base := t.TempDir()
	roots := Roots{Claude: filepath.Join(base, "claude"), CodexSessions: filepath.Join(base, "codex"), Pi: filepath.Join(base, "pi")}
	modified := time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)
	writeCompressedTranscript(t, filepath.Join(roots.Claude, "project", "claude.jsonl.zst"), modified,
		`{"type":"user","timestamp":"2026-10-03T12:00:00Z","cwd":"/Users/u/proj","message":{"role":"user","content":"compressed claude"}}`)
	writeCompressedTranscript(t, filepath.Join(roots.Pi, "project", "pi.jsonl.zst"), modified,
		`{"type":"session","id":"pi","timestamp":"2026-10-03T12:00:00Z","cwd":"/Users/u/proj"}`)
	writeCompressedTranscript(t, filepath.Join(roots.CodexSessions, "codex.jsonl.zst"), modified, compressedRollout...)

	report := List(context.Background(), roots, Filter{})

	if !report.Complete || len(report.Sessions) != 1 || report.Sessions[0].Harness != HarnessCodex {
		t.Fatalf("List = %+v, want only the Codex rollout", report)
	}
}
