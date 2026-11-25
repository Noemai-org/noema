package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	evidencereconciler "github.com/Noemai-org/noema/services/core/evidence/reconciler"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	ownershipport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/ownership"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

// compose_trustinputs.go builds the six Trust inputs of lane 2 of the G1 final composition (STM-01; A537, A469, A475, interim, relayed, not yet committed): Ownership, Writes, Evidence,
// Reconciler, Policies and Clocks. REQ-STM-001, REQ-TRU-001 to REQ-TRU-007, REQ-CTX-003, REQ-CTX-004; INV-11, INV-12, INV-14, INV-16, INV-19; AGENT-13, AGENT-16.
//
// Three are built: Ownership (the pack's ownership data only, one instance per served tenant), Evidence and Policies. Three are named gaps whatever is given, because what they need is
// not on main and is not invented (AGENT-20): Writes (its rule is a predicate over slot values, which the admitted intent does not carry), Reconciler (the pack carries neither the
// escalation reason of a classification nor the capabilities allowed under a freeze) and Clocks (no routing of a phase, no anchor-event mapping, and the trust limits are not pinned
// by the manifest). A component whose part is absent is a gap too: the field stays nil, and /readyz names it by its TrustAdapters field name. A value that is present and invalid is an
// error. Nothing is dialled, and no error carries a tenant, a path or a word of the pack.

// trustInputsIn is everything the builder takes: the verified pack (the trustpack.Pack and the bytes the manifest pinned), the per-tenant Trust logins, the Understanding parts main
// already builds (the rule set and the one evidence mapping), the payload source and its injection screen, the evidence read's list convention and the enterprise read. It reads
// no environment; the settings are the caller's. A part that is absent leaves the components that need it a named gap.
type trustInputsIn struct {
	Pack     *trustpack.Pack
	Verified packload.Pack
	Tenants  []string
	Pools    map[string]*pgxpool.Pool
	// Timeout is the bound of one read of the Trust logins: NOEMA_TRUST_DB_TIMEOUT_MS (EnvTrustDBTimeout), no default.
	Timeout time.Duration
	Clock   clock.Clock
	RuleSet *reconcile.RuleSet
	Mapping *mapping.Mapping
	// Payloads is the raw payload source (trustPayloads); Screen is the injection screen of the reader built over it.
	Payloads payloadread.PayloadReader
	Screen   ScreenDeps
	// ListEntities is the list entities the policies read and the item of the sealed snapshot that holds each (a source and a scope prefix). No committed ruling gives it, so
	// production leaves it empty and Policies is a gap until it is decided.
	ListEntities map[string]intakesources.ListSpec
	// OwnershipReader is the principal-scoped enterprise read (REQ-CTX-006). It is not built on main.
	OwnershipReader ownershipport.ScopedReader
}

// trustInputs is what the builder made. A nil port is a gap, named in Gaps by its TrustAdapters field name, in the order of that struct.
type trustInputs struct {
	Writes    intake.Writes
	Ownership intake.Ownership
	Evidence  intake.Evidence
	Policies  intake.Policies
	Gaps      []string
}

// inputsError is a failure whose text is fixed; the cause stays reachable with errors.Is and errors.As and is never in Error(), since a decoder's text can quote the pack (AGENT-13).
type inputsError struct {
	text  string
	cause error
}

func (e *inputsError) Error() string { return "cmd/core: " + e.text }
func (e *inputsError) Unwrap() error { return e.cause }

func inputsFail(text string, cause error) error { return &inputsError{text: text, cause: cause} }

// errNoOwnershipForTenant is the answer for a tenant that has no Ownership instance: an error, never "owned" and never "not owned" (INV-11, INV-19).
var errNoOwnershipForTenant = errors.New("cmd/core: no ownership adapter is built for this tenant")

// ownershipByTenant is one Ownership per served tenant (A320); a tenant that has none is refused, never served by another's (INV-11).
type ownershipByTenant map[string]ownershipport.Ownership

var _ intake.Ownership = ownershipByTenant(nil)

func (o ownershipByTenant) OwnsTarget(ctx context.Context, tenantID, principalID string, a intake.AdmittedIntent) (bool, error) {
	inst, ok := o[tenantID]
	if !ok {
		return false, errNoOwnershipForTenant
	}
	return inst.OwnsTarget(ctx, tenantID, principalID, a)
}

// The three login views over one set of per-tenant trust_rw pools. A tenant with no pool is an error and there is no fallback to another tenant's pool (INV-12).
type queryLogins struct{ p trustpg.Pools }

func (l queryLogins) For(tenantID string) (intakesources.Querier, error) {
	pool, err := l.p.For(tenantID)
	if err != nil {
		return nil, err
	}
	return pool, nil
}

type policyLogins struct{ p trustpg.Pools }

func (l policyLogins) For(tenantID string) (policystore.Querier, error) {
	pool, err := l.p.For(tenantID)
	if err != nil {
		return nil, err
	}
	return pool, nil
}

type txLogins struct{ p trustpg.Pools }

func (l txLogins) TxFor(tenantID string) (trustport.SnapshotTx, error) {
	pool, err := l.p.For(tenantID)
	if err != nil {
		return nil, err
	}
	return pool, nil
}

// buildTrustInputs builds what it can and names the rest (see the file comment). It returns an error only for a value that is present and invalid.
func buildTrustInputs(in trustInputsIn) (trustInputs, error) {
	var out trustInputs
	var err error
	if out.Writes, err = buildWritesInput(in); err != nil {
		return trustInputs{}, err
	}
	if out.Ownership, err = buildOwnershipInput(in); err != nil {
		return trustInputs{}, err
	}
	if out.Evidence, err = buildEvidenceInput(in); err != nil {
		return trustInputs{}, err
	}
	if out.Policies, err = buildPoliciesInput(in); err != nil {
		return trustInputs{}, err
	}
	built := out.fill(TrustAdapters{})
	for _, name := range unsetAdapters(built) {
		if slices.Contains([]string{"Writes", "Ownership", "Evidence", "Reconciler", "Policies", "Clocks"}, name) {
			out.Gaps = append(out.Gaps, name)
		}
	}
	return out, nil
}

// fill sets the adapters that were built and leaves every other field of a as it was.
func (t trustInputs) fill(a TrustAdapters) TrustAdapters {
	if t.Writes != nil {
		a.Writes = t.Writes
	}
	if t.Ownership != nil {
		a.Ownership = t.Ownership
	}
	if t.Evidence != nil {
		a.Evidence = t.Evidence
	}
	if t.Policies != nil {
		a.Policies = t.Policies
	}
	return a
}

// buildOwnershipInput builds one Ownership per served tenant from the pack's ownership data and nothing else (A475: Pack.OwnershipKinds through NodesFromKinds; the guard test reads
// this file). Without the pack, the enterprise read or a served tenant it is a gap.
func buildOwnershipInput(in trustInputsIn) (intake.Ownership, error) {
	if in.Pack == nil || isNilValue(in.OwnershipReader) || len(in.Tenants) == 0 {
		return nil, nil
	}
	kinds, err := in.Pack.OwnershipKinds()
	if err != nil {
		return nil, inputsFail("the pack's ownership data is not usable", err)
	}
	instances := make(ownershipByTenant, len(in.Tenants))
	for _, tenant := range in.Tenants {
		o, err := ownershipport.New(ownershipport.Config{TenantID: tenant, Reader: in.OwnershipReader, Nodes: ownershipport.NodesFromKinds(kinds)})
		if err != nil {
			return nil, inputsFail("the Ownership adapter cannot be built for a served tenant", err)
		}
		instances[tenant] = o
	}
	return instances, nil
}

// pools checks the Trust logins every database-backed input needs: a positive bound and one login for each served tenant.
func (in trustInputsIn) pools() (trustpg.Pools, error) {
	if in.Timeout <= 0 {
		return trustpg.Pools{}, inputsFail(EnvTrustDBTimeout+" must be a positive whole number of milliseconds", nil)
	}
	pools, err := trustpg.NewPools(in.Pools)
	if err != nil {
		return trustpg.Pools{}, inputsFail("the Trust logins are not usable ("+EnvTrustDSNDir+")", err)
	}
	for _, tenant := range in.Tenants {
		if _, err := pools.For(tenant); err != nil {
			return trustpg.Pools{}, inputsFail("a served tenant has no Trust login ("+EnvTrustDSNDir+")", err)
		}
	}
	return pools, nil
}

func (in trustInputsIn) screened() bool {
	return in.Payloads != nil && !isNilValue(in.Screen.Screen) && !isNilValue(in.Screen.Alert) && in.Screen.Correlation != nil
}

// buildEvidenceInput builds T9 over the pack's trust profiles, the tenants' sealed snapshots and the Reconciler's own facts (the rule set and the mapping main builds; no second
// mapping). Without a part it is a gap.
func buildEvidenceInput(in trustInputsIn) (intake.Evidence, error) {
	if len(in.Verified.Pack) == 0 || in.RuleSet == nil || in.Mapping == nil || isNilValue(in.Clock) || !in.screened() {
		return nil, nil
	}
	var doc contracts.PackV1
	if err := json.Unmarshal(in.Verified.Pack, &doc); err != nil {
		return nil, inputsFail("the pack's trust profiles cannot be read", err)
	}
	profiles, err := trustport.NewPackProfiles(doc.IntentNodes, doc.TrustProfiles)
	if err != nil {
		return nil, inputsFail("the pack's trust profiles are not usable", err)
	}
	pools, err := in.pools()
	if err != nil {
		return nil, err
	}
	snapshots, err := trustport.NewSealedSnapshots(txLogins{pools}, in.Timeout)
	if err != nil {
		return nil, inputsFail("the sealed snapshot reader cannot be built", err)
	}
	payloads, err := buildScreenedPayloads(in.Payloads, in.Screen)
	if err != nil {
		return nil, inputsFail("the payload reader cannot be built", err)
	}
	facts, err := evidencereconciler.New(evidencereconciler.Config{RuleSet: in.RuleSet, Mapping: in.Mapping, Payloads: payloads, Clock: in.Clock})
	if err != nil {
		return nil, inputsFail("the Reconciler's facts cannot be built", err)
	}
	ev, err := trustport.NewEvidence(snapshots, profiles, in.Clock, facts)
	if err != nil {
		return nil, inputsFail("the Evidence adapter cannot be built", err)
	}
	return ev, nil
}

// buildPoliciesInput builds T11 over the pack's policy book, the tenants' publications, the admission time of the stored intent and the fact source over the sealed snapshot's lists.
// Without a part (the list convention included) it is a gap: with no list entity a rule that reads a list could never be evaluated (INV-19).
func buildPoliciesInput(in trustInputsIn) (intake.Policies, error) {
	if len(in.Verified.Pack) == 0 || in.Mapping == nil || len(in.ListEntities) == 0 || !in.screened() {
		return nil, nil
	}
	book, err := intakeadapters.LoadPolicyBook(in.Verified.Pack)
	if err != nil {
		return nil, inputsFail("the pack's policy cannot be loaded", err)
	}
	pools, err := in.pools()
	if err != nil {
		return nil, err
	}
	logins := queryLogins{pools}
	admissions, err := intakesources.NewAdmissions(logins, in.Timeout)
	if err != nil {
		return nil, inputsFail("the admission reader cannot be built", err)
	}
	items, err := intakesources.NewSnapshotItems(logins, in.Timeout)
	if err != nil {
		return nil, inputsFail("the snapshot item reader cannot be built", err)
	}
	lists := intakesources.EvidenceLists{
		Mapping: in.Mapping, Items: items, Payloads: in.Payloads, Entities: in.ListEntities,
		Screen: in.Screen.Screen, Correlation: in.Screen.Correlation, Alert: in.Screen.Alert,
	}
	facts, err := intakesources.NewFacts(logins, in.Timeout, intakesources.WithLists(lists))
	if err != nil {
		return nil, inputsFail("the fact source cannot be built", err)
	}
	p, err := intakeadapters.NewPolicies(intakeadapters.PoliciesConfig{Book: book, Publications: intakeadapters.StorePublications{DBs: policyLogins{pools}}, Admissions: admissions, Facts: facts})
	if err != nil {
		return nil, inputsFail("the Policies adapter cannot be built", err)
	}
	return p, nil
}
