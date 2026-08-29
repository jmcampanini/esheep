//go:build linux

package skill

import (
	"os"
	"syscall"
	"unsafe"
)

func openManifestAt(directory *os.File) (*os.File, error) {
	name, err := syscall.BytePtrFromString(manifestName)
	if err != nil {
		return nil, err
	}
	fd, _, errno := syscall.Syscall6(
		syscall.SYS_OPENAT,
		directory.Fd(),
		uintptr(unsafe.Pointer(name)),
		uintptr(os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC),
		0,
		0,
		0,
	)
	if errno != 0 {
		return nil, errno
	}
	return os.NewFile(fd, manifestName), nil
}
