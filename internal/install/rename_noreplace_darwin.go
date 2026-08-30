//go:build darwin

package install

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameExchangeAt(directory *os.File, oldName, newName string) error {
	return unix.RenameatxNp(int(directory.Fd()), oldName, int(directory.Fd()), newName, unix.RENAME_SWAP)
}

func renameNoReplaceAt(directory *os.File, oldName, newName string) error {
	return unix.RenameatxNp(int(directory.Fd()), oldName, int(directory.Fd()), newName, unix.RENAME_EXCL)
}
