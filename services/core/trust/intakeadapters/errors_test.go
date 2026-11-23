package intakeadapters

// A347 (fixed public text of an error that crosses a package boundary; AGENT-13): an error of the adapters names neither the tenant, nor an identifier of the pack or of a fact (a
// clock, a profile, a node, a variable, an entity, a rule, a policy version), nor the database's message (SQLSTATE text, server and login details), nor a DSN or a path, nor the text of a
// dependency's error. The cause stays behind Unwrap, so errors.Is and errors.As still reach it, and the package's sentinels stay reachable with errors.Is. Every case provokes the failure
// on the shipped pack, the real policy engine and a real PostgreSQL publication store. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/policy"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

// leakyText is the kind of text a dependency's error can carry: a tenant, a login, a server, a path.
const leakyText = "boom for ten_aa at host=db.internal password=hunter2 /etc/noema/keys/trust.pem postgres://trust@db.internal/noema"

var errLeaky = errors.New(leakyText)

// commonLeaks is what no error text of the package may hold. The lower-cased text is searched.
var commonLeaks = []string{"ten_aa", "sqlstate", "error:", "fatal", "pgx", "pq", "postgres://", "host=", "password", "/etc/", "/tmp", ".pem", "127.0.0.1", "localhost",
	"policy_publication", "permission denied", "terminating", "closed pool", "hunter2", "db.internal", "boom", "zzmark"}

// assertFixed checks one error against the common leaks, the case's own identifiers, the package's prefix and the rule that the text does not hold its cause's text.
func assertFixed(t *testing.T, err error, kind error, own []string) {
	t.Helper()
	if err == nil {
		t.Fatal("no error, want one")
	}
	text := strings.ToLower(err.Error())
	for _, leak := range append(append([]string{}, commonLeaks...), own...) {
		if leak != "" && strings.Contains(text, strings.ToLower(leak)) {
			t.Errorf("the error text carries %q: %q", leak, err.Error())
		}
	}
	if !strings.HasPrefix(err.Error(), "intakeadapters: ") {
		t.Errorf("the error text %q does not start with the package's prefix", err.Error())
	}
	if cause := errors.Unwrap(err); cause != nil && strings.Contains(err.Error(), cause.Error()) {
		t.Errorf("the error text %q holds its cause's text %q", err.Error(), cause.Error())
	}
	if kind != nil && !errors.Is(err, kind) {
		t.Errorf("errors.Is(err, %v) is false: %v", kind, err)
	}
}

// the shipped pack's own identifiers, read from the file
func packIDs(t *testing.T) (clockID, profileID string) {
	t.Helper()
	var p struct {
		Profiles []struct {
			ID string `json:"profile_id"`
		} `json:"trust_profiles"`
	}
	if err := json.Unmarshal(realPack(t), &p); err != nil || len(p.Profiles) == 0 {
		t.Fatalf("the shipped pack has no trust profile: %v", err)
	}
	return string(packClock(t).ClockId), p.Profiles[0].ID
}

// A347: every error return site of the two pack loaders that carries an identifier or a decoder's text.
func TestEveryErrorOfThePackLoadersHasAFixedPublicText(t *testing.T) {
	clockID, profileID := packIDs(t)
	clock0 := func(m map[string]any) map[string]any { return m["clocks"].([]any)[0].(map[string]any) }
	policyPack := func(f func(m map[string]any)) []byte { return editPack(t, f) }
	type tc struct {
		name  string
		load  func() error
		own   []string
		cause bool // the decoder's or the engine's error is kept as the cause
	}
	cases := []tc{
		{"clocks: not JSON", func() error { _, err := LoadClockBook([]byte(`[`)); return err }, nil, true},
		{"clocks: a section does not decode", func() error {
			_, err := LoadClockBook(policyPack(func(m map[string]any) { m["trust_profiles"] = "zzmark_profiles" }))
			return err
		}, nil, true},
		{"clocks: a clock defined twice", func() error {
			_, err := LoadClockBook(policyPack(func(m map[string]any) { m["clocks"] = append(m["clocks"].([]any), m["clocks"].([]any)[0]) }))
			return err
		}, []string{clockID}, true},
		{"clocks: a clock that is not usable", func() error {
			_, err := LoadClockBook(policyPack(func(m map[string]any) { clock0(m)["duration_seconds"] = 0 }))
			return err
		}, []string{clockID}, true},
		{"clocks: a profile defined twice", func() error {
			_, err := LoadClockBook(policyPack(func(m map[string]any) {
				m["trust_profiles"] = append(m["trust_profiles"].([]any), m["trust_profiles"].([]any)[0])
			}))
			return err
		}, []string{profileID}, true},
		{"clocks: a profile naming a clock that is not defined", func() error {
			_, err := LoadClockBook(policyPack(func(m map[string]any) {
				m["trust_profiles"].([]any)[0].(map[string]any)["clocks"] = []any{"zzmark.clock.ghost"}
			}))
			return err
		}, []string{profileID, "zzmark.clock.ghost"}, true},
		{"clocks: a node defined twice", func() error {
			_, err := LoadClockBook(policyPack(func(m map[string]any) {
				m["intent_nodes"] = append(m["intent_nodes"].([]any), m["intent_nodes"].([]any)[0])
			}))
			return err
		}, []string{node}, true},
		{"clocks: a node naming a profile that is not defined", func() error {
			_, err := LoadClockBook(policyPack(func(m map[string]any) {
				m["intent_nodes"].([]any)[0].(map[string]any)["trust_profile"] = "zzmark.trust.ghost"
			}))
			return err
		}, []string{node, "zzmark.trust.ghost"}, true},
		{"policy: not JSON", func() error { _, err := LoadPolicyBook([]byte(`{`)); return err }, nil, true},
		{"policy: a section does not decode", func() error {
			_, err := LoadPolicyBook(policyPack(func(m map[string]any) { m["capabilities"] = "zzmark_capabilities" }))
			return err
		}, nil, true},
		{"policy: an entity described twice", func() error {
			_, err := LoadPolicyBook(policyPack(func(m map[string]any) {
				d := m["evidence_descriptors"].([]any)
				m["evidence_descriptors"] = append(d, d[0])
			}))
			return err
		}, []string{"transaction"}, true},
		{"policy: a field of an entity with a type that is not usable", func() error {
			_, err := LoadPolicyBook(policyPack(func(m map[string]any) {
				m["evidence_descriptors"].([]any)[0].(map[string]any)["fields"] = map[string]any{"zzmark_field": map[string]any{"type": "Blob"}}
			}))
			return err
		}, []string{"Blob"}, true},
		{"policy: a variable with a type that is not usable", func() error {
			_, err := LoadPolicyBook(policyPack(func(m map[string]any) {
				m["predicate_variables"].(map[string]any)["zzmark_variable"] = map[string]any{"type": "Blob"}
			}))
			return err
		}, []string{"Blob"}, true},
		{"policy: the document is refused by the engine", func() error {
			_, err := LoadPolicyBook(variantPack(t, "3.2", withRules(rule("zzmark.rule", "WORKFLOW", "AT_DECISION", "fin.cap.ghost", "true", "REJECT", "fin.reason.permitted", nil))))
			return err
		}, []string{"fin.cap.ghost"}, true},
		{"policy: a version given twice", func() error { _, err := LoadPolicyBook(realPack(t), realPack(t)); return err }, []string{"3.1"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.load()
			assertFixed(t, err, ErrPack, c.own)
			if c.cause && errors.Unwrap(err) == nil {
				t.Errorf("the cause is not kept behind Unwrap: %q", err.Error())
			}
		})
	}
}

// A347: the policy adapter, end to end on the real store, the real engine and the real pack.
func TestEveryErrorOfEvaluateHasAFixedPublicText(t *testing.T) {
	type tc struct {
		name  string
		kind  error
		own   []string
		cause func(t *testing.T, err error)
		run   func(t *testing.T, w *world) error
	}
	hasCause := func(t *testing.T, err error) {
		if errors.Unwrap(err) == nil {
			t.Errorf("the cause is not kept behind Unwrap: %q", err.Error())
		}
	}
	pgErr := func(t *testing.T, err error) {
		var pe *pgconn.PgError
		if !errors.As(err, &pe) {
			t.Errorf("the driver's PgError is not reachable: %v", err)
		}
	}
	factsEdit := func(capability string, edit func(FactSet) FactSet) func(t *testing.T, w *world) error {
		return func(t *testing.T, w *world) error {
			w.publish(t, tenantA, "3.1", t1)
			_, err := w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: resolvedMaster(), extra: edit}).Evaluate(ctx(t), query(capability, late))
			return err
		}
	}
	waitRule := func(params map[string]any) func(t *testing.T, w *world) error {
		return func(t *testing.T, w *world) error {
			w.publish(t, tenantA, "3.1", t1)
			b := book(t, variantPack(t, "3.1", withRules(rule("zzmark.hold", "WORKFLOW", "AT_DECISION", lookup, "true", "WAIT", "fin.reason.sla_at_risk", params))))
			_, err := w.policies(t, b, &adm{at: t1}, &facts{master: resolvedMaster()}).Evaluate(ctx(t), query(lookup, late))
			return err
		}
	}
	cases := []tc{
		{"the tenant's login cannot be had", nil, nil, hasCause, func(t *testing.T, w *world) error {
			p := &Policies{c: PoliciesConfig{Book: book(t, realPack(t)), Publications: StorePublications{DBs: failingFor{errLeaky}}, Admissions: &adm{at: t1}, Facts: &facts{master: resolvedMaster()}}}
			_, err := p.Evaluate(ctx(t), query(lookup, late))
			if !errors.Is(err, errLeaky) {
				t.Errorf("the cause is not reachable: %v", err)
			}
			return err
		}},
		{"the admission time cannot be read", nil, nil, hasCause, func(t *testing.T, w *world) error {
			_, err := w.policies(t, book(t, realPack(t)), &adm{err: errLeaky}, &facts{master: resolvedMaster()}).Evaluate(ctx(t), query(lookup, late))
			if !errors.Is(err, errLeaky) {
				t.Errorf("the cause is not reachable: %v", err)
			}
			return err
		}},
		{"the publications cannot be read: the pool is closed", nil, nil, hasCause, func(t *testing.T, w *world) error {
			w.logins[tenantA].Close()
			_, err := w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: resolvedMaster()}).Evaluate(ctx(t), query(lookup, late))
			return err
		}},
		{"the publications cannot be read: the login has no privilege", nil, nil, pgErr, func(t *testing.T, w *world) error {
			login := testpg.TenantLogin(t, w.admin, w.admin.Config().ConnString(), "outcome_rw", tenantA)
			p := &Policies{c: PoliciesConfig{Book: book(t, realPack(t)), Publications: StorePublications{DBs: fixedFor{login}}, Admissions: &adm{at: t1}, Facts: &facts{master: resolvedMaster()}}}
			_, err := p.Evaluate(ctx(t), query(lookup, late))
			return err
		}},
		{"the publications cannot be read: the session is terminated", nil, nil, hasCause, func(t *testing.T, w *world) error {
			tx, err := w.logins[tenantA].Begin(ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			var pid int
			if err := tx.QueryRow(ctx(t), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			if _, err := w.admin.Exec(ctx(t), `SELECT pg_terminate_backend($1)`, pid); err != nil {
				t.Fatal(err)
			}
			p := &Policies{c: PoliciesConfig{Book: book(t, realPack(t)), Publications: StorePublications{DBs: fixedFor{tx}}, Admissions: &adm{at: t1}, Facts: &facts{master: resolvedMaster()}}}
			_, err = p.Evaluate(ctx(t), query(lookup, late))
			return err
		}},
		{"a history that cannot be put in order", ErrPublications, []string{"3.1", "3.2"}, hasCause, func(t *testing.T, w *world) error {
			w.publish(t, tenantA, "3.1", t2)
			w.publish(t, tenantA, "3.2", t1)
			p := w.policies(t, book(t, realPack(t), variantPack(t, "3.2", nil)), &adm{at: t2}, &facts{master: resolvedMaster()})
			_, err := p.Evaluate(ctx(t), query(lookup, late))
			return err
		}},
		{"a published version with no document", ErrNoDocument, []string{policyID, "3.9"}, hasCause, func(t *testing.T, w *world) error {
			w.publish(t, tenantA, "3.1", t1)
			w.publish(t, tenantA, "3.9", t2)
			p := w.policies(t, book(t, realPack(t)), &adm{at: t2}, &facts{master: resolvedMaster()})
			_, err := p.Evaluate(ctx(t), query(lookup, late))
			return err
		}},
		{"a publication the engine cannot put in force", ErrPublications, []string{policyID, "3.1"}, hasCause, func(t *testing.T, w *world) error {
			w.publish(t, tenantA, "3.1", 0)
			p := w.policies(t, book(t, realPack(t)), &adm{at: 1}, &facts{master: resolvedMaster()})
			_, err := p.Evaluate(ctx(t), query(lookup, late))
			return err
		}},
		{"the facts cannot be read", nil, nil, hasCause, func(t *testing.T, w *world) error {
			w.publish(t, tenantA, "3.1", t1)
			_, err := w.policies(t, book(t, realPack(t)), &adm{at: t1}, &facts{master: resolvedMaster(), err: errLeaky}).Evaluate(ctx(t), query(lookup, late))
			if !errors.Is(err, errLeaky) {
				t.Errorf("the cause is not reachable: %v", err)
			}
			return err
		}},
		{"a fact the rules read was not given", ErrFacts, []string{"logical_resolution_epoch"}, hasCause, factsEdit(caseWrite, func(s FactSet) FactSet {
			delete(s.Variables, "logical_resolution_epoch")
			return s
		})},
		{"an entity the rules read was not read", ErrFacts, []string{"case"}, hasCause, factsEdit(caseWrite, func(s FactSet) FactSet { delete(s.Entities, "case"); return s })},
		{"a variable the pack does not declare", ErrFacts, []string{"invented"}, hasCause, factsEdit(lookup, func(s FactSet) FactSet { s.Variables["invented"] = predicate.Int(1); return s })},
		{"a variable of another kind", ErrFacts, []string{"logical_resolution_epoch"}, hasCause, factsEdit(caseWrite, func(s FactSet) FactSet {
			s.Variables["logical_resolution_epoch"] = predicate.Str("1")
			return s
		})},
		{"an entity the pack does not declare", ErrFacts, []string{"invented"}, hasCause, factsEdit(lookup, func(s FactSet) FactSet { s.Entities["invented"] = nil; return s })},
		{"a row field the entity does not declare", ErrFacts, []string{"owner"}, hasCause, factsEdit(caseWrite, func(s FactSet) FactSet {
			s.Entities["case"] = []predicate.Row{{"owner": predicate.Ref("x")}}
			return s
		})},
		{"a row field of another kind", ErrFacts, []string{"epoch"}, hasCause, factsEdit(caseWrite, func(s FactSet) FactSet {
			s.Entities["case"] = []predicate.Row{{"epoch": predicate.Str("1")}}
			return s
		})},
		{"a rule that cannot be evaluated", nil, []string{"fin.pol.tenant.epoch_floor"}, func(t *testing.T, err error) {
			if !errors.Is(err, policy.ErrCondition) {
				t.Errorf("policy.ErrCondition is not reachable: %v", err)
			}
		}, func(t *testing.T, w *world) error {
			w.publish(t, tenantA, "3.1", t1)
			bad := rule("fin.pol.tenant.epoch_floor", "TENANT_SECURITY", "AT_DECISION", lookup, "logical_resolution_epoch > 0", "REJECT", "fin.reason.permitted", nil)
			m := resolvedMaster()
			m.Variables["logical_resolution_epoch"] = predicate.Value{}
			_, err := w.policies(t, book(t, variantPack(t, "3.1", withRules(bad))), &adm{at: t1}, &facts{master: m}).Evaluate(ctx(t), query(lookup, late))
			return err
		}},
		{"a WAIT whose rule gives no wait", ErrRule, []string{"zzmark.hold"}, hasCause, waitRule(nil)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			err := c.run(t, w)
			assertFixed(t, err, c.kind, c.own)
			c.cause(t, err)
		})
	}
}

type failingFor struct{ err error }

func (f failingFor) For(string) (policystore.Querier, error) { return nil, f.err }

type fixedFor struct{ q policystore.Querier }

func (f fixedFor) For(string) (policystore.Querier, error) { return f.q, nil }

// A347: the clock adapter.
func TestEveryErrorOfTheClocksAdapterHasAFixedPublicText(t *testing.T) {
	clk := packClock(t)
	clockID := string(clk.ClockId)
	cases := []struct {
		name  string
		kind  error
		own   []string
		cause bool
		run   func(t *testing.T) error
	}{
		{"an anchor the source cannot give", nil, []string{clockID}, true, func(t *testing.T) error {
			c := newClocks(t, clockBook(t, realPack(t)), &anchors{err: errLeaky}, nil)
			_, err := c.Applicable(ctx(t), clockQuery())
			if !errors.Is(err, errLeaky) {
				t.Errorf("the cause is not reachable: %v", err)
			}
			return err
		}},
		{"an anchor that is not a time", ErrAnchor, []string{clockID}, true, func(t *testing.T) error {
			c := newClocks(t, clockBook(t, realPack(t)), &anchors{at: map[string]int64{clk.AnchorEvent: 0}}, nil)
			_, err := c.Applicable(ctx(t), clockQuery())
			return err
		}},
		{"a status with a phase that is not a clock phase", ErrQuery, []string{"zzmark_phase"}, true, func(t *testing.T) error {
			c := newClocks(t, clockBook(t, realPack(t)), &anchors{}, nil)
			_, err := c.Route(ctx(t), clockQuery(), []clocks.Status{{Phase: clocks.Phase("zzmark_phase")}})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(t)
			assertFixed(t, err, c.kind, c.own)
			if c.cause && errors.Unwrap(err) == nil {
				t.Errorf("the cause is not kept behind Unwrap: %q", err.Error())
			}
		})
	}
}

// StorePublications is exported: its own errors too.
func TestStorePublicationsErrorsHaveAFixedPublicText(t *testing.T) {
	w := newWorld(t)
	_, err := StorePublications{DBs: failingFor{errLeaky}}.Published(ctx(t), tenantA, policyID)
	assertFixed(t, err, nil, nil)
	if !errors.Is(err, errLeaky) {
		t.Errorf("the cause is not reachable: %v", err)
	}
	_, err = StorePublications{DBs: w}.Published(ctx(t), "ten_cc", policyID)
	assertFixed(t, err, nil, []string{"ten_cc", "no login"})
}
