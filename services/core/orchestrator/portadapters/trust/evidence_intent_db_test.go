package trustport_test

// A329 (DEBT-52 follow-up, the Integration Lead's note on noema#740): the intent the Evidence adapter attributes a sealed snapshot to is the latest of its correlation's chain, read
// as `ORDER BY version DESC LIMIT 1` over admitted_intent (migration 0026). These tests put that query's contract in the database itself, so that a change to the schema or to the
// query is caught: two intents of ONE correlation with DIFFERENT intent nodes give the later one, and an intent of another correlation, or of another tenant, is never the answer.
// Same real database and helpers as evidence_db_test.go. All data is SYNTHETIC (AGENT-25). Proves REQ-TRU-001, REQ-TRU-003, REQ-SEC-004; INV-11, INV-17.

import (
	"errors"
	"fmt"
	"testing"
	"time"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const (
	evdNodeFirst  = "fin.payments.exception.triage"
	evdNodeLater  = "fin.payments.exception.investigate"
	evdNodeThird  = "fin.payments.exception.escalate"
	evdNodeOtherC = "fin.payments.exception.close"
)

// intentOf stores one version of an intent chain as the migrating login, with an explicit admission time and node, for any tenant and correlation. Version 1 supersedes nothing;
// version n names n-1 of the same intent, as the table requires.
func (w *evdPG) intentOf(t *testing.T, tenant, corr, intentID string, version int, node string, at time.Time) {
	t.Helper()
	sup, supArg := "", any(nil)
	if version > 1 {
		sup = fmt.Sprintf(`,"supersedes":"%s@%d"`, intentID, version-1)
		supArg = fmt.Sprintf("%s@%d", intentID, version-1)
	}
	doc := fmt.Sprintf(`{"intent_id":%q,"tenant_id":%q,"correlation_id":%q,"version":%d,"status":"VALIDATED","intent_node":%q%s}`, intentID, tenant, corr, version, node, sup)
	if _, err := w.admin.Exec(evdCtx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9)`, tenant, intentID, version, supArg, node, evdHash(800+version), corr, doc, at); err != nil {
		t.Fatalf("intent: %v", err)
	}
}

// sealedItems writes the three usual items for tenant A and seals them, as evdCase does without its intents.
func (w *evdPG) sealedItems(t *testing.T, tenant string) {
	t.Helper()
	var items []trustport.SealedItem
	for i, src := range []string{evdLedger, evdSwitch, evdSettle} {
		items = append(items, w.item(t, tenant, evdItem(i+1, src), 600, evdPayload(src, 0)))
	}
	w.seal(t, tenant, evdSnap, items)
}

func (w *evdPG) reader(t *testing.T) trustport.SealedSnapshots {
	t.Helper()
	r, err := trustport.NewSealedSnapshots(evdPools(w.trust), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// REQ-TRU-001, INV-17: a chain of two intents of one correlation with different nodes. The later version is the intent of the snapshot, so its node is the one the trust profile is asked
// for, and the earlier version is not. Around the chain: a later intent of ANOTHER correlation of the same tenant, with a node of its own, and tenant B's chain for the SAME correlation
// ID with a higher version: neither may be the answer.
func TestEvidenceDBTheLaterOfTwoIntentsOfOneCorrelationIsAttributed(t *testing.T) {
	w := evdNewPG(t)
	w.intentOf(t, evdTenant, evdCorr, "int_0001", 1, evdNodeFirst, evdNow.Add(-6*time.Minute))
	w.intentOf(t, evdTenant, evdCorr, "int_0001", 2, evdNodeLater, evdNow.Add(-5*time.Minute))
	w.intentOf(t, evdTenant, "corr_ZZ99", "int_0009", 1, evdNodeOtherC, evdNow.Add(-4*time.Minute))
	w.intentOf(t, evdOther, evdCorr, "int_0001", 1, evdNodeFirst, evdNow.Add(-7*time.Minute))
	w.intentOf(t, evdOther, evdCorr, "int_0001", 2, evdNodeOtherC, evdNow.Add(-6*time.Minute))
	w.intentOf(t, evdOther, evdCorr, "int_0001", 3, evdNodeThird, evdNow.Add(-1*time.Minute))
	w.sealedItems(t, evdTenant)

	got, err := w.reader(t).Sealed(evdCtx(t), evdTenant, evdSnap)
	if err != nil {
		t.Fatal(err)
	}
	if want := (trustport.SealedIntent{IntentID: "int_0001", Version: 2, IntentNode: evdNodeLater, AdmittedAt: evdNow.Add(-5 * time.Minute)}); got.Intent.IntentID != want.IntentID ||
		got.Intent.Version != want.Version || got.Intent.IntentNode != want.IntentNode || !got.Intent.AdmittedAt.Equal(want.AdmittedAt) {
		t.Errorf("the reader's intent = %+v, want %+v", got.Intent, want)
	}

	// Through the adapter: the version is the later one, and the profile of the later node is the one asked, once.
	profiles := evdRules()
	a, err := trustport.NewEvidence(w.reader(t), profiles, clock.NewManual(evdNow), w.facts(t, evdPools(w.trust)))
	if err != nil {
		t.Fatal(err)
	}
	ev, err := a.SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	if err != nil || ev.IntentID != "int_0001" || ev.IntentVersion != 2 {
		t.Fatalf("attribution = %q v%d, %v; want int_0001 v2", ev.IntentID, ev.IntentVersion, err)
	}
	if want := []string{evdTenant + "/" + evdNodeLater}; fmt.Sprint(profiles.asked) != fmt.Sprint(want) {
		t.Errorf("the trust profile was asked for %v, want %v", profiles.asked, want)
	}
}

// REQ-TRU-001: the later of the chain is the highest VERSION, as the reader's comment says, whatever its stored admission time. A version 3 stored with an earlier time than version 2
// is still the latest, so a reader that ordered by the time would give version 2.
func TestEvidenceDBTheLatestIntentIsTheHighestVersionNotTheLatestTime(t *testing.T) {
	w := evdNewPG(t)
	w.intentOf(t, evdTenant, evdCorr, "int_0001", 1, evdNodeFirst, evdNow.Add(-6*time.Minute))
	w.intentOf(t, evdTenant, evdCorr, "int_0001", 2, evdNodeLater, evdNow.Add(-2*time.Minute))
	w.intentOf(t, evdTenant, evdCorr, "int_0001", 3, evdNodeThird, evdNow.Add(-5*time.Minute))
	w.sealedItems(t, evdTenant)
	got, err := w.reader(t).Sealed(evdCtx(t), evdTenant, evdSnap)
	if err != nil {
		t.Fatal(err)
	}
	if got.Intent.Version != 3 || got.Intent.IntentNode != evdNodeThird {
		t.Errorf("the reader's intent = v%d %q, want v3 %q", got.Intent.Version, got.Intent.IntentNode, evdNodeThird)
	}
}

// REQ-TRU-001, A226: a correlation with no admitted intent gives the zero intent and no error, and the adapter refuses to attribute the snapshot, whatever intents other correlations
// and tenants hold.
func TestEvidenceDBACorrelationWithNoIntentIsNotAttributedToAnotherOnes(t *testing.T) {
	w := evdNewPG(t)
	w.intentOf(t, evdTenant, "corr_ZZ99", "int_0009", 1, evdNodeOtherC, evdNow.Add(-4*time.Minute))
	w.intentOf(t, evdOther, evdCorr, "int_0001", 1, evdNodeFirst, evdNow.Add(-7*time.Minute))
	w.sealedItems(t, evdTenant)
	got, err := w.reader(t).Sealed(evdCtx(t), evdTenant, evdSnap)
	if err != nil || got.Intent != (trustport.SealedIntent{}) {
		t.Errorf("the reader's intent = %+v, %v; want the zero intent", got.Intent, err)
	}
	a, err := trustport.NewEvidence(w.reader(t), evdRules(), clock.NewManual(evdNow), w.facts(t, evdPools(w.trust)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SnapshotFreshness(evdCtx(t), evdTenant, evdSnap); !errors.Is(err, trustport.ErrSnapshotIntent) {
		t.Errorf("err = %v, want ErrSnapshotIntent", err)
	}
}
