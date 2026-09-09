package expansion

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestValidateAcceptsWholeLineVariables(t *testing.T) {
	t.Parallel()
	for _, variable := range []string{
		"{{esheep.sources}}",
		"{{esheep.include-by-harness \"body\"}}",
		"{{esheep.include-by-harness-optional \"extras\"}}",
		"{{esheep.include-by-harness-optional \"" + strings.Repeat("a", 64) + "\"}}",
	} {
		t.Run(variable, func(t *testing.T) {
			t.Parallel()
			for _, test := range []struct {
				name string
				body string
			}{
				{name: "between lines", body: "intro\n" + variable + "\ntail"},
				{name: "entire body", body: variable},
				{name: "at end without newline", body: "intro\n" + variable},
				{name: "crlf lines", body: "intro\r\n" + variable + "\r\ntail"},
				{name: "repeated", body: variable + "\n\n" + variable + "\n"},
			} {
				t.Run(test.name, func(t *testing.T) {
					if got := Validate([]byte(test.body)); len(got) != 0 {
						t.Errorf("Validate(%q) = %q, want no errors", test.body, got)
					}
				})
			}
		})
	}
}

func TestValidateRejectsInvalidVariables(t *testing.T) {
	t.Parallel()
	type syntaxCase struct {
		name   string
		body   string
		detail string
	}
	tests := []syntaxCase{
		{name: "unknown variable", body: "{{esheep.targets}}\n", detail: "unknown esheep variable"},
		{name: "unterminated variable", body: "see {{esheep.sources here\n", detail: "unknown esheep variable"},
		{name: "bare prefix", body: "the {{esheep. prefix\n", detail: "unknown esheep variable"},
		{name: "leading text", body: "see {{esheep.sources}}\n", detail: "must occupy its own line"},
		{name: "trailing text", body: "{{esheep.sources}} here\n", detail: "must occupy its own line"},
		{name: "indented", body: "  {{esheep.sources}}\n", detail: "must occupy its own line"},
		{name: "adjacent variables", body: "{{esheep.sources}}{{esheep.sources}}\n", detail: "must occupy its own line"},
	}
	for _, kind := range []string{"include-by-harness", "include-by-harness-optional"} {
		for _, argument := range []string{`"../body"`, `""`, "body", `'body'`, `"Body"`, ` "body"`, `"body" `, `"` + strings.Repeat("a", 65) + `"`} {
			tests = append(tests, syntaxCase{name: kind + " " + argument, body: fmt.Sprintf("{{esheep.%s %s}}", kind, argument), detail: "unknown esheep variable"})
		}
		tests = append(tests, syntaxCase{name: kind + " indented", body: fmt.Sprintf("  {{esheep.%s \"body\"}}", kind), detail: "must occupy its own line"})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			details := Validate([]byte(test.body))
			if len(details) == 0 || !strings.Contains(strings.Join(details, "\n"), test.detail) {
				t.Fatalf("Validate(%q) = %q, want %q", test.body, details, test.detail)
			}

			got, err := Expand([]byte(test.body), Variables{})
			var invalid *ValidationError
			if !errors.As(err, &invalid) || !slices.Equal(invalid.Details, details) || got != nil {
				t.Errorf("Expand(%q) = %q, %v, want no content and ValidationError with %q", test.body, got, err, details)
			}
		})
	}
}

func TestExpandReplacesSourcesList(t *testing.T) {
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
			got, err := Expand([]byte(test.body), variables)
			if err != nil || string(got) != test.want {
				t.Fatalf("Expand(%q) = %q, %v, want %q", test.body, got, err, test.want)
			}
		})
	}
}

func TestExpandRequiresSourcesForUse(t *testing.T) {
	t.Parallel()
	if got, err := Expand([]byte("before\n{{esheep.sources}}\n"), Variables{}); err == nil || got != nil {
		t.Fatalf("Expand() without source directories = %q, %v, want no content and error", got, err)
	}
	got, err := Expand([]byte("plain body"), Variables{})
	if err != nil || string(got) != "plain body" {
		t.Fatalf("Expand(plain body) = %q, %v", got, err)
	}
}
