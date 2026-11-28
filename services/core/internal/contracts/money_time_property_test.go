package contracts_test

// DATA-02 / REQ-DATA-002, DEC-2026-10-16 money-and-value-lists M3 and M4, AGENT-15: in Go an amount is an int64 decoded from a digit-only literal, and the canonical form every hash and signature
// is computed over writes it as digits only, within 2^53-1. Property tests over the generated Money type and `crypto.CanonicalJSON`. Every value is SYNTHETIC.
//
// What this file does not claim: the generated Go decoder holds no bounds (tools/codegen/README.md: "the JSON Schema validator owns those, at every boundary"), so a negative or over-ceiling
// literal decodes here; the ceiling is held where a hash is computed, which is what the last two tests pin.

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

const maxSafeInt = 9007199254740991 // 2^53-1

func moneyJSON(literal string) []byte {
	return []byte(`{"amount_minor":` + literal + `,"currency":"NGN"}`)
}

func TestAnIntegerAmountRoundTripsAsDigitsOnly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.Int64Range(0, maxSafeInt).Draw(t, "amount_minor")
		var m contracts.MoneyV1Money
		if err := json.Unmarshal(moneyJSON(fmt.Sprint(n)), &m); err != nil {
			t.Fatalf("a digit-only integer in range was refused: %v", err)
		}
		if int64(m.AmountMinor) != n {
			t.Fatalf("decoded %d, want %d", m.AmountMinor, n)
		}
		out, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf(`{"amount_minor":%d,"currency":"NGN"}`, n); string(out) != want {
			t.Fatalf("the encoded amount is not digits only: %s, want %s", out, want)
		}
		canonical, err := crypto.CanonicalJSON(m)
		if err != nil {
			t.Fatalf("an in-range amount has no canonical form: %v", err)
		}
		if want := fmt.Sprintf(`{"amount_minor":%d,"currency":"NGN"}`, n); string(canonical) != want {
			t.Fatalf("canonical form %s, want %s", canonical, want)
		}
	})
}

func TestAFloatQuotedOrContainerAmountIsNeverDecoded(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.Int64Range(0, maxSafeInt).Draw(t, "amount_minor")
		form := rapid.SampledFrom([]string{"%d.0", "%d.00", "%d.5", "%de0", "%dE0", "%d.0e0", `"%d"`, "[%d]", "%de3"}).Draw(t, "form")
		var m contracts.MoneyV1Money
		if err := json.Unmarshal(moneyJSON(fmt.Sprintf(form, n)), &m); err == nil {
			t.Fatalf("the literal %q was decoded as %d", fmt.Sprintf(form, n), m.AmountMinor)
		}
	})
}

func TestAnAmountAboveTheCeilingHasNoCanonicalForm(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.Int64Range(maxSafeInt+1, math.MaxInt64).Draw(t, "amount_minor")
		var m contracts.MoneyV1Money
		if err := json.Unmarshal(moneyJSON(fmt.Sprint(n)), &m); err != nil {
			return // refused at the decoder, which is also a refusal
		}
		if out, err := crypto.CanonicalJSON(m); err == nil {
			t.Fatalf("an amount of %d (above 2^53-1) was canonicalised: %s", n, out)
		}
	})
}

func TestACanonicalFormIsNeverComputedOverAFloat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := rapid.Float64().Draw(t, "float")
		if _, err := crypto.CanonicalJSON(map[string]any{"amount_minor": f}); err == nil {
			t.Fatalf("a float64 (%v) reached a canonical form", f)
		}
		whole := float64(rapid.Int64Range(0, maxSafeInt).Draw(t, "whole"))
		if _, err := crypto.CanonicalJSON(map[string]any{"amount_minor": whole}); err == nil {
			t.Fatalf("an integral float64 (%v) reached a canonical form: integral is not integer", whole)
		}
	})
}

// The edges, named: rapid may or may not draw them (M10: 0, 1 and 2^53-1 are accepted, 2^53 is not).
func TestTheCeilingEdgesAreExact(t *testing.T) {
	for _, tc := range []struct {
		n  int64
		ok bool
	}{{0, true}, {1, true}, {maxSafeInt - 1, true}, {maxSafeInt, true}, {maxSafeInt + 1, false}, {math.MaxInt64, false}} {
		var m contracts.MoneyV1Money
		if err := json.Unmarshal(moneyJSON(fmt.Sprint(tc.n)), &m); err != nil {
			t.Fatalf("%d: %v", tc.n, err)
		}
		_, err := crypto.CanonicalJSON(m)
		if (err == nil) != tc.ok {
			t.Fatalf("%d: canonical form err=%v, want ok=%v", tc.n, err, tc.ok)
		}
	}
}
