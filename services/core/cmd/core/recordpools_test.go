package main

// EDG-01 slice 3c, part 3 (ruling A170 and its default for OD-P-82, relayed, not yet committed): the Record reader's per-tenant pools come from a directory of
// file-mounted, read-only DSNs, one file per tenant, named by the tenant ID. Nothing is read from the environment but the directory's name; there is no default
// and no fallback to another tenant's pool (DEBT-04, REQ-AUD-003, REQ-SEC-004, INV-12, AGENT-13, AGENT-16). DSNs are SYNTHETIC; none is ever reached.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const synthPassword = "synthetic-pw-4417-never-logged"

func synthDSN(user string) string {
	return "postgres://" + user + ":" + synthPassword + "@127.0.0.1:1/noema?sslmode=disable"
}

func dsnDir(t *testing.T, files map[string]string) (dir string, env Env) {
	t.Helper()
	dir = t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := envMap{EnvRecordReadDSNDir: dir}
	return dir, Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
}

// With the setting absent there is no Record reader and no error: the route stays unmounted and /readyz says so (the state of today).
func TestWithNoDSNDirectoryThereIsNoRecordReader(t *testing.T) {
	reader, closer, err := buildRecordReader(t.Context(), Env{Getenv: envMap{}.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir})
	if err != nil || reader != nil || closer == nil {
		t.Fatalf("got reader %v, closer %v, error %v; want no reader, a callable closer and no error", reader, closer != nil, err)
	}
	closer() // nothing to close, and it must not panic
}

// Each file is one tenant's pool, and a tenant that has no file is an error: never another tenant's pool, never a default one.
func TestEachFileIsOneTenantsPoolAndAMissingTenantHasNone(t *testing.T) {
	_, env := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa") + "\n", "ten_bb": synthDSN("record_read_bb")})
	reader, closer, err := buildRecordReader(t.Context(), env)
	if err != nil || reader == nil {
		t.Fatalf("got %v, %v", reader, err)
	}
	defer closer()
	a, errA := reader.For("ten_aa")
	b, errB := reader.For("ten_bb")
	if errA != nil || errB != nil || a == nil || b == nil || a == b {
		t.Fatalf("two tenants must have two pools: %v %v %v %v", a, b, errA, errB)
	}
	if again, _ := reader.For("ten_aa"); again != a {
		t.Errorf("a tenant must keep its one pool")
	}
	for _, bad := range []string{"ten_cc", "", "ten_aa ", "TEN_AA", "../ten_aa", "ten_aa/../ten_bb"} {
		if p, err := reader.For(bad); err == nil || p != nil {
			t.Errorf("For(%q) gave %v, %v; want an error and no pool", bad, p, err)
		}
	}
}

// A configuration that cannot be applied safely stops the process, and the error names the setting or the tenant and never a DSN or any part of it (AGENT-13).
func TestABadDSNDirectoryRefusesToStartAndNeverEchoesAnyDSN(t *testing.T) {
	bad := map[string]map[string]string{
		"an empty directory":                 {},
		"a file that is not a tenant ID":     {"ten_aa": synthDSN("r"), "ten_aa.bak": synthDSN("r")},
		"a hidden file":                      {".ten_aa": synthDSN("r")},
		"a tenant ID in the wrong case":      {"TEN_AA": synthDSN("r")},
		"an empty file":                      {"ten_aa": "\n"},
		"a DSN that does not parse":          {"ten_aa": "postgres://r:" + synthPassword + "@[::1:bad"},
		"one good file and one bad":          {"ten_aa": synthDSN("r"), "ten_bb": "postgres://r:" + synthPassword + "@[::1:bad"},
		"a directory where a file should be": nil,
	}
	for name, files := range bad {
		var env Env
		if files == nil {
			dir, e := dsnDir(t, map[string]string{"ten_aa": synthDSN("r")})
			_ = os.Remove(filepath.Join(dir, "ten_aa"))
			if err := os.Mkdir(filepath.Join(dir, "ten_aa"), 0o700); err != nil {
				t.Fatal(err)
			}
			env = e
		} else {
			_, env = dsnDir(t, files)
		}
		reader, closer, err := buildRecordReader(t.Context(), env)
		if err == nil || reader != nil {
			t.Errorf("%s: got a reader %v and error %v; want an error and no reader", name, reader, err)
			continue
		}
		if closer != nil {
			closer()
		}
		if msg := err.Error(); strings.Contains(msg, synthPassword) || strings.Contains(msg, "127.0.0.1") || strings.Contains(msg, "postgres://") {
			t.Errorf("%s: the error echoes a DSN: %s", name, msg)
		}
	}
	missing := Env{Getenv: envMap{EnvRecordReadDSNDir: filepath.Join(t.TempDir(), "absent")}.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
	if reader, _, err := buildRecordReader(t.Context(), missing); err == nil || reader != nil || !strings.Contains(err.Error(), EnvRecordReadDSNDir) {
		t.Errorf("a directory that does not exist: got %v, %v; want an error that names %s", reader, err, EnvRecordReadDSNDir)
	}
}

// The reader is what the reconstruction route needs, and nothing more: one handle per tenant, closed with the process.
func TestClosingTheReaderIsSafeToRepeat(t *testing.T) {
	_, env := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa")})
	_, closer, err := buildRecordReader(t.Context(), env)
	if err != nil {
		t.Fatal(err)
	}
	closer()
	closer()
}
