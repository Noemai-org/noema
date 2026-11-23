package trustport

// TRU-07 (A435, A459 interim, relayed, not yet committed; REQ-TRU-009; INV-25, INV-17, INV-21): the confirmation binding evidence hash of a Record is ONE value per request (one
// snapshot per decision), the content digest of the sealed snapshot's entries (source, source record ID, facts digest, status; a repeated entry once), computed by the evidence layer
// from the sealed snapshot and never taken from the request. It reaches the Writer as recorder.Request.ConfirmationBindingEvidenceHash, apart from the grant's set hash. All data
// is SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// rbhFacts is the ItemFacts of these tests: one KNOWN fact per item, whose state is "S-<source record ID>" unless a record is overridden or made to fail.
type rbhFacts struct {
	state map[string]string
	err   error
	calls int
}

func (f *rbhFacts) Facts(_ context.Context, _ string, it layer.Item) ([]snapshot.Fact, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	rec := it.PayloadRef
	st := "S-" + rec
	if v, ok := f.state[rec]; ok {
		st = v
	}
	return []snapshot.Fact{{Entity: "rbh", Status: "KNOWN", Fields: map[string]snapshot.FactValue{"state": {Kind: snapshot.KindEnum, Text: st}}}}, nil
}

// rbhSnapshot is a sealed snapshot of the recRequest evaluation with two items that carry a source record ID and a payload reference (which rbhFacts reads as the record).
func rbhSnapshot(t *testing.T) SealedSnapshot {
	t.Helper()
	s := sfxSnapshot(t)
	s.Items[0].SourceRecordID, s.Items[0].PayloadRef = "rec-1", "rec-1"
	return s
}

// rbhWant is the content hash computed here from the definition: the entry of each item is (source, record, digest of its facts, status).
func rbhWant(t *testing.T, s SealedSnapshot, f *rbhFacts) string {
	t.Helper()
	var in []snapshot.ContentItem
	for _, it := range s.Items {
		facts, err := f.Facts(context.Background(), s.TenantID, layer.Item{PayloadRef: it.PayloadRef})
		if err != nil {
			t.Fatal(err)
		}
		d, err := snapshot.FactsDigest(facts)
		if err != nil {
			t.Fatal(err)
		}
		in = append(in, snapshot.ContentItem{Source: it.Source, SourceRecordID: it.SourceRecordID, FactsDigest: d, Status: it.Status})
	}
	h, err := snapshot.ContentHash(in)
	if err != nil {
		t.Fatal(err)
	}
	f.calls = 0
	return h
}

func rbhFactSource(t *testing.T, rd *sfxReader, f ItemFacts) *SealedFacts {
	t.Helper()
	s, err := NewSealedFacts(SealedFactsConfig{Snapshots: rd, PolicyID: "fin.pol", Steps: &sfxSteps{}, Facts: f})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rbhConfirmed(t *testing.T) intake.RecordRequest {
	t.Helper()
	r := recRequest(t, 1)
	r.ConfirmationEventID, r.ConfirmationPlanHash = "cfe_01AB", r.PlanHash
	return r
}

// A435, INV-25: for a Record that follows a confirmation the facts carry the content digest of the SEALED snapshot, whatever value the request names; it is not the set hash.
func TestSealedFactsGiveTheContentDigestOfTheSealedSnapshotForAConfirmation(t *testing.T) {
	rd, f := &sfxReader{snap: rbhSnapshot(t)}, &rbhFacts{}
	want := rbhWant(t, rd.snap, f)
	r := rbhConfirmed(t)
	r.ConfirmationBindingEvidenceHash = recHash(200) // what the confirmation was bound to, as the intake kept it: not the source of the fresh value
	got, err := rbhFactSource(t, rd, f).Facts(recCtx(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfirmationBindingEvidenceHash != want {
		t.Errorf("binding evidence %q, want the content digest %q", got.ConfirmationBindingEvidenceHash, want)
	}
	if got.ConfirmationBindingEvidenceHash == got.EvidenceSetHash {
		t.Error("the binding value is the snapshot's set hash")
	}
	if got.EvidenceSetHash != rd.snap.EvidenceSetHash {
		t.Errorf("the set hash %q is no longer the snapshot's", got.EvidenceSetHash)
	}
}

// A459: one value per request, whatever the number of steps; a request that follows no confirmation has none, and the facts are not read for it.
func TestSealedFactsGiveNoBindingEvidenceWithoutAConfirmation(t *testing.T) {
	rd, f := &sfxReader{snap: rbhSnapshot(t)}, &rbhFacts{}
	r := recRequest(t, 2)
	r.ConfirmationBindingEvidenceHash = recHash(200)
	got, err := rbhFactSource(t, rd, f).Facts(recCtx(t), r)
	if err != nil || got.ConfirmationBindingEvidenceHash != "" || f.calls != 0 {
		t.Errorf("no confirmation: %q (%v), %d fact reads, want none", got.ConfirmationBindingEvidenceHash, err, f.calls)
	}
}

// A435: a changed mapped fact gives another value, so a confirmation bound to the earlier one is refused at the spend (the Writer's binding check, trust/recorder).
func TestSealedFactsGiveAnotherValueWhenAMappedFactChanged(t *testing.T) {
	rd := &sfxReader{snap: rbhSnapshot(t)}
	a, errA := rbhFactSource(t, rd, &rbhFacts{}).Facts(recCtx(t), rbhConfirmed(t))
	b, errB := rbhFactSource(t, rd, &rbhFacts{state: map[string]string{"rec-1": "S-changed"}}).Facts(recCtx(t), rbhConfirmed(t))
	if errA != nil || errB != nil || a.ConfirmationBindingEvidenceHash == "" || a.ConfirmationBindingEvidenceHash == b.ConfirmationBindingEvidenceHash {
		t.Errorf("unchanged %q (%v), changed %q (%v)", a.ConfirmationBindingEvidenceHash, errA, b.ConfirmationBindingEvidenceHash, errB)
	}
}

// INV-19: facts that cannot be read, or an item with no source record ID, leave nothing to bind: refused with empty facts.
func TestSealedFactsRefuseAConfirmedRecordWhoseContentCannotBeDigested(t *testing.T) {
	noRecord := rbhSnapshot(t)
	noRecord.Items[0].SourceRecordID = ""
	cases := map[string]struct {
		snap SealedSnapshot
		f    *rbhFacts
	}{"facts fail": {rbhSnapshot(t), &rbhFacts{err: errors.New("store down")}}, "no source record": {noRecord, &rbhFacts{}}}
	for name, c := range cases {
		got, err := rbhFactSource(t, &sfxReader{snap: c.snap}, c.f).Facts(recCtx(t), rbhConfirmed(t))
		if !errors.Is(err, ErrSnapshotNotIntact) || got.PolicyID != "" || got.ConfirmationBindingEvidenceHash != "" || len(got.Steps) != 0 {
			t.Errorf("%s: %+v, %v, want empty facts and ErrSnapshotNotIntact", name, got, err)
		}
	}
}

func TestSealedFactsAreNotBuiltWithoutTheItemFactsSource(t *testing.T) {
	if _, err := NewSealedFacts(SealedFactsConfig{Snapshots: &sfxReader{}, PolicyID: "fin.pol", Steps: &sfxSteps{}}); !errors.Is(err, ErrConfig) {
		t.Errorf("err %v, want ErrConfig", err)
	}
}

// A435: the adapter hands the Writer the facts' value on the request, apart from every grant step's set hash, and only for a Record that follows a confirmation. What the intake's
// request names is not passed on.
func TestTheRecorderAdapterHandsTheWriterTheFreshBindingEvidenceHash(t *testing.T) {
	r := rbhConfirmed(t)
	r.ConfirmationBindingEvidenceHash = recHash(200)
	f, w := newRecFacts(), &recWriter{res: recGrants(1)}
	f.f.ConfirmationBindingEvidenceHash = recHash(5)
	if _, err := recAdapter(t, &recBegin{tx: &recTx{}}, f, w).Record(recCtx(t), r); err != nil {
		t.Fatal(err)
	}
	q := w.got[0]
	if string(q.ConfirmationBindingEvidenceHash) != recHash(5) {
		t.Errorf("the Writer got %q, want the facts' %q", q.ConfirmationBindingEvidenceHash, recHash(5))
	}
	if string(q.Steps[0].EvidenceSetHash) != recHash(3) {
		t.Errorf("the grant's set hash %q changed", q.Steps[0].EvidenceSetHash)
	}
	r2, w2 := recRequest(t, 1), &recWriter{res: recGrants(1)}
	r2.ConfirmationBindingEvidenceHash = recHash(200)
	if _, err := recAdapter(t, &recBegin{tx: &recTx{}}, f, w2).Record(recCtx(t), r2); err != nil || w2.got[0].ConfirmationBindingEvidenceHash != "" {
		t.Errorf("no confirmation: the Writer got %q (%v), want none", w2.got[0].ConfirmationBindingEvidenceHash, err)
	}
}

// INV-25: a Record that follows a confirmation with no well-formed fresh value is refused before any transaction.
func TestAConfirmedRecordWithoutAFreshBindingEvidenceHashIsRefusedBeforeAnyTransaction(t *testing.T) {
	for name, v := range map[string]string{"none": "", "malformed": "sha256:abc", "upper case": "sha256:" + strings.Repeat("A", 64)} {
		f, w, b := newRecFacts(), &recWriter{res: recGrants(1)}, &recBegin{tx: &recTx{}}
		f.f.ConfirmationBindingEvidenceHash = v
		r := rbhConfirmed(t)
		r.ConfirmationBindingEvidenceHash = recHash(5) // a good value on the request must not rescue it
		if _, err := recAdapter(t, b, f, w).Record(recCtx(t), r); !errors.Is(err, ErrRecordRequest) || len(w.got) != 0 || len(b.tenants) != 0 {
			t.Errorf("%s: %v, %d writes, %d begins", name, err, len(w.got), len(b.tenants))
		}
	}
}
