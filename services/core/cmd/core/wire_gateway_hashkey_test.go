//go:build unix

package main

// EDG-01 / SEC-09 follow-up (noema#810), REQ-SEC-009, INV-29: the input-hash key file gets the checks the grant keys get (regular file, not a link, no FIFO, owned by the process's
// user, mode 0400), and no refusal repeats a byte of the key. The keys are SYNTHETIC.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const hashSecret = "c0de" // the marker every refusal must not repeat

func writeHash(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hash.key")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTheGatewayRefusesAHashKeyFileThatTheKeyChecksRefuse(t *testing.T) {
	good := strings.Repeat(hashSecret, 10)
	cases := map[string]func(t *testing.T) string{
		"mode 0600": func(t *testing.T) string { return writeHash(t, good, 0o600) },
		"mode 0440": func(t *testing.T) string { return writeHash(t, good, 0o440) },
		"mode 0444": func(t *testing.T) string { return writeHash(t, good, 0o444) },
		"mode 0000": func(t *testing.T) string { return writeHash(t, good, 0) },
		"empty":     func(t *testing.T) string { return writeHash(t, "", 0o400) },
		"short":     func(t *testing.T) string { return writeHash(t, strings.Repeat(hashSecret, 7), 0o400) }, // 28 bytes
		"too large": func(t *testing.T) string { return writeHash(t, strings.Repeat(hashSecret, 4096), 0o400) },
		"a directory": func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "hash.key")
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
			return p
		},
		"a symbolic link": func(t *testing.T) string {
			target := writeHash(t, good, 0o400)
			p := filepath.Join(t.TempDir(), "hash.key")
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			return p
		},
		"a named pipe (must not block)": func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "hash.key")
			if err := syscall.Mkfifo(p, 0o400); err != nil {
				t.Fatal(err)
			}
			return p
		},
	}
	d := newGwDirs(t, tenA)
	for label, build := range cases {
		e := d.env()
		e[EnvInputHashKeyFile] = build(t)
		g, closer, err := buildGW(t, e, recordsOf(t, d.env()), noStart)
		if closer != nil {
			closer()
		}
		if err == nil || g != nil {
			t.Errorf("%s: got %v, %v; want a refusal and no gateway", label, g, err)
			continue
		}
		if !strings.Contains(err.Error(), EnvInputHashKeyFile) || strings.Contains(err.Error(), hashSecret) {
			t.Errorf("%s: %v; want an error that names the setting and holds no key bytes", label, err)
		}
	}
}

func TestTheGatewayRefusesAHashKeyFileOwnedByAnotherUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the owner of a file can be changed only by root; the owner check runs when the tests run as root (CI containers do)")
	}
	d := newGwDirs(t, tenA)
	if err := os.Chown(d.hash, 65534, -1); err != nil {
		t.Skipf("chown is not allowed here: %v", err)
	}
	g, closer, err := buildGW(t, d.env(), recordsOf(t, d.env()), noStart)
	if closer != nil {
		closer()
	}
	if err == nil || g != nil || !strings.Contains(err.Error(), EnvInputHashKeyFile) {
		t.Fatalf("got %v, %v; want a refusal that names the setting", g, err)
	}
}

func TestTheGatewayAcceptsARegularMode0400HashKeyFile(t *testing.T) {
	d := newGwDirs(t, tenA)
	e := d.env()
	e[EnvInputHashKeyFile] = writeHash(t, strings.Repeat("k", 32)+"\n", 0o400)
	g, closer, err := buildGW(t, e, recordsOf(t, d.env()), noStart)
	if closer != nil {
		defer closer()
	}
	if err != nil || g == nil {
		t.Fatalf("got %v, %v; want a gateway", g, err)
	}
}
