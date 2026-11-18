package intakeadapters

import (
	"context"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/policy"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

// PolicyBook is the policy documents the pack carries, with the vocabulary they were checked against. Policy is data (PERD 23.4): the book holds no rule of its own.
type PolicyBook struct {
	policyID string
	versions map[string]policy.Version
	scope    predicate.Scope
	needs    map[string]FactNeed // target to what the rules that apply to it read
}

// LoadPolicyBook loads the policy of each pack file given (the current pack, and the older pack versions whose policy a tenant may still have been admitted under).
func LoadPolicyBook(packs ...[]byte) (*PolicyBook, error) { return nil, ErrNotBuilt }

// PolicyID is the policy's ID, for example fin.pol.
func (b *PolicyBook) PolicyID() string { return "" }

// need is what the rules of every version in the book that apply to the target read.
func (b *PolicyBook) need(target string) FactNeed { return FactNeed{} }

// Publications is the tenant's published versions of one policy, oldest first as the database orders them.
type Publications interface {
	Published(ctx context.Context, tenantID, policyID string) ([]policystore.Publication, error)
}

// TenantQueriers gives the database login of one tenant.
type TenantQueriers interface {
	For(tenantID string) (policystore.Querier, error)
}

// StorePublications is Publications over the publication store, read as the tenant's own Trust login.
type StorePublications struct{ DBs TenantQueriers }

// Published reads the tenant's publications of the policy.
func (s StorePublications) Published(ctx context.Context, tenantID, policyID string) ([]policystore.Publication, error) {
	return nil, ErrNotBuilt
}

// Admissions gives the time an intent version was admitted, in Unix seconds (AT_ADMISSION rules, INV-09).
type Admissions interface {
	AdmittedAt(ctx context.Context, tenantID, intentID string, version int) (int64, error)
}

// PoliciesConfig is what NewPolicies needs. Every part is required; there are no defaults.
type PoliciesConfig struct {
	Book         *PolicyBook
	Publications Publications
	Admissions   Admissions
	Facts        FactSource
}

// Policies implements intake.Policies.
type Policies struct{ c PoliciesConfig }

var _ intake.Policies = (*Policies)(nil)

// NewPolicies builds the adapter.
func NewPolicies(c PoliciesConfig) (*Policies, error) { return nil, ErrNotBuilt }

// Evaluate answers for the one capability of the query.
func (p *Policies) Evaluate(ctx context.Context, q intake.PolicyQuery) (intake.PolicyEvaluation, error) {
	return intake.PolicyEvaluation{}, ErrNotBuilt
}
