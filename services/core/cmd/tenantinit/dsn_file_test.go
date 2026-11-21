package main

// SR note on noema#852 (the DSN file gets the checks of the other one-shots): a DSN file is a regular file (not a link), owned by the caller, and has no group or other permission
// bits. INV-12, AGENT-13, A347. The password is a SYNTHETIC marker.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
