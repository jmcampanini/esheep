// Package expansion substitutes declarative esheep variables in instruction text.
package expansion

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/jmcampanini/esheep/internal/naming"
)

const (
	variablePrefix                = "{{esheep."
	sourcesVariable               = "{{esheep.sources}}"
	includeVariablePrefix         = "{{esheep.include-by-harness \""
	optionalIncludeVariablePrefix = "{{esheep.include-by-harness-optional \""
)

// Variables supplies resolved source paths and the current document root and
// harness. Root and Harness are required only when an include is used; nested
// includes always resolve from Root, including through symlinks.
type Variables struct {
	Harness string
	Root    string
	Sources []string
}

// ValidationError describes invalid esheep variables in instruction text.
type ValidationError struct {
	Details []string
}

// Error implements error.
func (err *ValidationError) Error() string {
	return "invalid esheep variables: " + strings.Join(err.Details, "; ")
}

// Validate reports invalid variables without opening any included files.
func Validate(body []byte) []string {
	_, diagnostics := parseBodyVariables(body)
	return diagnostics
}

type variableKind uint8

const (
	variableSources variableKind = iota
	variableIncludeByHarness
	variableIncludeByHarnessOptional
)

type bodyVariable struct {
	argument string
	end      int
	kind     variableKind
	start    int
}

// Expand replaces whole-line esheep variables, recursively expanding included
// harness files. Missing optional includes contribute no bytes. Other bytes,
// including line endings, are preserved. Generated source paths are literal
// values and are not expanded again. Failed expansion returns no partial text.
func Expand(body []byte, variables Variables) ([]byte, error) {
	return variables.expand(body, nil)
}

func (variables Variables) expand(body []byte, chain []includedFile) ([]byte, error) {
	tokens, diagnostics := parseBodyVariables(body)
	if len(diagnostics) != 0 {
		return nil, &ValidationError{Details: diagnostics}
	}
	if len(tokens) == 0 {
		return body, nil
	}

	var expanded bytes.Buffer
	offset := 0
	for _, token := range tokens {
		expanded.Write(body[offset:token.start])
		switch token.kind {
		case variableSources:
			if len(variables.Sources) == 0 {
				return nil, fmt.Errorf("expand %s: no source directories provided", sourcesVariable)
			}
			expanded.WriteString("- " + strings.Join(variables.Sources, "\n- "))
		case variableIncludeByHarness, variableIncludeByHarnessOptional:
			content, err := variables.include(token.argument, token.kind, chain)
			if err != nil {
				return nil, err
			}
			expanded.Write(content)
		}
		offset = token.end
	}
	expanded.Write(body[offset:])
	return expanded.Bytes(), nil
}

func parseBodyVariables(body []byte) ([]bodyVariable, []string) {
	var tokens []bodyVariable
	var diagnostics []string
	for offset := 0; ; {
		index := bytes.Index(body[offset:], []byte(variablePrefix))
		if index < 0 {
			return tokens, diagnostics
		}
		start := offset + index
		token, ok := parseVariable(body[start:])
		if !ok {
			diagnostics = append(diagnostics, fmt.Sprintf("unknown esheep variable %q", variableSnippet(body[start:])))
			offset = start + len(variablePrefix)
			continue
		}
		token.start = start
		token.end += start
		if !ownsLine(body, token.start, token.end) {
			diagnostics = append(diagnostics, string(body[token.start:token.end])+" must occupy its own line")
		}
		tokens = append(tokens, token)
		offset = token.end
	}
}

func parseVariable(text []byte) (bodyVariable, bool) {
	if bytes.HasPrefix(text, []byte(sourcesVariable)) {
		return bodyVariable{end: len(sourcesVariable), kind: variableSources}, true
	}
	for _, include := range []struct {
		kind   variableKind
		prefix string
	}{
		{kind: variableIncludeByHarness, prefix: includeVariablePrefix},
		{kind: variableIncludeByHarnessOptional, prefix: optionalIncludeVariablePrefix},
	} {
		argument, ok := bytes.CutPrefix(text, []byte(include.prefix))
		if !ok {
			continue
		}
		end := bytes.Index(argument, []byte("\"}}"))
		if end < 0 || !naming.ValidSkillName(string(argument[:end])) {
			return bodyVariable{}, false
		}
		return bodyVariable{
			argument: string(argument[:end]),
			end:      len(include.prefix) + end + len("\"}}"),
			kind:     include.kind,
		}, true
	}
	return bodyVariable{}, false
}

// ownsLine reports whether body[start:end] is a complete line, allowing a
// CRLF terminator.
func ownsLine(body []byte, start, end int) bool {
	if start != 0 && body[start-1] != '\n' {
		return false
	}
	rest := body[end:]
	if len(rest) != 0 && rest[0] == '\r' {
		rest = rest[1:]
	}
	return len(rest) == 0 || rest[0] == '\n'
}

// variableSnippet extracts the unrecognized token for a diagnostic, ending at
// its closing braces when they are near.
func variableSnippet(text []byte) string {
	const limit = 120
	if end := bytes.Index(text, []byte("}}")); end >= 0 && end+2 <= limit {
		return string(text[:end+2])
	}
	if len(text) > limit {
		text = text[:limit]
	}
	return string(text)
}
