package trustport

// TRU-12 facts adapter (REQ-TRU-009, REQ-CTX-004; INV-16, INV-17, INV-19, INV-21; AGENT-16): the production RecordFactSource reads the evidence set hash and the policy
// from the SEALED SNAPSHOT of the evaluation and the pack the composition passes in, never from the request. All data is SYNTHETIC. No database is needed.

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

type sfxReader struct {
	snap  SealedSnapshot
	err   error
	calls int
	gotT  string
	gotID string
	ctx   context.Context
}

func (r *sfxReader) Sealed(ctx context.Context, tenantID, snapshotID string) (SealedSnapshot, error) {
	r.calls++
	r.gotT, r.gotID, r.ctx = tenantID, snapshotID, ctx
	return r.snap, r.err
}

type sfxSteps struct {
	err   error
	snaps []SealedSnapshot
	steps []intake.RecordStep
}

func (s *sfxSteps) StepFacts(_ context.Context, snap SealedSnapshot, st intake.RecordStep) (RecordStepFacts, error) {
	s.snaps, s.steps = append(s.snaps, snap), append(s.steps, st)
	if s.err != nil {
		return RecordStepFacts{}, s.err
	}
	p, err := reconcile.NewFreezeGate(nil).PermitTarget(false, st.CapabilityID, st.TargetRef)
	if err != nil {
		panic(err)
	}
	return RecordStepFacts{TargetVersions: []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: contracts.CommonV1ResourceRef(st.TargetRef), Source: "fin.src.ledger", VersionToken: "snap-v1"}}, Permission: p}, nil
}

// sfxSnapshot is a sealed snapshot of the recRequest evaluation: snapshot evs_01ABC, correlation corr_ABC123, one item, and the hash the database would hold.
func sfxSnapshot(t *testing.T) SealedSnapshot {
	t.Helper()
	h, err := snapshot.SetHash([]snapshot.Pair{{EvidenceID: "ev_1", PayloadHash: recHash(9)}})
	if err != nil {
		t.Fatal(err)
	}
	return SealedSnapshot{TenantID: recTenant, SnapshotID: "evs_01ABC", CorrelationID: "corr_ABC123", EvidenceIDs: []string{"ev_1"}, EvidenceSetHash: h,
		Items: []SealedItem{{EvidenceID: "ev_1", CorrelationID: "corr_ABC123", Source: "fin.src.ledger", ReadAt: recEpoch, EffectiveAt: recEpoch, SLASeconds: 300, Status: "KNOWN", PayloadHash: recHash(9)}}}
}

func sfxFacts(t *testing.T, rd *sfxReader, st *sfxSteps) *SealedFacts {
	t.Helper()
	f, err := NewSealedFacts(SealedFactsConfig{Snapshots: rd, PolicyID: "fin.pol", Steps: st, Facts: &rbhFacts{}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// REQ-CTX-004, INV-17: the evidence hash is the sealed snapshot's, read for the tenant of the request and the snapshot the evaluation recorded in its basis.
func TestSealedFactsTakeTheHashFromTheSealedSnapshotOfTheEvaluation(t *testing.T) {
	rd, st := &sfxReader{snap: sfxSnapshot(t)}, &sfxSteps{}
	r := recRequest(t, 2)
	got, err := sfxFacts(t, rd, st).Facts(recCtx(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.EvidenceSetHash != rd.snap.EvidenceSetHash || got.PolicyID != "fin.pol" || len(got.Steps) != 2 {
		t.Fatalf("facts %+v, want the snapshot's hash %s", got, rd.snap.EvidenceSetHash)
	}
	if rd.calls != 1 || rd.gotT != recTenant || rd.gotID != "evs_01ABC" {
		t.Fatalf("reader called %d times for %q %q, want once for the request's tenant and the basis's snapshot", rd.calls, rd.gotT, rd.gotID)
	}
	if _, ok := rd.ctx.Deadline(); !ok {
		t.Error("the reader's context has no deadline")
	}
	if len(st.snaps) != 2 || !reflect.DeepEqual(st.snaps[0], rd.snap) || st.steps[1].PlanStep != 1 {
		t.Errorf("the step source did not get the sealed snapshot and the step: %+v", st.steps)
	}
	if got.Steps[0].TargetVersions[0].VersionToken != "snap-v1" {
		t.Errorf("step versions %+v", got.Steps[0].TargetVersions)
	}
}

// A request has no field that could carry an evidence hash or a target version: if one is ever added, this fails and the fact source must be reviewed (INV-17).
// The one named exception is RecordRequest.ConfirmationBindingEvidenceHash (A435): the content digest the confirmation was bound over, which the Writer recomputes and
// never lets reach the snapshot's set hash, the decision or the grant (TestNoValueOfTheRequestReachesTheEvidenceHashOrTheVersions injects it). It is allowed on the
// request alone, never on a step, and under no other name.
func TestARecordRequestHasNoFieldThatCouldCarryAHashOrAVersionOfTheFacts(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(intake.RecordRequest{}), reflect.TypeOf(intake.RecordStep{})} {
		for i := 0; i < typ.NumField(); i++ {
			n := typ.Field(i).Name
			if typ == reflect.TypeOf(intake.RecordRequest{}) && n == "ConfirmationBindingEvidenceHash" {
				continue
			}
			if strings.Contains(n, "Evidence") || strings.Contains(n, "Version") && n != "IntentVersion" || strings.Contains(n, "Snapshot") {
				t.Errorf("%s.%s could carry a fact the sealed snapshot owns", typ.Name(), n)
			}
		}
	}
}

// Property (INV-17, INV-21): whatever the request's own strings are, the hash and the versions the facts carry are the snapshot's and the step source's.
func TestNoValueOfTheRequestReachesTheEvidenceHashOrTheVersions(t *testing.T) {
	rng := rand.New(rand.NewSource(20261120))
	junk := func() string { return recHash(byte(rng.Intn(256))) }
	for i := 0; i < 200; i++ {
		rd, st := &sfxReader{snap: sfxSnapshot(t)}, &sfxSteps{}
		r := recRequest(t, 1+rng.Intn(3))
		r.IntentHash, r.PlanHash = junk(), junk()
		r.ConfirmationBindingEvidenceHash = junk()
		for j := range r.Steps {
			r.Steps[j].ParamsHash = junk()
		}
		got, err := sfxFacts(t, rd, st).Facts(recCtx(t), r)
		if err != nil {
			t.Fatal(err)
		}
		if got.EvidenceSetHash != rd.snap.EvidenceSetHash {
			t.Fatalf("round %d: hash %s is not the snapshot's", i, got.EvidenceSetHash)
		}
		for j, s := range got.Steps {
			if len(s.TargetVersions) != 1 || s.TargetVersions[0].VersionToken != "snap-v1" {
				t.Fatalf("round %d step %d: versions %+v are not the step source's", i, j, s.TargetVersions)
			}
		}
	}
}

// The facts are one thing with the adapter: the writer gets the snapshot's hash on every grant step, whatever the request said.
func TestTheRecorderAdapterBindsEveryGrantToTheSealedSnapshotsHash(t *testing.T) {
	rd, st := &sfxReader{snap: sfxSnapshot(t)}, &sfxSteps{}
	w := &recWriter{res: recGrants(2)}
	a := &RecorderAdapter{begin: &recBegin{tx: &recTx{}}, facts: sfxFacts(t, rd, st), writer: w, txTimeout: recTxTimeout}
	if _, err := a.Record(recCtx(t), recRequest(t, 2)); err != nil {
		t.Fatal(err)
	}
	for i, s := range w.got[0].Steps {
		if string(s.EvidenceSetHash) != rd.snap.EvidenceSetHash {
			t.Errorf("step %d bound %s, want the sealed snapshot's %s", i, s.EvidenceSetHash, rd.snap.EvidenceSetHash)
		}
	}
}

// A377 item 7: the snapshot's own correlation must EQUAL the evaluation's, else refuse (INV-11).
func TestSealedFactsRefuseASnapshotThatIsNotTheEvaluations(t *testing.T) {
	cases := map[string]struct {
		mut  func(*SealedSnapshot)
		want error
	}{
		"another correlation": {func(s *SealedSnapshot) {
			s.CorrelationID = "corr_OTHER"
			s.Items[0].CorrelationID = "corr_OTHER"
		}, ErrSnapshotNotIntact},
		"another tenant":       {func(s *SealedSnapshot) { s.TenantID = "ten_bb" }, ErrSnapshotNotIntact},
		"another snapshot":     {func(s *SealedSnapshot) { s.SnapshotID = "evs_02XYZ" }, ErrSnapshotNotIntact},
		"a hash not the set's": {func(s *SealedSnapshot) { s.EvidenceSetHash = recHash(1) }, ErrSnapshotNotIntact},
		"no items":             {func(s *SealedSnapshot) { s.Items, s.EvidenceIDs = nil, nil }, ErrSnapshotNotIntact},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rd, st := &sfxReader{snap: sfxSnapshot(t)}, &sfxSteps{}
			c.mut(&rd.snap)
			got, err := sfxFacts(t, rd, st).Facts(recCtx(t), recRequest(t, 1))
			if !errors.Is(err, c.want) || !reflect.DeepEqual(got, RecordFacts{}) || len(st.steps) != 0 {
				t.Fatalf("got %+v, %v; want ErrSnapshotNotIntact, empty facts, no step read", got, err)
			}
		})
	}
}

func TestSealedFactsPassOnAReaderFailureAndReadNothingMore(t *testing.T) {
	for _, e := range []error{ErrSnapshotNotFound, ErrSnapshotStore} {
		rd, st := &sfxReader{err: e}, &sfxSteps{}
		if _, err := sfxFacts(t, rd, st).Facts(recCtx(t), recRequest(t, 1)); !errors.Is(err, e) || len(st.steps) != 0 {
			t.Errorf("err %v, want %v and no step read", err, e)
		}
	}
	boom := errors.New("boom")
	if _, err := sfxFacts(t, &sfxReader{snap: sfxSnapshot(t)}, &sfxSteps{err: boom}).Facts(recCtx(t), recRequest(t, 1)); !errors.Is(err, boom) {
		t.Errorf("step source failure: %v", err)
	}
}

// The policy is the verified pack's and must be one the evaluation used (A377 item 6); a policy the basis does not name is refused (INV-09).
func TestSealedFactsRefuseAPolicyTheEvaluationDidNotUse(t *testing.T) {
	f, err := NewSealedFacts(SealedFactsConfig{Snapshots: &sfxReader{snap: sfxSnapshot(t)}, PolicyID: "fin.other", Steps: &sfxSteps{}, Facts: &rbhFacts{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Facts(recCtx(t), recRequest(t, 1)); !errors.Is(err, ErrRecordRequest) {
		t.Fatalf("err %v, want ErrRecordRequest", err)
	}
}

func TestSealedFactsRefuseARequestWithNoEvaluationAndNoDeadline(t *testing.T) {
	r := recRequest(t, 1)
	r.Evaluated = nil
	rd := &sfxReader{snap: sfxSnapshot(t)}
	if _, err := sfxFacts(t, rd, &sfxSteps{}).Facts(recCtx(t), r); !errors.Is(err, ErrRecordRequest) || rd.calls != 0 {
		t.Errorf("no builder: %v, %d reads", err, rd.calls)
	}
	if _, err := sfxFacts(t, rd, &sfxSteps{}).Facts(context.Background(), recRequest(t, 1)); !errors.Is(err, ErrNoDeadline) || rd.calls != 0 {
		t.Errorf("no deadline: %v, %d reads", err, rd.calls)
	}
}

func TestSealedFactsAreNotBuiltWithoutAPortOrAPolicy(t *testing.T) {
	rd, st := &sfxReader{}, &sfxSteps{}
	for name, c := range map[string]SealedFactsConfig{"no reader": {PolicyID: "fin.pol", Steps: st, Facts: &rbhFacts{}}, "no steps": {Snapshots: rd, PolicyID: "fin.pol", Facts: &rbhFacts{}}, "no policy": {Snapshots: rd, Steps: st, Facts: &rbhFacts{}}} {
		if _, err := NewSealedFacts(c); !errors.Is(err, ErrConfig) {
			t.Errorf("%s: %v, want ErrConfig", name, err)
		}
	}
}

var _ RecordFactSource = (*SealedFacts)(nil)
