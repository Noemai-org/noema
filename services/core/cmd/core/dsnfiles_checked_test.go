package main

// SEC-09 (REQ-SEC-009, INV-29, AGENT-13; Founder instruction A509 and issue #810, relayed, not yet committed): every secret file cmd/core reads goes through the shared
// checked reader (keyfiles.ReadFile): a regular file, not a link, not a FIFO, owned by the running user, mode 0400, bounded in size. The per-tenant DSN files and the private keys
// of the model-service client and of the Temporal client are secrets; a certificate and a CA file are public and are not. Every DSN, login and key here is SYNTHETIC; none is reached.
// An error names the setting (and the tenant) and never the content.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// dsnDirSettings are the four settings that name a directory of per-tenant DSN files; each is read by loginPools.
var dsnDirSettings = []string{EnvRecordReadDSNDir, EnvOrchestratorDSNDir, EnvTrustDSNDir, EnvEdgeDSNDir}

func lenientEnv(setting, dir string) Env {
	e := envMap{setting: dir}
	return Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
}

func writeDSNFile(t *testing.T, dir, tenant, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, tenant)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func refusedWithoutContent(t *testing.T, label, setting, tenant string, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: the file was accepted; want a refusal", label)
		return
	}
	msg := err.Error()
	if !strings.Contains(msg, setting) {
		t.Errorf("%s: the error must name the setting %s: %s", label, setting, msg)
	}
	if tenant != "" && !strings.Contains(msg, tenant) {
		t.Errorf("%s: the error must name the tenant %s: %s", label, tenant, msg)
	}
	for _, s := range append([]string{synthPassword, "127.0.0.1", "postgres://"}, secrets...) {
		if s != "" && strings.Contains(msg, s) {
			t.Errorf("%s: the error repeats file content or a path: %s", label, msg)
		}
	}
}

// REQ-SEC-009, INV-29: a DSN file that group or world can read, or that the owner can write, is refused, for each of the four DSN settings.
func TestADSNFileThatIsNotOwnerReadOnlyIsRefused(t *testing.T) {
	for _, setting := range dsnDirSettings {
		for _, mode := range []os.FileMode{0o600, 0o640, 0o440, 0o444, 0o644, 0o500} {
			dir := t.TempDir()
			writeDSNFile(t, dir, "ten_aa", synthDSN("login_a"), mode)
			pools, closer, err := loginPools(t.Context(), lenientEnv(setting, dir), setting)
			if closer != nil {
				closer()
			}
			refusedWithoutContent(t, setting+" "+mode.String(), setting, "ten_aa", err)
			if pools != nil {
				t.Errorf("%s %s: pools were built from a file that must have been refused", setting, mode)
			}
		}
	}
}

// REQ-SEC-009, INV-29: the size bound. A DSN is a few hundred bytes; a file past the bound is refused without being parsed.
func TestADSNFileOverTheSizeBoundIsRefused(t *testing.T) {
	for _, setting := range dsnDirSettings {
		dir := t.TempDir()
		big := synthDSN("login_a") + "&application_name=" + strings.Repeat("a", 16*1024)
		writeDSNFile(t, dir, "ten_aa", big, 0o400)
		pools, closer, err := loginPools(t.Context(), lenientEnv(setting, dir), setting)
		if closer != nil {
			closer()
		}
		refusedWithoutContent(t, setting+" over the bound", setting, "ten_aa", err)
		if pools != nil {
			t.Errorf("%s: pools were built from an oversized file", setting)
		}
	}
}

// REQ-SEC-009: a file owned by another user is refused. Changing an owner needs root; elsewhere the case cannot be made, and is skipped, not faked.
func TestADSNFileOwnedByAnotherUserIsRefused(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing a file's owner needs root; the owner check is shared and is covered by keyfiles' own tests")
	}
	for _, setting := range dsnDirSettings {
		dir := t.TempDir()
		p := writeDSNFile(t, dir, "ten_aa", synthDSN("login_a"), 0o400)
		if err := os.Chown(p, 65534, 65534); err != nil {
			t.Skipf("cannot change the owner here: %v", err)
		}
		pools, closer, err := loginPools(t.Context(), lenientEnv(setting, dir), setting)
		if closer != nil {
			closer()
		}
		refusedWithoutContent(t, setting+" foreign owner", setting, "ten_aa", err)
		if pools != nil {
			t.Errorf("%s: pools were built from a file owned by another user", setting)
		}
	}
}

// REQ-SEC-009: a symbolic link and a FIFO standing in for a tenant's file are refused (and a FIFO is never opened, so nothing blocks).
func TestADSNLinkOrFIFOIsRefused(t *testing.T) {
	for _, setting := range dsnDirSettings {
		dir := t.TempDir()
		target := writeDSNFile(t, t.TempDir(), "elsewhere", synthDSN("login_a"), 0o400)
		if err := os.Symlink(target, filepath.Join(dir, "ten_aa")); err != nil {
			t.Skipf("symlinks are not available here: %v", err)
		}
		_, closer, err := loginPools(t.Context(), lenientEnv(setting, dir), setting)
		if closer != nil {
			closer()
		}
		refusedWithoutContent(t, setting+" symlink", setting, "", err)

		fdir := t.TempDir()
		if err := syscall.Mkfifo(filepath.Join(fdir, "ten_aa"), 0o400); err != nil {
			t.Skipf("FIFOs are not available here: %v", err)
		}
		_, closer, err = loginPools(t.Context(), lenientEnv(setting, fdir), setting)
		if closer != nil {
			closer()
		}
		refusedWithoutContent(t, setting+" FIFO", setting, "", err)
	}
}

// REQ-SEC-009, AGENT-13: the DSN read does not use the injected Env.ReadFile, which has no mode, owner or link check: a reader that would hand back a good DSN for any path
// must not make a group-readable file acceptable.
func TestTheDSNReadDoesNotGoThroughTheUncheckedEnvReader(t *testing.T) {
	dir := t.TempDir()
	writeDSNFile(t, dir, "ten_aa", synthDSN("login_a"), 0o644)
	e := envMap{EnvRecordReadDSNDir: dir}
	env := Env{Getenv: e.get, ReadDir: os.ReadDir, ReadFile: func(string) ([]byte, error) { return []byte(synthDSN("login_a")), nil }}
	reader, closer, err := buildRecordReader(t.Context(), env)
	if closer != nil {
		closer()
	}
	if err == nil || reader != nil {
		t.Fatalf("got a reader %v and error %v; want an error and no reader", reader, err)
	}
}

// REQ-SEC-009, INV-29: the model-service client's private key is a secret and is checked as one; the certificate and the CA are public and are not.
func TestTheModelClientPrivateKeyMustBeOwnerReadOnlyAndBounded(t *testing.T) {
	ca := newTestCA(t, "noema-poc-ca")
	cc, ck := ca.issue(t, "core-understanding", false)
	for name, prep := range map[string]func(f mtlsFiles){
		"mode 0600": func(f mtlsFiles) { _ = os.Chmod(f.key, 0o600) },
		"mode 0644": func(f mtlsFiles) { _ = os.Chmod(f.key, 0o644) },
		"over bound": func(f mtlsFiles) {
			_ = os.Chmod(f.key, 0o600)
			_ = os.WriteFile(f.key, append(ck, make([]byte, 64*1024)...), 0o600)
			_ = os.Chmod(f.key, 0o400)
		},
		"a symlink": func(f mtlsFiles) { _ = os.Rename(f.key, f.key+".real"); _ = os.Symlink(f.key+".real", f.key) },
	} {
		f := writeMTLS(t, cc, ck, ca.pem)
		prep(f)
		client, err := buildModelHTTPClient(modelEnv("https://127.0.0.1:1", f, "65536"))
		if client != nil {
			t.Errorf("%s: a client was built with a key file that must be refused", name)
		}
		refusedWithoutContent(t, name, EnvModelClientKeyFile, "", err, string(ck[:40]))
	}
	f := writeMTLS(t, cc, ck, ca.pem)
	_ = os.Chmod(f.cert, 0o644)
	_ = os.Chmod(f.ca, 0o644)
	if client, err := buildModelHTTPClient(modelEnv("https://127.0.0.1:1", f, "65536")); err != nil || client == nil {
		t.Errorf("a public certificate and CA with any mode and a 0400 key must be accepted: %v", err)
	}
}

// REQ-SEC-009, INV-29: the Temporal client's private key is a secret and is checked as one.
func TestTheTemporalClientPrivateKeyMustBeOwnerReadOnly(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o640, 0o644} {
		dir := t.TempDir()
		cert, key := writePair(t, dir)
		if err := os.Chmod(key, mode); err != nil {
			t.Fatal(err)
		}
		tc, err := temporalTLS(&WorkerConfig{Address: "temporal.internal:7233", TLS: WorkerTLS{CertFile: cert, KeyFile: key, CAFile: cert}})
		if tc != nil {
			t.Errorf("%s: a TLS configuration was built with a key file that must be refused", mode)
		}
		refusedWithoutContent(t, mode.String(), EnvTemporalTLSKey, "", err, dir)
	}
}
