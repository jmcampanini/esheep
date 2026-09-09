package render

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jmcampanini/esheep/internal/expansion"
	"github.com/jmcampanini/esheep/internal/skill"
)

func TestRenderIncludesBodyWithoutInstallingSourceOnlyEntries(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "demo")
	files := map[string]string{
		"SKILL.md":                           "---\nname: demo\nesheep-trigger: shared\nesheep-targets: [codex]\n---\n{{esheep.include-by-harness \"body\"}}",
		"esheep-body-codex.md":               "Codex instructions",
		"esheep-body-pi.md":                  "{{esheep.unknown}}",
		"esheep-notes/ordinary.md":           "source notes",
		"support/esheep-private/ordinary.md": "source notes",
		"support/esheep-notes.md":            "source notes",
		"empty-visible/esheep-notes.md":      "source notes",
		"support/guide.md":                   "{{esheep.sources}}",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	source, err := skill.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	staging := t.TempDir()

	rendered, err := Render(staging, source, source.Manifests[0].Document, TargetCodex, nil, expansion.Variables{})
	if err != nil || !rendered {
		t.Fatalf("Render() = %v, %v", rendered, err)
	}
	var paths []string
	if err := fs.WalkDir(os.DirFS(staging), ".", func(path string, _ fs.DirEntry, err error) error {
		if err == nil {
			paths = append(paths, path)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{".", "SKILL.md", "empty-visible", "support", "support/guide.md"}; !slices.Equal(paths, want) {
		t.Fatalf("installed paths = %q, want %q", paths, want)
	}
	manifest, err := os.ReadFile(filepath.Join(staging, "SKILL.md"))
	if want := "---\nname: demo\ndescription: shared\n---\nCodex instructions"; err != nil || string(manifest) != want {
		t.Fatalf("installed manifest = %q, %v, want %q", manifest, err, want)
	}
	guide, err := os.ReadFile(filepath.Join(staging, "support/guide.md"))
	if err != nil || string(guide) != "{{esheep.sources}}" {
		t.Fatalf("support file = %q, %v, want literal variable", guide, err)
	}
	assertMode(t, filepath.Join(staging, "support/guide.md"), 0o644)
}

func TestRenderMissingIncludeLeavesStagingEmpty(t *testing.T) {
	t.Parallel()
	source := loadManifestOnlyPackage(t)
	document := source.Manifests[0].Document
	document.Body = []byte("{{esheep.include-by-harness \"missing\"}}")
	staging := t.TempDir()

	_, err := Render(staging, source, document, TargetPi, nil, expansion.Variables{})
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "esheep-missing-pi.md") {
		t.Fatalf("Render() error = %v, want missing Pi include", err)
	}
	entries, err := os.ReadDir(staging)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging after failed render = %v, %v, want empty", entries, err)
	}
}
