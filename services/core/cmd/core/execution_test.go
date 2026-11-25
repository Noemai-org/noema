package main

// LANE-5 slice 1 (A547, A554, interim, `DEC-2026-11-23-architect-rulings-a554-a560`; REQ-SEC-004, REQ-SEC-005, REQ-EXE-001; INV-12, INV-29, AGENT-13, AGENT-16): the composition root reads the
// Execution side's four settings as it reads the orchestrator's and the edge's: all four or none, absence a named gap. One directory holds the per-tenant `execution_rw__<tenant>` logins,
// one the per-tenant `outcome_rw__<tenant>` logins, one the receipt signer's keys (identity core-execution) and the overlap is a positive integer with no default. A login of another
// power or another tenant is refused; no message carries a DSN, a login or key material. Nothing is dialled. DSNs and keys are SYNTHETIC.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type execDirs struct{ exec, outcome, keys string }

// newExecDirs makes the three directories for the tenants: each tenant has its execution login and its outcome login, and the key directory holds one core-execution key.
func newExecDirs(t *testing.T, tenants ...string) execDirs {
	t.Helper()
	d := execDirs{exec: t.TempDir(), outcome: t.TempDir(), keys: t.TempDir()}
	for _, tenant := range tenants {
		if err := os.WriteFile(filepath.Join(d.exec, tenant), []byte(synthDSN("execution_rw__"+tenant)+"\n"), 0o400); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d.outcome, tenant), []byte(synthDSN("outcome_rw__"+tenant)+"\n"), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	writeKey(t, d.keys, "core-execution.ed25519.1.key", aSeed, 0o400)
	return d
}

func execEnv(d execDirs, maxActive string) Env {
	e := envMap{EnvExecutionDSNDir: d.exec, EnvOutcomeDSNDir: d.outcome, EnvExecutionKeyDir: d.keys, EnvExecutionKeyMaxActive: maxActive}
	return Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
}

func TestWithNoExecutionSettingsThereIsNoExecutionSideAndNoError(t *testing.T) {
	parts, closer, err := buildExecution(t.Context(), Env{Getenv: envMap{}.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir})
	if err != nil || parts != nil || closer == nil {
		t.Fatalf("parts %v, closer nil %v, err %v; want no parts, a closer and no error", parts, closer == nil, err)
	}
	closer()
}

func TestAPartialExecutionConfigurationRefusesToStartAndNamesTheMissingSetting(t *testing.T) {
	d := newExecDirs(t, "ten_aa")
	for _, missing := range []string{EnvExecutionDSNDir, EnvOutcomeDSNDir, EnvExecutionKeyDir, EnvExecutionKeyMaxActive} {
		e := envMap{EnvExecutionDSNDir: d.exec, EnvOutcomeDSNDir: d.outcome, EnvExecutionKeyDir: d.keys, EnvExecutionKeyMaxActive: "2"}
		delete(e, missing)
		parts, _, err := buildExecution(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir})
		if err == nil || parts != nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("without %s: parts %v, err %v; want an error that names it", missing, parts, err)
		}
	}
}

func TestEachTenantIsOneExecutionPoolAndOneOutcomePoolAndTheCloserIsSafeTwice(t *testing.T) {
	d := newExecDirs(t, "ten_aa", "ten_bb")
	parts, closer, err := buildExecution(t.Context(), execEnv(d, "2"))
	if err != nil {
		t.Fatal(err)
	}
	if parts == nil || parts.Receipts == nil || len(parts.ExecutionPools) != 2 || len(parts.OutcomePools) != 2 {
		t.Fatalf("parts %+v, want a receipt ring and two pools of each kind", parts)
	}
	for _, tenant := range []string{"ten_aa", "ten_bb"} {
		if parts.ExecutionPools[tenant] == nil || parts.OutcomePools[tenant] == nil {
			t.Errorf("tenant %s lacks a pool", tenant)
		}
	}
	closer()
	closer()
}

func TestALoginOfAnotherPowerOrAnotherTenantIsRefusedAndNeverEchoed(t *testing.T) {
	for name, c := range map[string]struct {
		dir, user, tenant, setting string
	}{
		"a Trust login in the execution directory":       {"exec", "trust_rw__ten_aa", "ten_aa", EnvExecutionDSNDir},
		"an outcome login in the execution directory":    {"exec", "outcome_rw__ten_aa", "ten_aa", EnvExecutionDSNDir},
		"another tenant's execution login":               {"exec", "execution_rw__ten_bb", "ten_aa", EnvExecutionDSNDir},
		"an execution login in the outcome directory":    {"outcome", "execution_rw__ten_aa", "ten_aa", EnvOutcomeDSNDir},
		"a Record reader login in the outcome directory": {"outcome", "record_read__ten_aa", "ten_aa", EnvOutcomeDSNDir},
	} {
		d := newExecDirs(t, "ten_aa", "ten_bb")
		dir := d.exec
		if c.dir == "outcome" {
			dir = d.outcome
		}
		path := filepath.Join(dir, c.tenant)
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(synthDSN(c.user)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatal(err)
		}
		parts, _, err := buildExecution(t.Context(), execEnv(d, "2"))
		if err == nil || parts != nil || !strings.Contains(err.Error(), c.setting) || !strings.Contains(err.Error(), c.tenant) {
			t.Errorf("%s: parts %v, err %v; want a refusal that names the setting and the tenant", name, parts, err)
			continue
		}
		for _, secret := range []string{synthPassword, c.user} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the error carries %q", name, secret)
			}
		}
	}
}

func TestATenantWithOnlyOneOfTheTwoLoginsIsRefused(t *testing.T) {
	d := newExecDirs(t, "ten_aa", "ten_bb")
	if err := os.Remove(filepath.Join(d.outcome, "ten_bb")); err != nil {
		t.Fatal(err)
	}
	parts, _, err := buildExecution(t.Context(), execEnv(d, "2"))
	if err == nil || parts != nil || !strings.Contains(err.Error(), "ten_bb") {
		t.Errorf("parts %v, err %v; want a refusal that names tenant ten_bb", parts, err)
	}
}

func TestABadReceiptKeyDirectoryRefusesToStartAndNeverEchoesASecret(t *testing.T) {
	d := newExecDirs(t, "ten_aa")
	key := filepath.Join(d.keys, "core-execution.ed25519.1.key")
	if err := os.Remove(key); err != nil { // newExecDirs wrote it 0o400; a non-root runner cannot rewrite a read-only file in place
		t.Fatal(err)
	}
	writeKey(t, d.keys, "core-execution.ed25519.1.key", aSeed, 0o644) // group and other bits: refused
	parts, _, err := buildExecution(t.Context(), execEnv(d, "2"))
	if err == nil || parts != nil || !strings.Contains(err.Error(), EnvExecutionKeyDir) || strings.Contains(err.Error(), aSeed) {
		t.Errorf("parts %v, err %v; want a refusal that names %s and holds no key", parts, err, EnvExecutionKeyDir)
	}
}

func TestTheReceiptKeyOverlapIsAPositiveIntegerWithNoDefault(t *testing.T) {
	d := newExecDirs(t, "ten_aa")
	for _, bad := range []string{"0", "-1", "x", "1.5", " 2", "02"} {
		parts, _, err := buildExecution(t.Context(), execEnv(d, bad))
		if err == nil || parts != nil || !strings.Contains(err.Error(), EnvExecutionKeyMaxActive) {
			t.Errorf("overlap %q: parts %v, err %v; want a refusal that names %s", bad, parts, err, EnvExecutionKeyMaxActive)
		}
	}
}
