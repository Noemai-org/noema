package trustport

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// The production adapter of intake.Writes (STM-01 follow-up; T3 of PERD 23.3; REQ-TRU-002, REQ-NFR-002; INV-01, INV-14). T3 asks two questions: does the request lead to a write
// (NeedsWrite), and are writes enabled for the tenant (Enabled). The first is the pack's answer for the intent node, read as data through a WriteRule: core names no industry and no
// intent (INV-14). The second is the tenant's kill switch. Neither question has a default: no answer is an error, so T3 ends UNKNOWN (REQ-NFR-002), never a read and never a write.

// ErrNoWriteAnswer is what a WriteRule returns for an intent node it has no answer for: an unknown node, an unloaded pack, or a node the pack does not classify.
var ErrNoWriteAnswer = errors.New("trustport: the pack gives no write answer for this intent node")

// WriteRule answers, per intent node, whether the node may lead to a write. It is the pack's data and nothing else. For a node it has no answer for it returns an error that wraps
// ErrNoWriteAnswer, never false. An answer returned with an error is not used.
type WriteRule interface {
	MayWrite(intentNode string) (bool, error)
}

// WritesSwitch answers whether writes are enabled for a tenant. *KillSwitch is one.
type WritesSwitch interface {
	Enabled(ctx context.Context, tenantID string) (bool, error)
}

// Writes implements intake.Writes: Enabled is the switch's answer and NeedsWrite is the rule's. Build it with NewWrites.
type Writes struct {
	sw   WritesSwitch
	rule WriteRule
}

var _ intake.Writes = (*Writes)(nil)

// NewWrites builds the adapter. What cmd/core must pass:
//
//   - sw: the *KillSwitch of NewKillSwitch (the tenant's kill switch, read as the tenant's own trust_rw login; it is built only with the pack loaded).
//   - rule: a WriteRule over the pack the process loaded, the same pack the other Trust adapters were built from. No such rule exists yet: the pack carries no field that classifies
//     an intent node as read or write (status/STM-01-writes-adapter.md). Until it does, cmd/core cannot build this adapter, and the intake is not composed.
//
// A missing part is ErrConfig.
func NewWrites(sw WritesSwitch, rule WriteRule) (*Writes, error) {
	if sw == nil || rule == nil {
		return nil, ErrConfig
	}
	return &Writes{sw: sw, rule: rule}, nil
}

func (w *Writes) usable() error {
	if w == nil || w.sw == nil || w.rule == nil {
		return ErrConfig
	}
	return nil
}

// NeedsWrite is the rule's answer for the node of the intent. An intent with no node, a node the rule has no answer for, a rule that fails and a context that is done are all false
// with an error: the intake takes the answer for the Planner's WriteRequired, so a guess either way would shape the plan.
func (w *Writes) NeedsWrite(ctx context.Context, a intake.AdmittedIntent) (bool, error) {
	if err := w.usable(); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if a.IntentNode == "" {
		return false, fmt.Errorf("%w: the intent names no node", ErrStoredIntent)
	}
	may, err := w.rule.MayWrite(a.IntentNode)
	if err != nil {
		return false, fmt.Errorf("trustport: the write rule: %w", err)
	}
	return may, nil
}

// Enabled is true only when the switch says so without an error: an error is false, whatever else the switch returned with it.
func (w *Writes) Enabled(ctx context.Context, tenantID string) (bool, error) {
	if err := w.usable(); err != nil {
		return false, err
	}
	on, err := w.sw.Enabled(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return on, nil
}
