package mapping

import (
	"errors"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// ScopeFromDescriptors is not built yet (UND-04, A438; the red head of the move out of cmd/core).
func ScopeFromDescriptors(pack []byte) (predicate.Scope, error) {
	return predicate.Scope{}, errors.New("not built")
}
