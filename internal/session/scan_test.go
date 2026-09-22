package session

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestAdaptersStopAtRequestedEvent(t *testing.T) {
	for _, test := range []struct {
		adapter adapter
		name    string
		record  string
	}{
		{name: "claude user text", adapter: claudeAdapter{}, record: `{"type":"user","message":{"content":"one"}}`},
		{name: "claude user blocks", adapter: claudeAdapter{}, record: `{"type":"user","message":{"content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]}}`},
		{name: "claude results", adapter: claudeAdapter{}, record: `{"type":"user","message":{"content":[{"type":"tool_result","content":"one"},{"type":"tool_result","content":"two"}]}}`},
		{name: "claude assistant", adapter: claudeAdapter{}, record: `{"type":"assistant","message":{"content":[{"type":"text","text":"one"},{"type":"tool_use","name":"bash"}]}}`},
		{name: "claude tool calls", adapter: claudeAdapter{}, record: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"bash"},{"type":"text","text":"two"}]}}`},
		{name: "pi text", adapter: piAdapter{}, record: `{"type":"message","message":{"role":"user","content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]}}`},
		{name: "pi tool calls", adapter: piAdapter{}, record: `{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"bash"},{"type":"text","text":"two"}]}}`},
		{name: "pi result", adapter: piAdapter{}, record: `{"type":"message","message":{"role":"toolResult","content":[{"type":"text","text":"one"}]}}`},
		{name: "codex response", adapter: codexAdapter{}, record: `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"one"}]}}`},
		{name: "codex user", adapter: codexAdapter{}, record: `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"text":"one"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["user.text"]}}}`},
		{name: "codex call", adapter: codexAdapter{}, record: `{"type":"response_item","payload":{"type":"function_call","name":"bash"}}`},
		{name: "codex result", adapter: codexAdapter{}, record: `{"type":"response_item","payload":{"type":"function_call_output","output":"one"}}`},
		{name: "codex event", adapter: codexAdapter{}, record: `{"type":"event_msg","payload":{"type":"user_message","message":"one"}}`},
		{name: "codex error", adapter: codexAdapter{}, record: `{"type":"event_msg","payload":{"type":"mcp_tool_call_end","result":{"Err":"one"}}}`},
		{name: "cowork", adapter: coworkAdapter{}, record: `{"type":"assistant","message":{"content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			writeTranscript(t, path, time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC), test.record, "malformed after stop", test.record)
			var events []event

			malformed, err := test.adapter.scan(path, func(e event) bool {
				events = append(events, e)
				return false
			})

			if err != nil || malformed != 0 || len(events) != 1 || events[0].line != 1 {
				t.Fatalf("scan = %+v, %d malformed, %v; want only the first event", events, malformed, err)
			}
		})
	}
}

func TestPeriodSearchRetainsDecoderStateFromEarlierEvents(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, filepath.Join(root, "tools.jsonl"), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		`{"type":"session_meta","timestamp":"2026-08-20T10:00:00Z","payload":{"id":"tools"}}`,
		`{"type":"response_item","timestamp":"2026-09-13T23:59:00Z","payload":{"type":"function_call","call_id":"call","name":"shell","arguments":"retry"}}`,
		`{"type":"response_item","timestamp":"2026-09-14T00:00:00Z","payload":{"type":"function_call_output","call_id":"call","output":"retry completed"}}`,
	)

	report := Search(context.Background(), Roots{CodexSessions: root}, Filter{Since: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)}, SearchQuery{Tool: "shell"})

	if !report.Complete || len(report.Sessions) != 1 || len(report.Sessions[0].Hits) != 1 {
		t.Fatalf("search = %+v, want only the in-period tool result", report)
	}
	if hit := report.Sessions[0].Hits[0]; hit.Line != 3 || hit.Tool != "shell" {
		t.Errorf("hit = %+v, want tool name from the earlier call", hit)
	}
}

func TestCoworkPeriodRespectsEmbeddedSubagentsAndAuditTimestamp(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, filepath.Join(root, "scope", "group", "local_one", "audit.jsonl"), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		`{"type":"user","timestamp":"2026-08-20T10:00:00Z","message":{"content":"older main activity"}}`,
		`{"type":"user","timestamp":"invalid","_audit_timestamp":"2026-09-18T12:00:00Z","parent_tool_use_id":"child","message":{"content":"child activity"}}`,
	)
	for _, subagents := range []bool{false, true} {
		t.Run(fmt.Sprint(subagents), func(t *testing.T) {
			filter := Filter{IncludeSubagents: subagents, Since: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)}

			inventory := List(context.Background(), Roots{ClaudeCowork: root}, filter)
			matches := Search(context.Background(), Roots{ClaudeCowork: root}, filter, SearchQuery{Role: RoleUser})

			want := 0
			if subagents {
				want = 1
			}
			if !inventory.Complete || !matches.Complete || len(inventory.Sessions) != want || len(matches.Sessions) != want || len(inventory.Diagnostics) != 0 || len(matches.Diagnostics) != 0 {
				t.Fatalf("list = %+v, search = %+v, want %d sessions", inventory, matches, want)
			}
		})
	}
}
