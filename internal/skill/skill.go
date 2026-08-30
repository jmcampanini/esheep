// Package skill parses and validates esheep's declarative skill format.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jmcampanini/esheep/internal/naming"
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
	CodeInvalidProfile  Code = "invalid-profile"
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

// TargetOptions contains the declarative esheep-targets settings for one
// render target.
type TargetOptions struct {
	Listed       bool
	OnlyProfiles []string
}

// Applies reports whether the target is listed in esheep-targets and its
// profile gate matches the active profiles. An empty gate matches every
// profile.
func (options TargetOptions) Applies(profiles []string) bool {
	if !options.Listed {
		return false
	}
	return len(options.OnlyProfiles) == 0 || intersects(options.OnlyProfiles, profiles)
}

// Targets contains settings for every supported target.
type Targets struct {
	Claude TargetOptions
	Pi     TargetOptions
	Codex  TargetOptions
	Agents TargetOptions
}

func (targets *Targets) options(name string) *TargetOptions {
	switch name {
	case "claude":
		return &targets.Claude
	case "pi":
		return &targets.Pi
	case "codex":
		return &targets.Codex
	case "agents":
		return &targets.Agents
	}
	return nil
}

func (targets Targets) all() []TargetOptions {
	return []TargetOptions{targets.Claude, targets.Pi, targets.Codex, targets.Agents}
}

// ExtraField is a frontmatter field esheep does not interpret, preserved in
// source order for verbatim pass-through to every rendered target.
type ExtraField struct {
	Key   string
	Value *yaml.Node
}

// Document is a parsed manifest and its byte-exact Markdown body.
type Document struct {
	Name                   string
	Description            string
	License                *string
	Compatibility          *string
	Metadata               map[string]string
	DisableModelInvocation bool
	OnlyProfiles           []string
	Extra                  []ExtraField
	Targets                Targets
	Body                   []byte
}

// File is a regular supporting file. Path always uses slash separators.
type File struct {
	Path string
}

// Manifest is one parsed manifest file within a skill directory.
type Manifest struct {
	FileName string
	Profile  string
	Document Document
}

// Package is a validated skill tree ready for rendering.
type Package struct {
	Root             string
	Manifests        []Manifest
	Directories      []string
	Files            []File
	sourceParentPath string
	sourceName       string
	sourceParentInfo os.FileInfo
	sourceInfo       os.FileInfo
}

// Selection describes which manifest applies under the active profiles.
type Selection struct {
	Active    bool
	Manifest  Manifest
	Conflicts []string
}

// Select resolves which manifest applies under the active profiles. A manifest
// applies when its gate is empty or names an active profile. Specific beats
// base: an active profile variant overrides SKILL.md, and two active variants
// are a conflict.
func (source Package) Select(profiles []string) Selection {
	var base Manifest
	var hasBase bool
	var variants []Manifest
	for _, manifest := range source.Manifests {
		gate := manifestGate(manifest)
		if len(gate) != 0 && !intersects(gate, profiles) {
			continue
		}
		if manifest.Profile == "" {
			base = manifest
			hasBase = true
			continue
		}
		variants = append(variants, manifest)
	}

	if len(variants) > 1 {
		conflicts := make([]string, 0, len(variants))
		for _, variant := range variants {
			conflicts = append(conflicts, variant.FileName)
		}
		return Selection{Conflicts: conflicts}
	}
	if len(variants) == 1 {
		return Selection{Active: true, Manifest: variants[0]}
	}
	if hasBase {
		return Selection{Active: true, Manifest: base}
	}
	return Selection{}
}

// Gate returns the sorted union of profile names that limit when the skill
// applies. An empty gate means the skill applies under every profile.
func (source Package) Gate() []string {
	union := make(map[string]struct{})
	for _, manifest := range source.Manifests {
		gate := manifestGate(manifest)
		if len(gate) == 0 {
			return nil
		}
		for _, profile := range gate {
			union[profile] = struct{}{}
		}
	}
	return sortedProfiles(union)
}

// ReferencedProfiles returns every valid profile name any manifest gates on,
// including per-target esheep-targets gates, sorted.
func (source Package) ReferencedProfiles() []string {
	union := make(map[string]struct{})
	collect := func(profiles []string) {
		for _, profile := range profiles {
			if naming.ValidateProfileName(profile) != nil {
				continue
			}
			union[profile] = struct{}{}
		}
	}
	for _, manifest := range source.Manifests {
		collect(manifestGate(manifest))
		for _, options := range manifest.Document.Targets.all() {
			collect(options.OnlyProfiles)
		}
	}

	return sortedProfiles(union)
}

// Identity returns the validated skill name shared by the manifests, when any
// manifest carries one.
func (source Package) Identity(directoryName string) (string, bool) {
	for _, manifest := range source.Manifests {
		if ValidIdentity(manifest.Document.Name, directoryName) {
			return manifest.Document.Name, true
		}
	}
	return "", false
}

// manifestGate unions the filename profile with the frontmatter gate. An empty
// result means the manifest applies under every profile.
func manifestGate(manifest Manifest) []string {
	union := make(map[string]struct{}, len(manifest.Document.OnlyProfiles)+1)
	if manifest.Profile != "" {
		union[manifest.Profile] = struct{}{}
	}
	for _, profile := range manifest.Document.OnlyProfiles {
		union[profile] = struct{}{}
	}
	return sortedProfiles(union)
}

func sortedProfiles(union map[string]struct{}) []string {
	if len(union) == 0 {
		return nil
	}
	profiles := make([]string, 0, len(union))
	for profile := range union {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)
	return profiles
}

func intersects(gate, profiles []string) bool {
	for _, profile := range profiles {
		for _, gated := range gate {
			if profile == gated {
				return true
			}
		}
	}
	return false
}

type rawDocument struct {
	Name                   string            `yaml:"name"`
	Description            string            `yaml:"description"`
	License                *string           `yaml:"license"`
	Compatibility          *string           `yaml:"compatibility"`
	Metadata               map[string]string `yaml:"metadata"`
	DisableModelInvocation bool              `yaml:"disable-model-invocation"`
	OnlyProfiles           []string          `yaml:"esheep-only-profiles"`
}

// Parse parses frontmatter, preserves the Markdown body, and validates the
// fields esheep interprets. Fields it does not interpret are preserved in
// order for pass-through rendering, except that the esheep- key prefix is a
// reserved namespace and unknown keys within it are errors. It returns a
// partially decoded document with a ValidationError when possible so
// discovery can still classify identities. Diagnostics report fileName as
// their path.
func Parse(data []byte, directoryName, fileName string) (Document, error) {
	document, diagnostics := parse(data, directoryName)
	for index := range diagnostics {
		diagnostics[index].Path = fileName
	}
	if len(diagnostics) != 0 {
		return document, &ValidationError{Diagnostics: diagnostics}
	}
	return document, nil
}

func parse(data []byte, directoryName string) (Document, []Diagnostic) {
	frontmatter, body, splitErr := split(data)
	if splitErr != nil {
		return Document{}, []Diagnostic{{Code: CodeFrontmatter, Err: splitErr}}
	}

	var root yaml.Node
	unmarshalErr := yaml.Unmarshal(frontmatter, &root)
	mapping, mappingErr := documentMapping(&root)
	if mappingErr != nil {
		if unmarshalErr != nil {
			mappingErr = unmarshalErr
		}
		return Document{Body: body}, []Diagnostic{{Code: CodeYAML, Err: mappingErr}}
	}

	extra, targets, targetsSeen, diagnostics := validateShape(mapping)
	if unmarshalErr != nil {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeYAML, Err: unmarshalErr})
	}
	if !targetsSeen {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeRequiredField, Field: "esheep-targets"})
	}
	var raw rawDocument
	if err := mapping.Decode(&raw); err != nil {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeYAML, Err: err})
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
		OnlyProfiles:           raw.OnlyProfiles,
		Extra:                  extra,
		Targets:                targets,
		Body:                   body,
	}
	diagnostics = append(diagnostics, validateValues(document, directoryName)...)
	return document, diagnostics
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

func validateShape(mapping *yaml.Node) ([]ExtraField, Targets, bool, []Diagnostic) {
	var extra []ExtraField
	var targets Targets
	var targetsSeen bool
	var diagnostics []Diagnostic
	seen := make(map[string]struct{}, len(mapping.Content)/2)
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		keyNode := mapping.Content[index]
		value := mapping.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Detail: "frontmatter keys must be strings"})
			continue
		}
		key := keyNode.Value
		if _, duplicate := seen[key]; duplicate {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Field: key, Detail: "duplicate field"})
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
		case "esheep-only-profiles":
			diagnostics = append(diagnostics, validateOnlyProfiles(value)...)
		case "esheep-targets":
			targetsSeen = true
			var targetDiagnostics []Diagnostic
			targets, targetDiagnostics = parseTargets(value)
			diagnostics = append(diagnostics, targetDiagnostics...)
		default:
			if strings.HasPrefix(key, "esheep-") {
				diagnostics = append(diagnostics, Diagnostic{Code: CodeUnknownField, Field: key, Detail: "the esheep- prefix is reserved"})
				continue
			}
			extra = append(extra, ExtraField{Key: key, Value: value})
		}
	}
	return extra, targets, targetsSeen, diagnostics
}

// parseTargets decodes the esheep-targets sequence, where each entry is a
// target name or a single-pair mapping from a target name to its profile gate.
func parseTargets(node *yaml.Node) (Targets, []Diagnostic) {
	var targets Targets
	if node.Kind != yaml.SequenceNode {
		return targets, []Diagnostic{invalidType("esheep-targets", "list of target entries")}
	}
	if len(node.Content) == 0 {
		return targets, []Diagnostic{{Code: CodeInvalidValue, Field: "esheep-targets", Detail: "value must not be empty"}}
	}
	var diagnostics []Diagnostic
	seen := make(map[string]struct{}, len(node.Content))
	for _, item := range node.Content {
		name, gate, entryDiagnostics := parseTargetEntry(item)
		diagnostics = append(diagnostics, entryDiagnostics...)
		if name == "" {
			continue
		}
		options := targets.options(name)
		if options == nil {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Field: "esheep-targets", Detail: fmt.Sprintf("unknown target %q", name)})
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Field: "esheep-targets", Detail: fmt.Sprintf("duplicate target %q", name)})
			continue
		}
		seen[name] = struct{}{}
		options.Listed = true
		options.OnlyProfiles = gate
	}
	return targets, diagnostics
}

// parseTargetEntry decodes one esheep-targets entry. An empty name means the
// entry shape was invalid and already diagnosed.
func parseTargetEntry(item *yaml.Node) (string, []string, []Diagnostic) {
	if item.Kind == yaml.ScalarNode && item.Tag == "!!str" {
		return item.Value, nil, nil
	}
	if item.Kind != yaml.MappingNode || len(item.Content) != 2 ||
		item.Content[0].Kind != yaml.ScalarNode || item.Content[0].Tag != "!!str" {
		return "", nil, []Diagnostic{{Code: CodeInvalidValue, Field: "esheep-targets", Detail: "entry must be a target name or a single target-to-profiles pair"}}
	}
	target := item.Content[0].Value
	gate, diagnostics := parseProfileList(item.Content[1], "esheep-targets."+target)
	return target, gate, diagnostics
}

func validateOnlyProfiles(node *yaml.Node) []Diagnostic {
	_, diagnostics := parseProfileList(node, "esheep-only-profiles")
	return diagnostics
}

func parseProfileList(node *yaml.Node, field string) ([]string, []Diagnostic) {
	if node.Kind != yaml.SequenceNode {
		return nil, []Diagnostic{invalidType(field, "list of strings")}
	}
	if len(node.Content) == 0 {
		return nil, []Diagnostic{{Code: CodeInvalidValue, Field: field, Detail: "value must not be empty"}}
	}
	var profiles []string
	var diagnostics []Diagnostic
	seen := make(map[string]struct{}, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			diagnostics = append(diagnostics, invalidType(field, "list of strings"))
			continue
		}
		if err := naming.ValidateProfileName(item.Value); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidProfile, Field: field, Err: err})
			continue
		}
		if _, duplicate := seen[item.Value]; duplicate {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Field: field, Detail: fmt.Sprintf("duplicate profile %q", item.Value)})
			continue
		}
		seen[item.Value] = struct{}{}
		profiles = append(profiles, item.Value)
	}
	return profiles, diagnostics
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

func invalidType(field, expected string) Diagnostic {
	return Diagnostic{Code: CodeInvalidValue, Field: field, Detail: "value must be a " + expected}
}

func validateValues(document Document, directoryName string) []Diagnostic {
	var diagnostics []Diagnostic
	if document.Name == "" {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeRequiredField, Field: "name"})
	} else {
		if len(document.Name) > maxNameLength || !namePattern.MatchString(document.Name) {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidName, Field: "name"})
		}
		if document.Name != directoryName {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeNameMismatch, Field: "name"})
		}
	}
	if strings.TrimSpace(document.Description) == "" {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeRequiredField, Field: "description"})
	} else if utf8.RuneCountInString(document.Description) > maxDescriptionLength {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Field: "description", Detail: "value exceeds 1024 characters"})
	}
	if document.Compatibility != nil && utf8.RuneCountInString(*document.Compatibility) > maxCompatibilityLength {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidValue, Field: "compatibility", Detail: "value exceeds 500 characters"})
	}
	return diagnostics
}
