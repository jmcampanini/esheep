//go:build linux

package install

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplaceAt(directory *os.File, oldName, newName string) error {
	return unix.Renameat2(int(directory.Fd()), oldName, int(directory.Fd()), newName, unix.RENAME_NOREPLACE)
}
