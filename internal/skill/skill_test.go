package skill

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePreservesCRLFBodyAndOptionalPresence(t *testing.T) {
	t.Parallel()
	body := []byte("# Hello\r\n\x00tail")
	input := append([]byte("---\r\nname: demo-skill\r\ndescription: ' useful '\r\nlicense: ''\r\ncompatibility: ''\r\nmetadata: {}\r\nclaude:\r\n  argument-hint: ''\r\npi:\r\n  disabled: true\r\ncodex:\r\n  disabled: true\r\nagents:\r\n  disabled: false\r\n---\r\n"), body...)
	document, err := Parse(input, "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if string(document.Body) != string(body) {
		t.Fatalf("body = %q, want %q", document.Body, body)
	}
	if document.License == nil || *document.License != "" || document.Compatibility == nil || *document.Compatibility != "" {
		t.Fatalf("optional strings = license %#v, compatibility %#v", document.License, document.Compatibility)
	}
	if document.Metadata == nil || len(document.Metadata) != 0 {
		t.Fatalf("metadata = %#v, want present empty map", document.Metadata)
	}
	if document.Targets.Claude.ArgumentHint == nil || *document.Targets.Claude.ArgumentHint != "" {
		t.Fatalf("argument hint = %#v, want present empty string", document.Targets.Claude.ArgumentHint)
	}
	if !document.Targets.Pi.Disabled || !document.Targets.Codex.Disabled {
		t.Fatalf("targets = %#v", document.Targets)
	}
}

func TestParseDistinguishesAbsentOptionalFields(t *testing.T) {
	t.Parallel()
	document, err := Parse([]byte("---\nname: demo\ndescription: ok\n---\n"), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if document.License != nil || document.Compatibility != nil || document.Metadata != nil || document.Targets.Claude.ArgumentHint != nil {
		t.Fatalf("absent fields became present: %#v", document)
	}
}

func TestParseRejectsInvalidDeclarativeFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dir  string
		yaml string
		code Code
	}{
		{name: "missing frontmatter", dir: "demo", yaml: "name: demo\n", code: CodeFrontmatter},
		{name: "duplicate common key", dir: "demo", yaml: "name: demo\nname: demo\ndescription: ok\n", code: CodeYAML},
		{name: "duplicate target key", dir: "demo", yaml: "name: demo\ndescription: ok\nclaude:\n  disabled: true\n  disabled: false\n", code: CodeYAML},
		{name: "unknown common field", dir: "demo", yaml: "name: demo\ndescription: ok\nextra: true\n", code: CodeUnknownField},
		{name: "forbidden tools", dir: "demo", yaml: "name: demo\ndescription: ok\nallowed-tools: Bash\n", code: CodeForbiddenField},
		{name: "forbidden target hook", dir: "demo", yaml: "name: demo\ndescription: ok\nclaude:\n  hooks: run\n", code: CodeForbiddenField},
		{name: "codex hint", dir: "demo", yaml: "name: demo\ndescription: ok\ncodex:\n  argument-hint: no\n", code: CodeUnknownField},
		{name: "name is map", dir: "demo", yaml: "name: {}\ndescription: ok\n", code: CodeInvalidValue},
		{name: "license is boolean", dir: "demo", yaml: "name: demo\ndescription: ok\nlicense: true\n", code: CodeInvalidValue},
		{name: "metadata is scalar", dir: "demo", yaml: "name: demo\ndescription: ok\nmetadata: no\n", code: CodeInvalidValue},
		{name: "metadata value type", dir: "demo", yaml: "name: demo\ndescription: ok\nmetadata:\n  count: 2\n", code: CodeInvalidValue},
		{name: "target is scalar", dir: "demo", yaml: "name: demo\ndescription: ok\nclaude: no\n", code: CodeInvalidValue},
		{name: "disabled is string", dir: "demo", yaml: "name: demo\ndescription: ok\nclaude:\n  disabled: 'true'\n", code: CodeInvalidValue},
		{name: "hint is boolean", dir: "demo", yaml: "name: demo\ndescription: ok\npi:\n  argument-hint: true\n", code: CodeInvalidValue},
		{name: "invalid name", dir: "bad_name", yaml: "name: bad_name\ndescription: ok\n", code: CodeInvalidName},
		{name: "directory mismatch", dir: "other", yaml: "name: demo\ndescription: ok\n", code: CodeNameMismatch},
		{name: "blank description", dir: "demo", yaml: "name: demo\ndescription: '  '\n", code: CodeRequiredField},
		{name: "long description", dir: "demo", yaml: "name: demo\ndescription: " + strings.Repeat("界", 1025) + "\n", code: CodeInvalidValue},
		{name: "long compatibility", dir: "demo", yaml: "name: demo\ndescription: ok\ncompatibility: " + strings.Repeat("界", 501) + "\n", code: CodeInvalidValue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := []byte(test.yaml)
			if test.code != CodeFrontmatter {
				input = []byte("---\n" + test.yaml + "---\nbody")
			}
			_, err := Parse(input, test.dir)
			if err == nil {
				t.Fatal("Parse succeeded")
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %T, want ValidationError", err)
			}
			for _, diagnostic := range validationErr.Diagnostics {
				if diagnostic.Code == test.code {
					return
				}
			}
			t.Fatalf("diagnostics = %#v, want code %q", validationErr.Diagnostics, test.code)
		})
	}
}

func TestParseCountsUnicodeCharactersAtLimits(t *testing.T) {
	t.Parallel()
	input := "---\nname: demo\ndescription: " + strings.Repeat("界", 1024) + "\ncompatibility: " + strings.Repeat("界", 500) + "\n---\n"
	if _, err := Parse([]byte(input), "demo"); err != nil {
		t.Fatal(err)
	}
}
