package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const manifestName = "SKILL.md"

// Load reads and validates one recognized skill directory without modifying it.
func Load(root string) (Package, error) {
	manifestPath := filepath.Join(root, manifestName)
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return Package{}, &ValidationError{Diagnostics: []Diagnostic{{Code: CodeUnreadable, Path: manifestName, Err: err}}}
	}
	document, parseErr := Parse(manifest, filepath.Base(root))
	result := Package{Document: document}
	diagnostics := ErrorDiagnostics(parseErr)

	directories, files, treeDiagnostics := loadTree(root)
	result.Directories = directories
	result.Files = files
	diagnostics = append(diagnostics, treeDiagnostics...)
	if len(diagnostics) != 0 {
		return result, &ValidationError{Diagnostics: diagnostics}
	}
	return result, nil
}

func loadTree(root string) ([]string, []File, []Diagnostic) {
	var directories []string
	var files []File
	var diagnostics []Diagnostic
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return relativeErr
		}
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: filepath.ToSlash(relative), Err: err})
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if relative == "." {
			return nil
		}
		slashPath := filepath.ToSlash(relative)
		if relative == manifestName {
			return nil
		}
		if strings.EqualFold(relative, ".esheep.toml") {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeReservedPath, Path: slashPath})
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: slashPath, Err: infoErr})
			return nil
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			directories = append(directories, slashPath)
		case mode.IsRegular():
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: slashPath, Err: readErr})
				return nil
			}
			files = append(files, File{Path: slashPath, Data: data})
		case mode&os.ModeSymlink != 0:
			data, resolveErr := resolveSupportingLink(root, path)
			if resolveErr != nil {
				diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidSymlink, Path: slashPath, Err: resolveErr})
				return nil
			}
			files = append(files, File{Path: slashPath, Data: data})
		default:
			diagnostics = append(diagnostics, Diagnostic{Code: CodeUnsupportedFile, Path: slashPath})
		}
		return nil
	})
	if walkErr != nil {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: ".", Err: walkErr})
	}
	sort.Strings(directories)
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	return directories, files, diagnostics
}

func resolveSupportingLink(root, linkPath string) ([]byte, error) {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return nil, err
	}
	if filepath.IsAbs(target) {
		return nil, fmt.Errorf("absolute target")
	}
	return resolveRegular(root, filepath.Join(filepath.Dir(linkPath), target), make(map[string]struct{}))
}

func resolveRegular(root, candidate string, seen map[string]struct{}) ([]byte, error) {
	relative, err := filepath.Rel(root, filepath.Clean(candidate))
	if err != nil {
		return nil, err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, fmt.Errorf("target escapes skill root")
	}
	parts := strings.Split(relative, string(filepath.Separator))
	cursor := root
	for index, part := range parts {
		cursor = filepath.Join(cursor, part)
		info, statErr := os.Lstat(cursor)
		if statErr != nil {
			return nil, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			canonicalLink := filepath.Clean(cursor)
			if _, duplicate := seen[canonicalLink]; duplicate {
				return nil, fmt.Errorf("cyclic target")
			}
			seen[canonicalLink] = struct{}{}
			target, readErr := os.Readlink(cursor)
			if readErr != nil {
				return nil, readErr
			}
			if filepath.IsAbs(target) {
				return nil, fmt.Errorf("absolute target")
			}
			remainder := filepath.Join(parts[index+1:]...)
			return resolveRegular(root, filepath.Join(filepath.Dir(cursor), target, remainder), seen)
		}
		if index != len(parts)-1 {
			if !info.IsDir() {
				return nil, fmt.Errorf("non-directory path component")
			}
			continue
		}
		if info.IsDir() {
			return nil, fmt.Errorf("target is a directory")
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("target is not a regular file")
		}
		data, readErr := os.ReadFile(cursor)
		if readErr != nil {
			return nil, readErr
		}
		return data, nil
	}
	return nil, errors.New("empty target")
}
