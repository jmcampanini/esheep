package skill

import (
	"path"
	"strings"

	"github.com/jmcampanini/esheep/internal/naming"
)

// ValidateTree validates package paths using target-filesystem comparison rules.
func ValidateTree(source Package) []Diagnostic {
	entries := map[string]bool{naming.PathKey(manifestName): false}
	for _, manifest := range source.Manifests {
		entries[naming.PathKey(manifest.FileName)] = false
	}
	var diagnostics []Diagnostic
	for _, directory := range source.Directories {
		diagnostics = append(diagnostics, validateTreeEntry(entries, directory, true)...)
	}
	for _, file := range source.Files {
		diagnostics = append(diagnostics, validateTreeEntry(entries, file.Path, false)...)
	}
	return diagnostics
}

func validateTreeEntry(entries map[string]bool, relative string, directory bool) []Diagnostic {
	if !validRelativePath(relative) {
		return []Diagnostic{{Code: CodeUnsupportedFile, Path: relative, Detail: "path is unsafe"}}
	}
	key := naming.PathKey(relative)
	if !strings.ContainsRune(key, '/') && key == ".esheep.toml" {
		return []Diagnostic{{Code: CodeReservedPath, Path: relative}}
	}
	if _, duplicate := entries[key]; duplicate {
		return []Diagnostic{{Code: CodePathCollision, Path: relative, Detail: "path is duplicated under case-insensitive Unicode-normalized comparison"}}
	}
	for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
		if parentDirectory, exists := entries[parent]; exists && !parentDirectory {
			return []Diagnostic{{Code: CodePathCollision, Path: relative, Detail: "path descends through a file"}}
		}
	}
	if !directory {
		prefix := key + "/"
		for existing := range entries {
			if strings.HasPrefix(existing, prefix) {
				return []Diagnostic{{Code: CodePathCollision, Path: relative, Detail: "file contains another path"}}
			}
		}
	}
	entries[key] = directory
	return nil
}

func validRelativePath(relative string) bool {
	return relative != "" && relative != "." && !strings.ContainsRune(relative, '\\') &&
		!path.IsAbs(relative) && path.Clean(relative) == relative &&
		relative != ".." && !strings.HasPrefix(relative, "../")
}
