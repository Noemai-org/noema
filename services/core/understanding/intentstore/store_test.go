package intentstore_test

// A151 (ruling of the Architect, 12 Nov 2026; REQ-INT-001, REQ-SEC-004, REQ-AUD-001; INV-11, INV-12, INV-17, A136): the admitted-intent store. One table, written by
// the orchestrator's login only, read by Trust and Execution, insert-only, tenant-bound by row-level security with FORCE, with the Record event INTENT_ADMITTED
// written first in the same transaction. All data is SYNTHETIC.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

const (
	node  = "test.intent.locate_transfer" // SYNTHETIC
	actor = "customer:usr_88213"
	corr1 = "corr_c1"
)

// admitted builds a valid admitted intent: the document, the hash over a stand-in canonical text, and the columns the table keeps.
func admitted(tenant, id string, version int, corr string) intentstore.Admitted {
	doc := map[string]any{"schema_version": "1.0", "intent_id": id, "version": version, "supersedes": nil, "tenant_id": tenant, "correlation_id": corr, "status": "VALIDATED",
		"intent_node": node}
	sup := ""
	if version > 1 {
		sup = fmt.Sprintf("%s@%d", id, version-1)
		doc["supersedes"] = sup
	}
	b, _ := json.Marshal(doc)
	sum := sha256.Sum256(b)
	return intentstore.Admitted{TenantID: tenant, IntentID: id, Version: version, Supersedes: sup, IntentNode: node, IntentHash: "sha256:" + hex.EncodeToString(sum[:]),
		CorrelationID: corr, JSON: b}
}

func record(t *testing.T, w *world, types ...string) []struct {
	EventID, WrittenBy, Actor, EventType, ObjectRef, Corr string
	Payload                                               map[string]any
} {
	t.Helper()
	rows, err := w.admin.Query(ctx(t), `SELECT event_id, written_by, actor, event_type, object_ref, correlation_id, payload::text FROM record_event ORDER BY tenant_id, seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []struct {
		EventID, WrittenBy, Actor, EventType, ObjectRef, Corr string
		Payload                                               map[string]any
	}
	for rows.Next() {
		var e struct {
			EventID, WrittenBy, Actor, EventType, ObjectRef, Corr string
			Payload                                               map[string]any
		}
		var p string
		if err := rows.Scan(&e.EventID, &e.WrittenBy, &e.Actor, &e.EventType, &e.ObjectRef, &e.Corr, &p); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(p), &e.Payload); err != nil {
			t.Fatal(err)
		}
		keep := len(types) == 0
		for _, ty := range types {
			keep = keep || e.EventType == ty
		}
		if keep {
			out = append(out, e)
		}
	}
	return out
}

// The table: insert-only, forced row-level security, and exactly the privileges A151 names (writer orchestrator_rw; Trust and Execution read).
func TestTheTableIsForcedInsertOnlyAndGrantedToTheNamedRolesAlone(t *testing.T) {
	w := newWorld(t)
	var rls, force bool
	if err := w.admin.QueryRow(ctx(t), `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'public.admitted_intent'::regclass`).Scan(&rls, &force); err != nil {
		t.Fatalf("the table does not exist: %v", err)
	}
	if !rls || !force {
		t.Errorf("row-level security enabled %v forced %v, want both (A136)", rls, force)
	}
	want := map[string]string{"orchestrator_rw": "INSERT,SELECT", "trust_rw": "SELECT", "execution_rw": "SELECT"}
	for _, role := range []string{"orchestrator_rw", "trust_rw", "execution_rw", "outcome_rw", "evidence_rw", "record_append", "record_read", "record_verify", "record_emitter"} {
		var got []string
		for _, p := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			var ok bool
			if err := w.admin.QueryRow(ctx(t), `SELECT has_table_privilege($1, 'public.admitted_intent', $2)`, role, p).Scan(&ok); err != nil {
				t.Fatal(err)
			}
			if ok {
				got = append(got, p)
			}
		}
		sort.Strings(got)
		if strings.Join(got, ",") != want[role] {
			t.Errorf("%s holds %v on admitted_intent, want %q", role, got, want[role])
		}
	}
	var pub bool
	if err := w.admin.QueryRow(ctx(t), `SELECT has_table_privilege('public', 'public.admitted_intent', 'SELECT')`).Scan(&pub); err != nil || pub {
		t.Errorf("PUBLIC can select from the table: %v %v", pub, err)
	}
}

// A151: the intent and INTENT_ADMITTED go in one transaction, the event first; the event is written by the orchestrator's login as core-workflows, for the customer.
func TestSaveStoresTheIntentAndAppendsIntentAdmittedInOneTransaction(t *testing.T) {
	w := newWorld(t)
	a := admitted(tenantA, "int_01J9X3", 1, corr1)
	ref, err := w.store(tenantA).Save(ctx(t), actor, a)
	if err != nil {
		t.Fatal(err)
	}
	evs := record(t, w)
	if len(evs) != 1 || evs[0].EventType != "INTENT_ADMITTED" {
		t.Fatalf("the Record holds %+v, want exactly one INTENT_ADMITTED", evs)
	}
	e := evs[0]
	if e.WrittenBy != "core-workflows" || e.Actor != actor || e.ObjectRef != "int_01J9X3" || e.Corr != corr1 || e.EventID != ref.EventID {
		t.Errorf("event %+v, ref %+v", e, ref)
	}
	// The payload keys the reconstruction reads (intent_node, intent_id) and the version and hash a reader checks against the table.
	if e.Payload["intent_node"] != node || e.Payload["intent_id"] != "int_01J9X3" || e.Payload["intent_hash"] != a.IntentHash || e.Payload["version"] != float64(1) {
		t.Errorf("payload %v", e.Payload)
	}
	got, err := w.reader("trust_rw", tenantA).Get(ctx(t), "int_01J9X3", 1)
	if err != nil || got.IntentHash != a.IntentHash || got.CorrelationID != corr1 || got.IntentNode != node || string(got.JSON) == "" {
		t.Errorf("Get: %+v %v", got, err)
	}
	if w.count("admitted_intent") != 1 {
		t.Errorf("%d rows", w.count("admitted_intent"))
	}
}

// Record first (EC-E16): if the event cannot be appended no row is stored; if the row is refused after the append, neither stands.
func TestARefusedAppendOrARefusedRowLeavesNeitherTheEventNorTheRow(t *testing.T) {
	w := newWorld(t)
	s := w.store(tenantA)
	a := admitted(tenantA, "int_01J9X3", 1, corr1)
	w.fail.set(true)
	if _, err := s.Save(ctx(t), actor, a); err == nil {
		t.Fatal("saved although the Record refused the event")
	}
	if w.count("admitted_intent") != 0 || w.count("record_event") != 0 {
		t.Errorf("rows %d events %d after a refused append", w.count("admitted_intent"), w.count("record_event"))
	}
	w.fail.set(false)
	if _, err := s.Save(ctx(t), actor, a); err != nil {
		t.Fatal(err)
	}
	// The same version again: the append succeeds, the INSERT is refused (primary key), and the whole transaction rolls back.
	before := w.count("record_event")
	if _, err := s.Save(ctx(t), actor, a); err == nil {
		t.Fatal("stored the same version twice")
	}
	if w.count("admitted_intent") != 1 || w.count("record_event") != before {
		t.Errorf("rows %d events %d (was %d) after a refused insert", w.count("admitted_intent"), w.count("record_event"), before)
	}
}

// The database refuses what the store would never send: every rule is in the table, so a caller that holds the orchestrator's login and skips the store cannot
// write a row that disagrees with its own document, breaks the version chain or names another tenant.
func TestTheDatabaseRefusesARowThatBreaksItsOwnRules(t *testing.T) {
	w := newWorld(t)
	insert := func(a intentstore.Admitted, mutate func(args map[string]any), login string, tenant string) error {
		args := map[string]any{"tenant_id": a.TenantID, "intent_id": a.IntentID, "version": a.Version, "supersedes": nilIfEmpty(a.Supersedes), "intent_node": a.IntentNode,
			"intent_hash": a.IntentHash, "correlation_id": a.CorrelationID, "json": string(a.JSON)}
		if mutate != nil {
			mutate(args)
		}
		_, err := w.as(login, tenant).Exec(ctx(t), `INSERT INTO admitted_intent (tenant_id, intent_id, version, supersedes, intent_node, intent_hash, correlation_id, json)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`, args["tenant_id"], args["intent_id"], args["version"], args["supersedes"], args["intent_node"], args["intent_hash"],
			args["correlation_id"], args["json"])
		return err
	}
	good := admitted(tenantA, "int_01J9X3", 1, corr1)
	if err := insert(good, nil, "orchestrator_rw", tenantA); err != nil {
		t.Fatalf("control: the orchestrator's own login must store a valid row: %v", err)
	}
	with := func(a intentstore.Admitted, edit func(m map[string]any)) intentstore.Admitted {
		var m map[string]any
		_ = json.Unmarshal(a.JSON, &m)
		edit(m)
		a.JSON, _ = json.Marshal(m)
		return a
	}
	v2 := admitted(tenantA, "int_01J9X3", 2, corr1)
	cases := map[string]func() error{
		"the same version again": func() error { return insert(good, nil, "orchestrator_rw", tenantA) },
		"a document with another intent id": func() error {
			return insert(with(admitted(tenantA, "int_other", 1, "corr_c2"), func(m map[string]any) { m["intent_id"] = "int_x" }), nil, "orchestrator_rw", tenantA)
		},
		"a document with another tenant": func() error {
			return insert(with(admitted(tenantA, "int_o2", 1, "corr_c2"), func(m map[string]any) { m["tenant_id"] = tenantB }), nil, "orchestrator_rw", tenantA)
		},
		"a document with another correlation": func() error {
			return insert(with(admitted(tenantA, "int_o3", 1, "corr_c3"), func(m map[string]any) { m["correlation_id"] = "corr_zz" }), nil, "orchestrator_rw", tenantA)
		},
		"a document with another version": func() error {
			return insert(with(admitted(tenantA, "int_o4", 1, "corr_c4"), func(m map[string]any) { m["version"] = 2 }), nil, "orchestrator_rw", tenantA)
		},
		"a document with a fractional version": func() error {
			return insert(with(admitted(tenantA, "int_o5", 1, "corr_c5"), func(m map[string]any) { m["version"] = 1.0 }), func(a map[string]any) {
				a["json"] = strings.Replace(a["json"].(string), `"version":1`, `"version":1.0`, 1)
			}, "orchestrator_rw", tenantA)
		},
		"a document whose status is not VALIDATED": func() error {
			return insert(with(admitted(tenantA, "int_o6", 1, "corr_c6"), func(m map[string]any) { m["status"] = "EXTRACTED" }), nil, "orchestrator_rw", tenantA)
		},
		"a document with another node": func() error {
			return insert(with(admitted(tenantA, "int_o7", 1, "corr_c7"), func(m map[string]any) { m["intent_node"] = "test.intent.other" }), nil, "orchestrator_rw", tenantA)
		},
		"a document with another supersedes": func() error {
			return insert(with(v2, func(m map[string]any) { m["supersedes"] = "int_01J9X3@7" }), nil, "orchestrator_rw", tenantA)
		},
		"a version 2 that supersedes another version, column and document agreeing": func() error {
			a := v2
			a.Supersedes = "int_01J9X3@7"
			return insert(with(a, func(m map[string]any) { m["supersedes"] = "int_01J9X3@7" }), func(m map[string]any) { m["supersedes"] = "int_01J9X3@7" }, "orchestrator_rw", tenantA)
		},
		"a version 1 that supersedes": func() error {
			a := admitted(tenantA, "int_o8", 1, "corr_c8")
			a.Supersedes = "int_o8@0"
			return insert(a, func(m map[string]any) {
				m["json"] = strings.Replace(string(a.JSON), `"supersedes":null`, `"supersedes":"int_o8@0"`, 1)
			}, "orchestrator_rw", tenantA)
		},
		"a version 3 that skips version 2": func() error {
			return insert(admitted(tenantA, "int_01J9X3", 3, corr1), nil, "orchestrator_rw", tenantA)
		},
		"a version 2 in another correlation": func() error {
			return insert(admitted(tenantA, "int_01J9X3", 2, "corr_other"), nil, "orchestrator_rw", tenantA)
		},
		"a second intent in the correlation": func() error {
			return insert(admitted(tenantA, "int_second", 1, corr1), nil, "orchestrator_rw", tenantA)
		},
		"a hash off shape": func() error {
			return insert(admitted(tenantA, "int_o9", 1, "corr_c9"), func(m map[string]any) { m["intent_hash"] = "sha256:ABC" }, "orchestrator_rw", tenantA)
		},
		"an intent id off shape": func() error {
			return insert(admitted(tenantA, "INT 1", 1, "corr_ca"), nil, "orchestrator_rw", tenantA)
		},
		"a row of tenant A by tenant B's login": func() error {
			return insert(admitted(tenantA, "int_ob", 1, "corr_cb"), nil, "orchestrator_rw", tenantB)
		},
		"a row by Trust's login": func() error { return insert(admitted(tenantA, "int_oc", 1, "corr_cc"), nil, "trust_rw", tenantA) },
		"a row by Execution's login": func() error {
			return insert(admitted(tenantA, "int_od", 1, "corr_cd"), nil, "execution_rw", tenantA)
		},
		"a row by Evidence's login": func() error {
			return insert(admitted(tenantA, "int_oe", 1, "corr_ce"), nil, "evidence_rw", tenantA)
		},
	}
	for name, run := range cases {
		if err := run(); err == nil {
			t.Errorf("%s: stored", name)
		}
	}
	if err := insert(v2, nil, "orchestrator_rw", tenantA); err != nil {
		t.Errorf("control: version 2 after version 1 of the same intent and correlation must store: %v", err)
	}
	if n := w.count("admitted_intent"); n != 2 {
		t.Errorf("%d rows, want 2 (versions 1 and 2)", n)
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Insert-only: no role, the administrator included, edits, deletes or truncates a stored intent.
func TestAStoredIntentIsNeverEditedDeletedOrTruncated(t *testing.T) {
	w := newWorld(t)
	if _, err := w.store(tenantA).Save(ctx(t), actor, admitted(tenantA, "int_01J9X3", 1, corr1)); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() error{
		"update by the orchestrator": func() error {
			_, err := w.as("orchestrator_rw", tenantA).Exec(ctx(t), `UPDATE admitted_intent SET intent_hash = 'sha256:' || repeat('0', 64)`)
			return err
		},
		"delete by the orchestrator": func() error {
			_, err := w.as("orchestrator_rw", tenantA).Exec(ctx(t), `DELETE FROM admitted_intent`)
			return err
		},
		"update by the administrator": func() error {
			_, err := w.admin.Exec(ctx(t), `UPDATE admitted_intent SET intent_hash = 'sha256:' || repeat('0', 64)`)
			return err
		},
		"delete by the administrator":   func() error { _, err := w.admin.Exec(ctx(t), `DELETE FROM admitted_intent`); return err },
		"truncate by the administrator": func() error { _, err := w.admin.Exec(ctx(t), `TRUNCATE admitted_intent`); return err },
	} {
		if err := run(); err == nil {
			t.Errorf("%s: allowed", name)
		}
	}
	if w.count("admitted_intent") != 1 {
		t.Error("the stored intent changed")
	}
}

// REQ-SEC-004, INV-12: a tenant's logins see its own intents and no other tenant's, and tenant B's orchestrator cannot read or write tenant A's.
func TestEachTenantSeesOnlyItsOwnIntents(t *testing.T) {
	w := newWorld(t)
	if _, err := w.store(tenantA).Save(ctx(t), actor, admitted(tenantA, "int_01J9X3", 1, corr1)); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"trust_rw", "execution_rw", "orchestrator_rw"} {
		if _, err := w.reader(group, tenantA).Get(ctx(t), "int_01J9X3", 1); err != nil {
			t.Errorf("%s of the tenant cannot read its own intent: %v", group, err)
		}
		if _, err := w.reader(group, tenantB).Get(ctx(t), "int_01J9X3", 1); !errors.Is(err, intentstore.ErrNotFound) {
			t.Errorf("%s of another tenant: err %v, want ErrNotFound", group, err)
		}
		var n int
		if err := w.as(group, tenantB).QueryRow(ctx(t), `SELECT count(*) FROM admitted_intent`).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s of tenant B counts %d rows (%v)", group, n, err)
		}
	}
	// outcome_rw and evidence_rw have no privilege at all.
	for _, group := range []string{"outcome_rw", "evidence_rw"} {
		if _, err := w.as(group, tenantA).Exec(ctx(t), `SELECT count(*) FROM admitted_intent`); err == nil {
			t.Errorf("%s can read the table", group)
		}
	}
}

// E3 reads the hash of the latest admitted intent of an investigation (noema#448 IntentSource): the highest version of the correlation.
func TestTheLatestHashOfAnInvestigationIsTheHighestVersions(t *testing.T) {
	w := newWorld(t)
	s := w.store(tenantA)
	v1 := admitted(tenantA, "int_01J9X3", 1, corr1)
	v2 := admitted(tenantA, "int_01J9X3", 2, corr1)
	for _, a := range []intentstore.Admitted{v1, v2} {
		if _, err := s.Save(ctx(t), actor, a); err != nil {
			t.Fatal(err)
		}
	}
	for _, group := range []string{"execution_rw", "trust_rw"} {
		got, err := w.reader(group, tenantA).Latest(ctx(t), corr1)
		if err != nil || got.Version != 2 || got.IntentHash != v2.IntentHash || got.IntentHash == v1.IntentHash {
			t.Errorf("%s: Latest %+v %v, want version 2", group, got, err)
		}
	}
	if _, err := w.reader("execution_rw", tenantA).Latest(ctx(t), "corr_unknown"); !errors.Is(err, intentstore.ErrNotFound) {
		t.Errorf("an unknown correlation: %v, want ErrNotFound", err)
	}
	if _, err := w.reader("execution_rw", tenantB).Latest(ctx(t), corr1); !errors.Is(err, intentstore.ErrNotFound) {
		t.Errorf("another tenant's reader: %v, want ErrNotFound", err)
	}
	if _, err := w.reader("execution_rw", tenantA).Get(ctx(t), "int_01J9X3", 3); !errors.Is(err, intentstore.ErrNotFound) {
		t.Errorf("an unknown version: %v, want ErrNotFound", err)
	}
}

// What the store refuses before it asks the database, with the Record untouched.
func TestTheStoreRefusesAnIncompleteSaveAndAppendsNothing(t *testing.T) {
	w := newWorld(t)
	s := w.store(tenantA)
	good := admitted(tenantA, "int_01J9X3", 1, corr1)
	bad := map[string]func(a *intentstore.Admitted, actor *string){
		"no actor":                    func(a *intentstore.Admitted, ac *string) { *ac = "" },
		"an actor off shape":          func(a *intentstore.Admitted, ac *string) { *ac = "usr_88213" },
		"no tenant":                   func(a *intentstore.Admitted, ac *string) { a.TenantID = "" },
		"a version of 0":              func(a *intentstore.Admitted, ac *string) { a.Version = 0 },
		"no hash":                     func(a *intentstore.Admitted, ac *string) { a.IntentHash = "" },
		"no document":                 func(a *intentstore.Admitted, ac *string) { a.JSON = nil },
		"a document that is not JSON": func(a *intentstore.Admitted, ac *string) { a.JSON = []byte("{") },
	}
	for name, mutate := range bad {
		a, ac := good, actor
		mutate(&a, &ac)
		if _, err := s.Save(ctx(t), ac, a); err == nil {
			t.Errorf("%s: saved", name)
		}
	}
	if w.count("record_event") != 0 || w.count("admitted_intent") != 0 {
		t.Errorf("events %d rows %d after refused saves", w.count("record_event"), w.count("admitted_intent"))
	}
}

// A reconstruction built from the Record serves the admitted intent in its REQUEST section (intent_node and intent_id), citing the event.
func TestTheReconstructionServesTheAdmittedIntent(t *testing.T) {
	w := newWorld(t)
	ref, err := w.store(tenantA).Save(ctx(t), actor, admitted(tenantA, "int_01J9X3", 1, corr1))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := reconstruct.Build(ctx(t), w.as("record_read", tenantA), tenantA, corr1)
	if err != nil {
		t.Fatalf("the Record does not reconstruct: %v", err)
	}
	var out struct {
		Sections []struct {
			Kind  string `json:"kind"`
			Items []struct {
				RecordEventID string         `json:"record_event_id"`
				Facts         map[string]any `json:"facts"`
			} `json:"items"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range out.Sections {
		for _, it := range s.Items {
			if s.Kind == "REQUEST" && it.RecordEventID == ref.EventID {
				found = true
				if b := string(mustJSON(it.Facts)); !strings.Contains(b, node) || !strings.Contains(b, "int_01J9X3") || strings.Contains(b, "sha256:") {
					t.Errorf("REQUEST facts %s", b)
				}
			}
		}
	}
	if !found {
		t.Error("the REQUEST section does not cite the INTENT_ADMITTED event")
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
