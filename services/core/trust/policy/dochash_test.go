package policy_test

// TRU-02 (REQ-TRU-004, REQ-TRU-005; INV-09, INV-21; ruling on noema#771, interim): the hash of a policy document. The hash is what a publication row keeps in place of the document
// (the store of documents is DEBT-23): a loader that reads the document again compares its hash with the published one. So the hash must be of the document and not of its spelling: the same
// for any key order and any whitespace, different for any change of a value, and domain-separated from every other SHA-256 in Noema. The golden value below is computed here, from the
// RFC 8785 text written out by hand, and shares no code with the package. All data is SYNTHETIC.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/policy"
)

var hashForm = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func golden(canonical string) string {
	sum := sha256.Sum256([]byte("noema/policy-document/v1\x00" + canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// The hash is SHA-256 over the domain tag, a 0x00 byte and the RFC 8785 form of the document (written out here by hand: keys sorted, no whitespace).
func TestTheDocumentHashIsTheDomainTaggedHashOfTheCanonicalForm(t *testing.T) {
	doc := ` { "version" : "1.0", "rules": [ {"b": 2, "a": "é"} ], "policy_id": "p" } `
	got, err := policy.DocumentHash([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if want := golden(`{"policy_id":"p","rules":[{"a":"é","b":2}],"version":"1.0"}`); got != want {
		t.Fatalf("hash %s, want %s", got, want)
	}
	if !hashForm.MatchString(got) {
		t.Fatalf("hash %q is not sha256: and 64 lower-case hex digits", got)
	}
	if policy.DocumentHashLabel != "noema/policy-document/v1" {
		t.Fatalf("the label is %q", policy.DocumentHashLabel)
	}
	plain := sha256.Sum256([]byte(`{"policy_id":"p","rules":[{"a":"é","b":2}],"version":"1.0"}`))
	if got == "sha256:"+hex.EncodeToString(plain[:]) {
		t.Fatal("the hash carries no domain tag")
	}
}

// The hash does not depend on key order or whitespace, at any depth.
func TestTheDocumentHashDoesNotDependOnKeyOrderOrWhitespace(t *testing.T) {
	a := `{"policy_id":"p","version":"1.0","rules":[{"rule_id":"r","params":{"x":"1","y":"2"},"effect":"PERMIT"}]}`
	b := "{\n  \"rules\": [ { \"effect\": \"PERMIT\",\n \"params\": { \"y\": \"2\", \"x\": \"1\" },\t\"rule_id\": \"r\" } ],\n  \"version\": \"1.0\", \"policy_id\": \"p\"\n}\n"
	ha, err := policy.DocumentHash([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	hb, err := policy.DocumentHash([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Fatalf("one document spelled two ways has two hashes: %s and %s", ha, hb)
	}
}

// Any change of a value, a member, or the order of an array changes the hash.
func TestAnyChangeOfTheDocumentChangesTheHash(t *testing.T) {
	base := `{"policy_id":"p","version":"1.0","rules":[{"rule_id":"a","reason_code":"X"},{"rule_id":"b","reason_code":"Y"}]}`
	h0, err := policy.DocumentHash([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"a reason code":   strings.Replace(base, `"X"`, `"Z"`, 1),
		"the version":     strings.Replace(base, `"1.0"`, `"1.1"`, 1),
		"an extra member": strings.Replace(base, `"version"`, `"extra":true,"version"`, 1),
		"the rule order":  `{"policy_id":"p","version":"1.0","rules":[{"rule_id":"b","reason_code":"Y"},{"rule_id":"a","reason_code":"X"}]}`,
		"a dropped rule":  `{"policy_id":"p","version":"1.0","rules":[{"rule_id":"a","reason_code":"X"}]}`,
	} {
		h, err := policy.DocumentHash([]byte(doc))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if h == h0 {
			t.Errorf("%s: the hash did not change", name)
		}
	}
}

// A document the loader would refuse has no hash: a duplicate member (two readings), a document that is not JSON, one that is not an object, a fractional number.
func TestADocumentWithNoSingleReadingHasNoHash(t *testing.T) {
	for name, doc := range map[string]string{
		"a duplicate member":        `{"policy_id":"p","policy_id":"q","version":"1.0"}`,
		"a nested duplicate member": `{"policy_id":"p","rules":[{"a":1,"a":2}]}`,
		"not JSON":                  `policy_id: p`,
		"empty":                     ``,
		"trailing data":             `{"policy_id":"p"} {"x":1}`,
		"not an object":             `["policy_id"]`,
		"a fractional number":       `{"policy_id":"p","x":1.5}`,
	} {
		h, err := policy.DocumentHash([]byte(doc))
		if err == nil || h != "" {
			t.Errorf("%s: hash %q, err %v; want no hash and an error", name, h, err)
		}
		if err != nil && !errors.Is(err, policy.ErrInvalidPolicy) {
			t.Errorf("%s: err %v, want ErrInvalidPolicy", name, err)
		}
	}
}
