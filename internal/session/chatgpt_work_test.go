package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSharedCodexStorageClassifiesAndFiltersProducers(t *testing.T) {
	roots := fixtureRoots(t)
	fixtures := []struct {
		name       string
		originator string
		source     any
		want       Harness
	}{
		{name: "work", originator: "codex_work_desktop", source: "exec", want: HarnessChatGPTWork},
		{name: "desktop", originator: "Codex Desktop", source: "vscode", want: HarnessCodex},
		{name: "cli", originator: "codex_cli_rs", source: "cli", want: HarnessCodex},
		{name: "missing", source: "exec", want: HarnessCodex},
		{name: "unknown", originator: "future_producer", source: "exec", want: HarnessCodex},
		{name: "near match", originator: "codex_work_desktop_other", source: "exec", want: HarnessCodex},
		{name: "work subagent", originator: "codex_work_desktop", source: map[string]any{"subagent": map[string]string{"other": "guardian"}}, want: HarnessChatGPTWork},
		{name: "desktop subagent", originator: "Codex Desktop", source: map[string]string{"subagent": "review"}, want: HarnessCodex},
		{name: "missing subagent", source: map[string]string{"subagent": "review"}, want: HarnessCodex},
	}
	for _, fixture := range fixtures {
		payload := map[string]any{"cwd": "/project/chatgpt-work", "id": fixture.name, "source": fixture.source}
		if fixture.originator != "" {
			payload["originator"] = fixture.originator
		}
		header, err := json.Marshal(map[string]any{"type": "session_meta", "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		writeTranscript(t, filepath.Join(roots.Codex, fixture.name+".jsonl"), time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
			string(header),
			`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"migration"}]}}`,
		)
	}
	writeTranscript(t, filepath.Join(roots.Codex, "headerless.jsonl"), time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
		`{"type":"event_msg","payload":{"type":"user_message","message":"migration"}}`,
	)
	for _, selection := range []struct {
		harnesses []Harness
		name      string
	}{
		{name: "unfiltered"},
		{name: "work", harnesses: []Harness{HarnessChatGPTWork}},
		{name: "codex", harnesses: []Harness{HarnessCodex}},
		{name: "combined", harnesses: []Harness{HarnessCodex, HarnessChatGPTWork}},
		{name: "repeated", harnesses: []Harness{HarnessChatGPTWork, HarnessCodex, HarnessChatGPTWork}},
	} {
		for _, subagents := range []bool{false, true} {
			t.Run(selection.name+"/subagents="+strconv.FormatBool(subagents), func(t *testing.T) {
				filter := Filter{Harnesses: selection.harnesses, IncludeSubagents: subagents, Project: "chatgpt-work"}
				want := make(map[string]Harness)
				for _, fixture := range fixtures {
					if strings.Contains(fixture.name, "subagent") && !subagents {
						continue
					}
					if selection.name == "work" && fixture.want != HarnessChatGPTWork || selection.name == "codex" && fixture.want != HarnessCodex {
						continue
					}
					want[fixture.name] = fixture.want
				}

				list := List(context.Background(), roots, filter)
				search := Search(context.Background(), roots, filter, SearchQuery{Pattern: regexp.MustCompile("migration")})

				if !list.Complete || !search.Complete || len(list.Diagnostics) != 0 || len(search.Diagnostics) != 0 {
					t.Fatalf("reports = %+v, %+v", list, search)
				}
				got := make(map[string]Harness)
				for _, entry := range list.Sessions {
					got[entry.ID] = entry.Harness
				}
				if !reflect.DeepEqual(got, want) || len(list.Sessions) != len(want) {
					t.Errorf("List sessions = %+v, want %v without duplicates", list.Sessions, want)
				}
				got = make(map[string]Harness)
				for _, entry := range search.Sessions {
					got[entry.ID] = entry.Harness
					if len(entry.Hits) != 1 || entry.Hits[0].Line != 2 || entry.Path != filepath.Join(roots.Codex, entry.ID+".jsonl") {
						t.Errorf("Search entry = %+v, want one hit at original path and line 2", entry)
					}
				}
				if !reflect.DeepEqual(got, want) || len(search.Sessions) != len(want) {
					t.Errorf("Search sessions = %+v, want %v without duplicates", search.Sessions, want)
				}
			})
		}
	}

	headerless := Search(context.Background(), roots, Filter{Harnesses: []Harness{HarnessCodex}}, SearchQuery{Role: RoleUser, Pattern: regexp.MustCompile("migration")})
	if len(headerless.Sessions) != 1 || headerless.Sessions[0].ID != "headerless" || headerless.Sessions[0].Hits[0].Line != 1 {
		t.Fatalf("headerless search = %+v", headerless)
	}
}

func TestWorkInventoryRequiresSavedConversationEvents(t *testing.T) {
	for _, test := range []struct {
		body    string
		history string
		name    string
		want    int
	}{
		{name: "header only"},
		{name: "title only", body: `{"type":"session_info","payload":{"title":"migration"}}`},
		{name: "injected only", body: `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"injected"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["plugins.recommendations"]}}}`},
		{name: "untagged user context", body: `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"context"}]}}`},
		{name: "empty message", body: `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[]}}`},
		{name: "unsupported event", body: `{"type":"response_item","payload":{"type":"web_search_call"}}`},
		{name: "paginated user text", history: "paginated", want: 1, body: `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"migration"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["user.text"]}}}`},
		{name: "unknown history assistant", history: "unknown", want: 1, body: `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"migration"}]}}`},
		{name: "absent history legacy user", want: 1, body: `{"type":"event_msg","payload":{"type":"user_message","message":"migration"}}`},
		{name: "tool call only", want: 1, body: `{"type":"response_item","payload":{"type":"function_call","call_id":"a","name":"shell","arguments":"migration"}}`},
		{name: "tool result only", want: 1, body: `{"type":"response_item","payload":{"type":"function_call_output","call_id":"a","output":"migration"}}`},
		{name: "custom tool call only", want: 1, body: `{"type":"response_item","payload":{"type":"custom_tool_call","call_id":"a","name":"apply_patch","input":"migration"}}`},
		{name: "custom tool result only", want: 1, body: `{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"a","output":"migration"}}`},
		{name: "MCP result only", want: 1, body: `{"type":"event_msg","payload":{"type":"mcp_tool_call_end","invocation":{"tool":"query"},"result":{"Err":"migration"}}}`},
		{name: "late event after malformed line", want: 1, body: strings.Repeat("{\"type\":\"turn_context\"}\n", 600) + "malformed\n" + `{"type":"event_msg","payload":{"type":"user_message","message":"migration"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			header := `{"type":"session_meta","payload":{"originator":"codex_work_desktop"}}`
			if test.history != "" {
				header = `{"type":"session_meta","payload":{"originator":"codex_work_desktop","history_mode":"` + test.history + `"}}`
			}
			writeTranscript(t, filepath.Join(root, "work.jsonl"), time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC), header, test.body)
			filter := Filter{Harnesses: []Harness{HarnessChatGPTWork}}

			list := List(context.Background(), Roots{Codex: root}, filter)
			raw := Search(context.Background(), Roots{Codex: root}, filter, SearchQuery{Raw: true, Pattern: regexp.MustCompile(".")})

			if !list.Complete || len(list.Diagnostics) != 0 || len(list.Sessions) != test.want {
				t.Errorf("List = %+v, want %d sessions", list, test.want)
			}
			if !raw.Complete || len(raw.Diagnostics) != 0 || len(raw.Sessions) != test.want {
				t.Errorf("raw Search = %+v, want %d sessions", raw, test.want)
			}
		})
	}
}

func TestWorkSearchPreservesProvenanceToolsAndTranscriptLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "work.jsonl")
	writeTranscript(t, path, time.Date(2026, 9, 6, 11, 0, 0, 0, time.UTC),
		`{"timestamp":"2026-09-06T10:00:00Z","type":"session_meta","payload":{"id":"work","cwd":"/project/work","originator":"codex_work_desktop","history_mode":"paginated"}}`,
		`{"timestamp":"2026-09-06T10:01:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"injected context"},{"text":"migration request"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["plugins.recommendations","user.text"]}}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"migration request"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"migration plan"}]}}`,
		`{"type":"response_item","payload":{"type":"function_call","call_id":"a","name":"shell","arguments":"migration command"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"a","output":[{"text":"migration output"}]}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","call_id":"b","name":"apply_patch","input":"migration patch"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"b","output":"migration patched"}}`,
		`{"type":"event_msg","payload":{"type":"mcp_tool_call_end","invocation":{"tool":"query"},"result":{"Err":"migration denied"}}}`,
		`malformed`,
	)
	filter := Filter{Harnesses: []Harness{HarnessChatGPTWork}, Project: "/project/work", Since: time.Date(2026, 9, 6, 10, 30, 0, 0, time.UTC), Until: time.Date(2026, 9, 6, 10, 30, 0, 0, time.UTC)}
	for _, test := range []struct {
		lines []int
		name  string
		query SearchQuery
	}{
		{name: "all", lines: []int{2, 4, 5, 6, 7, 8, 9}, query: SearchQuery{Pattern: regexp.MustCompile("migration")}},
		{name: "user", lines: []int{2}, query: SearchQuery{Role: RoleUser}},
		{name: "assistant", lines: []int{4}, query: SearchQuery{Role: RoleAssistant}},
		{name: "shell", lines: []int{5, 6}, query: SearchQuery{Tool: "shell"}},
		{name: "custom tool", lines: []int{7, 8}, query: SearchQuery{Tool: "apply_patch"}},
		{name: "errors", lines: []int{9}, query: SearchQuery{ErrorsOnly: true}},
		{name: "injected", query: SearchQuery{Pattern: regexp.MustCompile("injected")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := Search(context.Background(), Roots{Codex: root}, filter, test.query)

			if !report.Complete || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != codeMalformedLines || report.Diagnostics[0].Harness != HarnessChatGPTWork {
				t.Fatalf("Search diagnostics = %+v, complete = %v", report.Diagnostics, report.Complete)
			}
			var lines []int
			for _, entry := range report.Sessions {
				if entry.Harness != HarnessChatGPTWork || entry.Path != path || entry.ID != "work" {
					t.Errorf("Search session = %+v", entry.Session)
				}
				for _, hit := range entry.Hits {
					lines = append(lines, hit.Line)
					if hit.Line == 2 && (hit.Role != RoleUser || !hit.Timestamp.Equal(time.Date(2026, 9, 6, 10, 1, 0, 0, time.UTC))) {
						t.Errorf("user hit = %+v", hit)
					}
				}
			}
			if !reflect.DeepEqual(lines, test.lines) {
				t.Errorf("Search lines = %v, want %v", lines, test.lines)
			}
		})
	}
}

func TestSharedCodexRootReportsOneDiagnostic(t *testing.T) {
	roots := Roots{Codex: filepath.Join(t.TempDir(), "missing")}
	for _, harnesses := range [][]Harness{{HarnessChatGPTWork}, {HarnessCodex, HarnessChatGPTWork}} {
		report := List(context.Background(), roots, Filter{Harnesses: harnesses})

		if !report.Complete || len(report.Diagnostics) != 1 || report.Diagnostics[0].Harness != HarnessCodex || report.Diagnostics[0].Path != roots.Codex {
			t.Errorf("List(%v) = %+v, want one shared-root diagnostic", harnesses, report)
		}
	}
}
