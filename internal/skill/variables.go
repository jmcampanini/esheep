package skill

import (
	"bytes"
	"fmt"
	"strings"
)

// The variablePrefix text is reserved everywhere in a manifest body: every
// occurrence must begin a known variable, and a known variable must occupy
// its own line. There is no escape syntax.
const (
	variablePrefix  = "{{esheep."
	sourcesVariable = "{{esheep.sources}}"
)

// Variables contains the values substituted for esheep variables during
// rendering.
type Variables struct {
	Sources []string
}

// ExpandVariables replaces each esheep variable in body with its value:
// {{esheep.sources}} becomes a Markdown bullet list of the source directories
// in configuration order. A body that uses the variable requires at least one
// source directory.
func ExpandVariables(body []byte, variables Variables) ([]byte, error) {
	if !bytes.Contains(body, []byte(sourcesVariable)) {
		return body, nil
	}
	if len(variables.Sources) == 0 {
		return nil, fmt.Errorf("expand %s: no source directories provided", sourcesVariable)
	}

	list := "- " + strings.Join(variables.Sources, "\n- ")
	return bytes.ReplaceAll(body, []byte(sourcesVariable), []byte(list)), nil
}

// validateBody rejects variable text that rendering would not replace.
func validateBody(body []byte) []Diagnostic {
	var diagnostics []Diagnostic
	for offset := 0; ; {
		index := bytes.Index(body[offset:], []byte(variablePrefix))
		if index < 0 {
			return diagnostics
		}
		start := offset + index
		if !bytes.HasPrefix(body[start:], []byte(sourcesVariable)) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:   CodeInvalidVariable,
				Detail: fmt.Sprintf("unknown esheep variable %q", variableSnippet(body[start:])),
			})
			offset = start + len(variablePrefix)
			continue
		}
		if !ownsLine(body, start, start+len(sourcesVariable)) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:   CodeInvalidVariable,
				Detail: sourcesVariable + " must occupy its own line",
			})
		}
		offset = start + len(sourcesVariable)
	}
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
	const limit = 40
	if end := bytes.Index(text, []byte("}}")); end >= 0 && end+2 <= limit {
		return string(text[:end+2])
	}
	if len(text) > limit {
		text = text[:limit]
	}
	return string(text)
}
