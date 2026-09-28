package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryWorkflow(t *testing.T) {
	root := filepath.Join(workDir, "memory")
	home := filepath.Join(root, "home")
	dataHome := filepath.Join(root, "data")
	repository := filepath.Join(root, "widgets")
	worktree := filepath.Join(root, "widgets-feature")
	// The sandbox sits inside this repository, so a directory outside git
	// must come from the system temp dir; git reports canonical paths, so
	// resolve the macOS /var symlink up front.
	scratch, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{home, dataHome, repository} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "-c", "user.name=t", "-c", "user.email=t@example.test", "commit", "-q", "--allow-empty", "-m", "init")
	runGit(t, repository, "remote", "add", "origin", "git@github.com:Acme/Widgets.git")
	runGit(t, repository, "worktree", "add", "-q", worktree)
	environment := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(root, "config"), "XDG_DATA_HOME": dataHome}
	inClaude := map[string]string{"CLAUDE_CODE_SESSION_ID": "claude-1"}
	inCodexInsideClaude := map[string]string{"CLAUDE_CODE_SESSION_ID": "claude-1", "CODEX_THREAD_ID": "codex-1"}
	wantDirectory := filepath.Join(dataHome, "esheep", "memory", "github.com-Acme-Widgets", "memory")

	fromRepository := runEsheepIn(t, repository, "", environment, inClaude, "memory", "record", "prefer merge over rebase", "--why", "one merge commit", "--source", "README.md")
	assertSuccess(t, fromRepository)
	fromWorktree := runEsheepIn(t, worktree, "multi\nline\n", environment, inCodexInsideClaude, "memory", "record", "-")
	assertSuccess(t, fromWorktree)
	fromScratch := runEsheepIn(t, scratch, "", environment, inClaude, "memory", "record", "outside git")
	assertSuccess(t, fromScratch)
	byHand := runEsheepIn(t, scratch, "", environment, nil, "memory", "record", "typed by a person", "--session", "manual-1")
	assertSuccess(t, byHand)
	noSession := runEsheepIn(t, scratch, "", environment, nil, "memory", "record", "nowhere to go")
	blank := runEsheepIn(t, scratch, "", environment, inClaude, "memory", "record", " ")

	if fromRepository.stdout != filepath.Join(wantDirectory, "claude-1.jsonl")+"\n" {
		t.Fatalf("repository stdout = %q", fromRepository.stdout)
	}
	if fromWorktree.stdout != filepath.Join(wantDirectory, "codex-1.jsonl")+"\n" {
		t.Fatalf("worktree stdout = %q, want the codex session in the same project", fromWorktree.stdout)
	}
	scratchDirectory := filepath.Join(dataHome, "esheep", "memory", strings.ReplaceAll(scratch, "/", "-"), "memory")
	if fromScratch.stdout != filepath.Join(scratchDirectory, "claude-1.jsonl")+"\n" || byHand.stdout != filepath.Join(scratchDirectory, "manual-1.jsonl")+"\n" {
		t.Fatalf("scratch stdout = %q, by hand stdout = %q", fromScratch.stdout, byHand.stdout)
	}
	if noSession.exitCode != 1 || !strings.Contains(noSession.stderr, "CLAUDE_CODE_SESSION_ID") || noSession.stdout != "" {
		t.Fatalf("no session result = %#v", noSession)
	}
	if blank.exitCode != 2 || !strings.Contains(blank.stderr, "TEXT must not be blank") {
		t.Fatalf("blank result = %#v", blank)
	}

	claudeEntries := readMemoryEntries(t, filepath.Join(wantDirectory, "claude-1.jsonl"))
	if len(claudeEntries) != 1 || claudeEntries[0]["text"] != "prefer merge over rebase" || claudeEntries[0]["why"] != "one merge commit" || claudeEntries[0]["cwd"] != repository {
		t.Fatalf("claude entries = %#v", claudeEntries)
	}
	if sources, ok := claudeEntries[0]["sources"].([]any); !ok || len(sources) != 1 || sources[0] != "README.md" {
		t.Fatalf("claude sources = %#v", claudeEntries[0]["sources"])
	}
	codexEntries := readMemoryEntries(t, filepath.Join(wantDirectory, "codex-1.jsonl"))
	if len(codexEntries) != 1 || codexEntries[0]["text"] != "multi\nline\n" || codexEntries[0]["cwd"] != worktree {
		t.Fatalf("codex entries = %#v", codexEntries)
	}
	if _, present := codexEntries[0]["why"]; present {
		t.Fatalf("codex entry has why: %#v", codexEntries[0])
	}
	if _, present := codexEntries[0]["sources"]; present {
		t.Fatalf("codex entry has sources: %#v", codexEntries[0])
	}
	if _, err := os.Stat(filepath.Join(home, ".local")); !os.IsNotExist(err) {
		t.Fatalf("home .local stat = %v, want absent because XDG_DATA_HOME is set", err)
	}
}

func readMemoryEntries(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		if !strings.HasPrefix(line, `{"time":"`) || !strings.Contains(line, `","cwd":"`) {
			t.Fatalf("line %q does not start with time then cwd", line)
		}
		entries = append(entries, entry)
	}
	return entries
}

// runEsheepIn runs esheep in dir with stdin and with extra variables layered
// over the base environment.
func runEsheepIn(t *testing.T, dir, stdin string, environment, extra map[string]string, args ...string) processResult {
	t.Helper()
	merged := make(map[string]string, len(environment)+len(extra))
	for key, value := range environment {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	command := exec.Command(binaryPath, args...)
	command.Dir = dir
	command.Env = processEnvironment(merged)
	command.Stdin = strings.NewReader(stdin)
	return runProcess(t, command)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
