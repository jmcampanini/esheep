package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"testing"
	"time"
)

func writeCoworkCompanion(t *testing.T, audit string, metadata map[string]any) {
	t.Helper()
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Dir(audit)+".json", data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCoworkDiscoversOnlyQualifyingSessionAudits(t *testing.T) {
	root := t.TempDir()
	modified := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	message := `{"type":"user","_audit_timestamp":"2026-09-06T10:00:00Z","message":{"role":"user","content":"needle"}}`
	for _, scope := range []string{"scope-a", "scope-b"} {
		path := filepath.Join(root, scope, "group", "local_same", "audit.jsonl")
		writeTranscript(t, path, modified, message)
	}
	for _, name := range []string{
		"audit.jsonl", "scope-a/audit.jsonl", "scope-a/group/audit.jsonl",
		"scope-a/group/other/audit.jsonl", "scope-a/group/local_/audit.jsonl",
		"scope-a/group/local_same/notes.jsonl", "scope-a/group/local_same/artifacts/audit.jsonl",
		"scope-a/group/local_same/local_nested/audit.jsonl", "scope-a/group/extra/local_deep/audit.jsonl",
	} {
		writeTranscript(t, filepath.Join(root, filepath.FromSlash(name)), modified, message)
	}
	for _, test := range []struct {
		body string
		name string
	}{
		{name: "title", body: `{"type":"system","title":"needle"}`},
		{name: "result", body: `{"type":"result","is_error":true,"result":"needle"}`},
		{name: "injected", body: `{"type":"user","isMeta":true,"message":{"content":"needle"}}`},
		{name: "synthetic", body: `{"type":"user","isSynthetic":true,"message":{"content":"needle"}}`},
		{name: "unsupported", body: `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"needle"},{"type":"image"},{"type":"document"}]}}`},
		{name: "empty"},
	} {
		path := filepath.Join(root, "scope-a", "group", "local_"+test.name, "audit.jsonl")
		writeTranscript(t, path, modified, test.body)
		writeCoworkCompanion(t, path, map[string]any{"sessionId": "local_" + test.name, "title": "needle"})
	}
	if err := os.WriteFile(filepath.Join(root, "scope-a", "group", "local_metadata-only.json"), []byte(`{"sessionId":"local_metadata-only","title":"needle"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := sessionTreeSnapshot(t, root)
	roots := Roots{ClaudeCowork: root}

	list := List(context.Background(), roots, Filter{})
	search := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("needle"), Raw: true})

	wantIDs := []string{"scope-a/group/local_same", "scope-b/group/local_same"}
	var ids []string
	for _, entry := range list.Sessions {
		ids = append(ids, entry.ID)
		if entry.Harness != HarnessClaudeCowork || entry.Archived || entry.Subagent || entry.Title != "" || entry.Projects == nil || len(entry.Projects) != 0 {
			t.Errorf("session = %+v, want Cowork with absent metadata", entry)
		}
	}
	if !list.Complete || len(list.Diagnostics) != 0 || !slices.Equal(ids, wantIDs) {
		t.Fatalf("List = %+v, want IDs %v", list, wantIDs)
	}
	if !search.Complete || len(search.Diagnostics) != 0 || len(search.Sessions) != len(wantIDs) {
		t.Fatalf("raw Search = %+v, want only qualifying audits", search)
	}
	if after := sessionTreeSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Error("Cowork discovery or search changed source files")
	}
}

func TestCoworkSearchKeepsPrimaryRequestsAndResponses(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scope", "group", "local_main", "audit.jsonl")
	modified := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	writeTranscript(t, path, modified,
		`{"type":"system","_audit_timestamp":"2026-09-06T10:00:00Z"}`,
		`{"type":"user","session_id":"desktop","_audit_timestamp":"2026-09-06T10:00:01Z","message":{"role":"user","content":"needle primary prompt"}}`,
		`{"type":"assistant","session_id":"cli","timestamp":"2026-09-06T10:00:02Z","_audit_timestamp":"2026-09-06T10:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"needle reply"}]}}`,
		`{"type":"assistant","timestamp":"unavailable","_audit_timestamp":"2026-09-06T10:00:04Z","message":{"content":[{"type":"tool_use","id":"call","name":"Agent","input":{"prompt":"needle request"}}]}}`,
		`{"type":"user","parent_tool_use_id":"call","message":{"content":[{"type":"text","text":"needle child prompt"}]}}`,
		`{"type":"assistant","parent_tool_use_id":"call","message":{"content":[{"type":"tool_use","id":"call","name":"Read","input":{"file":"needle.txt"}}]}}`,
		`{"type":"user","parent_tool_use_id":"call","message":{"content":[{"type":"tool_result","tool_use_id":"call","is_error":true,"content":[{"type":"text","text":"needle child failure"}]}]}}`,
		`{"type":"user","isSynthetic":true,"parent_tool_use_id":null,"message":{"content":[{"type":"text","text":"needle injected context"},{"type":"tool_result","tool_use_id":"call","content":"needle child response with error text"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"needle final"},{"type":"thinking","thinking":"needle hidden"},{"type":"image"},{"type":"document"}]}}`,
		`{"type":"result","is_error":true,"result":"needle summary"}`,
		`{"type":"rate_limit_event","details":"needle ignored"}`,
		`{"type":"user","message":"needle unfinished"`,
	)
	started := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	writeCoworkCompanion(t, path, map[string]any{
		"createdAt": started.UnixMilli(), "isArchived": true, "sessionId": "local_main", "title": "Migration plan",
		"userSelectedFolders": []string{"/host/api", "/host/docs", "/host/api", ""}, "cwd": "/sessions/vm/work",
	})
	roots := Roots{ClaudeCowork: root}
	before := sessionTreeSnapshot(t, root)

	for _, test := range []struct {
		filter Filter
		lines  []int
		name   string
		query  SearchQuery
	}{
		{name: "primary", lines: []int{2, 3, 4, 8, 9}},
		{name: "with children", filter: Filter{IncludeSubagents: true}, lines: []int{2, 3, 4, 5, 6, 7, 8, 9}},
		{name: "Agent request and return", query: SearchQuery{Tool: "agent"}, lines: []int{4, 8}},
		{name: "child tool excluded", query: SearchQuery{Tool: "Read"}},
		{name: "child tool included", filter: Filter{IncludeSubagents: true}, query: SearchQuery{Tool: "Read"}, lines: []int{6, 7}},
		{name: "primary user", query: SearchQuery{Role: RoleUser}, lines: []int{2}},
		{name: "no inferred primary failure", query: SearchQuery{ErrorsOnly: true}},
		{name: "structured child failure", filter: Filter{IncludeSubagents: true}, query: SearchQuery{ErrorsOnly: true}, lines: []int{7}},
		{name: "raw includes saved child and unsupported lines", query: SearchQuery{Raw: true}, lines: []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}},
		{name: "second folder with archive and time filters", filter: Filter{ArchiveState: ArchiveArchived, Project: "DOCS", Since: modified, Until: started}, lines: []int{2, 3, 4, 8, 9}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.query.Pattern = regexp.MustCompile("needle")

			report := Search(context.Background(), roots, test.filter, test.query)

			if !report.Complete {
				t.Fatalf("Search = %+v", report)
			}
			if test.query.Raw {
				if len(report.Diagnostics) != 0 {
					t.Errorf("raw diagnostics = %+v", report.Diagnostics)
				}
			} else if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != codeMalformedLines {
				t.Errorf("diagnostics = %+v, want tolerated malformed line", report.Diagnostics)
			}
			var lines []int
			for _, match := range report.Sessions {
				if match.Path != path || match.ID != "scope/group/local_main" || match.Subagent || !match.Archived ||
					!match.StartedAt.Equal(started) || !match.ModifiedAt.Equal(modified) || match.Title != "Migration plan" ||
					!slices.Equal(match.Projects, []string{"/host/api", "/host/docs"}) {
					t.Errorf("session metadata = %+v", match.Session)
				}
				for _, hit := range match.Hits {
					lines = append(lines, hit.Line)
					if test.query.Raw {
						continue
					}
					if (hit.Line == 4 || hit.Line == 8) && hit.Tool != "Agent" {
						t.Errorf("primary tool = %+v, want Agent despite reused child call ID", hit)
					}
					if hit.Line == 3 && !hit.Timestamp.Equal(time.Date(2026, 9, 6, 10, 0, 2, 0, time.UTC)) {
						t.Errorf("message timestamp = %v", hit.Timestamp)
					}
					if hit.Line == 4 && !hit.Timestamp.Equal(time.Date(2026, 9, 6, 10, 0, 4, 0, time.UTC)) {
						t.Errorf("audit timestamp fallback = %v", hit.Timestamp)
					}
				}
			}
			if !slices.Equal(lines, test.lines) {
				t.Errorf("hit lines = %v, want %v", lines, test.lines)
			}
		})
	}
	active := List(context.Background(), roots, Filter{ArchiveState: ArchiveActive})
	if len(active.Sessions) != 0 {
		t.Errorf("active filter included archived Cowork: %+v", active)
	}
	if after := sessionTreeSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Error("Cowork metadata or search changed source files")
	}
}

func TestCoworkRetainsPartialMessagesWithoutUsableCompanions(t *testing.T) {
	for _, test := range []struct {
		body string
		code string
		name string
	}{
		{name: "missing"},
		{name: "malformed", body: `{`, code: codeMetadataInvalid},
		{name: "mismatched", body: `{"sessionId":"local_other","title":"wrong title","isArchived":true,"userSelectedFolders":["/wrong"]}`, code: codeMetadataInvalid},
		{name: "invalid flag", body: `{"sessionId":"local_partial","isArchived":"yes"}`, code: codeMetadataInvalid},
		{name: "missing flag", body: `{"sessionId":"local_partial"}`},
		{name: "unusable", code: codeMetadataRead},
		{name: "unreadable", body: `{"sessionId":"local_partial"}`, code: codeMetadataRead},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "unreadable" && os.Geteuid() == 0 {
				t.Skip("root can read permission-denied files")
			}
			root := t.TempDir()
			path := filepath.Join(root, "scope", "group", "local_partial", "audit.jsonl")
			writeTranscript(t, path, time.Now(),
				`{"type":"user","_audit_timestamp":"2026-09-06T10:00:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"missing-call","is_error":true,"content":"saved partial output"}]}}`)
			companion := filepath.Dir(path) + ".json"
			if test.body != "" {
				if err := os.WriteFile(companion, []byte(test.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "unusable" {
				if err := os.Mkdir(companion, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "unreadable" {
				if err := os.Chmod(companion, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(companion, 0o600) })
			}

			report := Search(context.Background(), Roots{ClaudeCowork: root}, Filter{ArchiveState: ArchiveActive}, SearchQuery{ErrorsOnly: true})

			if report.Complete != (test.code != codeMetadataRead) || len(report.Sessions) != 1 {
				t.Fatalf("Search = %+v, want readable partial output", report)
			}
			match := report.Sessions[0]
			if match.Archived || match.Title != "" || len(match.Projects) != 0 || !match.StartedAt.Equal(time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)) ||
				len(match.Hits) != 1 || match.Hits[0].Tool != "" || match.Hits[0].Line != 1 || !match.Hits[0].Error {
				t.Errorf("partial match = %+v", match)
			}
			if test.code == "" {
				if len(report.Diagnostics) != 0 {
					t.Errorf("diagnostics = %+v", report.Diagnostics)
				}
			} else if len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != test.code || report.Diagnostics[0].Path != companion {
				t.Errorf("diagnostics = %+v, want %s for companion", report.Diagnostics, test.code)
			}
		})
	}
}
