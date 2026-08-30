package skill

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/jmcampanini/esheep/internal/naming"
)

const manifestName = "SKILL.md"

// ParseManifestName classifies a skill-root filename. It reports the profile
// named by a variant manifest ("" for SKILL.md), whether the name is a
// manifest at all, and an error when the name sits in the reserved
// SKILL.<profile>.md namespace with an invalid profile segment.
func ParseManifestName(name string) (string, bool, error) {
	if name == manifestName {
		return "", true, nil
	}
	parts := strings.Split(name, ".")
	if len(parts) != 3 || parts[0] != "SKILL" || parts[2] != "md" {
		return "", false, nil
	}
	if err := naming.ValidateProfileName(parts[1]); err != nil {
		return "", false, err
	}
	return parts[1], true, nil
}

// Load reads and validates one recognized skill directory without modifying
// it. Symlinks are followed wherever they resolve; a link that does not
// resolve is a diagnostic.
func Load(root string) (Package, error) {
	result := Package{Root: root}
	entries, err := os.ReadDir(root)
	if err != nil {
		return result, validationError(CodeUnreadable, ".", err)
	}
	manifests, diagnostics := loadManifests(root, entries, filepath.Base(root))
	result.Manifests = manifests
	directories, files, treeDiagnostics := loadTree(root, entries)
	result.Directories = directories
	result.Files = files
	diagnostics = append(diagnostics, treeDiagnostics...)
	diagnostics = append(diagnostics, ValidateTree(result)...)
	if len(diagnostics) != 0 {
		return result, &ValidationError{Diagnostics: diagnostics}
	}
	return result, nil
}

func loadManifests(root string, entries []os.DirEntry, directoryName string) ([]Manifest, []Diagnostic) {
	var manifests []Manifest
	var diagnostics []Diagnostic
	for _, entry := range entries {
		name := entry.Name()
		profile, ok, nameErr := ParseManifestName(name)
		if nameErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidProfile, Path: name, Err: nameErr})
			continue
		}
		if !ok {
			continue
		}
		data, readErr := readManifest(root, name)
		if readErr != nil {
			diagnostics = append(diagnostics, ErrorDiagnostics(readErr)...)
			continue
		}
		document, parseErr := Parse(data, directoryName, name)
		diagnostics = append(diagnostics, ErrorDiagnostics(parseErr)...)
		manifests = append(manifests, Manifest{FileName: name, Profile: profile, Document: document})
	}
	if len(manifests) == 0 && len(diagnostics) == 0 {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: manifestName, Err: fs.ErrNotExist})
	}

	sort.Slice(manifests, func(left, right int) bool {
		if (manifests[left].Profile == "") != (manifests[right].Profile == "") {
			return manifests[left].Profile == ""
		}
		return manifests[left].Profile < manifests[right].Profile
	})
	return manifests, diagnostics
}

func readManifest(root, name string) ([]byte, error) {
	manifest, openErr := os.OpenFile(filepath.Join(root, name), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if openErr != nil {
		return nil, classifyManifestOpenError(root, name, openErr)
	}
	info, statErr := manifest.Stat()
	if statErr != nil {
		return nil, validationError(CodeUnreadable, name, closeFile(manifest, statErr))
	}
	if !info.Mode().IsRegular() {
		return nil, validationError(CodeUnsupportedFile, name, closeFile(manifest, nil))
	}

	data, readErr := io.ReadAll(manifest)
	if err := closeFile(manifest, readErr); err != nil {
		return nil, validationError(CodeUnreadable, name, err)
	}
	return data, nil
}

// classifyManifestOpenError distinguishes manifests that resolve to an
// unopenable special file from manifests that cannot be read at all.
func classifyManifestOpenError(root, name string, openErr error) error {
	if info, statErr := os.Stat(filepath.Join(root, name)); statErr == nil && !info.Mode().IsRegular() {
		return validationError(CodeUnsupportedFile, name, openErr)
	}
	return validationError(CodeUnreadable, name, openErr)
}

func validationError(code Code, diagnosticPath string, err error) error {
	return &ValidationError{Diagnostics: []Diagnostic{{Code: code, Path: diagnosticPath, Err: err}}}
}

func loadTree(root string, entries []os.DirEntry) ([]string, []File, []Diagnostic) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, nil, []Diagnostic{{Code: CodeUnreadable, Path: ".", Err: err}}
	}
	walker := treeWalker{root: root}
	walker.walkEntries(".", entries, []os.FileInfo{info})
	sort.Strings(walker.directories)
	sort.Slice(walker.files, func(left, right int) bool { return walker.files[left].Path < walker.files[right].Path })
	return walker.directories, walker.files, walker.diagnostics
}

// treeWalker accumulates the supporting tree, following symlinks as if their
// targets sat in place. A directory that resolves to one of its own ancestors
// is a cycle.
type treeWalker struct {
	diagnostics []Diagnostic
	directories []string
	files       []File
	root        string
}

func (walker *treeWalker) walkEntries(relative string, entries []os.DirEntry, ancestors []os.FileInfo) {
	for _, entry := range entries {
		name := entry.Name()
		childRelative := relative + "/" + name
		if relative == "." {
			childRelative = name
			if _, manifest, nameErr := ParseManifestName(name); manifest || nameErr != nil {
				continue
			}
			if strings.EqualFold(name, ".esheep.toml") {
				walker.diagnostics = append(walker.diagnostics, Diagnostic{Code: CodeReservedPath, Path: name})
				continue
			}
		}
		walker.walkEntry(childRelative, ancestors)
	}
}

func (walker *treeWalker) walkEntry(relative string, ancestors []os.FileInfo) {
	absolute := filepath.Join(walker.root, filepath.FromSlash(relative))
	info, err := os.Stat(absolute)
	if err != nil {
		walker.diagnostics = append(walker.diagnostics, Diagnostic{Code: CodeUnreadable, Path: relative, Err: err})
		return
	}
	switch {
	case info.IsDir():
		for _, ancestor := range ancestors {
			if os.SameFile(ancestor, info) {
				walker.diagnostics = append(walker.diagnostics, Diagnostic{Code: CodeUnreadable, Path: relative, Err: fmt.Errorf("symlink cycle")})
				return
			}
		}
		entries, readErr := os.ReadDir(absolute)
		if readErr != nil {
			walker.diagnostics = append(walker.diagnostics, Diagnostic{Code: CodeUnreadable, Path: relative, Err: readErr})
			return
		}
		walker.directories = append(walker.directories, relative)
		walker.walkEntries(relative, entries, append(ancestors, info))
	case info.Mode().IsRegular():
		if readErr := validateReadableRegular(absolute); readErr != nil {
			walker.diagnostics = append(walker.diagnostics, Diagnostic{Code: CodeUnreadable, Path: relative, Err: readErr})
			return
		}
		walker.files = append(walker.files, File{Path: relative})
	default:
		walker.diagnostics = append(walker.diagnostics, Diagnostic{Code: CodeUnsupportedFile, Path: relative})
	}
}

func validateReadableRegular(absolute string) error {
	file, err := os.OpenFile(absolute, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil {
		return errors.Join(statErr, closeErr)
	}
	if !info.Mode().IsRegular() {
		return errors.Join(fmt.Errorf("opened file is not regular"), closeErr)
	}
	return closeErr
}

func closeFile(file *os.File, primary error) error {
	return errors.Join(primary, file.Close())
}
