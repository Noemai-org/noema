package main

// TRU-02 (REQ-SEC-001; INV-12, AGENT-13; ruling A509, interim): the DSN file is read the way a key file is, through ONE open descriptor: opened without following a link or blocking on a special
// file; a regular file owned by the effective user with no group or other bit (the key-file rule: mode 0400); no larger than the limit; read from that same descriptor, so a swap of the path
// after the open cannot change what is read. A refusal is the command's one existing refusal, unchanged: exit 2, the same fixed sentence, nothing written, and no DSN, path or login in any
// output. All data is SYNTHETIC.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// dsnVariant copies the DSN of a pool's login into a new file of the given mode (set with chmod, so the process's umask cannot change it).
func (w *world) dsnVariant(name string, mode os.FileMode) string {
	w.t.Helper()
	raw, err := os.ReadFile(w.login("src-"+name, w.trustA))
	if err != nil {
		w.t.Fatal(err)
	}
	p := filepath.Join(w.files, name)
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// refusedAsAnUnusableDSNFile asserts the command's existing refusal for a DSN file it cannot use: the same exit code, stderr sentence and result as a missing file, nothing written, and nothing of the
// DSN, the path or the login in any output.
func (w *world) refusedAsAnUnusableDSNFile(what, path string) {
	w.t.Helper()
	base := w.run(w.args(tenantA, filepath.Join(w.files, "absent-"+strings.ReplaceAll(what, " ", "-")), packRoot)...)
	o := w.run(w.args(tenantA, path, packRoot)...)
	if o.code != 2 || o.code != base.code || o.err != base.err || o.rep.Result != base.rep.Result || o.rep.Reason != base.rep.Reason {
		w.t.Errorf("%s: exit %d stderr %q result %q reason %q; want what a missing file gives: exit %d stderr %q result %q reason %q", what, o.code, o.err, o.rep.Result, o.rep.Reason, base.code, base.err, base.rep.Result, base.rep.Reason)
	}
	for _, leak := range []string{path, filepath.Base(path), "postgres://", "trust_rw", "sslmode"} {
		if strings.Contains(o.out+o.err, leak) {
			w.t.Errorf("%s: the output holds %q: %s%s", what, leak, o.out, o.err)
		}
	}
	w.nothingWritten()
}

// A509, AGENT-13: a final-component link is refused and never followed, even to a valid DSN file.
func TestADSNFileThatIsALinkIsRefusedAndNeverFollowed(t *testing.T) {
	w := newWorld(t)
	target := w.dsnVariant("target", 0o400)
	link := filepath.Join(w.files, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	w.refusedAsAnUnusableDSNFile("a link", link)
}

// A509: a pipe is refused, and the command does not block on it (the open is non-blocking; the check is on the open descriptor).
func TestADSNFileThatIsAPipeIsRefusedWithoutBlocking(t *testing.T) {
	w := newWorld(t)
	fifo := filepath.Join(w.files, "pipe")
	if err := syscall.Mkfifo(fifo, 0o400); err != nil {
		t.Skipf("no pipe can be made here: %v", err)
	}
	t.Cleanup(func() { // unblock a reader that is stuck, so the test cannot leave a goroutine behind
		if f, err := os.OpenFile(fifo, os.O_RDWR, 0); err == nil {
			_ = f.Close()
		}
	})
	done := make(chan struct{})
	go func() { defer close(done); w.refusedAsAnUnusableDSNFile("a pipe", fifo) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the command blocked on a pipe given as the DSN file")
	}
}

// A509 (the key-file rule): any group or other bit refuses the file, and so does a mode that is not 0400.
func TestADSNFileWithAnyGroupOrOtherBitOrAnyModeButOwnerReadOnlyIsRefused(t *testing.T) {
	w := newWorld(t)
	for _, mode := range []os.FileMode{0o440, 0o404, 0o444, 0o640, 0o604, 0o644, 0o660, 0o606, 0o666, 0o600, 0o700} {
		w.refusedAsAnUnusableDSNFile("mode "+mode.String(), w.dsnVariant("m"+mode.String(), mode))
	}
}

// A509: a file longer than the limit is refused by the bounded read from the descriptor, whatever its content.
func TestADSNFileLongerThanTheLimitIsRefused(t *testing.T) {
	w := newWorld(t)
	raw, err := os.ReadFile(w.dsnVariant("big-src", 0o400))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(w.files, "big")
	if err := os.WriteFile(p, append(raw, []byte(strings.Repeat(" ", maxDSNBytes))...), 0o400); err != nil {
		t.Fatal(err)
	}
	w.refusedAsAnUnusableDSNFile("a long file", p)
}

// A509: a file owned by another user is refused. Only a process that may change an owner can build it; without that the case cannot be made and says so.
func TestADSNFileOwnedByAnotherUserIsRefused(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the process is not root, so it cannot make a file owned by another user; the owner check is proved where root runs the tests")
	}
	w := newWorld(t)
	p := w.dsnVariant("foreign", 0o400)
	if err := os.Chown(p, 65532, 65532); err != nil {
		t.Skipf("the owner cannot be changed here: %v", err)
	}
	w.refusedAsAnUnusableDSNFile("a foreign owner", p)
}

// A509: the file is read through one descriptor, so the source never validates the path first (Lstat or Stat) and never reads it by path (os.ReadFile); the shared key-file reader does both.
func TestTheDSNFileIsNeverCheckedOrReadByPath(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"os.Lstat(", "os.Stat(", "os.ReadFile(", "ioutil.ReadFile("} {
		if strings.Contains(string(src), banned) {
			t.Errorf("main.go calls %s: the DSN file must be read through one open descriptor (A509)", banned)
		}
	}
}
