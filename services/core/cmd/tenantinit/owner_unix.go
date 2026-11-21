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
