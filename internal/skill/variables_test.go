package skill

import (
	"errors"
	"strings"
	"testing"
)

func TestParseAcceptsSourcesVariableOnOwnLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "between lines", body: "intro\n{{esheep.sources}}\ntail"},
		{name: "entire body", body: "{{esheep.sources}}"},
		{name: "at end without newline", body: "intro\n{{esheep.sources}}"},
		{name: "crlf lines", body: "intro\r\n{{esheep.sources}}\r\ntail"},
		{name: "repeated", body: "{{esheep.sources}}\n\n{{esheep.sources}}\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := "---\nname: demo\ndescription: ok\nesheep-targets: [claude]\n---\n" + test.body
			if _, err := Parse([]byte(input), "demo", "SKILL.md"); err != nil {
				t.Fatalf("Parse(%q): %v", test.body, err)
			}
		})
	}
}

func TestParseIgnoresVariableTextInFrontmatter(t *testing.T) {
	t.Parallel()
	input := "---\nname: demo\ndescription: '{{esheep.sources}}'\nesheep-targets: [claude]\n---\nbody"
	if _, err := Parse([]byte(input), "demo", "SKILL.md"); err != nil {
		t.Fatal(err)
	}
}

func TestParseRejectsInvalidBodyVariables(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		body   string
		detail string
	}{
		{name: "unknown variable", body: "{{esheep.targets}}\n", detail: "unknown esheep variable \"{{esheep.targets}}\""},
		{name: "unterminated variable", body: "see {{esheep.sources here\n", detail: "unknown esheep variable"},
		{name: "bare prefix", body: "the {{esheep. prefix\n", detail: "unknown esheep variable"},
		{name: "leading text", body: "see {{esheep.sources}}\n", detail: "must occupy its own line"},
		{name: "trailing text", body: "{{esheep.sources}} here\n", detail: "must occupy its own line"},
		{name: "indented", body: "  {{esheep.sources}}\n", detail: "must occupy its own line"},
		{name: "adjacent variables", body: "{{esheep.sources}}{{esheep.sources}}\n", detail: "must occupy its own line"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := "---\nname: demo\ndescription: ok\nesheep-targets: [claude]\n---\n" + test.body
			_, err := Parse([]byte(input), "demo", "SKILL.md")
			if err == nil {
				t.Fatalf("Parse(%q) succeeded", test.body)
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("error = %T, want ValidationError", err)
			}
			for _, diagnostic := range validationErr.Diagnostics {
				if diagnostic.Code == CodeInvalidVariable && strings.Contains(diagnostic.Detail, test.detail) {
					return
				}
			}
			t.Fatalf("diagnostics = %#v, want code %q with detail %q", validationErr.Diagnostics, CodeInvalidVariable, test.detail)
		})
	}
}

func TestExpandVariablesReplacesSourcesList(t *testing.T) {
	t.Parallel()
	variables := Variables{Sources: []string{"/alpha", "/beta"}}
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "no variable", body: "plain body", want: "plain body"},
		{name: "between lines", body: "before\n{{esheep.sources}}\nafter", want: "before\n- /alpha\n- /beta\nafter"},
		{name: "at end without newline", body: "before\n{{esheep.sources}}", want: "before\n- /alpha\n- /beta"},
		{name: "crlf line", body: "{{esheep.sources}}\r\nafter", want: "- /alpha\n- /beta\r\nafter"},
		{name: "repeated", body: "{{esheep.sources}}\n\n{{esheep.sources}}\n", want: "- /alpha\n- /beta\n\n- /alpha\n- /beta\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExpandVariables([]byte(test.body), variables)
			if err != nil {
				t.Fatalf("ExpandVariables(%q): %v", test.body, err)
			}
			if string(got) != test.want {
				t.Fatalf("ExpandVariables(%q) = %q, want %q", test.body, got, test.want)
			}
		})
	}
}

func TestExpandVariablesRequiresSourcesForUse(t *testing.T) {
	t.Parallel()
	if _, err := ExpandVariables([]byte("{{esheep.sources}}\n"), Variables{}); err == nil {
		t.Fatal("ExpandVariables succeeded without source directories")
	}
	got, err := ExpandVariables([]byte("plain body"), Variables{})
	if err != nil || string(got) != "plain body" {
		t.Fatalf("ExpandVariables(plain body) = %q, %v", got, err)
	}
}
