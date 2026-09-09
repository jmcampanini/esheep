package expansion

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// MaxIncludeDepth limits nested included files; the source document is depth zero.
const MaxIncludeDepth = 2

type includedFile struct {
	info os.FileInfo
	name string
}

func (variables Variables) include(prefix string, kind variableKind, chain []includedFile) ([]byte, error) {
	if variables.Harness == "" || variables.Root == "" {
		return nil, fmt.Errorf("include by harness %q: document root and harness are required", prefix)
	}
	name := "esheep-" + prefix + "-" + variables.Harness + ".md"
	path := filepath.Join(variables.Root, name)
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if kind == variableIncludeByHarnessOptional && errors.Is(err, os.ErrNotExist) && includeAbsent(path) {
			return nil, nil
		}
		return nil, fmt.Errorf("include %q for harness %q: %w", name, variables.Harness, err)
	}
	content, err := readIncludedFile(file, name, chain)
	if err := errors.Join(err, file.Close()); err != nil {
		return nil, fmt.Errorf("include %q for harness %q: %w", name, variables.Harness, err)
	}

	expanded, err := variables.expand(content.body, append(chain, includedFile{info: content.info, name: name}))
	if err != nil {
		return nil, fmt.Errorf("expand include %q: %w", name, err)
	}
	return expanded, nil
}

// Absence of the entry is allowed, but a broken link or unavailable document
// root must not silently erase installed instructions.
func includeAbsent(path string) bool {
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	parent, err := os.Stat(filepath.Dir(path))
	return err == nil && parent.IsDir()
}

type includeContent struct {
	body []byte
	info os.FileInfo
}

func readIncludedFile(file *os.File, name string, chain []includedFile) (includeContent, error) {
	info, err := file.Stat()
	if err != nil {
		return includeContent{}, err
	}
	if !info.Mode().IsRegular() {
		return includeContent{}, fmt.Errorf("not a regular file")
	}
	for _, ancestor := range chain {
		if ancestor.name == name || os.SameFile(ancestor.info, info) {
			return includeContent{}, fmt.Errorf("include cycle: %s", includeChain(chain, name))
		}
	}
	if len(chain) >= MaxIncludeDepth {
		return includeContent{}, fmt.Errorf("include depth exceeds %d: %s", MaxIncludeDepth, includeChain(chain, name))
	}

	body, err := io.ReadAll(file)
	if err != nil {
		return includeContent{}, err
	}
	return includeContent{body: body, info: info}, nil
}

func includeChain(chain []includedFile, name string) string {
	names := make([]string, 0, len(chain)+1)
	for _, ancestor := range chain {
		names = append(names, ancestor.name)
	}
	return strings.Join(append(names, name), " -> ")
}
