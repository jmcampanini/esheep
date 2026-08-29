// Package render writes deterministic target-specific skill trees.
package render

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmcampanini/esheep/internal/skill"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
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

// Render writes a skill into an existing empty staging directory. A false
// result means the skill disabled this target and the directory was untouched.
func Render(staging string, source skill.Package, target Target) (bool, error) {
	options, err := targetOptions(source.Document.Targets, target)
	if err != nil {
		return false, err
	}
	if options.Disabled {
		return false, nil
	}
	staging = filepath.Clean(staging)
	if err := validateTree(source); err != nil {
		return false, err
	}
	if err := validateStaging(staging); err != nil {
		return false, err
	}

	manifest, err := renderManifest(source.Document, options.ArgumentHint, target)
	if err != nil {
		return false, err
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		return false, err
	}
	for _, directory := range source.Directories {
		if err := makeDirectory(staging, directory); err != nil {
			return false, err
		}
	}
	if err := writeRegular(staging, "SKILL.md", manifest); err != nil {
		return false, err
	}
	for _, file := range source.Files {
		if err := writeRegular(staging, file.Path, file.Data); err != nil {
			return false, err
		}
	}
	return true, nil
}

func targetOptions(targets skill.Targets, target Target) (skill.TargetOptions, error) {
	switch target {
	case TargetClaude:
		return targets.Claude, nil
	case TargetPi:
		return targets.Pi, nil
	case TargetCodex:
		return targets.Codex, nil
	case TargetAgents:
		return targets.Agents, nil
	default:
		return skill.TargetOptions{}, fmt.Errorf("render: unsupported target %q", target)
	}
}

func renderManifest(document skill.Document, argumentHint *string, target Target) ([]byte, error) {
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
	if (target == TargetClaude || target == TargetPi) && argumentHint != nil {
		appendString("argument-hint", *argumentHint)
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

func validateTree(source skill.Package) error {
	entries := map[string]bool{canonicalPathKey("SKILL.md"): false}
	for _, directory := range source.Directories {
		if err := addTreeEntry(entries, directory, true); err != nil {
			return err
		}
	}
	for _, file := range source.Files {
		if err := addTreeEntry(entries, file.Path, false); err != nil {
			return err
		}
	}
	return nil
}

func addTreeEntry(entries map[string]bool, relative string, directory bool) error {
	if !validRelativePath(relative) {
		return fmt.Errorf("render path %q is unsafe", relative)
	}
	key := canonicalPathKey(relative)
	if !strings.ContainsRune(key, '/') && key == ".esheep.toml" {
		return fmt.Errorf("render path %q is reserved", relative)
	}
	if _, duplicate := entries[key]; duplicate {
		return fmt.Errorf("render path %q is duplicated", relative)
	}
	for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
		if parentDirectory, exists := entries[parent]; exists && !parentDirectory {
			return fmt.Errorf("render path %q descends through a file", relative)
		}
	}
	if !directory {
		prefix := key + "/"
		for existing := range entries {
			if strings.HasPrefix(existing, prefix) {
				return fmt.Errorf("render file %q contains another path", relative)
			}
		}
	}
	entries[key] = directory
	return nil
}

func canonicalPathKey(relative string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(relative)))
}

func validRelativePath(relative string) bool {
	return relative != "" && relative != "." && !strings.ContainsRune(relative, '\\') &&
		!path.IsAbs(relative) && path.Clean(relative) == relative &&
		relative != ".." && !strings.HasPrefix(relative, "../")
}

func makeDirectory(staging, relative string) error {
	destination := filepath.Join(staging, filepath.FromSlash(relative))
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	return os.Chmod(destination, 0o755)
}

func writeRegular(staging, relative string, data []byte) error {
	destination := filepath.Join(staging, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := chmodParents(staging, filepath.Dir(destination)); err != nil {
		return err
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		return err
	}
	return os.Chmod(destination, 0o644)
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
