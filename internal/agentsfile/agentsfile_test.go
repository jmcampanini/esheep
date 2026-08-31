package agentsfile

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

func TestParseName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		wantProfile string
		wantOK      bool
		wantErr     bool
	}{
		{name: "AGENTS.md", wantProfile: "", wantOK: true},
		{name: "AGENTS.work.md", wantProfile: "work", wantOK: true},
		{name: "AGENTS.Bad.md", wantErr: true},
		{name: "AGENTS.base.md", wantErr: true},
		{name: "AGENTS.md.bak", wantOK: false},
		{name: "agents.md", wantOK: false},
		{name: "README.md", wantOK: false},
		{name: "AGENTS.two.parts.md", wantOK: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, ok, err := ParseName(test.name)
			if (err != nil) != test.wantErr || ok != test.wantOK || profile != test.wantProfile {
				t.Fatalf("ParseName(%q) = %q, %t, %v, want %q, %t, error=%t", test.name, profile, ok, err, test.wantProfile, test.wantOK, test.wantErr)
			}
		})
	}
}

func TestDiscoverFindsAgentsFilesAndReportsInvalidNames(t *testing.T) {
	t.Parallel()
	first := t.TempDir()
	second := t.TempDir()
	writeAgentsFile(t, first, "AGENTS.md", "base")
	writeAgentsFile(t, first, "AGENTS.work.md", "work")
	writeAgentsFile(t, first, "notes.md", "ignored")
	writeAgentsFile(t, second, "AGENTS.Bad.md", "invalid")

	candidates, diagnostics := Discover([]Source{{Name: "first", Path: first}, {Name: "second", Path: second}})

	want := []Candidate{
		{Path: filepath.Join(first, "AGENTS.md"), Profile: "", Source: "first"},
		{Path: filepath.Join(first, "AGENTS.work.md"), Profile: "work", Source: "first"},
	}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("Discover candidates = %#v, want %#v", candidates, want)
	}
	if len(diagnostics) != 1 || diagnostics[0].Source != "second" || diagnostics[0].Path != filepath.Join(second, "AGENTS.Bad.md") {
		t.Fatalf("Discover diagnostics = %#v, want one invalid-name diagnostic", diagnostics)
	}
}

func TestDiscoverReportsUnresolvableAndIrregularEntries(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	if err := os.Symlink("missing", filepath.Join(source, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "AGENTS.work.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	candidates, diagnostics := Discover([]Source{{Name: "source", Path: source}})

	if len(candidates) != 0 || len(diagnostics) != 2 {
		t.Fatalf("Discover = %#v, %#v, want no candidates and two diagnostics", candidates, diagnostics)
	}
}

func TestDiscoverFollowsResolvableSymlinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real.md"), []byte("linked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "real.md"), filepath.Join(source, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	candidates, diagnostics := Discover([]Source{{Name: "source", Path: source}})

	if len(diagnostics) != 0 || len(candidates) != 1 || candidates[0].Profile != "" {
		t.Fatalf("Discover = %#v, %#v, want the linked candidate", candidates, diagnostics)
	}
}

func TestSelectWalksProfilesThenBaseWithUniquenessPerTier(t *testing.T) {
	t.Parallel()
	base := Candidate{Path: "/one/AGENTS.md", Source: "one"}
	work := Candidate{Path: "/one/AGENTS.work.md", Profile: "work", Source: "one"}
	client := Candidate{Path: "/two/AGENTS.client.md", Profile: "client", Source: "two"}
	duplicate := Candidate{Path: "/two/AGENTS.work.md", Profile: "work", Source: "two"}
	tests := []struct {
		name       string
		candidates []Candidate
		profiles   []string
		want       Candidate
		wantFound  bool
		wantErr    bool
	}{
		{name: "no candidates", wantFound: false},
		{name: "base only", candidates: []Candidate{base}, want: base, wantFound: true},
		{name: "first active profile wins", candidates: []Candidate{base, work, client}, profiles: []string{"work", "client"}, want: work, wantFound: true},
		{name: "empty tier falls through", candidates: []Candidate{base, client}, profiles: []string{"work", "client"}, want: client, wantFound: true},
		{name: "all tiers empty falls to base", candidates: []Candidate{base}, profiles: []string{"work"}, want: base, wantFound: true},
		{name: "inactive variant never matches", candidates: []Candidate{work}, wantFound: false},
		{name: "duplicate tier is an error", candidates: []Candidate{work, duplicate, base}, profiles: []string{"work"}, wantErr: true},
		{name: "duplicate base is an error", candidates: []Candidate{base, {Path: "/two/AGENTS.md", Source: "two"}}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selection, err := Select(test.candidates, test.profiles)
			if (err != nil) != test.wantErr {
				t.Fatalf("Select() error = %v, want error=%t", err, test.wantErr)
			}
			if selection.Found != test.wantFound || (test.wantFound && selection.Candidate != test.want) {
				t.Fatalf("Select() = %#v, want found=%t candidate=%#v", selection, test.wantFound, test.want)
			}
		})
	}
}

func TestDeployInstallsRepairsAndLeavesMatchingContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	destination := filepath.Join(canonicalTempDir(t), "nested", "CLAUDE.md")

	outcome, err := Deploy(ctx, []byte("first"), destination)
	if err != nil || outcome != OutcomeInstalled {
		t.Fatalf("Deploy(fresh) = %q, %v, want installed", outcome, err)
	}
	assertFileContent(t, destination, "first")
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("deployed mode = %v, %v, want 0644", info, err)
	}

	outcome, err = Deploy(ctx, []byte("first"), destination)
	if err != nil || outcome != OutcomeUnchanged {
		t.Fatalf("Deploy(same) = %q, %v, want unchanged", outcome, err)
	}

	outcome, err = Deploy(ctx, []byte("second"), destination)
	if err != nil || outcome != OutcomeRepaired {
		t.Fatalf("Deploy(changed) = %q, %v, want repaired", outcome, err)
	}
	assertFileContent(t, destination, "second")

	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 1 {
		t.Fatalf("destination directory entries = %v, %v, want only the agents file", entries, err)
	}
}

func TestDeployOverwritesForeignContent(t *testing.T) {
	t.Parallel()
	destination := filepath.Join(canonicalTempDir(t), "CLAUDE.md")
	if err := os.WriteFile(destination, []byte("handwritten"), 0o600); err != nil {
		t.Fatal(err)
	}

	outcome, err := Deploy(context.Background(), []byte("managed"), destination)

	if err != nil || outcome != OutcomeRepaired {
		t.Fatalf("Deploy(foreign) = %q, %v, want repaired", outcome, err)
	}
	assertFileContent(t, destination, "managed")
}

func TestDeployFailsWhenDestinationIsADirectory(t *testing.T) {
	t.Parallel()
	destination := filepath.Join(canonicalTempDir(t), "CLAUDE.md")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Deploy(context.Background(), []byte("content"), destination); err == nil {
		t.Fatal("Deploy(directory) succeeded")
	}
}

func TestInspectAndDeployRejectSymlinkedDestination(t *testing.T) {
	t.Parallel()
	root := canonicalTempDir(t)
	realDestination := filepath.Join(root, "real.md")
	if err := os.WriteFile(realDestination, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "AGENTS.md")
	if err := os.Symlink(realDestination, destination); err != nil {
		t.Fatal(err)
	}

	assertOperationsReject(t, destination)
	assertFileContent(t, realDestination, "content")
}

func TestInspectAndDeployRejectSymlinkedParent(t *testing.T) {
	t.Parallel()
	root := canonicalTempDir(t)
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, alias); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(alias, "AGENTS.md")

	assertOperationsReject(t, destination)
	if _, err := os.Stat(filepath.Join(realParent, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("symlinked parent destination was created: %v", err)
	}
}

func TestInspectAndDeployRejectFIFO(t *testing.T) {
	t.Parallel()
	destination := filepath.Join(canonicalTempDir(t), "AGENTS.md")
	if err := syscall.Mkfifo(destination, 0o600); err != nil {
		t.Fatal(err)
	}

	assertOperationsReject(t, destination)
}

func TestInspectReportsMissingStaleAndSynced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	destination := filepath.Join(canonicalTempDir(t), "AGENTS.md")

	state, err := Inspect(ctx, []byte("content"), destination)
	if err != nil || state != StateMissing {
		t.Fatalf("Inspect(missing) = %q, %v, want missing", state, err)
	}

	if err := os.WriteFile(destination, []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = Inspect(ctx, []byte("content"), destination)
	if err != nil || state != StateStale {
		t.Fatalf("Inspect(differs) = %q, %v, want stale", state, err)
	}

	if err := os.WriteFile(destination, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = Inspect(ctx, []byte("content"), destination)
	if err != nil || state != StateSynced {
		t.Fatalf("Inspect(equal) = %q, %v, want synced", state, err)
	}
}

func assertOperationsReject(t *testing.T, destination string) {
	t.Helper()
	operations := []struct {
		name string
		run  func() error
	}{
		{name: "inspect", run: func() error {
			_, err := Inspect(context.Background(), []byte("content"), destination)
			return err
		}},
		{name: "deploy", run: func() error {
			_, err := Deploy(context.Background(), []byte("replacement"), destination)
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			result := make(chan error, 1)
			go func() { result <- operation.run() }()
			select {
			case err := <-result:
				if err == nil {
					t.Fatalf("%s(%q) succeeded", operation.name, destination)
				}
			case <-time.After(time.Second):
				t.Fatalf("%s(%q) blocked", operation.name, destination)
			}
		})
	}
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func writeAgentsFile(t *testing.T, source, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("content of %q = %q, want %q", path, data, want)
	}
}
