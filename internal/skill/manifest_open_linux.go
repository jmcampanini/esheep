//go:build linux

package skill

import "syscall"

const sysOpenat = syscall.SYS_OPENAT
