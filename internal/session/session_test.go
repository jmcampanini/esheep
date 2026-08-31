package session

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func writeTranscript(t *testing.T, path string, modTime time.Time, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("Chtimes(%q): %v", path, err)
	}
}

func claudeFixture(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "-Users-u-proj", "11111111-aaaa-bbbb-cccc-222222222222.jsonl")
	writeTranscript(t, path, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC),
		`{"type":"mode","mode":"normal","sessionId":"s"}`,
		`{"type":"user","timestamp":"2026-08-20T10:00:00Z","cwd":"/Users/u/proj","gitBranch":"main","message":{"role":"user","content":"find the GOMODCACHE bug"}}`,
		`{"type":"ai-title","aiTitle":"Debug permissions","sessionId":"s"}`,
		`{"type":"assistant","timestamp":"2026-08-20T10:00:05Z","cwd":"/Users/u/proj","message":{"role":"assistant","content":[{"type":"text","text":"Looking now"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go env GOMODCACHE"}}]}}`,
		`{"type":"user","timestamp":"2026-08-20T10:00:09Z","cwd":"/Users/u/proj","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"permission denied","is_error":true}]},"toolUseResult":"Error: permission denied"}`,
		`this line is not json`,
		`{"type":"user","isMeta":true,"timestamp":"2026-08-20T10:00:10Z","cwd":"/Users/u/proj","message":{"role":"user","content":"injected context"}}`,
	)
	subagent := filepath.Join(root, "-Users-u-proj", "11111111-aaaa-bbbb-cccc-222222222222", "subagents", "agent-abc.jsonl")
	writeTranscript(t, subagent, time.Date(2026, 8, 20, 12, 5, 0, 0, time.UTC),
		`{"type":"user","timestamp":"2026-08-20T12:01:00Z","cwd":"/Users/u/proj","isSidechain":true,"message":{"role":"user","content":"subagent prompt about GOMODCACHE"}}`,
	)
	return path
}

func codexFixture(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "2026", "08", "25", "rollout-2026-08-25T09-00-00-33333333-dddd-eeee-ffff-444444444444.jsonl")
	writeTranscript(t, path, time.Date(2026, 8, 25, 9, 30, 0, 0, time.UTC),
		`{"timestamp":"2026-08-25T09:00:00Z","type":"session_meta","payload":{"id":"33333333-dddd-eeee-ffff-444444444444","timestamp":"2026-08-25T09:00:00Z","cwd":"/Users/u/codexproj","cli_version":"0.151.0","source":"exec"}}`,
		`{"timestamp":"2026-08-25T09:00:00Z","type":"turn_context","payload":{"model":"gpt-5.6","effort":"high"}}`,
		`{"timestamp":"2026-08-25T09:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<user_instructions>injected legacy context</user_instructions>"}]}}`,
		`{"timestamp":"2026-08-25T09:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the flaky test"}]}}`,
		`{"timestamp":"2026-08-25T09:00:02Z","type":"event_msg","payload":{"type":"user_message","message":"fix the flaky test"}}`,
		`{"timestamp":"2026-08-25T09:00:03Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"injected plugin context"},{"type":"input_text","text":"modern visible prompt"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["plugins.recommendations","user.text"]}}}`,
		`{"timestamp":"2026-08-25T09:00:03Z","type":"event_msg","payload":{"type":"user_message","message":"modern visible prompt"}}`,
		`{"timestamp":"2026-08-25T09:00:04Z","type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"call1","arguments":"{\"command\":[\"go\",\"test\"]}"}}`,
		`{"timestamp":"2026-08-25T09:00:07Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call1","output":[{"type":"output_text","text":"Wall time: 2 seconds\nOutput:\nFAIL"}]}}`,
		`{"timestamp":"2026-08-25T09:00:09Z","type":"event_msg","payload":{"type":"mcp_tool_call_end","invocation":{"server":"db","tool":"query"},"result":{"Err":"connection refused"}}}`,
	)
	reviewSubagent := filepath.Join(root, "2026", "08", "25", "rollout-2026-08-25T09-31-00-88888888-dddd-eeee-ffff-444444444444.jsonl")
	writeTranscript(t, reviewSubagent, time.Date(2026, 8, 25, 9, 31, 0, 0, time.UTC),
		`{"timestamp":"2026-08-25T09:31:00Z","type":"session_meta","payload":{"id":"88888888-dddd-eeee-ffff-444444444444","cwd":"/Users/u/codexproj","source":{"subagent":"review"}}}`,
	)
	guardianSubagent := filepath.Join(root, "2026", "08", "25", "rollout-2026-08-25T09-32-00-99999999-dddd-eeee-ffff-444444444444.jsonl")
	writeTranscript(t, guardianSubagent, time.Date(2026, 8, 25, 9, 32, 0, 0, time.UTC),
		`{"timestamp":"2026-08-25T09:32:00Z","type":"session_meta","payload":{"id":"99999999-dddd-eeee-ffff-444444444444","cwd":"/Users/u/codexproj","source":{"subagent":{"other":"guardian"}}}}`,
	)
	return path
}

func piFixture(t *testing.T, root string) string {
	t.Helper()
	directory := filepath.Join(root, "--Users-u-piproj--")
	path := filepath.Join(directory, "2026-08-10T08-00-00-000Z_55555555-aaaa-bbbb-cccc-666666666666.jsonl")
	writeTranscript(t, path, time.Date(2026, 8, 10, 8, 30, 0, 0, time.UTC),
		`{"type":"session","version":3,"id":"55555555-aaaa-bbbb-cccc-666666666666","timestamp":"2026-08-10T08:00:00Z","cwd":"/Users/u/piproj"}`,
		`{"type":"session_info","id":"aa","parentId":null,"timestamp":"2026-08-10T08:00:01Z","name":"Fix the suite"}`,
		`{"type":"message","id":"ab","parentId":"aa","timestamp":"2026-08-10T08:00:02Z","message":{"role":"user","timestamp":"2026-08-10T08:00:02Z","content":[{"type":"text","text":"please fix the tests"}]}}`,
		`{"type":"message","id":"ac","parentId":"ab","timestamp":"2026-08-10T08:00:05Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"text","text":"on it"},{"type":"toolCall","id":"call_1|fc_1","name":"bash","arguments":{"command":"make test"}}]}}`,
		`{"type":"message","id":"ad","parentId":"ac","timestamp":"2026-08-10T08:00:08Z","message":{"role":"toolResult","toolCallId":"call_1|fc_1","toolName":"bash","isError":true,"content":[{"type":"text","text":"make: *** [test] Error 1"}]}}`,
	)
	subagent := filepath.Join(directory, "2026-08-10T09-00-00-000Z_77777777-aaaa-bbbb-cccc-888888888888.jsonl")
	writeTranscript(t, subagent, time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC),
		`{"type":"session","version":3,"id":"77777777-aaaa-bbbb-cccc-888888888888","timestamp":"2026-08-10T09:00:00Z","cwd":"/Users/u/piproj"}`,
	)
	if err := os.WriteFile(subagent+".meta", []byte(`{"agent":"scout"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(meta sidecar): %v", err)
	}
	writeTranscript(t, filepath.Join(directory, "artifacts", "id", "notes.jsonl"), time.Now(), `{"type":"scratch"}`)
	return path
}

func fixtureRoots(t *testing.T) Roots {
	t.Helper()
	base := t.TempDir()
	roots := Roots{
		Claude: filepath.Join(base, "claude"),
		Codex:  filepath.Join(base, "codex"),
		Pi:     filepath.Join(base, "pi"),
	}
	claudeFixture(t, roots.Claude)
	codexFixture(t, roots.Codex)
	piFixture(t, roots.Pi)
	return roots
}

func TestListInventoriesMainSessionsAcrossHarnesses(t *testing.T) {
	roots := fixtureRoots(t)

	report := List(context.Background(), roots, Filter{})

	if !report.Complete {
		t.Fatalf("List complete = false, diagnostics = %+v", report.Diagnostics)
	}
	if len(report.Sessions) != 3 {
		t.Fatalf("List sessions = %d, want 3: %+v", len(report.Sessions), report.Sessions)
	}
	order := []Harness{HarnessCodex, HarnessClaude, HarnessPi}
	for index, harness := range order {
		if report.Sessions[index].Harness != harness {
			t.Errorf("sessions[%d].Harness = %q, want %q", index, report.Sessions[index].Harness, harness)
		}
	}
	claude := report.Sessions[1]
	if claude.ID != "11111111-aaaa-bbbb-cccc-222222222222" {
		t.Errorf("claude ID = %q", claude.ID)
	}
	if claude.Project != "/Users/u/proj" || claude.Title != "Debug permissions" {
		t.Errorf("claude metadata = %q %q", claude.Project, claude.Title)
	}
	if got := claude.StartedAt; !got.Equal(time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("claude StartedAt = %v", got)
	}
	codex := report.Sessions[0]
	if codex.ID != "33333333-dddd-eeee-ffff-444444444444" || codex.Project != "/Users/u/codexproj" {
		t.Errorf("codex metadata = %q %q", codex.ID, codex.Project)
	}
	pi := report.Sessions[2]
	if pi.ID != "55555555-aaaa-bbbb-cccc-666666666666" || pi.Title != "Fix the suite" {
		t.Errorf("pi metadata = %q %q", pi.ID, pi.Title)
	}
	for _, entry := range report.Sessions {
		if entry.Subagent {
			t.Errorf("session %q marked subagent in default listing", entry.Path)
		}
	}
}

func TestListFindsTitlesAfterEarlyBookkeepingRecords(t *testing.T) {
	base := t.TempDir()
	claudePath := filepath.Join(base, "claude.jsonl")
	claudeLines := []string{`{"type":"user","timestamp":"2026-08-20T10:00:00Z","cwd":"/project"}`}
	piPath := filepath.Join(base, "pi.jsonl")
	piLines := []string{`{"type":"session","id":"session-id","timestamp":"2026-08-20T10:00:00Z","cwd":"/project"}`}
	for range 50 {
		claudeLines = append(claudeLines, `{"type":"progress"}`)
		piLines = append(piLines, `{"type":"message"}`)
	}
	claudeLines = append(claudeLines, `{"type":"ai-title","aiTitle":"Late Claude title"}`)
	piLines = append(piLines, `{"type":"session_info","name":"Late Pi title"}`)
	writeTranscript(t, claudePath, time.Now(), claudeLines...)
	writeTranscript(t, piPath, time.Now(), piLines...)

	claude, claudeDiagnostics := (claudeAdapter{}).meta(transcript{path: claudePath})
	pi, piDiagnostics := (piAdapter{}).meta(transcript{path: piPath})

	if len(claudeDiagnostics) != 0 || claude.Title != "Late Claude title" {
		t.Errorf("Claude metadata = %+v, diagnostics = %+v", claude, claudeDiagnostics)
	}
	if len(piDiagnostics) != 0 || pi.Title != "Late Pi title" {
		t.Errorf("Pi metadata = %+v, diagnostics = %+v", pi, piDiagnostics)
	}
}

func TestListIncludesSubagentsOnRequest(t *testing.T) {
	roots := fixtureRoots(t)

	report := List(context.Background(), roots, Filter{IncludeSubagents: true})

	subagents := 0
	for _, entry := range report.Sessions {
		if entry.Subagent {
			subagents++
		}
	}
	if len(report.Sessions) != 7 || subagents != 4 {
		t.Fatalf("sessions = %d with %d subagents, want 7 with 4", len(report.Sessions), subagents)
	}
}

func TestListFilters(t *testing.T) {
	roots := fixtureRoots(t)
	tests := []struct {
		filter Filter
		name   string
		want   []Harness
	}{
		{name: "harness", filter: Filter{Harnesses: []Harness{HarnessPi}}, want: []Harness{HarnessPi}},
		{name: "project", filter: Filter{Project: "codexproj"}, want: []Harness{HarnessCodex}},
		{name: "since", filter: Filter{Since: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)}, want: []Harness{HarnessCodex}},
		{
			name:   "until",
			filter: Filter{Until: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)},
			want:   []Harness{HarnessPi},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := List(context.Background(), roots, test.filter)

			var got []Harness
			for _, entry := range report.Sessions {
				got = append(got, entry.Harness)
			}
			if len(got) != len(test.want) {
				t.Fatalf("List(%+v) harnesses = %v, want %v", test.filter, got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("List(%+v) harnesses = %v, want %v", test.filter, got, test.want)
				}
			}
		})
	}
}

func TestListMissingRootSkipsHarness(t *testing.T) {
	roots := fixtureRoots(t)
	roots.Codex = filepath.Join(t.TempDir(), "absent")

	report := List(context.Background(), roots, Filter{})

	if !report.Complete {
		t.Fatalf("Complete = false, want true: %+v", report.Diagnostics)
	}
	if len(report.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(report.Sessions))
	}
	found := false
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == codeRootMissing && diagnostic.Harness == HarnessCodex {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %+v, want root-missing for codex", report.Diagnostics)
	}
}

func TestSearchDecodedTextAcrossGrammars(t *testing.T) {
	roots := fixtureRoots(t)

	report := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("(?i)gomodcache")})

	if len(report.Sessions) != 1 {
		t.Fatalf("sessions with hits = %d, want 1: %+v", len(report.Sessions), report.Sessions)
	}
	hits := report.Sessions[0].Hits
	if len(hits) != 2 {
		t.Fatalf("hits = %+v, want user prompt and tool call", hits)
	}
	if hits[0].Line != 2 || hits[0].Role != RoleUser {
		t.Errorf("hits[0] = %+v, want user event on line 2", hits[0])
	}
	if hits[1].Line != 4 || hits[1].Tool != "Bash" {
		t.Errorf("hits[1] = %+v, want Bash tool call on line 4", hits[1])
	}
}

func TestSearchDecodesCodexArrayToolOutput(t *testing.T) {
	roots := fixtureRoots(t)

	report := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("FAIL")})

	if len(report.Sessions) != 1 || len(report.Sessions[0].Hits) != 1 || report.Sessions[0].Hits[0].Tool != "shell" {
		t.Fatalf("sessions = %+v, want one Codex shell output hit", report.Sessions)
	}
}

func TestSearchUsesCodexUserMessageProvenance(t *testing.T) {
	roots := fixtureRoots(t)
	for _, pattern := range []string{"fix the flaky test", "modern visible prompt"} {
		t.Run(pattern, func(t *testing.T) {
			report := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile(pattern), Role: RoleUser})

			if len(report.Sessions) != 1 || len(report.Sessions[0].Hits) != 1 || report.Sessions[0].Harness != HarnessCodex {
				t.Fatalf("sessions = %+v, want one Codex user hit", report.Sessions)
			}
		})
	}
}

func TestSearchSkipsInjectedAndMetaText(t *testing.T) {
	roots := fixtureRoots(t)

	injected := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("(?i)injected")})

	if len(injected.Sessions) != 0 {
		t.Fatalf("injected-context hits = %+v, want none", injected.Sessions)
	}
}

func TestSearchErrorsOnly(t *testing.T) {
	roots := fixtureRoots(t)

	report := Search(context.Background(), roots, Filter{}, SearchQuery{ErrorsOnly: true})

	if len(report.Sessions) != 3 {
		t.Fatalf("sessions with error hits = %d, want 3: %+v", len(report.Sessions), report.Sessions)
	}
	for _, entry := range report.Sessions {
		for _, hit := range entry.Hits {
			if !hit.Error || hit.Role != RoleTool {
				t.Errorf("%s hit = %+v, want flagged tool error", entry.Harness, hit)
			}
		}
	}
}

func TestSearchToolFilter(t *testing.T) {
	roots := fixtureRoots(t)

	report := Search(context.Background(), roots, Filter{}, SearchQuery{Tool: "shell"})

	if len(report.Sessions) != 1 || report.Sessions[0].Harness != HarnessCodex {
		t.Fatalf("sessions = %+v, want one codex session", report.Sessions)
	}
	if hits := report.Sessions[0].Hits; len(hits) != 2 {
		t.Fatalf("hits = %+v, want call and output", hits)
	}
}

func TestSearchRoleFilter(t *testing.T) {
	roots := fixtureRoots(t)

	report := Search(context.Background(), roots, Filter{}, SearchQuery{
		Pattern: regexp.MustCompile("(?i)test"),
		Role:    RoleUser,
	})

	for _, entry := range report.Sessions {
		for _, hit := range entry.Hits {
			if hit.Role != RoleUser {
				t.Errorf("hit = %+v, want only user events", hit)
			}
		}
	}
}

func TestSearchRawMatchesUndecodedLines(t *testing.T) {
	roots := fixtureRoots(t)

	report := Search(context.Background(), roots, Filter{}, SearchQuery{
		Pattern: regexp.MustCompile(`"tool_use_id"`),
		Raw:     true,
	})

	if len(report.Sessions) != 1 || report.Sessions[0].Harness != HarnessClaude {
		t.Fatalf("sessions = %+v, want one claude session", report.Sessions)
	}
	if hit := report.Sessions[0].Hits[0]; hit.Role != RoleRaw || hit.Line != 5 {
		t.Fatalf("hit = %+v, want raw hit on line 5", hit)
	}
}

func TestSearchReportsMalformedLinesWithoutFailing(t *testing.T) {
	roots := fixtureRoots(t)

	report := Search(context.Background(), roots, Filter{}, SearchQuery{Pattern: regexp.MustCompile("(?i)gomodcache")})

	if !report.Complete {
		t.Fatalf("Complete = false, want true: %+v", report.Diagnostics)
	}
	found := false
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == codeMalformedLines && diagnostic.Harness == HarnessClaude {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %+v, want malformed-lines for the claude fixture", report.Diagnostics)
	}
}

func TestParseTimeFlag(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		value   string
		want    time.Time
		wantErr bool
	}{
		{value: "7d", want: now.AddDate(0, 0, -7)},
		{value: "36h", want: now.Add(-36 * time.Hour)},
		{value: "2026-08-01", want: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
		{value: "yesterday", wantErr: true},
		{value: "-24h", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := ParseTimeFlag(test.value, now)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseTimeFlag(%q) = %v, want error", test.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTimeFlag(%q): %v", test.value, err)
			}
			if !got.Equal(test.want) {
				t.Fatalf("ParseTimeFlag(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestParseHarnessAndRoleRejectUnknownNames(t *testing.T) {
	if _, err := ParseHarness("emacs"); err == nil {
		t.Error("ParseHarness(emacs) succeeded, want error")
	}
	if _, err := ParseRole("system"); err == nil {
		t.Error("ParseRole(system) succeeded, want error")
	}
}

func TestExcerptWindowsAroundMatch(t *testing.T) {
	text := strings.Repeat("界", 167) + " NEEDLE " + strings.Repeat("界", 167)
	pattern := regexp.MustCompile("NEEDLE")

	got := excerpt(text, pattern.FindStringIndex(text))

	if !utf8.ValidString(got) || !strings.Contains(got, "NEEDLE") {
		t.Fatalf("excerpt = %q, want valid UTF-8 containing the match", got)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("excerpt = %q, want ellipses marking truncation", got)
	}
	if len(got) > 200 {
		t.Fatalf("excerpt length = %d, want a bounded window", len(got))
	}
}
