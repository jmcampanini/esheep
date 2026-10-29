package session

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestCoworkRelationshipsRetainEachChildScope(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "s", "g", "local_test", "audit.jsonl")
	var records []string
	for _, parent := range []string{"", "child-a", "child-b"} {
		records = append(records,
			fmt.Sprintf(`{"type":"assistant","parent_tool_use_id":%q,"message":{"content":[{"type":"tool_use","id":"same","name":"shell","input":{"command":"needle"}}]}}`, parent),
			fmt.Sprintf(`{"type":"user","parent_tool_use_id":%q,"message":{"content":"needle child prompt"}}`, parent))
	}
	for _, parent := range []string{"child-b", "child-a", ""} {
		records = append(records, fmt.Sprintf(`{"type":"user","parent_tool_use_id":%q,"message":{"content":[{"type":"tool_result","tool_use_id":"same","content":"needle output"}]}}`, parent))
	}
	writeTranscript(t, path, time.Now(), records...)

	for _, children := range []bool{false, true} {
		report := Search(context.Background(), Roots{ClaudeCowork: root}, Filter{IncludeSubagents: children}, SearchQuery{Pattern: regexp.MustCompile("needle")})
		want := 3
		if children {
			want = 9
		}
		if len(report.Sessions) != 1 || len(report.Sessions[0].Hits) != want {
			t.Fatalf("Search(subagents=%t) = %+v, want %d hits", children, report, want)
		}
		calls := make(map[string]int)
		results := make(map[string]int)
		for _, hit := range report.Sessions[0].Hits {
			expectedParent := map[int]string{3: "child-a", 4: "child-a", 5: "child-b", 6: "child-b", 7: "child-b", 8: "child-a"}[hit.Line]
			if hit.ParentCallID != expectedParent {
				t.Errorf("hit = %+v, want scope %q", hit, expectedParent)
			}
			switch hit.Kind {
			case KindToolCall:
				calls[hit.ParentCallID+"/"+hit.CallID]++
			case KindToolResult:
				results[hit.ParentCallID+"/"+hit.CallID]++
			}
		}
		for key, count := range calls {
			if count != 1 || results[key] != 1 {
				t.Errorf("scope/call %q = %d calls, %d results, want one each", key, count, results[key])
			}
		}
	}
}

func TestCodexMCPResultsAndRawHits(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, filepath.Join(root, "events.jsonl"), time.Now(),
		`{"type":"session_meta","payload":{"id":"test","thread_source":"automation"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"mcp__shell","call_id":"a","arguments":"needle"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"a","output":"needle output"}}`,
		`{"type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":"a","invocation":{"tool":"shell"},"result":{"Err":"needle error"}}}`)
	roots := Roots{CodexSessions: root}

	decoded := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("needle")})
	if len(decoded.Sessions) != 1 || len(decoded.Sessions[0].Hits) != 3 {
		t.Fatalf("Search() = %+v", decoded)
	}
	for _, hit := range decoded.Sessions[0].Hits {
		if hit.CallID != "a" {
			t.Errorf("hit = %+v, want shared call ID", hit)
		}
	}
	last := decoded.Sessions[0].Hits[2]
	if last.Kind != KindToolResult || last.Origin.Kind != EventOriginTool || !last.Error {
		t.Errorf("MCP result = %+v, want failed tool result", last)
	}

	raw := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("needle"), Raw: true})
	if len(raw.Sessions) != 1 || len(raw.Sessions[0].Hits) != 3 {
		t.Fatalf("raw Search() = %+v", raw)
	}
	for _, hit := range raw.Sessions[0].Hits {
		if hit.Kind != KindRaw || hit.Role != RoleRaw || hit.Origin.Kind != EventOriginUnknown || hit.CallID != "" || hit.ParentCallID != "" {
			t.Errorf("raw hit = %+v, want undecoded origin and relationship", hit)
		}
	}
}

func TestInjectedContentDoesNotQualifySessionsOrPeriods(t *testing.T) {
	for _, harness := range []Harness{HarnessChatGPTWork, HarnessClaudeCowork} {
		t.Run(string(harness), func(t *testing.T) {
			root := t.TempDir()
			path, roots := filepath.Join(root, "session.jsonl"), Roots{CodexSessions: root}
			records := []string{
				`{"type":"session_meta","payload":{"originator":"codex_work_desktop"}}`,
				`{"type":"response_item","timestamp":"2026-09-01T12:00:00Z","payload":{"type":"message","role":"user","content":[{"text":"injected"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["harness.context"]}}}`,
			}
			if harness == HarnessClaudeCowork {
				path, roots = filepath.Join(root, "s", "g", "local_test", "audit.jsonl"), Roots{ClaudeCowork: root}
				records = []string{
					`{"type":"user","timestamp":"2026-09-01T12:00:00Z","isMeta":true,"origin":{"kind":"coordinator"},"message":{"content":"injected"}}`,
					`{"type":"user","timestamp":"2026-09-01T12:00:00Z","isSynthetic":true,"message":{"content":"injected"}}`,
				}
			}
			writeTranscript(t, path, time.Now(), records...)

			for _, filter := range []Filter{{}, {Since: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}} {
				list := List(context.Background(), roots, filter)
				search := Search(context.Background(), roots, filter, SearchQuery{Pattern: regexp.MustCompile("injected")})
				if !list.Complete || !search.Complete || len(list.Sessions) != 0 || len(search.Sessions) != 0 {
					t.Errorf("injected-only list=%+v search=%+v, want complete empty reports", list, search)
				}
			}
		})
	}
}
