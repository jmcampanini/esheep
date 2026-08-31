//go:build linux

package agentsfile

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameExchangeAt(directory *os.File, oldName, newName string) error {
	return unix.Renameat2(int(directory.Fd()), oldName, int(directory.Fd()), newName, unix.RENAME_EXCHANGE)
}

func renameNoReplaceAt(directory *os.File, oldName, newName string) error {
	return unix.Renameat2(int(directory.Fd()), oldName, int(directory.Fd()), newName, unix.RENAME_NOREPLACE)
}
