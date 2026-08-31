package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var markdownLinkPattern = regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)

func TestDocumentedCommandsMatchBinaryHelp(t *testing.T) {
	rootHelp := runEsheep(t, nil, "--help")
	assertSuccess(t, rootHelp)
	skillsHelp := runEsheep(t, nil, "skills", "--help")
	assertSuccess(t, skillsHelp)
	sessionsHelp := runEsheep(t, nil, "sessions", "--help")
	assertSuccess(t, sessionsHelp)
	readme := readRepositoryFile(t, "README.md")

	commands := []struct {
		documentation string
		help          string
		name          string
	}{
		{documentation: "esheep completion zsh", help: rootHelp.stdout, name: "completion"},
		{documentation: "esheep config", help: rootHelp.stdout, name: "config"},
		{documentation: "esheep doctor", help: rootHelp.stdout, name: "doctor"},
		{documentation: "esheep profiles", help: rootHelp.stdout, name: "profiles"},
		{documentation: "esheep sessions list", help: sessionsHelp.stdout, name: "list"},
		{documentation: "esheep sessions search", help: sessionsHelp.stdout, name: "search"},
		{documentation: "esheep skills list", help: skillsHelp.stdout, name: "list"},
		{documentation: "esheep skills status", help: skillsHelp.stdout, name: "status"},
		{documentation: "esheep sync", help: rootHelp.stdout, name: "sync"},
	}
	for _, command := range commands {
		if !strings.Contains(command.help, "  "+command.name+" ") {
			t.Errorf("binary help does not list command %q", command.name)
		}
		if !strings.Contains(readme, command.documentation) {
			t.Errorf("README.md does not document %q", command.documentation)
		}
	}
	if !strings.Contains(readme, "esheep --version") {
		t.Error("README.md does not document the version flag")
	}
}

func TestDocumentationLinksResolve(t *testing.T) {
	root := repositoryRoot(t)
	patterns := []string{filepath.Join(root, "*.md"), filepath.Join(root, "plans", "*.md")}
	var documents []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("Glob(%q): %v", pattern, err)
		}
		documents = append(documents, matches...)
	}

	for _, document := range documents {
		data, err := os.ReadFile(document)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", document, err)
		}
		for _, match := range markdownLinkPattern.FindAllStringSubmatch(string(data), -1) {
			target := strings.SplitN(match[1], "#", 2)[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			path := filepath.Join(filepath.Dir(document), filepath.FromSlash(target))
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s: link target %q does not resolve: %v", document, match[1], err)
			}
		}
	}
}

func readRepositoryFile(t *testing.T, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), relative))
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", relative, err)
	}
	return string(data)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("Abs(repository root): %v", err)
	}
	return root
}
