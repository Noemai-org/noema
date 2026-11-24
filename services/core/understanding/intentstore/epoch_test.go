package intentstore_test

// A345 S1 (ruling of the Architect, 19 Nov 2026; migration 0033, owner C2 because A151 gave C2 the store; REQ-INT-001, REQ-SEC-004, REQ-AUD-001; INV-11, INV-17, INV-19, A151):
// `admitted_intent.logical_resolution_epoch`, "1 at creation; the increment path is later debt". The tests pin what is built and what is not: the column is an integer that is
// never NULL and is 1 on every row; Save writes it and the readers read it back; the database refuses any other value at insert and any update of the column (the table is
// insert-only, and an epoch that moved would be an increment path, which is not built); rows that existed before the migration become epoch 1. All data is SYNTHETIC.
//
// Written before the column exists: these fail first because `Admitted` has no `LogicalResolutionEpoch` and the table has no such column.
//
// Silences of the ruling, each read the safer way (A332): (1) a version N+1 of an intent also gets epoch 1 (the ruling says only "1 at creation"; no increment path exists);
// (2) the column has DEFAULT 1 and CHECK = 1, so a writer that does not know the column gets 1 and no writer can store another value before the increment path is designed;
// (3) Save refuses a caller-set epoch other than 0 (not stated) or 1.

import (
	"io/fs"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

const epochColumn = "logical_resolution_epoch"

func epochOf(t *testing.T, w *world, intentID string, version int) int {
	t.Helper()
	var e int
	if err := w.admin.QueryRow(ctx(t), `SELECT logical_resolution_epoch FROM admitted_intent WHERE intent_id = $1 AND version = $2`, intentID, version).Scan(&e); err != nil {
		t.Fatalf("read the epoch of %s@%d: %v", intentID, version, err)
	}
	return e
}

// rawRow stores an admitted intent as the administrator does, with no Record event: for the tests whose subject is the table or the readers, not the store.
func rawRow(t *testing.T, w *world, a intentstore.Admitted) {
	t.Helper()
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8::jsonb)`, a.TenantID, a.IntentID, a.Version, a.Supersedes, a.IntentNode, a.IntentHash, a.CorrelationID, string(a.JSON)); err != nil {
		t.Fatal(err)
	}
}

// A345 S1: the column is an integer and never NULL.
func TestTheEpochColumnIsAnIntegerThatIsNeverNull(t *testing.T) {
	w := newWorld(t)
	var typ, nullable string
	if err := w.admin.QueryRow(ctx(t), `SELECT data_type, is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'admitted_intent' AND column_name = $1`,
		epochColumn).Scan(&typ, &nullable); err != nil {
		t.Fatalf("the column does not exist: %v", err)
	}
	if typ != "integer" || nullable != "NO" {
		t.Errorf("%s is %s, nullable %s; want integer, NOT NULL", epochColumn, typ, nullable)
	}
}

// A345 S1: Save writes epoch 1 (the row, whatever the readers do).
func TestSaveWritesEpochOneAtCreation(t *testing.T) {
	w := newWorld(t)
	if _, err := w.store(tenantA).Save(ctx(t), actor, admitted(tenantA, "int_01J9X3", 1, corr1)); err != nil {
		t.Fatal(err)
	}
	if e := epochOf(t, w, "int_01J9X3", 1); e != 1 {
		t.Errorf("stored epoch %d, want 1", e)
	}
	got, err := w.reader("trust_rw", tenantA).Get(ctx(t), "int_01J9X3", 1)
	if err != nil || got.LogicalResolutionEpoch != 1 {
		t.Errorf("Get after Save: epoch %d, err %v; want 1", got.LogicalResolutionEpoch, err)
	}
}

// A345 S1: every reader reads the epoch back (Get and Latest; Trust, Execution and the orchestrator), and each version carries its own row's value.
func TestTheReadersReadTheEpochOfTheRow(t *testing.T) {
	w := newWorld(t)
	rawRow(t, w, admitted(tenantA, "int_01J9X3", 1, corr1))
	for _, group := range []string{"trust_rw", "execution_rw", "orchestrator_rw"} {
		got, err := w.reader(group, tenantA).Get(ctx(t), "int_01J9X3", 1)
		if err != nil || got.LogicalResolutionEpoch != 1 {
			t.Errorf("%s Get: epoch %d, err %v; want 1", group, got.LogicalResolutionEpoch, err)
		}
		latest, err := w.reader(group, tenantA).Latest(ctx(t), corr1)
		if err != nil || latest.LogicalResolutionEpoch != 1 {
			t.Errorf("%s Latest: epoch %d, err %v; want 1", group, latest.LogicalResolutionEpoch, err)
		}
	}
	// The reader takes the epoch from the column, not from a constant: a row stored with epoch 5 and a subject (as 0037 allows) reads as it is. The administrator makes it in a
	// database where the stored epoch is not 1; the immutability trigger is disabled for the test only.
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE admitted_intent DISABLE TRIGGER admitted_intent_immutable;
		UPDATE admitted_intent SET logical_resolution_epoch = 5, subject_ref = 'fin.transaction:TX0001'; ALTER TABLE admitted_intent ENABLE TRIGGER admitted_intent_immutable`); err != nil {
		t.Fatal(err)
	}
	got, err := w.reader("execution_rw", tenantA).Latest(ctx(t), corr1)
	if err != nil || got.LogicalResolutionEpoch != 5 {
		t.Errorf("Latest of an epoch-5 row: %d, err %v; want 5", got.LogicalResolutionEpoch, err)
	}
	got, err = w.reader("execution_rw", tenantA).Get(ctx(t), "int_01J9X3", 1)
	if err != nil || got.LogicalResolutionEpoch != 5 {
		t.Errorf("Get of an epoch-5 row: %d, err %v; want 5", got.LogicalResolutionEpoch, err)
	}
}

// Silence 1: the ruling says only "1 at creation". A version 2 of the same intent is a creation too, and no increment path exists, so it is 1.
func TestAVersionTwoOfTheSameIntentAlsoCarriesEpochOne(t *testing.T) {
	w := newWorld(t)
	s := w.store(tenantA)
	for _, a := range []intentstore.Admitted{admitted(tenantA, "int_01J9X3", 1, corr1), admitted(tenantA, "int_01J9X3", 2, corr1)} {
		if _, err := s.Save(ctx(t), actor, a); err != nil {
			t.Fatal(err)
		}
	}
	for v := 1; v <= 2; v++ {
		if e := epochOf(t, w, "int_01J9X3", v); e != 1 {
			t.Errorf("version %d: stored epoch %d, want 1", v, e)
		}
	}
	got, err := w.reader("execution_rw", tenantA).Latest(ctx(t), corr1)
	if err != nil || got.Version != 2 || got.LogicalResolutionEpoch != 1 {
		t.Errorf("Latest: version %d epoch %d, err %v; want version 2 epoch 1", got.Version, got.LogicalResolutionEpoch, err)
	}
}

// DEBT-54 part 2 (A520, A513), replacing silence 3 of A345: the epoch the caller states (from the case list, by the Interpreter) must be a positive integer the column can hold. A
// negative one, or one past the column's range, is refused before anything is appended (and before the database is asked). Zero means not stated and is stored as 1.
func TestSaveRefusesAnEpochThatIsNotAPositiveIntegerAndAppendsNothing(t *testing.T) {
	w := newWorld(t)
	s := w.store(tenantA)
	for _, bad := range []int{-1, -7, math.MaxInt32 + 1} {
		a := admitted(tenantA, "int_01J9X3", 1, corr1)
		a.SubjectRef = "fin.transaction:TX0001"
		a.LogicalResolutionEpoch = bad
		if _, err := s.Save(ctx(t), actor, a); err == nil || !strings.Contains(err.Error(), "epoch") {
			t.Errorf("Save of epoch %d: %v; want a refusal that names the epoch", bad, err)
		}
	}
	if w.count("admitted_intent") != 0 || w.count("record_event") != 0 {
		t.Errorf("rows %d events %d after refused saves, want none", w.count("admitted_intent"), w.count("record_event"))
	}
}

// Silence 3, the other half: 0 (not stated) and 1 are accepted, and both are stored as 1.
func TestSaveAcceptsAnEpochThatIsNotStatedOrOne(t *testing.T) {
	w := newWorld(t)
	s := w.store(tenantA)
	for i, ok := range []int{0, 1} {
		a := admitted(tenantA, "int_01J9X3", 1, corr1)
		if i == 1 {
			a = admitted(tenantA, "int_01J9X4", 1, "corr_c2")
		}
		a.LogicalResolutionEpoch = ok
		if _, err := s.Save(ctx(t), actor, a); err != nil {
			t.Errorf("Save refused epoch %d: %v", ok, err)
		}
		if e := epochOf(t, w, a.IntentID, 1); e != 1 {
			t.Errorf("epoch %d was stored as %d, want 1", ok, e)
		}
	}
}

// DEBT-54 part 2 (migration 0037 replaces the 0033 constraint that pinned the epoch to 1): whoever holds the orchestrator's login and skips the store cannot store an epoch that
// is not positive, a NULL epoch, or an epoch above 1 for an intent with no subject (a node with no declared subject has epoch 1). An epoch above 1 with a subject is stored.
func TestTheDatabaseRefusesAnEpochThatIsNotPositiveOrThatHasNoSubject(t *testing.T) {
	w := newWorld(t)
	insert := func(id string, epoch, subject any) error {
		x := admitted(tenantA, id, 1, "corr_"+id[len(id)-3:])
		_, err := w.as("orchestrator_rw", tenantA).Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json, logical_resolution_epoch, subject_ref)
			VALUES ($1, $2, $3, NULL, $4, $5, $6, $7::jsonb, $8, $9)`, x.TenantID, x.IntentID, x.Version, x.IntentNode, x.IntentHash, x.CorrelationID, string(x.JSON), epoch, subject)
		return err
	}
	for name, c := range map[string]struct{ epoch, subject any }{"zero": {0, "fin.transaction:TX0001"}, "negative": {-1, "fin.transaction:TX0001"}, "NULL": {nil, "fin.transaction:TX0001"},
		"two with no subject": {2, nil}, "an empty subject": {1, ""}, "a subject off its shape": {1, "txn synth"}} {
		if err := insert("int_bad", c.epoch, c.subject); err == nil {
			t.Errorf("the table stored %s", name)
		}
	}
	if w.count("admitted_intent") != 0 {
		t.Fatalf("a refused epoch left %d rows", w.count("admitted_intent"))
	}
	if err := insert("int_ok1", 1, nil); err != nil {
		t.Fatalf("control: epoch 1 with no subject must be stored: %v", err)
	}
	if err := insert("int_ok2", 2, "fin.transaction:TX0001"); err != nil {
		t.Fatalf("control: epoch 2 with a subject must be stored: %v", err)
	}
	// A writer that does not name the columns (the raw inserts of other tests) gets the defaults: epoch 1 and no subject.
	x := admitted(tenantA, "int_ep2", 1, "corr_ep2")
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json) VALUES ($1, $2, $3, NULL, $4, $5, $6, $7::jsonb)`,
		x.TenantID, x.IntentID, x.Version, x.IntentNode, x.IntentHash, x.CorrelationID, string(x.JSON)); err != nil {
		t.Fatal(err)
	}
	if e := epochOf(t, w, "int_ep2", 1); e != 1 {
		t.Errorf("default epoch %d, want 1", e)
	}
}

// A345 S1: the increment path is later debt. The table is insert-only, so an UPDATE of the epoch is refused for every role, the administrator included, and the row keeps 1.
func TestTheEpochIsNeverUpdated(t *testing.T) {
	w := newWorld(t)
	rawRow(t, w, admitted(tenantA, "int_01J9X3", 1, corr1))
	for name, run := range map[string]func() error{
		"the orchestrator": func() error {
			_, err := w.as("orchestrator_rw", tenantA).Exec(ctx(t), `UPDATE admitted_intent SET logical_resolution_epoch = 2`)
			return err
		},
		"trust": func() error {
			_, err := w.as("trust_rw", tenantA).Exec(ctx(t), `UPDATE admitted_intent SET logical_resolution_epoch = 2`)
			return err
		},
		"the administrator": func() error {
			_, err := w.admin.Exec(ctx(t), `UPDATE admitted_intent SET logical_resolution_epoch = logical_resolution_epoch + 1`)
			return err
		},
		"the administrator, the same value": func() error {
			_, err := w.admin.Exec(ctx(t), `UPDATE admitted_intent SET logical_resolution_epoch = 1`)
			return err
		},
	} {
		if err := run(); err == nil {
			t.Errorf("%s updated the epoch", name)
		}
	}
	if e := epochOf(t, w, "int_01J9X3", 1); e != 1 {
		t.Errorf("the epoch is %d after refused updates, want 1", e)
	}
	// No role holds UPDATE on the column, so the refusal does not rest on the trigger alone.
	for _, role := range []string{"orchestrator_rw", "trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "record_append"} {
		var ok bool
		if err := w.admin.QueryRow(ctx(t), `SELECT has_column_privilege($1, 'public.admitted_intent', $2, 'UPDATE')`, role, epochColumn).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Errorf("%s holds UPDATE on %s", role, epochColumn)
		}
	}
}

// migrationsWithout is the shipped migrations less the file of the given number: the database as it was before that migration.
func migrationsWithout(t *testing.T, numbers ...string) fs.FS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	dropped := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		skip := false
		for _, number := range numbers {
			if strings.HasPrefix(e.Name(), number+"_") {
				dropped++
				skip = true
			}
		}
		if skip {
			continue
		}
		b, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = &fstest.MapFile{Data: b}
	}
	if dropped != len(numbers) {
		t.Fatalf("migrations %v: %d of them exist", numbers, dropped)
	}
	return out
}

// A345 S1: a database that already holds admitted intents takes the migration and gives each row epoch 1 (the ruling's "1 at creation": they were created before the column existed).
// The immutability trigger never sees it: adding a column with a default is not an UPDATE.
func TestAnUpgradeGivesTheRowsAlreadyThereEpochOne(t *testing.T) {
	admin, _ := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrationsWithout(t, "0033", "0037")); err != nil {
		t.Fatalf("migrate before 0033: %v", err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, tenantA); err != nil {
		t.Fatal(err)
	}
	for _, a := range []intentstore.Admitted{admitted(tenantA, "int_old", 1, "corr_old"), admitted(tenantA, "int_old", 2, "corr_old")} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8::jsonb)`, a.TenantID, a.IntentID, a.Version, a.Supersedes, a.IntentNode, a.IntentHash, a.CorrelationID, string(a.JSON)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate 0033 onto a database that holds intents: %v", err)
	}
	rows, err := admin.Query(ctx(t), `SELECT version, logical_resolution_epoch FROM admitted_intent ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var v, e int
		if err := rows.Scan(&v, &e); err != nil {
			t.Fatal(err)
		}
		n++
		if e != 1 {
			t.Errorf("version %d has epoch %d after the upgrade, want 1", v, e)
		}
	}
	if n != 2 {
		t.Errorf("%d rows after the upgrade, want 2", n)
	}
}
