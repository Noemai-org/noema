//go:build unix

package keyfiles

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// keyFileMode is the one mode a key file may have: read by its owner, nothing else.
const keyFileMode = os.FileMode(syscall.S_IRUSR)

// readKeyFile opens path without following a link and without blocking on a special file, checks the open descriptor (a regular file, owned by the effective user, mode 0400, small enough) and
// reads it. One descriptor serves every check and the read, so a swap of the path after the open cannot change what is read. The error never holds the file's content.
func readKeyFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("cannot open the file (a symbolic link is refused)")
	}
	defer func() { _ = f.Close() }() // read-only: a close error changes nothing
	info, err := f.Stat()
	if err != nil {
		return nil, errors.New("cannot stat the file")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return nil, errors.New("the owner of the file cannot be read")
	}
	if st.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("the file is owned by another user")
	}
	if info.Mode().Perm() != keyFileMode {
		return nil, errors.New("the mode is not 0400")
	}
	if info.Size() > int64(fileBytes) {
		return nil, errors.New("too large to be a key file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(fileBytes)+1))
	if err != nil {
		return nil, errors.New("cannot read the file")
	}
	return raw, nil
}
