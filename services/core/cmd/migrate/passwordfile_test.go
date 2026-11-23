//go:build unix

package main

// EXE-08 (A518, A521, A527, A528; REQ-SEC-001; INV-29, AGENT-13): the password file holds the migration login's password, so it is read through the SHARED key-file reader (trust/grants/keyfiles.ReadFile):
// opened once without following a link and without blocking on a special file, a regular file owned by the effective user, mode exactly 0400 (0600 is refused, A518), small, read from the same descriptor.
// The command has no reader of its own. All data is SYNTHETIC.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// modeFile writes a DSN file with exactly this mode (a umask must not change it).
func (w *world) modeFile(name, body string, mode os.FileMode) string {
	w.t.Helper()
	p := filepath.Join(w.dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		w.t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// A password file that a group or the world can read, write or run, and any mode but exactly 0400 (0600 too, A518), is refused before any connection: the password in it is exposed. Only 0400 is accepted.
func TestAPasswordFileOfAnyModeButOwnerReadOnlyIsRefused(t *testing.T) {
	w := newWorld(t)
	for _, mode := range []os.FileMode{0o440, 0o404, 0o444, 0o640, 0o620, 0o602, 0o660, 0o666, 0o700 | 0o010, 0o604, 0o750, 0o600, 0o200, 0o700, 0o000} {
		p := w.modeFile("password-"+mode.String(), w.pass, mode)
		code, out, errw := w.run(w.args(p, realMigrations)...)
		if code != 2 {
			t.Errorf("mode %v: exit %d, want 2 (stdout %q)", mode, code, out)
			continue
		}
		if strings.Contains(out+errw, w.pass) {
			t.Errorf("mode %v: the password is in the output", mode)
		}
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a refused file touched the database (schema_migration has %d rows)", got)
	}
	p := w.modeFile("ok-0400", w.pass, 0o400)
	if code, out, errw := w.run(w.args(p, realMigrations)...); code != 0 {
		t.Errorf("mode 0400 must be accepted: exit %d (stdout %q, stderr %q)", code, out, errw)
	}
}

// A link is refused whatever it points to, and a dangling one too: the final component is never followed (O_NOFOLLOW).
func TestALinkToAPasswordFileIsNeverFollowed(t *testing.T) {
	w := newWorld(t)
	good := w.modeFile("target", w.pass, 0o400)
	link := filepath.Join(w.dir, "to-good")
	dangling := filepath.Join(w.dir, "to-nothing")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(w.dir, "absent"), dangling); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{"a link to a good file": link, "a dangling link": dangling} {
		if code, out, errw := w.run(w.args(p, realMigrations)...); code != 2 || strings.Contains(out+errw, w.pass) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want 2 and no password", name, code, out, errw)
		}
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a link touched the database (schema_migration has %d rows)", got)
	}
}

// A named pipe is refused and the command does not wait on it: the open must not block for a writer that never comes (O_NONBLOCK), and the type is read from the open descriptor.
func TestAPipeAsThePasswordFileIsRefusedWithoutBlocking(t *testing.T) {
	w := newWorld(t)
	fifo := filepath.Join(w.dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("no named pipe here: %v", err)
	}
	done := make(chan int, 1)
	go func() { code, _, _ := w.run(w.args(fifo, realMigrations)...); done <- code }()
	select {
	case code := <-done:
		if code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command is waiting on a named pipe")
	}
}

// A password file owned by another user is refused: only the user the command runs as may have written the password it will connect with. Provable only where the test can change an owner (root).
func TestAPasswordFileOwnedByAnotherUserIsRefused(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing a file's owner needs root; the owner rule is the shared reader's own (its tests prove it)")
	}
	w := newWorld(t)
	p := w.modeFile("foreign", w.pass, 0o400)
	if err := os.Chown(p, 65534, 65534); err != nil {
		t.Skipf("cannot change the owner here: %v", err)
	}
	if code, out, errw := w.run(w.args(p, realMigrations)...); code != 2 || strings.Contains(out+errw, w.pass) {
		t.Errorf("exit %d, stdout %q, stderr %q; want 2 and no password", code, out, errw)
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a foreign file touched the database (schema_migration has %d rows)", got)
	}
}

// The command has no file reader of its own (A518, A528): the two files that held it are gone, no source file of the command opens a path or checks a mode itself, and the password is read by the
// shared reader. One reader means one place where the rule (regular, owned, 0400, no link, one descriptor) can be wrong.
func TestThePasswordFileIsReadOnlyByTheSharedReader(t *testing.T) {
	for _, gone := range []string{"dsnfile_unix.go", "dsnfile_other.go"} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s still exists: the command's own copy of the file checks must be removed", gone)
		}
	}
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(b)
	}
	src := all.String()
	for _, banned := range []string{"os.Lstat", "os.ReadFile", "os.OpenFile", "O_NOFOLLOW", "syscall.", "Geteuid", "LimitReader"} {
		if strings.Contains(src, banned) {
			t.Errorf("the command's source contains %s: a file check of its own", banned)
		}
	}
	if !strings.Contains(src, "keyfiles.ReadFile(") {
		t.Error("the command's source does not call keyfiles.ReadFile")
	}
}
