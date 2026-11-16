package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// ErrBinderNotWired is returned when a source the binder needs has not been plugged in. It is an error, never an empty answer: Execution treats it as "could not
// read", which voids or refuses the grant (fail closed).
var ErrBinderNotWired = errors.New(errPrefix + "the binder is missing a source")

// IntentSource reads the hash of the latest admitted intent of a grant's investigation (E3). The intent store is built by another task and plugged in by cmd/core.
type IntentSource interface {
	LatestIntentHash(ctx context.Context, g controller.Grant) (string, error)
}

// PlanSource reads the stored plan step a grant names (E5). The plan store is built by another task and plugged in by cmd/core.
type PlanSource interface {
	Plan(ctx context.Context, g controller.Grant) (controller.PlanStep, error)
}

// StoreBinder is the production controller.Binder (TRU-08, DEBT-23). Its database half is here: as the tenant's own Execution login (the tenant is the login's, not
// an argument, DEBT-04) it reads the active policy version from the publication store and whether the grant's decision is still an ALLOW under it. The intent
// hash and the plan come from their sources; the kill switch from the Ledger. A grant that names another tenant finds nothing, which reads as "no".
//
// It reads policy_publication and decision_record directly and does not import trust/policystore (Execution never imports Trust's internals; the e2e test proves the
// two agree). The version order is the database's (version_key), never compared in code.
type StoreBinder struct {
	Pool    Queryer
	Ledger  *Ledger
	Intents IntentSource
	Plans   PlanSource
}

// Queryer is the one read the binder makes of Postgres; a pgx pool or transaction satisfies it.
type Queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func needDeadlineCtx(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New(errPrefix + "the context needs a deadline")
	}
	return nil
}

// ActivePolicyVersion is policy_id@version of the grant's own policy as it is active now (the highest published), or "" when it has none or the grant's policy
// version has no "@". Only the grant's own policy_id is looked up: versions are never compared across policy IDs (ruling of 5 Nov 2026).
func (b *StoreBinder) ActivePolicyVersion(ctx context.Context, g controller.Grant) (string, error) {
	if err := needDeadlineCtx(ctx); err != nil {
		return "", err
	}
	if b == nil || b.Pool == nil {
		return "", ErrBinderNotWired
	}
	id, _, ok := strings.Cut(g.PolicyVersion, "@")
	if !ok || id == "" {
		return "", nil
	}
	var v string
	err := b.Pool.QueryRow(ctx, `SELECT version FROM policy_publication WHERE tenant_id = $1 AND policy_id = $2 ORDER BY version_key DESC LIMIT 1`, g.TenantID, id).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%sread the active policy version: %w", errPrefix, err)
	}
	return id + "@" + v, nil
}

// DecisionStillAllowed is true only when the grant's decision is recorded for its tenant as an ALLOW and the grant's policy version is the active one (E4). A decision
// that is not in the Record, a policy with no publication and another active version all answer false with no error.
func (b *StoreBinder) DecisionStillAllowed(ctx context.Context, g controller.Grant) (bool, error) {
	if err := needDeadlineCtx(ctx); err != nil {
		return false, err
	}
	if b == nil || b.Pool == nil {
		return false, ErrBinderNotWired
	}
	var outcome string
	err := b.Pool.QueryRow(ctx, `SELECT outcome FROM decision_record WHERE decision_id = $1 AND tenant_id = $2`, g.DecisionID, g.TenantID).Scan(&outcome)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%sread the decision: %w", errPrefix, err)
	}
	if outcome != "ALLOW" {
		return false, nil
	}
	active, err := b.ActivePolicyVersion(ctx, g)
	if err != nil {
		return false, err
	}
	return active != "" && active == g.PolicyVersion, nil
}

// LatestIntentHash reads through the intent source (E3).
func (b *StoreBinder) LatestIntentHash(ctx context.Context, g controller.Grant) (string, error) {
	if b == nil || b.Intents == nil {
		return "", ErrBinderNotWired
	}
	return b.Intents.LatestIntentHash(ctx, g)
}

// Plan reads through the plan source (E5).
func (b *StoreBinder) Plan(ctx context.Context, g controller.Grant) (controller.PlanStep, error) {
	if b == nil || b.Plans == nil {
		return controller.PlanStep{}, ErrBinderNotWired
	}
	return b.Plans.Plan(ctx, g)
}

// WritesEnabled reads the tenant kill switch through the Ledger (E6); an error means "could not read", which the controller treats as disabled.
func (b *StoreBinder) WritesEnabled(ctx context.Context, tenantID string) (bool, error) {
	if b == nil || b.Ledger == nil {
		return false, ErrBinderNotWired
	}
	return b.Ledger.WritesEnabled(ctx, tenantID)
}
