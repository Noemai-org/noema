package session_test

// Ruling A21: the clock skew leeway on exp and iat is 60 s and is pack data, not a constant in code (AGENT-16). REQ-SEC-001, INV-21.

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/session"
)

const sessionLimitsFile = "../../../../packs/fintech/limits/session_limits.json"

func limitsDoc(skew string) string {
	return `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","clock_skew_s":` + skew + `}`
}

func TestThePackFileCarriesTheRulingA21Leeway(t *testing.T) {
	f, err := os.Open(sessionLimitsFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := session.LoadLimits(f)
	if err != nil {
		t.Fatal(err)
	}
	if l.ClockSkew != 60*time.Second {
		t.Fatalf("clock skew = %s, want 60s", l.ClockSkew)
	}
}

func TestTheLeewayComesFromTheDocumentAndNotFromTheCode(t *testing.T) {
	l, err := session.LoadLimits(strings.NewReader(limitsDoc("7")))
	if err != nil || l.ClockSkew != 7*time.Second {
		t.Fatalf("%+v %v", l, err)
	}
	if l, err := session.LoadLimits(strings.NewReader(limitsDoc("0"))); err != nil || l.ClockSkew != 0 {
		t.Fatalf("a zero leeway is allowed (no leeway): %+v %v", l, err)
	}
}

func TestADocumentThatIsNotExactlyTheContractIsRefusedWithSchemaInvalid(t *testing.T) {
	for name, in := range map[string]string{
		"empty":             ``,
		"not json":          `{`,
		"unknown field":     `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","clock_skew_s":60,"x":1}`,
		"missing skew":      `{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC"}`,
		"negative skew":     limitsDoc("-1"),
		"fractional skew":   limitsDoc("1.5"),
		"skew as a string":  limitsDoc(`"60"`),
		"overflowing skew":  limitsDoc("9007199254740991"),
		"bad pack id":       `{"schema_version":"1.0","pack_id":"!","label":"SYNTHETIC","clock_skew_s":60}`,
		"wrong version":     `{"schema_version":"2.0","pack_id":"fin","label":"SYNTHETIC","clock_skew_s":60}`,
		"trailing brace":    limitsDoc("60") + `}`,
		"a second document": limitsDoc("60") + limitsDoc("60"),
	} {
		_, err := session.LoadLimits(strings.NewReader(in))
		var le *session.LimitsError
		if !errors.As(err, &le) || le.Code != session.CodeSchemaInvalid {
			t.Errorf("%s: %v is not a *session.LimitsError with code %s", name, err, session.CodeSchemaInvalid)
		}
	}
	_, err := session.LoadLimits(strings.NewReader(`{"schema_version":`))
	if errors.Unwrap(err) == nil {
		t.Error("the decode cause was dropped")
	}
}
