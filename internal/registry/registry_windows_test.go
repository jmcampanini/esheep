package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsLocalSourcesSurviveRegistryReload(t *testing.T) {
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

	tests := []struct {
		name   string
		source func() string
	}{
		{name: "absolute", source: func() string { return source }},
		{name: "relative", source: func() string {
			value, err := filepath.Rel(addFrom, source)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.Chdir(addFrom); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, test.name, "repos.toml")
			if err := Add(path, test.source()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(useFrom); err != nil {
				t.Fatal(err)
			}
			repositories, err := List(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(repositories) != 1 || !filepath.IsAbs(repositories[0].URL) {
				t.Fatalf("repositories = %#v", repositories)
			}
		})
	}
}
