package intentstore_test

// DEBT-54 part 2 (ruling A520, 22 Nov 2026, interim; A513, A482; REQ-INT-001, REQ-SEC-004, REQ-AUD-001; INV-11, INV-17, INV-19; PERD 28.2 "It increments only when the prior logical
// action's case is verified CLOSED in the case system and a new intent is admitted" [REC]): migration 0037 replaces 0033's CHECK (epoch = 1) with the increment path's storage.
// `subject_ref` is the resolved subject of the intent (the Resource slot the pack names `subject_slot`, part 1), NULL for an intent with no resolved subject; the epoch may be above
// 1 only for an intent that has a subject. Save stores what the Interpreter derived from the case list, in the same transaction as the Record event (A482). The table stays insert-only.
// All data is SYNTHETIC. Written before the migration and the field exist.

import (
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

const subjectA = "fin.transaction:TX0001"

func subjectOf(t *testing.T, w *world, intentID string, version int) (ref *string) {
	t.Helper()
	if err := w.admin.QueryRow(ctx(t), `SELECT subject_ref FROM admitted_intent WHERE intent_id = $1 AND version = $2`, intentID, version).Scan(&ref); err != nil {
		t.Fatalf("read the subject of %s@%d: %v", intentID, version, err)
	}
	return ref
}

// A520, A482: Save stores the stated epoch and the subject with the row and the event in one transaction; Get and Latest read both back, for every reader login.
func TestSaveStoresTheStatedEpochAndSubjectAndTheReadersReadThemBack(t *testing.T) {
	w := newWorld(t)
	a := admitted(tenantA, "int_01J9X3", 1, corr1)
	a.LogicalResolutionEpoch, a.SubjectRef = 3, subjectA
	if _, err := w.store(tenantA).Save(ctx(t), actor, a); err != nil {
		t.Fatal(err)
	}
	if e := epochOf(t, w, "int_01J9X3", 1); e != 3 {
		t.Errorf("stored epoch %d, want 3", e)
	}
	if r := subjectOf(t, w, "int_01J9X3", 1); r == nil || *r != subjectA {
		t.Errorf("stored subject %v, want %s", r, subjectA)
	}
	for _, group := range []string{"trust_rw", "execution_rw", "orchestrator_rw"} {
		got, err := w.reader(group, tenantA).Get(ctx(t), "int_01J9X3", 1)
		if err != nil || got.LogicalResolutionEpoch != 3 || got.SubjectRef != subjectA {
			t.Errorf("%s Get: epoch %d subject %q err %v; want 3 and %s", group, got.LogicalResolutionEpoch, got.SubjectRef, err, subjectA)
		}
		latest, err := w.reader(group, tenantA).Latest(ctx(t), corr1)
		if err != nil || latest.LogicalResolutionEpoch != 3 || latest.SubjectRef != subjectA {
			t.Errorf("%s Latest: epoch %d subject %q err %v; want 3 and %s", group, latest.LogicalResolutionEpoch, latest.SubjectRef, err, subjectA)
		}
	}
}

// The part 1 reading: an intent with no resolved subject has epoch 1 and a NULL subject. Not stated (zero) is stored as 1; a subject that is not stated is stored as NULL.
func TestAnIntentWithNoSubjectIsStoredWithEpochOneAndNoSubject(t *testing.T) {
	w := newWorld(t)
	if _, err := w.store(tenantA).Save(ctx(t), actor, admitted(tenantA, "int_01J9X3", 1, corr1)); err != nil {
		t.Fatal(err)
	}
	if e := epochOf(t, w, "int_01J9X3", 1); e != 1 {
		t.Errorf("stored epoch %d, want 1", e)
	}
	if r := subjectOf(t, w, "int_01J9X3", 1); r != nil {
		t.Errorf("stored subject %q, want NULL", *r)
	}
	got, err := w.reader("trust_rw", tenantA).Get(ctx(t), "int_01J9X3", 1)
	if err != nil || got.SubjectRef != "" || got.LogicalResolutionEpoch != 1 {
		t.Errorf("Get: subject %q epoch %d err %v; want no subject and epoch 1", got.SubjectRef, got.LogicalResolutionEpoch, err)
	}
}

// A subject off its shape, and an epoch above 1 with no subject, are refused by Save before anything is appended (the database refuses them too; see epoch_test.go).
func TestSaveRefusesASubjectOffItsShapeAndAnEpochAboveOneWithNoSubject(t *testing.T) {
	w := newWorld(t)
	s := w.store(tenantA)
	for name, c := range map[string]struct {
		epoch   int
		subject string
		want    string
	}{
		"a space":                   {1, "fin.transaction:TX 0001", "subject"},
		"too long":                  {1, strings.Repeat("a", 65), "subject"},
		"a quote":                   {1, "fin.transaction:'x", "subject"},
		"epoch two with no subject": {2, "", "subject"},
	} {
		a := admitted(tenantA, "int_01J9X3", 1, corr1)
		a.LogicalResolutionEpoch, a.SubjectRef = c.epoch, c.subject
		if _, err := s.Save(ctx(t), actor, a); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v; want a refusal that names the %s", name, err, c.want)
		}
	}
	if w.count("admitted_intent") != 0 || w.count("record_event") != 0 {
		t.Errorf("rows %d events %d after refused saves, want none", w.count("admitted_intent"), w.count("record_event"))
	}
}

// A482: when the row cannot be stored the event is not either (one transaction); here the database refuses a duplicate key after the event was appended.
func TestAFailedInsertLeavesNeitherTheEventNorTheRowOfAnIncrementedEpoch(t *testing.T) {
	w := newWorld(t)
	s := w.store(tenantA)
	first := admitted(tenantA, "int_01J9X3", 1, corr1)
	first.LogicalResolutionEpoch, first.SubjectRef = 2, subjectA
	if _, err := s.Save(ctx(t), actor, first); err != nil {
		t.Fatal(err)
	}
	events := w.count("record_event")
	if _, err := s.Save(ctx(t), actor, first); err == nil {
		t.Fatal("the same intent version was stored twice")
	}
	if w.count("admitted_intent") != 1 || w.count("record_event") != events {
		t.Errorf("rows %d events %d after the refused duplicate, want 1 and %d", w.count("admitted_intent"), w.count("record_event"), events)
	}
}

// Upgrade: rows stored before 0037 keep epoch 1 and get no subject.
func TestAnUpgradeToTheIncrementPathKeepsEpochOneAndGivesNoSubject(t *testing.T) {
	admin, _ := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrationsWithout(t, "0037")); err != nil {
		t.Fatalf("migrate before 0037: %v", err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, tenantA); err != nil {
		t.Fatal(err)
	}
	a := admitted(tenantA, "int_old", 1, "corr_old")
	if _, err := admin.Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json)
		VALUES ($1, $2, $3, NULL, $4, $5, $6, $7::jsonb)`, a.TenantID, a.IntentID, a.Version, a.IntentNode, a.IntentHash, a.CorrelationID, string(a.JSON)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate 0037 onto a database that holds intents: %v", err)
	}
	var e int
	var r *string
	if err := admin.QueryRow(ctx(t), `SELECT logical_resolution_epoch, subject_ref FROM admitted_intent WHERE intent_id = 'int_old'`).Scan(&e, &r); err != nil {
		t.Fatal(err)
	}
	if e != 1 || r != nil {
		t.Errorf("after the upgrade epoch %d subject %v, want 1 and NULL", e, r)
	}
}
