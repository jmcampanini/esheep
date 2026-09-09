package skill

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAcceptsVariablesWithoutOpeningIncludes(t *testing.T) {
	t.Parallel()
	body := "{{esheep.sources}}\n{{esheep.include-by-harness \"body\"}}\n{{esheep.include-by-harness-optional \"extras\"}}"
	input := "---\nname: demo\nesheep-trigger: ok\nesheep-targets: [claude]\n---\n" + body
	path := filepath.Join(t.TempDir(), "SKILL.md")

	document, err := Parse([]byte(input), "demo", path)
	if err != nil || string(document.Body) != body {
		t.Fatalf("Parse() body = %q, %v, want unexpanded %q", document.Body, err, body)
	}
}

func TestParseIgnoresVariableTextInFrontmatter(t *testing.T) {
	t.Parallel()
	input := "---\nname: demo\nesheep-trigger: '{{esheep.unknown}}'\nesheep-targets: [claude]\nnotes: '{{esheep.include-by-harness-optional unquoted}}'\n---\nbody"
	if _, err := Parse([]byte(input), "demo", "SKILL.md"); err != nil {
		t.Fatal(err)
	}
}

func TestParseMapsVariableSyntaxErrorsToDiagnostics(t *testing.T) {
	t.Parallel()
	input := "---\nname: demo\nesheep-trigger: ok\nesheep-targets: [claude]\n---\n{{esheep.include-by-harness-optional unquoted}}"
	path := filepath.Join("demo", "SKILL.md")

	_, err := Parse([]byte(input), "demo", path)
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("Parse() error = %v, want ValidationError", err)
	}
	if len(invalid.Diagnostics) != 1 {
		t.Fatalf("Parse() diagnostics = %#v, want one invalid-variable diagnostic", invalid.Diagnostics)
	}
	diagnostic := invalid.Diagnostics[0]
	if diagnostic.Code != CodeInvalidVariable || diagnostic.Path != path || !strings.Contains(diagnostic.Detail, "unknown esheep variable") {
		t.Errorf("Parse() diagnostic = %#v, want code %q at %q with variable error detail", diagnostic, CodeInvalidVariable, path)
	}
}
