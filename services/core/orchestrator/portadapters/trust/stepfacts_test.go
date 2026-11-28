package trustport

// EXP-01 (A550, A556, A665, A706, A710): the production StepFactSource of the Recorder. For one write step of an ALLOW decision it gives the target version the evidence was read at,
// from the SEALED snapshot of the evaluation and the evidence mapping's own declaration of which source carries the token (never from the request, INV-17), and it fails closed
// (INV-19): no item for the step's target, two items, an item that is not KNOWN, a payload that is not the sealed one, a read not marked complete, or no token is an error with no version and
// never a guess. The freeze Permission is a NAMED GAP until the freeze row (RES-02, noema#958) and a Record reader for it exist: StepFacts refuses with ErrStepPermission, so the Recorder
// treats the step as not permitted. REQ-TRU-009, REQ-RES-002, REQ-CTX-004; INV-17, INV-19, INV-20; AGENT-13. All data is SYNTHETIC. No database is needed.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const (
	svfLedger = "fin.src.core_ledger"
	svfSwitch = "fin.src.switch"
	svfToken  = "tok-SECRET-12"
)

var svfPackDir = filepath.Join("..", "..", "..", "..", "..", "packs")

// svfMapping is the pack's real evidence mapping, loaded the way a composition root loads it, so the declaration's member names are the pack's and not the test's.
func svfMapping(t *testing.T) *mapping.Mapping {
	t.Helper()
	read := func(parts ...string) []byte {
		b, err := os.ReadFile(filepath.Join(append([]string{svfPackDir}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	rs, err := reconcile.ParseRuleSet(read("fintech", "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	pack := read("fintech_v1.json")
	scope, err := mapping.ScopeFromDescriptors(pack)
	if err != nil {
		t.Fatal(err)
	}
	m, err := mapping.Parse(read("fintech", "evidence_mapping", "mapping.v1.json"), rs, []string{svfLedger, svfSwitch, "fin.src.destination", "fin.src.case_system"}, "fin.map.v1@3",
		mapping.Limits{MaxBytes: 1 << 20, MaxDepth: 32}, mapping.WithScope(scope))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type svfRead struct {
	tenant string
	item   layer.Item
}

// svfPayloads is the payload port of these tests: the verified payload of an evidence item by its ID, or the error it is made to give.
type svfPayloads struct {
	byID  map[string]map[string]any
	errs  map[string]error
	reads []svfRead
}

func (p *svfPayloads) Payload(_ context.Context, tenantID string, it layer.Item) (map[string]any, error) {
	p.reads = append(p.reads, svfRead{tenantID, it})
	if err := p.errs[it.EvidenceID]; err != nil {
		return nil, err
	}
	v, ok := p.byID[it.EvidenceID]
	if !ok {
		return nil, payloadread.ErrUnavailable
	}
	return v, nil
}

func svfItem(id, source, record, status string, n byte) SealedItem {
	return SealedItem{EvidenceID: id, CorrelationID: "corr_ABC123", Source: source, SourceRecordID: record, PayloadRef: "pay_" + id, ReadAt: recEpoch, EffectiveAt: recEpoch, SLASeconds: 300,
		Status: status, PayloadHash: recHash(n)}
}

// svfSeal is the snapshot as the sealer stores it: ids sorted, the hash the database would compute, the tenant and correlation of the recRequest evaluation.
func svfSeal(t *testing.T, items ...SealedItem) SealedSnapshot {
	t.Helper()
	pairs := make([]snapshot.Pair, len(items))
	ids := make([]string, len(items))
	for i, it := range items {
		pairs[i], ids[i] = snapshot.Pair{EvidenceID: it.EvidenceID, PayloadHash: it.PayloadHash}, it.EvidenceID
	}
	sort.Strings(ids)
	h, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	return SealedSnapshot{TenantID: recTenant, SnapshotID: "evs_01ABC", CorrelationID: "corr_ABC123", EvidenceIDs: ids, EvidenceSetHash: h, Items: items}
}

func svfComplete(token string) map[string]any {
	return map[string]any{"transaction_ref": "TX88213", "entries": []any{}, "version_token": token, "complete": true}
}

// svfWorld is the usual snapshot of the step's target (recTarget, fin.transaction:TX88213): its ledger read and its switch read, and the ledger read of another transaction.
func svfWorld(t *testing.T) (SealedSnapshot, *svfPayloads) {
	t.Helper()
	snap := svfSeal(t, svfItem("ev_1", svfLedger, "TX88213", "KNOWN", 1), svfItem("ev_2", svfSwitch, "TX88213", "KNOWN", 2), svfItem("ev_3", svfLedger, "TX99999", "KNOWN", 3))
	return snap, &svfPayloads{byID: map[string]map[string]any{"ev_1": svfComplete(svfToken), "ev_2": {"semantic_status": "SUCCESS"}, "ev_3": svfComplete("tok-other-1")}}
}

func svfSource(t *testing.T, p *svfPayloads) *SealedSteps {
	t.Helper()
	s, err := NewSealedSteps(SealedStepsConfig{Mapping: svfMapping(t), Payloads: p})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func svfStep() intake.RecordStep {
	return intake.RecordStep{PlanStep: 0, CapabilityID: recCap, TargetRef: recTarget, ParamsHash: recHash(7)}
}

// INV-17, REQ-CTX-004: the version is the token of the ledger read of the step's own target, from the sealed snapshot; it names the step's target, the source that gave it and nothing else.
func TestTheTargetVersionIsTheTokenOfTheLedgerReadOfTheStepsTarget(t *testing.T) {
	snap, p := svfWorld(t)
	got, err := svfSource(t, p).TargetVersions(recCtx(t), snap, svfStep())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0].ResourceRef) != recTarget || got[0].Source != svfLedger || got[0].VersionToken != svfToken {
		t.Fatalf("versions %+v, want the one token of the target's ledger read", got)
	}
	if len(p.reads) != 1 || p.reads[0].tenant != recTenant || p.reads[0].item.EvidenceID != "ev_1" || p.reads[0].item.PayloadHash != recHash(1) || p.reads[0].item.PayloadRef != "pay_ev_1" {
		t.Errorf("payload reads %+v, want one read of ev_1 as the snapshot's tenant (no switch read, no other transaction's read)", p.reads)
	}
}

// The order of the items in the snapshot decides nothing, and the other transaction's token is never the answer.
func TestTheStepGetsNoOtherTransactionsToken(t *testing.T) {
	snap, p := svfWorld(t)
	snap.Items[0], snap.Items[2] = snap.Items[2], snap.Items[0]
	got, err := svfSource(t, p).TargetVersions(recCtx(t), snap, svfStep())
	if err != nil || len(got) != 1 || got[0].VersionToken != svfToken {
		t.Fatalf("got %+v, %v; want %s", got, err, svfToken)
	}
	other := svfStep()
	other.TargetRef = "fin.transaction:TX99999"
	got, err = svfSource(t, p).TargetVersions(recCtx(t), snap, other)
	if err != nil || len(got) != 1 || got[0].VersionToken != "tok-other-1" || string(got[0].ResourceRef) != "fin.transaction:TX99999" {
		t.Fatalf("got %+v, %v; want the other transaction's own token for the other target", got, err)
	}
}

// INV-19: every case below is a version that cannot be given, so there is none and the error is the typed one, never a partial answer.
func TestNoVersionIsGivenWhenTheEvidenceCannotSupportIt(t *testing.T) {
	type mutation func(*SealedSnapshot, *svfPayloads, *intake.RecordStep)
	cases := map[string]mutation{
		"the step's target has no ledger item": func(s *SealedSnapshot, _ *svfPayloads, st *intake.RecordStep) {
			st.TargetRef = "fin.transaction:TX00000"
		},
		"the ledger item is not KNOWN (STALE)": func(s *SealedSnapshot, _ *svfPayloads, _ *intake.RecordStep) { s.Items[0].Status = "STALE" },
		"the ledger item is UNKNOWN":           func(s *SealedSnapshot, _ *svfPayloads, _ *intake.RecordStep) { s.Items[0].Status = "UNKNOWN" },
		"the ledger item is UNAVAILABLE":       func(s *SealedSnapshot, _ *svfPayloads, _ *intake.RecordStep) { s.Items[0].Status = "UNAVAILABLE" },
		"two ledger items for the target": func(s *SealedSnapshot, p *svfPayloads, _ *intake.RecordStep) {
			*s = svfSeal(t, append(s.Items, svfItem("ev_4", svfLedger, "TX88213", "KNOWN", 4))...)
			p.byID["ev_4"] = svfComplete(svfToken)
		},
		"the payload read says it is not complete": func(_ *SealedSnapshot, p *svfPayloads, _ *intake.RecordStep) {
			p.byID["ev_1"] = map[string]any{"version_token": svfToken, "complete": false}
		},
		"the payload has no complete marker": func(_ *SealedSnapshot, p *svfPayloads, _ *intake.RecordStep) {
			p.byID["ev_1"] = map[string]any{"version_token": svfToken}
		},
		"the payload has no token": func(_ *SealedSnapshot, p *svfPayloads, _ *intake.RecordStep) {
			p.byID["ev_1"] = map[string]any{"complete": true}
		},
		"the token is empty": func(_ *SealedSnapshot, p *svfPayloads, _ *intake.RecordStep) { p.byID["ev_1"] = svfComplete("") },
		"the payload is not the sealed one": func(_ *SealedSnapshot, p *svfPayloads, _ *intake.RecordStep) {
			p.errs["ev_1"] = payloadread.ErrHashMismatch
		},
		"the payload is not one object": func(_ *SealedSnapshot, p *svfPayloads, _ *intake.RecordStep) {
			p.errs["ev_1"] = payloadread.ErrNotObject
		},
		"the payload cannot be read": func(_ *SealedSnapshot, p *svfPayloads, _ *intake.RecordStep) {
			p.errs["ev_1"] = payloadread.ErrUnavailable
		},
		"the payload has a value of its own": func(_ *SealedSnapshot, p *svfPayloads, _ *intake.RecordStep) {
			p.errs["ev_1"] = errors.New("pgx: password for " + svfToken)
		},
		"the snapshot hash is not the sealed one": func(s *SealedSnapshot, _ *svfPayloads, _ *intake.RecordStep) { s.EvidenceSetHash = recHash(5) },
		"the snapshot has no tenant":              func(s *SealedSnapshot, _ *svfPayloads, _ *intake.RecordStep) { s.TenantID = "" },
		"an item of another correlation": func(s *SealedSnapshot, _ *svfPayloads, _ *intake.RecordStep) {
			s.Items[0].CorrelationID = "corr_OTHER1"
		},
		"the target is not a resource reference": func(_ *SealedSnapshot, _ *svfPayloads, st *intake.RecordStep) { st.TargetRef = "TX88213" },
		"the target has no id":                   func(_ *SealedSnapshot, _ *svfPayloads, st *intake.RecordStep) { st.TargetRef = "fin.transaction:" },
		"the target has two ids": func(_ *SealedSnapshot, _ *svfPayloads, st *intake.RecordStep) {
			st.TargetRef = "fin.transaction:TX88213:TX1"
		},
		"the target is empty":           func(_ *SealedSnapshot, _ *svfPayloads, st *intake.RecordStep) { st.TargetRef = "" },
		"the target is a wildcard":      func(_ *SealedSnapshot, _ *svfPayloads, st *intake.RecordStep) { st.TargetRef = "fin.transaction:*" },
		"the item has no source record": func(s *SealedSnapshot, _ *svfPayloads, _ *intake.RecordStep) { s.Items[0].SourceRecordID = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			snap, p := svfWorld(t)
			p.errs = map[string]error{}
			st := svfStep()
			mutate(&snap, p, &st)
			got, err := svfSource(t, p).TargetVersions(recCtx(t), snap, st)
			if !errors.Is(err, ErrStepVersion) || got != nil {
				t.Fatalf("got %+v, %v; want no version and ErrStepVersion", got, err)
			}
			for _, secret := range []string{svfToken, "TX88213", "pay_ev_1", "password", recHash(1)} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("the error text carries %q (AGENT-13): %v", secret, err)
				}
			}
		})
	}
}

// A snapshot that is the sealed one but has the first (and only) matching item NOT marked KNOWN still reads no payload at all: an item that cannot support a version is not read for one.
func TestAnItemThatIsNotKnownIsNotReadForAVersion(t *testing.T) {
	snap, p := svfWorld(t)
	snap.Items[0].Status = "STALE"
	if _, err := svfSource(t, p).TargetVersions(recCtx(t), snap, svfStep()); !errors.Is(err, ErrStepVersion) {
		t.Fatal(err)
	}
	if len(p.reads) != 0 {
		t.Errorf("payload reads %+v, want none", p.reads)
	}
}

func TestTheVersionNeedsAContextWithADeadline(t *testing.T) {
	snap, p := svfWorld(t)
	if got, err := svfSource(t, p).TargetVersions(context.Background(), snap, svfStep()); !errors.Is(err, ErrNoDeadline) || got != nil || len(p.reads) != 0 {
		t.Errorf("got %+v, %v, %d reads; want ErrNoDeadline and no read", got, err, len(p.reads))
	}
}

func TestACancelledContextGivesNoVersion(t *testing.T) {
	snap, p := svfWorld(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	cancel()
	if got, err := svfSource(t, p).TargetVersions(ctx, snap, svfStep()); err == nil || got != nil {
		t.Errorf("got %+v, %v; a cancelled context gives no version", got, err)
	}
}

func TestAStepSourceNeedsItsPartsAndNoneIsDefaulted(t *testing.T) {
	p := &svfPayloads{}
	for name, c := range map[string]SealedStepsConfig{"no mapping": {Payloads: p}, "no payloads": {Mapping: svfMapping(t)}, "nothing": {}} {
		if s, err := NewSealedSteps(c); !errors.Is(err, ErrConfig) || s != nil {
			t.Errorf("%s: got %v, %v; want ErrConfig", name, s, err)
		}
	}
}

// --- the Permission: a named gap (REQ-RES-002, INV-20) ---------------------------------------------------------------------------------------

// Until the freeze row and a Record reader for it exist, no Permission can be built, and one must never be guessed: StepFacts refuses even when the version is perfectly good, with the typed
// error and no facts at all, so the Recorder cannot issue a grant for the step.
func TestStepFactsRefuseWithTheNamedGapAndGiveNoPermission(t *testing.T) {
	snap, p := svfWorld(t)
	src := svfSource(t, p)
	if _, err := src.TargetVersions(recCtx(t), snap, svfStep()); err != nil {
		t.Fatalf("control: the version part must work, or the refusal below proves nothing: %v", err)
	}
	got, err := src.StepFacts(recCtx(t), snap, svfStep())
	if !errors.Is(err, ErrStepPermission) {
		t.Fatalf("err %v, want ErrStepPermission", err)
	}
	if got.Permission.Valid() || got.TargetVersions != nil {
		t.Errorf("facts %+v with the gap: a refusal carries no version and no permission", got)
	}
	if errors.Is(err, ErrStepVersion) {
		t.Errorf("the gap is not a version failure: a caller must tell them apart: %v", err)
	}
}

// The gap is the same whatever the evidence is: no path through StepFacts returns a valid Permission.
func TestNoPathThroughStepFactsReturnsAValidPermission(t *testing.T) {
	for name, mutate := range map[string]func(*SealedSnapshot, *svfPayloads){
		"a good version":    func(*SealedSnapshot, *svfPayloads) {},
		"no ledger payload": func(_ *SealedSnapshot, p *svfPayloads) { delete(p.byID, "ev_1") },
		"an incomplete one": func(_ *SealedSnapshot, p *svfPayloads) {
			p.byID["ev_1"] = map[string]any{"version_token": svfToken, "complete": false}
		},
	} {
		snap, p := svfWorld(t)
		mutate(&snap, p)
		got, err := svfSource(t, p).StepFacts(recCtx(t), snap, svfStep())
		if err == nil || got.Permission.Valid() {
			t.Errorf("%s: got %+v, %v; want an error and no permission", name, got, err)
		}
	}
}

// The Recorder's own fact source takes the refusal whole: SealedFacts returns no facts and the error, and the adapter writes nothing (no transaction, no grant, EC-E16, INV-03).
func TestTheRecorderTreatsTheNamedGapAsNotPermitted(t *testing.T) {
	snap, p := svfWorld(t)
	rd := &sfxReader{snap: snap}
	sealed, err := NewSealedFacts(SealedFactsConfig{Snapshots: rd, PolicyID: "fin.pol", Steps: svfSource(t, p), Facts: &rbhFacts{}})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := sealed.Facts(recCtx(t), recRequest(t, 1))
	if !errors.Is(err, ErrStepPermission) || len(facts.Steps) != 0 || facts.EvidenceSetHash != "" {
		t.Fatalf("facts %+v, err %v; want no facts and ErrStepPermission", facts, err)
	}
	w, b := &recWriter{res: recGrants(1)}, &recBegin{tx: &recTx{}}
	a := &RecorderAdapter{begin: b, facts: sealed, writer: w, txTimeout: recTxTimeout}
	if _, err := a.Record(recCtx(t), recRequest(t, 1)); !errors.Is(err, ErrStepPermission) {
		t.Errorf("Record err %v, want ErrStepPermission", err)
	}
	if len(w.got) != 0 || len(b.tenants) != 0 {
		t.Errorf("%d writes and %d transactions for a step the gap refuses; want none", len(w.got), len(b.tenants))
	}
}

func TestTheStepSourceIsTheRecordersPort(t *testing.T) {
	var _ StepFactSource = (*SealedSteps)(nil)
}
