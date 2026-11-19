package policy

import (
	"bytes"
	"encoding/json"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// DocumentHashLabel is the domain tag of a policy document's hash: the bytes hashed are this label, a 0x00 byte and the RFC 8785 form of the document, so that no other SHA-256 in
// Noema (a payload hash, a signature message) can equal it.
const DocumentHashLabel = "noema/policy-document/v1"

// DocumentHash is the hash of a policy document, written "sha256:" and 64 lower-case hex digits (TRU-02; REQ-TRU-005, INV-21). It is the hash of the document and not of its spelling: the
// document is put in RFC 8785 form first, so key order and whitespace do not change it, and any change of a value, a member or the order of an array does. A publication keeps this hash in
// place of the document (the document store is DEBT-23); the command that publishes and the loader that reads the document again both call this function on the same bytes (the value of the
// pack's "policy" member), and the loader refuses a document whose hash is not the published one.
//
// A document with no single reading has no hash: a repeated member name at any depth, data after the document, something that is not a JSON object, or a number that is not an integer
// within 2^53-1. The error is ErrInvalidPolicy and never carries the document's text.
func DocumentHash(doc []byte) (string, error) {
	if err := noDuplicateMembers(doc); err != nil {
		return "", invalid("the document has no single reading")
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return "", invalid("the document is not a JSON object")
	}
	canon, err := crypto.CanonicalJSON(obj)
	if err != nil {
		return "", invalid("the document cannot be put in canonical form")
	}
	return crypto.Hash(crypto.SigningMessage(DocumentHashLabel, canon)), nil
}
