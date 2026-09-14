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
	)
	settings := "[[machines]]\nname = \"e2e-remote\"\ncommand = \"" + binaryPath + "\"\n\n[[machines]]\nname = \"e2e-down\"\n"
	if err := os.WriteFile(filepath.Join(localConfig, "esheep", "esheep.toml"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	// The fake ssh answers e2e-remote by running the received command line
	// in the remote fixture home and refuses every other host like ssh does.
	script := `#!/bin/sh
cat > "$REMOTE_RECORD"
shift 5
host="$1"
shift
if [ "$host" != "e2e-remote" ]; then
  echo "ssh: Could not resolve hostname $host" >&2
  exit 255
fi
export HOME="$REMOTE_HOME" XDG_CONFIG_HOME="$REMOTE_CONFIG"
exec sh -c "$1" < "$REMOTE_RECORD"
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
	}
	storesBefore := snapshotTree(t, localHome) + snapshotTree(t, remoteHome)

	searched := runEsheep(t, environment, "sessions", "search", "timeout", "--remote", "e2e-remote", "--json")
	assertSuccess(t, searched)
	var matches struct {
		Complete bool `json:"complete"`
		Sessions []struct {
			Harness string `json:"harness"`
			Machine string `json:"machine"`
			Path    string `json:"path"`
			Hits    []struct {
				Line int `json:"line"`
			} `json:"hits"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(searched.stdout), &matches); err != nil {
		t.Fatalf("decode search: %v\n%s", err, searched.stdout)
	}
	if !matches.Complete || len(matches.Sessions) != 2 {
		t.Fatalf("search = %#v", matches)
	}
	remoteMatch, localMatch := matches.Sessions[0], matches.Sessions[1]
	if remoteMatch.Machine != "e2e-remote" || remoteMatch.Harness != "pi" || !strings.HasPrefix(remoteMatch.Path, remoteHome) || len(remoteMatch.Hits) != 1 || remoteMatch.Hits[0].Line != 2 {
		t.Errorf("remote match = %#v, want the pi hit from the remote home first", remoteMatch)
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
		t.Errorf("list stdout = %q, want a MACHINE column with both machines' sessions", listed.stdout)
	}
	if !strings.Contains(listed.stderr, "e2e-down: machine-unreachable: ssh: Could not resolve hostname e2e-down") || !strings.Contains(listed.stderr, "session inventory is incomplete") {
		t.Errorf("list stderr = %q, want the unreachable diagnostic and the incomplete error", listed.stderr)
	}

	unknown := runEsheep(t, environment, "sessions", "list", "--remote", "laptop9")
	if unknown.exitCode != 2 || !strings.Contains(unknown.stderr, `unknown machine "laptop9"`) {
		t.Errorf("unknown machine = %#v, want usage error", unknown)
	}

	if got := snapshotTree(t, localHome) + snapshotTree(t, remoteHome); got != storesBefore {
		t.Fatal("remote session commands modified a session store")
	}
}
