package decide_test

// TRU-07 (A341, interim; REQ-TRU-009, REQ-TRU-007, INV-25, INV-21; SoT 43.2a): the policy version a confirmation binds is the COMPOSITE of every policy version that took part in T11:
// "ps:" and the lower-case SHA-256 hex of the RFC 8785 form of the sorted, distinct list of "policy_id@version" strings. The vectors were computed by an independent Python
// implementation (json.dumps(sorted(set(list)), separators=(",", ":")) hashed with hashlib.sha256). All data is SYNTHETIC.

import (
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

func refs(pairs ...string) []decide.PolicyVersionRef {
	var out []decide.PolicyVersionRef
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, decide.PolicyVersionRef{PolicyID: pairs[i], Version: pairs[i+1]})
	}
	return out
}

const (
	vecOne    = "ps:50ca8c31e714463c1257e6e3f8b3f6c57fdb0c3fe471e8a2f015fa2b4f539ebb" // ["pol_synth01@3"]
	vecTwo    = "ps:f81f4a1916c40274bfeb88b814e9b98bc36d43393b9cff1249f6b39c1fe364d1" // ["pol_synth01@3","pol_synth02@1"]
	vecTwoVer = "ps:52a779a7fd72828f57e07726d30090c0beacc19b4b76fc1f06573b45d1d0c5d7" // ["pol_synth01@2","pol_synth01@3"]
	vecDotted = "ps:c1c966ac3b0b555cd16913245fcdf40ebe103d30ea356053aca7a1be0b76967e" // ["fin.pol@3.1"]
)

// One policy version is still a composite: one rule, no special case.
func TestCompositePolicyVersionGoldenVectors(t *testing.T) {
	for name, tc := range map[string]struct {
		in   []decide.PolicyVersionRef
		want string
	}{
		"one policy version":                   {refs("pol_synth01", "3"), vecOne},
		"one dotted policy version":            {refs("fin.pol", "3.1"), vecDotted},
		"two policies":                         {refs("pol_synth01", "3", "pol_synth02", "1"), vecTwo},
		"two versions of one policy":           {refs("pol_synth01", "2", "pol_synth01", "3"), vecTwoVer},
		"two policies listed in another order": {refs("pol_synth02", "1", "pol_synth01", "3"), vecTwo},
		"duplicates are one":                   {refs("pol_synth02", "1", "pol_synth01", "3", "pol_synth01", "3", "pol_synth02", "1"), vecTwo},
	} {
		got, err := decide.CompositePolicyVersion(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", name, got, err, tc.want)
		}
	}
}

// The list is sorted as whole "policy_id@version" strings, in byte order, not by (id, version): "a.b@1" sorts before "a@1" because '.' (0x2e) is below '@' (0x40), while sorting by
// the id alone would put "a" first. The vector was computed with the independent implementation.
func TestCompositePolicyVersionSortsTheWholeStringsInByteOrder(t *testing.T) {
	const want = "ps:130485d2518500b88d985d8c3ffb4e0d5d0f1ae2129c4fecf0c226428bcc984e"
	a, err1 := decide.CompositePolicyVersion(refs("a", "1", "a.b", "1"))
	b, err2 := decide.CompositePolicyVersion(refs("a.b", "1", "a", "1"))
	if err1 != nil || err2 != nil || a != b {
		t.Fatalf("order changed the composite: %q %v, %q %v", a, err1, b, err2)
	}
	if a != want {
		t.Errorf("composite %q, want %q", a, want)
	}
}

// INV-25: any change to any policy version, or one more, is another composite; the form is "ps:" and 64 lower-case hex characters.
func TestCompositePolicyVersionChangesWithAnyMemberAndHasItsForm(t *testing.T) {
	base := refs("pol_synth01", "3", "pol_synth02", "1")
	want, err := decide.CompositePolicyVersion(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 3+64 || !strings.HasPrefix(want, "ps:") || strings.ToLower(want) != want {
		t.Errorf("composite %q is not ps: and 64 lower-case hex", want)
	}
	for name, other := range map[string][]decide.PolicyVersionRef{
		"another version":        refs("pol_synth01", "4", "pol_synth02", "1"),
		"another policy":         refs("pol_synth01", "3", "pol_synth03", "1"),
		"one fewer":              refs("pol_synth01", "3"),
		"one more":               refs("pol_synth01", "3", "pol_synth02", "1", "pol_synth03", "1"),
		"id and version swapped": refs("3", "pol_synth01", "pol_synth02", "1"),
	} {
		got, err := decide.CompositePolicyVersion(other)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got == want {
			t.Errorf("%s: the composite did not change", name)
		}
	}
}

// INV-21: the order and repeats of the list never change the composite.
func TestCompositePolicyVersionIgnoresOrderAndRepeats(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	base := refs("pol_synth01", "3", "pol_synth02", "1", "pol_synth03", "2.1", "pol_synth01", "2")
	want, err := decide.CompositePolicyVersion(base)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		shuffled := append(append([]decide.PolicyVersionRef(nil), base...), base[rng.Intn(len(base))])
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		if got, err := decide.CompositePolicyVersion(shuffled); err != nil || got != want {
			t.Fatalf("shuffle %d: %q, %v; want %q", i, got, err, want)
		}
	}
}

// A set that cannot be bound is an error and never a composite: none, an empty part, a character outside the binding's set (a space, a quote, a backslash, a control character, a
// non-ASCII letter) and an "@" inside a part (which would make "a@b"+"c" and "a"+"b@c" one string). Fail closed.
func TestCompositePolicyVersionRefusesASetThatCannotBeBound(t *testing.T) {
	for name, in := range map[string][]decide.PolicyVersionRef{
		"no policy":               nil,
		"an empty list":           {},
		"an empty policy ID":      refs("", "3"),
		"an empty version":        refs("pol_synth01", ""),
		"a space":                 refs("pol synth", "3"),
		"a quote":                 refs("pol\"synth", "3"),
		"a backslash":             refs("pol_synth", "3\\1"),
		"a newline":               refs("pol_synth", "3\n"),
		"a non-ASCII letter":      refs("pol_synthé", "3"),
		"an @ in the policy ID":   refs("pol@synth", "3"),
		"an @ in the version":     refs("pol_synth", "3@1"),
		"one bad among good ones": refs("pol_synth01", "3", "pol synth02", "1"),
	} {
		got, err := decide.CompositePolicyVersion(in)
		if !errors.Is(err, decide.ErrPolicySetInvalid) || got != "" {
			t.Errorf("%s: %q, %v; want ErrPolicySetInvalid and no composite", name, got, err)
		}
	}
}

// The Basis form is the same function over the distinct (policy ID, version) of the rules: two rules of one policy version are one member, and a basis with no rule has none.
func TestBasisCompositePolicyVersionIsTheCompositeOfItsRules(t *testing.T) {
	b := decide.Basis{PolicyRules: []decide.PolicyRuleVersion{
		{RuleID: "r1", PolicyID: "pol_synth02", Version: "1"}, {RuleID: "r2", PolicyID: "pol_synth01", Version: "3"}, {RuleID: "r3", PolicyID: "pol_synth01", Version: "3"}}}
	if got, err := b.CompositePolicyVersion(); err != nil || got != vecTwo {
		t.Errorf("%q, %v; want %q", got, err, vecTwo)
	}
	if got, err := (decide.Basis{}).CompositePolicyVersion(); !errors.Is(err, decide.ErrPolicySetInvalid) || got != "" {
		t.Errorf("a basis with no rule: %q, %v; want ErrPolicySetInvalid", got, err)
	}
}
