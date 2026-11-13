package intentstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// ErrNotBound is returned by Save for an Admitted whose document, hash and row do not describe one another. Nothing is appended for it.
var ErrNotBound = errors.New(prefix + "the document, its hash and its row do not match")

const statusAdmitted = "VALIDATED" // what the gate admits: the document says so itself

func notBound(why string) error { return fmt.Errorf("%w: %s", ErrNotBound, why) }

// verify binds what Save writes to itself, before anything is appended: the document is the canonical (RFC 8785) form, its sha-256 is the hash recorded in
// INTENT_ADMITTED, and the identity, version, supersedes, correlation, node and status the row carries are the document's own (REQ-AUD-001, INV-17). The messages name
// the member that disagrees and never quote the document.
func verify(a Admitted) error {
	dec := json.NewDecoder(bytes.NewReader(a.JSON))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return notBound("the document is not a JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return notBound("the document has data after the object")
	}
	canon, err := crypto.CanonicalJSON(doc)
	if err != nil {
		return notBound("the document has no canonical form")
	}
	if !bytes.Equal(canon, a.JSON) {
		return notBound("the document is not in canonical form")
	}
	sum := sha256.Sum256(a.JSON)
	if a.IntentHash != "sha256:"+hex.EncodeToString(sum[:]) {
		return notBound("the hash is not the document's")
	}
	str := func(k string) (string, bool) { s, ok := doc[k].(string); return s, ok }
	for _, f := range []struct{ member, want string }{
		{"tenant_id", a.TenantID}, {"intent_id", a.IntentID}, {"correlation_id", a.CorrelationID}, {"intent_node", a.IntentNode}, {"status", statusAdmitted},
	} {
		if got, ok := str(f.member); !ok || got != f.want {
			return notBound(f.member + " differs")
		}
	}
	if n, ok := doc["version"].(json.Number); !ok || n.String() != fmt.Sprint(a.Version) {
		return notBound("version differs")
	}
	switch sup := doc["supersedes"].(type) {
	case nil:
		if a.Supersedes != "" {
			return notBound("supersedes differs")
		}
	case string:
		if sup != a.Supersedes {
			return notBound("supersedes differs")
		}
	default:
		return notBound("supersedes differs")
	}
	return nil
}
