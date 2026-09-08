package skill

import (
	"bytes"
	"fmt"
	"strings"
)

const (
	variablePrefix        = "{{esheep."
	sourcesVariable       = "{{esheep.sources}}"
	includeVariablePrefix = "{{esheep.include-by-harness \""
)

// Variables supplies source paths and the current skill and harness for body
// expansion. SkillRoot and Harness are required only when an include is used.
type Variables struct {
	Harness   string
	SkillRoot string
	Sources   []string
}

type variableKind uint8

const (
	variableSources variableKind = iota
	variableIncludeByHarness
)

type bodyVariable struct {
	argument string
	end      int
	kind     variableKind
	start    int
}

// ExpandVariables replaces whole-line esheep variables, recursively expanding
// included harness files. Other bytes, including line endings, are preserved.
// Generated source paths are literal values and are not expanded again.
func ExpandVariables(body []byte, variables Variables) ([]byte, error) {
	return variables.expand(body, nil)
}

func (variables Variables) expand(body []byte, chain []includedFile) ([]byte, error) {
	tokens, diagnostics := parseBodyVariables(body)
	if len(diagnostics) != 0 {
		return nil, fmt.Errorf("expand body: %s: %w", diagnostics[0].Detail, &ValidationError{Diagnostics: diagnostics})
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
		case variableIncludeByHarness:
			content, err := variables.include(token.argument, chain)
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

func parseBodyVariables(body []byte) ([]bodyVariable, []Diagnostic) {
	var tokens []bodyVariable
	var diagnostics []Diagnostic
	for offset := 0; ; {
		index := bytes.Index(body[offset:], []byte(variablePrefix))
		if index < 0 {
			return tokens, diagnostics
		}
		start := offset + index
		token, ok := parseVariable(body[start:])
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{
				Code:   CodeInvalidVariable,
				Detail: fmt.Sprintf("unknown esheep variable %q", variableSnippet(body[start:])),
			})
			offset = start + len(variablePrefix)
			continue
		}
		token.start = start
		token.end += start
		if !ownsLine(body, token.start, token.end) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:   CodeInvalidVariable,
				Detail: string(body[token.start:token.end]) + " must occupy its own line",
			})
		}
		tokens = append(tokens, token)
		offset = token.end
	}
}

func parseVariable(text []byte) (bodyVariable, bool) {
	if bytes.HasPrefix(text, []byte(sourcesVariable)) {
		return bodyVariable{end: len(sourcesVariable), kind: variableSources}, true
	}
	argument, ok := bytes.CutPrefix(text, []byte(includeVariablePrefix))
	if !ok {
		return bodyVariable{}, false
	}
	end := bytes.Index(argument, []byte("\"}}"))
	if end < 0 {
		return bodyVariable{}, false
	}
	prefix := string(argument[:end])
	if !ValidIdentity(prefix, prefix) {
		return bodyVariable{}, false
	}
	return bodyVariable{
		argument: prefix,
		end:      len(includeVariablePrefix) + end + len("\"}}"),
		kind:     variableIncludeByHarness,
	}, true
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
