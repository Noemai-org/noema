//go:build unix

package main

// EXE-08 (A484; REQ-DATA-001; INV-29, AGENT-13), Codex P2 on noema#846: the DSN file holds the migration login's password, so it is opened ONCE without following a link and without blocking on a special
// file, and every check (a regular file, owned by the effective user, no group or world permission bit, small) and the bounded read are made on that one open descriptor. A path that is checked and then
// opened leaves a gap in which the file can be swapped. All data is SYNTHETIC.

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

// A DSN file that a group or the world can read, write or run is refused before any connection: the password in it is exposed (Codex P2, AGENT-13). Owner-only files are accepted.
func TestADSNFileWithAGroupOrWorldBitIsRefused(t *testing.T) {
	w := newWorld(t)
	for _, mode := range []os.FileMode{0o440, 0o404, 0o444, 0o640, 0o620, 0o602, 0o660, 0o666, 0o700 | 0o010, 0o604, 0o750} {
		p := w.modeFile("dsn-"+mode.String(), w.dsn, mode)
		code, out, errw := w.run(w.args(p, realMigrations)...)
		if code != 2 {
			t.Errorf("mode %v: exit %d, want 2 (stdout %q)", mode, code, out)
			continue
		}
		if strings.Contains(out+errw, w.dsn) {
			t.Errorf("mode %v: the DSN is in the output", mode)
		}
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a refused file touched the database (schema_migration has %d rows)", got)
	}
	for _, mode := range []os.FileMode{0o400, 0o600} {
		p := w.modeFile("ok-"+mode.String(), w.dsn, mode)
		if code, out, errw := w.run(w.args(p, realMigrations)...); code != 0 {
			t.Errorf("mode %v is owner-only and must be accepted: exit %d (stdout %q, stderr %q)", mode, code, out, errw)
		}
	}
}

// A link is refused whatever it points to, and a dangling one too: the final component is never followed (O_NOFOLLOW).
func TestALinkToADSNFileIsNeverFollowed(t *testing.T) {
	w := newWorld(t)
	good := w.modeFile("target", w.dsn, 0o400)
	link := filepath.Join(w.dir, "to-good")
	dangling := filepath.Join(w.dir, "to-nothing")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(w.dir, "absent"), dangling); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{"a link to a good file": link, "a dangling link": dangling} {
		if code, out, errw := w.run(w.args(p, realMigrations)...); code != 2 || strings.Contains(out+errw, w.dsn) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want 2 and no DSN", name, code, out, errw)
		}
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a link touched the database (schema_migration has %d rows)", got)
	}
}

// A named pipe is refused and the command does not wait on it: the open must not block for a writer that never comes (O_NONBLOCK), and the type is read from the open descriptor.
func TestAPipeAsTheDSNFileIsRefusedWithoutBlocking(t *testing.T) {
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

// A DSN file owned by another user is refused: only the user the command runs as may have written the password it will connect with. Provable only where the test can change an owner (root).
func TestADSNFileOwnedByAnotherUserIsRefused(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing a file's owner needs root; the rule is the effective user's own file (checked in the source test below)")
	}
	w := newWorld(t)
	p := w.modeFile("foreign", w.dsn, 0o400)
	if err := os.Chown(p, 65534, 65534); err != nil {
		t.Skipf("cannot change the owner here: %v", err)
	}
	if code, out, errw := w.run(w.args(p, realMigrations)...); code != 2 || strings.Contains(out+errw, w.dsn) {
		t.Errorf("exit %d, stdout %q, stderr %q; want 2 and no DSN", code, out, errw)
	}
	if got := w.applied(); got != -1 {
		t.Errorf("a foreign file touched the database (schema_migration has %d rows)", got)
	}
}

// The DSN path is opened once and everything is read from that descriptor. No source file of the command checks the path first (Lstat, Stat) or reads it by name (ReadFile): that split is the gap
// Codex named. The open refuses a link and does not block, and the owner and the mode are read from the open descriptor.
func TestTheDSNPathIsOpenedOnceAndCheckedOnTheDescriptor(t *testing.T) {
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
	for _, banned := range []string{"os.Lstat", "os.ReadFile"} {
		if strings.Contains(src, banned) {
			t.Errorf("the command's source contains %s", banned)
		}
	}
	for _, want := range []string{"O_NOFOLLOW", "O_NONBLOCK", ".Stat()", "Geteuid", "LimitReader"} {
		if !strings.Contains(src, want) {
			t.Errorf("the command's source lacks %s", want)
		}
	}
}
