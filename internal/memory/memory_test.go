package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordAppendsOneLinePerMemoryInFieldOrder(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	env := map[string]string{"CLAUDE_CODE_SESSION_ID": "session-1"}
	first := Request{Cwd: cwd, Env: env, Root: root, Text: "prefer merge over rebase", Time: time.Date(2026, 9, 28, 10, 30, 0, 0, time.FixedZone("EDT", -4*3600))}
	second := first
	second.Text = "multi\nline"
	second.Why = "the user said so"
	second.Sources = []string{"README.md", "https://example.test/doc"}

	firstResult, err := Record(context.Background(), first)
	if err != nil {
		t.Fatalf("Record(first): %v", err)
	}
	secondResult, err := Record(context.Background(), second)
	if err != nil {
		t.Fatalf("Record(second): %v", err)
	}

	wantPath := filepath.Join(root, Segment(cwd), "memory", "session-1.jsonl")
	if firstResult.Path != wantPath || secondResult.Path != wantPath {
		t.Fatalf("paths = %q, %q, want %q", firstResult.Path, secondResult.Path, wantPath)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-09-28T10:30:00-04:00","cwd":` + quote(cwd) + `,"text":"prefer merge over rebase"}` + "\n" +
		`{"time":"2026-09-28T10:30:00-04:00","cwd":` + quote(cwd) + `,"text":"multi\nline","why":"the user said so","sources":["README.md","https://example.test/doc"]}` + "\n"
	if string(data) != want {
		t.Fatalf("file = %s, want %s", data, want)
	}
}

func TestRecordSessionPrecedence(t *testing.T) {
	tests := []struct {
		env      map[string]string
		name     string
		override string
		want     string
	}{
		{name: "codex wins over pi and claude", env: map[string]string{"CLAUDE_CODE_SESSION_ID": "c", "CODEX_THREAD_ID": "x", "PI_SESSION_ID": "p"}, want: "x"},
		{name: "pi wins over claude", env: map[string]string{"CLAUDE_CODE_SESSION_ID": "c", "PI_SESSION_ID": "p"}, want: "p"},
		{name: "claude alone", env: map[string]string{"CLAUDE_CODE_SESSION_ID": "c"}, want: "c"},
		{name: "empty values are unset", env: map[string]string{"CODEX_THREAD_ID": "", "PI_SESSION_ID": "p"}, want: "p"},
		{name: "override wins", env: map[string]string{"CODEX_THREAD_ID": "x"}, override: "manual", want: "manual"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()

			result, err := Record(context.Background(), Request{Cwd: t.TempDir(), Env: test.env, Root: root, Session: test.override, Text: "t"})
			if err != nil {
				t.Fatalf("Record: %v", err)
			}

			if got := filepath.Base(result.Path); got != test.want+".jsonl" {
				t.Fatalf("file = %q, want %q", got, test.want+".jsonl")
			}
		})
	}
}

func TestRecordRejectsMissingOrUnsafeSession(t *testing.T) {
	tests := []struct {
		env  map[string]string
		name string
		want string
	}{
		{name: "none", env: map[string]string{}, want: "no session detected"},
		{name: "path separator", env: map[string]string{"PI_SESSION_ID": "../x"}, want: "PI_SESSION_ID"},
		{name: "whitespace", env: map[string]string{"CODEX_THREAD_ID": "a b"}, want: "CODEX_THREAD_ID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()

			_, err := Record(context.Background(), Request{Cwd: t.TempDir(), Env: test.env, Root: root, Text: "t"})

			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Record error = %v, want containing %q", err, test.want)
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("root entries = %v, %v, want none", entries, readErr)
			}
		})
	}
}

func TestValidateSession(t *testing.T) {
	valid := []string{"94c6638d-97b9-410e-a44a-a6a24fbd52fd", "01a0ccc8-4c86-76eb-af08-8acfb88ae784", "manual_1"}
	for _, session := range valid {
		if err := ValidateSession(session); err != nil {
			t.Errorf("ValidateSession(%q) = %v, want nil", session, err)
		}
	}
	invalid := []string{"", "a/b", `a\b`, "..", "a..b", "a b", "a\tb", "a\nb", "a\x00b"}
	for _, session := range invalid {
		if err := ValidateSession(session); err == nil {
			t.Errorf("ValidateSession(%q) = nil, want error", session)
		}
	}
}

func TestProjectIdentityInsideGit(t *testing.T) {
	repository := initRepository(t)
	nested := filepath.Join(repository, "pkg", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "linked")
	runGit(t, repository, "worktree", "add", "-q", worktree)

	withoutOrigin, _, err := resolveProject(context.Background(), nested)
	if err != nil {
		t.Fatalf("resolveProject(no origin): %v", err)
	}
	runGit(t, repository, "remote", "add", "origin", "git@github.com:Acme/Widgets.git")
	fromNested, _, err := resolveProject(context.Background(), nested)
	if err != nil {
		t.Fatalf("resolveProject(nested): %v", err)
	}
	fromWorktree, _, err := resolveProject(context.Background(), worktree)
	if err != nil {
		t.Fatalf("resolveProject(worktree): %v", err)
	}

	if withoutOrigin != repository {
		t.Errorf("identity without origin = %q, want main worktree root %q", withoutOrigin, repository)
	}
	if fromNested != "github.com/Acme/Widgets" || fromWorktree != "github.com/Acme/Widgets" {
		t.Errorf("identities = %q, %q, want github.com/Acme/Widgets", fromNested, fromWorktree)
	}
}

func TestProjectIdentityInWorktreeWithoutOriginIsMainRoot(t *testing.T) {
	repository := initRepository(t)
	worktree := filepath.Join(t.TempDir(), "linked")
	runGit(t, repository, "worktree", "add", "-q", worktree)

	identity, _, err := resolveProject(context.Background(), worktree)
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}

	if identity != repository {
		t.Fatalf("identity = %q, want %q", identity, repository)
	}
}

func TestProjectIdentityWithLocalOriginFallsBackWithDiagnostic(t *testing.T) {
	repository := initRepository(t)
	runGit(t, repository, "remote", "add", "origin", "/srv/git/widgets.git")

	identity, diagnostics, err := resolveProject(context.Background(), repository)
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}

	if identity != repository || len(diagnostics) != 1 || !strings.Contains(diagnostics[0], "/srv/git/widgets.git") {
		t.Fatalf("identity = %q, diagnostics = %q", identity, diagnostics)
	}
}

func TestProjectIdentityOutsideGitIsWorkingDirectory(t *testing.T) {
	cwd := t.TempDir()

	identity, diagnostics, err := resolveProject(context.Background(), cwd)
	if err != nil {
		t.Fatalf("resolveProject: %v", err)
	}

	if identity != cwd || len(diagnostics) != 0 {
		t.Fatalf("identity = %q, diagnostics = %q, want %q and none", identity, diagnostics, cwd)
	}
}

func TestRemoteIdentity(t *testing.T) {
	tests := []struct {
		ok     bool
		remote string
		want   string
	}{
		{remote: "git@github.com:jmcampanini/esheep.git", want: "github.com/jmcampanini/esheep", ok: true},
		{remote: "GitHub.com:org/repo", want: "github.com/org/repo", ok: true},
		{remote: "https://github.com/jmcampanini/esheep.git", want: "github.com/jmcampanini/esheep", ok: true},
		{remote: "https://user:secret@GitLab.example.com:8443/group/sub/repo/", want: "gitlab.example.com/group/sub/repo", ok: true},
		{remote: "ssh://git@github.com:22/org/repo", want: "github.com/org/repo", ok: true},
		{remote: "git://host/org/repo.git", want: "host/org/repo", ok: true},
		{remote: "git+ssh://host/org/repo", want: "host/org/repo", ok: true},
		{remote: "file:///srv/git/repo.git"},
		{remote: "/srv/git/repo.git"},
		{remote: "../sibling"},
		{remote: "host:"},
		{remote: "https://host/"},
		{remote: "host:org/../repo"},
		{remote: ""},
	}
	for _, test := range tests {
		got, ok := RemoteIdentity(test.remote)
		if got != test.want || ok != test.ok {
			t.Errorf("RemoteIdentity(%q) = %q, %v, want %q, %v", test.remote, got, ok, test.want, test.ok)
		}
	}
}

func TestSegment(t *testing.T) {
	if got := Segment("github.com/acme/widgets"); got != "github.com-acme-widgets" {
		t.Errorf("Segment(git) = %q", got)
	}
	if got := Segment("/Users/me/scratch"); got != "-Users-me-scratch" {
		t.Errorf("Segment(path) = %q", got)
	}
}

func initRepository(t *testing.T) string {
	t.Helper()
	// git reports canonical paths, so resolve the macOS /var symlink up front.
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "init", "-q")
	runGit(t, repository, "-c", "user.name=t", "-c", "user.email=t@example.test", "commit", "-q", "--allow-empty", "-m", "init")
	return repository
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
