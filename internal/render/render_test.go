package render

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/jmcampanini/esheep/internal/skill"
)

func TestRenderExactTargetTrees(t *testing.T) {
	t.Parallel()
	source := skill.Package{
		Document: skill.Document{
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
		},
		Directories: []string{"empty", "nested"},
		Files:       []skill.File{{Path: "nested/data", Data: []byte{0, 1, 2}}},
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
	source := skill.Package{Document: skill.Document{
		Name:          "demo",
		Description:   "ok",
		License:       stringPointer(""),
		Compatibility: stringPointer(""),
		Metadata:      map[string]string{},
		Targets:       skill.Targets{Claude: skill.TargetOptions{ArgumentHint: stringPointer("")}},
	}}
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
	source := skill.Package{
		Document: skill.Document{Name: "demo", Description: "ok"},
		Files:    []skill.File{{Path: "support/.ESHEEP.TOML", Data: []byte("allowed")}},
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
	source := skill.Package{
		Document:    skill.Document{Name: "demo", Description: "ok"},
		Directories: []string{"explicit"},
		Files:       []skill.File{{Path: "implicit/data", Data: []byte("payload")}},
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
