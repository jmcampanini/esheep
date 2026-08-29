package render

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jmcampanini/esheep/internal/skill"
)

func TestRenderExactTargetTrees(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillManifest(t, root)
	if err := os.WriteFile(filepath.Join(root, "nested", "data"), []byte{0, 1, 2}, 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := skill.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	source.Document = skill.Document{
		Name:          "demo",
		Description:   "A demo",
		License:       stringPointer("MIT"),
		Compatibility: stringPointer("macOS and Linux"),
		Metadata:      map[string]string{"z": "last", "a": "first"},
		Targets: skill.Targets{
			Claude: skill.TargetOptions{ArgumentHint: stringPointer("CLAUDE-ARG")},
			Pi:     skill.TargetOptions{ArgumentHint: stringPointer("PI-ARG")},
		},
		Body: []byte("# Body\r\nexact\x00"),
	}
	tests := []struct {
		target Target
		golden string
	}{
		{target: TargetClaude, golden: "claude.golden"},
		{target: TargetPi, golden: "pi.golden"},
		{target: TargetCodex, golden: "common.golden"},
		{target: TargetAgents, golden: "common.golden"},
	}
	for _, test := range tests {
		t.Run(string(test.target), func(t *testing.T) {
			t.Parallel()
			staging := t.TempDir()
			rendered, err := Render(staging, source, test.target)
			if err != nil {
				t.Fatal(err)
			}
			if !rendered {
				t.Fatal("Render reported disabled")
			}
			got, err := os.ReadFile(filepath.Join(staging, "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", test.golden))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Fatalf("SKILL.md bytes:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestRenderPreservesEmptyOptionalFields(t *testing.T) {
	t.Parallel()
	source := loadManifestOnlyPackage(t)
	source.Document = skill.Document{
		Name:          "demo",
		Description:   "ok",
		License:       stringPointer(""),
		Compatibility: stringPointer(""),
		Metadata:      map[string]string{},
		Targets:       skill.Targets{Claude: skill.TargetOptions{ArgumentHint: stringPointer("")}},
	}
	claude := t.TempDir()
	if _, err := Render(claude, source, TargetClaude); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(claude, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nname: demo\ndescription: ok\nlicense: \"\"\ncompatibility: \"\"\nmetadata: {}\nargument-hint: \"\"\n---\n"
	if string(manifest) != want {
		t.Fatalf("Claude manifest = %q, want %q", manifest, want)
	}

	codex := t.TempDir()
	if _, err := Render(codex, source, TargetCodex); err != nil {
		t.Fatal(err)
	}
	manifest, err = os.ReadFile(filepath.Join(codex, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "argument-hint") {
		t.Fatalf("Codex manifest contains Claude hint: %q", manifest)
	}
}

func TestRenderDisabledTargetLeavesStagingUntouched(t *testing.T) {
	t.Parallel()
	staging := t.TempDir()
	source := skill.Package{Document: skill.Document{Targets: skill.Targets{Codex: skill.TargetOptions{Disabled: true}}}}
	rendered, err := Render(staging, source, TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatal(err)
	}
	if rendered || len(entries) != 0 {
		t.Fatalf("rendered = %t, entries = %#v", rendered, entries)
	}
}

func TestRenderRejectsInvalidConstructedTrees(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		directories []string
		files       []skill.File
	}{
		{name: "traversal", files: []skill.File{{Path: "../escape"}}},
		{name: "absolute", files: []skill.File{{Path: "/escape"}}},
		{name: "duplicate files", files: []skill.File{{Path: "same"}, {Path: "same"}}},
		{name: "duplicate directories", directories: []string{"same", "same"}},
		{name: "generated manifest", files: []skill.File{{Path: "SKILL.md"}}},
		{name: "manifest case collision", files: []skill.File{{Path: "skill.md"}}},
		{name: "nested case duplicate", files: []skill.File{{Path: "Docs/Guide.md"}, {Path: "docs/guide.MD"}}},
		{name: "canonical Unicode duplicate", files: []skill.File{{Path: "café"}, {Path: "cafe\u0301"}}},
		{name: "reserved file", files: []skill.File{{Path: ".ESHEEP.TOML"}}},
		{name: "reserved directory", directories: []string{".EsHeEp.ToMl"}},
		{name: "file contains path", directories: []string{"parent/child"}, files: []skill.File{{Path: "parent"}}},
		{name: "case-folded file contains directory", directories: []string{"Docs/Child"}, files: []skill.File{{Path: "docs"}}},
		{name: "path descends through file", files: []skill.File{{Path: "parent"}, {Path: "parent/child"}}},
		{name: "case-folded path descends through file", files: []skill.File{{Path: "Docs"}, {Path: "docs/child"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := skill.Package{Document: skill.Document{Name: "demo", Description: "ok"}, Directories: test.directories, Files: test.files}
			staging := t.TempDir()
			before, err := os.Stat(staging)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Render(staging, source, TargetClaude); err == nil {
				t.Fatal("invalid tree rendered")
			}
			after, err := os.Stat(staging)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(staging)
			if err != nil {
				t.Fatal(err)
			}
			if before.Mode().Perm() != after.Mode().Perm() || len(entries) != 0 {
				t.Fatalf("staging changed: mode %04o to %04o, entries %#v", before.Mode().Perm(), after.Mode().Perm(), entries)
			}
		})
	}
}

func TestRenderAllowsNestedReservedName(t *testing.T) {
	t.Parallel()
	staging := t.TempDir()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(filepath.Join(root, "support"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "support", ".ESHEEP.TOML"), []byte("allowed"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSkillManifest(t, root)
	source, err := skill.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Render(staging, source, TargetClaude); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(staging, "support", ".ESHEEP.TOML"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "allowed" {
		t.Fatalf("nested data = %q", data)
	}
}

func TestRenderRejectsNonemptyStaging(t *testing.T) {
	t.Parallel()
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, "existing"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	source := skill.Package{Document: skill.Document{Name: "demo", Description: "ok"}}
	if _, err := Render(staging, source, TargetClaude); err == nil {
		t.Fatal("nonempty staging rendered")
	}
}

func TestRenderCleansStagingAndNormalizesModesDespiteUmask(t *testing.T) {
	oldUmask := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(oldUmask) })
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(parent, "staging")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceRoot := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(filepath.Join(sourceRoot, "implicit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "implicit", "data"), []byte("payload"), 0o711); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sourceRoot, "explicit"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillManifest(t, sourceRoot)
	source, err := skill.Load(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Render(staging+string(filepath.Separator), source, TargetClaude); err != nil {
		t.Fatal(err)
	}
	assertMode(t, parent, 0o700)
	assertMode(t, staging, 0o755)
	assertMode(t, filepath.Join(staging, "explicit"), 0o755)
	assertMode(t, filepath.Join(staging, "implicit"), 0o755)
	assertMode(t, filepath.Join(staging, "SKILL.md"), 0o644)
	assertMode(t, filepath.Join(staging, "implicit", "data"), 0o644)
}

func TestRenderStreamsCurrentRegularAndWithinRootSymlinkData(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillManifest(t, root)
	dataPath := filepath.Join(root, "data")
	if err := os.WriteFile(dataPath, []byte("validated"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	loaded, err := skill.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataPath, []byte("rendered"), 0o755); err != nil {
		t.Fatal(err)
	}

	staging := t.TempDir()
	if _, err := Render(staging, loaded, TargetClaude); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"data", "alias"} {
		got, err := os.ReadFile(filepath.Join(staging, relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "rendered" {
			t.Fatalf("%s = %q, want current source data", relative, got)
		}
		assertMode(t, filepath.Join(staging, relative), 0o644)
	}
}

func TestRenderRejectsSupportFileReplacedByDirectory(t *testing.T) {
	t.Parallel()
	root, loaded := loadRenderSkill(t)
	path := filepath.Join(root, "data")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(t.TempDir(), loaded, TargetClaude); err == nil {
		t.Fatal("Render accepted a support file replaced by a directory")
	}
}

func TestRenderRejectsSupportFileReplacedByFIFOWithoutBlocking(t *testing.T) {
	t.Parallel()
	root, loaded := loadRenderSkill(t)
	path := filepath.Join(root, "data")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()
	result := make(chan error, 1)
	go func() {
		_, err := Render(staging, loaded, TargetClaude)
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Render accepted a support file replaced by a FIFO")
		}
	case <-time.After(time.Second):
		t.Fatal("Render blocked opening a support FIFO")
	}
}

func TestRenderRejectsSupportSymlinkChangedToEscape(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	root := filepath.Join(parent, "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillManifest(t, root)
	if err := os.WriteFile(filepath.Join(root, "data"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink("data", link); err != nil {
		t.Fatal(err)
	}
	loaded, err := skill.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside", link); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(t.TempDir(), loaded, TargetClaude); err == nil {
		t.Fatal("Render followed an escaping support symlink")
	}
}

func TestRenderRejectsReplacedSkillOrSourceDirectoryBeforeStagingMutation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		replace func(*testing.T, string, string)
	}{
		{
			name: "outside symlink",
			replace: func(t *testing.T, source, root string) {
				t.Helper()
				if err := os.Rename(root, root+"-original"); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(filepath.Dir(source), "outside")
				if err := os.Mkdir(outside, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(outside, "data"), []byte("outside"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, root); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "different in-source directory",
			replace: func(t *testing.T, _ string, root string) {
				t.Helper()
				if err := os.Rename(root, root+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(root, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "data"), []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "replaced source parent",
			replace: func(t *testing.T, source, _ string) {
				t.Helper()
				if err := os.Rename(source, source+"-original"); err != nil {
					t.Fatal(err)
				}
				root := filepath.Join(source, "demo")
				if err := os.MkdirAll(root, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "data"), []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			source := filepath.Join(parent, "source")
			root := filepath.Join(source, "demo")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			writeSkillManifest(t, root)
			if err := os.WriteFile(filepath.Join(root, "data"), []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := skill.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			test.replace(t, source, root)

			staging := t.TempDir()
			if err := os.Chmod(staging, 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := Render(staging, loaded, TargetClaude); err == nil {
				t.Fatal("Render accepted a replaced source identity")
			}
			entries, err := os.ReadDir(staging)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("staging entries = %#v", entries)
			}
			assertMode(t, staging, 0o700)
		})
	}
}

func loadManifestOnlyPackage(t *testing.T) skill.Package {
	t.Helper()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillManifest(t, root)
	loaded, err := skill.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func loadRenderSkill(t *testing.T) (string, skill.Package) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkillManifest(t, root)
	if err := os.WriteFile(filepath.Join(root, "data"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := skill.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, loaded
}

func writeSkillManifest(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: demo\ndescription: ok\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

func stringPointer(value string) *string {
	return &value
}
