//go:build unix

package main

import (
	"io"
	"os"
	"strings"
	"syscall"
)

// readDSN reads the DSN from its file. The path is opened ONCE, without following a link (O_NOFOLLOW) and without waiting on a special file (O_NONBLOCK), and every check is made on that open descriptor:
// a regular file, owned by the effective user, with no group or world permission bit (the file holds the migration login's password), no larger than maxDSNBytes. The read is bounded on the same descriptor.
// Nothing checks the path first and opens it later, so a swap of the path after the open cannot change what is read (Codex P2 on noema#846). No error carries the DSN or any part of it.
func readDSN(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", errDSNFile
	}
	defer func() { _ = f.Close() }() // read-only: a close error changes nothing
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxDSNBytes {
		return "", errDSNFile
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil || st.Uid != uint32(os.Geteuid()) {
		return "", errDSNFile
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxDSNBytes+1))
	if err != nil || len(raw) > maxDSNBytes {
		return "", errDSNFile
	}
	dsn := strings.TrimSpace(string(raw))
	if dsn == "" {
		return "", errDSNFile
	}
	return dsn, nil
}
