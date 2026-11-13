package understanding_test

// UND-04, core half (REQ-INT-001, REQ-INP-002; INV-02, INV-12): the second schema gate, the overwrite of the identity from the session, and the intent hash.
// All data is SYNTHETIC.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/understanding"
)

var session = understanding.Session{PrincipalID: "usr_88213", TenantID: "ten_demo", AuthnRef: "ses_7a1b", PrincipalType: "end_user", AssertedBy: "enterprise_idp"}

const corr = "corr_4c21f9"

type testPack struct{}

func (testPack) Ref() (string, string) { return "fin", "1.0.0" }
func (testPack) Node(id string) (understanding.Node, bool) {
	switch id {
	case "fin.payments.exception.investigate":
		return understanding.Node{Slots: map[string]string{"transaction": "Resource", "amount": "Quantity", "counterparty": "Party", "when": "TimeRef", "question_type": "Reference"},
			Outcomes: map[string]bool{"fin.outcome.explained": true, "fin.outcome.escalated": true}}, true
	case "fin.meta.out_of_scope":
		return understanding.Node{Slots: map[string]string{}, Outcomes: map[string]bool{}}, true
	}
	return understanding.Node{}, false
}

func gate(t *testing.T) *understanding.Gate {
	t.Helper()
	g, err := understanding.NewGate(64<<10, testPack{})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// base is a valid Intent Object with no fractional number (a fractional one cannot be hashed yet, see the conflict test).
func base() map[string]any {
	return map[string]any{
		"schema_version": "1.0", "intent_id": "int_01J9X3", "version": 1, "supersedes": nil, "tenant_id": "ten_demo", "correlation_id": corr,
		"created_at":  "2026-10-01T09:47:03Z",
		"principal":   map[string]any{"principal_id": "usr_88213", "principal_type": "unset", "authn_ref": "ses_7a1b", "asserted_by": "unset"},
		"source":      map[string]any{"mode": "MODE_1", "channel": "text", "locale_detected": []any{"en-NG"}, "asr_confidence": nil, "raw_ref": "raw_01J9X3"},
		"pack":        map[string]any{"pack_id": "fin", "pack_version": "1.0.0"},
		"intent_node": "fin.payments.exception.investigate",
		"slots": map[string]any{
			"transaction":   map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX88213"},
			"amount":        map[string]any{"type": "Quantity", "value": 2000000, "unit": "NGN"},
			"counterparty":  map[string]any{"type": "Party", "surface_form": "Ade"},
			"when":          map[string]any{"type": "TimeRef", "surface_form": "yesterday", "window": []any{"2026-09-30T00:00:00Z", "2026-09-30T23:59:59Z"}},
			"question_type": map[string]any{"type": "Reference", "value": "WHERE_IS_MY_MONEY"},
		},
		"ambiguity_flags": []any{}, "desired_outcome_refs": []any{"fin.outcome.explained"}, "status": "VALIDATED",
		"sig": map[string]any{"key_id": "unsigned", "value": "unsigned"},
	}
}

func raw(t *testing.T, mutate func(m map[string]any)) []byte {
	t.Helper()
	m := base()
	if mutate != nil {
		mutate(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func reasons(t *testing.T, err error) []understanding.Reason {
	t.Helper()
	var rej *understanding.Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("error %v is not a *Rejection", err)
	}
	return rej.Reasons
}

func has(rs []understanding.Reason, want understanding.Reason) bool {
	for _, r := range rs {
		if r == want {
			return true
		}
	}
	return false
}

func TestAValidObjectIsAdmittedWithTheSessionsIdentityAndAnIndependentlyComputedHash(t *testing.T) {
	a, err := gate(t).Admit(raw(t, nil), session, corr)
	if err != nil || len(a.Overrides) != 0 {
		t.Fatalf("err %v overrides %v; want an admission with no override", err, a.Overrides)
	}
	want := `{"ambiguity_flags":[],"correlation_id":"corr_4c21f9","created_at":"2026-10-01T09:47:03Z","desired_outcome_refs":["fin.outcome.explained"],"intent_id":"int_01J9X3",` +
		`"intent_node":"fin.payments.exception.investigate","pack":{"pack_id":"fin","pack_version":"1.0.0"},"principal":{"asserted_by":"enterprise_idp","authn_ref":"ses_7a1b",` +
		`"principal_id":"usr_88213","principal_type":"end_user"},"schema_version":"1.0","sig":{"key_id":"unsigned","value":"unsigned"},` +
		`"slots":{"amount":{"type":"Quantity","unit":"NGN","value":2000000},"counterparty":{"surface_form":"Ade","type":"Party"},` +
		`"question_type":{"type":"Reference","value":"WHERE_IS_MY_MONEY"},"transaction":{"resolved_ref":"fin.transaction:TX88213","status":"RESOLVED","type":"Resource"},` +
		`"when":{"surface_form":"yesterday","type":"TimeRef","window":["2026-09-30T00:00:00Z","2026-09-30T23:59:59Z"]}},"source":{"asr_confidence":null,"channel":"text",` +
		`"locale_detected":["en-NG"],"mode":"MODE_1","raw_ref":"raw_01J9X3"},"status":"VALIDATED","supersedes":null,"tenant_id":"ten_demo","version":1}`
	sum := sha256.Sum256([]byte(want))
	if string(a.Intent.JSON()) != want || a.Intent.Hash() != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("canonical form or hash differs from the independently written one:\n got %s\nwant %s\n hash %s", a.Intent.JSON(), want, a.Intent.Hash())
	}
}

func TestTheSessionsIdentityIsWrittenOverWhateverTheModelServiceReturned(t *testing.T) {
	a, err := gate(t).Admit(raw(t, func(m map[string]any) {
		m["tenant_id"] = "ten_other"
		m["principal"] = map[string]any{"principal_id": "usr_attacker", "principal_type": "unset", "authn_ref": "ses_forged", "asserted_by": "unset"}
	}), session, corr)
	if err != nil {
		t.Fatal(err)
	}
	o := a.Intent.Object()
	if string(o.TenantId) != "ten_demo" || string(o.Principal.PrincipalId) != "usr_88213" || o.Principal.PrincipalType != "end_user" || string(o.Principal.AuthnRef) != "ses_7a1b" ||
		o.Principal.AssertedBy != "enterprise_idp" {
		t.Fatalf("the admitted identity is %+v tenant %s, want the session's", o.Principal, o.TenantId)
	}
	if got := strings.Join(a.Overrides, ","); got != "tenant_id,principal_id,authn_ref" {
		t.Errorf("overrides %q, want the identity members that differed, by name (principal_type and asserted_by are never the model's, ruling A166)", got)
	}
	if strings.Contains(string(a.Intent.JSON()), "attacker") || strings.Contains(string(a.Intent.JSON()), "ten_other") {
		t.Error("a forged identity survives in the canonical form the hash covers")
	}
}

// REQ-INP-002 acceptance: random principals in the model output never survive.
func TestNoPrincipalOrTenantInTheModelOutputEverSurvives(t *testing.T) {
	g := gate(t)
	rapid.Check(t, func(rt *rapid.T) {
		pid, tid := rapid.StringN(0, 40, 80).Draw(rt, "principal"), rapid.StringN(0, 40, 80).Draw(rt, "tenant")
		a, err := g.Admit(rawAny(func(m map[string]any) {
			m["tenant_id"] = tid
			m["principal"].(map[string]any)["principal_id"] = pid
		}), session, corr)
		if err != nil {
			rt.Fatalf("admission failed for %q %q: %v", pid, tid, err)
		}
		o := a.Intent.Object()
		if string(o.TenantId) != "ten_demo" || string(o.Principal.PrincipalId) != "usr_88213" {
			rt.Fatalf("a model-supplied identity survived: %q %q", o.Principal.PrincipalId, o.TenantId)
		}
	})
}

func rawAny(mutate func(m map[string]any)) []byte {
	m := base()
	mutate(m)
	b, _ := json.Marshal(m)
	return b
}

// REQ-INT-001 acceptance: a fuzzed Intent Object with any unknown field is rejected (100% over 10^4 generated cases).
func TestEveryUnknownFieldIsRejectedOverTenThousandGeneratedObjects(t *testing.T) {
	g := gate(t)
	r := rand.New(rand.NewSource(20261111)) // a fixed seed: the run is repeatable
	places := []func(m map[string]any, name string){
		func(m map[string]any, n string) { m[n] = "x" },
		func(m map[string]any, n string) { m["principal"].(map[string]any)[n] = "x" },
		func(m map[string]any, n string) { m["source"].(map[string]any)[n] = 1 },
		func(m map[string]any, n string) { m["pack"].(map[string]any)[n] = true },
		func(m map[string]any, n string) { m["sig"].(map[string]any)[n] = nil },
		func(m map[string]any, n string) { m["slots"].(map[string]any)["amount"].(map[string]any)[n] = "x" },
		func(m map[string]any, n string) { m["slots"].(map[string]any)["transaction"].(map[string]any)[n] = "x" },
		func(m map[string]any, n string) { m["slots"].(map[string]any)["when"].(map[string]any)[n] = []any{} },
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz_ABCDEFGHIJ0123456789"
	for i := 0; i < 10000; i++ {
		b := make([]byte, 1+r.Intn(24))
		for j := range b {
			b[j] = alphabet[r.Intn(len(alphabet))]
		}
		name := "x" + string(b) // never one of the contract's names
		doc := rawAny(func(m map[string]any) { places[i%len(places)](m, name) })
		_, err := g.Admit(doc, session, corr)
		if err == nil || !has(reasons(t, err), understanding.ReasonShape) && !has(reasons(t, err), understanding.ReasonUnknownSlot) {
			t.Fatalf("case %d (place %d, member %q): admitted or refused for another reason: %v", i, i%len(places), name, err)
		}
	}
}

func TestWhatTheGateRefusesAndWhy(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m map[string]any)
		want   understanding.Reason
	}{
		{"status EXTRACTED", func(m map[string]any) { m["status"] = "EXTRACTED" }, understanding.ReasonStatus},
		{"status REJECTED_AT_GATE", func(m map[string]any) { m["status"] = "REJECTED_AT_GATE" }, understanding.ReasonStatus},
		{"schema version 2.0", func(m map[string]any) { m["schema_version"] = "2.0" }, understanding.ReasonSchemaVersion},
		{"intent id off shape", func(m map[string]any) { m["intent_id"] = "INT_1" }, understanding.ReasonIntentID},
		{"version 0", func(m map[string]any) { m["version"] = 0 }, understanding.ReasonVersion},
		{"version above the safe integer", func(m map[string]any) { m["version"] = 9007199254740992 }, understanding.ReasonVersion},
		{"version 1 that supersedes", func(m map[string]any) { m["supersedes"] = "int_01J9X3@0" }, understanding.ReasonSupersedes},
		{"version 2 that supersedes nothing", func(m map[string]any) { m["version"] = 2 }, understanding.ReasonSupersedes},
		{"version 3 that supersedes version 1", func(m map[string]any) { m["version"] = 3; m["supersedes"] = "int_01J9X3@1" }, understanding.ReasonSupersedes},
		{"supersedes another intent", func(m map[string]any) { m["version"] = 2; m["supersedes"] = "int_other@1" }, understanding.ReasonSupersedes},
		{"created_at without Z", func(m map[string]any) { m["created_at"] = "2026-10-01T09:47:03+01:00" }, understanding.ReasonCreatedAt},
		{"created_at that is not a date", func(m map[string]any) { m["created_at"] = "2026-13-45T25:61:61Z" }, understanding.ReasonCreatedAt},
		{"another correlation id", func(m map[string]any) { m["correlation_id"] = "corr_other" }, understanding.ReasonCorrelation},
		{"MODE_2", func(m map[string]any) { m["source"].(map[string]any)["mode"] = "MODE_2" }, understanding.ReasonModeNotEnabled},
		{"channel email", func(m map[string]any) { m["source"].(map[string]any)["channel"] = "email" }, understanding.ReasonShape},
		{"locale off shape", func(m map[string]any) { m["source"].(map[string]any)["locale_detected"] = []any{"EN_ng"} }, understanding.ReasonSource},
		{"empty raw_ref", func(m map[string]any) { m["source"].(map[string]any)["raw_ref"] = "" }, understanding.ReasonSource},
		{"another pack version", func(m map[string]any) { m["pack"].(map[string]any)["pack_version"] = "1.0.1" }, understanding.ReasonPackMismatch},
		{"another pack", func(m map[string]any) { m["pack"].(map[string]any)["pack_id"] = "ins" }, understanding.ReasonPackMismatch},
		{"a node the pack does not have", func(m map[string]any) { m["intent_node"] = "fin.payments.exception.invent" }, understanding.ReasonUnknownNode},
		{"a node name off shape", func(m map[string]any) { m["intent_node"] = "Fin Node" }, understanding.ReasonUnknownNode},
		{"a slot the node does not name", func(m map[string]any) {
			m["slots"].(map[string]any)["invented"] = map[string]any{"type": "Party", "surface_form": "x"}
		}, understanding.ReasonUnknownSlot},
		{"a slot of another type", func(m map[string]any) {
			m["slots"].(map[string]any)["amount"] = map[string]any{"type": "Party", "surface_form": "x"}
		}, understanding.ReasonSlotType},
		{"a slot name off shape", func(m map[string]any) {
			m["slots"].(map[string]any)["Bad-Name"] = map[string]any{"type": "Party", "surface_form": "x"}
		}, understanding.ReasonSlotName},
		{"RESOLVED without a resolved_ref", func(m map[string]any) {
			delete(m["slots"].(map[string]any)["transaction"].(map[string]any), "resolved_ref")
		}, understanding.ReasonSlotInvalid},
		{"a resolved_ref on a slot that is not RESOLVED", func(m map[string]any) {
			m["slots"].(map[string]any)["transaction"].(map[string]any)["status"] = "AMBIGUOUS"
		}, understanding.ReasonSlotInvalid},
		{"a resolved_ref off shape", func(m map[string]any) {
			m["slots"].(map[string]any)["transaction"].(map[string]any)["resolved_ref"] = "TX1"
		}, understanding.ReasonSlotInvalid},
		{"a negative amount", func(m map[string]any) { m["slots"].(map[string]any)["amount"].(map[string]any)["value"] = -1 }, understanding.ReasonSlotInvalid},
		{"an amount above the safe integer", func(m map[string]any) {
			m["slots"].(map[string]any)["amount"].(map[string]any)["value"] = 9007199254740992
		}, understanding.ReasonSlotInvalid},
		{"a unit that is neither a currency nor a pack unit", func(m map[string]any) { m["slots"].(map[string]any)["amount"].(map[string]any)["unit"] = "USD" }, understanding.ReasonSlotInvalid},
		{"a window with one instant", func(m map[string]any) {
			m["slots"].(map[string]any)["when"].(map[string]any)["window"] = []any{"2026-09-30T00:00:00Z"}
		}, understanding.ReasonSlotInvalid},
		{"a window that is not a pair of instants", func(m map[string]any) {
			m["slots"].(map[string]any)["when"].(map[string]any)["window"] = []any{"yesterday", "today"}
		}, understanding.ReasonSlotInvalid},
		{"an empty reference value", func(m map[string]any) { m["slots"].(map[string]any)["question_type"].(map[string]any)["value"] = "" }, understanding.ReasonSlotInvalid},
		{"an ambiguity flag in lower case", func(m map[string]any) { m["ambiguity_flags"] = []any{"multiple_match"} }, understanding.ReasonAmbiguityFlag},
		{"a repeated ambiguity flag", func(m map[string]any) { m["ambiguity_flags"] = []any{"MULTIPLE_MATCH", "MULTIPLE_MATCH"} }, understanding.ReasonAmbiguityFlag},
		{"an outcome the node does not define", func(m map[string]any) { m["desired_outcome_refs"] = []any{"fin.outcome.refunded"} }, understanding.ReasonOutcomeNotInNode},
		{"a repeated outcome", func(m map[string]any) {
			m["desired_outcome_refs"] = []any{"fin.outcome.explained", "fin.outcome.explained"}
		}, understanding.ReasonOutcomeNotInNode},
		{"an empty signature", func(m map[string]any) { m["sig"] = map[string]any{"key_id": "", "value": "x"} }, understanding.ReasonSig},
	}
	g := gate(t)
	for _, c := range cases {
		_, err := g.Admit(raw(t, c.mutate), session, corr)
		if err == nil {
			t.Errorf("%s: admitted", c.name)
			continue
		}
		if !has(reasons(t, err), c.want) {
			t.Errorf("%s: reasons %v, want %s among them", c.name, reasons(t, err), c.want)
		}
	}
}

func TestADocumentThatIsNotExactlyOneObjectIsRefused(t *testing.T) {
	good := string(raw(t, nil))
	g := gate(t)
	cases := map[string]string{
		"empty":                 "",
		"not json":              "not json",
		"trailing data":         good + " {}",
		"trailing garbage":      good + "x",
		"an array":              "[" + good + "]",
		"a repeated member":     strings.Replace(good, `"version":1`, `"version":1,"version":2`, 1),
		"a null member":         strings.Replace(good, `"status":"VALIDATED"`, `"status":null`, 1),
		"a missing member":      strings.Replace(good, `"intent_node":"fin.payments.exception.investigate",`, "", 1),
		"a string for a number": strings.Replace(good, `"version":1`, `"version":"1"`, 1),
		"a float for a version": strings.Replace(good, `"version":1`, `"version":1.0`, 1),
	}
	for name, doc := range cases {
		if _, err := g.Admit([]byte(doc), session, corr); err == nil {
			t.Errorf("%s: admitted", name)
		}
	}
	big := strings.Repeat(" ", 64<<10) + good
	if _, err := g.Admit([]byte(big), session, corr); err == nil || !has(reasons(t, err), understanding.ReasonSize) {
		t.Errorf("an over-size document: %v", err)
	}
}

// The intent hash cannot cover a fraction yet (platform/crypto refuses every non-integer number), so an object with a confidence is refused, never hashed by a
// different rule. The PR carries the CONFLICT block for the decision; this test is the safer reading made explicit.
func TestAnObjectWithAFractionalConfidenceIsRefusedBecauseItsHashIsUndefined(t *testing.T) {
	g := gate(t)
	for name, mutate := range map[string]func(m map[string]any){
		"a slot confidence": func(m map[string]any) {
			m["slots"].(map[string]any)["transaction"].(map[string]any)["confidence"] = 0.91
		},
		"an ASR confidence": func(m map[string]any) { m["source"].(map[string]any)["asr_confidence"] = 0.91 },
	} {
		_, err := g.Admit(raw(t, mutate), session, corr)
		if err == nil || !has(reasons(t, err), understanding.ReasonHashUnavailable) {
			t.Errorf("%s: %v, want a refusal with HASH_UNAVAILABLE", name, err)
		}
	}
	// A confidence of exactly 1 or 0 is an integer in JSON and is hashed.
	if _, err := g.Admit(raw(t, func(m map[string]any) { m["slots"].(map[string]any)["transaction"].(map[string]any)["confidence"] = 1 }), session, corr); err != nil {
		t.Errorf("a confidence of 1: %v", err)
	}
	// An out-of-range ratio is a schema failure first.
	if _, err := g.Admit(raw(t, func(m map[string]any) {
		m["slots"].(map[string]any)["transaction"].(map[string]any)["confidence"] = 1.5
	}), session, corr); err == nil ||
		!has(reasons(t, err), understanding.ReasonSlotInvalid) {
		t.Errorf("a confidence of 1.5: %v", err)
	}
}

func TestASessionThatIsIncompleteIsTheCallersBugAndAdmitsNothing(t *testing.T) {
	g := gate(t)
	for name, s := range map[string]understanding.Session{
		"no principal": {TenantID: "ten_demo", AuthnRef: "ses_7a1b", PrincipalType: "end_user", AssertedBy: "enterprise_idp"},
		"no tenant":    {PrincipalID: "usr_88213", AuthnRef: "ses_7a1b", PrincipalType: "end_user", AssertedBy: "enterprise_idp"},
		"a bad authn":  {PrincipalID: "usr_88213", TenantID: "ten_demo", AuthnRef: "x", PrincipalType: "end_user", AssertedBy: "enterprise_idp"},
		"a bad type":   {PrincipalID: "usr_88213", TenantID: "ten_demo", AuthnRef: "ses_7a1b", PrincipalType: "End User", AssertedBy: "enterprise_idp"},
		"nothing":      {},
	} {
		_, err := g.Admit(raw(t, nil), s, corr)
		if err == nil || !has(reasons(t, err), understanding.ReasonIdentityNotInOrder) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := g.Admit(raw(t, nil), session, "corr x"); err == nil {
		t.Error("a malformed correlation id was accepted")
	}
}

func TestARejectionNamesReasonsAndNeverQuotesTheObject(t *testing.T) {
	const marker = "MARKER-0123456789-SECRET"
	_, err := gate(t).Admit(raw(t, func(m map[string]any) {
		m["intent_node"] = marker
		m["source"].(map[string]any)["raw_ref"] = ""
		m["sig"] = map[string]any{"key_id": marker, "value": ""}
	}), session, corr)
	if err == nil || strings.Contains(err.Error(), marker) || strings.Contains(fmt.Sprintf("%+v", err), marker) {
		t.Fatalf("error %v quotes the object", err)
	}
	rs := reasons(t, err)
	for i := 1; i < len(rs); i++ {
		if rs[i-1] >= rs[i] {
			t.Errorf("reasons %v are not sorted and unique", rs)
		}
	}
}

func TestNewGateRefusesWhatItCannotRunWith(t *testing.T) {
	if _, err := understanding.NewGate(0, testPack{}); err == nil {
		t.Error("a zero size bound was accepted")
	}
	if _, err := understanding.NewGate(1, nil); err == nil {
		t.Error("no pack was accepted")
	}
}

// Ruling A163 (asserted_by pattern ^[a-z][a-z0-9_-]*$): the session's identity provider may be named with a hyphen, as the POC's own is (noema-poc-idp). Session.valid()
// is the gate's own check of the session the caller passes, so it must allow what the contract's pattern allows and nothing more (INV-12).
func TestTheAssertedByOfASessionFollowsTheContractsPattern(t *testing.T) {
	g := gate(t)
	with := func(by string) understanding.Session {
		s := session
		s.AssertedBy = by
		return s
	}
	for _, by := range []string{"noema-poc-idp", "enterprise_idp", "idp", "a-b_c-9", "x9"} {
		a, err := g.Admit(raw(t, nil), with(by), corr)
		if err != nil {
			t.Errorf("asserted_by %q: %v", by, err)
			continue
		}
		if o := a.Intent.Object(); o.Principal.AssertedBy != by {
			t.Errorf("asserted_by %q was admitted as %q", by, o.Principal.AssertedBy)
		}
	}
	for name, by := range map[string]string{"empty": "", "a leading hyphen": "-idp", "a leading digit": "9idp", "a leading underscore": "_idp", "upper case": "Noema-idp",
		"a space": "noema idp", "a dot": "noema.idp", "a slash": "noema/idp", "a newline at the end": "noema-idp\n", "a unicode hyphen": "noema\u2010idp"} {
		_, err := g.Admit(raw(t, nil), with(by), corr)
		if err == nil || !has(reasons(t, err), understanding.ReasonIdentityNotInOrder) {
			t.Errorf("%s (%q): %v, want SESSION_INVALID", name, by, err)
		}
	}
}
