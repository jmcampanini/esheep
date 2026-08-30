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
	parent := t.TempDir()
	root := filepath.Join(parent, "source")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if err := os.Symlink("../../outside.txt", filepath.Join(valid, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(parent, "outside.txt"), filepath.Join(valid, "absolute.txt")); err != nil {
		t.Fatal(err)
	}

	invalid := filepath.Join(root, "invalid")
	writeSkill(t, invalid, "invalid", "invalid")
	links := map[string]string{
		"broken":      "missing",
		"cycle-a":     "cycle-b",
		"cycle-b":     "cycle-a",
		"self-parent": ".",
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
	if validCandidate.Package.Root != valid {
		t.Fatalf("package root = %q, want %q", validCandidate.Package.Root, valid)
	}
	for _, path := range []string{"data.txt", "copy.txt", "chain.txt", "escape.txt", "absolute.txt"} {
		if !hasSupportingPath(validCandidate.Package.Files, path) {
			t.Fatalf("support path %q missing from %#v", path, validCandidate.Package.Files)
		}
	}
	if invalidCandidate.Valid() || countSkillCode(invalidCandidate.Diagnostics, skill.CodeUnreadable) != len(links) {
		t.Fatalf("invalid candidate diagnostics = %#v", invalidCandidate.Diagnostics)
	}
	if countSkillCode(invalidCandidate.Diagnostics, skill.CodeUnsupportedFile) != 1 {
		t.Fatalf("special-file diagnostics = %#v", invalidCandidate.Diagnostics)
	}
	for _, diagnostic := range catalog.Diagnostics {
		if diagnostic.SkillCode == skill.CodeUnreadable && diagnostic.Path == "broken" {
			return
		}
	}
	t.Fatal("discovery did not preserve supporting-file diagnostic path")
}

func TestDiscoverFollowsSymlinkedSkillDirectories(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	overlay := filepath.Join(parent, "overlay")
	writeSkill(t, filepath.Join(overlay, "linked"), "linked", "overlay skill")
	root := filepath.Join(parent, "source")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "overlay", "linked"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "overlay", "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "note.md"), []byte("not a skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "note.md"), filepath.Join(root, "note.md")); err != nil {
		t.Fatal(err)
	}

	catalog := Discover([]Source{{Name: "source", Path: root}})

	if len(catalog.Candidates) != 2 {
		t.Fatalf("candidates = %#v, want linked and dangling", catalog.Candidates)
	}
	var linkedCandidate, danglingCandidate Candidate
	for _, candidate := range catalog.Candidates {
		switch candidate.Location.RelativePath {
		case "linked":
			linkedCandidate = candidate
		case "dangling":
			danglingCandidate = candidate
		default:
			t.Fatalf("unexpected candidate %q", candidate.Location.RelativePath)
		}
	}
	if !linkedCandidate.Valid() || linkedCandidate.Package.Manifests[0].Document.Name != "linked" {
		t.Fatalf("linked candidate = %#v", linkedCandidate)
	}
	if danglingCandidate.Valid() || countSkillCode(danglingCandidate.Diagnostics, skill.CodeUnreadable) == 0 {
		t.Fatalf("dangling candidate diagnostics = %#v", danglingCandidate.Diagnostics)
	}
}

func TestDiscoverRejectsCaseCollidingSupportPathsWhenFilesystemRepresentsThem(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	root := filepath.Join(source, "demo")
	writeSkill(t, root, "demo", "valid")
	if err := os.WriteFile(filepath.Join(root, "Guide"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := os.OpenFile(filepath.Join(root, "guide"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if os.IsExist(err) {
		t.Skip("filesystem cannot represent case-colliding fixture")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	catalog := Discover([]Source{{Name: "source", Path: source}})
	if len(catalog.Candidates) != 1 || catalog.Candidates[0].Valid() || countSkillCode(catalog.Candidates[0].Diagnostics, skill.CodePathCollision) != 1 {
		t.Fatalf("catalog = %#v", catalog)
	}
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
	if len(catalog.ValidCandidates()) != 1 || catalog.ValidCandidates()[0].Package.Manifests[0].Document.Name != "nested" {
		t.Fatalf("valid candidates = %#v", catalog.ValidCandidates())
	}
	if !hasSupportingPath(catalog.ValidCandidates()[0].Package.Files, "support/.EsHeEp.ToMl") {
		t.Fatalf("nested support paths = %#v", catalog.ValidCandidates()[0].Package.Files)
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
	if len(catalog.ValidCandidates()) != 1 || catalog.ValidCandidates()[0].Package.Manifests[0].Document.Name != "other" {
		t.Fatalf("valid candidates = %#v", catalog.ValidCandidates())
	}
	for _, candidate := range catalog.Candidates {
		if candidate.Package.Manifests[0].Document.Name == "same" && !candidate.Colliding {
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

func TestDiscoverReportsPresentInvalidManifests(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	directoryManifest := filepath.Join(root, "directory-manifest")
	if err := os.MkdirAll(filepath.Join(directoryManifest, "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	fifoManifest := filepath.Join(root, "fifo-manifest")
	if err := os.Mkdir(fifoManifest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(fifoManifest, "SKILL.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkManifest := filepath.Join(root, "symlink-manifest")
	if err := os.Mkdir(symlinkManifest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(symlinkManifest, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	want := map[string]skill.Code{
		"directory-manifest": skill.CodeUnsupportedFile,
		"fifo-manifest":      skill.CodeUnsupportedFile,
		"symlink-manifest":   skill.CodeUnreadable,
	}
	if os.Geteuid() != 0 {
		unreadableManifest := filepath.Join(root, "unreadable-manifest")
		if err := os.Mkdir(unreadableManifest, 0o755); err != nil {
			t.Fatal(err)
		}
		writeManifest(t, filepath.Join(unreadableManifest, "SKILL.md"), "unreadable-manifest", "valid")
		if err := os.Chmod(filepath.Join(unreadableManifest, "SKILL.md"), 0); err != nil {
			t.Fatal(err)
		}
		want["unreadable-manifest"] = skill.CodeUnreadable
	}

	catalog := Discover([]Source{{Name: "source", Path: root}})
	if len(catalog.Candidates) != len(want) || len(catalog.Diagnostics) != len(want) {
		t.Fatalf("candidates = %#v, diagnostics = %#v", catalog.Candidates, catalog.Diagnostics)
	}
	for _, candidate := range catalog.Candidates {
		code, exists := want[candidate.Location.RelativePath]
		if !exists || countSkillCode(candidate.Diagnostics, code) != 1 {
			t.Fatalf("candidate = %#v, want diagnostic %q", candidate, code)
		}
	}
}

func TestDiscoverRecognizesProfileVariantManifests(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	variantOnly := filepath.Join(root, "variant-only")
	if err := os.Mkdir(variantOnly, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, filepath.Join(variantOnly, "SKILL.work.md"), "variant-only", "work only")
	invalidVariant := filepath.Join(root, "invalid-variant")
	if err := os.Mkdir(invalidVariant, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, filepath.Join(invalidVariant, "SKILL.Bad.md"), "invalid-variant", "bad segment")
	noManifest := filepath.Join(root, "no-manifest")
	if err := os.Mkdir(noManifest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noManifest, "notes.md"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog := Discover([]Source{{Name: "source", Path: root}})

	if len(catalog.Candidates) != 2 {
		t.Fatalf("candidates = %#v, want variant-only and invalid-variant", catalog.Candidates)
	}
	var variantCandidate, invalidCandidate Candidate
	for _, candidate := range catalog.Candidates {
		switch candidate.Location.RelativePath {
		case "variant-only":
			variantCandidate = candidate
		case "invalid-variant":
			invalidCandidate = candidate
		default:
			t.Fatalf("unexpected candidate %q", candidate.Location.RelativePath)
		}
	}
	if !variantCandidate.Valid() || variantCandidate.Package.Manifests[0].Profile != "work" {
		t.Fatalf("variant candidate = %#v", variantCandidate)
	}
	if invalidCandidate.Valid() || countSkillCode(invalidCandidate.Diagnostics, skill.CodeInvalidProfile) != 1 {
		t.Fatalf("invalid candidate diagnostics = %#v", invalidCandidate.Diagnostics)
	}
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

func hasSupportingPath(files []skill.File, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
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
