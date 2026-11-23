package main

// TRU-02 (ruling A484): the core-trust public keys the command registers. REQ-SEC-005 (the Record's signing keys; only public keys are registered, by key_register), REQ-AUD-006,
// INV-12, AGENT-13 and A347. Every key is SYNTHETIC (AGENT-25): a seed made of a label, never used outside a test.

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAKeyIsRegisteredOnceAndTwice(t *testing.T) { // REQ-SEC-005
	w := newWorld(t)
	pub := ed25519.PublicKey(w.key(1, "tenantinit key one"))
	a := w.args("1")
	if r := w.run(a...); r.code != exitDone || r.out != outLine(1, 0) {
		t.Fatalf("first run: code %d, stdout %q, stderr %q", r.code, r.out, r.err)
	}
	if r := w.run(a...); r.code != exitDone || r.out != outLine(0, 1) || r.err != "" {
		t.Fatalf("second run: code %d, stdout %q, stderr %q; want 0, %q and nothing", r.code, r.out, r.err, outLine(0, 1))
	}
	keys := w.keys()
	row, ok := keys["core-trust.ed25519.1"]
	if len(keys) != 1 || !ok || row.status != "ACTIVE" || row.version != 1 || !bytes.Equal(row.public, pub) {
		t.Fatalf("signing_key rows %+v, want exactly one ACTIVE core-trust.ed25519.1 holding the public half of the file's key", keys)
	}
}

func TestTheRegisteredPublicKeyVerifiesASignatureOfTheFilesKey(t *testing.T) { // REQ-SEC-005, REQ-AUD-006: the stored key is the public half of the private file
	w := newWorld(t)
	if r := w.run(w.args("1")...); r.code != exitDone {
		t.Fatalf("code %d, stderr %q", r.code, r.err)
	}
	stored := ed25519.PublicKey(w.keys()["core-trust.ed25519.1"].public)
	msg := []byte("SYNTHETIC message signed by the key of the file")
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seedFor("tenantinit key one")), msg)
	if len(stored) != ed25519.PublicKeySize || !ed25519.Verify(stored, msg, sig) {
		t.Fatal("a signature made with the key of the file does not verify against the stored public key")
	}
	other := ed25519.Sign(ed25519.NewKeyFromSeed(seedFor("another key")), msg)
	if ed25519.Verify(stored, msg, other) {
		t.Fatal("a signature of another key verifies against the stored public key")
	}
}

func TestTheSameVersionWithAnotherPublicKeyIsRefusedAndTheTableIsUnchanged(t *testing.T) { // REQ-SEC-005, AGENT-13
	w := newWorld(t)
	foreign := ed25519.NewKeyFromSeed(seedFor("a foreign key")).Public().(ed25519.PublicKey)
	w.registerKey(1, foreign)
	r := w.run(w.args("1")...)
	if r.code != exitRefused || r.out != "" || r.err != textKeyDiffers+"\n" {
		t.Fatalf("code %d, stdout %q, stderr %q; want 1, nothing and %q", r.code, r.out, r.err, textKeyDiffers+"\n")
	}
	keys := w.keys()
	if len(keys) != 1 || !bytes.Equal(keys["core-trust.ed25519.1"].public, foreign) || keys["core-trust.ed25519.1"].status != "ACTIVE" {
		t.Fatalf("signing_key rows %+v, want the foreign key unchanged and alone", keys)
	}
}

func TestAKeyThatIsRegisteredButNoLongerActiveIsRefused(t *testing.T) { // REQ-SEC-005: a retired key is not made usable again by a success
	w := newWorld(t)
	w.registerKey(1, ed25519.NewKeyFromSeed(seedFor("tenantinit key one")).Public().(ed25519.PublicKey))
	if _, err := w.admin.Exec(ctx(t), `SELECT key_retire('core-trust.ed25519.1')`); err != nil {
		t.Fatal(err)
	}
	r := w.run(w.args("1")...)
	if r.code != exitRefused || r.out != "" || r.err != textKeyNotActive+"\n" {
		t.Fatalf("code %d, stdout %q, stderr %q; want 1, nothing and %q", r.code, r.out, r.err, textKeyNotActive+"\n")
	}
}

func TestARotationRegistersBothKeysAndASecondRunRegistersNone(t *testing.T) { // REQ-SEC-005: the rotation overlap, the keys in version order
	w := newWorld(t)
	p1, p2 := ed25519.PublicKey(w.key(1, "tenantinit key one")), ed25519.PublicKey(w.key(2, "tenantinit key two"))
	a := w.args("2")
	if r := w.run(a...); r.code != exitDone || r.out != outLine(2, 0) {
		t.Fatalf("first run: code %d, stdout %q, stderr %q", r.code, r.out, r.err)
	}
	keys := w.keys()
	if len(keys) != 2 || !bytes.Equal(keys["core-trust.ed25519.1"].public, p1) || !bytes.Equal(keys["core-trust.ed25519.2"].public, p2) {
		t.Fatalf("signing_key rows %+v, want both public keys", keys)
	}
	if r := w.run(a...); r.code != exitDone || r.out != outLine(0, 2) {
		t.Fatalf("second run: code %d, stdout %q, stderr %q", r.code, r.out, r.err)
	}
}

func TestOneRegisteredKeyAndOneNewKeyCountSeparately(t *testing.T) { // REQ-SEC-005
	w := newWorld(t)
	w.registerKey(1, ed25519.NewKeyFromSeed(seedFor("tenantinit key one")).Public().(ed25519.PublicKey))
	w.key(2, "tenantinit key two")
	r := w.run(w.args("2")...)
	if r.code != exitDone || r.out != outLine(1, 1) {
		t.Fatalf("code %d, stdout %q, stderr %q; want 0 and %q", r.code, r.out, r.err, outLine(1, 1))
	}
}

func TestAThirdActiveKeyIsRefusedByTheDatabaseAndEverythingIsRolledBack(t *testing.T) { // REQ-SEC-005 (two ACTIVE keys at most, migration 0013); one transaction
	w := newWorld(t)
	w.key(2, "tenantinit key two")
	w.key(3, "tenantinit key three")
	r := w.run(w.args("3")...)
	if r.code != exitRefused || r.out != "" || r.err != textKeyRefused+"\n" {
		t.Fatalf("code %d, stdout %q, stderr %q; want 1, nothing and %q", r.code, r.out, r.err, textKeyRefused+"\n")
	}
	w.nothingWritten()
}

func TestAKeyOlderThanARegisteredOneIsRefused(t *testing.T) { // REQ-SEC-005: a version is newer than every registered one
	w := newWorld(t)
	w.registerKey(2, ed25519.NewKeyFromSeed(seedFor("tenantinit key two")).Public().(ed25519.PublicKey))
	r := w.run(w.args("1")...) // the directory holds version 1 only
	if r.code != exitRefused || r.err != textKeyRefused+"\n" || r.out != "" {
		t.Fatalf("code %d, stdout %q, stderr %q; want 1, nothing and %q", r.code, r.out, r.err, textKeyRefused+"\n")
	}
}

// The loader of execution/signing decides what a key file is. Each of these is refused, none of them is read past, and nothing is written: the files are checked before the database is opened.
func TestAKeyFileTheLoaderRefusesRegistersNothing(t *testing.T) { // REQ-SEC-005, AGENT-13, A347
	faults := map[string]func(t *testing.T, w *world){
		"mode 0440":            func(t *testing.T, w *world) { writeKeyFile(t, w.keyDir, "core-trust.ed25519.2.key", "key two", 0o440) },
		"mode 0600":            func(t *testing.T, w *world) { writeKeyFile(t, w.keyDir, "core-trust.ed25519.2.key", "key two", 0o600) },
		"mode 0444":            func(t *testing.T, w *world) { writeKeyFile(t, w.keyDir, "core-trust.ed25519.2.key", "key two", 0o444) },
		"leading zero version": func(t *testing.T, w *world) { writeKeyFile(t, w.keyDir, "core-trust.ed25519.02.key", "key two", 0o400) },
		"version zero":         func(t *testing.T, w *world) { writeKeyFile(t, w.keyDir, "core-trust.ed25519.0.key", "key two", 0o400) },
		"version not a number": func(t *testing.T, w *world) { writeKeyFile(t, w.keyDir, "core-trust.ed25519.x.key", "key two", 0o400) },
		"no version":           func(t *testing.T, w *world) { writeKeyFile(t, w.keyDir, "core-trust.key", "key two", 0o400) },
		"another identity": func(t *testing.T, w *world) {
			writeKeyFile(t, w.keyDir, "core-execution.ed25519.1.key", "key two", 0o400)
		},
		"not hex": func(t *testing.T, w *world) {
			if err := os.WriteFile(filepath.Join(w.keyDir, "core-trust.ed25519.2.key"), []byte("NOT-A-SEED-SYNTH-XYZ\n"), 0o400); err != nil {
				t.Fatal(err)
			}
		},
		"upper-case hex": func(t *testing.T, w *world) {
			if err := os.WriteFile(filepath.Join(w.keyDir, "core-trust.ed25519.2.key"), []byte("ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789\n"), 0o400); err != nil {
				t.Fatal(err)
			}
		},
		"a symbolic link": func(t *testing.T, w *world) {
			dir := t.TempDir()
			writeKeyFile(t, dir, "real.key", "key two", 0o400)
			if err := os.Symlink(filepath.Join(dir, "real.key"), filepath.Join(w.keyDir, "core-trust.ed25519.2.key")); err != nil {
				t.Fatal(err)
			}
		},
		"a directory": func(t *testing.T, w *world) {
			if err := os.Mkdir(filepath.Join(w.keyDir, "core-trust.ed25519.2.key"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"a named pipe": func(t *testing.T, w *world) {
			if err := syscall.Mkfifo(filepath.Join(w.keyDir, "core-trust.ed25519.2.key"), 0o400); err != nil {
				t.Fatal(err)
			}
		},
		"more keys than the overlap": func(t *testing.T, w *world) { w.key(2, "key two") }, // the overlap is 1 for this one
	}
	for name, fault := range faults {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t) // the directory already holds a valid version 1: it must not be registered either
			fault(t, w)
			overlap := "2" // room for the valid version 1 and the faulty version 2: only the fault may refuse the directory
			if name == "more keys than the overlap" {
				overlap = "1"
			}
			r := w.run(w.args(overlap)...)
			if r.code != exitFailure || r.out != "" || r.err != textKeysUnusable+"\n" {
				t.Fatalf("code %d, stdout %q, stderr %q; want 3, nothing and %q", r.code, r.out, r.err, textKeysUnusable+"\n")
			}
			if w.dials != 0 {
				t.Fatalf("%d connections opened before the keys were checked, want none", w.dials)
			}
			w.nothingWritten()
		})
	}
}

func TestAnEmptyOrMissingKeyDirectoryRegistersNothing(t *testing.T) { // REQ-SEC-005: with no key there is nothing to register, and the command does not report success
	for name, dir := range map[string]func(t *testing.T) string{
		"empty":   func(t *testing.T) string { return t.TempDir() },
		"missing": func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent") },
		"only a file that is not a key": func(t *testing.T) string {
			d := t.TempDir()
			if err := os.WriteFile(filepath.Join(d, "README"), []byte("not a key\n"), 0o400); err != nil {
				t.Fatal(err)
			}
			return d
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.keyDir = dir(t)
			r := w.run(w.args("1")...)
			if r.code != exitFailure || r.out != "" || r.err != textKeysUnusable+"\n" {
				t.Fatalf("code %d, stdout %q, stderr %q; want 3, nothing and %q", r.code, r.out, r.err, textKeysUnusable+"\n")
			}
			w.nothingWritten()
		})
	}
}
