package main

// TRU-02 (ruling A484): nothing secret reaches the output, and a file or a database that cannot be used is reported in fixed words. AGENT-13 (no secret, no full identifier in a log),
// A347 (fixed public error texts, the cause kept for errors.Is and never printed), REQ-SEC-005 (a private key is never printed). The DSN password and the keys are SYNTHETIC markers.

import (
	"crypto/ed25519"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbidden is everything no stdout or stderr line may carry: the DSN password, the host and port, the paths, and the seeds and public keys in hex and raw.
func (w *world) forbidden(t *testing.T) []string {
	t.Helper()
	u, err := url.Parse(w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{markerPassword, u.Host, u.Port(), w.dsnPath, w.keyDir, w.files}
	for _, label := range []string{"tenantinit key one", "tenantinit key two", "a foreign key"} {
		seed := seedFor(label)
		out = append(out, hex.EncodeToString(seed), string(seed))
	}
	for id, k := range w.keys() {
		out = append(out, id+":", hex.EncodeToString(k.public))
	}
	return out
}

func TestNoSecretReachesTheOutputInAnyOutcome(t *testing.T) { // AGENT-13, A347, REQ-SEC-005
	w := newWorld(t)
	scenarios := map[string]func() result{
		"success":            func() result { return w.run(w.args("2")...) },
		"the same again":     func() result { return w.run(w.args("2")...) },
		"a bad command line": func() result { return w.run(append(w.args("2"), "--tenant", "ten_aa=Name")...) },
	}
	for name, f := range scenarios {
		r := f()
		mustContainNone(t, name+" output", r.all(), w.forbidden(t)...)
	}
	// A different public key under a registered version.
	w2 := newWorld(t)
	w2.registerKey(1, ed25519.NewKeyFromSeed(seedFor("a foreign key")).Public().(ed25519.PublicKey))
	r := w2.run(w2.args("1")...)
	if r.code != exitRefused {
		t.Fatalf("code %d, want 1", r.code)
	}
	mustContainNone(t, "key mismatch output", r.all(), w2.forbidden(t)...)
}

func TestADatabaseThatCannotBeReachedIsReportedInFixedWords(t *testing.T) { // AGENT-13, A347: the driver's text can quote the DSN
	w := newWorld(t)
	u, err := url.Parse(withPassword(t, w.dsn))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/no_such_database_synthetic"
	w.dsnPath = w.dsnFile("missing-db", u.String())
	r := w.run(w.args("1")...)
	if r.code != exitFailure || r.out != "" || !strings.HasPrefix(r.err, "tenantinit: the database cannot be reached") || strings.Count(r.err, "\n") != 1 {
		t.Fatalf("code %d, stdout %q, stderr %q; want 3, nothing and one line starting %q", r.code, r.out, r.err, "tenantinit: the database cannot be reached")
	}
	mustContainNone(t, "output", r.all(), markerPassword, "no_such_database_synthetic", u.Host, "FATAL", "does not exist")
	w.dsnPath = w.dsnFile("operator", withPassword(t, w.dsn)) // the real database holds nothing
	w.nothingWritten()
}

func TestADSNFileThatCannotBeUsedIsReportedInFixedWords(t *testing.T) { // AGENT-13, A347
	w := newWorld(t)
	realDSN := w.dsnFile("real", withPassword(t, w.dsn))
	link := filepath.Join(w.files, "link")
	if err := os.Symlink(realDSN, link); err != nil {
		t.Fatal(err)
	}
	big := w.dsnFile("big", strings.Repeat("x", 9<<10))
	cases := map[string]string{
		"missing":     filepath.Join(w.files, "absent"),
		"a link":      link,
		"a directory": w.files,
		"empty":       w.dsnFile("empty", ""),
		"only spaces": w.dsnFile("blank", "  \n"),
		"too large":   big,
		"unparsable":  w.dsnFile("garbage", "postgres://user:"+markerPassword+"@host:notaport/db"),
		"not a DSN":   w.dsnFile("text", "this is "+markerPassword+" not a DSN"),
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			a := w.args("1")
			a[1] = path
			r := w.run(a...)
			if r.code != exitFailure || r.out != "" || r.err != textDSNUnusable+"\n" {
				t.Fatalf("code %d, stdout %q, stderr %q; want 3, nothing and %q", r.code, r.out, r.err, textDSNUnusable+"\n")
			}
			mustContainNone(t, "output", r.all(), markerPassword, path)
			w.nothingWritten()
		})
	}
}

func TestARunOpensOneConnection(t *testing.T) { // the run holds one connection
	w := newWorld(t)
	if r := w.run(w.args("1")...); r.code != exitDone {
		t.Fatalf("code %d, stderr %q", r.code, r.err)
	}
	if w.dials != 1 {
		t.Fatalf("%d connections opened, want 1", w.dials)
	}
}
