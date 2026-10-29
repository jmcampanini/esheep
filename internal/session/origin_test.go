package session

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSearchClassifiesEventsAndRecordedRelationships(t *testing.T) {
	claude := []string{
		`{"type":"user","message":{"content":"needle command quoted"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"shell","input":{"command":"needle a"}},{"type":"tool_use","id":"b","name":"read","input":{"file":"needle b"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b","content":"needle b result"},{"type":"tool_result","tool_use_id":"a","content":"needle a result","is_error":true}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"unnamed","input":{"command":"needle"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"needle missing ID"},{"type":"tool_result","tool_use_id":"orphan","content":"needle orphan"}]}}`,
	}
	codex := []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"needle command quoted"}}`,
		`{"type":"response_item","payload":{"type":"function_call","call_id":"a","name":"shell","arguments":"needle a"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","call_id":"b","name":"read","input":"needle b"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"b","output":"needle b result"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"a","output":"needle a result"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"unnamed","arguments":"needle"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","output":"needle missing ID"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"orphan","output":"needle orphan"}}`,
	}
	pi := []string{
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"needle command quoted"}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"a","name":"shell","arguments":{"command":"needle a"}},{"type":"toolCall","id":"b","name":"read","arguments":{"file":"needle b"}}]}}`,
		`{"type":"message","message":{"role":"toolResult","toolCallId":"b","toolName":"read","content":[{"text":"needle b result"}]}}`,
		`{"type":"message","message":{"role":"toolResult","toolCallId":"a","toolName":"shell","isError":true,"content":[{"text":"needle a result"}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","name":"unnamed","arguments":{"command":"needle"}}]}}`,
		`{"type":"message","message":{"role":"toolResult","content":[{"text":"needle missing ID"}]}}`,
		`{"type":"message","message":{"role":"toolResult","toolCallId":"orphan","content":[{"text":"needle orphan"}]}}`,
	}
	for _, harness := range []Harness{HarnessClaude, HarnessClaudeCowork, HarnessCodex, HarnessChatGPTWork, HarnessPi} {
		t.Run(string(harness), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "events.jsonl")
			var roots Roots
			var records []string
			switch harness {
			case HarnessClaude:
				roots.Claude, records = root, claude
			case HarnessClaudeCowork:
				roots.ClaudeCowork, records = root, claude
				path = filepath.Join(root, "scope", "group", "local_test", "audit.jsonl")
			case HarnessCodex, HarnessChatGPTWork:
				roots.CodexSessions = root
				originator := "codex_cli_rs"
				if harness == HarnessChatGPTWork {
					originator = "codex_work_desktop"
				}
				records = append([]string{fmt.Sprintf(`{"type":"session_meta","payload":{"id":"events","originator":%q,"thread_source":"user"}}`, originator)}, codex...)
			case HarnessPi:
				roots.Pi = root
				records = append([]string{`{"type":"session","id":"events"}`}, pi...)
			}
			writeTranscript(t, path, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), records...)
			before := sessionTreeSnapshot(t, root)

			report := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("needle")})
			if !report.Complete || len(report.Sessions) != 1 || len(report.Sessions[0].Hits) != 8 {
				t.Fatalf("Search() = %+v, want eight classified hits", report)
			}
			wantKinds := []EventKind{KindMessage, KindToolCall, KindToolCall, KindToolResult, KindToolResult, KindToolCall, KindToolResult, KindToolResult}
			wantIDs := []string{"", "a", "b", "b", "a", "", "", "orphan"}
			wantTools := []string{"", "shell", "read", "read", "shell", "unnamed", "", ""}
			for i, hit := range report.Sessions[0].Hits {
				wantOrigin := EventOriginTool
				if i == 0 {
					wantOrigin = EventOriginUnknown
				} else if wantKinds[i] == KindToolCall {
					wantOrigin = EventOriginAgent
				}
				if hit.Kind != wantKinds[i] || hit.CallID != wantIDs[i] || hit.Tool != wantTools[i] || hit.Origin.Kind != wantOrigin || hit.ParentCallID != "" {
					t.Errorf("hit[%d] = %+v, want %s id=%q tool=%q origin=%s in main scope", i, hit, wantKinds[i], wantIDs[i], wantTools[i], wantOrigin)
				}
			}
			filtered := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("a result"), Tool: "shell"})
			if len(filtered.Sessions) != 1 || len(filtered.Sessions[0].Hits) != 1 || filtered.Sessions[0].Hits[0].CallID != "a" {
				t.Fatalf("filtered Search() = %+v, want call relationship from unmatched call", filtered)
			}
			_ = List(context.Background(), roots, Filter{})
			if after := sessionTreeSnapshot(t, root); !reflect.DeepEqual(after, before) {
				t.Error("list/search changed transcript inputs")
			}
		})
	}
}

func TestSessionOriginsUseOnlyRecordedInitiation(t *testing.T) {
	for _, test := range []struct {
		name     string
		metadata string
		want     OriginKind
	}{
		{name: "missing", metadata: `{}`, want: OriginUnknown},
		{name: "exec alone", metadata: `{"source":"exec","originator":"codex_work_desktop"}`, want: OriginUnknown},
		{name: "cli alone", metadata: `{"source":"cli"}`, want: OriginUnknown},
		{name: "recorded user", metadata: `{"source":"exec","thread_source":"user"}`, want: OriginHuman},
		{name: "automation", metadata: `{"thread_source":"automation"}`, want: OriginAutomation},
		{name: "memory", metadata: `{"thread_source":"memory_consolidation"}`, want: OriginAgent},
		{name: "internal wins", metadata: `{"source":{"internal":"guardian"},"thread_source":"user"}`, want: OriginAgent},
		{name: "subagent wins", metadata: `{"source":{"subagent":{"other":"guardian"}},"thread_source":"user"}`, want: OriginSubagent},
		{name: "subagent label only", metadata: `{"thread_source":"subagent"}`, want: OriginUnknown},
		{name: "guardian label only", metadata: `{"thread_source":"guardian_review"}`, want: OriginUnknown},
		{name: "future", metadata: `{"source":{"future":{"mode":"background"}},"thread_source":"future"}`, want: OriginUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "session.jsonl")
			writeTranscript(t, path, time.Now(), `{"type":"session_meta","payload":`+test.metadata+`}`,
				`{"type":"event_msg","payload":{"type":"user_message","message":"later user message"}}`)

			report := Search(context.Background(), Roots{CodexSessions: root}, Filter{IncludeSubagents: true}, SearchQuery{Pattern: regexp.MustCompile("later")})

			if len(report.Sessions) != 1 {
				t.Fatalf("Search() = %+v", report)
			}
			entry := report.Sessions[0]
			if entry.Origin.Kind != test.want || entry.Subagent != (test.want == OriginSubagent) || entry.Hits[0].Origin.Kind != EventOriginUnknown {
				t.Errorf("Search() = %+v, want session origin %s independent of unknown event origin", entry, test.want)
			}
			if test.name == "future" && entry.Origin.Evidence["source.future.mode"] != "background" {
				t.Errorf("origin evidence = %+v, want unfamiliar source preserved", entry.Origin.Evidence)
			}
		})
	}
}

func TestMalformedCodexOriginatorCannotSupplyMetadata(t *testing.T) {
	root := t.TempDir()
	writeTranscript(t, filepath.Join(root, "fallback.jsonl"), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		`{"type":"session_meta","payload":{"id":"untrusted","originator":7,"cwd":"/untrusted","source":{"subagent":"review"}}}`)

	report := List(context.Background(), Roots{CodexSessions: root}, Filter{})

	if !report.Complete || len(report.Sessions) != 1 {
		t.Fatalf("List() = %+v, want one session without usable header metadata", report)
	}
	entry := report.Sessions[0]
	if entry.ID != "fallback" || len(entry.Projects) != 0 || entry.Subagent || entry.Origin.Kind != OriginUnknown || len(entry.Origin.Evidence) != 0 {
		t.Errorf("List() metadata = %+v, want filename ID and unknown origin", entry)
	}
}

func TestClaudeAndCoworkEventOriginEvidence(t *testing.T) {
	for _, test := range []struct {
		metadata string
		want     EventOriginKind
	}{
		{metadata: `"origin":{"kind":"human"},"promptSource":"typed"`, want: EventOriginHuman},
		{metadata: `"origin":{"kind":"task-notification"}`, want: EventOriginHarness},
		{metadata: `"origin":{"kind":"peer","body":"secret body"}`, want: EventOriginAgent},
		{metadata: `"origin":{"kind":"coordinator"}`, want: EventOriginAgent},
		{metadata: `"turnOrigin":"human"`, want: EventOriginHuman},
		{metadata: `"turnOrigin":"peer"`, want: EventOriginAgent},
		{metadata: `"turnOrigin":"task_notification"`, want: EventOriginHarness},
		{metadata: `"origin":null,"turnOrigin":"human"`, want: EventOriginHuman},
		{metadata: `"origin":{},"turnOrigin":"human"`, want: EventOriginUnknown},
		{metadata: `"origin":"malformed","turnOrigin":"human"`, want: EventOriginUnknown},
		{metadata: `"origin":{"kind":"human"},"turnOrigin":"peer"`, want: EventOriginUnknown},
		{metadata: `"origin":{"kind":"human"},"turnOrigin":"sdk"`, want: EventOriginUnknown},
		{metadata: `"origin":{"kind":"future"},"promptSource":"typed"`, want: EventOriginUnknown},
		{metadata: `"promptSource":"typed"`, want: EventOriginUnknown},
	} {
		for _, harness := range []Harness{HarnessClaude, HarnessClaudeCowork} {
			t.Run(string(harness)+"/"+test.metadata, func(t *testing.T) {
				root := t.TempDir()
				path, roots := filepath.Join(root, "events.jsonl"), Roots{Claude: root}
				if harness == HarnessClaudeCowork {
					path, roots = filepath.Join(root, "s", "g", "local_test", "audit.jsonl"), Roots{ClaudeCowork: root}
				}
				writeTranscript(t, path, time.Now(),
					`{"type":"user","message":{"content":"earlier unknown prompt"}}`,
					`{"type":"user",`+test.metadata+`,"message":{"content":[{"type":"text","text":"needle message"},{"type":"tool_result","tool_use_id":"a","content":"needle result"}]}}`)

				report := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("needle")})

				if len(report.Sessions) != 1 || len(report.Sessions[0].Hits) != 2 {
					t.Fatalf("Search() = %+v", report)
				}
				entry := report.Sessions[0]
				message, result := entry.Hits[0], entry.Hits[1]
				if entry.Origin.Kind != OriginUnknown || message.Origin.Kind != test.want || message.Role != RoleUser || result.Origin.Kind != EventOriginTool || len(result.Origin.Evidence) != 0 {
					t.Errorf("Search() = %+v, want independent origins unknown, %s, tool", entry, test.want)
				}
				encoded, err := json.Marshal(message.Origin)
				if err != nil || strings.Contains(string(encoded), "secret body") || strings.Contains(string(encoded), "origin.body") {
					t.Errorf("origin JSON = %s (%v), must not include peer body", encoded, err)
				}
			})
		}
	}
}

func TestOriginEvidenceBounds(t *testing.T) {
	metadata := map[string]json.RawMessage{
		"originator": json.RawMessage(`null`),
		"source":     json.RawMessage(`{"n":1234567890123456789,"null":null,"bool":true,"array":["ignored"],"deep":{"a":{"b":{"c":"kept","d":{"e":"omitted"}}}}}`),
	}
	want := map[string]string{"source.n": "1234567890123456789", "source.deep.a.b.c": "kept"}
	if got := originEvidence(metadata); !reflect.DeepEqual(got, want) {
		t.Errorf("originEvidence() = %+v, want %+v", got, want)
	}

	values := make(map[string]string)
	for i := range 40 {
		values[fmt.Sprintf("k%02d", i)] = strings.Repeat("界", 90)
	}
	values[strings.Repeat("z", 257)] = "ignored"
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	got := originEvidence(map[string]json.RawMessage{"source": raw})
	if len(got) != 32 || got["source.k31"] == "" || got["source.k32"] != "" {
		t.Errorf("evidence = %+v, want first 32 sorted entries", got)
	}
	for key, value := range got {
		if len(key) > 256 || len(value) > 256 || !utf8.ValidString(value) || !strings.HasSuffix(value, "...") {
			t.Errorf("evidence[%q] = %q, want bounded UTF-8 value", key, value)
		}
	}
	boundary := originEvidence(map[string]json.RawMessage{
		"source": json.RawMessage(`{"` + strings.Repeat("k", 256) + `":"omitted","n":-1.5e3,"control":"esc\u001b[31m","at_limit":"` + strings.Repeat("a", 256) + `"}`),
	})
	boundaryWant := map[string]string{"source.n": "-1.5e3", "source.control": "esc\x1b[31m", "source.at_limit": strings.Repeat("a", 256)}
	if !reflect.DeepEqual(boundary, boundaryWant) {
		t.Errorf("boundary evidence = %q, want %q", boundary, boundaryWant)
	}
}

func TestClaudeEvidencePreservesScalarsWithoutOriginBodies(t *testing.T) {
	for _, test := range []struct {
		metadata string
		want     map[string]string
	}{
		{metadata: `{"origin":{"kind":{"nested":"human"},"body":"excluded"}}`},
		{metadata: `{"origin":{"kind":1e400},"turnOrigin":"sdk"}`, want: map[string]string{"origin.kind": "1e400", "turnOrigin": "sdk"}},
		{metadata: `{"origin":{"kind":"human"},"turnOrigin":"peer","promptSource":"queued"}`, want: map[string]string{"origin.kind": "human", "turnOrigin": "peer", "promptSource": "queued"}},
		{metadata: `{"promptSource":{"future":"source"},"turnOrigin":{"future":42}}`, want: map[string]string{"promptSource.future": "source", "turnOrigin.future": "42"}},
	} {
		t.Run(test.metadata, func(t *testing.T) {
			var metadata claudeOriginMetadata
			if err := json.Unmarshal([]byte(test.metadata), &metadata); err != nil {
				t.Fatal(err)
			}

			origin := metadata.eventOrigin()

			if origin.Kind != EventOriginUnknown || !maps.Equal(origin.Evidence, test.want) {
				t.Errorf("eventOrigin() = %+v, want unknown with evidence %v", origin, test.want)
			}
		})
	}
}

func TestEvidenceTruncationDoesNotChangeClassification(t *testing.T) {
	root := t.TempDir()
	label := strings.Repeat("x", 300)
	writeTranscript(t, filepath.Join(root, "rollout.jsonl"), time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
		`{"type":"session_meta","payload":{"id":"one","thread_source":"user","source":{"internal":"`+label+`"}}}`,
	)

	list := List(context.Background(), Roots{CodexSessions: root}, Filter{})

	if len(list.Sessions) != 1 || list.Sessions[0].Origin.Kind != OriginAgent || len(list.Sessions[0].Origin.Evidence["source.internal"]) != 256 {
		t.Errorf("List = %+v, want an agent origin with truncated evidence", list.Sessions)
	}
}
