// Package skill parses and validates esheep's declarative skill format.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const (
	maxNameLength          = 64
	maxDescriptionLength   = 1024
	maxCompatibilityLength = 500
)

var namePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Code identifies a stable validation category.
type Code string

// Validation codes used by parser and filesystem diagnostics.
const (
	CodeFrontmatter     Code = "frontmatter"
	CodeYAML            Code = "yaml"
	CodeUnknownField    Code = "unknown-field"
	CodeRequiredField   Code = "required-field"
	CodeInvalidName     Code = "invalid-name"
	CodeNameMismatch    Code = "name-mismatch"
	CodeInvalidValue    Code = "invalid-value"
	CodeReservedPath    Code = "reserved-path"
	CodePathCollision   Code = "path-collision"
	CodeUnsupportedFile Code = "unsupported-file"
	CodeInvalidSymlink  Code = "invalid-symlink"
	CodeUnreadable      Code = "unreadable"
)

// Diagnostic describes one validation failure without prescribing user-facing wording.
type Diagnostic struct {
	Code   Code
	Path   string
	Field  string
	Detail string
	Err    error
}

// ValidationError aggregates skill diagnostics.
type ValidationError struct {
	Diagnostics []Diagnostic
}

// Error implements error.
func (e *ValidationError) Error() string {
	if len(e.Diagnostics) == 0 {
		return "skill validation failed"
	}
	return fmt.Sprintf("skill validation failed with %d diagnostic(s)", len(e.Diagnostics))
}

// Unwrap exposes underlying filesystem or parser errors.
func (e *ValidationError) Unwrap() []error {
	causes := make([]error, 0, len(e.Diagnostics))
	for _, diagnostic := range e.Diagnostics {
		if diagnostic.Err != nil {
			causes = append(causes, diagnostic.Err)
		}
	}
	return causes
}

// TargetOptions contains declarative target-specific settings.
type TargetOptions struct {
	Disabled     bool
	ArgumentHint *string
}

// Targets contains settings for every supported target.
type Targets struct {
	Claude TargetOptions
	Pi     TargetOptions
	Codex  TargetOptions
	Agents TargetOptions
}

// ExtraField is a frontmatter field esheep does not interpret, preserved in
// source order for verbatim pass-through to every rendered target.
type ExtraField struct {
	Key   string
	Value *yaml.Node
}

// Document is a parsed SKILL.md and its byte-exact Markdown body.
type Document struct {
	Name                   string
	Description            string
	License                *string
	Compatibility          *string
	Metadata               map[string]string
	DisableModelInvocation bool
	Extra                  []ExtraField
	Targets                Targets
	Body                   []byte
}

// File is a regular supporting file. Path always uses slash separators.
type File struct {
	Path string
}

// Package is a validated skill tree ready for rendering.
type Package struct {
	Root             string
	Document         Document
	Directories      []string
	Files            []File
	sourceParentPath string
	sourceName       string
	sourceParentInfo os.FileInfo
	sourceInfo       os.FileInfo
}

type rawDocument struct {
	Name                   string            `yaml:"name"`
	Description            string            `yaml:"description"`
	License                *string           `yaml:"license"`
	Compatibility          *string           `yaml:"compatibility"`
	Metadata               map[string]string `yaml:"metadata"`
	DisableModelInvocation bool              `yaml:"disable-model-invocation"`
	Claude                 rawTarget         `yaml:"claude"`
	Pi                     rawTarget         `yaml:"pi"`
	Codex                  rawTarget         `yaml:"codex"`
	Agents                 rawTarget         `yaml:"agents"`
}

type rawTarget struct {
	Disabled     bool    `yaml:"disabled"`
	ArgumentHint *string `yaml:"argument-hint"`
}

// Parse parses frontmatter, preserves the Markdown body, and validates the
// fields esheep interprets. Fields it does not interpret are preserved in
// order for pass-through rendering. It returns a partially decoded document
// with a ValidationError when possible so discovery can still classify
// identities.
func Parse(data []byte, directoryName string) (Document, error) {
	frontmatter, body, splitErr := split(data)
	if splitErr != nil {
		return Document{}, &ValidationError{Diagnostics: []Diagnostic{{Code: CodeFrontmatter, Path: "SKILL.md", Err: splitErr}}}
	}

	var root yaml.Node
	unmarshalErr := yaml.Unmarshal(frontmatter, &root)
	mapping, mappingErr := documentMapping(&root)
	if mappingErr != nil {
		if unmarshalErr != nil {
			mappingErr = unmarshalErr
		}
		return Document{Body: body}, &ValidationError{Diagnostics: []Diagnostic{{Code: CodeYAML, Path: "SKILL.md", Err: mappingErr}}}
	}

	extra, diagnostics := validateShape(mapping)
	if unmarshalErr != nil {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeYAML, Path: "SKILL.md", Err: unmarshalErr})
	}
	var raw rawDocument
	if err := mapping.Decode(&raw); err != nil {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeYAML, Path: "SKILL.md", Err: err})
	}
	if raw.Name == "" {
		raw.Name = scalarField(mapping, "name")
	}
	document := Document{
		Name:                   raw.Name,
		Description:            raw.Description,
		License:                raw.License,
		Compatibility:          raw.Compatibility,
		Metadata:               raw.Metadata,
		DisableModelInvocation: raw.DisableModelInvocation,
		Extra:                  extra,
		Targets: Targets{
			Claude: TargetOptions{Disabled: raw.Claude.Disabled, ArgumentHint: raw.Claude.ArgumentHint},
			Pi:     TargetOptions{Disabled: raw.Pi.Disabled, ArgumentHint: raw.Pi.ArgumentHint},
			Codex:  TargetOptions{Disabled: raw.Codex.Disabled},
			Agents: TargetOptions{Disabled: raw.Agents.Disabled},
		},
		Body: body,
	}
	diagnostics = append(diagnostics, validateValues(document, directoryName)...)
	if len(diagnostics) != 0 {
		return document, &ValidationError{Diagnostics: diagnostics}
	}
	return document, nil
}

// ValidIdentity reports whether name has the approved grammar and matches its directory.
func ValidIdentity(name, directoryName string) bool {
	return len(name) <= maxNameLength && namePattern.MatchString(name) && name == directoryName
}

// ErrorDiagnostics returns validation diagnostics carried by err.
func ErrorDiagnostics(err error) []Diagnostic {
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		return nil
	}
	return validationErr.Diagnostics
}

func split(data []byte) ([]byte, []byte, error) {
	firstEnd := bytes.IndexByte(data, '\n')
	if firstEnd < 0 || string(bytes.TrimSuffix(data[:firstEnd], []byte{'\r'})) != "---" {
		return nil, nil, fmt.Errorf("opening delimiter is missing")
	}
	frontStart := firstEnd + 1
	lineStart := frontStart
	for lineStart <= len(data) {
		relativeEnd := bytes.IndexByte(data[lineStart:], '\n')
		lineEnd := len(data)
		next := len(data)
		if relativeEnd >= 0 {
			lineEnd = lineStart + relativeEnd
			next = lineEnd + 1
		}
		line := bytes.TrimSuffix(data[lineStart:lineEnd], []byte{'\r'})
		if string(line) == "---" {
			return data[frontStart:lineStart], data[next:], nil
		}
		if relativeEnd < 0 {
			break
		}
		lineStart = next
	}
	return nil, nil, fmt.Errorf("closing delimiter is missing")
}

func documentMapping(root *yaml.Node) (*yaml.Node, error) {
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("frontmatter must be a mapping")
	}
	return root.Content[0], nil
}

func scalarField(mapping *yaml.Node, field string) string {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == field && mapping.Content[index+1].Kind == yaml.ScalarNode && mapping.Content[index+1].Tag == "!!str" {
			return mapping.Content[index+1].Value
		}
	}
	return ""
}

func validateShape(mapping *yaml.Node) ([]ExtraField, []Diagnostic) {
	var extra []ExtraField
	var diagnostics []Diagnostic
	seen := make(map[string]struct{}, len(mapping.Content)/2)
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		keyNode := mapping.Content[index]
		value := mapping.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Path: "SKILL.md", Detail: "frontmatter keys must be strings"})
			continue
		}
		key := keyNode.Value
		if _, duplicate := seen[key]; duplicate {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Path: "SKILL.md", Field: key, Detail: "duplicate field"})
		}
		seen[key] = struct{}{}
		switch key {
		case "name", "description", "license", "compatibility":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				diagnostics = append(diagnostics, invalidType(key, "string"))
			}
		case "disable-model-invocation":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" {
				diagnostics = append(diagnostics, invalidType(key, "boolean"))
			}
		case "metadata":
			diagnostics = append(diagnostics, validateMetadata(value)...)
		case "claude", "pi":
			diagnostics = append(diagnostics, validateTarget(value, key, true)...)
		case "codex", "agents":
			diagnostics = append(diagnostics, validateTarget(value, key, false)...)
		default:
			extra = append(extra, ExtraField{Key: key, Value: value})
		}
	}
	return extra, diagnostics
}

func validateMetadata(node *yaml.Node) []Diagnostic {
	if node.Kind != yaml.MappingNode {
		return []Diagnostic{invalidType("metadata", "string-to-string mapping")}
	}
	var diagnostics []Diagnostic
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index]
		value := node.Content[index+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			diagnostics = append(diagnostics, invalidType("metadata", "string-to-string mapping"))
			continue
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			diagnostics = append(diagnostics, invalidType("metadata."+key.Value, "string"))
		}
	}
	return diagnostics
}

func validateTarget(node *yaml.Node, field string, allowArgumentHint bool) []Diagnostic {
	if node.Kind != yaml.MappingNode {
		return []Diagnostic{invalidType(field, "mapping")}
	}
	allowed := map[string]struct{}{"disabled": {}}
	if allowArgumentHint {
		allowed["argument-hint"] = struct{}{}
	}
	diagnostics := validateMapping(node, allowed, field)
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		value := node.Content[index+1]
		switch key {
		case "disabled":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" {
				diagnostics = append(diagnostics, invalidType(field+".disabled", "boolean"))
			}
		case "argument-hint":
			if allowArgumentHint && (value.Kind != yaml.ScalarNode || value.Tag != "!!str") {
				diagnostics = append(diagnostics, invalidType(field+".argument-hint", "string"))
			}
		}
	}
	return diagnostics
}

func invalidType(field, expected string) Diagnostic {
	return Diagnostic{Code: CodeInvalidValue, Path: "SKILL.md", Field: field, Detail: "value must be a " + expected}
}

func validateMapping(mapping *yaml.Node, allowed map[string]struct{}, prefix string) []Diagnostic {
	seen := make(map[string]struct{}, len(mapping.Content)/2)
	var diagnostics []Diagnostic
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		keyNode := mapping.Content[index]
		field := keyNode.Value
		if prefix != "" {
			field = prefix + "." + field
		}
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeUnknownField, Path: "SKILL.md", Field: field})
			continue
		}
		if _, duplicate := seen[keyNode.Value]; duplicate {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Path: "SKILL.md", Field: field, Detail: "duplicate field"})
		}
		seen[keyNode.Value] = struct{}{}
		if _, ok := allowed[keyNode.Value]; ok {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{Code: CodeUnknownField, Path: "SKILL.md", Field: field})
	}
	return diagnostics
}

func validateValues(document Document, directoryName string) []Diagnostic {
	var diagnostics []Diagnostic
	if document.Name == "" {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeRequiredField, Path: "SKILL.md", Field: "name"})
	} else {
		if len(document.Name) > maxNameLength || !namePattern.MatchString(document.Name) {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidName, Path: "SKILL.md", Field: "name"})
		}
		if document.Name != directoryName {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeNameMismatch, Path: "SKILL.md", Field: "name"})
		}
	}
	if strings.TrimSpace(document.Description) == "" {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeRequiredField, Path: "SKILL.md", Field: "description"})
	} else if utf8.RuneCountInString(document.Description) > maxDescriptionLength {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Path: "SKILL.md", Field: "description", Detail: "value exceeds 1024 characters"})
	}
	if document.Compatibility != nil && utf8.RuneCountInString(*document.Compatibility) > maxCompatibilityLength {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Path: "SKILL.md", Field: "compatibility", Detail: "value exceeds 500 characters"})
	}
	return diagnostics
}
