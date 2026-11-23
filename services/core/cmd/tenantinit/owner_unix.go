//go:build unix

package main

import (
	"os"
	"syscall"
)

// fileOwner is the uid that owns the file, and whether the platform says so.
func fileOwner(info os.FileInfo) (uint32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

// openDSN opens the DSN path once for reading: a link at the path is refused (O_NOFOLLOW) and a FIFO is opened without waiting (O_NONBLOCK), so that the descriptor's own type decides.
func openDSN(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
}
