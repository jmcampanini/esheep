package remote

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jmcampanini/esheep/internal/config"
	"github.com/jmcampanini/esheep/internal/session"
)

var fleet = []config.ResolvedMachine{
	{Command: "esheep", Host: "nas", Name: "nas", Timeout: time.Minute},
	{Command: "/opt/homebrew/bin/esheep", Host: "javier@studio.example", Name: "Studio", Timeout: 2 * time.Minute},
	{Command: "esheep", Host: "mba", Name: "mba", Timeout: time.Minute},
}

func TestParseNamesRejectsShapesBeforeConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		noLocal bool
		values  []string
		want    string
	}{
		{name: "no-local alone", noLocal: true, want: "--no-local requires --remote"},
		{name: "all with names", values: []string{"nas", "ALL"}, want: "--remote all cannot combine"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseNames(test.values, test.noLocal)

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ParseNames(%q, %t) error = %v, want containing %q", test.values, test.noLocal, err, test.want)
			}
		})
	}
}

func TestSelectResolvesNamesAgainstConfiguredMachines(t *testing.T) {
	tests := []struct {
		hostname      string
		name          string
		noLocal       bool
		values        []string
		wantErr       string
		wantLocal     bool
		wantLocalName string
		wantMachines  []string
	}{
		{name: "local only", hostname: "laptop.local", wantLocal: true, wantLocalName: "laptop"},
		{name: "named in configuration order", hostname: "laptop", values: []string{"mba", "NAS", "mba"}, wantLocal: true, wantLocalName: "laptop", wantMachines: []string{"nas", "mba"}},
		{name: "no local", hostname: "laptop", noLocal: true, values: []string{"studio"}, wantLocalName: "laptop", wantMachines: []string{"Studio"}},
		{name: "all skips self by first label", hostname: "STUDIO.tail1234.ts.net", values: []string{"all"}, wantLocal: true, wantLocalName: "Studio", wantMachines: []string{"nas", "mba"}},
		{name: "unknown machine", hostname: "laptop", values: []string{"laptop9"}, wantErr: `unknown machine "laptop9" (configured: nas, Studio, mba)`},
		{name: "own hostname", hostname: "nas.home", values: []string{"nas"}, wantErr: `machine "nas" is this machine`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			names, err := ParseNames(test.values, test.noLocal)
			if err != nil {
				t.Fatal(err)
			}

			selection, diagnostics, err := names.Select(fleet, test.hostname)

			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Select() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Select() error = %v", err)
			}
			if len(diagnostics) != 0 {
				t.Errorf("diagnostics = %+v, want none", diagnostics)
			}
			if selection.Local != test.wantLocal || selection.LocalName != test.wantLocalName {
				t.Errorf("local = %t %q, want %t %q", selection.Local, selection.LocalName, test.wantLocal, test.wantLocalName)
			}
			var got []string
			for _, machine := range selection.Machines {
				got = append(got, machine.Name)
			}
			if strings.Join(got, ",") != strings.Join(test.wantMachines, ",") {
				t.Errorf("machines = %v, want %v", got, test.wantMachines)
			}
		})
	}
}

func TestSelectAllWithNoOtherMachine(t *testing.T) {
	names, err := ParseNames([]string{"all"}, false)
	if err != nil {
		t.Fatal(err)
	}

	selection, diagnostics, err := names.Select(nil, "laptop")

	if err != nil {
		t.Fatal(err)
	}
	if !selection.Local || len(selection.Machines) != 0 {
		t.Errorf("selection = %+v, want local only", selection)
	}
	if len(diagnostics) != 1 || diagnostics[0].Code != CodeNoMachines || diagnostics[0].Machine != "laptop" || !strings.Contains(diagnostics[0].Message, "[[machines]]") {
		t.Errorf("diagnostics = %+v, want one no-machines diagnostic naming [[machines]]", diagnostics)
	}

	noLocal, err := ParseNames([]string{"all"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := noLocal.Select(fleet[:1], "nas"); err == nil || !strings.Contains(err.Error(), "nothing to search") {
		t.Errorf("Select() with --no-local and only self error = %v, want nothing to search", err)
	}
}

// fakeSSH writes a shell script that behaves according to the host it is
// asked to reach and records the argument list and stdin it received.
func fakeSSH(t *testing.T) (ssh string, recordDir string) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "$@" > "$RECORD_DIR/args"
cat > "$RECORD_DIR/stdin"
host="$6"
case "$host" in
classification)
  printf '%s' "$CLASSIFICATION_REPLY" ;;
ok)
  printf '%s' '{"complete":true,"sessions":[{"origin":{"kind":"unknown"},"harness":"claude","id":"remote-1","machine":"ignored","modified_at":"2026-09-10T10:00:00Z","path":"/home/j/.claude/a.jsonl","projects":[],"started_at":"2026-09-10T10:00:00Z","subagent":false,"hits":[{"kind":"message","origin":{"kind":"unknown"},"line":3,"role":"user","excerpt":"timeout"}]}],"future_field":1}'
  echo '/home/j/.pi [pi]: root-missing: skipped' >&2 ;;
failed|silent|disconnected|timed-out)
  printf '%s' '{"complete":true,"sessions":[{"origin":{"kind":"unknown"},"id":"retained","path":"/home/j/session.jsonl","hits":[{"kind":"message","origin":{"kind":"unknown"},"line":3,"role":"user","excerpt":"timeout"}]}]}'
  case "$host" in
    failed) echo 'esheep: failed after writing results' >&2; exit 1 ;;
    silent) exit 1 ;;
    disconnected) echo 'ssh: connection lost' >&2; exit 255 ;;
    timed-out) echo 'still waiting for the remote process' >&2; sleep 5 ;;
  esac ;;
warnings)
  printf '%s' '{"complete":true,"sessions":[]}'
  printf '%s' "$REMOTE_STDERR" >&2 ;;
down)
  echo "ssh: connect to host down port 22: Connection refused" >&2
  exit 255 ;;
slow)
  sleep 5
  exit 0 ;;
banner)
  echo 'shell startup warning: before stdout' >&2
  echo "Welcome to the shell"
  printf '%s' '{"complete":true}' ;;
invalid-field)
  printf '%s' '{"complete":true,"sessions":[{"origin":{"kind":"unknown"},"id":"invalid","started_at":"not a timestamp"}]}' ;;
missing)
  echo "fish: Unknown command: esheep" >&2
  exit 127 ;;
esac
`
	ssh = filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	recordDir = filepath.Join(dir, "record")
	if err := os.Mkdir(recordDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECORD_DIR", recordDir)
	return ssh, recordDir
}

func machine(name, host string, timeout time.Duration) config.ResolvedMachine {
	return config.ResolvedMachine{Command: "/opt/homebrew/bin/esheep", Host: host, Name: name, Timeout: timeout}
}

func TestSearchMergesLocalAndRemoteReplies(t *testing.T) {
	ssh, recordDir := fakeSSH(t)
	since := time.Date(2026, 9, 6, 21, 47, 6, 0, time.UTC)
	request := session.Request{Filter: session.RequestFilter{ArchiveState: "all", Since: &since}, Mode: session.ModeSearch, Query: session.RequestQuery{Pattern: "timeout"}}
	local := func(context.Context) session.SearchReport {
		return session.SearchReport{Complete: true, Period: session.Period{Since: &since}, Sessions: []session.Match{
			{Session: session.Session{Harness: session.HarnessPi, ID: "local-old", Path: "/Users/j/.pi/old.jsonl", StartedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}},
			{Session: session.Session{Harness: session.HarnessPi, ID: "local-tie", Path: "/Users/j/.pi/tie.jsonl", StartedAt: time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)}},
		}}
	}
	selection := Selection{Local: true, LocalName: "laptop", Machines: []config.ResolvedMachine{machine("nas", "ok", time.Minute)}}

	report, err := Search(context.Background(), ssh, selection, request, local)

	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete {
		t.Errorf("complete = false, want a complete report despite the nonfatal remote warning")
	}
	if report.Period.Since == nil || !report.Period.Since.Equal(since) || report.Period.Until != nil {
		t.Errorf("period = %+v, want the local resolved period", report.Period)
	}
	var ids []string
	for _, match := range report.Sessions {
		ids = append(ids, match.Machine+":"+match.ID)
	}
	if got := strings.Join(ids, " "); got != "laptop:local-tie nas:remote-1 laptop:local-old" {
		t.Errorf("merged order = %q, want time descending with machine tiebreak", got)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Machine != "nas" || report.Diagnostics[0].Code != CodeDiagnostic || report.Diagnostics[0].Message != "/home/j/.pi [pi]: root-missing: skipped" {
		t.Errorf("diagnostics = %+v, want the remote diagnostic stamped nas", report.Diagnostics)
	}
	if report.Sessions[1].Path != "/home/j/.claude/a.jsonl" || len(report.Sessions[1].Hits) != 1 {
		t.Errorf("remote match = %+v, want remote path and hits preserved", report.Sessions[1])
	}

	args, err := os.ReadFile(filepath.Join(recordDir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := "-T\n-o\nBatchMode=yes\n-o\nConnectTimeout=10\nok\n/opt/homebrew/bin/esheep sessions query\n"
	if string(args) != wantArgs {
		t.Errorf("ssh args = %q, want %q", args, wantArgs)
	}
	stdin, err := os.ReadFile(filepath.Join(recordDir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	var received session.Request
	if err := json.Unmarshal(stdin, &received); err != nil {
		t.Fatalf("decode forwarded request: %v\n%s", err, stdin)
	}
	if received.Mode != session.ModeSearch || received.Query.Pattern != "timeout" || received.Filter.Since == nil || !received.Filter.Since.Equal(since) {
		t.Errorf("forwarded request = %+v", received)
	}
}

func TestListWithoutMachinesStampsLocalResults(t *testing.T) {
	local := func(context.Context) session.ListReport {
		return session.ListReport{Complete: false, Diagnostics: []session.Diagnostic{{Code: "walk", Path: "/roots"}}, Sessions: []session.Session{{ID: "one"}}}
	}

	report, err := List(context.Background(), "", Selection{Local: true, LocalName: "laptop"}, session.Request{Mode: session.ModeList}, local)

	if err != nil {
		t.Fatal(err)
	}
	if report.Complete || len(report.Sessions) != 1 || report.Sessions[0].Machine != "laptop" || report.Diagnostics[0].Machine != "laptop" {
		t.Errorf("report = %+v, want local results stamped laptop and incompleteness preserved", report)
	}
}

func TestRemoteClassificationValidation(t *testing.T) {
	ssh, _ := fakeSSH(t)
	for _, test := range []struct {
		name string
		edit func(map[string]any, map[string]any)
	}{
		{name: "valid"},
		{name: "missing session origin", edit: func(s, _ map[string]any) { delete(s, "origin") }},
		{name: "invalid session origin", edit: func(s, _ map[string]any) { s["origin"] = map[string]any{"kind": "tool"} }},
		{name: "inconsistent subagent", edit: func(s, _ map[string]any) { s["subagent"] = true }},
		{name: "missing hit kind", edit: func(_, h map[string]any) { delete(h, "kind") }},
		{name: "invalid hit kind", edit: func(_, h map[string]any) { h["kind"] = "future" }},
		{name: "missing event origin", edit: func(_, h map[string]any) { delete(h, "origin") }},
		{name: "invalid event origin", edit: func(_, h map[string]any) { h["origin"] = map[string]any{"kind": "automation"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			hit := map[string]any{"kind": "tool_result", "role": "tool", "call_id": "call", "parent_call_id": "parent", "origin": map[string]any{"kind": "tool"}, "line": 3, "excerpt": "output"}
			entry := map[string]any{"id": "remote", "path": "/remote/events.jsonl", "origin": map[string]any{"kind": "automation", "evidence": map[string]string{"thread_source": "automation"}}, "hits": []any{hit}}
			if test.edit != nil {
				test.edit(entry, hit)
			}
			encoded, err := json.Marshal(map[string]any{"complete": true, "sessions": []any{entry}})
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("CLASSIFICATION_REPLY", string(encoded))
			selection := Selection{Machines: []config.ResolvedMachine{machine("remote", "classification", time.Minute)}}

			report, err := Search(context.Background(), ssh, selection, session.Request{Mode: session.ModeSearch}, nil)

			if err != nil {
				t.Fatal(err)
			}
			if test.edit == nil {
				if !report.Complete || len(report.Sessions) != 1 || report.Sessions[0].Origin.Kind != session.OriginAutomation || report.Sessions[0].Origin.Evidence["thread_source"] != "automation" || report.Sessions[0].Hits[0].CallID != "call" || report.Sessions[0].Hits[0].ParentCallID != "parent" {
					t.Fatalf("Search() = %+v, want classification and IDs preserved", report)
				}
			} else if report.Complete || len(report.Sessions) != 0 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != CodeReply {
				t.Fatalf("Search() = %+v, want machine-reply and rejected payload", report)
			}
		})
	}
}

func TestRemoteOnlyPeriodComesFromRequest(t *testing.T) {
	ssh, _ := fakeSSH(t)
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	until := since.Add(25 * time.Hour)
	for _, host := range []string{"ok", "down"} {
		t.Run(host, func(t *testing.T) {
			request := session.Request{Mode: session.ModeList, Filter: session.RequestFilter{Since: &since, Until: &until}}
			selection := Selection{Machines: []config.ResolvedMachine{machine("nas", host, time.Minute)}}

			report, err := List(context.Background(), ssh, selection, request, nil)

			if err != nil || report.Period.Since == nil || !report.Period.Since.Equal(since) || report.Period.Until == nil || !report.Period.Until.Equal(until) {
				t.Fatalf("period = %+v, error = %v; want request bounds even on failure", report.Period, err)
			}
		})
	}
}

func TestQueryFailuresBecomeDiagnostics(t *testing.T) {
	ssh, _ := fakeSSH(t)
	tests := []struct {
		host        string
		name        string
		timeout     time.Duration
		wantCode    string
		wantMessage string
		wantStderr  string
	}{
		{name: "ssh failure", host: "down", timeout: time.Minute, wantCode: CodeUnreachable, wantMessage: "exit status 255", wantStderr: "ssh: connect to host down port 22: Connection refused"},
		{name: "timeout", host: "slow", timeout: 300 * time.Millisecond, wantCode: CodeTimeout, wantMessage: "no reply within 300ms"},
		{name: "stdout not a document", host: "banner", timeout: time.Minute, wantCode: CodeReply, wantMessage: `stdout begins "Welcome`, wantStderr: "shell startup warning: before stdout"},
		{name: "invalid field discards partial decoding", host: "invalid-field", timeout: time.Minute, wantCode: CodeReply, wantMessage: `not a timestamp`},
		{name: "remote command failed", host: "missing", timeout: time.Minute, wantCode: CodeCommand, wantMessage: "exit status 127", wantStderr: "fish: Unknown command: esheep"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection := Selection{LocalName: "laptop", Machines: []config.ResolvedMachine{machine("box", test.host, test.timeout)}}

			report, err := List(context.Background(), ssh, selection, session.Request{Mode: session.ModeList}, nil)

			if err != nil {
				t.Fatal(err)
			}
			if report.Complete || len(report.Sessions) != 0 {
				t.Errorf("report = %+v, want incomplete and empty", report)
			}
			wantCount := 1
			if test.wantStderr != "" {
				wantCount++
			}
			if len(report.Diagnostics) != wantCount {
				t.Fatalf("diagnostics = %+v, want %d", report.Diagnostics, wantCount)
			}
			if test.wantStderr != "" {
				want := session.Diagnostic{Code: CodeDiagnostic, Machine: "box", Message: test.wantStderr}
				if report.Diagnostics[0] != want {
					t.Errorf("stderr diagnostic = %+v, want %+v", report.Diagnostics[0], want)
				}
			}
			diagnostic := report.Diagnostics[wantCount-1]
			if diagnostic.Code != test.wantCode || diagnostic.Machine != "box" || !strings.Contains(diagnostic.Message, test.wantMessage) {
				t.Errorf("diagnostic = %+v, want %s on box containing %q", diagnostic, test.wantCode, test.wantMessage)
			}
		})
	}
}

func TestQueryKeepsRecordsButForcesIncompleteOnProcessFailure(t *testing.T) {
	ssh, _ := fakeSSH(t)
	for _, test := range []struct {
		host       string
		timeout    time.Duration
		wantCode   string
		wantStderr string
	}{
		{host: "failed", timeout: time.Minute, wantCode: CodeCommand, wantStderr: "esheep: failed after writing results"},
		{host: "silent", timeout: time.Minute, wantCode: CodeCommand},
		{host: "disconnected", timeout: time.Minute, wantCode: CodeUnreachable, wantStderr: "ssh: connection lost"},
		{host: "timed-out", timeout: 300 * time.Millisecond, wantCode: CodeTimeout, wantStderr: "still waiting for the remote process"},
	} {
		t.Run(test.host, func(t *testing.T) {
			selection := Selection{Machines: []config.ResolvedMachine{machine("box", test.host, test.timeout)}}
			report, err := Search(context.Background(), ssh, selection, session.Request{Mode: session.ModeSearch}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if report.Complete || len(report.Sessions) != 1 || report.Sessions[0].ID != "retained" || report.Sessions[0].Machine != "box" || len(report.Sessions[0].Hits) != 1 {
				t.Fatalf("report = %+v, want incomplete with the remote match retained", report)
			}
			if len(report.Diagnostics) == 0 || report.Diagnostics[len(report.Diagnostics)-1].Code != test.wantCode {
				t.Fatalf("diagnostics = %+v, want %s", report.Diagnostics, test.wantCode)
			}
			if test.wantStderr != "" && (report.Diagnostics[0].Code != CodeDiagnostic || report.Diagnostics[0].Message != test.wantStderr) {
				t.Errorf("diagnostics = %+v, want remote stderr %q", report.Diagnostics, test.wantStderr)
			}
		})
	}
}

func TestQueryPreservesEveryStderrLineAsOpaqueText(t *testing.T) {
	ssh, _ := fakeSSH(t)
	lines := []string{
		"/home/j/project: archive/session.jsonl [pi]: malformed-lines: skipped 1 unparseable lines",
		"warning: first",
		"warning: second",
		"warning: third",
		"warning: fourth",
		"warning: fifth",
		"  an unstructured message: with trailing spaces  ",
	}
	t.Setenv("REMOTE_STDERR", strings.Join(lines, "\n"))
	selection := Selection{Machines: []config.ResolvedMachine{machine("box", "warnings", time.Minute)}}
	report, err := List(context.Background(), ssh, selection, session.Request{Mode: session.ModeList}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete {
		t.Fatal("stderr warnings made a successful report incomplete")
	}
	want := make([]session.Diagnostic, 0, len(lines))
	for _, line := range lines {
		want = append(want, session.Diagnostic{Code: CodeDiagnostic, Machine: "box", Message: line})
	}
	if !reflect.DeepEqual(report.Diagnostics, want) {
		t.Errorf("diagnostics = %+v, want %+v", report.Diagnostics, want)
	}
}

func TestConnectTimeoutSeconds(t *testing.T) {
	tests := []struct {
		timeout time.Duration
		want    int
	}{
		{timeout: 300 * time.Millisecond, want: 1},
		{timeout: 2500 * time.Millisecond, want: 3},
		{timeout: 10 * time.Second, want: 10},
		{timeout: 2 * time.Minute, want: 10},
	}
	for _, test := range tests {
		if got := connectTimeoutSeconds(test.timeout); got != test.want {
			t.Errorf("connectTimeoutSeconds(%s) = %d, want %d", test.timeout, got, test.want)
		}
	}
}
