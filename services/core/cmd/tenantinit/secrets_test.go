package main

// TRU-02 (ruling A484): nothing secret reaches the output, and a file or a database that cannot be used is reported in fixed words. AGENT-13 (no secret, no full identifier in a log),
// A347 (fixed public error texts, the cause kept for errors.Is and never printed), REQ-SEC-005 (a private key is never printed). The DSN password and the keys are SYNTHETIC markers.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
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

// holders lists the tables of the public schema in which any row, as text, carries one of the needles (a bytea shows as its hex, so the hex of a seed also finds its raw bytes).
func (w *world) holders(needles ...string) []string {
	w.t.Helper()
	rows, err := w.admin.Query(ctx(w.t), `SELECT format('%I.%I', schemaname, tablename) FROM pg_tables WHERE schemaname = 'public' ORDER BY 1`)
	if err != nil {
		w.t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			w.t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	var found []string
	for _, table := range tables {
		for _, needle := range needles {
			var n int
			if err := w.admin.QueryRow(ctx(w.t), `SELECT count(*) FROM `+table+` r WHERE r::text ILIKE '%' || $1 || '%'`, needle).Scan(&n); err != nil {
				w.t.Fatal(err)
			}
			if n > 0 {
				found = append(found, table)
				break
			}
		}
	}
	return found
}

// SR on noema#852: a private seed planted in the key directory is in no output, error or database. The command reads the seed only to derive the public half (execution/signing.LoadRing), and
// the one value it sends is registeredKey.public. Held three ways: the type that carries a key to the database has no private member; no table holds the seed in hex, base64 or raw bytes
// after a run (a positive control proves the scan finds a planted copy); and no output of any outcome carries it (TestNoSecretReachesTheOutputInAnyOutcome).
func TestAPlantedPrivateSeedIsNeverStoredNorSentToTheDatabase(t *testing.T) { // AGENT-13, REQ-SEC-005, INV-12
	typ := reflect.TypeOf(registeredKey{})
	var members []string
	for i := 0; i < typ.NumField(); i++ {
		members = append(members, typ.Field(i).Name)
	}
	if !reflect.DeepEqual(members, []string{"version", "public"}) {
		t.Fatalf("registeredKey has members %v, want version and public only: nothing private is carried to the database", members)
	}

	w := newWorld(t)
	seed := seedFor("tenantinit key one") // planted in the key directory by newWorld
	needles := []string{hex.EncodeToString(seed), base64.StdEncoding.EncodeToString(seed), base64.RawStdEncoding.EncodeToString(seed)}

	// Positive control: the scan finds a copy that is really there.
	if _, err := w.admin.Exec(ctx(t), `CREATE TABLE public.seed_probe (v text)`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO public.seed_probe VALUES ($1)`, needles[0]); err != nil {
		t.Fatal(err)
	}
	if got := w.holders(needles...); !reflect.DeepEqual(got, []string{"public.seed_probe"}) {
		t.Fatalf("the scan found %v in a database holding a planted seed, want only public.seed_probe", got)
	}
	if _, err := w.admin.Exec(ctx(t), `DROP TABLE public.seed_probe`); err != nil {
		t.Fatal(err)
	}

	if r := w.run(w.args("1")...); r.code != exitDone {
		t.Fatalf("code %d, stderr %q", r.code, r.err)
	}
	if got := w.holders(needles...); len(got) != 0 {
		t.Fatalf("the database holds the private seed in %v after a run", got)
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if row := w.keys()["core-trust.ed25519.1"]; string(row.public) != string(pub) {
		t.Fatalf("the registered key is not the public half of the planted seed")
	}
}
