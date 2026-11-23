package main

// STM-01, Founder instruction (interim, relayed, not yet committed; A503, A475): a grant lifetime above the record lifetime is refused, because a Decision Grant must not outlive the
// Decision Record that carries it (REQ-TRU-008, INV-03). Equal values and a smaller grant are accepted. The refusal names both settings and never echoes a value (AGENT-13). No
// default in code (AGENT-16, A330). The numbers here are SYNTHETIC test numbers, not the product's lifetimes.

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func orderEnv(grantMS, recordMS int64) Env {
	return recorderSettingsEnv(map[string]string{
		EnvTrustGrantLifetimeMS:  strconv.FormatInt(grantMS, 10),
		EnvTrustRecordLifetimeMS: strconv.FormatInt(recordMS, 10),
	})
}

func assertNamesBothNoValue(t *testing.T, msg string, values ...int64) {
	t.Helper()
	for _, n := range []string{EnvTrustGrantLifetimeMS, EnvTrustRecordLifetimeMS} {
		if !strings.Contains(msg, n) {
			t.Errorf("the error does not name %s: %q", n, msg)
		}
	}
	if strings.Contains(msg, EnvTrustTxTimeoutMS) {
		t.Errorf("the error names the transaction timeout: %q", msg)
	}
	for _, v := range values {
		if s := strconv.FormatInt(v, 10); strings.Contains(msg, s) {
			t.Errorf("the error echoes the value %s: %q", s, msg)
		}
	}
}

func TestRecorderSettingsRefuseGrantAboveRecord(t *testing.T) {
	got, err := loadRecorderSettings(orderEnv(987654, 123456))
	if err == nil {
		t.Fatalf("a grant lifetime above the record lifetime was accepted: %+v", got)
	}
	if got != (RecorderSettings{}) {
		t.Fatalf("an error came with values: %+v", got)
	}
	assertNamesBothNoValue(t, err.Error(), 987654, 123456)
}

func TestRecorderSettingsAcceptGrantEqualToRecord(t *testing.T) {
	got, err := loadRecorderSettings(orderEnv(4321, 4321))
	if err != nil {
		t.Fatalf("equal lifetimes were refused: %v", err)
	}
	if got.GrantLifetime != 4321*time.Millisecond || got.RecordLifetime != 4321*time.Millisecond {
		t.Fatalf("got %+v", got)
	}
}

func TestRecorderSettingsAcceptGrantBelowRecord(t *testing.T) {
	got, err := loadRecorderSettings(orderEnv(4320, 4321))
	if err != nil {
		t.Fatalf("a smaller grant lifetime was refused: %v", err)
	}
	if got.GrantLifetime != 4320*time.Millisecond || got.RecordLifetime != 4321*time.Millisecond {
		t.Fatalf("got %+v", got)
	}
}

// One more than the record is refused; the bound is exact, at both ends of the range.
func TestRecorderSettingsGrantOrderBoundary(t *testing.T) {
	for _, rec := range []int64{1, 2, 1000, maxReadTimeoutMS - 1} {
		if _, err := loadRecorderSettings(orderEnv(rec, rec)); err != nil {
			t.Errorf("record=grant=%d refused: %v", rec, err)
		}
		if _, err := loadRecorderSettings(orderEnv(rec+1, rec)); err == nil {
			t.Errorf("grant=%d above record=%d accepted", rec+1, rec)
		}
	}
	if _, err := loadRecorderSettings(orderEnv(maxReadTimeoutMS, maxReadTimeoutMS)); err != nil {
		t.Errorf("both at the largest accepted value refused: %v", err)
	}
}

// A table of many SYNTHETIC pairs: accepted exactly when grant <= record, and a refusal always names both settings and never a value.
func TestRecorderSettingsGrantOrderTable(t *testing.T) {
	vals := []int64{1, 2, 3, 7, 10, 99, 100, 101, 999, 1000, 1001, 12345, 54321, 86399999, 86400000}
	for _, g := range vals {
		for _, r := range vals {
			if g > maxReadTimeoutMS || r > maxReadTimeoutMS {
				continue
			}
			got, err := loadRecorderSettings(orderEnv(g, r))
			if g <= r {
				if err != nil {
					t.Errorf("grant=%d record=%d refused: %v", g, r, err)
				}
				continue
			}
			if err == nil {
				t.Errorf("grant=%d record=%d accepted: %+v", g, r, got)
				continue
			}
			if got != (RecorderSettings{}) {
				t.Errorf("grant=%d record=%d: an error came with values: %+v", g, r, got)
			}
			msg := err.Error()
			for _, n := range []string{EnvTrustGrantLifetimeMS, EnvTrustRecordLifetimeMS} {
				if !strings.Contains(msg, n) {
					t.Errorf("grant=%d record=%d: the error does not name %s: %q", g, r, n, msg)
				}
			}
			// strip the setting names, then no digit of a value may remain
			rest := strings.NewReplacer(EnvTrustGrantLifetimeMS, "", EnvTrustRecordLifetimeMS, "").Replace(msg)
			if strings.ContainsAny(rest, "0123456789") {
				t.Errorf("grant=%d record=%d: the error carries a number: %q", g, r, msg)
			}
		}
	}
}

// An invalid value is still refused first and names only its own setting; the order check never masks it.
func TestRecorderSettingsInvalidValueBeatsOrderCheck(t *testing.T) {
	_, err := loadRecorderSettings(recorderSettingsEnv(map[string]string{EnvTrustGrantLifetimeMS: "9000", EnvTrustRecordLifetimeMS: "100", EnvTrustTxTimeoutMS: "x"}))
	if err == nil {
		t.Fatal("accepted")
	}
	assertNamesOnly(t, err.Error(), EnvTrustTxTimeoutMS, "")
}
