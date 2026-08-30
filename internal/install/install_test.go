package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/render"
	"github.com/jmcampanini/esheep/internal/skill"
)

func TestMarkerRequiresExactlyValidOwnershipFields(t *testing.T) {
	t.Parallel()
	valid := "source = \"personal\"\nskill = \"demo\"\ntarget = \"claude\"\n"
	tests := []struct {
		name string
		text string
	}{
		{name: "missing source", text: "skill = \"demo\"\ntarget = \"claude\"\n"},
		{name: "unknown field", text: valid + "hash = \"abc\"\n"},
		{name: "duplicate field", text: valid + "source = \"other\"\n"},
		{name: "wrong type", text: "source = 1\nskill = \"demo\"\ntarget = \"claude\"\n"},
		{name: "invalid source", text: "source = \"../personal\"\nskill = \"demo\"\ntarget = \"claude\"\n"},
		{name: "invalid skill", text: "source = \"personal\"\nskill = \"Demo\"\ntarget = \"claude\"\n"},
		{name: "invalid target", text: "source = \"personal\"\nskill = \"demo\"\ntarget = \"other\"\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseMarker([]byte(test.text)); err == nil {
				t.Fatalf("ParseMarker(%q) succeeded", test.text)
			}
		})
	}

	marker, err := ParseMarker([]byte("# managed\n" + valid))
	if err != nil {
		t.Fatal(err)
	}
	if marker.Source != "personal" || marker.Skill != "demo" || marker.Target != render.TargetClaude {
		t.Fatalf("marker = %#v", marker)
	}
}

func TestReconcileInstallsDetectsDriftAndRepairs(t *testing.T) {
	t.Parallel()
	request := installRequest(t, filepath.Join(t.TempDir(), "target"))

	installed, err := Reconcile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Action != ActionInstalled {
		t.Fatalf("action = %q, want %q", installed.Action, ActionInstalled)
	}
	marker, err := os.ReadFile(filepath.Join(request.Root, "demo", MarkerName))
	if err != nil {
		t.Fatal(err)
	}
	wantMarker := "source = \"personal\"\nskill = \"demo\"\ntarget = \"claude\"\n"
	if string(marker) != wantMarker {
		t.Fatalf("marker = %q, want %q", marker, wantMarker)
	}
	state, err := Inspect(context.Background(), request)
	if err != nil || state != StateSynced {
		t.Fatalf("Inspect() = %q, %v, want %q", state, err, StateSynced)
	}

	manifest := filepath.Join(request.Root, "demo", "SKILL.md")
	if err := os.WriteFile(manifest, []byte("drift"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = Inspect(context.Background(), request)
	if err != nil || state != StateDrifted {
		t.Fatalf("Inspect() after edit = %q, %v, want %q", state, err, StateDrifted)
	}

	repaired, err := Reconcile(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Action != ActionRepaired {
		t.Fatalf("action = %q, want %q", repaired.Action, ActionRepaired)
	}
	state, err = Inspect(context.Background(), request)
	if err != nil || state != StateSynced {
		t.Fatalf("Inspect() after repair = %q, %v, want %q", state, err, StateSynced)
	}
}

func TestReconcileRefusesUnownedDestinationsAndAliases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(*testing.T, Request)
	}{
		{
			name: "unmarked directory",
			setup: func(t *testing.T, request Request) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(request.Root, "demo"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(request.Root, "demo", "human"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "mismatched marker",
			setup: func(t *testing.T, request Request) {
				t.Helper()
				writeOwned(t, request.Root, Marker{Source: "other", Skill: "demo", Target: render.TargetClaude})
			},
		},
		{
			name: "symlinked destination",
			setup: func(t *testing.T, request Request) {
				t.Helper()
				if err := os.MkdirAll(request.Root, 0o755); err != nil {
					t.Fatal(err)
				}
				outside := t.TempDir()
				if err := os.Symlink(outside, filepath.Join(request.Root, "demo")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlinked marker",
			setup: func(t *testing.T, request Request) {
				t.Helper()
				directory := filepath.Join(request.Root, "demo")
				if err := os.MkdirAll(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "marker")
				if err := os.WriteFile(outside, []byte("source = \"personal\"\nskill = \"demo\"\ntarget = \"claude\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(directory, MarkerName)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "case alias",
			setup: func(t *testing.T, request Request) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(request.Root, "Demo"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := installRequest(t, filepath.Join(t.TempDir(), "target"))
			test.setup(t, request)

			before := snapshotDirectory(t, request.Root)
			result, err := Reconcile(context.Background(), request)
			if err == nil || result.Action != ActionBlocked {
				t.Fatalf("Reconcile() = %#v, %v, want blocked error", result, err)
			}
			state, inspectErr := Inspect(context.Background(), request)
			if inspectErr != nil || state != StateBlocked {
				t.Fatalf("Inspect() = %q, %v, want %q", state, inspectErr, StateBlocked)
			}
			after := snapshotDirectory(t, request.Root)
			if before != after {
				t.Fatalf("target changed:\nbefore: %s\nafter:  %s", before, after)
			}
		})
	}
}

func TestFreshInstallDoesNotReplaceDestinationThatAppearsBeforeCommit(t *testing.T) {
	t.Parallel()
	request := installRequest(t, t.TempDir())
	fsys := defaultFilesystem
	fsys.noReplace = func(root *targetRoot, oldName, newName string) error {
		if err := root.handle.WriteFile(newName, []byte("human"), 0o600); err != nil {
			t.Fatal(err)
		}
		return root.renameNoReplace(oldName, newName)
	}

	result, err := reconcile(context.Background(), request, fsys)
	if err == nil || result.Action != ActionBlocked {
		t.Fatalf("reconcile() = %#v, %v, want blocked error", result, err)
	}
	data, err := os.ReadFile(filepath.Join(request.Root, "demo"))
	if err != nil || string(data) != "human" {
		t.Fatalf("appeared destination = %q, %v", data, err)
	}
}

func TestReconcileRefusesSymlinkedTargetRoot(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	root := filepath.Join(t.TempDir(), "target")
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	request := installRequest(t, root)

	result, err := Reconcile(context.Background(), request)
	if err == nil || result.Action != ActionFailed {
		t.Fatalf("Reconcile() = %#v, %v, want failed error", result, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlink target contents = %#v", entries)
	}
}

func TestReconcileRefusesSymlinkedMissingTargetParent(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	request := installRequest(t, filepath.Join(t.TempDir(), "unused"))
	request.Root = filepath.Join(alias, "target")

	result, err := Reconcile(context.Background(), request)
	if err == nil || result.Action != ActionFailed {
		t.Fatalf("Reconcile() = %#v, %v, want failed error", result, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlinked parent contents = %#v", entries)
	}
}

func TestReconcileRefusesExistingTargetThroughSymlinkedAncestor(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "target"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	request := installRequest(t, filepath.Join(t.TempDir(), "unused"))
	request.Root = filepath.Join(alias, "target")

	result, err := Reconcile(context.Background(), request)
	if err == nil || result.Action != ActionFailed {
		t.Fatalf("Reconcile() = %#v, %v, want failed error", result, err)
	}
	entries, err := os.ReadDir(filepath.Join(outside, "target"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("redirected target was modified: %#v", entries)
	}
}

func TestFailedReplacementRestoresPriorDirectory(t *testing.T) {
	t.Parallel()
	request := installRequest(t, t.TempDir())
	if _, err := Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	before := snapshotDirectory(t, filepath.Join(request.Root, "demo"))
	request.Document.Body = []byte("changed\n")
	fsys := defaultFilesystem
	fsys.exchange = func(*targetRoot, string, string) error {
		return errors.New("injected exchange failure")
	}

	result, err := reconcile(context.Background(), request, fsys)
	if err == nil || result.Action != ActionFailed {
		t.Fatalf("reconcile() = %#v, %v, want failed error", result, err)
	}
	after := snapshotDirectory(t, filepath.Join(request.Root, "demo"))
	if before != after {
		t.Fatalf("installed directory changed:\nbefore: %s\nafter:  %s", before, after)
	}
	entries, err := os.ReadDir(request.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".esheep-txn-") {
			t.Fatalf("transaction directory remains: %q", entry.Name())
		}
	}
}

func TestTargetRootReplacementCannotRedirectSwap(t *testing.T) {
	t.Parallel()
	request := installRequest(t, t.TempDir())
	if _, err := Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	before := snapshotDirectory(t, filepath.Join(request.Root, "demo"))
	request.Document.Body = []byte("changed\n")
	outside := t.TempDir()
	originalRoot := request.Root + "-original"
	exchanges := 0
	fsys := defaultFilesystem
	fsys.exchange = func(root *targetRoot, oldName, newName string) error {
		exchanges++
		if exchanges == 1 {
			if err := os.Rename(request.Root, originalRoot); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, request.Root); err != nil {
				t.Fatal(err)
			}
		}
		return root.renameExchange(oldName, newName)
	}

	result, err := reconcile(context.Background(), request, fsys)
	if err == nil || result.Action != ActionFailed {
		t.Fatalf("reconcile() = %#v, %v, want failed error", result, err)
	}
	if after := snapshotDirectory(t, filepath.Join(originalRoot, "demo")); after != before {
		t.Fatalf("authorized installation changed:\nbefore: %s\nafter:  %s", before, after)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("replacement root was modified: %#v", entries)
	}
}

func TestPostCommitDestinationRemovalRestoresPriorInstallation(t *testing.T) {
	t.Parallel()
	request := installRequest(t, t.TempDir())
	if _, err := Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	before := snapshotDirectory(t, filepath.Join(request.Root, "demo"))
	request.Document.Body = []byte("changed\n")
	exchanges := 0
	fsys := defaultFilesystem
	fsys.exchange = func(root *targetRoot, oldName, newName string) error {
		exchanges++
		err := root.renameExchange(oldName, newName)
		if err == nil && exchanges == 1 {
			if err := root.handle.RemoveAll(newName); err != nil {
				t.Fatal(err)
			}
		}
		return err
	}

	result, err := reconcile(context.Background(), request, fsys)
	if err == nil || result.Action != ActionFailed {
		t.Fatalf("reconcile() = %#v, %v, want failed error", result, err)
	}
	if after := snapshotDirectory(t, filepath.Join(request.Root, "demo")); after != before {
		t.Fatalf("prior installation was not restored:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestPostCommitRootReplacementRestoresPriorInstallation(t *testing.T) {
	t.Parallel()
	request := installRequest(t, t.TempDir())
	if _, err := Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	before := snapshotDirectory(t, filepath.Join(request.Root, "demo"))
	request.Document.Body = []byte("changed\n")
	outside := t.TempDir()
	originalRoot := request.Root + "-original"
	exchanges := 0
	fsys := defaultFilesystem
	fsys.exchange = func(root *targetRoot, oldName, newName string) error {
		exchanges++
		err := root.renameExchange(oldName, newName)
		if err == nil && exchanges == 1 {
			if err := os.Rename(request.Root, originalRoot); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, request.Root); err != nil {
				t.Fatal(err)
			}
		}
		return err
	}

	result, err := reconcile(context.Background(), request, fsys)
	if err == nil || result.Action != ActionFailed {
		t.Fatalf("reconcile() = %#v, %v, want failed error", result, err)
	}
	if after := snapshotDirectory(t, filepath.Join(originalRoot, "demo")); after != before {
		t.Fatalf("prior installation was not restored:\nbefore: %s\nafter:  %s", before, after)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("replacement root was modified: %#v", entries)
	}
}

func TestPostCommitCleanupFailureKeepsNewInstallation(t *testing.T) {
	t.Parallel()
	request := installRequest(t, t.TempDir())
	fsys := filesystem{
		noReplace: func(root *targetRoot, oldName, newName string) error { return root.renameNoReplace(oldName, newName) },
		removeAll: func(*os.Root, string) error { return errors.New("injected cleanup failure") },
		rename:    func(root *os.Root, oldName, newName string) error { return root.Rename(oldName, newName) },
	}

	result, err := reconcile(context.Background(), request, fsys)
	if err == nil || result.Action != ActionInstalled {
		t.Fatalf("reconcile() = %#v, %v, want committed cleanup error", result, err)
	}
	requireCleanupError(t, err, request.Root, ".esheep-txn-")
	state, inspectErr := Inspect(context.Background(), request)
	if inspectErr != nil || state != StateSynced {
		t.Fatalf("Inspect() = %q, %v, want %q", state, inspectErr, StateSynced)
	}
}

func TestPostCommitCleanupFailureKeepsReplacement(t *testing.T) {
	t.Parallel()
	request := installRequest(t, t.TempDir())
	if _, err := Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	request.Document.Body = []byte("changed\n")
	fsys := defaultFilesystem
	fsys.removeAll = func(*os.Root, string) error { return errors.New("injected cleanup failure") }

	result, err := reconcile(context.Background(), request, fsys)
	if err == nil || result.Action != ActionRepaired {
		t.Fatalf("reconcile() = %#v, %v, want committed cleanup error", result, err)
	}
	requireCleanupError(t, err, request.Root, ".esheep-txn-")
	state, inspectErr := Inspect(context.Background(), request)
	if inspectErr != nil || state != StateSynced {
		t.Fatalf("Inspect() = %q, %v, want %q", state, inspectErr, StateSynced)
	}
}

func TestTreeComparisonStreamsLargeRegularFilesExactly(t *testing.T) {
	t.Parallel()
	left := t.TempDir()
	right := t.TempDir()
	leftPath := filepath.Join(left, "large")
	rightPath := filepath.Join(right, "large")
	contents := strings.Repeat("0123456789abcdef", 128*1024)
	if err := os.WriteFile(leftPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rightPath, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	equal, err := treesEqual(left, right)
	if err != nil || !equal {
		t.Fatalf("treesEqual() = %t, %v, want true", equal, err)
	}
	file, err := os.OpenFile(rightPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("different"), int64(len(contents)-9)); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	equal, err = treesEqual(left, right)
	if err != nil || equal {
		t.Fatalf("treesEqual() = %t, %v, want false", equal, err)
	}
}

func TestPostCommitCleanupFailureKeepsPrune(t *testing.T) {
	t.Parallel()
	root := canonicalTestPath(t, t.TempDir())
	writeOwned(t, root, Marker{Source: "removed", Skill: "stale", Target: render.TargetClaude})
	fsys := defaultFilesystem
	fsys.removeAll = func(*os.Root, string) error { return errors.New("injected cleanup failure") }

	results, err := prune(context.Background(), root, render.TargetClaude, func(Marker) bool { return true }, fsys)
	if len(results) != 1 || results[0].Action != ActionPruned || results[0].Identity.Skill != "stale" {
		t.Fatalf("prune() results = %#v, want committed prune", results)
	}
	requireCleanupError(t, err, root, ".esheep-prune-")
	if _, statErr := os.Stat(filepath.Join(root, "stale")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("committed prune destination still exists: %v", statErr)
	}
}

func TestPruneRemovesOnlyExactlyOwnedStaleInstallations(t *testing.T) {
	t.Parallel()
	root := canonicalTestPath(t, t.TempDir())
	writeOwned(t, root, Marker{Source: "removed", Skill: "stale", Target: render.TargetClaude})
	writeOwned(t, root, Marker{Source: "personal", Skill: "keep", Target: render.TargetClaude})
	writeOwned(t, root, Marker{Source: "personal", Skill: "other-target", Target: render.TargetPi})
	if err := os.Mkdir(filepath.Join(root, "unmarked"), 0o755); err != nil {
		t.Fatal(err)
	}
	malformed := filepath.Join(root, "malformed")
	if err := os.Mkdir(malformed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malformed, MarkerName), []byte("source = \"removed\"\nskill = \"malformed\"\ntarget = \"claude\"\nextra = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Prune(context.Background(), root, render.TargetClaude, func(marker Marker) bool {
		return marker.Source == "removed"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != ActionPruned || results[0].Identity.Skill != "stale" {
		t.Fatalf("results = %#v", results)
	}
	for _, name := range []string{"keep", "other-target", "unmarked", "malformed"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("preserved directory %q: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "stale")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale directory still exists: %v", err)
	}
}

func requireCleanupError(t *testing.T, err error, root, transactionPrefix string) {
	t.Helper()
	var cleanupErr *CleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("error = %v, want *CleanupError", err)
	}
	if filepath.Dir(cleanupErr.Path) != root || !strings.HasPrefix(filepath.Base(cleanupErr.Path), transactionPrefix) {
		t.Fatalf("cleanup path = %q, want %q transaction under %q", cleanupErr.Path, transactionPrefix, root)
	}
}

func installRequest(t *testing.T, targetRoot string) Request {
	t.Helper()
	targetRoot = canonicalTestPath(t, targetRoot)
	sourceRoot := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(sourceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: demo\ndescription: Demo skill\nesheep-targets: [claude, pi, codex, agents]\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := skill.Load(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	return Request{
		Document: loaded.Manifests[0].Document,
		Identity: Identity{Source: "personal", Skill: "demo", Target: render.TargetClaude},
		Package:  loaded,
		Root:     targetRoot,
	}
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, filepath.Base(path))
}

func writeOwned(t *testing.T, root string, marker Marker) {
	t.Helper()
	directory := filepath.Join(root, marker.Skill)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := MarshalMarker(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, MarkerName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "payload"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func snapshotDirectory(t *testing.T, root string) string {
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
		_, _ = snapshot.WriteString(filepath.ToSlash(relative) + " " + info.Mode().String())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = snapshot.WriteString(" " + string(data))
		}
		_ = snapshot.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.String()
}
