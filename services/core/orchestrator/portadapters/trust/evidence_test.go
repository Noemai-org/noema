package trustport_test

// A329, the Trust intake's T9 adapter `intake.Evidence`. These tests need no database: the adapter's reader of the sealed snapshot is a fake that returns data (tampered
// data included), and the transaction of the real reader is a fake that records how it was used. The same behaviour over a real PostgreSQL, with the row-level security of
// the tenant's own trust_rw login, is in evidence_db_test.go. Every name here starts with `evd` so that the files of the other adapters of the package cannot collide with it.
// Proves REQ-TRU-001, REQ-TRU-003, REQ-CTX-002, REQ-CTX-003, REQ-CTX-004, REQ-NFR-002; INV-11, INV-16, INV-17, INV-19, INV-23; AGENT-13; EC-E16. All data is SYNTHETIC (AGENT-25).

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const (
	evdTenant = "ten_aa"
	evdOther  = "ten_bb"
	evdSnap   = "evs_AA01"
	evdCorr   = "corr_AA11"
	evdNode   = "fin.payments.exception.investigate"
	evdLedger = "fin.src.core_ledger"
	evdSwitch = "fin.src.switch"
	evdSettle = "fin.src.settlement"
	evdSecret = "SECRETROW"
)

// evdNow is the SYNTHETIC instant of the evaluation.
var evdNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

func evdCtx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func evdHash(n int) string { return fmt.Sprintf("sha256:%064x", n) }

// evdItem is a KNOWN item of one source, read 42 seconds before the evaluation, with a source limit of 600 seconds.
func evdItem(n int, source string) trustport.SealedItem {
	return trustport.SealedItem{EvidenceID: fmt.Sprintf("evr_%02d", n), CorrelationID: evdCorr, Source: source, ReadAt: evdNow.Add(-42 * time.Second),
		EffectiveAt: evdNow.Add(-60 * time.Second), SLASeconds: 600, Status: "KNOWN", PayloadHash: evdHash(n)}
}

// evdSeal is the snapshot as the sealer stores it: the ids sorted, the hash the one the database computes, the intent the correlation's latest, admitted before every read.
// The items are handed over in reverse order: nothing may rely on the order of the items, only on the ids.
func evdSeal(t testing.TB, items ...trustport.SealedItem) trustport.SealedSnapshot {
	t.Helper()
	pairs := make([]snapshot.Pair, len(items))
	ids := make([]string, len(items))
	for i, it := range items {
		pairs[i], ids[i] = snapshot.Pair{EvidenceID: it.EvidenceID, PayloadHash: it.PayloadHash}, it.EvidenceID
	}
	sort.Strings(ids)
	hash, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	rev := make([]trustport.SealedItem, len(items))
	for i := range items {
		rev[len(items)-1-i] = items[i]
	}
	return trustport.SealedSnapshot{TenantID: evdTenant, SnapshotID: evdSnap, CorrelationID: evdCorr, EvidenceIDs: ids, EvidenceSetHash: hash, Items: rev,
		Intent: trustport.SealedIntent{IntentID: "int_0001", Version: 2, IntentNode: evdNode, AdmittedAt: evdNow.Add(-24 * time.Hour)}}
}

// evdWorld is the usual snapshot: three sources, the evidence of the case.
func evdWorld(t testing.TB) trustport.SealedSnapshot {
	t.Helper()
	return evdSeal(t, evdItem(1, evdLedger), evdItem(2, evdSwitch), evdItem(3, evdSettle))
}

type evdStore struct {
	snaps map[string]trustport.SealedSnapshot
	err   error
	calls []string
}

func (s *evdStore) Sealed(_ context.Context, tenantID, snapshotID string) (trustport.SealedSnapshot, error) {
	s.calls = append(s.calls, tenantID+"/"+snapshotID)
	if s.err != nil {
		return trustport.SealedSnapshot{}, s.err
	}
	v, ok := s.snaps[tenantID+"/"+snapshotID]
	if !ok {
		return trustport.SealedSnapshot{}, trustport.ErrSnapshotNotFound
	}
	return v, nil
}

func evdStoreOf(s trustport.SealedSnapshot) *evdStore {
	return &evdStore{snaps: map[string]trustport.SealedSnapshot{evdTenant + "/" + evdSnap: s}}
}

type evdProfiles struct {
	rules trustport.SourceRules
	err   error
	asked []string
}

func (p *evdProfiles) SourceRules(_ context.Context, tenantID, intentNode string) (trustport.SourceRules, error) {
	p.asked = append(p.asked, tenantID+"/"+intentNode)
	return p.rules, p.err
}

func evdRules() *evdProfiles {
	return &evdProfiles{rules: trustport.SourceRules{RequiredSources: []string{evdLedger, evdSwitch, evdSettle}, MaxAgeSeconds: 300}}
}

func evdAdapter(t testing.TB, s trustport.SnapshotReader, p trustport.SourceProfiles) trustport.Evidence {
	t.Helper()
	e, err := trustport.NewEvidence(s, p, clock.NewManual(evdNow))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func evdRead(t testing.TB, s *evdStore, p *evdProfiles) (intake.SnapshotEvidence, error) {
	t.Helper()
	return evdAdapter(t, s, p).SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
}

// evdNothing says an answer that is an error carries no evidence at all: T9 must have nothing to decide on.
func evdNothing(t testing.TB, got intake.SnapshotEvidence) {
	t.Helper()
	if !reflect.DeepEqual(got, intake.SnapshotEvidence{}) {
		t.Errorf("an error came with evidence: %+v", got)
	}
}

// REQ-TRU-001, REQ-CTX-003, REQ-CTX-004; INV-17, INV-23: the freshness is the one the Decision Record keeps, worked out from what was sealed. The age is measured from read_at
// to the evaluation (PERD 21.2); the limit is the stricter of the source's and the profile's, and the record says which set it. The intent comes from the stored chain, never
// from a request; the items come in the order of the sealed ids.
func TestEvidenceReportsTheFreshnessThatWasSealed(t *testing.T) {
	a, b, c := evdItem(1, evdLedger), evdItem(2, evdSwitch), evdItem(3, evdSettle)
	a.SLASeconds = 600 // the profile's 300 is stricter
	b.SLASeconds, b.Status, b.ReadAt = 120, "UNKNOWN", evdNow.Add(-100*time.Second)
	c.SLASeconds, c.Status = 300, "INFERRED" // equal to the profile's: the source set it
	got, err := evdRead(t, evdStoreOf(evdSeal(t, c, a, b)), evdRules())
	if err != nil {
		t.Fatal(err)
	}
	want := intake.SnapshotEvidence{IntentID: "int_0001", IntentVersion: 2, Items: []intake.SourceItem{
		{Status: contracts.EnumsV1FactStatusKNOWN, Freshness: decide.Freshness{EvidenceID: "evr_01", EffectiveAt: evdNow.Add(-60 * time.Second).Unix(), Source: evdLedger,
			AgeSeconds: 42, EffectiveLimitSeconds: 300, LimitSetBy: decide.LimitByTrustProfile}},
		{Status: contracts.EnumsV1FactStatusUNKNOWN, Freshness: decide.Freshness{EvidenceID: "evr_02", EffectiveAt: evdNow.Add(-60 * time.Second).Unix(), Source: evdSwitch,
			AgeSeconds: 100, EffectiveLimitSeconds: 120, LimitSetBy: decide.LimitBySource}},
		{Status: contracts.EnumsV1FactStatusINFERRED, Freshness: decide.Freshness{EvidenceID: "evr_03", EffectiveAt: evdNow.Add(-60 * time.Second).Unix(), Source: evdSettle,
			AgeSeconds: 42, EffectiveLimitSeconds: 300, LimitSetBy: decide.LimitBySource}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// Every one of the eight fact statuses of the contract is carried as the item holds it; the adapter never turns one into another (INV-19: absence is never a negative fact).
func TestEvidenceCarriesEachFactStatusAsSealed(t *testing.T) {
	for _, st := range []contracts.EnumsV1FactStatus{contracts.EnumsV1FactStatusKNOWN, contracts.EnumsV1FactStatusINFERRED, contracts.EnumsV1FactStatusAMBIGUOUS,
		contracts.EnumsV1FactStatusUNAVAILABLE, contracts.EnumsV1FactStatusSTALE, contracts.EnumsV1FactStatusCONTRADICTORY, contracts.EnumsV1FactStatusUNKNOWN,
		contracts.EnumsV1FactStatusUNAUTHORIZED} {
		it := evdItem(1, evdLedger)
		it.Status = string(st)
		got, err := evdRead(t, evdStoreOf(evdSeal(t, it)), &evdProfiles{rules: trustport.SourceRules{MaxAgeSeconds: 300}})
		if err != nil || len(got.Items) != 1 || got.Items[0].Status != st {
			t.Errorf("%s: got %+v, %v", st, got, err)
		}
	}
}

// REQ-CTX-003; INV-23: the age is rounded up to a whole second and compared with the limit the way the status function compares (older than the limit is stale), so the adapter
// never reports an item younger than it is. At exactly the limit an item is not stale; a millisecond more and it is.
func TestEvidenceAgeIsRoundedUpAndAgreesWithTheStatusRule(t *testing.T) {
	for _, c := range []struct {
		ms         int64
		age        int64
		staleAtMax bool
	}{{0, 0, false}, {1, 1, false}, {41_000, 41, false}, {41_001, 42, false}, {300_000, 300, false}, {300_001, 301, true}, {301_000, 301, true}} {
		it := evdItem(1, evdLedger)
		it.ReadAt = evdNow.Add(-time.Duration(c.ms) * time.Millisecond)
		got, err := evdRead(t, evdStoreOf(evdSeal(t, it)), &evdProfiles{rules: trustport.SourceRules{MaxAgeSeconds: 300}})
		if err != nil {
			t.Fatalf("%d ms: %v", c.ms, err)
		}
		if len(got.Items) != 1 {
			t.Fatalf("%d ms: %d items", c.ms, len(got.Items))
		}
		f := got.Items[0].Freshness
		if f.AgeSeconds != c.age || (f.AgeSeconds > f.EffectiveLimitSeconds) != c.staleAtMax {
			t.Errorf("%d ms: age %d (want %d), limit %d, stale %v (want %v)", c.ms, f.AgeSeconds, c.age, f.EffectiveLimitSeconds, f.AgeSeconds > f.EffectiveLimitSeconds, c.staleAtMax)
		}
	}
}

// REQ-CTX-003, INV-23, INV-21: for any source limit, profile limit and read time, the limit is the stricter of the two, SOURCE names who set it unless the profile is stricter,
// the age is the read-to-evaluation time rounded up, and "older than the limit" holds exactly when the real elapsed time exceeds it.
func TestEvidenceFreshnessProperty(t *testing.T) {
	rapid.Check(t, func(r *rapid.T) {
		sla := rapid.Int64Range(1, 5000).Draw(r, "source limit seconds")
		max := rapid.Int64Range(1, 5000).Draw(r, "profile limit seconds")
		ms := rapid.Int64Range(0, 6_000_000).Draw(r, "elapsed milliseconds")
		it := evdItem(1, evdLedger)
		it.SLASeconds, it.ReadAt = sla, evdNow.Add(-time.Duration(ms)*time.Millisecond)
		got, err := evdRead(t, evdStoreOf(evdSeal(t, it)), &evdProfiles{rules: trustport.SourceRules{MaxAgeSeconds: max}})
		if err != nil {
			r.Fatal(err)
		}
		if len(got.Items) != 1 {
			r.Fatalf("%d items", len(got.Items))
		}
		f := got.Items[0].Freshness
		limit, by := sla, decide.LimitBySource
		if max < sla {
			limit, by = max, decide.LimitByTrustProfile
		}
		if f.EffectiveLimitSeconds != limit || f.LimitSetBy != by {
			r.Fatalf("limit %d by %s, want %d by %s", f.EffectiveLimitSeconds, f.LimitSetBy, limit, by)
		}
		if f.AgeSeconds != (ms+999)/1000 {
			r.Fatalf("age %d for %d ms", f.AgeSeconds, ms)
		}
		if (f.AgeSeconds > f.EffectiveLimitSeconds) != (ms > limit*1000) {
			r.Fatalf("stale %v for %d ms against %d s", f.AgeSeconds > f.EffectiveLimitSeconds, ms, limit)
		}
	})
}

// A226 (the Basis refuses a negative age): an item read after the evaluation is not evidence. It is not clamped to zero.
func TestEvidenceRefusesAnItemReadAfterTheEvaluation(t *testing.T) {
	it := evdItem(1, evdLedger)
	it.ReadAt = evdNow.Add(time.Millisecond)
	got, err := evdRead(t, evdStoreOf(evdSeal(t, it)), evdRules())
	if !errors.Is(err, trustport.ErrSnapshotNotIntact) {
		t.Errorf("err = %v, want ErrSnapshotNotIntact", err)
	}
	evdNothing(t, got)
}

// REQ-CTX-004, INV-17, EC-E16: a snapshot that is not exactly what was sealed is an error, never evidence. Each case is a stored snapshot that someone changed, or that the
// database should never have held.
func TestEvidenceRefusesASnapshotThatIsNotIntact(t *testing.T) {
	cases := map[string]func(s *trustport.SealedSnapshot){
		"the stored hash is another one":     func(s *trustport.SealedSnapshot) { s.EvidenceSetHash = evdHash(999) },
		"the stored hash is empty":           func(s *trustport.SealedSnapshot) { s.EvidenceSetHash = "" },
		"the stored hash is not well formed": func(s *trustport.SealedSnapshot) { s.EvidenceSetHash = "sha256:xyz" },
		"a payload hash changed":             func(s *trustport.SealedSnapshot) { s.Items[0].PayloadHash = evdHash(998) },
		"an item is missing":                 func(s *trustport.SealedSnapshot) { s.Items = s.Items[1:] },
		"an item is not in the sealed ids":   func(s *trustport.SealedSnapshot) { x := evdItem(9, evdLedger); s.Items = append(s.Items, x) },
		"an item is there twice":             func(s *trustport.SealedSnapshot) { s.Items = append(s.Items[:2:2], s.Items[1]) },
		"the sealed ids are not in order": func(s *trustport.SealedSnapshot) {
			s.EvidenceIDs[0], s.EvidenceIDs[1] = s.EvidenceIDs[1], s.EvidenceIDs[0]
		},
		"the sealed ids repeat one":             func(s *trustport.SealedSnapshot) { s.EvidenceIDs[2] = s.EvidenceIDs[1] },
		"a sealed id is empty":                  func(s *trustport.SealedSnapshot) { s.EvidenceIDs[0] = "" },
		"the set is empty":                      func(s *trustport.SealedSnapshot) { s.EvidenceIDs, s.Items = nil, nil },
		"an item belongs to another case":       func(s *trustport.SealedSnapshot) { s.Items[1].CorrelationID = "corr_ZZ99" },
		"the snapshot is of another tenant":     func(s *trustport.SealedSnapshot) { s.TenantID = evdOther },
		"the snapshot is another snapshot":      func(s *trustport.SealedSnapshot) { s.SnapshotID = "evs_AA02" },
		"the snapshot names no case":            func(s *trustport.SealedSnapshot) { s.CorrelationID = "" },
		"an item has no id":                     func(s *trustport.SealedSnapshot) { s.Items[0].EvidenceID = "" },
		"an item has another id than the ids":   func(s *trustport.SealedSnapshot) { s.Items[0].EvidenceID = "evr_77" },
		"the items hold the same item as three": func(s *trustport.SealedSnapshot) { s.Items[0], s.Items[1] = s.Items[2], s.Items[2] },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := evdWorld(t)
			mutate(&s)
			got, err := evdRead(t, evdStoreOf(s), evdRules())
			if !errors.Is(err, trustport.ErrSnapshotNotIntact) {
				t.Errorf("err = %v, want ErrSnapshotNotIntact", err)
			}
			evdNothing(t, got)
		})
	}
}

// REQ-CTX-002, INV-19, REQ-NFR-002: an item whose status is missing or not one of the eight is an error and the whole answer is empty, so T9 ends UNKNOWN: it is never skipped,
// defaulted or passed. The same holds for an item that has no source, no usable limit or no usable time.
func TestEvidenceRefusesAnItemThatCannotBeReadAsEvidence(t *testing.T) {
	cases := map[string]func(it *trustport.SealedItem){
		"no status":                      func(it *trustport.SealedItem) { it.Status = "" },
		"a status outside the enum":      func(it *trustport.SealedItem) { it.Status = "FRESH" },
		"a status in lower case":         func(it *trustport.SealedItem) { it.Status = "known" },
		"no source":                      func(it *trustport.SealedItem) { it.Source = "" },
		"a limit of zero":                func(it *trustport.SealedItem) { it.SLASeconds = 0 },
		"a negative limit":               func(it *trustport.SealedItem) { it.SLASeconds = -5 },
		"no read time":                   func(it *trustport.SealedItem) { it.ReadAt = time.Time{} },
		"no effective time":              func(it *trustport.SealedItem) { it.EffectiveAt = time.Time{} },
		"an effective time at the epoch": func(it *trustport.SealedItem) { it.EffectiveAt = time.Unix(0, 0).UTC() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			items := []trustport.SealedItem{evdItem(1, evdLedger), evdItem(2, evdSwitch), evdItem(3, evdSettle)}
			mutate(&items[1]) // one bad item among good ones: the good ones do not make the answer usable
			got, err := evdRead(t, evdStoreOf(evdSeal(t, items...)), evdRules())
			if !errors.Is(err, trustport.ErrSnapshotNotIntact) {
				t.Errorf("err = %v, want ErrSnapshotNotIntact", err)
			}
			evdNothing(t, got)
		})
	}
}

// REQ-TRU-003, INV-11: a snapshot of another tenant and a snapshot that does not exist are the same answer, in value and in text. A snapshot ID that is not the shape of one never
// reaches the store.
func TestEvidenceAnotherTenantsSnapshotIsTheSameAnswerAsAnUnknownOne(t *testing.T) {
	store := &evdStore{snaps: map[string]trustport.SealedSnapshot{evdOther + "/" + evdSnap: evdWorld(t)}} // tenant B's snapshot, asked for as tenant A
	_, other := evdAdapter(t, store, evdRules()).SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	_, unknown := evdAdapter(t, store, evdRules()).SnapshotFreshness(evdCtx(t), evdTenant, "evs_ZZ99")
	if !errors.Is(other, trustport.ErrSnapshotNotFound) || !errors.Is(unknown, trustport.ErrSnapshotNotFound) {
		t.Fatalf("other tenant: %v, unknown: %v; want ErrSnapshotNotFound for both", other, unknown)
	}
	if other.Error() != unknown.Error() || other.Error() != trustport.ErrSnapshotNotFound.Error() || errors.Is(other, trustport.ErrSnapshotStore) || errors.Is(unknown, trustport.ErrSnapshotStore) {
		t.Errorf("the two answers differ, or are not the bare not-found: %q and %q", other, unknown)
	}
	for _, bad := range []string{"", "evs_", "evs_A", "evs_" + strings.Repeat("A", 33), "snap_AA01", "evs_AA01'; --", "evs_AA 01", "evs_../x", "EVS_AA01"} {
		s := &evdStore{}
		_, err := evdAdapter(t, s, evdRules()).SnapshotFreshness(evdCtx(t), evdTenant, bad)
		if !errors.Is(err, trustport.ErrSnapshotNotFound) || len(s.calls) != 0 {
			t.Errorf("snapshot %q: err = %v, store calls %v", bad, err, s.calls)
		}
	}
}

// INV-12: the call is refused without a usable tenant or a deadline, before the store or the profile is asked. The adapter is not usable unwired (AGENT-16: no default).
func TestEvidenceRefusesACallThatCannotBeAnsweredSafely(t *testing.T) {
	s, p := evdStoreOf(evdWorld(t)), evdRules()
	a := evdAdapter(t, s, p)
	for _, tenant := range []string{"", "ten_", "TEN_AA", "ten_aa; DROP", "tenant"} {
		if _, err := a.SnapshotFreshness(evdCtx(t), tenant, evdSnap); !errors.Is(err, trustport.ErrSession) {
			t.Errorf("tenant %q: err = %v, want ErrSession", tenant, err)
		}
	}
	if _, err := a.SnapshotFreshness(context.Background(), evdTenant, evdSnap); !errors.Is(err, trustport.ErrEvidenceNoDeadline) {
		t.Errorf("no deadline: err = %v, want ErrEvidenceNoDeadline", err)
	}
	if len(s.calls) != 0 || len(p.asked) != 0 {
		t.Errorf("a refused call reached a port: store %v, profile %v", s.calls, p.asked)
	}
	for name, build := range map[string]func() (trustport.Evidence, error){
		"no reader":  func() (trustport.Evidence, error) { return trustport.NewEvidence(nil, p, clock.NewManual(evdNow)) },
		"no profile": func() (trustport.Evidence, error) { return trustport.NewEvidence(s, nil, clock.NewManual(evdNow)) },
		"no clock":   func() (trustport.Evidence, error) { return trustport.NewEvidence(s, p, nil) },
	} {
		if _, err := build(); !errors.Is(err, trustport.ErrConfig) {
			t.Errorf("%s: err = %v, want ErrConfig", name, err)
		}
	}
	if _, err := (trustport.Evidence{}).SnapshotFreshness(evdCtx(t), evdTenant, evdSnap); !errors.Is(err, trustport.ErrConfig) {
		t.Errorf("the zero adapter: err = %v, want ErrConfig", err)
	}
}

// AGENT-13: no error carries row text, a payload, another tenant's ID or an identifier of the sealed evidence. A failure of the store or of the profile keeps its cause for
// errors.Is and shows only its own fixed text.
func TestEvidenceErrorsCarryNoRowTextAndNoOtherTenant(t *testing.T) {
	cause := errors.New("pq: duplicate key in evidence_item for ten_bb " + evdSecret + " payload_hash " + evdHash(5))
	leaks := []string{evdSecret, evdOther, evdHash(5), evdSnap, "evr_", "payload_hash"}
	check := func(name string, err error, kind error) {
		t.Helper()
		if !errors.Is(err, kind) {
			t.Errorf("%s: err = %v, want %v", name, err, kind)
			return
		}
		for _, l := range leaks {
			if strings.Contains(err.Error(), l) {
				t.Errorf("%s: the error text holds %q: %q", name, l, err)
			}
		}
	}
	_, err := evdRead(t, &evdStore{err: cause}, evdRules())
	check("store failure", err, trustport.ErrSnapshotStore)
	if !errors.Is(err, cause) {
		t.Errorf("the cause is lost: %v", err)
	}
	_, err = evdRead(t, evdStoreOf(evdWorld(t)), &evdProfiles{err: cause})
	check("profile failure", err, trustport.ErrSourceRules)
	if !errors.Is(err, cause) {
		t.Errorf("the cause is lost: %v", err)
	}
	bad := evdWorld(t)
	bad.EvidenceSetHash = evdHash(5)
	_, err = evdRead(t, evdStoreOf(bad), evdRules())
	check("not intact", err, trustport.ErrSnapshotNotIntact)
	nobody := evdWorld(t)
	nobody.Intent = trustport.SealedIntent{}
	_, err = evdRead(t, evdStoreOf(nobody), evdRules())
	check("no intent", err, trustport.ErrSnapshotIntent)
	_, err = evdRead(t, &evdStore{}, evdRules())
	check("not found", err, trustport.ErrSnapshotNotFound)
}

// A226, REQ-TRU-001: T9 refuses a snapshot that was sealed for another intent or version, so the adapter says which intent this one belongs to, from the stored chain of its
// correlation and never from the request. The sealed snapshot does not name its intent (DEBT, see the README), so the adapter attributes it only when it can be certain: the
// intent chain's latest version was admitted before every item was read. A newer version admitted after a read means the evidence was read for an older one.
func TestEvidenceNamesTheIntentOnlyWhenTheEvidenceWasReadForIt(t *testing.T) {
	earliest := evdNow.Add(-42 * time.Second)
	cases := map[string]struct {
		intent trustport.SealedIntent
		ok     bool
	}{
		"admitted before the reads":     {trustport.SealedIntent{IntentID: "int_0001", Version: 2, IntentNode: evdNode, AdmittedAt: earliest.Add(-time.Second)}, true},
		"admitted at the first read":    {trustport.SealedIntent{IntentID: "int_0001", Version: 2, IntentNode: evdNode, AdmittedAt: earliest}, true},
		"admitted after the first read": {trustport.SealedIntent{IntentID: "int_0001", Version: 2, IntentNode: evdNode, AdmittedAt: earliest.Add(time.Millisecond)}, false},
		"admitted after the evaluation": {trustport.SealedIntent{IntentID: "int_0001", Version: 2, IntentNode: evdNode, AdmittedAt: evdNow.Add(time.Second)}, false},
		"no intent for the case":        {trustport.SealedIntent{}, false},
		"no intent id":                  {trustport.SealedIntent{Version: 2, IntentNode: evdNode, AdmittedAt: earliest.Add(-time.Hour)}, false},
		"version zero":                  {trustport.SealedIntent{IntentID: "int_0001", IntentNode: evdNode, AdmittedAt: earliest.Add(-time.Hour)}, false},
		"no node":                       {trustport.SealedIntent{IntentID: "int_0001", Version: 2, AdmittedAt: earliest.Add(-time.Hour)}, false},
		"no admission time":             {trustport.SealedIntent{IntentID: "int_0001", Version: 2, IntentNode: evdNode}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := evdWorld(t)
			s.Intent = c.intent
			p := evdRules()
			got, err := evdRead(t, evdStoreOf(s), p)
			if c.ok {
				if err != nil || got.IntentID != "int_0001" || got.IntentVersion != 2 {
					t.Errorf("got %+v, %v", got, err)
				}
				return
			}
			if !errors.Is(err, trustport.ErrSnapshotIntent) {
				t.Errorf("err = %v, want ErrSnapshotIntent", err)
			}
			evdNothing(t, got)
			if len(p.asked) != 0 {
				t.Errorf("the profile was asked about an intent that could not be attributed: %v", p.asked)
			}
		})
	}
}

// REQ-CTX-003, INV-19: a required source with no item at all in the snapshot is a source that could not be read, and T9 must hear it; the items that were read are still
// reported. A required source that has an item, even one that is UNAVAILABLE, is not "missing": its status carries that. The profile is asked for the tenant and the sealed
// intent's node.
func TestEvidenceSaysWhenARequiredSourceIsMissing(t *testing.T) {
	cases := map[string]struct {
		required []string
		missing  bool
	}{
		"all three present":             {[]string{evdLedger, evdSwitch, evdSettle}, false},
		"one required source is absent": {[]string{evdLedger, evdSwitch, evdSettle, "fin.src.beneficiary"}, true},
		"only the absent one":           {[]string{"fin.src.beneficiary"}, true},
		"the profile requires nothing":  {nil, false},
		"a source named twice":          {[]string{evdLedger, evdLedger}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p := &evdProfiles{rules: trustport.SourceRules{RequiredSources: c.required, MaxAgeSeconds: 300}}
			got, err := evdRead(t, evdStoreOf(evdWorld(t)), p)
			if err != nil || got.RequiredUnavailable != c.missing || len(got.Items) != 3 {
				t.Errorf("got %+v, %v; want RequiredUnavailable %v with three items", got, err, c.missing)
			}
			if !reflect.DeepEqual(p.asked, []string{evdTenant + "/" + evdNode}) {
				t.Errorf("the profile was asked %v", p.asked)
			}
		})
	}
	down := evdItem(1, evdLedger)
	down.Status = "UNAVAILABLE"
	got, err := evdRead(t, evdStoreOf(evdSeal(t, down)), &evdProfiles{rules: trustport.SourceRules{RequiredSources: []string{evdLedger}, MaxAgeSeconds: 300}})
	if err != nil || got.RequiredUnavailable || len(got.Items) != 1 || got.Items[0].Status != contracts.EnumsV1FactStatusUNAVAILABLE {
		t.Errorf("an UNAVAILABLE item: got %+v, %v; its status, not RequiredUnavailable, carries it", got, err)
	}
}

// INV-23, REQ-NFR-002: rules that cannot be used end the read with an error: a profile limit below one second (a profile that sets none is not a limit of zero), an empty
// source name, and a profile that fails. No answer is built on a limit the adapter had to invent (AGENT-16).
func TestEvidenceRefusesRulesItCannotUse(t *testing.T) {
	for name, rules := range map[string]trustport.SourceRules{
		"a limit of zero":  {MaxAgeSeconds: 0, RequiredSources: []string{evdLedger}},
		"a negative limit": {MaxAgeSeconds: -1},
		"an empty source":  {MaxAgeSeconds: 300, RequiredSources: []string{evdLedger, ""}},
		"no rules at all":  {},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := evdRead(t, evdStoreOf(evdWorld(t)), &evdProfiles{rules: rules})
			if !errors.Is(err, trustport.ErrSourceRules) {
				t.Errorf("err = %v, want ErrSourceRules", err)
			}
			evdNothing(t, got)
		})
	}
	got, err := evdRead(t, evdStoreOf(evdWorld(t)), &evdProfiles{err: errors.New("no profile for the node")})
	if !errors.Is(err, trustport.ErrSourceRules) {
		t.Errorf("a failing profile: err = %v, want ErrSourceRules", err)
	}
	evdNothing(t, got)
}

// INV-21: the same sealed snapshot and the same instant give the same answer, and the adapter changes nothing it was handed.
func TestEvidenceIsDeterministicAndLeavesItsInputAlone(t *testing.T) {
	s := evdWorld(t)
	before := evdWorld(t)
	store := evdStoreOf(s)
	a := evdAdapter(t, store, evdRules())
	first, err1 := a.SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	second, err2 := a.SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	if err1 != nil || err2 != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("first %+v, %v; second %+v, %v", first, err1, second, err2)
	}
	if !reflect.DeepEqual(s, before) {
		t.Errorf("the snapshot the store holds was changed")
	}
	if len(first.Items) == 0 {
		t.Fatal("no items")
	}
	first.Items[0].Freshness.AgeSeconds = 12345
	again, _ := a.SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	if len(again.Items) == 0 || again.Items[0].Freshness.AgeSeconds == 12345 {
		t.Errorf("an answer shares memory with the next")
	}
}

// evdTx records how the reader of the real store used its transaction. Anything else it is asked to do panics on the nil interface, except the one query that fails.
type evdTx struct {
	pgx.Tx
	rollbacks, commits int
	queryErr           error
}

func (t *evdTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, t.queryErr }
func (t *evdTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return evdRow{err: t.queryErr}
}
func (t *evdTx) Commit(context.Context) error   { t.commits++; return nil }
func (t *evdTx) Rollback(context.Context) error { t.rollbacks++; return nil }

type evdRow struct{ err error }

func (r evdRow) Scan(...any) error { return r.err }

type evdBegin struct {
	tx       *evdTx
	opts     []pgx.TxOptions
	deadline []time.Time
	err      error
}

func (b *evdBegin) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	b.opts = append(b.opts, o)
	d, _ := ctx.Deadline()
	b.deadline = append(b.deadline, d)
	if b.err != nil {
		return nil, b.err
	}
	return b.tx, nil
}

type evdLoginSet map[string]trustport.SnapshotTx

// evdNoTx is a login source that answers neither a transaction source nor an error.
type evdNoTx struct{}

func (evdNoTx) TxFor(string) (trustport.SnapshotTx, error) { return nil, nil }

func (l evdLoginSet) TxFor(tenantID string) (trustport.SnapshotTx, error) {
	if tx, ok := l[tenantID]; ok {
		return tx, nil
	}
	return nil, errors.New("no login for " + tenantID + " " + evdSecret)
}

// REQ-AUD-001, INV-11: the real reader works in one read-only transaction of the tenant's own login, at a snapshot isolation level so that the snapshot, its items and its intent
// are one view; it ends it by rollback, never a commit; it is bounded by its own timeout under the caller's deadline; and a failure of the database or of the login is
// ErrSnapshotStore with no database text, which is never a pass.
func TestEvidenceReaderUsesOneReadOnlyTransactionOfTheTenantsLogin(t *testing.T) {
	bad := errors.New("ERROR: relation evidence_item row for ten_bb " + evdSecret + " (SQLSTATE 42501)")
	b := &evdBegin{tx: &evdTx{queryErr: bad}}
	r, err := trustport.NewSealedSnapshots(evdLoginSet{evdTenant: b}, 750*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	caller := evdCtx(t)
	callerEnd, _ := caller.Deadline()
	_, err = r.Sealed(caller, evdTenant, evdSnap)
	if !errors.Is(err, trustport.ErrSnapshotStore) || !errors.Is(err, bad) || strings.Contains(err.Error(), evdSecret) || strings.Contains(err.Error(), "SQLSTATE") {
		t.Errorf("err = %v; want ErrSnapshotStore, its cause kept, no database text in it", err)
	}
	if len(b.opts) != 1 || b.opts[0].AccessMode != pgx.ReadOnly || b.opts[0].IsoLevel != pgx.RepeatableRead {
		t.Errorf("transaction options = %+v, want one read-only repeatable-read transaction", b.opts)
	}
	if b.tx.rollbacks != 1 || b.tx.commits != 0 {
		t.Errorf("rollbacks %d, commits %d; want one rollback and no commit", b.tx.rollbacks, b.tx.commits)
	}
	if len(b.deadline) != 1 || b.deadline[0].IsZero() || b.deadline[0].After(callerEnd.Add(-30*time.Second)) {
		t.Errorf("the transaction's deadline %v is not bounded by the reader's timeout", b.deadline)
	}

	_, err = r.Sealed(evdCtx(t), evdOther, evdSnap) // a tenant with no login of its own
	if !errors.Is(err, trustport.ErrSnapshotStore) || strings.Contains(err.Error(), evdSecret) || strings.Contains(err.Error(), evdOther) {
		t.Errorf("no login: err = %v, want ErrSnapshotStore with no tenant in its text", err)
	}
	none, _ := trustport.NewSealedSnapshots(evdNoTx{}, time.Second)
	if _, err := none.Sealed(evdCtx(t), evdTenant, evdSnap); !errors.Is(err, trustport.ErrSnapshotStore) {
		t.Errorf("a login source with no transaction source: err = %v, want ErrSnapshotStore", err)
	}
	b2 := &evdBegin{err: bad}
	r2, _ := trustport.NewSealedSnapshots(evdLoginSet{evdTenant: b2}, time.Second)
	if _, err := r2.Sealed(evdCtx(t), evdTenant, evdSnap); !errors.Is(err, trustport.ErrSnapshotStore) || strings.Contains(err.Error(), evdSecret) {
		t.Errorf("begin failure: err = %v", err)
	}
}

// AGENT-16: the reader has no default timeout and no default login.
func TestEvidenceReaderNeedsItsConfiguration(t *testing.T) {
	logins := evdLoginSet{}
	for name, build := range map[string]func() error{
		"no logins":    func() error { _, err := trustport.NewSealedSnapshots(nil, time.Second); return err },
		"zero timeout": func() error { _, err := trustport.NewSealedSnapshots(logins, 0); return err },
		"negative":     func() error { _, err := trustport.NewSealedSnapshots(logins, -time.Second); return err },
	} {
		if err := build(); !errors.Is(err, trustport.ErrConfig) {
			t.Errorf("%s: err = %v, want ErrConfig", name, err)
		}
	}
	if _, err := (trustport.SealedSnapshots{}).Sealed(evdCtx(t), evdTenant, evdSnap); !errors.Is(err, trustport.ErrConfig) {
		t.Errorf("the zero reader: err = %v, want ErrConfig", err)
	}
}
