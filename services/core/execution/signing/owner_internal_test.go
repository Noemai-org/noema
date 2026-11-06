package signing

// D3 (W1): a key file must belong to the process that reads it. The check reads the owner from the open descriptor's Stat
// (the same file the bytes come from). Unit-level, so it runs without root and on any runner.

import (
	"io/fs"
	"os"
	"syscall"
	"testing"
	"time"
)

type fakeInfo struct{ sys any }

func (fakeInfo) Name() string       { return "x" }
func (fakeInfo) Size() int64        { return 0 }
func (fakeInfo) Mode() fs.FileMode  { return keyFileMode }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any         { return f.sys }

func TestAKeyFileOwnedByAnotherUserIsRefused(t *testing.T) {
	me := uint32(os.Geteuid())
	if err := checkOwner(fakeInfo{sys: &syscall.Stat_t{Uid: me}}); err != nil {
		t.Fatalf("a file owned by the process was refused: %v", err)
	}
	for name, sys := range map[string]any{
		"another user": &syscall.Stat_t{Uid: me + 1},
		"root":         &syscall.Stat_t{Uid: 0},
		"no owner":     nil,
		"wrong type":   "not a Stat_t",
	} {
		if name == "root" && me == 0 {
			continue
		}
		if err := checkOwner(fakeInfo{sys: sys}); err == nil {
			t.Errorf("%s: the file was accepted", name)
		}
	}
}
