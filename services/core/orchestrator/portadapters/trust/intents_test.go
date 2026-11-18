package trustport_test

// The production adapter of intake.Intents (STM-01 follow-up; REQ-TRU-003, REQ-AMB-001, REQ-SEC-004; INV-06, INV-11, INV-12, INV-17): the stored intent read as the session's tenant,
// its latest version, and the check that it still verifies. Real PostgreSQL, real gate, real store. SYNTHETIC.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const (
	intentID = "int_01J9X3"
	target   = "fin.transaction:TX88213"
)

// A resolved intent is read back as the tenant's own: the columns, the principal the document names (the session's, written by the gate), the one target of the owned slot, and the
// view T7 admits it by.
func TestGetReadsTheTenantsOwnAdmittedIntentWithItsViewAndTarget(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	adm := w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	got, err := w.intents(w.trustPools(), pack).Get(tctx(t), tenantA, intentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != tenantA || got.IntentID != intentID || got.Version != 1 || got.IntentNode != "fin.payments.exception.investigate" || got.IntentHash != adm.Hash() ||
		got.CorrelationID != corrOf(intentID) || got.PrincipalID != user || got.TargetRef != target {
		t.Errorf("got %+v", got)
	}
	want := decide.IntentView{Version: 1, RequiredSlots: map[string]decide.SlotStatus{"subject_resource": decide.SlotResolved, "question_type": decide.SlotResolved}}
	if got.View.Version != want.Version || len(got.View.MaterialFlags) != 0 || len(got.View.RequiredSlots) != 2 || got.View.RequiredSlots["subject_resource"] != decide.SlotResolved ||
		got.View.RequiredSlots["question_type"] != decide.SlotResolved {
		t.Errorf("view %+v, want %+v", got.View, want)
	}
	if _, ok := decide.Admit(got.View).Resolved(); !ok {
		t.Error("a resolved intent is not admitted as resolved")
	}
}

// What the pack requires and the document does not give is not RESOLVED (INV-06): a required slot that is ambiguous, one that is missing, and any ambiguity flag all make the intent
// unresolved, and an unresolved target names no target.
func TestGetViewsAnAmbiguousAbsentOrFlaggedIntentAsUnresolved(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, "int_amb", 1, func(m map[string]any) {
		m["slots"] = map[string]any{"subject_resource": map[string]any{"type": "Resource", "status": "AMBIGUOUS", "candidates_ref": "cns_01J9X3"}}
		m["ambiguity_flags"] = []any{"MULTIPLE_TRANSACTIONS_MATCH"}
	}))
	s := w.intents(w.trustPools(), pack)
	got, err := s.Get(tctx(t), tenantA, "int_amb", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetRef != "" || got.View.RequiredSlots["subject_resource"] != decide.SlotAmbiguous || got.View.RequiredSlots["question_type"] != decide.SlotAbsent ||
		!slices.Equal(got.View.MaterialFlags, []string{"MULTIPLE_TRANSACTIONS_MATCH"}) {
		t.Errorf("view %+v target %q", got.View, got.TargetRef)
	}
	if _, ok := decide.Admit(got.View).Unresolved(); !ok {
		t.Error("an ambiguous intent is admitted as resolved")
	}
	// A flag alone, with every required slot resolved, is enough.
	w.save(pack, tenantA, proposal(tenantA, "int_flag", 1, func(m map[string]any) { m["ambiguity_flags"] = []any{"TIME_VAGUE"} }))
	flagged, err := s.Get(tctx(t), tenantA, "int_flag", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decide.Admit(flagged.View).Unresolved(); !ok {
		t.Errorf("a flagged intent is admitted as resolved: %+v", flagged.View)
	}
}

// Another tenant's intent and an intent that does not exist are the same answer (REQ-TRU-003), whichever login asks.
func TestGetOfAnotherTenantsOrAnAbsentIntentIsNotFound(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantB, proposal(tenantB, "int_bonly", 1, nil))
	s := w.intents(w.trustPools(), pack)
	for name, call := range map[string]func() error{
		"tenant A asks for tenant B's intent": func() error { _, err := s.Get(tctx(t), tenantA, "int_bonly", 1); return err },
		"an intent that does not exist":       func() error { _, err := s.Get(tctx(t), tenantA, "int_nobody", 1); return err },
		"a version that does not exist":       func() error { _, err := s.Get(tctx(t), tenantB, "int_bonly", 2); return err },
		"latest of another tenant's intent":   func() error { _, err := s.LatestVersion(tctx(t), tenantA, "int_bonly"); return err },
		"latest of an absent intent":          func() error { _, err := s.LatestVersion(tctx(t), tenantA, "int_nobody"); return err },
	} {
		if err := call(); !errors.Is(err, intake.ErrNotFound) {
			t.Errorf("%s: got %v, want ErrNotFound", name, err)
		}
	}
	// A login that is not the tenant's, handed out for the tenant by mistake, still sees nothing of it: the database binds the rows to the login's tenant (A136).
	wrong := w.intents(trustport.PoolsOf(map[string]*pgxpool.Pool{tenantB: w.as("trust_rw", tenantA)}), pack)
	if _, err := wrong.Get(tctx(t), tenantB, "int_bonly", 1); !errors.Is(err, intake.ErrNotFound) {
		t.Errorf("tenant A's login asked for tenant B's intent: got %v, want ErrNotFound", err)
	}
}

// The adapter binds the tenant itself, whatever login it is given: an administrator's connection (no row-level security) asked for another tenant's intent finds nothing.
func TestTheAdapterBindsTheTenantWithoutHelpFromRowLevelSecurity(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantB, proposal(tenantB, "int_bonly", 1, nil))
	s := w.intents(trustport.PoolsOf(map[string]*pgxpool.Pool{tenantA: w.admin}), pack)
	if _, err := s.Get(tctx(t), tenantA, "int_bonly", 1); !errors.Is(err, intake.ErrNotFound) {
		t.Errorf("Get: got %v, want ErrNotFound", err)
	}
	if _, err := s.LatestVersion(tctx(t), tenantA, "int_bonly"); !errors.Is(err, intake.ErrNotFound) {
		t.Errorf("LatestVersion: got %v, want ErrNotFound", err)
	}
	a := intake.AdmittedIntent{TenantID: tenantA, IntentID: "int_bonly", Version: 1}
	if err := s.Verify(tctx(t), a); !errors.Is(err, intake.ErrInvalid) {
		t.Errorf("Verify: got %v, want ErrInvalid", err)
	}
}

func TestLatestVersionIsTheNewestOfTheTenantsOwnIntent(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	s := w.intents(w.trustPools(), pack)
	if v, err := s.LatestVersion(tctx(t), tenantA, intentID); err != nil || v != 1 {
		t.Fatalf("one version: %d %v", v, err)
	}
	w.save(pack, tenantA, proposal(tenantA, intentID, 2, nil))
	if v, err := s.LatestVersion(tctx(t), tenantA, intentID); err != nil || v != 2 {
		t.Fatalf("two versions: %d %v", v, err)
	}
	// Tenant B has an intent of the same ID: A's latest is A's.
	w.save(pack, tenantB, proposal(tenantB, intentID, 1, nil))
	w.save(pack, tenantB, proposal(tenantB, intentID, 2, nil))
	w.save(pack, tenantB, proposal(tenantB, intentID, 3, nil))
	if v, err := s.LatestVersion(tctx(t), tenantA, intentID); err != nil || v != 2 {
		t.Errorf("A's latest with B holding three: %d %v", v, err)
	}
}

// Verify: a stored intent that is what the gate admitted passes; the check is the store's own (the hash is the document's, the row is the document's).
func TestVerifyPassesAStoredIntentAndRefusesAHashThatIsNotItsDocuments(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	s := w.intents(w.trustPools(), pack)
	got, err := s.Get(tctx(t), tenantA, intentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(tctx(t), got); err != nil {
		t.Fatalf("a stored intent does not verify: %v", err)
	}
	// A row whose hash is not its document's (written around the store, by the administrator): the database does not compare them, the check does.
	if _, err := w.admin.Exec(tctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, intent_node, intent_hash, correlation_id, json)
		SELECT tenant_id, 'int_forged', 1, intent_node, 'sha256:' || repeat('0', 64), 'corr_forged',
		jsonb_set(jsonb_set(json, '{intent_id}', '"int_forged"'), '{correlation_id}', '"corr_forged"') FROM admitted_intent WHERE intent_id = $1`, intentID); err != nil {
		t.Fatal(err)
	}
	forged, err := s.Get(tctx(t), tenantA, "int_forged", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(tctx(t), forged); !errors.Is(err, intake.ErrInvalid) {
		t.Errorf("a hash that is not the document's: got %v, want ErrInvalid", err)
	}
}

// Verify holds the AdmittedIntent Trust carries to the stored one: a changed hash, target, principal, node, correlation or view is not the stored intent (INV-12, INV-17), and an
// intent the store does not hold does not verify.
func TestVerifyRefusesAnAdmittedIntentThatIsNotTheStoredOne(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	s := w.intents(w.trustPools(), pack)
	got, err := s.Get(tctx(t), tenantA, intentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(a *intake.AdmittedIntent){
		"another hash":        func(a *intake.AdmittedIntent) { a.IntentHash = "sha256:" + strings.Repeat("1", 64) },
		"another target":      func(a *intake.AdmittedIntent) { a.TargetRef = "fin.transaction:TX00001" },
		"no target":           func(a *intake.AdmittedIntent) { a.TargetRef = "" },
		"another principal":   func(a *intake.AdmittedIntent) { a.PrincipalID = "usr_99999" },
		"another node":        func(a *intake.AdmittedIntent) { a.IntentNode = "fin.meta.out_of_scope" },
		"another correlation": func(a *intake.AdmittedIntent) { a.CorrelationID = "corr_other" },
		"a flag added":        func(a *intake.AdmittedIntent) { a.View.MaterialFlags = []string{"X"} },
		"a slot changed": func(a *intake.AdmittedIntent) {
			a.View.RequiredSlots = map[string]decide.SlotStatus{"subject_resource": decide.SlotAmbiguous, "question_type": decide.SlotResolved}
		},
		"a version that is not stored": func(a *intake.AdmittedIntent) { a.Version = 2; a.View.Version = 2 },
		"another tenant":               func(a *intake.AdmittedIntent) { a.TenantID = tenantB },
	} {
		a := got
		a.View.RequiredSlots = map[string]decide.SlotStatus{"subject_resource": got.View.RequiredSlots["subject_resource"], "question_type": got.View.RequiredSlots["question_type"]}
		mutate(&a)
		if err := s.Verify(tctx(t), a); !errors.Is(err, intake.ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
}

// A store that cannot be read is an error, never a refusal: not ErrNotFound (T1 would REJECT), not ErrInvalid, and never a version or an intent.
func TestAStoreFailureIsAnErrorAndNeverANotFoundOrAnInvalid(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	s := w.intents(w.trustPools(), pack)
	got, err := s.Get(tctx(t), tenantA, intentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	// A login that has lost its right to read the table: the database answers with an error, not with no row.
	if _, err := w.admin.Exec(tctx(t), `REVOKE SELECT ON admitted_intent FROM trust_rw`); err != nil {
		t.Fatal(err)
	}
	notRefusal := func(name string, err error) {
		t.Helper()
		if err == nil || errors.Is(err, intake.ErrNotFound) || errors.Is(err, intake.ErrInvalid) || !errors.Is(err, trustport.ErrStore) {
			t.Errorf("%s: got %v, want ErrStore and neither ErrNotFound nor ErrInvalid", name, err)
		}
	}
	_, err = s.Get(tctx(t), tenantA, intentID, 1)
	notRefusal("Get", err)
	_, err = s.LatestVersion(tctx(t), tenantA, intentID)
	notRefusal("LatestVersion", err)
	notRefusal("Verify", s.Verify(tctx(t), got))
}

// The context's deadline is respected: an expired or cancelled context is that error, not a refusal.
func TestAnExpiredOrCancelledContextIsThatErrorAndNotARefusal(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	s := w.intents(w.trustPools(), pack)
	got, err := s.Get(tctx(t), tenantA, intentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, c := range map[string]struct {
		ctx  context.Context
		want error
	}{"expired": {expired, context.DeadlineExceeded}, "cancelled": {cancelled, context.Canceled}} {
		_, err := s.Get(c.ctx, tenantA, intentID, 1)
		if !errors.Is(err, c.want) || errors.Is(err, intake.ErrNotFound) {
			t.Errorf("Get, %s: got %v", name, err)
		}
		_, err = s.LatestVersion(c.ctx, tenantA, intentID)
		if !errors.Is(err, c.want) || errors.Is(err, intake.ErrNotFound) {
			t.Errorf("LatestVersion, %s: got %v", name, err)
		}
		err = s.Verify(c.ctx, got)
		if !errors.Is(err, c.want) || errors.Is(err, intake.ErrInvalid) {
			t.Errorf("Verify, %s: got %v", name, err)
		}
	}
}

// A tenant with no login is an error, and no other tenant's login is used in its place.
func TestATenantWithNoLoginIsAnErrorAndNeverFallsBackToAnotherLogin(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	s := w.intents(trustport.PoolsOf(map[string]*pgxpool.Pool{tenantA: w.as("trust_rw", tenantA)}), pack)
	if _, err := s.Get(tctx(t), tenantB, intentID, 1); !errors.Is(err, trustport.ErrNoPool) || errors.Is(err, intake.ErrNotFound) {
		t.Errorf("Get: got %v, want ErrNoPool", err)
	}
	if _, err := s.LatestVersion(tctx(t), tenantB, intentID); !errors.Is(err, trustport.ErrNoPool) {
		t.Errorf("LatestVersion: got %v, want ErrNoPool", err)
	}
}

// A stored intent the loaded pack does not describe (another pack version, or a node it does not know) is not usable: an error, so Trust ends UNKNOWN, never a guessed view.
func TestAStoredIntentTheLoadedPackDoesNotDescribeIsNotUsable(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, intentID, 1, nil))
	newer := []byte(strings.Replace(string(pack), `"pack_version": "1.0.0"`, `"pack_version": "1.1.0"`, -1))
	if string(newer) == string(pack) {
		t.Fatal("the fixture pack has no version to change")
	}
	if _, err := w.intents(w.trustPools(), newer).Get(tctx(t), tenantA, intentID, 1); !errors.Is(err, trustport.ErrStoredIntent) {
		t.Errorf("another pack version: got %v, want ErrStoredIntent", err)
	}
	onlyOther := []byte(strings.Replace(string(pack), `"node_id": "fin.payments.exception.investigate"`, `"node_id": "fin.payments.exception.other"`, 1))
	if _, err := w.intents(w.trustPools(), onlyOther).Get(tctx(t), tenantA, intentID, 1); err == nil {
		t.Error("a node the pack does not describe was read")
	}
}

// miniPack is a pack with one node whose two Resource slots are required, and a trust profile that names the given slots as owned.
func miniPack(profile string, owned ...string) []byte {
	names := `["` + strings.Join(owned, `","`) + `"]`
	if len(owned) == 0 {
		names = `[]`
	}
	return []byte(`{"pack_id":"fin","pack_version":"1.0.0",
	  "intent_nodes":[{"node_id":"fin.test.node","pack":"fin","pack_version":"1.0.0","trust_profile":` + profile + `,"outcome_definitions":["fin.outcome.test"],
	    "slots":[{"name":"first_resource","type":"Resource","required":true},{"name":"second_resource","type":"Resource","required":false}]}],
	  "trust_profiles":[{"profile_id":"fin.trust.test","authority":{"principal_must_own":` + names + `,"roles_allowed":["customer"]}}],
	  "outcome_definitions":[{"outcome_id":"fin.outcome.test"}]}`)
}

func miniProposal(second map[string]any) func(m map[string]any) {
	return func(m map[string]any) {
		m["intent_node"] = "fin.test.node"
		m["desired_outcome_refs"] = []any{"fin.outcome.test"}
		slots := map[string]any{"first_resource": map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX1"}}
		if second != nil {
			slots["second_resource"] = second
		}
		m["slots"] = slots
	}
}

// The port has one TargetRef and T14 lets a step act on that target alone: two owned slots that resolve to two targets are not usable (fail closed), two that resolve to the same one are
// that one, and a slot the profile names that is not resolved names no target.
func TestTheTargetIsTheOneTargetTheOwnedSlotsResolveTo(t *testing.T) {
	w := newWorld(t)
	pack := miniPack(`"fin.trust.test"`, "first_resource", "second_resource")
	s := w.intents(w.trustPools(), pack)
	w.save(pack, tenantA, proposal(tenantA, "int_two", 1, miniProposal(map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX2"})))
	if _, err := s.Get(tctx(t), tenantA, "int_two", 1); !errors.Is(err, trustport.ErrStoredIntent) {
		t.Errorf("two targets: got %v, want ErrStoredIntent", err)
	}
	w.save(pack, tenantA, proposal(tenantA, "int_same", 1, miniProposal(map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": "fin.transaction:TX1"})))
	if got, err := s.Get(tctx(t), tenantA, "int_same", 1); err != nil || got.TargetRef != "fin.transaction:TX1" {
		t.Errorf("one target twice: %+v %v", got, err)
	}
	w.save(pack, tenantA, proposal(tenantA, "int_one", 1, miniProposal(map[string]any{"type": "Resource", "status": "UNRESOLVED"})))
	if got, err := s.Get(tctx(t), tenantA, "int_one", 1); err != nil || got.TargetRef != "fin.transaction:TX1" {
		t.Errorf("one resolved, one not: %+v %v", got, err)
	}
}

// A node with no trust profile (the pack's out-of-scope node) owns nothing: no target, no required slot.
func TestANodeWithNoTrustProfileNamesNoTarget(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	w.save(pack, tenantA, proposal(tenantA, "int_oos", 1, func(m map[string]any) {
		m["intent_node"] = "fin.meta.out_of_scope"
		m["slots"] = map[string]any{}
		m["desired_outcome_refs"] = []any{}
	}))
	got, err := w.intents(w.trustPools(), pack).Get(tctx(t), tenantA, "int_oos", 1)
	if err != nil || got.TargetRef != "" || len(got.View.RequiredSlots) != 0 {
		t.Errorf("%+v %v", got, err)
	}
}

// Pack data the adapter cannot trust: a profile the node names but the pack lacks, an owned slot the node does not have, a repeated node.
func TestPackDataThatNamesWhatItDoesNotHaveIsRefused(t *testing.T) {
	w := newWorld(t)
	for name, pack := range map[string][]byte{
		"a profile the pack lacks":             miniPack(`"fin.trust.missing"`, "first_resource"),
		"an owned slot the node does not have": miniPack(`"fin.trust.test"`, "no_such_slot"),
	} {
		if _, err := trustport.NewStoredIntents(w.trustPools(), pack, time.Second); !errors.Is(err, trustport.ErrPackData) {
			t.Errorf("%s: got %v, want ErrPackData", name, err)
		}
	}
	dup := []byte(strings.Replace(string(miniPack(`"fin.trust.test"`, "first_resource")), `"intent_nodes":[`, `"intent_nodes":[{"node_id":"fin.test.node"},`, 1))
	if _, err := trustport.NewStoredIntents(w.trustPools(), dup, time.Second); !errors.Is(err, trustport.ErrPackData) {
		t.Errorf("a repeated node: got %v, want ErrPackData", err)
	}
}

func TestTheConstructorRefusesWhatItCannotRunWith(t *testing.T) {
	w := newWorld(t)
	pack := fintechPack(t)
	good := w.trustPools()
	for name, c := range map[string]struct {
		pools   trustport.TenantPools
		pack    []byte
		timeout time.Duration
		want    error
	}{
		"no pools":           {nil, pack, time.Second, trustport.ErrConfig},
		"no timeout":         {good, pack, 0, trustport.ErrConfig},
		"a negative timeout": {good, pack, -time.Second, trustport.ErrConfig},
		"no pack":            {good, nil, time.Second, trustport.ErrPackData},
		"not JSON":           {good, []byte("x"), time.Second, trustport.ErrPackData},
		"data after":         {good, append(append([]byte{}, pack...), []byte(" {}")...), time.Second, trustport.ErrPackData},
		"no nodes":           {good, []byte(`{"pack_id":"fin","pack_version":"1.0.0","intent_nodes":[],"trust_profiles":[]}`), time.Second, trustport.ErrPackData},
	} {
		if _, err := trustport.NewStoredIntents(c.pools, c.pack, c.timeout); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}

func TestEveryCallRefusesWhatIsNotAnIntentOfATenant(t *testing.T) {
	w := newWorld(t)
	s := w.intents(w.trustPools(), fintechPack(t))
	if _, err := s.Get(tctx(t), "", intentID, 1); !errors.Is(err, trustport.ErrSession) {
		t.Errorf("no tenant: got %v, want ErrSession", err)
	}
	if _, err := s.Get(tctx(t), tenantA, "not an id", 1); !errors.Is(err, trustport.ErrIntentRef) {
		t.Errorf("a bad intent id: got %v, want ErrIntentRef", err)
	}
	if _, err := s.Get(tctx(t), tenantA, intentID, 0); !errors.Is(err, trustport.ErrIntentRef) {
		t.Errorf("version 0: got %v, want ErrIntentRef", err)
	}
	if _, err := s.LatestVersion(tctx(t), "", intentID); !errors.Is(err, trustport.ErrSession) {
		t.Errorf("latest, no tenant: got %v, want ErrSession", err)
	}
}
