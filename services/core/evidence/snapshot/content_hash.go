package snapshot

import "errors"

// ContentItem is one fact of a snapshot as its content, without the identifier the evidence layer minted for the read: where it came from, which record there, and the hash of what
// was read.
type ContentItem struct {
	Source         string
	SourceRecordID string
	PayloadHash    string
}

// ErrContentHashNotBuilt is the answer of the stub that precedes the function (RED).
var ErrContentHashNotBuilt = errors.New("snapshot: the content hash is not built")

// ContentHash is not built yet.
func ContentHash(items []ContentItem) (string, error) { return "", ErrContentHashNotBuilt }
