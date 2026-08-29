package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/jmcampanini/esheep/internal/skill"
)

func TestDiscoverUsesSourceThenLexicalChildOrderWithoutDescending(t *testing.T) {
	t.Parallel()
	first := t.TempDir()
	second := t.TempDir()
	writeSkill(t, filepath.Join(first, "z-last"), "z-last", "valid")
	writeSkill(t, filepath.Join(first, "a-first"), "a-first", "valid")
	writeSkill(t, filepath.Join(first, "container", "nested"), "nested", "not immediate")
	writeSkill(t, filepath.Join(first, ".hidden"), ".hidden", "ignored")
	writeSkill(t, filepath.Join(first, "node_modules"), "node_modules", "ignored")
	writeSkill(t, filepath.Join(second, "b-second-source"), "b-second-source", "valid")
	writeManifest(t, filepath.Join(first, "SKILL.md"), "root", "root is not a skill")
	manifestLink := filepath.Join(first, "manifest-link")
	if err := os.Mkdir(manifestLink, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(first, "a-first", "SKILL.md"), filepath.Join(manifestLink, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(first, "a-first"), filepath.Join(first, "directory-link")); err != nil {
		t.Fatal(err)
	}

	catalog := Discover([]Source{{Name: "first", Path: first}, {Name: "second", Path: second}})
	var locations []string
	for _, candidate := range catalog.Candidates {
		locations = append(locations, candidate.Location.Source+"/"+candidate.Location.RelativePath)
	}
	want := []string{"first/a-first", "first/z-last", "second/b-second-source"}
	if !reflect.DeepEqual(locations, want) {
		t.Fatalf("candidates = %#v, want %#v", locations, want)
	}
	if len(catalog.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", catalog.Diagnostics)
	}
}

func TestDiscoverValidatesSupportingFilesAndSymlinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	valid := filepath.Join(root, "valid")
	writeSkill(t, valid, "valid", "valid")
	if err := os.WriteFile(filepath.Join(valid, "data.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data.txt", filepath.Join(valid, "copy.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("copy.txt", filepath.Join(valid, "chain.txt")); err != nil {
		t.Fatal(err)
	}

	invalid := filepath.Join(root, "invalid")
	writeSkill(t, invalid, "invalid", "invalid")
	links := map[string]string{
		"absolute":       filepath.Join(root, "valid", "data.txt"),
		"broken":         "missing",
		"directory-link": ".",
		"escape":         "../../outside",
		"cycle-a":        "cycle-b",
		"cycle-b":        "cycle-a",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(invalid, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(invalid, "named-pipe"), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog := Discover([]Source{{Name: "source", Path: root}})
	if len(catalog.Candidates) != 2 {
		t.Fatalf("candidate count = %d", len(catalog.Candidates))
	}
	var validCandidate, invalidCandidate Candidate
	for _, candidate := range catalog.Candidates {
		switch candidate.Location.RelativePath {
		case "valid":
			validCandidate = candidate
		case "invalid":
			invalidCandidate = candidate
		}
	}
	if !validCandidate.Valid() {
		t.Fatalf("valid candidate diagnostics = %#v", validCandidate.Diagnostics)
	}
	for _, path := range []string{"copy.txt", "chain.txt"} {
		if got := supportingBytes(validCandidate.Package.Files, path); string(got) != "payload" {
			t.Fatalf("%s dereferenced bytes = %q", path, got)
		}
	}
	if invalidCandidate.Valid() || countSkillCode(invalidCandidate.Diagnostics, skill.CodeInvalidSymlink) != len(links) {
		t.Fatalf("invalid candidate diagnostics = %#v", invalidCandidate.Diagnostics)
	}
	if countSkillCode(invalidCandidate.Diagnostics, skill.CodeUnsupportedFile) != 1 {
		t.Fatalf("special-file diagnostics = %#v", invalidCandidate.Diagnostics)
	}
	for _, diagnostic := range catalog.Diagnostics {
		if diagnostic.SkillCode == skill.CodeInvalidSymlink && diagnostic.Path == "escape" {
			return
		}
	}
	t.Fatal("discovery did not preserve supporting-file diagnostic path")
}

func TestDiscoverRejectsCaseInsensitiveRootReservedNameOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reserved := filepath.Join(root, "reserved")
	writeSkill(t, reserved, "reserved", "valid")
	if err := os.WriteFile(filepath.Join(reserved, ".ESHEEP.TOML"), []byte("reserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	writeSkill(t, nested, "nested", "valid")
	if err := os.Mkdir(filepath.Join(nested, "support"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "support", ".EsHeEp.ToMl"), []byte("allowed"), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog := Discover([]Source{{Name: "source", Path: root}})
	if len(catalog.ValidCandidates()) != 1 || catalog.ValidCandidates()[0].Package.Document.Name != "nested" {
		t.Fatalf("valid candidates = %#v", catalog.ValidCandidates())
	}
	if got := supportingBytes(catalog.ValidCandidates()[0].Package.Files, "support/.EsHeEp.ToMl"); string(got) != "allowed" {
		t.Fatalf("nested reserved-name bytes = %q", got)
	}
	for _, diagnostic := range catalog.Diagnostics {
		if diagnostic.SkillCode == skill.CodeReservedPath && diagnostic.Path == ".ESHEEP.TOML" {
			return
		}
	}
	t.Fatal("root reserved-path diagnostic missing")
}

func TestDiscoverCollidesValidIdentitiesDespiteOtherValidationErrors(t *testing.T) {
	t.Parallel()
	first := t.TempDir()
	second := t.TempDir()
	writeSkill(t, filepath.Join(first, "same"), "same", "valid")
	writeSkill(t, filepath.Join(second, "same"), "same", "   ")
	writeSkill(t, filepath.Join(second, "other"), "other", "valid")

	catalog := Discover([]Source{{Name: "first", Path: first}, {Name: "second", Path: second}})
	if len(catalog.ValidCandidates()) != 1 || catalog.ValidCandidates()[0].Package.Document.Name != "other" {
		t.Fatalf("valid candidates = %#v", catalog.ValidCandidates())
	}
	for _, candidate := range catalog.Candidates {
		if candidate.Package.Document.Name == "same" && !candidate.Colliding {
			t.Fatalf("collision not marked: %#v", candidate)
		}
	}
	for _, diagnostic := range catalog.Diagnostics {
		if diagnostic.Code == CodeCollision {
			if len(diagnostic.Collisions) != 2 || diagnostic.Collisions[0].Source != "first" || diagnostic.Collisions[1].Source != "second" {
				t.Fatalf("collision locations = %#v", diagnostic.Collisions)
			}
			return
		}
	}
	t.Fatal("collision diagnostic missing")
}

func TestDiscoverReportsMissingAndNonDirectorySourcesInInputOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := Discover([]Source{{Name: "file", Path: file}, {Name: "missing", Path: filepath.Join(root, "missing")}})
	if len(catalog.Diagnostics) != 2 || catalog.Diagnostics[0].Code != CodeSourceUnavailable || catalog.Diagnostics[0].Location.Source != "file" || catalog.Diagnostics[1].Location.Source != "missing" {
		t.Fatalf("diagnostics = %#v", catalog.Diagnostics)
	}
}

func TestDiscoverDoesNotOpenSpecialSourceRoot(t *testing.T) {
	t.Parallel()
	fifo := filepath.Join(t.TempDir(), "source-fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	result := make(chan Catalog, 1)
	go func() {
		result <- Discover([]Source{{Name: "fifo", Path: fifo}})
	}()
	select {
	case catalog := <-result:
		if len(catalog.Diagnostics) != 1 || catalog.Diagnostics[0].Code != CodeSourceUnavailable {
			t.Fatalf("diagnostics = %#v", catalog.Diagnostics)
		}
	case <-time.After(time.Second):
		t.Fatal("discovery blocked opening a special source root")
	}
}

func writeSkill(t *testing.T, root, name, description string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, filepath.Join(root, "SKILL.md"), name, description)
}

func writeManifest(t *testing.T, path, name, description string) {
	t.Helper()
	contents := "---\nname: " + name + "\ndescription: '" + description + "'\n---\n# Body\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func supportingBytes(files []skill.File, path string) []byte {
	for _, file := range files {
		if file.Path == path {
			return file.Data
		}
	}
	return nil
}

func countSkillCode(diagnostics []skill.Diagnostic, code skill.Code) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			count++
		}
	}
	return count
}
