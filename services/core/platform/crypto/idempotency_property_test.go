package crypto_test

// EXE-03 / REQ-EXE-002 property tests for the idempotency key of PERD 27.2: stable, sensitive to each of the seven fields, unambiguous
// across field boundaries, and with no attempt or policy-version input. All values are SYNTHETIC.

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

var keyShape = regexp.MustCompile(`^idk_v1_[0-9a-f]{64}$`)

// field values include the awkward ones: empty, NUL, multi-byte, and strings that look like length prefixes.
func fieldValue() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.String(),
		rapid.StringMatching(`[a-z_0-9]{0,6}`),
		rapid.SampledFrom([]string{"", "\x00", "\x00\x00\x00\x01a", "é", "日本", "ten_a", "ten_ab", "a|b", "0", "1"}),
	)
}

func fields() *rapid.Generator[crypto.IdempotencyKeyFields] {
	return rapid.Custom(func(t *rapid.T) crypto.IdempotencyKeyFields {
		return crypto.IdempotencyKeyFields{
			TenantID: fieldValue().Draw(t, "tenant"), PrincipalID: fieldValue().Draw(t, "principal"), TransactionID: fieldValue().Draw(t, "transaction"),
			LogicalAction: fieldValue().Draw(t, "action"), ReasonCode: fieldValue().Draw(t, "reason"), PolicyScope: fieldValue().Draw(t, "policy"),
			LogicalResolutionEpoch: fieldValue().Draw(t, "epoch"),
		}
	})
}

// REQ-EXE-002: the key is stable and well formed for any input.
func TestKeyIsStableAndWellFormedForAnyInput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := fields().Draw(t, "fields")
		k := crypto.IdempotencyKey(f)
		if !keyShape.MatchString(k) || k != crypto.IdempotencyKey(f) {
			t.Fatalf("key %q is unstable or malformed for %+v", k, f)
		}
	})
}

// Two different field tuples never share a key. This is the property that fails if the length prefixes are dropped, because then
// ("ab", "c") and ("a", "bc") would hash alike.
func TestDifferentFieldsNeverShareAKey(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := fields().Draw(t, "a"), fields().Draw(t, "b")
		if a != b && crypto.IdempotencyKey(a) == crypto.IdempotencyKey(b) {
			t.Fatalf("%+v and %+v share the key %s", a, b, crypto.IdempotencyKey(a))
		}
		if a == b && crypto.IdempotencyKey(a) != crypto.IdempotencyKey(b) {
			t.Fatalf("%+v gave two keys", a)
		}
	})
}

// Changing any one of the seven fields changes the key, and moving text across the border of two neighbouring fields changes it too.
func TestEachFieldAndEachBoundaryChangesTheKey(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := fields().Draw(t, "fields")
		i := rapid.IntRange(0, 6).Draw(t, "field")
		g := f
		v := reflect.ValueOf(&g).Elem().Field(i)
		v.SetString(v.String() + "x")
		if crypto.IdempotencyKey(f) == crypto.IdempotencyKey(g) {
			t.Fatalf("field %d does not change the key", i)
		}
		// boundary: move the first byte of field i+1 to the end of field i (when there is one and it is a character of its own)
		if i < 6 {
			h := f
			next := reflect.ValueOf(&h).Elem().Field(i + 1)
			if next.Len() > 0 {
				cur := reflect.ValueOf(&h).Elem().Field(i)
				moved := next.String()[:1]
				cur.SetString(cur.String() + moved)
				next.SetString(next.String()[1:])
				if crypto.IdempotencyKey(f) == crypto.IdempotencyKey(h) {
					t.Fatalf("moving %q across fields %d and %d keeps the key", moved, i, i+1)
				}
			}
		}
	})
}

// REQ-EXE-002: "ignores attempt number; a policy publish never creates a new key". The attempt number and a policy version are not inputs:
// the fields are exactly the seven of PERD 27.2, all strings, none named for an attempt, a retry, or a version. A future field fails this.
func TestThereIsNoAttemptOrPolicyVersionInput(t *testing.T) {
	want := []string{"TenantID", "PrincipalID", "TransactionID", "LogicalAction", "ReasonCode", "PolicyScope", "LogicalResolutionEpoch"}
	ty := reflect.TypeOf(crypto.IdempotencyKeyFields{})
	if ty.NumField() != len(want) {
		t.Fatalf("the key has %d inputs, PERD 27.2 has %d", ty.NumField(), len(want))
	}
	for i, name := range want {
		f := ty.Field(i)
		if f.Name != name || f.Type.Kind() != reflect.String {
			t.Fatalf("input %d is %s %s, want string %s", i, f.Name, f.Type, name)
		}
		for _, banned := range []string{"attempt", "retry", "version", "nonce", "time"} {
			if strings.Contains(strings.ToLower(f.Name), banned) {
				t.Fatalf("input %s names %q", f.Name, banned)
			}
		}
	}
}
