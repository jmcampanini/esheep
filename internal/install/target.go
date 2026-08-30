package install

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type targetRoot struct {
	directory *os.File
	handle    *os.Root
	info      os.FileInfo
	path      string
}

type destination struct {
	info  os.FileInfo
	path  string
	state State
}

func openTargetRoot(path string, create bool) (*targetRoot, bool, error) {
	handle, info, exists, err := openVerifiedDirectory(path, create)
	if err != nil || !exists {
		return nil, exists, err
	}
	directory, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, false, errors.Join(fmt.Errorf("open target root directory: %w", err), handle.Close())
	}
	directoryInfo, err := directory.Stat()
	if err != nil || !directoryInfo.IsDir() || !os.SameFile(info, directoryInfo) {
		return nil, false, errors.Join(fmt.Errorf("verify target root directory: root was replaced"), err, directory.Close(), handle.Close())
	}
	root := &targetRoot{directory: directory, handle: handle, info: info, path: filepath.Clean(path)}
	if err := root.verifyPath(); err != nil {
		return nil, false, errors.Join(err, root.close())
	}
	return root, true, nil
}

func openVerifiedDirectory(path string, create bool) (*os.Root, os.FileInfo, bool, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return nil, nil, false, fmt.Errorf("open target root: path must be absolute")
	}
	current, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, nil, false, fmt.Errorf("open filesystem root: %w", err)
	}
	relative := strings.TrimPrefix(clean, string(filepath.Separator))
	if relative == "" {
		info, err := current.Stat(".")
		if err != nil {
			return nil, nil, false, errors.Join(fmt.Errorf("inspect filesystem root: %w", err), current.Close())
		}
		return current, info, true, nil
	}
	components := strings.Split(relative, string(filepath.Separator))
	for index, component := range components {
		created := false
		info, err := current.Lstat(component)
		if errors.Is(err, os.ErrNotExist) && !create {
			return nil, nil, false, current.Close()
		}
		if errors.Is(err, os.ErrNotExist) {
			if err := current.Mkdir(component, 0o755); err != nil {
				return nil, nil, false, errors.Join(fmt.Errorf("create target root component %q: %w", component, err), current.Close())
			}
			created = true
			info, err = current.Lstat(component)
		}
		if err != nil {
			return nil, nil, false, errors.Join(fmt.Errorf("inspect target root component %q: %w", component, err), current.Close())
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, false, errors.Join(fmt.Errorf("inspect target root component %q: not a regular directory", component), current.Close())
		}
		child, err := current.OpenRoot(component)
		if err != nil {
			return nil, nil, false, errors.Join(fmt.Errorf("open target root component %q: %w", component, err), current.Close())
		}
		childInfo, err := child.Stat(".")
		if err != nil || !os.SameFile(info, childInfo) {
			return nil, nil, false, errors.Join(fmt.Errorf("verify target root component %q: component was replaced", component), err, child.Close(), current.Close())
		}
		if created && index == len(components)-1 {
			if err := child.Chmod(".", 0o755); err != nil {
				return nil, nil, false, errors.Join(fmt.Errorf("set target root permissions: %w", err), child.Close(), current.Close())
			}
		}
		if err := current.Close(); err != nil {
			return nil, nil, false, errors.Join(fmt.Errorf("close target root parent: %w", err), child.Close())
		}
		current = child
	}
	info, err := current.Stat(".")
	if err != nil {
		return nil, nil, false, errors.Join(fmt.Errorf("inspect target root: %w", err), current.Close())
	}
	return current, info, true, nil
}

func (root *targetRoot) close() error {
	return errors.Join(root.handle.Close(), root.directory.Close())
}

func (root *targetRoot) verifyPath() error {
	handle, info, exists, err := openVerifiedDirectory(root.path, false)
	if err != nil {
		return fmt.Errorf("verify target root: %w", err)
	}
	if !exists {
		return fmt.Errorf("verify target root: root is missing")
	}
	closeErr := handle.Close()
	if !os.SameFile(root.info, info) {
		return errors.Join(fmt.Errorf("verify target root: root was replaced"), closeErr)
	}
	return closeErr
}

func (root *targetRoot) renameExchange(oldName, newName string) error {
	return renameExchangeAt(root.directory, oldName, newName)
}

func (root *targetRoot) renameNoReplace(oldName, newName string) error {
	return renameNoReplaceAt(root.directory, oldName, newName)
}

func (root *targetRoot) verifyEntry(name string, expected os.FileInfo) error {
	info, err := root.handle.Lstat(name)
	if err != nil {
		return fmt.Errorf("verify target entry %q: %w", name, err)
	}
	if !os.SameFile(expected, info) {
		return fmt.Errorf("verify target entry %q: entry was replaced", name)
	}
	return nil
}

func (root *targetRoot) readDir() ([]os.DirEntry, error) {
	directory, err := root.handle.Open(".")
	if err != nil {
		return nil, fmt.Errorf("read target root: %w", err)
	}
	entries, readErr := directory.ReadDir(-1)
	if err := errors.Join(readErr, directory.Close()); err != nil {
		return nil, fmt.Errorf("read target root: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	return entries, nil
}

func (root *targetRoot) inspect(identity Identity) (destination, error) {
	entries, err := root.readDir()
	if err != nil {
		return destination{}, err
	}
	var matches []os.DirEntry
	key := pathKey(identity.Skill)
	for _, entry := range entries {
		if pathKey(entry.Name()) == key {
			matches = append(matches, entry)
		}
	}
	result := destination{path: filepath.Join(root.path, identity.Skill), state: StateMissing}
	if len(matches) == 0 {
		return result, nil
	}
	if len(matches) != 1 || matches[0].Name() != identity.Skill || !matches[0].IsDir() || matches[0].Type()&os.ModeSymlink != 0 {
		result.state = StateBlocked
		return result, nil
	}

	result.info, err = root.handle.Lstat(identity.Skill)
	if err != nil {
		return destination{}, fmt.Errorf("inspect destination: %w", err)
	}
	if !result.info.IsDir() || result.info.Mode()&os.ModeSymlink != 0 {
		result.state = StateBlocked
		return result, nil
	}
	owned, err := root.ownedBy(identity)
	if err != nil {
		return destination{}, err
	}
	if !owned {
		result.state = StateBlocked
		return result, nil
	}
	result.state = StateSynced
	return result, nil
}

func (root *targetRoot) ownedBy(identity Identity) (bool, error) {
	return root.ownedAt(identity.Skill, identity)
}

func (root *targetRoot) ownedAt(directory string, identity Identity) (bool, error) {
	markerPath := filepath.Join(directory, MarkerName)
	markerInfo, err := root.handle.Lstat(markerPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect ownership marker: %w", err)
	}
	if !markerInfo.Mode().IsRegular() || markerInfo.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	data, same, err := readMarker(root.handle, markerPath, markerInfo)
	if err != nil {
		return false, fmt.Errorf("read ownership marker: %w", err)
	}
	if !same {
		return false, nil
	}
	marker, err := ParseMarker(data)
	if err != nil {
		return false, nil
	}
	return marker.Source == identity.Source && marker.Skill == identity.Skill && marker.Target == identity.Target, nil
}

func readMarker(root *os.Root, path string, expected os.FileInfo) (data []byte, same bool, resultErr error) {
	const maxMarkerSize = 64 << 10

	file, err := root.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, false, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(expected, info) {
		return nil, false, nil
	}
	data, err = io.ReadAll(io.LimitReader(file, maxMarkerSize+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > maxMarkerSize {
		return nil, false, nil
	}
	return data, true, nil
}

func (root *targetRoot) createTransaction(prefix string) (string, string, error) {
	if err := root.verifyPath(); err != nil {
		return "", "", err
	}
	for range 100 {
		suffix := make([]byte, 8)
		if _, err := rand.Read(suffix); err != nil {
			return "", "", fmt.Errorf("create transaction name: %w", err)
		}
		name := prefix + hex.EncodeToString(suffix)
		if err := root.handle.Mkdir(name, 0o700); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return "", "", fmt.Errorf("create target transaction: %w", err)
		}
		info, err := root.handle.Lstat(name)
		if err != nil {
			return "", "", errors.Join(fmt.Errorf("inspect target transaction: %w", err), root.handle.RemoveAll(name))
		}
		absoluteInfo, err := os.Lstat(filepath.Join(root.path, name))
		if err != nil || !os.SameFile(info, absoluteInfo) {
			return "", "", errors.Join(fmt.Errorf("inspect target transaction: target root was replaced"), root.handle.RemoveAll(name))
		}
		return name, filepath.Join(root.path, name), nil
	}
	return "", "", fmt.Errorf("create target transaction: unique name unavailable")
}

func pathKey(value string) string {
	return norm.NFC.String(cases.Fold().String(norm.NFC.String(value)))
}
