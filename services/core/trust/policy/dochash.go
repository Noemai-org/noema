package policy

import "errors"

// DocumentHashLabel is the domain tag of a policy document's hash (TRU-02, the document_hash of a publication).
const DocumentHashLabel = "noema/policy-document/v1"

// ErrNotBuilt is the answer of a function that is not built yet.
var ErrNotBuilt = errors.New("policy: not built")

// DocumentHash is not built yet.
func DocumentHash(doc []byte) (string, error) { return "", ErrNotBuilt }
