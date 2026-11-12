package recorder_test

// EXP-01 slice 5 (ruling A155; REQ-EXP-002, REQ-AUD-001, REQ-AUD-002, REQ-AUD-003; INV-11, INV-12; EC-E16): the Record-backed Recorder of the Explainer. A real PostgreSQL
// with every migration, the tenant's core-edge emitter login that appends, and the tenant's record_read login that the reconstruction reads. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/explanation/recorder"
	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

func TestMain(m *testing.M) { testpg.Main(m) }

var _ explanation.Recorder = (*recorder.Recorder)(nil)

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"
	corr    = "corr_c1"
)

var subject = explanation.Subject{TenantID: tenantA, PrincipalID: "usr_88213"}

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

type failingIDs struct {
	mu    sync.Mutex
	inner chain.IDSource
	fail  bool
}

func (f *failingIDs) ID(prefix string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail && prefix == "recev" {
		return "", errors.New("synthetic: the Record refused the event")
	}
	return f.inner.ID(prefix)
}

func (f *failingIDs) set(v bool) { f.mu.Lock(); f.fail = v; f.mu.Unlock() }

type world struct {
	t     *testing.T
	admin *pgxpool.Pool
	clk   *clock.Manual
	fail  *failingIDs
	sign  map[string]chain.Signer // one signer per emitter (REC-08): an emitter with no signer cannot append
	edge  map[string]*pgxpool.Pool
	read  map[string]*pgxpool.Pool
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w := &world{t: t, admin: admin, clk: clock.NewManual(time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)), edge: map[string]*pgxpool.Pool{}, read: map[string]*pgxpool.Pool{}}
	w.fail = &failingIDs{inner: ids.New(w.clk, nil)}
	w.sign = recordsig.All(t, admin, "explrecorder", nil)
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'Synthetic')`, id); err != nil {
			t.Fatal(err)
		}
		w.edge[id] = testpg.EmitterLogin(t, admin, dsn, id, "core-edge")
		w.read[id] = testpg.TenantLogin(t, admin, dsn, "record_read", id)
	}
	return w
}

func (w *world) recorder(tenant string) *recorder.Recorder {
	return &recorder.Recorder{Pool: w.edge[tenant], Appender: &chain.Appender{Clock: w.clk, IDs: w.fail, Signers: w.sign}, Timeout: 5 * time.Second}
}

type stored struct {
	Seq       int64
	EventID   string
	TenantID  string
	WrittenBy string
	Actor     string
	EventType string
	ObjectRef string
	Corr      string
	Payload   map[string]any
}

func (w *world) events(types ...string) []stored {
	w.t.Helper()
	rows, err := w.admin.Query(ctx(w.t), `SELECT seq, event_id, tenant_id, written_by, actor, event_type, object_ref, correlation_id, payload::text FROM record_event ORDER BY tenant_id, seq`)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []stored
	for rows.Next() {
		var s stored
		var p string
		if err := rows.Scan(&s.Seq, &s.EventID, &s.TenantID, &s.WrittenBy, &s.Actor, &s.EventType, &s.ObjectRef, &s.Corr, &p); err != nil {
			w.t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(p), &s.Payload); err != nil {
			w.t.Fatal(err)
		}
		for _, ty := range types {
			if s.EventType == ty {
				out = append(out, s)
			}
		}
	}
	return out
}

func rendered(text string, factIDs ...string) explanation.Event {
	return explanation.Event{Rendered: true, CorrelationID: corr, Text: text, FactIDs: factIDs, TemplateID: "en-NG/fin.outcome.exception.status_explained/SUCCESS_CONFIRMED",
		TemplateVersion: "sha256:" + strings.Repeat("a", 64)}
}

func blocked(rules ...string) explanation.Event {
	return explanation.Event{CorrelationID: corr, Rules: rules}
}

// REQ-AUD-002: a rendered explanation is EXPLANATION_RENDERED, written by core-edge in the tenant of the login, with the claim as the reconstruction reads it.
func TestARenderedExplanationIsAppendedAsExplanationRenderedByCoreEdge(t *testing.T) {
	w := newWorld(t)
	if err := w.recorder(tenantA).AppendExplanation(ctx(t), subject, rendered("Your transfer of NGN 5,000 has settled.", "f1")); err != nil {
		t.Fatal(err)
	}
	evs := w.events("EXPLANATION_RENDERED")
	if len(evs) != 1 {
		t.Fatalf("%d events, want 1", len(evs))
	}
	e := evs[0]
	if e.WrittenBy != "core-edge" || e.TenantID != tenantA || e.Corr != corr || e.ObjectRef != "exp_c1" {
		t.Errorf("event %+v", e)
	}
	if e.Actor != "core-edge" {
		t.Errorf("actor %q: no authenticated person caused the event, so it is the emitter's own identity", e.Actor)
	}
	claims, _ := e.Payload["claims"].([]any)
	if len(claims) != 1 {
		t.Fatalf("claims %v", e.Payload["claims"])
	}
	c, _ := claims[0].(map[string]any)
	if c["text"] != "Your transfer of NGN 5,000 has settled." || c["bound_fact_status"] != "KNOWN" || c["fact_id"] != "f1" {
		t.Errorf("claim %v", c)
	}
	if refs, ok := c["disclosed_refs"].([]any); !ok || len(refs) != 0 {
		t.Errorf("disclosed_refs %v: nothing is disclosed by this slice", c["disclosed_refs"])
	}
	if e.Payload["validator_result"] != "PASSED" || e.Payload["template_id"] != "en-NG/fin.outcome.exception.status_explained/SUCCESS_CONFIRMED" || !strings.HasPrefix(e.Payload["template_version"].(string), "sha256:") {
		t.Errorf("payload %v", e.Payload)
	}
	if _, has := e.Payload["rule"]; has {
		t.Errorf("a rendered explanation carries no rule: %v", e.Payload)
	}
}

// A template with no slot is bound to the template, not to a fact: no fact_id, and a null status (the reconstruction accepts null).
func TestAnExplanationFilledFromNoFactIsBoundToNoFact(t *testing.T) {
	w := newWorld(t)
	if err := w.recorder(tenantA).AppendExplanation(ctx(t), subject, rendered("Your transfer has settled.")); err != nil {
		t.Fatal(err)
	}
	c := w.events("EXPLANATION_RENDERED")[0].Payload["claims"].([]any)[0].(map[string]any)
	if v, has := c["bound_fact_status"]; !has || v != nil {
		t.Errorf("bound_fact_status %v (present %v), want an explicit null", v, has)
	}
	if _, has := c["fact_id"]; has {
		t.Errorf("fact_id %v on a claim bound to no fact", c["fact_id"])
	}
}

// Several facts filled the slots: the claim lists them all and names no single one as its fact_id.
func TestAnExplanationFilledFromSeveralFactsListsThemAll(t *testing.T) {
	w := newWorld(t)
	if err := w.recorder(tenantA).AppendExplanation(ctx(t), subject, rendered("Your transfer of NGN 5,000 to the account ending 0001 has settled.", "f1", "f2")); err != nil {
		t.Fatal(err)
	}
	c := w.events("EXPLANATION_RENDERED")[0].Payload["claims"].([]any)[0].(map[string]any)
	ids, _ := c["fact_ids"].([]any)
	if len(ids) != 2 || ids[0] != "f1" || ids[1] != "f2" || c["bound_fact_status"] != "KNOWN" {
		t.Errorf("claim %v", c)
	}
	if _, has := c["fact_id"]; has {
		t.Errorf("fact_id %v with two facts", c["fact_id"])
	}
}

// PERD 28.2: a block is EXPLANATION_BLOCKED with the rule codes, and never any text.
func TestABlockedExplanationIsAppendedAsExplanationBlockedWithRuleCodesAndNoText(t *testing.T) {
	w := newWorld(t)
	if err := w.recorder(tenantA).AppendExplanation(ctx(t), subject, blocked("FORBIDDEN_CLAIM", "UNSUPPORTED_NAME")); err != nil {
		t.Fatal(err)
	}
	evs := w.events("EXPLANATION_BLOCKED")
	if len(evs) != 1 || len(w.events("EXPLANATION_RENDERED")) != 0 {
		t.Fatalf("events %+v", w.events("EXPLANATION_BLOCKED", "EXPLANATION_RENDERED"))
	}
	e := evs[0]
	if e.WrittenBy != "core-edge" || e.Actor != "core-edge" || e.ObjectRef != "exp_c1" || e.Corr != corr {
		t.Errorf("event %+v", e)
	}
	if e.Payload["rule"] != "FORBIDDEN_CLAIM" {
		t.Errorf("rule %v, want the first code (PERD 28.2: EXPLANATION_BLOCKED{rule})", e.Payload["rule"])
	}
	if rules, _ := e.Payload["rules"].([]any); len(rules) != 2 || rules[1] != "UNSUPPORTED_NAME" {
		t.Errorf("rules %v", e.Payload["rules"])
	}
	for _, k := range []string{"claims", "text", "template_id"} {
		if _, has := e.Payload[k]; has {
			t.Errorf("a blocked explanation carries %q: %v", k, e.Payload)
		}
	}
}

// REQ-AUD-003: what the recorder wrote is what the reconstruction serves, citing the event, with the status the claim carries.
func TestTheReconstructionServesTheRecordedExplanation(t *testing.T) {
	w := newWorld(t)
	if err := w.recorder(tenantA).AppendExplanation(ctx(t), subject, rendered("Your transfer of NGN 5,000 has settled.", "f1")); err != nil {
		t.Fatal(err)
	}
	if err := w.recorder(tenantA).AppendExplanation(ctx(t), subject, blocked("FORBIDDEN_CLAIM")); err != nil {
		t.Fatal(err)
	}
	raw, err := reconstruct.Build(ctx(t), w.read[tenantA], tenantA, corr)
	if err != nil {
		t.Fatalf("the Record the recorder wrote does not reconstruct: %v", err)
	}
	var out struct {
		Chain    struct{ Verified bool } `json:"chain"`
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
	if !out.Chain.Verified {
		t.Error("the chain did not verify")
	}
	want := w.events("EXPLANATION_RENDERED")[0].EventID
	found := 0
	for _, s := range out.Sections {
		if s.Kind != "EXPLANATION" {
			continue
		}
		for _, it := range s.Items {
			found++
			if it.RecordEventID != want || it.Facts["text"] != "Your transfer of NGN 5,000 has settled." || it.Facts["bound_fact_status"] != "KNOWN" || it.Facts["fact_id"] != "f1" {
				t.Errorf("item %+v", it)
			}
		}
	}
	if found != 1 {
		t.Errorf("%d explanation items, want 1 (the blocked event is on the timeline only)", found)
	}
}

// EC-E16: if the Record refuses the event, nothing is stored and the error says so.
func TestARefusedAppendStoresNothing(t *testing.T) {
	w := newWorld(t)
	w.fail.set(true)
	for _, ev := range []explanation.Event{rendered("Your transfer has settled."), blocked("FORBIDDEN_CLAIM")} {
		if err := w.recorder(tenantA).AppendExplanation(ctx(t), subject, ev); err == nil {
			t.Error("a refused append returned no error")
		}
	}
	if n := len(w.events("EXPLANATION_RENDERED", "EXPLANATION_BLOCKED")); n != 0 {
		t.Errorf("%d events stored after a refused append", n)
	}
	w.fail.set(false)
	if err := w.recorder(tenantA).AppendExplanation(ctx(t), subject, rendered("Your transfer has settled.")); err != nil {
		t.Fatalf("control: %v", err)
	}
}

// INV-11: the tenant is the login's. A subject of another tenant is refused by the database and nothing is stored.
func TestASubjectOfAnotherTenantIsRefusedByTheDatabase(t *testing.T) {
	w := newWorld(t)
	other := explanation.Subject{TenantID: tenantB, PrincipalID: "usr_88213"}
	if err := w.recorder(tenantA).AppendExplanation(ctx(t), other, rendered("Your transfer has settled.")); err == nil {
		t.Error("tenant A's login wrote an event for tenant B")
	}
	if n := len(w.events("EXPLANATION_RENDERED")); n != 0 {
		t.Errorf("%d events stored", n)
	}
}

// What the reconstruction would refuse is refused before it is written (fail closed): a Record that cannot be served is worse than no event.
func TestWhatTheReconstructionCouldNotServeIsNotWritten(t *testing.T) {
	w := newWorld(t)
	long := strings.Repeat("a ", 150) + "b" // 301 characters
	cases := map[string]explanation.Event{
		"a text of 301 characters":               rendered(long),
		"an empty text":                          rendered(""),
		"a text with a digit run of 5":           rendered("Your transfer of 12345 has settled."),
		"a rendered event without a template":    {Rendered: true, CorrelationID: corr, Text: "Your transfer has settled."},
		"a rendered event with a rule":           {Rendered: true, CorrelationID: corr, Text: "Your transfer has settled.", TemplateID: "en-NG/x/y", TemplateVersion: "sha256:" + strings.Repeat("a", 64), Rules: []string{"X"}},
		"a rendered event with a bad fact id":    rendered("Your transfer has settled.", "f 1"),
		"a blocked event without a rule":         blocked(),
		"a blocked event with a lower-case rule": blocked("forbidden_claim"),
		"a blocked event with text":              {CorrelationID: corr, Rules: []string{"X"}, Text: "Your transfer has settled."},
		"an event without a correlation id":      {Rendered: true, Text: "Your transfer has settled.", TemplateID: "en-NG/x/y", TemplateVersion: "sha256:" + strings.Repeat("a", 64)},
		"an event with a bad correlation id":     {CorrelationID: "corr 1", Rules: []string{"X"}},
	}
	for name, ev := range cases {
		if err := w.recorder(tenantA).AppendExplanation(ctx(t), subject, ev); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	if n := len(w.events("EXPLANATION_RENDERED", "EXPLANATION_BLOCKED")); n != 0 {
		t.Errorf("%d events stored for events that must be refused", n)
	}
}

// A subject without a tenant or a principal asks for no write.
func TestASubjectWithoutATenantOrAPrincipalWritesNothing(t *testing.T) {
	w := newWorld(t)
	for _, s := range []explanation.Subject{{}, {TenantID: tenantA}, {PrincipalID: "usr_88213"}} {
		if err := w.recorder(tenantA).AppendExplanation(ctx(t), s, rendered("Your transfer has settled.")); err == nil {
			t.Errorf("subject %+v: written", s)
		}
	}
	if n := len(w.events("EXPLANATION_RENDERED")); n != 0 {
		t.Errorf("%d events stored", n)
	}
}

// A recorder that cannot do everything does nothing.
func TestARecorderWithoutAPoolAnAppenderOrATimeoutWritesNothing(t *testing.T) {
	w := newWorld(t)
	good := w.recorder(tenantA)
	for name, r := range map[string]*recorder.Recorder{
		"nil":         nil,
		"no pool":     {Appender: good.Appender, Timeout: time.Second},
		"no appender": {Pool: good.Pool, Timeout: time.Second},
		"no timeout":  {Pool: good.Pool, Appender: good.Appender},
	} {
		if err := r.AppendExplanation(ctx(t), subject, rendered("Your transfer has settled.")); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	if n := len(w.events("EXPLANATION_RENDERED")); n != 0 {
		t.Errorf("%d events stored", n)
	}
}
