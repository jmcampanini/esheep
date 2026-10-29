package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemoteSessionsWorkflow drives --remote through a fake ssh that runs
// the received command line against the built binary and a second fixture
// home, so the whole path from flag to merged output runs with no network.
func TestRemoteSessionsWorkflow(t *testing.T) {
	root := filepath.Join(workDir, "remote")
	localHome := filepath.Join(root, "local-home")
	localConfig := filepath.Join(root, "local-config")
	remoteHome := filepath.Join(root, "remote-home")
	remoteConfig := filepath.Join(root, "remote-config")
	fakeBin := filepath.Join(root, "bin")
	for _, directory := range []string{filepath.Join(localConfig, "esheep"), remoteConfig, fakeBin} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeSessionTranscript(t, filepath.Join(localHome, ".claude", "projects", "-Users-u-proj", "11111111-aaaa-bbbb-cccc-222222222222.jsonl"),
		`{"type":"user","timestamp":"2026-08-20T10:00:00Z","cwd":"/Users/u/proj","message":{"role":"user","content":"local timeout question"}}`,
	)
	writeSessionTranscript(t, filepath.Join(remoteHome, ".pi", "agent", "sessions", "--home-u-piproj--", "2026-08-22T08-00-00-000Z_55555555-aaaa-bbbb-cccc-666666666666.jsonl"),
		`{"type":"session","version":3,"id":"55555555-aaaa-bbbb-cccc-666666666666","timestamp":"2026-08-22T08:00:00Z","cwd":"/home/u/piproj"}`,
		`{"type":"message","id":"m1","parentId":null,"timestamp":"2026-08-22T08:00:05Z","message":{"role":"user","content":[{"type":"text","text":"remote timeout question"}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"call-a","name":"shell","arguments":{"command":"timeout"}}]}}`,
		`{"type":"message","message":{"role":"toolResult","toolCallId":"call-a","toolName":"shell","content":[{"type":"text","text":"timeout output"}]}}`,
		`malformed transcript line`,
	)
	settings := "[[machines]]\nname = \"e2e-remote\"\ncommand = \"" + binaryPath + "\"\n\n[[machines]]\nname = \"e2e-down\"\n"
	if err := os.WriteFile(filepath.Join(localConfig, "esheep", "config.toml"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	// The fake ssh answers e2e-remote by running the received command line
	// in the remote fixture home and refuses every other host like ssh does.
	script := `#!/bin/sh
shift 5
host="$1"
shift
if [ "$host" != "e2e-remote" ]; then
  echo "ssh: Could not resolve hostname $host" >&2
  exit 255
fi
cat > "$REMOTE_RECORD"
export HOME="$REMOTE_HOME" XDG_CONFIG_HOME="$REMOTE_CONFIG"
sh -c "$1" < "$REMOTE_RECORD" > "$REMOTE_REPLY"
result=$?
cat "$REMOTE_REPLY"
if [ "$REMOTE_FORCE_FAILURE" = 1 ]; then
  echo 'remote launcher: failed after writing results' >&2
  exit 1
fi
exit "$result"
`
	if err := os.WriteFile(filepath.Join(fakeBin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{
		"HOME":            localHome,
		"XDG_CONFIG_HOME": localConfig,
		"PATH":            fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"REMOTE_HOME":     remoteHome,
		"REMOTE_CONFIG":   remoteConfig,
		"REMOTE_RECORD":   filepath.Join(root, "request.json"),
		"REMOTE_REPLY":    filepath.Join(root, "reply.json"),
	}
	storesBefore := snapshotTree(t, localHome) + snapshotTree(t, remoteHome)

	searched := runEsheep(t, environment, "sessions", "search", "timeout", "--remote", "e2e-remote", "--json")
	assertSuccessWithDiagnostics(t, searched, "malformed-lines", "root-missing")
	if !strings.Contains(searched.stderr, "e2e-remote: machine-diagnostic: "+remoteHome) || !strings.Contains(searched.stderr, "malformed-lines: skipped 1 unparseable lines") {
		t.Fatalf("search stderr = %q, want the remote transcript warning", searched.stderr)
	}
	var matches struct {
		Complete bool `json:"complete"`
		Sessions []struct {
			Harness string `json:"harness"`
			Machine string `json:"machine"`
			Path    string `json:"path"`
			Origin  struct {
				Kind string `json:"kind"`
			} `json:"origin"`
			Hits []struct {
				CallID string `json:"call_id"`
				Kind   string `json:"kind"`
				Line   int    `json:"line"`
				Origin struct {
					Kind string `json:"kind"`
				} `json:"origin"`
			} `json:"hits"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(searched.stdout), &matches); err != nil {
		t.Fatalf("decode search: %v\n%s", err, searched.stdout)
	}
	if !matches.Complete || len(matches.Sessions) != 2 {
		t.Fatalf("search = %#v", matches)
	}
	assertRemotePayloadHasNoDiagnostics(t, environment["REMOTE_REPLY"], searched.stdout)
	remoteMatch, localMatch := matches.Sessions[0], matches.Sessions[1]
	if remoteMatch.Machine != "e2e-remote" || remoteMatch.Harness != "pi" || !strings.HasPrefix(remoteMatch.Path, remoteHome) || len(remoteMatch.Hits) != 3 || remoteMatch.Hits[0].Line != 2 {
		t.Errorf("remote match = %#v, want the pi hit from the remote home first", remoteMatch)
	}
	if len(remoteMatch.Hits) == 3 && (remoteMatch.Origin.Kind != "unknown" || remoteMatch.Hits[0].Kind != "message" || remoteMatch.Hits[0].Origin.Kind != "unknown" || remoteMatch.Hits[1].Kind != "tool_call" || remoteMatch.Hits[1].CallID != "call-a" || remoteMatch.Hits[2].Kind != "tool_result" || remoteMatch.Hits[2].CallID != "call-a" || remoteMatch.Hits[2].Origin.Kind != "tool") {
		t.Errorf("remote match = %+v, want session/event classification and call IDs across query transport", remoteMatch)
	}
	if localMatch.Machine == "" || localMatch.Machine == "e2e-remote" || localMatch.Harness != "claude" || !strings.HasPrefix(localMatch.Path, localHome) {
		t.Errorf("local match = %#v, want the claude hit labeled with this host", localMatch)
	}
	request, err := os.ReadFile(filepath.Join(root, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var forwarded struct {
		Mode  string `json:"mode"`
		Query struct {
			Pattern string `json:"pattern"`
		} `json:"query"`
	}
	if err := json.Unmarshal(request, &forwarded); err != nil || forwarded.Mode != "search" || forwarded.Query.Pattern != "timeout" {
		t.Errorf("forwarded request = %s (%v), want a search for timeout", request, err)
	}

	listed := runEsheep(t, environment, "sessions", "list", "--remote", "all")
	if listed.exitCode != 1 {
		t.Fatalf("list --remote all with an unreachable machine = %#v, want exit 1", listed)
	}
	if !strings.Contains(listed.stdout, "MACHINE") || !strings.Contains(listed.stdout, "e2e-remote") || !strings.Contains(listed.stdout, "claude") {
		t.Errorf("list stdout = %q, stderr = %q; want a MACHINE column with both machines' sessions", listed.stdout, listed.stderr)
	}
	if !strings.Contains(listed.stderr, "e2e-down: machine-diagnostic: ssh: Could not resolve hostname e2e-down") || !strings.Contains(listed.stderr, "e2e-down: machine-unreachable: exit status 255") || !strings.Contains(listed.stderr, "session inventory is incomplete") {
		t.Errorf("list stderr = %q, want the unreachable diagnostic and the incomplete error", listed.stderr)
	}

	unknown := runEsheep(t, environment, "sessions", "list", "--remote", "laptop9")
	if unknown.exitCode != 1 || !strings.Contains(unknown.stderr, `unknown machine "laptop9"`) {
		t.Errorf("unknown machine = %#v, want application error", unknown)
	}

	if got := snapshotTree(t, localHome) + snapshotTree(t, remoteHome); got != storesBefore {
		t.Fatal("remote session commands modified a session store")
	}

	environment["REMOTE_FORCE_FAILURE"] = "1"
	for _, command := range [][]string{{"list"}, {"search", "timeout"}} {
		args := append([]string{"sessions"}, command...)
		args = append(args, "--harness", "pi", "--remote", "e2e-remote", "--no-local", "--json")
		result := runEsheep(t, environment, args...)
		if result.exitCode != 1 || !strings.Contains(result.stderr, "e2e-remote: machine-diagnostic: remote launcher: failed after writing results") || !strings.Contains(result.stderr, "e2e-remote: machine-command: exit status 1") {
			t.Fatalf("%s = %#v, want the remote process failure", command[0], result)
		}
		if err := json.Unmarshal([]byte(result.stdout), &matches); err != nil || matches.Complete || len(matches.Sessions) != 1 || matches.Sessions[0].Machine != "e2e-remote" {
			t.Fatalf("%s payload = %q (%v), want an incomplete report retaining the remote session", command[0], result.stdout, err)
		}
		if command[0] == "search" && len(matches.Sessions[0].Hits) != 3 {
			t.Errorf("search payload = %q, want the remote hits retained", result.stdout)
		}
		reply := assertRemotePayloadHasNoDiagnostics(t, environment["REMOTE_REPLY"], result.stdout)
		if string(reply["complete"]) != "true" {
			t.Fatalf("remote payload = %v, want complete before the launcher failed", reply)
		}
	}
	delete(environment, "REMOTE_FORCE_FAILURE")
	if got := snapshotTree(t, localHome) + snapshotTree(t, remoteHome); got != storesBefore {
		t.Fatal("failed remote session commands modified a session store")
	}

	brokenRoot := filepath.Join(remoteHome, ".codex", "sessions")
	if err := os.MkdirAll(filepath.Dir(brokenRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brokenRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	storesBefore = snapshotTree(t, localHome) + snapshotTree(t, remoteHome)
	for _, command := range [][]string{{"list"}, {"search", "timeout"}} {
		args := append([]string{"sessions"}, command...)
		args = append(args, "--harness", "codex", "--remote", "e2e-remote", "--no-local", "--json")

		result := runEsheep(t, environment, args...)

		if result.exitCode != 1 || !strings.Contains(result.stderr, "e2e-remote: machine-diagnostic: "+brokenRoot+" [codex]: root-unusable:") || strings.Count(result.stderr, "\nesheep: ") != 1 {
			t.Fatalf("%s = %#v, want the remote read diagnostic and one final failure line", command[0], result)
		}
		failure := "session inventory is incomplete"
		if command[0] == "search" {
			failure = "session search is incomplete"
		}
		if !strings.HasSuffix(result.stderr, "esheep: "+failure+"\n") {
			t.Errorf("stderr = %q, want final %q", result.stderr, failure)
		}
		if !strings.Contains(result.stderr, "e2e-remote: machine-diagnostic: esheep: "+failure+"\n") || !strings.Contains(result.stderr, "e2e-remote: machine-command: exit status 1") {
			t.Errorf("stderr = %q, want the remote failure forwarded", result.stderr)
		}
		if err := json.Unmarshal([]byte(result.stdout), &matches); err != nil || matches.Complete || len(matches.Sessions) != 0 {
			t.Fatalf("%s payload = %q (%v), want an incomplete empty inventory", command[0], result.stdout, err)
		}
		assertRemotePayloadHasNoDiagnostics(t, environment["REMOTE_REPLY"], result.stdout)
	}
	if got := snapshotTree(t, localHome) + snapshotTree(t, remoteHome); got != storesBefore {
		t.Fatal("incomplete remote session commands modified a session store")
	}
}

func assertRemotePayloadHasNoDiagnostics(t *testing.T, replyPath, stdout string) map[string]json.RawMessage {
	t.Helper()
	reply, err := os.ReadFile(replyPath)
	if err != nil {
		t.Fatal(err)
	}
	var remotePayload map[string]json.RawMessage
	for _, payload := range []string{stdout, string(reply)} {
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			t.Fatalf("decode payload %q: %v", payload, err)
		}
		if _, exists := decoded["diagnostics"]; exists || strings.Contains(payload, "machine-diagnostic") || strings.Contains(payload, "malformed-lines") || strings.Contains(payload, "root-unusable") || strings.Contains(payload, "failed after writing results") {
			t.Errorf("payload = %q, want no diagnostics", payload)
		}
		remotePayload = decoded
	}
	return remotePayload
}
