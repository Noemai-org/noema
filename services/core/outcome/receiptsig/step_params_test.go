package receiptsig_test

import (
	"crypto/ed25519"
	"testing"

	"github.com/Noemai-org/noema/services/core/outcome/receiptsig"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// A692 (interim, relayed; OD-P-119; REQ-AUD-006): step_params is a member of the signed document, so the signature covers it. A receipt without it (every receipt made before the
// member existed) still verifies unchanged; changing, adding or removing the member after signing is INVALID. The values are SYNTHETIC.
func withStepParams(f fixture) fixture {
	doc := map[string]any{}
	for k, v := range f.doc {
		doc[k] = v
	}
	doc["step_params"] = map[string]any{"queue": "SYNTH_QUEUE", "urgent": true, "rank": int64(2), "empty": nil}
	f.doc = doc
	return f
}

func TestAReceiptWithStepParamsIsValidAndOneWithoutStillIs(t *testing.T) {
	f := newFixture(t)
	if got := check(t, f.checker(receiptsig.Active), f.signed(t)); got != verifier.AuthValid {
		t.Fatalf("a receipt with no step_params: %s", got)
	}
	g := withStepParams(newFixture(t))
	if got := check(t, g.checker(receiptsig.Active), g.signed(t)); got != verifier.AuthValid {
		t.Fatalf("a receipt with step_params: %s", got)
	}
}

func TestStepParamsAreBoundByTheSignature(t *testing.T) {
	g := withStepParams(newFixture(t))
	sig := ed25519.Sign(g.priv, g.message(t, tagRecpt, g.doc))
	// signed with the member, then stored without it, with a changed value, and with an added one
	for name, mutate := range map[string]func(map[string]any){
		"removed": func(d map[string]any) { delete(d, "step_params") },
		"changed": func(d map[string]any) {
			d["step_params"] = map[string]any{"queue": "OTHER", "urgent": true, "rank": int64(2), "empty": nil}
		},
		"added": func(d map[string]any) { d["step_params"].(map[string]any)["extra"] = "x" },
	} {
		h := newFixture(t)
		h.pub, h.priv = g.pub, g.priv
		doc := map[string]any{}
		for k, v := range g.doc {
			doc[k] = v
		}
		doc["step_params"] = map[string]any{"queue": "SYNTH_QUEUE", "urgent": true, "rank": int64(2), "empty": nil}
		mutate(doc)
		h.doc = doc
		if got := check(t, g.checker(receiptsig.Active), h.receipt(t, keyID, sig)); got != verifier.AuthInvalid {
			t.Errorf("%s: got %s, want INVALID", name, got)
		}
	}
}
