// Package render writes deterministic target-specific skill trees.
package render

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/jmcampanini/esheep/internal/skill"
	"go.yaml.in/yaml/v3"
)

// Target identifies a supported output schema.
type Target string

// Supported render targets.
const (
	TargetClaude Target = "claude"
	TargetPi     Target = "pi"
	TargetCodex  Target = "codex"
	TargetAgents Target = "agents"
)

// Disabled reports whether the manifest's esheep-targets list excludes a
// target under the active profiles.
func Disabled(document skill.Document, target Target, profiles []string) (bool, error) {
	switch target {
	case TargetClaude:
		return !document.Targets.Claude.Applies(profiles), nil
	case TargetPi:
		return !document.Targets.Pi.Applies(profiles), nil
	case TargetCodex:
		return !document.Targets.Codex.Applies(profiles), nil
	case TargetAgents:
		return !document.Targets.Agents.Applies(profiles), nil
	default:
		return false, fmt.Errorf("render: unsupported target %q", target)
	}
}

// Render writes a skill into an existing empty staging directory using the
// selected manifest document. A false result means the document disabled this
// target under the active profiles and the directory was untouched.
func Render(staging string, source skill.Package, document skill.Document, target Target, profiles []string) (bool, error) {
	disabled, err := Disabled(document, target, profiles)
	if err != nil {
		return false, err
	}
	if disabled {
		return false, nil
	}
	staging = filepath.Clean(staging)
	if diagnostics := skill.ValidateTree(source); len(diagnostics) != 0 {
		return false, fmt.Errorf("render tree: %w", &skill.ValidationError{Diagnostics: diagnostics})
	}
	if err := validateStaging(staging); err != nil {
		return false, err
	}

	manifest, err := renderManifest(document)
	if err != nil {
		return false, err
	}
	if err := renderTree(staging, source, document, manifest, target); err != nil {
		return false, err
	}
	return true, nil
}

// codexPolicyPath is the Codex per-skill metadata file that carries invocation
// policy. Codex ignores unknown SKILL.md frontmatter, so disable-model-invocation
// is expressed there instead.
const codexPolicyPath = "agents/openai.yaml"

const codexPolicyManifest = "policy:\n  allow_implicit_invocation: false\n"

func renderTree(staging string, source skill.Package, document skill.Document, manifest []byte, target Target) error {
	if err := os.Chmod(staging, 0o755); err != nil {
		return err
	}
	for _, directory := range source.Directories {
		if err := makeDirectory(staging, directory); err != nil {
			return err
		}
	}
	if err := writeRegular(staging, "SKILL.md", bytes.NewReader(manifest)); err != nil {
		return err
	}
	for _, file := range source.Files {
		if err := copySupportFile(source.Root, staging, file.Path); err != nil {
			return err
		}
	}
	if target == TargetCodex && document.DisableModelInvocation && !hasFile(source, codexPolicyPath) {
		if err := writeRegular(staging, codexPolicyPath, strings.NewReader(codexPolicyManifest)); err != nil {
			return err
		}
	}
	return nil
}

// hasFile reports whether the source skill provides its own copy of path. A
// source-provided file is authoritative and suppresses generation.
func hasFile(source skill.Package, path string) bool {
	for _, file := range source.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func renderManifest(document skill.Document) ([]byte, error) {
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	appendString := func(key, value string) {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
		)
	}
	appendString("name", document.Name)
	appendString("description", document.Description)
	if document.License != nil {
		appendString("license", *document.License)
	}
	if document.Compatibility != nil {
		appendString("compatibility", *document.Compatibility)
	}
	if document.Metadata != nil {
		metadata := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := make([]string, 0, len(document.Metadata))
		for key := range document.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			metadata.Content = append(metadata.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: document.Metadata[key]},
			)
		}
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "metadata"}, metadata,
		)
	}
	if document.DisableModelInvocation {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "disable-model-invocation"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
		)
	}
	for _, field := range document.Extra {
		mapping.Content = append(mapping.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: field.Key},
			field.Value,
		)
	}

	var yamlText bytes.Buffer
	encoder := yaml.NewEncoder(&yamlText)
	encoder.SetIndent(2)
	if err := encoder.Encode(mapping); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	result := make([]byte, 0, yamlText.Len()+len(document.Body)+8)
	result = append(result, "---\n"...)
	result = append(result, yamlText.Bytes()...)
	result = append(result, "---\n"...)
	result = append(result, document.Body...)
	return result, nil
}

func validateStaging(staging string) error {
	info, err := os.Lstat(staging)
	if err != nil {
		return fmt.Errorf("render staging: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("render staging: not a regular directory")
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return fmt.Errorf("render staging: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("render staging: directory is not empty")
	}
	return nil
}

func makeDirectory(staging, relative string) error {
	destination := filepath.Join(staging, filepath.FromSlash(relative))
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	return os.Chmod(destination, 0o755)
}

func copySupportFile(root, staging, relative string) error {
	source, err := os.OpenFile(filepath.Join(root, filepath.FromSlash(relative)), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("render source %q: %w", relative, err)
	}
	info, err := source.Stat()
	if err != nil {
		return closeFiles(err, source)
	}
	if !info.Mode().IsRegular() {
		return closeFiles(fmt.Errorf("render source %q is not a regular file", relative), source)
	}
	destination, err := createRegular(staging, relative)
	if err != nil {
		return closeFiles(err, source)
	}
	_, copyErr := io.Copy(destination, source)
	if err := closeFiles(copyErr, destination, source); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(staging, filepath.FromSlash(relative)), 0o644)
}

func writeRegular(staging, relative string, source io.Reader) error {
	destination, err := createRegular(staging, relative)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, source)
	if err := closeFiles(copyErr, destination); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(staging, filepath.FromSlash(relative)), 0o644)
}

func createRegular(staging, relative string) (*os.File, error) {
	destination := filepath.Join(staging, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return nil, err
	}
	if err := chmodParents(staging, filepath.Dir(destination)); err != nil {
		return nil, err
	}
	return os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

func closeFiles(primary error, files ...*os.File) error {
	errs := []error{primary}
	for _, file := range files {
		errs = append(errs, file.Close())
	}
	return errors.Join(errs...)
}

func chmodParents(root, directory string) error {
	root = filepath.Clean(root)
	directory = filepath.Clean(directory)
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("render path escaped staging directory")
	}
	for directory != root {
		if err := os.Chmod(directory, 0o755); err != nil {
			return err
		}
		directory = filepath.Dir(directory)
	}
	return nil
}
