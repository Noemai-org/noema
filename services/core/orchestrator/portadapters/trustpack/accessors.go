package trustpack

import (
	"errors"

	"github.com/Noemai-org/noema/services/core/execution/registry"
)

// ErrNotBuilt is the answer of an accessor in the red commit: it is removed by the green one.
var ErrNotBuilt = errors.New("trustpack: not built")

// ConfirmationRequirements is the red stub.
func (p *Pack) ConfirmationRequirements() (map[string]string, error) { return nil, ErrNotBuilt }

// Registry is the red stub.
func (p *Pack) Registry() (*registry.Registry, error) { return nil, ErrNotBuilt }
