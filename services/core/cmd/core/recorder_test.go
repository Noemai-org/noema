package main

// STM-01 (G1 card, ruling A202's shape for the pools and ruling A204 for the write path; both relayed, not yet committed; REQ-AUD-001, REQ-SEC-004, REQ-STM-001; INV-12,
// AGENT-13, AGENT-16): the composition root builds the orchestrator's Recorder from file-mounted settings, as it builds the Record reader (noema#535). One directory with one file
// per tenant holds that tenant's core-workflows login; one directory holds the signing keys of the identity core-workflows; the number of keys that may be mounted at once is
// a setting with no default. The Recorder is one investigation store per tenant, each over its own pool, signing under core-workflows and confirming an input against the
// Record through the Record reader. A tenant with no file is an error, never another tenant's store. No message carries a DSN or key material. DSNs and keys are SYNTHETIC.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

// aSeed is a SYNTHETIC key seed, made at run time so that no key-shaped literal sits in the source.
var aSeed = strings.Repeat("c3", 32)

type fakeDBs struct{}

func (fakeDBs) For(string) (reconstruct.TxBeginner, error) {
	return nil, errors.New("synthetic: no pool")
}

type orchDirs struct{ dsn, keys string }

func newOrchDirs(t *testing.T, tenants ...string) orchDirs {
	t.Helper()
	d := orchDirs{dsn: t.TempDir(), keys: t.TempDir()}
	for _, tenant := range tenants {
		if err := os.WriteFile(filepath.Join(d.dsn, tenant), []byte(synthDSN("workflows_"+tenant)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeKey(t, d.keys, "core-workflows.ed25519.1.key", aSeed, 0o400)
	return d
}

func writeKey(t *testing.T, dir, name, seed string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(seed+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func orchEnv(d orchDirs, maxActive string) Env {
	e := envMap{EnvOrchestratorDSNDir: d.dsn, EnvOrchestratorKeyDir: d.keys, EnvOrchestratorKeyMaxActive: maxActive}
	return Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
}

func build(t *testing.T, env Env, records reconstruct.TenantDBs) (*orchestratorRecorder, func(), error) {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC))
	return buildRecorder(t.Context(), env, clk, ids.New(clk, nil), records)
}

// With none of the settings there is no Recorder and no error: the slice is not composed and /readyz says so (the state of today).
func TestWithNoOrchestratorSettingsThereIsNoRecorder(t *testing.T) {
	rec, closer, err := build(t, Env{Getenv: envMap{}.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, fakeDBs{})
	if err != nil || rec != nil || closer == nil {
		t.Fatalf("got %v, closer %v, error %v; want no recorder, a callable closer and no error", rec, closer != nil, err)
	}
	closer()
}

// A setting that is given without the others cannot be applied: the process does not start, and the error names the setting that is missing.
func TestAPartialConfigurationRefusesToStartAndNamesTheMissingSetting(t *testing.T) {
	d := newOrchDirs(t, "ten_aa")
	for _, missing := range []string{EnvOrchestratorDSNDir, EnvOrchestratorKeyDir, EnvOrchestratorKeyMaxActive} {
		e := envMap{EnvOrchestratorDSNDir: d.dsn, EnvOrchestratorKeyDir: d.keys, EnvOrchestratorKeyMaxActive: "2"}
		delete(e, missing)
		rec, _, err := build(t, Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, fakeDBs{})
		if err == nil || rec != nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("without %s: got %v, %v; want an error that names it and no recorder", missing, rec, err)
		}
	}
}

// The store confirms an input against the Record: without the Record reader there is nothing to confirm it with, and the Recorder is not built (fail closed).
func TestTheRecorderIsNotBuiltWithoutTheRecordReader(t *testing.T) {
	d := newOrchDirs(t, "ten_aa")
	if rec, _, err := build(t, orchEnv(d, "2"), nil); err == nil || rec != nil {
		t.Fatalf("got %v, %v; want an error and no recorder", rec, err)
	}
}

// One store per tenant file, each with its own pool, the orchestrator's signer and an input check; a tenant with no file has no store and never another's.
func TestEachTenantFileIsOneStoreAndAMissingTenantHasNone(t *testing.T) {
	d := newOrchDirs(t, "ten_aa", "ten_bb")
	rec, closer, err := build(t, orchEnv(d, "2"), fakeDBs{})
	if err != nil || rec == nil {
		t.Fatalf("got %v, %v", rec, err)
	}
	defer closer()
	a, errA := rec.For("ten_aa")
	b, errB := rec.For("ten_bb")
	if errA != nil || errB != nil || a == nil || b == nil || a == b || a.Pool == nil || a.Pool == b.Pool {
		t.Fatalf("two tenants must have two stores over two pools: %v %v %v %v", a, b, errA, errB)
	}
	if a.Appender == nil || a.Appender.Signers["core-workflows"] == nil || len(a.Appender.Signers) != 1 || a.Inputs == nil {
		t.Errorf("the store must sign as core-workflows only and confirm inputs: %+v", a)
	}
	for _, bad := range []string{"ten_cc", "", "TEN_AA", "../ten_aa"} {
		if s, err := rec.For(bad); err == nil || s != nil {
			t.Errorf("For(%q) gave %v, %v; want an error and no store", bad, s, err)
		}
	}
}

// A change for a tenant with no store is refused before any database is asked (INV-12): the Recorder never falls back to another tenant's store.
func TestAChangeForATenantWithNoStoreIsRefused(t *testing.T) {
	d := newOrchDirs(t, "ten_aa")
	rec, closer, err := build(t, orchEnv(d, "2"), fakeDBs{})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = rec.AppendChange(ctx, orchestrator.Session{TenantID: "ten_zz", PrincipalID: "usr_01"}, orchestrator.Change{ID: "inv_AA11", From: "RECEIVED", To: "INTERPRETING", FromVersion: 0, ToVersion: 1})
	if err == nil {
		t.Fatal("want an error")
	}
}

// The keys are the orchestrator's alone and are a mounted secret: no key, a key of another identity, too many keys and a key readable by others are all refused, and no
// message carries key material or a DSN (AGENT-13).
func TestABadKeyDirectoryRefusesToStartAndNeverEchoesASecret(t *testing.T) {
	bad := map[string]func(d orchDirs){
		"no key": func(d orchDirs) { _ = os.Remove(filepath.Join(d.keys, "core-workflows.ed25519.1.key")) },
		"a key of another identity": func(d orchDirs) {
			_ = os.Rename(filepath.Join(d.keys, "core-workflows.ed25519.1.key"), filepath.Join(d.keys, "core-edge.ed25519.1.key"))
		},
		"two keys over the overlap":   func(d orchDirs) { writeKey(t, d.keys, "core-workflows.ed25519.2.key", strings.Repeat("d4", 32), 0o400) },
		"a key readable by the group": func(d orchDirs) { _ = os.Chmod(filepath.Join(d.keys, "core-workflows.ed25519.1.key"), 0o440) },
	}
	for name, mutate := range bad {
		d := newOrchDirs(t, "ten_aa")
		mutate(d)
		rec, _, err := build(t, orchEnv(d, "1"), fakeDBs{})
		if err == nil || rec != nil {
			t.Errorf("%s: got %v, %v; want an error and no recorder", name, rec, err)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, aSeed) || strings.Contains(msg, synthPassword) || strings.Contains(msg, "postgres://") {
			t.Errorf("%s: the error echoes a secret: %s", name, msg)
		}
	}
}

// The number of keys that may be mounted at once is a setting and has no default (AGENT-16): empty, zero, negative and not a number are refused.
func TestTheKeyOverlapIsAPositiveIntegerWithNoDefault(t *testing.T) {
	for _, bad := range []string{"", "0", "-1", "x", "1.5", " 2"} {
		d := newOrchDirs(t, "ten_aa")
		if rec, _, err := build(t, orchEnv(d, bad), fakeDBs{}); err == nil || rec != nil {
			t.Errorf("%q: got %v, %v; want an error and no recorder", bad, rec, err)
		}
	}
}

// The directory of logins follows the same rules as the Record reader's (noema#535): one login in two tenants' files is refused, and the error names the tenants and never the login.
func TestOneOrchestratorLoginInTwoTenantFilesIsRefused(t *testing.T) {
	d := newOrchDirs(t)
	for _, tenant := range []string{"ten_aa", "ten_bb"} {
		if err := os.WriteFile(filepath.Join(d.dsn, tenant), []byte(synthDSN("workflows_shared")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rec, _, err := build(t, orchEnv(d, "2"), fakeDBs{})
	if err == nil || rec != nil || !strings.Contains(err.Error(), EnvOrchestratorDSNDir) || strings.Contains(err.Error(), "workflows_shared") || strings.Contains(err.Error(), synthPassword) {
		t.Fatalf("got %v, %v; want an error that names the setting and no login", rec, err)
	}
}
