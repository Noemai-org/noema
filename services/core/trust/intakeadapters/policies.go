package intakeadapters

import (
	"context"
	"fmt"
	"math"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/policy"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

// waitParam is the rule parameter that gives a WAIT its delay in seconds (PERD 23.4, params: `wait_seconds`).
const waitParam = "wait_seconds"

// PolicyBook is the policy documents the pack carries, with the vocabulary they were checked against. Policy is data (PERD 23.4): the book holds no rule of its own.
type PolicyBook struct {
	policyID string
	versions map[string]policy.Version
	scope    predicate.Scope
	needs    map[string]FactNeed // target to what the rules that apply to it read
}

// PolicyID is the policy's ID, for example fin.pol.
func (b *PolicyBook) PolicyID() string { return b.policyID }

// need is what the rules of every version in the book that apply to the target read.
func (b *PolicyBook) need(target string) FactNeed { return b.needs[target] }

// Publications is the tenant's published versions of one policy, oldest first as the database orders them (the adapter never orders versions itself).
type Publications interface {
	Published(ctx context.Context, tenantID, policyID string) ([]policystore.Publication, error)
}

// TenantQueriers gives the database login of one tenant: the tenant's own Trust login, and no other.
type TenantQueriers interface {
	For(tenantID string) (policystore.Querier, error)
}

// StorePublications is Publications over the publication store, read as the tenant's own Trust login. The composition root passes the per-tenant pools it already holds for Trust
// (the same login files as the Record reader's, with the tenant's trust_rw login).
type StorePublications struct{ DBs TenantQueriers }

func (s StorePublications) configured() bool { return s.DBs != nil }

// Published reads the tenant's publications of the policy. The database binds the tenant to the login, so a pool that is another tenant's shows nothing.
func (s StorePublications) Published(ctx context.Context, tenantID, policyID string) ([]policystore.Publication, error) {
	if !s.configured() {
		return nil, ErrConfig
	}
	q, err := s.DBs.For(tenantID)
	if err != nil {
		return nil, fail(nil, "the tenant's database login could not be had", err)
	}
	if q == nil {
		return nil, ErrConfig
	}
	return policystore.Published(ctx, q, tenantID, policyID)
}

// Admissions gives the time an intent version was admitted, in Unix seconds (AT_ADMISSION rules, INV-09). There is no production implementation yet: the admitted-intent store
// holds a creation time and the intent contract has no admitted_at (see status/TRU-adapters-policies-clocks.md).
type Admissions interface {
	AdmittedAt(ctx context.Context, tenantID, intentID string, version int) (int64, error)
}

// PoliciesConfig is what NewPolicies needs. Every part is required; there are no defaults.
type PoliciesConfig struct {
	// Book is the pack's policy documents.
	Book *PolicyBook
	// Publications is the tenant's publication history; StorePublications over the per-tenant Trust logins.
	Publications Publications
	// Admissions is the intent store's admission time.
	Admissions Admissions
	// Facts is the source of the facts the rules' conditions read.
	Facts FactSource
}

// Policies implements intake.Policies: the tenant's active policy set, evaluated for one request and one capability (A306).
type Policies struct{ c PoliciesConfig }

var _ intake.Policies = (*Policies)(nil)

// NewPolicies builds the adapter. A missing part, or a publication store with no logins, is ErrConfig.
func NewPolicies(c PoliciesConfig) (*Policies, error) {
	if c.Book == nil || c.Publications == nil || c.Admissions == nil || c.Facts == nil {
		return nil, ErrConfig
	}
	if s, ok := c.Publications.(interface{ configured() bool }); ok && !s.configured() {
		return nil, ErrConfig
	}
	return &Policies{c: c}, nil
}

// Evaluate answers for the one capability of the query, as the tenant (A306). The rules are those of the policy version in force at the query's reading (the newest one published
// at or before it); an AT_ADMISSION rule is read from the version in force when the intent was admitted (D8, INV-09). The target of the evaluation is the capability, or the intent
// node when the query names none. Every rule that matched is returned with its policy and version (INV-09), and the policy versions used (REQ-TRU-007). No rule that permits is REJECT
// NO_PERMITTING_RULE as the engine says it. Anything that cannot be evaluated is an error and no answer: a rule that errors (policy.ErrCondition), a published version with no
// document, a history that cannot be put in order, facts that are missing or of the wrong type, a WAIT with no usable delay.
func (p *Policies) Evaluate(ctx context.Context, q intake.PolicyQuery) (intake.PolicyEvaluation, error) {
	if p == nil || p.c.Book == nil {
		return intake.PolicyEvaluation{}, ErrConfig
	}
	target := q.CapabilityID
	if target == "" {
		target = q.IntentNode
	}
	if q.TenantID == "" || q.IntentID == "" || q.IntentVersion < 1 || q.Now < 1 || target == "" {
		return intake.PolicyEvaluation{}, fmt.Errorf("%w: a tenant, an intent, a version, a reading and a capability or a node are required", ErrQuery)
	}
	admittedAt, err := p.c.Admissions.AdmittedAt(ctx, q.TenantID, q.IntentID, q.IntentVersion)
	if err != nil {
		return intake.PolicyEvaluation{}, fail(nil, "the admission time could not be read", err)
	}
	if admittedAt < 1 || admittedAt > q.Now {
		return intake.PolicyEvaluation{}, fmt.Errorf("%w: the intent's admission time is not before the reading", ErrQuery)
	}
	published, err := p.c.Publications.Published(ctx, q.TenantID, p.c.Book.policyID)
	if err != nil {
		return intake.PolicyEvaluation{}, fail(nil, "the tenant's publications could not be read", err)
	}
	set, err := p.c.Book.inForce(published, q.Now, admittedAt)
	if err != nil {
		return intake.PolicyEvaluation{}, err
	}
	need := p.c.Book.need(target)
	given, err := p.c.Facts.PolicyFacts(ctx, q, need)
	if err != nil {
		return intake.PolicyEvaluation{}, fail(nil, "the facts could not be read", err)
	}
	env, err := checkFacts(given, need, p.c.Book.scope)
	if err != nil {
		return intake.PolicyEvaluation{}, err
	}
	res, err := set.Evaluate(p.c.Book.policyID, policy.Request{Target: target, Now: q.Now, AdmittedAt: admittedAt, Env: env})
	if err != nil {
		return intake.PolicyEvaluation{}, fail(nil, "the policy could not be evaluated", err)
	}
	return answer(res, q.Now)
}

// inForce builds the policy set for one reading: the versions published at or before it, the newest of them ACTIVE and the others RETIRED, each effective from its publication. The
// database's order of versions is the order of the list, and the times must follow it, or the history cannot be put in order (INV-21). The newest version, and the one in force
// at admission, must have a document; an older version nobody needs does not.
func (b *PolicyBook) inForce(published []policystore.Publication, now, admittedAt int64) (policy.Set, error) {
	var visible []policystore.Publication
	for _, pub := range published {
		if pub.PublishedAt <= now {
			visible = append(visible, pub)
		}
	}
	for i := 1; i < len(visible); i++ {
		if visible[i].PublishedAt <= visible[i-1].PublishedAt {
			return policy.Set{}, fail(ErrPublications, "a version is not published after the one before it", fmt.Errorf("%s is not published after %s", visible[i].Version, visible[i-1].Version))
		}
	}
	if len(visible) == 0 {
		return policy.NewSet()
	}
	newest, atAdmission := len(visible)-1, -1
	for i, pub := range visible {
		if pub.PublishedAt <= admittedAt {
			atAdmission = i
		}
	}
	versions := make([]policy.Version, 0, len(visible))
	for i, pub := range visible {
		doc, ok := b.versions[pub.Version]
		if !ok {
			if i == newest || i == atAdmission {
				return policy.Set{}, fail(ErrNoDocument, "", fmt.Errorf("%s@%s", b.policyID, pub.Version))
			}
			continue
		}
		status := policy.Retired
		if i == newest {
			status = policy.Active
		}
		v, err := doc.WithMeta(policy.Meta{Status: status, EffectiveFrom: pub.PublishedAt, PublishedAt: pub.PublishedAt})
		if err != nil {
			return policy.Set{}, fail(ErrPublications, "a published version cannot be put in force", err)
		}
		versions = append(versions, v)
	}
	set, err := policy.NewSet(versions...)
	if err != nil {
		return policy.Set{}, fail(ErrPublications, "the published versions cannot be put in force together", err)
	}
	return set, nil
}

// answer turns the engine's result into the port's: every rule that matched with its policy and version, the versions used, and for a WAIT the time it is re-evaluated.
func answer(res policy.Result, now int64) (intake.PolicyEvaluation, error) {
	out := intake.PolicyEvaluation{Effect: res.Effect, ReasonCode: res.Reason, PermitMatched: res.PermitMatched}
	for _, a := range res.Applied {
		out.Rules = append(out.Rules, decide.PolicyRuleVersion{RuleID: a.RuleID, PolicyID: a.PolicyID, Version: a.Version})
	}
	for _, v := range res.Versions {
		out.Versions = append(out.Versions, decide.PolicyVersionRef{PolicyID: v.PolicyID, Version: v.Version})
	}
	if res.Effect == policy.Wait {
		at, err := reevaluateAt(res.Winner, now)
		if err != nil {
			return intake.PolicyEvaluation{}, err
		}
		out.ReevaluateAt = at
	}
	return out, nil
}

// reevaluateAt is the reading plus the winning WAIT rule's wait_seconds. A rule without it, with it as anything but a whole number of seconds of at least one, or with one that
// overflows the clock, gives no time: nothing is guessed.
func reevaluateAt(winner *policy.Applied, now int64) (int64, error) {
	if winner == nil {
		return 0, fmt.Errorf("%w: a WAIT with no winning rule", ErrRule)
	}
	w, ok := winner.Params[waitParam]
	if !ok || w.Kind != policy.ParamInteger || w.Int < 1 || w.Int > math.MaxInt64-now {
		return 0, fail(ErrRule, "the winning rule needs a whole number of seconds from one that fits the clock", fmt.Errorf("rule %s needs %s", winner.RuleID, waitParam))
	}
	return now + w.Int, nil
}
