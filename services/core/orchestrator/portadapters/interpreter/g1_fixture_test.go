package interpreterport_test

// UND-16 (A439, A460; REQ-INT-001, REQ-BEN-004; INV-12, INV-21, INV-22; AGENT-12, AGENT-25): the canonical inputs of BO's five G1 cases, built by the real Interpreter port.
//
// The record-only pass of the compile acceptance run must send the model service what the core sends: the canonical input the port builds from a verified session and a turn, and nothing the
// port does not build (no locale hint). A460: the Go test that builds them lives here; the fixture it produces sits under benchmark/replay/, pinned to BO's `inputs_sha256`. The inputs below are a
// small SYNTHETIC copy of the shape of `benchmark/replay/g1_inputs.json`; the real file is read by the test that compares the committed fixture with fresh output.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

const goldenInstant = "2026-11-16T12:30:00Z" // the compile acceptance goldens' synthetic afternoon (benchmark/compile_acceptance/golden.py)

var (
	uuidShape  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	corrLetter = regexp.MustCompile(`^corr_[a-z]{20}$`)
	allDigits  = regexp.MustCompile(`^[0-9-]+$`)
)

// inputsDigest is the rule of BO's exporter (benchmark/replay/g1_inputs.py), written again here so that the builder is held to it by something it did not write: the document without its own
// digest, keys sorted, two-space indent, not HTML-escaped, not ASCII-escaped.
func inputsDigest(t *testing.T, doc map[string]any) string {
	t.Helper()
	body := map[string]any{}
	for k, v := range doc {
		if k != "inputs_sha256" {
			body[k] = v
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(body); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes.TrimRight(buf.Bytes(), "\n"))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func g1Case(id, principal, text string, locale ...string) map[string]any {
	loc := make([]any, 0, len(locale))
	for _, l := range locale {
		loc = append(loc, l)
	}
	return map[string]any{
		"case_id": id, "case_sha256": "sha256:" + strings.Repeat("c", 64), "channel": "text", "locale_mix": loc, "canonical_locale": "en-NG",
		"principal": map[string]any{"principal_id": principal, "roles": []any{"customer"}},
		"candidates": map[string]any{
			"beneficiaries": []any{map[string]any{"beneficiary_id": "ben_0001", "name": "Chidi Okonkwo", "aliases": []any{"Chidi"}, "account_last4": "3644"}},
			"transactions": []any{map[string]any{"ref": "fin.transaction:TX7992", "direction": "OUTGOING", "amount_minor": json.Number("250000"), "currency": "NGN", "age_minutes": json.Number("150"),
				"beneficiary": "ben_0001"}},
		},
		"utterances": []any{map[string]any{"turn": json.Number("1"), "text": text}},
	}
}

// g1Inputs is a SYNTHETIC inputs document of BO's shape with its digest.
func g1Inputs(t *testing.T, mutate func(doc map[string]any)) []byte {
	t.Helper()
	doc := map[string]any{
		"label": "SYNTHETIC generated cases, inputs only", "suite": map[string]any{"generator": "noema-casegen", "generator_version": "1.3.3", "seed": json.Number("20261020"),
			"manifest_sha256": "sha256:" + strings.Repeat("a", 64), "tree_sha256": "sha256:" + strings.Repeat("b", 64)},
		"cases": []any{
			g1Case("BEN-CLEAN-0001", "usr_synthnine", "  Wetin happen to the N2,500 wey I send Chidi dis morning?\n", "pcm"),
			g1Case("BEN-CLEAN-0027", "usr_synthtwo", "Abeg where is the 30k wey I send Amina dis morning?", "pcm", "en-NG"),
		},
	}
	if mutate != nil {
		mutate(doc)
	}
	doc["inputs_sha256"] = inputsDigest(t, doc)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type fixtureDoc struct {
	Label        string `json:"label"`
	InputsSHA256 string `json:"inputs_sha256"`
	PackRef      string `json:"pack_ref"`
	Cases        []struct {
		CaseID         string         `json:"case_id"`
		CanonicalInput map[string]any `json:"canonical_input"`
	} `json:"cases"`
}

func decodeFixture(t *testing.T, raw []byte) fixtureDoc {
	t.Helper()
	var f fixtureDoc
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// Each input becomes what the port builds from a verified session and a turn: the ten members of canonical-input.v2 and nothing else, in particular no locale hint (A460), the principal of the case,
// the text trimmed (the port's rule), the goldens' instant.
func TestEachG1InputBecomesWhatThePortBuildsFromTheVerifiedSessionAndTheTurn(t *testing.T) {
	raw, err := buildG1Fixture(t, g1Inputs(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFixture(t, raw)
	if len(f.Cases) != 2 || f.Cases[0].CaseID != "BEN-CLEAN-0001" || f.Cases[1].CaseID != "BEN-CLEAN-0027" {
		t.Fatalf("cases %+v", f.Cases)
	}
	want := []map[string]any{
		{"principal_ref": "usr_synthnine", "text": "Wetin happen to the N2,500 wey I send Chidi dis morning?"},
		{"principal_ref": "usr_synthtwo", "text": "Abeg where is the 30k wey I send Amina dis morning?"},
	}
	for i, c := range f.Cases {
		ci := c.CanonicalInput
		if len(ci) != 10 {
			t.Errorf("%s: canonical input has %d members, want the ten of canonical-input.v2: %v", c.CaseID, len(ci), ci)
		}
		for _, k := range []string{"locale_hint", "locale", "locale_mix", "candidates", "expected"} {
			if _, ok := ci[k]; ok {
				t.Errorf("%s: canonical input carries %q, which the port does not build", c.CaseID, k)
			}
		}
		fixed := map[string]any{"schema_version": "2.0", "tenant_id": "ten_synthetic", "session_id": "ses_synthetic", "channel": "text", "mode": "MODE_1", "received_at": goldenInstant,
			"principal_ref": want[i]["principal_ref"], "text": want[i]["text"]}
		for k, v := range fixed {
			if ci[k] != v {
				t.Errorf("%s: %s = %v, want %v", c.CaseID, k, ci[k], v)
			}
		}
	}
}

// The ids are derived from the case and are the same every time (INV-21): a lower-case UUID that is not all digits (so no id holds a digit run, AGENT-13) and a correlation id of twenty letters.
func TestTheRequestAndCorrelationIDsAreDerivedFromTheCaseAndAreStable(t *testing.T) {
	a, err := buildG1Fixture(t, g1Inputs(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildG1Fixture(t, g1Inputs(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two builds of the same inputs differ")
	}
	f := decodeFixture(t, a)
	seen := map[string]bool{}
	for _, c := range f.Cases {
		rid, _ := c.CanonicalInput["request_id"].(string)
		corr, _ := c.CanonicalInput["correlation_id"].(string)
		if !uuidShape.MatchString(rid) || allDigits.MatchString(rid) {
			t.Errorf("%s: request id %q is not a lower-case UUID with a letter", c.CaseID, rid)
		}
		if !corrLetter.MatchString(corr) {
			t.Errorf("%s: correlation id %q is not corr_ and twenty letters", c.CaseID, corr)
		}
		if seen[rid] || seen[corr] {
			t.Errorf("%s: an id repeats across cases", c.CaseID)
		}
		seen[rid], seen[corr] = true, true
	}
}

// A460: the locale is en-NG for all five cases (what the UI sends); the file says so as canonical_locale, locale_mix stays a tag. Anything else is refused: null (the mix has no canonical locale),
// another locale, a missing member.
func TestAnyCanonicalLocaleButEnNGIsRefused(t *testing.T) {
	for name, set := range map[string]func(c map[string]any){
		"another locale": func(c map[string]any) { c["canonical_locale"] = "pcm" },
		"null":           func(c map[string]any) { c["canonical_locale"] = nil },
		"missing":        func(c map[string]any) { delete(c, "canonical_locale") },
	} {
		t.Run(name, func(t *testing.T) {
			in := g1Inputs(t, func(doc map[string]any) { set(doc["cases"].([]any)[1].(map[string]any)) })
			if raw, err := buildG1Fixture(t, in); err == nil {
				t.Fatalf("accepted: %s", raw)
			}
		})
	}
}

// The inputs are trusted for nothing they do not prove (INV-22): a digest that does not match, a key beyond BO's shape (an expectation, an oracle reference), a missing principal, a case with more than
// one utterance, an empty text. None builds anything, and no error carries a piece of the file (AGENT-13).
func TestAnInputsFileThatIsNotBOsFileIsRefusedAndTheErrorCarriesNoText(t *testing.T) {
	good := g1Inputs(t, nil)
	badDigest := bytes.Replace(good, []byte(`"label": "SYNTHETIC`), []byte(`"label": "SYNTHETIC EDITED`), 1)
	cases := map[string][]byte{
		"digest":          badDigest,
		"expectation key": g1Inputs(t, func(doc map[string]any) { doc["cases"].([]any)[0].(map[string]any)["expected"] = "x" }),
		"oracle key":      g1Inputs(t, func(doc map[string]any) { doc["oracle_ref"] = "x" }),
		"no principal":    g1Inputs(t, func(doc map[string]any) { delete(doc["cases"].([]any)[0].(map[string]any), "principal") }),
		"two utterances": g1Inputs(t, func(doc map[string]any) {
			c := doc["cases"].([]any)[0].(map[string]any)
			c["utterances"] = append(c["utterances"].([]any), map[string]any{"turn": json.Number("2"), "text": "again"})
		}),
		"empty text": g1Inputs(t, func(doc map[string]any) {
			doc["cases"].([]any)[0].(map[string]any)["utterances"] = []any{map[string]any{"turn": json.Number("1"), "text": "  "}}
		}),
		"not json": []byte("not json"),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := buildG1Fixture(t, in)
			if err == nil {
				t.Fatalf("accepted: %s", raw)
			}
			for _, leak := range []string{"Wetin", "Chidi", "Amina", "usr_synth"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("the error carries %q: %v", leak, err)
				}
			}
		})
	}
}

// The fixture is pinned to the inputs it was built from: BO's digest, verbatim, and the pack reference the port was configured with. It is sorted, indented and ends with a newline, so a regeneration
// is a byte-for-byte comparison.
func TestTheFixtureIsPinnedToBOsDigestAndIsByteStable(t *testing.T) {
	in := g1Inputs(t, nil)
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(in))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	raw, err := buildG1Fixture(t, in)
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFixture(t, raw)
	if f.InputsSHA256 != doc["inputs_sha256"] || f.PackRef != packRef || !strings.HasPrefix(f.Label, "SYNTHETIC") {
		t.Errorf("fixture %q %q %q", f.InputsSHA256, f.PackRef, f.Label)
	}
	if !bytes.HasSuffix(raw, []byte("}\n")) {
		t.Error("the fixture does not end with a closing brace and a newline")
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	var again bytes.Buffer
	enc := json.NewEncoder(&again)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(back); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again.Bytes(), raw) {
		t.Error("the fixture is not in the canonical form (sorted keys, two-space indent)")
	}
}
