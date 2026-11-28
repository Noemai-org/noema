package trustport

import (
	"context"
	"errors"
	"regexp"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// The production StepFactSource of the Recorder (EXP-01; rulings A550, A556, A665, A706, A710; REQ-TRU-009, REQ-RES-002, REQ-CTX-004; INV-17, INV-19, INV-20; AGENT-13).
//
// For one write step of an ALLOW decision it gives the target versions of the step's grant. The version is the token the evidence was read at: it is in the stored payload of the item the
// sealed snapshot holds for the step's target, under the member the pack's evidence mapping declares (`target_versions`), and it is read only from there. It is never read from the request
// (INV-17: a RecordRequest has no field for it). The item is the snapshot's own: its source is declared by the mapping, its source record is the id of the step's target, it is KNOWN, and it
// is the only one; its payload is the stored one, verified against the item's hash by the payload port, and marked complete. Anything else is no version, never a guess (INV-19).
//
// The freeze Permission of the step is a NAMED GAP. It can only come from reconcile.FreezeGate.PermitResult over the reconciliation of the target and the effective freeze (REQ-RES-002,
// INV-20), and the freeze row and a Record reader for it are not on main (noema#958 and its Record side, C1 and EL). Until they are, StepFacts refuses with ErrStepPermission and returns no
// facts, so the Recorder issues no grant for the step: the step is not permitted. Nothing here builds a Permission, fills a zero one or defaults to "not frozen".

var (
	// ErrStepVersion is returned when a step has no usable target version: no or more than one item for its target, an item that is not KNOWN, a payload that is not the sealed one or not
	// marked complete, no token, a snapshot that is not the sealed one, or a target that is not a resource reference. The cause stays reachable behind the fixed text.
	ErrStepVersion = errors.New("trustport: the step has no usable target version")
	// ErrStepPermission is the named gap: the freeze permission of a step cannot be built yet. A Recorder that gets it writes nothing.
	ErrStepPermission = errors.New("trustport: the freeze permission of the step cannot be built yet")
)

// targetRefRe is the contract's resource reference, `<vertical>.<kind>:<id>`, with the id in the group (common.v1 ResourceRef).
var targetRefRe = regexp.MustCompile(`\A[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:([A-Za-z0-9_.-]{1,64})\z`)

// VersionMapping is the evidence mapping's declaration of which sources carry a target version token and how it reads one (trust/reconcile/mapping.Mapping): pack data, never a literal here
// (INV-14).
type VersionMapping interface {
	DeclaresTargetVersion(source string) bool
	TargetVersion(source string, payload map[string]any) (string, error)
}

// ItemPayloads gives the stored payload of an evidence item as the tenant, verified against the item's payload hash (evidence/payloadread.Reader is the real one).
type ItemPayloads interface {
	Payload(ctx context.Context, tenantID string, it layer.Item) (map[string]any, error)
}

// SealedStepsConfig builds a SealedSteps. Every part is required; there are no defaults.
type SealedStepsConfig struct {
	Mapping  VersionMapping
	Payloads ItemPayloads
}

// SealedSteps implements StepFactSource over the sealed snapshot of the evaluation.
type SealedSteps struct {
	mapping  VersionMapping
	payloads ItemPayloads
}

var _ StepFactSource = (*SealedSteps)(nil)

// NewSealedSteps builds the step source. A missing mapping or payload port is ErrConfig.
func NewSealedSteps(c SealedStepsConfig) (*SealedSteps, error) {
	if c.Mapping == nil || c.Payloads == nil {
		return nil, ErrConfig
	}
	return &SealedSteps{mapping: c.Mapping, payloads: c.Payloads}, nil
}

// TargetVersions gives the one target version of a write step, from the sealed snapshot it is handed (SealedFacts has read it as the request's tenant and checked it is intact; it is checked
// again here, since a step source must not trust its caller's check for a value a grant binds). The payload is read as the snapshot's tenant, once, for the one item of the step's target.
// On any error the versions are nil. The error text names no tenant, reference, token or payload (AGENT-13).
func (s *SealedSteps) TargetVersions(ctx context.Context, snap SealedSnapshot, st intake.RecordStep) ([]contracts.DecisionGrantV1TargetVersionsItem, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrNoDeadline
	}
	if err := ctx.Err(); err != nil {
		return nil, wrapped(ErrStepVersion, err)
	}
	m := targetRefRe.FindStringSubmatch(st.TargetRef)
	if m == nil || snap.TenantID == "" {
		return nil, wrapped(ErrStepVersion, nil)
	}
	ordered, err := intact(snap, snap.TenantID, snap.SnapshotID)
	if err != nil {
		return nil, wrapped(ErrStepVersion, err)
	}
	var found []SealedItem
	for _, it := range ordered {
		if it.SourceRecordID == m[1] && s.mapping.DeclaresTargetVersion(it.Source) {
			found = append(found, it)
		}
	}
	if len(found) != 1 || found[0].Status != string(contracts.EnumsV1FactStatusKNOWN) {
		return nil, wrapped(ErrStepVersion, nil)
	}
	it := found[0]
	payload, err := s.payloads.Payload(ctx, snap.TenantID, layer.Item{EvidenceID: it.EvidenceID, PayloadRef: it.PayloadRef, PayloadHash: it.PayloadHash,
		Status: contracts.EnumsV1FactStatus(it.Status), Source: it.Source, ReadAt: it.ReadAt, EffectiveAt: it.EffectiveAt})
	if err != nil {
		return nil, wrapped(ErrStepVersion, err)
	}
	token, err := s.mapping.TargetVersion(it.Source, payload)
	if err != nil {
		return nil, wrapped(ErrStepVersion, err)
	}
	return []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: contracts.CommonV1ResourceRef(st.TargetRef), Source: contracts.CommonV1PackId(it.Source), VersionToken: token}}, nil
}

// StepFacts is the Recorder's port. The version part is TargetVersions; the Permission is the named gap, so StepFacts always ends in an error and returns no facts: with a good version it is
// ErrStepPermission, with no usable version it is ErrStepVersion. It never returns a valid Permission.
func (s *SealedSteps) StepFacts(ctx context.Context, snap SealedSnapshot, st intake.RecordStep) (RecordStepFacts, error) {
	if _, err := s.TargetVersions(ctx, snap, st); err != nil {
		return RecordStepFacts{}, err
	}
	return RecordStepFacts{}, ErrStepPermission
}
