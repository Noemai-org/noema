package main

// SR note on noema#852 (the DSN file gets the checks of the other one-shots): a DSN file is a regular file (not a link), owned by the caller, and has no group or other permission
// bits. INV-12, AGENT-13, A347. The password is a SYNTHETIC marker.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestADSNFileWithGroupOrOtherBitsIsRefused(t *testing.T) {
	w := newWorld(t)
	for _, mode := range []os.FileMode{0o640, 0o604, 0o644, 0o660, 0o606, 0o666, 0o620, 0o602} {
		p := filepath.Join(w.files, "mode-"+mode.String())
		if err := os.WriteFile(p, []byte(withPassword(t, w.dsn)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		a := w.args("1")
		a[1] = p
		r := w.run(a...)
		if r.code != exitFailure || r.out != "" || r.err != textDSNUnusable+"\n" {
			t.Errorf("mode %v: code %d, stdout %q, stderr %q; want 3, nothing and %q", mode, r.code, r.out, r.err, textDSNUnusable+"\n")
		}
		mustContainNone(t, "output", r.all(), markerPassword, p)
	}
	w.nothingWritten()
}

func TestADSNFileOfAnotherOwnerIsRefusedAndOneOfTheCallerIsUsed(t *testing.T) {
	w := newWorld(t)
	good := w.dsnFile("mine", withPassword(t, w.dsn))
	me := uint32(os.Geteuid())
	if _, err := connectAs(context.Background(), good, me+1); !errors.Is(err, errDSNUnusable) {
		t.Fatalf("a file owned by uid %d read as uid %d: err %v, want errDSNUnusable", me, me+1, err)
	}
	conn, err := connectAs(context.Background(), good, me)
	if err != nil {
		t.Fatalf("a file of the caller with mode 0600 was refused: %v", err)
	}
	_ = conn.Close(context.Background())
}

// Codex on noema#852 (P2): the DSN file was checked by path and then read by path, so the path could be replaced between the two and the bytes read were not those of the file that was
// checked. The file is opened once, without following a link, and its type, size, mode and owner are checked on that descriptor, which is also the one read. The hook runs at the moment
// between the check and the read of the descriptor; the test replaces the path then. INV-12, AGENT-13, A347.
func TestTheDSNIsReadFromTheFileThatWasChecked(t *testing.T) {
	me := uint32(os.Geteuid())
	swaps := map[string]func(t *testing.T, dir, path string){
		"a file of other bytes and mode 0600": func(t *testing.T, dir, path string) {
			other := filepath.Join(dir, "other-0600")
			if err := os.WriteFile(other, []byte("B-DSN-OTHER-0600\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(other, path); err != nil {
				t.Fatal(err)
			}
		},
		"a file readable by the group and others": func(t *testing.T, dir, path string) {
			other := filepath.Join(dir, "other-0644")
			if err := os.WriteFile(other, []byte("B-DSN-OTHER-0644\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(other, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(other, path); err != nil {
				t.Fatal(err)
			}
		},
		"a link to another file": func(t *testing.T, dir, path string) {
			target := filepath.Join(dir, "other-target")
			if err := os.WriteFile(target, []byte("B-DSN-VIA-LINK\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "other-link")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(link, path); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, swap := range swaps {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "dsn")
			if err := os.WriteFile(path, []byte("A-DSN-CHECKED\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			raw, err := readDSNFile(path, me, func() { swap(t, dir, path) })
			if err != nil || string(raw) != "A-DSN-CHECKED\n" {
				t.Fatalf("read %q, %v after the path was replaced by %s, want the bytes of the file that was checked", raw, err, name)
			}
		})
	}
}

// What is not a plain file of the caller is refused on the descriptor and never read or waited on: a link at the path, a FIFO (opening one for reading must not block), a directory, a
// file with a group or other bit, a file of another owner, a file past the size limit, an empty path.
func TestTheDSNFileIsRefusedOnWhatWasOpened(t *testing.T) {
	me := uint32(os.Geteuid())
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	if err := os.WriteFile(good, []byte("A-DSN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, maxDSNBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		path string
		euid uint32
	}{
		"a link to a good file": {link, me},
		"a FIFO":                {fifo, me},
		"a directory":           {dir, me},
		"another owner":         {good, me + 1},
		"past the size limit":   {big, me},
		"a missing file":        {filepath.Join(dir, "missing"), me},
		"an empty path":         {"", me},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			go func() { _, err := readDSNFile(c.path, c.euid, nil); done <- err }()
			select {
			case err := <-done:
				if !errors.Is(err, errDSNUnusable) {
					t.Fatalf("err %v, want errDSNUnusable", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the read waited: a path that is not a plain file must be refused without blocking")
			}
		})
	}
	if raw, err := readDSNFile(good, me, nil); err != nil || string(raw) != "A-DSN\n" {
		t.Fatalf("a good file read %q, %v", raw, err)
	}
}
