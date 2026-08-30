package skill

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
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

// Load reads and validates one recognized skill directory without modifying it.
func Load(root string) (Package, error) {
	result := Package{Root: root}
	sourceRoot, openErr := result.captureSourceRoot()
	if openErr != nil {
		return result, validationError(CodeUnreadable, ".", openErr)
	}
	manifests, diagnostics := loadManifests(sourceRoot, filepath.Base(root))
	result.Manifests = manifests
	if closeErr := sourceRoot.Close(); closeErr != nil {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: ".", Err: closeErr})
	}

	sourceRoot, openErr = result.OpenSourceRoot()
	if openErr != nil {
		diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: ".", Err: openErr})
	} else {
		directories, files, treeDiagnostics := loadTree(sourceRoot)
		result.Directories = directories
		result.Files = files
		diagnostics = append(diagnostics, treeDiagnostics...)
		diagnostics = append(diagnostics, ValidateTree(result)...)
		if closeErr := sourceRoot.Close(); closeErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: ".", Err: closeErr})
		}
	}
	if len(diagnostics) != 0 {
		return result, &ValidationError{Diagnostics: diagnostics}
	}
	return result, nil
}

// OpenSourceRoot opens the loaded skill through its captured source collection
// and rejects replacement of either directory.
func (source Package) OpenSourceRoot() (*os.Root, error) {
	if source.sourceParentPath == "" || source.sourceName == "" || source.sourceParentInfo == nil || source.sourceInfo == nil {
		return nil, fmt.Errorf("skill source identity is unavailable")
	}
	parentRoot, err := os.OpenRoot(source.sourceParentPath)
	if err != nil {
		return nil, err
	}
	parentInfo, err := parentRoot.Stat(".")
	if err != nil {
		return nil, closeRoot(parentRoot, err)
	}
	if !os.SameFile(source.sourceParentInfo, parentInfo) {
		return nil, closeRoot(parentRoot, fmt.Errorf("skill source parent was replaced"))
	}
	skillRoot, err := parentRoot.OpenRoot(source.sourceName)
	if err != nil {
		return nil, closeRoot(parentRoot, err)
	}
	skillInfo, err := skillRoot.Stat(".")
	if err != nil {
		return nil, errors.Join(closeRoot(skillRoot, err), parentRoot.Close())
	}
	if !os.SameFile(source.sourceInfo, skillInfo) {
		return nil, errors.Join(fmt.Errorf("skill source was replaced"), skillRoot.Close(), parentRoot.Close())
	}
	if err := parentRoot.Close(); err != nil {
		return nil, closeRoot(skillRoot, err)
	}
	return skillRoot, nil
}

func (source *Package) captureSourceRoot() (*os.Root, error) {
	cleanRoot := filepath.Clean(source.Root)
	source.sourceParentPath = filepath.Dir(cleanRoot)
	source.sourceName = filepath.Base(cleanRoot)
	parentRoot, err := os.OpenRoot(source.sourceParentPath)
	if err != nil {
		return nil, err
	}
	parentInfo, err := parentRoot.Stat(".")
	if err != nil {
		return nil, closeRoot(parentRoot, err)
	}
	skillRoot, err := parentRoot.OpenRoot(source.sourceName)
	if err != nil {
		return nil, closeRoot(parentRoot, err)
	}
	skillInfo, err := skillRoot.Stat(".")
	if err != nil {
		return nil, errors.Join(closeRoot(skillRoot, err), parentRoot.Close())
	}
	if err := parentRoot.Close(); err != nil {
		return nil, closeRoot(skillRoot, err)
	}
	source.sourceParentInfo = parentInfo
	source.sourceInfo = skillInfo
	return skillRoot, nil
}

func loadManifests(root *os.Root, directoryName string) ([]Manifest, []Diagnostic) {
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, []Diagnostic{{Code: CodeUnreadable, Path: ".", Err: err}}
	}

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

func readManifest(root *os.Root, name string) ([]byte, error) {
	expected, err := preclassifyManifest(root, name)
	if err != nil {
		return nil, err
	}

	manifest, openErr := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
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
	// Root.OpenFile follows in-root symlinks even with O_NOFOLLOW, so the
	// identity comparison is what pins the open to the Lstat-classified file.
	if !os.SameFile(expected, info) {
		return nil, validationError(CodeUnreadable, name, closeFile(manifest, fmt.Errorf("manifest was replaced")))
	}

	data, readErr := io.ReadAll(manifest)
	if err := closeFile(manifest, readErr); err != nil {
		return nil, validationError(CodeUnreadable, name, err)
	}
	return data, nil
}

func preclassifyManifest(root *os.Root, name string) (os.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, validationError(CodeUnreadable, name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, validationError(CodeInvalidSymlink, name, nil)
	}
	if !info.Mode().IsRegular() {
		return nil, validationError(CodeUnsupportedFile, name, nil)
	}
	return info, nil
}

func classifyManifestOpenError(root *os.Root, name string, openErr error) error {
	info, statErr := root.Lstat(name)
	if statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return validationError(CodeInvalidSymlink, name, openErr)
		}
		if !info.Mode().IsRegular() {
			return validationError(CodeUnsupportedFile, name, openErr)
		}
	}
	if errors.Is(openErr, syscall.ELOOP) {
		return validationError(CodeInvalidSymlink, name, errors.Join(openErr, statErr))
	}
	return validationError(CodeUnreadable, name, errors.Join(openErr, statErr))
}

func validationError(code Code, diagnosticPath string, err error) error {
	return &ValidationError{Diagnostics: []Diagnostic{{Code: code, Path: diagnosticPath, Err: err}}}
}

func loadTree(root *os.Root) ([]string, []File, []Diagnostic) {
	var directories []string
	var files []File
	var diagnostics []Diagnostic
	walkErr := fs.WalkDir(root.FS(), ".", func(relative string, entry fs.DirEntry, err error) error {
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: relative, Err: err})
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if relative == "." {
			return nil
		}
		if !strings.Contains(relative, "/") {
			if _, manifest, nameErr := ParseManifestName(relative); manifest || nameErr != nil {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if strings.EqualFold(relative, ".esheep.toml") {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeReservedPath, Path: relative})
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: relative, Err: infoErr})
			return nil
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			directories = append(directories, relative)
		case mode.IsRegular():
			if readErr := validateReadableRegular(root, relative); readErr != nil {
				diagnostics = append(diagnostics, Diagnostic{Code: CodeUnreadable, Path: relative, Err: readErr})
				return nil
			}
			files = append(files, File{Path: relative})
		case mode&os.ModeSymlink != 0:
			if resolveErr := resolveSupportingLink(root, relative); resolveErr != nil {
				diagnostics = append(diagnostics, Diagnostic{Code: CodeInvalidSymlink, Path: relative, Err: resolveErr})
				return nil
			}
			files = append(files, File{Path: relative})
		default:
			diagnostics = append(diagnostics, Diagnostic{Code: CodeUnsupportedFile, Path: relative})
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

func validateReadableRegular(root *os.Root, relative string) error {
	file, err := root.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK, 0)
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

func resolveSupportingLink(root *os.Root, linkPath string) error {
	target, err := root.Readlink(linkPath)
	if err != nil {
		return err
	}
	if path.IsAbs(target) {
		return fmt.Errorf("absolute target")
	}
	return resolveRegular(root, path.Join(path.Dir(linkPath), target), make(map[string]struct{}))
}

func resolveRegular(root *os.Root, candidate string, seen map[string]struct{}) error {
	candidate = path.Clean(candidate)
	if candidate == ".." || strings.HasPrefix(candidate, "../") || path.IsAbs(candidate) {
		return fmt.Errorf("target escapes skill root")
	}
	parts := strings.Split(candidate, "/")
	cursor := ""
	for index, part := range parts {
		cursor = path.Join(cursor, part)
		info, statErr := root.Lstat(cursor)
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if _, duplicate := seen[cursor]; duplicate {
				return fmt.Errorf("cyclic target")
			}
			seen[cursor] = struct{}{}
			target, readErr := root.Readlink(cursor)
			if readErr != nil {
				return readErr
			}
			if path.IsAbs(target) {
				return fmt.Errorf("absolute target")
			}
			remainder := path.Join(parts[index+1:]...)
			return resolveRegular(root, path.Join(path.Dir(cursor), target, remainder), seen)
		}
		if index != len(parts)-1 {
			if !info.IsDir() {
				return fmt.Errorf("non-directory path component")
			}
			continue
		}
		if info.IsDir() {
			return fmt.Errorf("target is a directory")
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("target is not a regular file")
		}
		return validateReadableRegular(root, cursor)
	}
	return errors.New("empty target")
}

func closeFile(file *os.File, primary error) error {
	return errors.Join(primary, file.Close())
}

func closeRoot(root *os.Root, primary error) error {
	return errors.Join(primary, root.Close())
}
