// Package verifier is a canary: it reuses the evidence layer's snapshot instead of reading for itself.
package verifier

import "github.com/Noemai-org/noema/services/core/evidence/layer"

// Reuse reads Trust's evidence, which the verifier must never do.
func Reuse() layer.Snapshot { return layer.Snapshot{} }
