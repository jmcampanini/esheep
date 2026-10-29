package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// binaryPath is the binary under test. The mise test task builds it through
// the build task and passes its path in ESHEEP_E2E_BINARY, so the suite never
// builds for itself.
var (
	binaryPath string
	workDir    string
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	binaryPath = os.Getenv("ESHEEP_E2E_BINARY")
	if binaryPath == "" {
		fmt.Fprintln(os.Stderr, "ESHEEP_E2E_BINARY is not set; run the suite with 'mise run test'")
		return 1
	}
	if _, err := os.Stat(binaryPath); err != nil {
		fmt.Fprintf(os.Stderr, "stat e2e binary %q: %v\n", binaryPath, err)
		return 1
	}
	repositoryRoot, err := filepath.Abs("..")
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve repository root: %v\n", err)
		return 1
	}
	sandbox := filepath.Join(repositoryRoot, ".sandbox")
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create sandbox: %v\n", err)
		return 1
	}
	workDir, err = os.MkdirTemp(sandbox, "e2e-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create e2e directory: %v\n", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	return m.Run()
}

func TestFoundationConfigurationWorkflow(t *testing.T) {
	root := filepath.Join(workDir, "foundation")
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	for _, directory := range []string{home, configHome} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	environment := map[string]string{
		"HOME":                    home,
		"XDG_CONFIG_HOME":         configHome,
		"ESHEEP_PI_SESSIONS_PATH": "~/pi-sessions",
	}
	settingsPath := filepath.Join(configHome, "esheep", "config.toml")

	configuration := runEsheep(t, environment, "--memory-path=~/memories", "config", "--provenance")
	assertSuccess(t, configuration)
	if !strings.Contains(configuration.stdout, "[memory]\npath = \"~/memories\"\n") ||
		!strings.Contains(configuration.stdout, `# memory.path = "~/memories" (source: <pflag>)`) ||
		!strings.Contains(configuration.stdout, `# sessions.pi.path = "~/pi-sessions" (source: <env>)`) ||
		!strings.Contains(configuration.stdout, "# Resolved paths") ||
		!strings.Contains(configuration.stdout, filepath.Join(home, "memories")) ||
		!strings.Contains(configuration.stdout, "# Provenance") {
		t.Fatalf("config output = %s", configuration.stdout)
	}
	missingConfig := runEsheep(t, environment, "--config", filepath.Join(root, "missing.toml"), "config")
	if missingConfig.exitCode != 1 || missingConfig.stdout != "" || missingConfig.stderr == "" {
		t.Fatalf("missing explicit config result = %#v", missingConfig)
	}
	if _, err := os.Stat(settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent settings file was created: %v", err)
	}

	settings := []byte(`[sessions.claude]
path = "~/claude-sessions"

[[machines]]
name = "nas"
`)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, settings, 0o640); err != nil {
		t.Fatal(err)
	}
	fixedTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(settingsPath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	settingsBefore, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	discovered := runEsheep(t, environment, "config", "--provenance")
	assertSuccess(t, discovered)
	var decoded struct {
		Machines []struct {
			Name string `toml:"name"`
		} `toml:"machines"`
		Sessions struct {
			Claude struct {
				Path string `toml:"path"`
			} `toml:"claude"`
			Pi struct {
				Path string `toml:"path"`
			} `toml:"pi"`
		} `toml:"sessions"`
	}
	if _, err := toml.Decode(discovered.stdout, &decoded); err != nil {
		t.Fatalf("config output is not TOML: %v\n%s", err, discovered.stdout)
	}
	if len(decoded.Machines) != 1 || decoded.Machines[0].Name != "nas" {
		t.Fatalf("machines = %#v", decoded.Machines)
	}
	if decoded.Sessions.Claude.Path != "~/claude-sessions" || decoded.Sessions.Pi.Path != "~/pi-sessions" {
		t.Fatalf("sessions = %#v", decoded.Sessions)
	}
	for _, content := range []string{filepath.Join(home, "claude-sessions"), `# machines.nas.host = "nas"`} {
		if !strings.Contains(discovered.stdout, content) {
			t.Fatalf("config output missing %q:\n%s", content, discovered.stdout)
		}
	}

	settingsAfter, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	settingsContents, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(settingsContents, settings) || settingsAfter.Mode() != settingsBefore.Mode() || !settingsAfter.ModTime().Equal(settingsBefore.ModTime()) {
		t.Fatalf("human-owned settings changed: before=%#v after=%#v contents=%q", settingsBefore, settingsAfter, settingsContents)
	}

	withoutHome := map[string]string{"XDG_CONFIG_HOME": configHome}
	noHome := runEsheep(t, withoutHome, "config")
	if noHome.exitCode != 1 || noHome.stdout != "" || noHome.stderr == "" {
		t.Fatalf("missing HOME result = %#v", noHome)
	}
	relativeXDG := runEsheep(t, map[string]string{"HOME": home, "XDG_CONFIG_HOME": "relative"}, "config")
	assertSuccess(t, relativeXDG)
	fallbackLine := fmt.Sprintf("# config_file = %q", filepath.Join(home, ".config", "esheep", "config.toml"))
	if !strings.Contains(relativeXDG.stdout, fallbackLine) {
		t.Fatalf("relative XDG config stdout = %s, want %s", relativeXDG.stdout, fallbackLine)
	}
}

func TestConfigurationEnvironmentPrecedenceAndProvenance(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	settingsPath := filepath.Join(configHome, "esheep", "config.toml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte("[sessions.codex]\nhome = '~/file-codex'\n[memory]\npath = '~/file-memory'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dataHome := filepath.Join(root, "data")
	for _, test := range []struct {
		name         string
		env          map[string]string
		flags        []string
		wantCodex    string
		wantMemory   string
		codexSource  string
		memorySource string
	}{
		{name: "TOML", wantCodex: "~/file-codex", wantMemory: "~/file-memory", codexSource: settingsPath, memorySource: settingsPath},
		{name: "standard environment", env: map[string]string{"CODEX_HOME": "~/env-codex", "XDG_DATA_HOME": dataHome}, wantCodex: "~/env-codex", wantMemory: filepath.Join(dataHome, "esheep", "memory"), codexSource: "CODEX_HOME", memorySource: "XDG_DATA_HOME"},
		{name: "empty standard environment", env: map[string]string{"CODEX_HOME": "", "XDG_DATA_HOME": ""}, wantCodex: "~/file-codex", wantMemory: "~/file-memory", codexSource: settingsPath, memorySource: settingsPath},
		{name: "Esheep environment overrides invalid standard environment", env: map[string]string{"CODEX_HOME": "relative", "XDG_DATA_HOME": "relative", "ESHEEP_CODEX_HOME": "~/esheep-codex", "ESHEEP_MEMORY_PATH": "~/esheep-memory"}, wantCodex: "~/esheep-codex", wantMemory: "~/esheep-memory", codexSource: "<env>", memorySource: "<env>"},
		{name: "flags override invalid environments", env: map[string]string{"CODEX_HOME": "relative", "XDG_DATA_HOME": "relative", "ESHEEP_CODEX_HOME": "relative", "ESHEEP_MEMORY_PATH": "relative"}, flags: []string{"--codex-home=~/flag-codex", "--memory-path=~/flag-memory"}, wantCodex: "~/flag-codex", wantMemory: "~/flag-memory", codexSource: "<pflag>", memorySource: "<pflag>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := map[string]string{"HOME": home, "XDG_CONFIG_HOME": configHome}
			for key, value := range test.env {
				env[key] = value
			}
			args := append([]string{"config", "--provenance"}, test.flags...)
			result := runEsheep(t, env, args...)
			assertSuccess(t, result)
			for _, line := range []string{
				fmt.Sprintf("# sessions.codex.home = %q (source: %s)", test.wantCodex, test.codexSource),
				fmt.Sprintf("# memory.path = %q (source: %s)", test.wantMemory, test.memorySource),
			} {
				if !strings.Contains(result.stdout, line) {
					t.Fatalf("config report missing %q:\n%s", line, result.stdout)
				}
			}
			var decoded struct {
				Memory struct {
					Path string `toml:"path"`
				} `toml:"memory"`
				Sessions struct {
					Codex struct {
						Home string `toml:"home"`
					} `toml:"codex"`
				} `toml:"sessions"`
			}
			if _, err := toml.Decode(result.stdout, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Memory.Path != test.wantMemory || decoded.Sessions.Codex.Home != test.wantCodex {
				t.Fatalf("config = %#v, want memory %q and Codex home %q", decoded, test.wantMemory, test.wantCodex)
			}
			redirected := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(redirected, []byte(result.stdout), 0o600); err != nil {
				t.Fatal(err)
			}
			reloaded := runEsheep(t, map[string]string{"HOME": home, "XDG_CONFIG_HOME": configHome}, "--config", redirected, "config")
			assertSuccess(t, reloaded)
			if _, err := toml.Decode(reloaded.stdout, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Memory.Path != test.wantMemory || decoded.Sessions.Codex.Home != test.wantCodex {
				t.Fatalf("reloaded config = %#v, want memory %q and Codex home %q", decoded, test.wantMemory, test.wantCodex)
			}
		})
	}
	invalid := runEsheep(t, map[string]string{"HOME": home, "XDG_CONFIG_HOME": configHome, "XDG_DATA_HOME": "relative"}, "config")
	if invalid.exitCode != 1 || invalid.stdout != "" || !strings.Contains(invalid.stderr, "XDG_DATA_HOME must be absolute") {
		t.Fatalf("invalid winning XDG_DATA_HOME result = %#v", invalid)
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var snapshot strings.Builder
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(&snapshot, "%s %s", filepath.ToSlash(relative), info.Mode())
		switch {
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(&snapshot, " %q", data)
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(&snapshot, " -> %q", target)
		}
		_ = snapshot.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.String()
}

type processResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runEsheep(t *testing.T, environment map[string]string, args ...string) processResult {
	t.Helper()
	command := exec.Command(binaryPath, args...)
	command.Env = processEnvironment(environment)
	return runProcess(t, command)
}

func runProcess(t *testing.T, command *exec.Cmd) processResult {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	exitCode := 0
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Fatalf("run esheep: %v", err)
		}
		exitCode = exitError.ExitCode()
	}
	return processResult{stdout: stdout.String(), stderr: stderr.String(), exitCode: exitCode}
}

func assertSuccess(t *testing.T, result processResult) {
	t.Helper()
	if result.exitCode != 0 || result.stderr != "" {
		t.Fatalf("process result = %#v", result)
	}
}

// assertSuccessWithDiagnostics fails unless result exited 0 and stderr holds
// at least one line, each a diagnostic carrying one of codes.
func assertSuccessWithDiagnostics(t *testing.T, result processResult, codes ...string) {
	t.Helper()
	if result.exitCode != 0 || result.stderr == "" {
		t.Fatalf("process result = %#v, want exit 0 and diagnostics on stderr", result)
	}
	for _, line := range strings.Split(strings.TrimSuffix(result.stderr, "\n"), "\n") {
		if !slices.ContainsFunc(codes, func(code string) bool { return strings.Contains(line, ": "+code+": ") }) {
			t.Fatalf("stderr line %q carries none of the codes %q", line, codes)
		}
	}
}

func processEnvironment(overrides map[string]string) []string {
	blocked := map[string]struct{}{
		"CLAUDE_CODE_SESSION_ID": {}, "CODEX_HOME": {}, "CODEX_THREAD_ID": {}, "HOME": {},
		"PI_SESSION_ID": {}, "XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {},
	}
	var environment []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, found := blocked[key]; found || strings.HasPrefix(key, "ESHEEP_") {
			continue
		}
		if _, found := overrides[key]; !found {
			environment = append(environment, entry)
		}
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	return environment
}

func TestSessionsWorkflow(t *testing.T) {
	root := filepath.Join(workDir, "sessions")
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	claudeProject := filepath.Join(home, ".claude", "projects", "-Users-u-proj")
	codexDay := filepath.Join(home, ".codex", "sessions", "2026", "08", "25")
	piProject := filepath.Join(home, ".pi", "agent", "sessions", "--Users-u-piproj--")
	if err := os.MkdirAll(configHome, 0o755); err != nil {
		t.Fatal(err)
	}

	writeSessionTranscript(t, filepath.Join(claudeProject, "11111111-aaaa-bbbb-cccc-222222222222.jsonl"),
		`{"type":"user","timestamp":"2026-08-20T10:00:00Z","cwd":"/Users/u/proj","message":{"role":"user","content":"find the GOMODCACHE bug"}}`,
		`{"type":"assistant","timestamp":"2026-08-20T10:00:05Z","cwd":"/Users/u/proj","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go env GOMODCACHE"}}]}}`,
		`{"type":"user","timestamp":"2026-08-20T10:00:09Z","cwd":"/Users/u/proj","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"permission denied","is_error":true}]}}`,
	)
	writeSessionTranscript(t, filepath.Join(claudeProject, "11111111-aaaa-bbbb-cccc-222222222222", "subagents", "agent-abc.jsonl"),
		`{"type":"user","timestamp":"2026-08-20T12:01:00Z","cwd":"/Users/u/proj","isSidechain":true,"message":{"role":"user","content":"subagent prompt"}}`,
	)
	writeSessionTranscript(t, filepath.Join(codexDay, "rollout-2026-08-25T09-00-00-33333333-dddd-eeee-ffff-444444444444.jsonl"),
		`{"timestamp":"2026-08-25T09:00:00Z","type":"session_meta","payload":{"id":"33333333-dddd-eeee-ffff-444444444444","timestamp":"2026-08-25T09:00:00Z","cwd":"/Users/u/codexproj","cli_version":"0.151.0"}}`,
		`{"timestamp":"2026-08-25T09:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the flaky test"}]}}`,
	)
	writeSessionTranscript(t, filepath.Join(piProject, "2026-08-10T08-00-00-000Z_55555555-aaaa-bbbb-cccc-666666666666.jsonl"),
		`{"type":"session","version":3,"id":"55555555-aaaa-bbbb-cccc-666666666666","timestamp":"2026-08-10T08:00:00Z","cwd":"/Users/u/piproj"}`,
		`{"type":"message","id":"ad","parentId":null,"timestamp":"2026-08-10T08:00:08Z","message":{"role":"toolResult","toolName":"bash","isError":true,"content":[{"type":"text","text":"make: *** [test] Error 1"}]}}`,
	)
	subagent := filepath.Join(piProject, "2026-08-10T09-00-00-000Z_77777777-aaaa-bbbb-cccc-888888888888.jsonl")
	writeSessionTranscript(t, subagent,
		`{"type":"session","version":3,"id":"77777777-aaaa-bbbb-cccc-888888888888","timestamp":"2026-08-10T09:00:00Z","cwd":"/Users/u/piproj"}`,
	)
	if err := os.WriteFile(subagent+".meta", []byte(`{"agent":"scout"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{"HOME": home, "XDG_CONFIG_HOME": configHome}
	storesBefore := snapshotTree(t, home)

	listed := runEsheep(t, environment, "sessions", "list", "--json")
	assertSuccessWithDiagnostics(t, listed, "root-missing")
	var inventory struct {
		Complete bool `json:"complete"`
		Sessions []struct {
			Harness string `json:"harness"`
			ID      string `json:"id"`
			Path    string `json:"path"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(listed.stdout), &inventory); err != nil {
		t.Fatalf("decode inventory: %v\n%s", err, listed.stdout)
	}
	if !inventory.Complete || len(inventory.Sessions) != 3 {
		t.Fatalf("inventory = %#v", inventory)
	}
	for _, entry := range inventory.Sessions {
		if _, err := os.Stat(entry.Path); err != nil {
			t.Fatalf("session %q path does not resolve: %v", entry.ID, err)
		}
	}

	withSubagents := runEsheep(t, environment, "sessions", "list", "--json", "--subagents")
	assertSuccessWithDiagnostics(t, withSubagents, "root-missing")
	if err := json.Unmarshal([]byte(withSubagents.stdout), &inventory); err != nil {
		t.Fatalf("decode subagent inventory: %v\n%s", err, withSubagents.stdout)
	}
	if got := len(inventory.Sessions); got != 5 {
		t.Fatalf("subagent inventory sessions = %d, want 5\n%s", got, withSubagents.stdout)
	}

	searched := runEsheep(t, environment, "sessions", "search", "--json", "GOMODCACHE")
	assertSuccessWithDiagnostics(t, searched, "root-missing")
	var matches struct {
		Sessions []struct {
			Harness string `json:"harness"`
			Hits    []struct {
				Line int    `json:"line"`
				Role string `json:"role"`
			} `json:"hits"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(searched.stdout), &matches); err != nil {
		t.Fatalf("decode search: %v\n%s", err, searched.stdout)
	}
	if len(matches.Sessions) != 1 || matches.Sessions[0].Harness != "claude" || len(matches.Sessions[0].Hits) != 2 {
		t.Fatalf("search = %#v", matches)
	}
	if matches.Sessions[0].Hits[0].Line != 1 || matches.Sessions[0].Hits[0].Role != "user" {
		t.Fatalf("first hit = %#v", matches.Sessions[0].Hits[0])
	}

	failures := runEsheep(t, environment, "sessions", "search", "--json", "--errors")
	assertSuccessWithDiagnostics(t, failures, "root-missing")
	if got := strings.Count(failures.stdout, `"error": true`); got != 2 {
		t.Fatalf("error search hits = %d, want 2\n%s", got, failures.stdout)
	}

	if got := snapshotTree(t, home); got != storesBefore {
		t.Fatal("sessions commands modified a session store")
	}
}

func writeSessionTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
