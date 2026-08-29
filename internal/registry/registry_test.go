package registry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveName(t *testing.T) {
	tests := []struct {
		source string
		name   string
	}{
		{"git@github.com:jmcampanini/skills.git", "github.com/jmcampanini/skills"},
		{"https://github.com/jmcampanini/skills.git", "github.com/jmcampanini/skills"},
		{"ssh://git@github.com/jmcampanini/skills.git", "github.com/jmcampanini/skills"},
		{"/tmp/my-skills.git", "my-skills"},
	}
	for _, test := range tests {
		source, err := ParseSource(test.source)
		if err != nil {
			t.Fatalf("ParseSource(%q): %v", test.source, err)
		}
		if source.Name != test.name {
			t.Errorf("ParseSource(%q) = %#v", test.source, source)
		}
	}
}

func TestParseSourceRejectsInvalidNetworkURLs(t *testing.T) {
	for _, source := range []string{
		"https://user:secret@example.com/repo.git",
		"https://:443/repo.git",
		"https://example.com/%zz",
	} {
		if err := Add(filepath.Join(t.TempDir(), "repos.toml"), source, "valid/repo"); !errors.Is(err, ErrInvalidSource) {
			t.Fatalf("Add(%q) error = %v", source, err)
		}
	}
}

func TestAddPreservesOrderAndRejectsIdentityConflicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "repos.toml")
	if err := Add(path, "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	if err := Add(path, "https://example.com/b.git", "work/b"); err != nil {
		t.Fatal(err)
	}
	if err := Add(path, "https://example.com/c.git", "example.com-a"); !errors.Is(err, ErrSlugCollision) {
		t.Fatalf("slug collision error = %v", err)
	}
	if err := Add(path, "https://example.com/d.git", "example.com/a"); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate name error = %v", err)
	}
	if err := Add(path, "https://example.com/a.git", "other"); !errors.Is(err, ErrDuplicateURL) {
		t.Fatalf("duplicate URL error = %v", err)
	}
	got, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "example.com/a" || got[1].Name != "work/b" {
		t.Fatalf("order = %#v", got)
	}
}

func TestRelativeLocalSourceRemainsStableAcrossWorkingDirectories(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "sources", "repo")
	addFrom := filepath.Join(root, "add-from")
	useFrom := filepath.Join(root, "use-from")
	for _, path := range []string{source, addFrom, useFrom} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWorkingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(addFrom); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "state", "repos.toml")
	relativeSource, err := filepath.Rel(addFrom, source)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSource(relativeSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := Add(registryPath, relativeSource); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(useFrom); err != nil {
		t.Fatal(err)
	}
	repositories, err := List(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || repositories[0].URL != parsed.Stored || !filepath.IsAbs(repositories[0].URL) {
		t.Fatalf("repositories = %#v", repositories)
	}
	relativeSource, err = filepath.Rel(useFrom, source)
	if err != nil {
		t.Fatal(err)
	}
	if err := Add(registryPath, relativeSource, "other/repo"); !errors.Is(err, ErrDuplicateURL) {
		t.Fatalf("duplicate relative source error = %v", err)
	}
}

func TestAbsentRegistryIsEmpty(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil || len(got.Repos) != 0 {
		t.Fatalf("Load absent registry = %#v, %v", got, err)
	}
}

func TestCloneSlugCollisionsAreCaseInsensitive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repos.toml")
	if err := Add(path, "https://example.com/one.git", "Work/Repo"); err != nil {
		t.Fatal(err)
	}
	if err := Add(path, "https://example.com/two.git", "work/repo"); !errors.Is(err, ErrSlugCollision) {
		t.Fatalf("case-insensitive slug collision error = %v", err)
	}
}

func TestExplicitNameAllowsUnderivableSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repos.toml")
	source := filepath.Join(t.TempDir(), "My Skills")
	if err := Add(path, source, "local/skills"); err != nil {
		t.Fatal(err)
	}
	got, err := List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != (Repo{Name: "local/skills", URL: source}) {
		t.Fatalf("repositories = %#v", got)
	}
	if _, err := DeriveName(source); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("DeriveName error = %v", err)
	}
	remote := "ssh://git@[2001:db8::1]/group/repo.git"
	if err := Add(path, remote, "ipv6/repo"); err != nil {
		t.Fatal(err)
	}
	if _, err := DeriveName(remote); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("remote DeriveName error = %v", err)
	}
}

func TestMalformedRegistryFailsLoud(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repos.toml")
	if err := os.WriteFile(path, []byte("[[repos]]\nname = \"x\"\nurl = nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Load error = %v", err)
	}
}

func TestRegistryRejectsNonTOMLStringEscapesAndDuplicateKeys(t *testing.T) {
	tests := []string{
		"[[repos]]\nname = \"example/repo\"\nurl = \"https://example.com/\\arepo.git\"\n",
		"[[repos]]\nname = \"\"\nname = \"example/repo\"\nurl = \"https://example.com/repo.git\"\n",
		"[[repos]]\nname = \"example/repo\"\nurl = \"https://example.com/repo.git\"\nextra = true\n",
	}
	for _, contents := range tests {
		path := filepath.Join(t.TempDir(), "repos.toml")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); !errors.Is(err, ErrMalformed) {
			t.Fatalf("Load error = %v", err)
		}
	}
}

func TestRemoveDeletesOnlySafeClone(t *testing.T) {
	dataRoot := t.TempDir()
	path := filepath.Join(t.TempDir(), "repos.toml")
	if err := Add(path, "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(dataRoot, "example.com-a")
	outside := filepath.Join(dataRoot, "outside")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path, "example.com/a", dataRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(clone); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clone still exists: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside was removed: %v", err)
	}
	if got, err := Load(path); err != nil || len(got.Repos) != 0 {
		t.Fatalf("registry after remove = %#v, %v", got, err)
	}
}

func TestRemoveRejectsSymlinkedCloneRoot(t *testing.T) {
	dataRoot := t.TempDir()
	path := filepath.Join(t.TempDir(), "repos.toml")
	if err := Add(path, "https://example.com/a.git"); err != nil {
		t.Fatal(err)
	}
	realRoot := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, filepath.Join(dataRoot, "repos")); err != nil {
		t.Fatal(err)
	}
	if err := Remove(path, "example.com/a", filepath.Join(dataRoot, "repos")); !errors.Is(err, ErrUnsafeClonePath) {
		t.Fatalf("Remove error = %v", err)
	}
	if got, err := Load(path); err != nil || len(got.Repos) != 1 {
		t.Fatalf("registry changed: %#v, %v", got, err)
	}
}
