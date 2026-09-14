package remote

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
host="$5"
case "$host" in
ok)
  printf '%s' '{"complete":true,"diagnostics":[{"code":"root-missing","harness":"pi","message":"skipped","path":"/home/j/.pi"}],"sessions":[{"harness":"claude","id":"remote-1","machine":"ignored","modified_at":"2026-09-10T10:00:00Z","path":"/home/j/.claude/a.jsonl","projects":[],"started_at":"2026-09-10T10:00:00Z","subagent":false,"hits":[{"line":3,"role":"user","excerpt":"timeout"}]}],"future_field":1}'
  exit 1 ;;
down)
  echo "ssh: connect to host down port 22: Connection refused" >&2
  exit 255 ;;
slow)
  sleep 5
  exit 0 ;;
banner)
  echo "Welcome to the shell"
  printf '%s' '{"complete":true}' ;;
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
		return session.SearchReport{Complete: true, Sessions: []session.Match{
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
		t.Errorf("complete = false, want the remote document's completeness to win over its exit status")
	}
	var ids []string
	for _, match := range report.Sessions {
		ids = append(ids, match.Machine+":"+match.ID)
	}
	if got := strings.Join(ids, " "); got != "laptop:local-tie nas:remote-1 laptop:local-old" {
		t.Errorf("merged order = %q, want time descending with machine tiebreak", got)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Machine != "nas" || report.Diagnostics[0].Code != "root-missing" {
		t.Errorf("diagnostics = %+v, want the remote diagnostic stamped nas", report.Diagnostics)
	}
	if report.Sessions[1].Path != "/home/j/.claude/a.jsonl" || len(report.Sessions[1].Hits) != 1 {
		t.Errorf("remote match = %+v, want remote path and hits preserved", report.Sessions[1])
	}

	args, err := os.ReadFile(filepath.Join(recordDir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := "-o\nBatchMode=yes\n-o\nConnectTimeout=10\nok\n/opt/homebrew/bin/esheep sessions query\n"
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

func TestQueryFailuresBecomeDiagnostics(t *testing.T) {
	ssh, _ := fakeSSH(t)
	tests := []struct {
		host        string
		name        string
		timeout     time.Duration
		wantCode    string
		wantMessage string
	}{
		{name: "ssh failure", host: "down", timeout: time.Minute, wantCode: CodeUnreachable, wantMessage: "Connection refused"},
		{name: "timeout", host: "slow", timeout: 300 * time.Millisecond, wantCode: CodeTimeout, wantMessage: "no reply within 300ms"},
		{name: "stdout not a document", host: "banner", timeout: time.Minute, wantCode: CodeReply, wantMessage: `stdout begins "Welcome`},
		{name: "remote command failed", host: "missing", timeout: time.Minute, wantCode: CodeCommand, wantMessage: "Unknown command: esheep"},
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
			if len(report.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %+v, want exactly one", report.Diagnostics)
			}
			diagnostic := report.Diagnostics[0]
			if diagnostic.Code != test.wantCode || diagnostic.Machine != "box" || !strings.Contains(diagnostic.Message, test.wantMessage) {
				t.Errorf("diagnostic = %+v, want %s on box containing %q", diagnostic, test.wantCode, test.wantMessage)
			}
		})
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
