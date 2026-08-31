package agentsfile

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const stagingPrefix = ".esheep-agents-md-"

type destinationRoot struct {
	directory *os.File
	handle    *os.Root
	info      os.FileInfo
	name      string
	path      string
}

type deployedFile struct {
	content []byte
	info    os.FileInfo
	present bool
}

// Inspect compares the selected content with the deployed copy without
// modifying it.
func Inspect(ctx context.Context, content []byte, destination string) (state State, resultErr error) {
	if err := validateDestination("inspect", destination); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	root, exists, err := openDestinationRoot(destination, false)
	if err != nil {
		return "", fmt.Errorf("inspect agents file: %w", err)
	}
	if !exists {
		return StateMissing, nil
	}
	defer func() { resultErr = errors.Join(resultErr, root.close()) }()

	deployed, err := root.read()
	if err != nil {
		return "", fmt.Errorf("inspect agents file: %w", err)
	}
	if !deployed.present {
		return StateMissing, nil
	}
	if !bytes.Equal(deployed.content, content) {
		return StateStale, nil
	}
	return StateSynced, nil
}

// Deploy makes the destination byte-identical to content, creating parent
// directories as needed and atomically replacing an existing regular file.
// Ownership of the destination is positional, and nothing is ever deleted
// when no source agents file is selected.
func Deploy(ctx context.Context, content []byte, destination string) (outcome Outcome, resultErr error) {
	if err := validateDestination("deploy", destination); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	root, _, err := openDestinationRoot(destination, true)
	if err != nil {
		return "", fmt.Errorf("deploy agents file: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, root.close()) }()

	deployed, err := root.read()
	if err != nil {
		return "", fmt.Errorf("deploy agents file: %w", err)
	}
	if deployed.present && bytes.Equal(deployed.content, content) {
		return OutcomeUnchanged, nil
	}

	stagingName, stagingInfo, err := root.stage(content)
	if err != nil {
		return "", fmt.Errorf("stage agents file: %w", err)
	}
	cleanup := func(expected os.FileInfo) error {
		info, err := root.handle.Lstat(stagingName)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil || !os.SameFile(expected, info) {
			return errors.Join(fmt.Errorf("clean agents file staging: staging was replaced"), err)
		}
		if err := root.handle.Remove(stagingName); err != nil {
			return fmt.Errorf("clean agents file staging: %w", err)
		}
		return root.verifyPath()
	}

	if !deployed.present {
		if err := root.install(stagingName, stagingInfo); err != nil {
			return "", errors.Join(fmt.Errorf("commit agents file: %w", err), cleanup(stagingInfo))
		}
		return OutcomeInstalled, nil
	}
	cleanupInfo, err := root.replace(stagingName, stagingInfo, deployed.info)
	if err != nil {
		if cleanupInfo != nil {
			return "", errors.Join(fmt.Errorf("commit agents file: %w", err), cleanup(cleanupInfo))
		}
		return "", fmt.Errorf("commit agents file: %w", err)
	}
	return OutcomeRepaired, cleanup(cleanupInfo)
}

func validateDestination(operation, destination string) error {
	if destination == "" || !filepath.IsAbs(destination) || filepath.Base(filepath.Clean(destination)) == string(filepath.Separator) {
		return fmt.Errorf("%s agents file: invalid destination", operation)
	}
	return nil
}

func openDestinationRoot(destination string, create bool) (*destinationRoot, bool, error) {
	clean := filepath.Clean(destination)
	parentPath := filepath.Dir(clean)
	handle, info, exists, err := openVerifiedDirectory(parentPath, create)
	if err != nil || !exists {
		return nil, exists, err
	}
	directory, err := os.Open(parentPath)
	if err != nil {
		return nil, false, errors.Join(fmt.Errorf("open agents file parent: %w", err), handle.Close())
	}
	directoryInfo, err := directory.Stat()
	if err != nil || !directoryInfo.IsDir() || !os.SameFile(info, directoryInfo) {
		return nil, false, errors.Join(fmt.Errorf("verify agents file parent: parent was replaced"), err, directory.Close(), handle.Close())
	}
	root := &destinationRoot{directory: directory, handle: handle, info: info, name: filepath.Base(clean), path: parentPath}
	if err := root.verifyPath(); err != nil {
		return nil, false, errors.Join(err, root.close())
	}
	return root, true, nil
}

func openVerifiedDirectory(path string, create bool) (*os.Root, os.FileInfo, bool, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return nil, nil, false, fmt.Errorf("open agents file parent: path must be absolute")
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
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		info, err := current.Lstat(component)
		if errors.Is(err, os.ErrNotExist) && !create {
			return nil, nil, false, current.Close()
		}
		if errors.Is(err, os.ErrNotExist) {
			if err := current.Mkdir(component, 0o755); err != nil {
				return nil, nil, false, errors.Join(fmt.Errorf("create agents file parent component %q: %w", component, err), current.Close())
			}
			info, err = current.Lstat(component)
		}
		if err != nil {
			return nil, nil, false, errors.Join(fmt.Errorf("inspect agents file parent component %q: %w", component, err), current.Close())
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, false, errors.Join(fmt.Errorf("inspect agents file parent component %q: not a regular directory", component), current.Close())
		}
		child, err := current.OpenRoot(component)
		if err != nil {
			return nil, nil, false, errors.Join(fmt.Errorf("open agents file parent component %q: %w", component, err), current.Close())
		}
		childInfo, err := child.Stat(".")
		if err != nil || !os.SameFile(info, childInfo) {
			return nil, nil, false, errors.Join(fmt.Errorf("verify agents file parent component %q: component was replaced", component), err, child.Close(), current.Close())
		}
		if err := current.Close(); err != nil {
			return nil, nil, false, errors.Join(fmt.Errorf("close agents file parent component: %w", err), child.Close())
		}
		current = child
	}
	info, err := current.Stat(".")
	if err != nil {
		return nil, nil, false, errors.Join(fmt.Errorf("inspect agents file parent: %w", err), current.Close())
	}
	return current, info, true, nil
}

func (root *destinationRoot) close() error {
	return errors.Join(root.handle.Close(), root.directory.Close())
}

func (root *destinationRoot) verifyPath() error {
	handle, info, exists, err := openVerifiedDirectory(root.path, false)
	if err != nil {
		return fmt.Errorf("verify agents file parent: %w", err)
	}
	if !exists {
		return fmt.Errorf("verify agents file parent: parent is missing")
	}
	closeErr := handle.Close()
	if !os.SameFile(root.info, info) {
		return errors.Join(fmt.Errorf("verify agents file parent: parent was replaced"), closeErr)
	}
	return closeErr
}

func (root *destinationRoot) read() (deployedFile, error) {
	info, err := root.handle.Lstat(root.name)
	if errors.Is(err, os.ErrNotExist) {
		return deployedFile{}, root.verifyPath()
	}
	if err != nil {
		return deployedFile{}, fmt.Errorf("inspect destination: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return deployedFile{}, fmt.Errorf("inspect destination: not a regular non-symlink file")
	}
	file, err := root.handle.OpenFile(root.name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return deployedFile{}, fmt.Errorf("open destination: %w", err)
	}
	fileInfo, statErr := file.Stat()
	if statErr != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(info, fileInfo) {
		return deployedFile{}, errors.Join(fmt.Errorf("verify destination: destination was replaced"), statErr, file.Close())
	}
	content, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return deployedFile{}, fmt.Errorf("read destination: %w", err)
	}
	if err := root.verifyEntry(root.name, info); err != nil {
		return deployedFile{}, err
	}
	if err := root.verifyPath(); err != nil {
		return deployedFile{}, err
	}
	return deployedFile{content: content, info: info, present: true}, nil
}

func (root *destinationRoot) stage(content []byte) (string, os.FileInfo, error) {
	if err := root.verifyPath(); err != nil {
		return "", nil, err
	}
	for range 100 {
		suffix := make([]byte, 8)
		if _, err := rand.Read(suffix); err != nil {
			return "", nil, fmt.Errorf("create staging name: %w", err)
		}
		name := stagingPrefix + hex.EncodeToString(suffix)
		file, err := root.handle.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("create staging file: %w", err)
		}
		_, writeErr := file.Write(content)
		chmodErr := file.Chmod(0o644)
		info, statErr := file.Stat()
		closeErr := file.Close()
		if err := errors.Join(writeErr, chmodErr, statErr, closeErr); err != nil {
			return "", nil, errors.Join(fmt.Errorf("write staging file: %w", err), root.handle.Remove(name))
		}
		entryInfo, err := root.handle.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, entryInfo) {
			return "", nil, errors.Join(fmt.Errorf("verify staging file: staging was replaced"), err, root.handle.Remove(name))
		}
		if err := root.verifyPath(); err != nil {
			return "", nil, errors.Join(err, root.handle.Remove(name))
		}
		return name, info, nil
	}
	return "", nil, fmt.Errorf("create staging file: unique name unavailable")
}

func (root *destinationRoot) install(stagingName string, stagingInfo os.FileInfo) error {
	if err := root.verifyPath(); err != nil {
		return err
	}
	if err := root.verifyEntry(stagingName, stagingInfo); err != nil {
		return err
	}
	if _, err := root.handle.Lstat(root.name); err == nil {
		return fmt.Errorf("destination appeared during deployment")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect destination: %w", err)
	}
	if err := renameNoReplaceAt(root.directory, stagingName, root.name); err != nil {
		return err
	}
	if err := root.verifyEntry(root.name, stagingInfo); err != nil {
		return err
	}
	return root.verifyPath()
}

func (root *destinationRoot) replace(stagingName string, stagingInfo, deployedInfo os.FileInfo) (os.FileInfo, error) {
	if err := root.verifyPath(); err != nil {
		return stagingInfo, err
	}
	if err := root.verifyEntry(root.name, deployedInfo); err != nil {
		return stagingInfo, err
	}
	if err := root.verifyEntry(stagingName, stagingInfo); err != nil {
		return stagingInfo, err
	}
	if err := renameExchangeAt(root.directory, stagingName, root.name); err != nil {
		return stagingInfo, err
	}
	if err := root.verifyEntry(root.name, stagingInfo); err != nil {
		return root.rollback(stagingName, stagingInfo, err)
	}
	if err := root.verifyEntry(stagingName, deployedInfo); err != nil {
		return root.rollback(stagingName, stagingInfo, err)
	}
	if err := root.verifyPath(); err != nil {
		return root.rollback(stagingName, stagingInfo, err)
	}
	return deployedInfo, nil
}

func (root *destinationRoot) rollback(stagingName string, stagingInfo os.FileInfo, cause error) (os.FileInfo, error) {
	current, err := root.handle.Lstat(root.name)
	if err != nil || !os.SameFile(stagingInfo, current) {
		return nil, errors.Join(cause, fmt.Errorf("restore destination: committed destination was replaced"), err)
	}
	displaced, err := root.handle.Lstat(stagingName)
	if err != nil {
		return nil, errors.Join(cause, fmt.Errorf("restore destination: displaced destination is unavailable"), err)
	}
	if err := renameExchangeAt(root.directory, root.name, stagingName); err != nil {
		return nil, errors.Join(cause, fmt.Errorf("restore destination: %w", err))
	}
	if err := root.verifyEntry(root.name, displaced); err != nil {
		return nil, errors.Join(cause, fmt.Errorf("verify restored destination: %w", err))
	}
	if err := root.verifyEntry(stagingName, stagingInfo); err != nil {
		return nil, errors.Join(cause, fmt.Errorf("verify restored staging: %w", err))
	}
	if err := root.verifyPath(); err != nil {
		return nil, errors.Join(cause, err)
	}
	return stagingInfo, cause
}

func (root *destinationRoot) verifyEntry(name string, expected os.FileInfo) error {
	info, err := root.handle.Lstat(name)
	if err != nil {
		return fmt.Errorf("verify destination entry %q: %w", name, err)
	}
	if !os.SameFile(expected, info) {
		return fmt.Errorf("verify destination entry %q: entry was replaced", name)
	}
	return nil
}
