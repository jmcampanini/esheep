package install

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const compareBufferSize = 32 * 1024

type treeEntry struct {
	info os.FileInfo
	mode fs.FileMode
}

func treesEqual(left, right string) (bool, error) {
	leftTree, err := readTree(left)
	if err != nil {
		return false, err
	}
	rightTree, err := readTree(right)
	if err != nil {
		return false, err
	}
	if len(leftTree) != len(rightTree) {
		return false, nil
	}
	for path, leftEntry := range leftTree {
		rightEntry, exists := rightTree[path]
		if !exists || leftEntry.mode != rightEntry.mode {
			return false, nil
		}
		if !leftEntry.mode.IsRegular() {
			continue
		}
		equal, err := filesEqual(
			filepath.Join(left, filepath.FromSlash(path)),
			filepath.Join(right, filepath.FromSlash(path)),
			leftEntry.info,
			rightEntry.info,
		)
		if err != nil || !equal {
			return equal, err
		}
	}
	return true, nil
}

func readTree(root string) (map[string]treeEntry, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect tree root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("inspect tree root: not a regular directory")
	}

	entries := make(map[string]treeEntry)
	err = filepath.WalkDir(root, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, infoErr := os.Lstat(path)
		if infoErr != nil {
			return infoErr
		}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return relativeErr
		}
		relative = filepath.ToSlash(relative)
		entries[relative] = treeEntry{info: info, mode: info.Mode().Type() | info.Mode().Perm()}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inspect tree %q: %w", root, err)
	}
	return entries, nil
}

func filesEqual(leftPath, rightPath string, leftExpected, rightExpected os.FileInfo) (equal bool, resultErr error) {
	left, err := os.Open(leftPath)
	if err != nil {
		return false, fmt.Errorf("open comparison file %q: %w", leftPath, err)
	}
	right, err := os.Open(rightPath)
	if err != nil {
		return false, errors.Join(fmt.Errorf("open comparison file %q: %w", rightPath, err), left.Close())
	}
	defer func() {
		resultErr = errors.Join(resultErr, left.Close(), right.Close())
	}()
	leftInfo, err := left.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect comparison file %q: %w", leftPath, err)
	}
	rightInfo, err := right.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect comparison file %q: %w", rightPath, err)
	}
	if !leftInfo.Mode().IsRegular() || !rightInfo.Mode().IsRegular() ||
		!os.SameFile(leftExpected, leftInfo) || !os.SameFile(rightExpected, rightInfo) {
		return false, fmt.Errorf("inspect comparison files: file was replaced")
	}

	leftBuffer := make([]byte, compareBufferSize)
	rightBuffer := make([]byte, compareBufferSize)
	for {
		leftCount, leftErr := io.ReadFull(left, leftBuffer)
		rightCount, rightErr := io.ReadFull(right, rightBuffer)
		leftDone := errors.Is(leftErr, io.EOF) || errors.Is(leftErr, io.ErrUnexpectedEOF)
		rightDone := errors.Is(rightErr, io.EOF) || errors.Is(rightErr, io.ErrUnexpectedEOF)
		if leftErr != nil && !leftDone {
			return false, fmt.Errorf("read comparison file %q: %w", leftPath, leftErr)
		}
		if rightErr != nil && !rightDone {
			return false, fmt.Errorf("read comparison file %q: %w", rightPath, rightErr)
		}
		if leftCount != rightCount || !bytes.Equal(leftBuffer[:leftCount], rightBuffer[:rightCount]) {
			return false, nil
		}
		if leftDone || rightDone {
			return leftDone && rightDone, nil
		}
	}
}
