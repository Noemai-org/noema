package portread_test

// TRU-07 portread slice (ruling A461, interim, relayed, not yet committed; A438): the reader returns both case lists from the stand-in's ports. The case list is
// mock-case's CasePage (its items and its own `complete` marker); the complaint_ref list is its own list, read from FindComplaints, with its own marker. A list is
// KNOWN only when marked complete: a missing or false marker is UNKNOWN, never an empty "none" (INV-19). REQ-CTX-007 (the injection screen and the payloadread path are
// untouched: the reader only builds an observation), INV-12 (the tenant and principal are the session's), INV-21 (the same answers give the same payloads), AGENT-09
// (what the case system says is data). No database; fake ports and a fake recorder. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/portread"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

type csCasePort struct {
	adapters.Case // the write methods are never called: a call would panic
	find          func(adapters.CaseQuery) (adapters.CasePage, error)
	queries       []adapters.CaseQuery
	deadlines     int
}

func (p *csCasePort) FindCases(ctx context.Context, q adapters.CaseQuery) (adapters.CasePage, error) {
	p.queries = append(p.queries, q)
	if _, ok := ctx.Deadline(); ok {
		p.deadlines++
	}
	return p.find(q)
}

type csRecordsPort struct {
	adapters.CaseRecords
	find    func(adapters.RecordQuery) (adapters.ComplaintPage, error)
	queries []adapters.RecordQuery
}

func (p *csRecordsPort) FindComplaints(_ context.Context, q adapters.RecordQuery) (adapters.ComplaintPage, error) {
	p.queries = append(p.queries, q)
	return p.find(q)
}

func csCase(id string) adapters.CaseRecord {
	return adapters.CaseRecord{CaseID: id, Reference: "CASE-" + id, Status: "OPEN", OwnerQueue: "fin.queue.payments_ops", Priority: "NORMAL", VersionToken: "v1",
		CreatedAt: "2026-11-11T08:00:00Z", TenantID: "ten_aa", PrincipalID: "usr_ab12", TransactionRef: "TX1", ReasonCode: "DEBIT_NO_CREDIT",
		CreatedByIntent: "int_aa", Epoch: 3}
}

func csComplaint(ref, caseID string) adapters.ComplaintRecord {
	return adapters.ComplaintRecord{ComplaintRef: ref, CaseID: caseID, IdempotencyKey: "key_" + ref, LoggedAt: "2026-11-11T08:31:00Z",
		ClockStartedAt: "2026-11-11T08:30:00Z", TenantID: "ten_aa"}
}

func csCasePage(complete bool, items ...adapters.CaseRecord) adapters.CasePage {
	p := adapters.CasePage{Items: items, Complete: complete, RawHash: hash}
	if !complete {
		p.NextCursor = "cur_2"
	}
	return p
}

func csComplaintPage(complete bool, items ...adapters.ComplaintRecord) adapters.ComplaintPage {
	p := adapters.ComplaintPage{Items: items, Complete: complete, RawHash: hash}
	if !complete {
		p.NextCursor = "cur_2"
	}
	return p
}

type csRig struct {
	cases   *csCasePort
	records *csRecordsPort
	rec     *recorder
	r       *portread.Reader
}

// csNew gives a rig whose ports answer one complete case (c1) and one complete complaint on it, unless the test changes them.
func csNew() *csRig {
	g := &csRig{rec: &recorder{}}
	g.cases = &csCasePort{find: func(adapters.CaseQuery) (adapters.CasePage, error) { return csCasePage(true, csCase("c1")), nil }}
	g.records = &csRecordsPort{find: func(q adapters.RecordQuery) (adapters.ComplaintPage, error) {
		return csComplaintPage(true, csComplaint("cmp_1", q.CaseID)), nil
	}}
	g.r = reader(healthy(), g.rec)
	g.r.Cases, g.r.CaseRecords = g.cases, g.records
	g.r.MaxPayloadBytes = 1 << 20
	mk := func(fact string) portread.Source {
		return portread.Source{ID: "fin.src.case_system", Fact: fact, Authority: layer.AuthAuthoritative, Limit: 60 * time.Second, Sensitivity: "S3_financial_pii",
			ResourceKind: "fin.case"}
	}
	g.r.CaseSources = portread.CaseSources{Case: mk("case_existence_and_status"), Complaint: mk("complaint_reference")}
	g.r.CaseSources.Complaint.ResourceKind = "fin.complaint_ref"
	return g
}

func (g *csRig) run(t *testing.T) ([]layer.Item, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return g.r.ReadCases(ctx, scope, "corr_synthetic", "TX1")
}

func csGot(t *testing.T, g *csRig, i int) layer.Observation {
	t.Helper()
	if i >= len(g.rec.got) {
		t.Fatalf("only %d observations were recorded, want at least %d", len(g.rec.got), i+1)
	}
	return g.rec.got[i]
}

// csWantUnknownComplaint asserts A473 (interim, relayed, recorded in docs#216, not yet merged): a case list that is unusable or unavailable leaves the complaint list explicitly
// UNKNOWN (an item, never an absent one and never an empty "none"): no payload, no bytes, no complaint read, the complaint source's descriptor, and the case item first.
func csWantUnknownComplaint(t *testing.T, name string, g *csRig, items []layer.Item, err error) {
	t.Helper()
	if err != nil || len(items) != 2 || len(g.rec.got) != 2 {
		t.Errorf("%s: items %d, observations %d, err %v; want 2, 2, nil (the case item, then an UNKNOWN complaint item)", name, len(items), len(g.rec.got), err)
		return
	}
	if len(g.records.queries) != 0 {
		t.Errorf("%s: complaints were read with no usable case list", name)
	}
	o := g.rec.got[1]
	if o.Payload != nil || !o.NoBytes || o.RawHash != "" || status(o) != contracts.EnumsV1FactStatusUNKNOWN || o.Read.Class == layer.ReadOK {
		t.Errorf("%s: complaint item payload %v nobytes %v raw %q status %s class %s; want no payload, no bytes, UNKNOWN", name, o.Payload, o.NoBytes, o.RawHash, status(o), o.Read.Class)
	}
	if o.Source != g.r.CaseSources.Complaint.ID || o.ResourceScope != "fin.complaint_ref:TX1" || o.PrincipalScope != "usr_ab12" || o.TenantID != "ten_aa" {
		t.Errorf("%s: complaint item source %q scope %q principal %q tenant %q", name, o.Source, o.ResourceScope, o.PrincipalScope, o.TenantID)
	}
	if st := status(g.rec.got[0]); st == contracts.EnumsV1FactStatusKNOWN {
		t.Errorf("%s: the case item is KNOWN (%s) although the list is unusable", name, st)
	}
}

func csList(t *testing.T, o layer.Observation) ([]any, bool) {
	t.Helper()
	p, ok := o.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %T, want an object", o.Payload)
	}
	items, ok := p["items"].([]any)
	if !ok {
		t.Fatalf("payload items = %T, want a list", p["items"])
	}
	done, ok := p["complete"].(bool)
	if !ok {
		t.Fatalf("payload complete = %T, want a boolean always present", p["complete"])
	}
	return items, done
}

func TestBothListsAreRecordedEachWithItsOwnItemsAndItsOwnMarker(t *testing.T) {
	g := csNew()
	items, err := g.run(t)
	if err != nil || len(items) != 2 || len(g.rec.got) != 2 {
		t.Fatalf("items %d, observations %d, err %v; want 2, 2, nil", len(items), len(g.rec.got), err)
	}
	cs, cp := g.rec.got[0], g.rec.got[1]
	rows, done := csList(t, cs)
	if len(rows) != 1 || !done {
		t.Fatalf("case list: %d rows, complete %v; want 1, true", len(rows), done)
	}
	row, ok := rows[0].(map[string]any)
	if !ok || row["case_id"] != "c1" || row["status"] != "OPEN" || row["transaction_ref"] != "TX1" || row["created_by_intent"] != "int_aa" || row["owner_queue"] != "fin.queue.payments_ops" || row["epoch"] != int64(3) {
		t.Errorf("case row = %#v", rows[0])
	}
	crows, cdone := csList(t, cp)
	if len(crows) != 1 || !cdone {
		t.Fatalf("complaint list: %d rows, complete %v; want 1, true", len(crows), cdone)
	}
	cr, ok := crows[0].(map[string]any)
	if !ok || cr["complaint_ref"] != "cmp_1" || cr["case_id"] != "c1" || cr["clock_started_at"] != "2026-11-11T08:30:00Z" {
		t.Errorf("complaint row = %#v", crows[0])
	}
	for i, o := range g.rec.got {
		if o.Source != "fin.src.case_system" || o.TenantID != "ten_aa" || o.PrincipalScope != "usr_ab12" || o.SourceRecordID != "TX1" || o.CorrelationID != "corr_synthetic" || o.NoBytes || o.RawHash != hash ||
			o.DataSource != "stand-in" || !o.Synthetic {
			t.Errorf("observation %d: %+v", i, o)
		}
		if got := status(o); got != contracts.EnumsV1FactStatusKNOWN {
			t.Errorf("observation %d: status %s, want KNOWN", i, got)
		}
	}
	if cs.ResourceScope != "fin.case:TX1" || cp.ResourceScope != "fin.complaint_ref:TX1" {
		t.Errorf("resource scopes %q, %q: the two lists must be told apart", cs.ResourceScope, cp.ResourceScope)
	}
	if _, leaked := cs.Payload.(map[string]any)["complaints"]; leaked {
		t.Error("the complaint list rode inside the case payload; it is its own list")
	}
}

func TestThePortsAreAskedWithTheSessionsScopeAndTheCaseThatWasFound(t *testing.T) {
	g := csNew()
	if _, err := g.run(t); err != nil {
		t.Fatal(err)
	}
	if len(g.cases.queries) != 1 || g.cases.queries[0] != (adapters.CaseQuery{TenantID: "ten_aa", PrincipalID: "usr_ab12", TransactionRef: "TX1"}) {
		t.Errorf("case query = %+v, want the session's tenant and principal and the transaction (INV-12)", g.cases.queries)
	}
	if len(g.records.queries) != 1 || g.records.queries[0] != (adapters.RecordQuery{TenantID: "ten_aa", CaseID: "c1"}) {
		t.Errorf("complaint query = %+v, want the session's tenant and the found case", g.records.queries)
	}
	if g.cases.deadlines != 1 {
		t.Error("the case call had no deadline")
	}
}

func TestACaseListMarkedCompleteAndEmptyIsNoCaseAndAnIncompleteOneIsUnknown(t *testing.T) {
	g := csNew()
	g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) { return csCasePage(true), nil }
	items, err := g.run(t)
	if err != nil || len(items) != 1 {
		t.Fatalf("items %d, err %v; want 1 (no case, so no complaint list to read)", len(items), err)
	}
	rows, done := csList(t, csGot(t, g, 0))
	if len(rows) != 0 || !done || status(csGot(t, g, 0)) != contracts.EnumsV1FactStatusKNOWN {
		t.Errorf("a complete empty list: %d rows, complete %v, status %s; want 0, true, KNOWN", len(rows), done, status(csGot(t, g, 0)))
	}
	if len(g.records.queries) != 0 {
		t.Error("complaints were read with no case to read them for")
	}

	for name, page := range map[string]adapters.CasePage{"empty": csCasePage(false), "with rows": csCasePage(false, csCase("c1"))} {
		g := csNew()
		g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) { return page, nil }
		items, err := g.run(t)
		if err != nil {
			t.Fatal(err)
		}
		_, done := csList(t, csGot(t, g, 0))
		if done || status(csGot(t, g, 0)) != contracts.EnumsV1FactStatusUNKNOWN {
			t.Errorf("%s incomplete list: complete %v, status %s; want false, UNKNOWN (never an empty none)", name, done, status(csGot(t, g, 0)))
		}
		csWantUnknownComplaint(t, name+" incomplete list", g, items, err)
	}
}

func TestTheComplaintListHasItsOwnMarker(t *testing.T) {
	g := csNew()
	g.records.find = func(q adapters.RecordQuery) (adapters.ComplaintPage, error) {
		return csComplaintPage(false, csComplaint("cmp_1", q.CaseID)), nil
	}
	if _, err := g.run(t); err != nil {
		t.Fatal(err)
	}
	if _, done := csList(t, csGot(t, g, 0)); !done || status(csGot(t, g, 0)) != contracts.EnumsV1FactStatusKNOWN {
		t.Error("an incomplete complaint list made the case list unknown")
	}
	if _, done := csList(t, csGot(t, g, 1)); done || status(csGot(t, g, 1)) != contracts.EnumsV1FactStatusUNKNOWN {
		t.Errorf("incomplete complaint list: complete %v, status %s; want false, UNKNOWN", done, status(csGot(t, g, 1)))
	}

	g = csNew()
	g.records.find = func(adapters.RecordQuery) (adapters.ComplaintPage, error) { return csComplaintPage(true), nil }
	if _, err := g.run(t); err != nil {
		t.Fatal(err)
	}
	rows, done := csList(t, csGot(t, g, 1))
	if len(rows) != 0 || !done || status(csGot(t, g, 1)) != contracts.EnumsV1FactStatusKNOWN {
		t.Errorf("complete empty complaint list: %d rows, complete %v, status %s; want 0, true, KNOWN", len(rows), done, status(csGot(t, g, 1)))
	}
}

func TestSeveralCasesGiveOneSortedComplaintListThatIsCompleteOnlyIfEveryReadWas(t *testing.T) {
	build := func(incompleteCase string) *csRig {
		g := csNew()
		g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) {
			return csCasePage(true, csCase("c2"), csCase("c1")), nil
		}
		g.records.find = func(q adapters.RecordQuery) (adapters.ComplaintPage, error) {
			return csComplaintPage(q.CaseID != incompleteCase, csComplaint("cmp_b"+q.CaseID, q.CaseID), csComplaint("cmp_a"+q.CaseID, q.CaseID)), nil
		}
		return g
	}
	g := build("")
	if _, err := g.run(t); err != nil {
		t.Fatal(err)
	}
	if len(g.records.queries) != 2 || g.records.queries[0].CaseID != "c1" || g.records.queries[1].CaseID != "c2" {
		t.Errorf("complaint reads = %+v, want one per case in case-id order (INV-21)", g.records.queries)
	}
	rows, _ := csList(t, csGot(t, g, 0))
	if len(rows) != 2 || rows[0].(map[string]any)["case_id"] != "c1" || rows[1].(map[string]any)["case_id"] != "c2" {
		t.Errorf("case rows are not in case-id order: %#v", rows)
	}
	crows, done := csList(t, csGot(t, g, 1))
	want := []string{"cmp_ac1", "cmp_bc1", "cmp_ac2", "cmp_bc2"}
	if len(crows) != len(want) || !done {
		t.Fatalf("complaint rows %#v complete %v", crows, done)
	}
	for i, w := range want {
		if crows[i].(map[string]any)["complaint_ref"] != w {
			t.Errorf("complaint row %d is %v, want %s (by case id, then by ref)", i, crows[i].(map[string]any)["complaint_ref"], w)
		}
	}
	if o := csGot(t, g, 1); o.RawHash == hash || !strings.HasPrefix(o.RawHash, "sha256:") || len(o.RawHash) != len(hash) {
		t.Errorf("the raw hash of two pages must be one digest of both, got %q", o.RawHash)
	}
	for _, c := range []string{"c1", "c2"} {
		g = build(c)
		if _, err := g.run(t); err != nil {
			t.Fatal(err)
		}
		if o := csGot(t, g, 1); func() bool { _, d := csList(t, o); return d }() || status(o) != contracts.EnumsV1FactStatusUNKNOWN {
			t.Errorf("an unfinished complaint read of %s must make the whole complaint list unknown", c)
		}
	}
}

func TestAnEmptyCompleteListIsStampedWithTheReadersClock(t *testing.T) {
	g := csNew()
	g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) { return csCasePage(true), nil }
	if _, err := g.run(t); err != nil {
		t.Fatal(err)
	}
	if o := csGot(t, g, 0); !o.EffectiveAt.Equal(epoch) {
		t.Errorf("effective_at = %v, want the reader's clock %v", o.EffectiveAt, epoch)
	}
}

func TestACaseAnswerTheReaderCannotUseIsUnknownAndKeepsNoPayload(t *testing.T) {
	bad := func(f func(c *adapters.CaseRecord)) adapters.CasePage {
		c := csCase("c1")
		f(&c)
		return csCasePage(true, c)
	}
	badHash := csCasePage(true, csCase("c1"))
	badHash.RawHash = "not-a-hash"
	cases := map[string]adapters.CasePage{
		"complete with a next cursor":    {Items: []adapters.CaseRecord{csCase("c1")}, Complete: true, NextCursor: "x", RawHash: hash},
		"incomplete with no next cursor": {Items: []adapters.CaseRecord{csCase("c1")}, Complete: false, RawHash: hash},
		"another tenant's case":          bad(func(c *adapters.CaseRecord) { c.TenantID = "ten_bb" }),
		"another principal's case":       bad(func(c *adapters.CaseRecord) { c.PrincipalID = "usr_zz99" }),
		"another transaction's case":     bad(func(c *adapters.CaseRecord) { c.TransactionRef = "TX2" }),
		"no case id":                     bad(func(c *adapters.CaseRecord) { c.CaseID = "" }),
		"no status":                      bad(func(c *adapters.CaseRecord) { c.Status = "" }),
		"a time that does not parse":     bad(func(c *adapters.CaseRecord) { c.CreatedAt = "yesterday" }),
		"a time with no zone":            bad(func(c *adapters.CaseRecord) { c.CreatedAt = "2026-11-11T08:00:00" }),
		"a duplicate case id":            csCasePage(true, csCase("c1"), csCase("c1")),
		"no provenance hash":             badHash,
	}
	for name, page := range cases {
		g := csNew()
		g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) { return page, nil }
		items, err := g.run(t)
		csWantUnknownComplaint(t, name, g, items, err)
		if len(items) < 1 {
			continue
		}
		o := csGot(t, g, 0)
		if o.Payload != nil || o.Read.Class != layer.ReadMalformed || status(o) != contracts.EnumsV1FactStatusUNKNOWN || (o.RawHash == "") == !o.NoBytes {
			t.Errorf("%s: payload %v class %s status %s raw %q nobytes %v", name, o.Payload, o.Read.Class, status(o), o.RawHash, o.NoBytes)
		}
	}

	badComplaint := func(f func(c *adapters.ComplaintRecord)) func(adapters.RecordQuery) (adapters.ComplaintPage, error) {
		return func(q adapters.RecordQuery) (adapters.ComplaintPage, error) {
			c := csComplaint("cmp_1", q.CaseID)
			f(&c)
			return csComplaintPage(true, c), nil
		}
	}
	complaints := map[string]func(adapters.RecordQuery) (adapters.ComplaintPage, error){
		"another tenant's complaint":  badComplaint(func(c *adapters.ComplaintRecord) { c.TenantID = "ten_bb" }),
		"another case's complaint":    badComplaint(func(c *adapters.ComplaintRecord) { c.CaseID = "c9" }),
		"no complaint ref":            badComplaint(func(c *adapters.ComplaintRecord) { c.ComplaintRef = "" }),
		"a clock that does not parse": badComplaint(func(c *adapters.ComplaintRecord) { c.ClockStartedAt = "soon" }),
		"complete with a cursor": func(q adapters.RecordQuery) (adapters.ComplaintPage, error) {
			return adapters.ComplaintPage{Complete: true, NextCursor: "x", RawHash: hash}, nil
		},
	}
	for name, find := range complaints {
		g := csNew()
		g.records.find = find
		if _, err := g.run(t); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		o := csGot(t, g, 1)
		if o.Payload != nil || o.Read.Class != layer.ReadMalformed || status(o) != contracts.EnumsV1FactStatusUNKNOWN {
			t.Errorf("%s: payload %v class %s status %s", name, o.Payload, o.Read.Class, status(o))
		}
		if status(csGot(t, g, 0)) != contracts.EnumsV1FactStatusKNOWN {
			t.Errorf("%s: a bad complaint answer spoiled the case list", name)
		}
	}
}

func TestAListOverTheSizeLimitIsRefusedFailClosedAndOneAtTheLimitIsNot(t *testing.T) {
	g := csNew()
	if _, err := g.run(t); err != nil {
		t.Fatal(err)
	}
	size := func(o layer.Observation) int {
		raw, err := json.Marshal(o.Payload)
		if err != nil {
			t.Fatal(err)
		}
		return len(raw)
	}
	caseSize, complaintSize := size(csGot(t, g, 0)), size(csGot(t, g, 1))

	g = csNew()
	g.r.MaxPayloadBytes = caseSize
	if _, err := g.run(t); err != nil || status(csGot(t, g, 0)) != contracts.EnumsV1FactStatusKNOWN {
		t.Fatalf("a case list of exactly the limit (%d bytes) must be read: %v", caseSize, err)
	}
	g = csNew()
	g.r.MaxPayloadBytes = caseSize - 1
	items, err := g.run(t)
	csWantUnknownComplaint(t, "a case list over the limit", g, items, err)
	if o := csGot(t, g, 0); o.Payload != nil || status(o) != contracts.EnumsV1FactStatusUNKNOWN || o.Read.Class != layer.ReadMalformed {
		t.Errorf("a case list one byte over the limit: payload %v status %s", o.Payload, status(o))
	}
	many := func(g *csRig) {
		g.records.find = func(q adapters.RecordQuery) (adapters.ComplaintPage, error) {
			var cs []adapters.ComplaintRecord
			for _, ref := range []string{"cmp_1", "cmp_2", "cmp_3", "cmp_4", "cmp_5", "cmp_6", "cmp_7", "cmp_8"} {
				cs = append(cs, csComplaint(ref, q.CaseID))
			}
			return csComplaintPage(true, cs...), nil
		}
	}
	g = csNew()
	many(g)
	if _, err := g.run(t); err != nil {
		t.Fatal(err)
	}
	complaintSize = size(csGot(t, g, 1))
	if complaintSize <= caseSize {
		t.Fatalf("fixture: the complaint payload (%d bytes) must be larger than the case payload (%d)", complaintSize, caseSize)
	}
	g = csNew()
	many(g)
	g.r.MaxPayloadBytes = complaintSize
	if _, err := g.run(t); err != nil || status(csGot(t, g, 1)) != contracts.EnumsV1FactStatusKNOWN {
		t.Fatalf("a complaint list of exactly the limit must be read: %v", err)
	}
	g = csNew()
	many(g)
	g.r.MaxPayloadBytes = complaintSize - 1
	if _, err := g.run(t); err != nil {
		t.Fatal(err)
	}
	if o := csGot(t, g, 1); o.Payload != nil || status(o) != contracts.EnumsV1FactStatusUNKNOWN || o.Read.Class != layer.ReadMalformed {
		t.Error("a complaint list over the limit must be refused fail closed")
	}
	if status(csGot(t, g, 0)) != contracts.EnumsV1FactStatusKNOWN {
		t.Error("a refused complaint list spoiled the case list")
	}
}

func TestAPortThatFailsIsAnItemAndStopsTheCaseDependentRead(t *testing.T) {
	g := csNew()
	g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) {
		return adapters.CasePage{}, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "503", RawHash: hash}
	}
	items, err := g.run(t)
	csWantUnknownComplaint(t, "the case read failed", g, items, err)
	if o := csGot(t, g, 0); status(o) != contracts.EnumsV1FactStatusUNAVAILABLE || o.Payload != nil || o.RawHash != hash {
		t.Errorf("case read failed: status %s payload %v raw %q", status(o), o.Payload, o.RawHash)
	}

	g = csNew()
	g.records.find = func(adapters.RecordQuery) (adapters.ComplaintPage, error) {
		return adapters.ComplaintPage{}, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "TIMEOUT"}
	}
	if _, err := g.run(t); err != nil {
		t.Fatal(err)
	}
	if o := csGot(t, g, 1); status(o) != contracts.EnumsV1FactStatusUNAVAILABLE || o.Payload != nil || !o.NoBytes {
		t.Errorf("complaint read failed: status %s payload %v nobytes %v", status(o), o.Payload, o.NoBytes)
	}
	if status(csGot(t, g, 0)) != contracts.EnumsV1FactStatusKNOWN {
		t.Error("a failed complaint read spoiled the case list")
	}
}

func TestWhenTheRecordRefusesTheCaseListsStopAtOnce(t *testing.T) {
	g := csNew()
	g.rec.fail = 1
	items, err := g.run(t)
	if err == nil || len(items) != 0 || len(g.records.queries) != 0 {
		t.Errorf("items %d, err %v, complaint reads %d; want 0, an error, 0 (EC-E16)", len(items), err, len(g.records.queries))
	}
	g = csNew()
	g.rec.fail = 2
	items, err = g.run(t)
	if err == nil || len(items) != 1 {
		t.Errorf("items %d, err %v; want the first item returned with the error", len(items), err)
	}
}

func TestARequestOrConfigurationTheCaseReadRefusesReadsNothing(t *testing.T) {
	ctxOK := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), time.Minute)
	}
	cases := map[string]func(g *csRig, ctx *context.Context, s *adapters.Scope, corr, ref *string){
		"no deadline": func(g *csRig, ctx *context.Context, s *adapters.Scope, corr, ref *string) {
			*ctx = context.Background()
		},
		"no case port":              func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) { g.r.Cases = nil },
		"no records port":           func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) { g.r.CaseRecords = nil },
		"no size limit":             func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) { g.r.MaxPayloadBytes = 0 },
		"a negative size limit":     func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) { g.r.MaxPayloadBytes = -1 },
		"no read timeout":           func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) { g.r.ReadTimeout = 0 },
		"no recorder":               func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) { g.r.Rec = nil },
		"no clock":                  func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) { g.r.Clock = nil },
		"read_by not allowed":       func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) { g.r.ReadBy = "MODEL" },
		"a case source that is bad": func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) { g.r.CaseSources.Case.Limit = 0 },
		"a complaint source that is bad": func(g *csRig, _ *context.Context, _ *adapters.Scope, _, _ *string) {
			g.r.CaseSources.Complaint.ResourceKind = "Fin Complaint"
		},
		"no tenant":       func(_ *csRig, _ *context.Context, s *adapters.Scope, _, _ *string) { s.TenantID = "" },
		"a bad principal": func(_ *csRig, _ *context.Context, s *adapters.Scope, _, _ *string) { s.PrincipalID = "someone" },
		"a bad reference": func(_ *csRig, _ *context.Context, _ *adapters.Scope, _, ref *string) { *ref = "TX 1; drop" },
		"no correlation":  func(_ *csRig, _ *context.Context, _ *adapters.Scope, corr, _ *string) { *corr = "" },
	}
	for name, mutate := range cases {
		g := csNew()
		ctx, cancel := ctxOK()
		s, corr, ref := scope, "corr_synthetic", "TX1"
		mutate(g, &ctx, &s, &corr, &ref)
		items, err := g.r.ReadCases(ctx, s, corr, ref)
		cancel()
		if err == nil || len(items) != 0 || len(g.cases.queries) != 0 || len(g.records.queries) != 0 || len(g.rec.got) != 0 {
			t.Errorf("%s: err %v, items %d, reads %d+%d, recorded %d; want a refusal and nothing read", name, err, len(items), len(g.cases.queries), len(g.records.queries), len(g.rec.got))
		}
	}
}

func TestTheCaseReadDoesNotNeedTheTransactionPortsAndTheTransactionReadDoesNotNeedTheCasePorts(t *testing.T) {
	g := csNew()
	g.r.Ledger, g.r.Switch, g.r.Destination = nil, nil, nil
	if items, err := g.run(t); err != nil || len(items) != 2 {
		t.Errorf("the case read needs no ledger, switch or destination: items %d, err %v", len(items), err)
	}
	p, rec := healthy(), &recorder{}
	r := reader(p, rec)
	if items, err := run(t, r); err != nil || len(items) != 3 {
		t.Errorf("ReadTransaction with no case ports: items %d, err %v; want 3, nil (unchanged)", len(items), err)
	}
}

func TestTheSameAnswersGiveTheSamePayloads(t *testing.T) {
	a, b := csNew(), csNew()
	for _, g := range []*csRig{a, b} {
		g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) {
			return csCasePage(true, csCase("c2"), csCase("c1")), nil
		}
		if _, err := g.run(t); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.rec.got) != 2 || len(b.rec.got) != 2 {
		t.Fatalf("observations %d and %d, want 2 and 2", len(a.rec.got), len(b.rec.got))
	}
	for i := range a.rec.got {
		x, _ := json.Marshal(a.rec.got[i].Payload)
		y, _ := json.Marshal(b.rec.got[i].Payload)
		if string(x) != string(y) || strings.Contains(string(x), "version_token") {
			t.Errorf("payload %d differs between two runs, or carries the version token: %s / %s", i, x, y)
		}
	}
}

// A473: every way a case list can be unusable or unavailable gives the explicit UNKNOWN complaint item; the same holds for a timeout and a refused read, and an answering source
// with an empty complete list stays "no cases" (one item, no complaint item).
func TestEveryUnusableOrUnavailableCaseListLeavesTheComplaintListExplicitlyUnknown(t *testing.T) {
	for name, find := range map[string]func(adapters.CaseQuery) (adapters.CasePage, error){
		"timeout": func(adapters.CaseQuery) (adapters.CasePage, error) {
			return adapters.CasePage{}, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "TIMEOUT"}
		},
		"server error": func(adapters.CaseQuery) (adapters.CasePage, error) {
			return adapters.CasePage{}, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "503", RawHash: hash}
		},
		"unusable page": func(adapters.CaseQuery) (adapters.CasePage, error) {
			return adapters.CasePage{Complete: true, NextCursor: "x", RawHash: hash}, nil
		},
	} {
		g := csNew()
		g.cases.find = find
		items, err := g.run(t)
		csWantUnknownComplaint(t, name, g, items, err)
	}
	g := csNew()
	g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) { return csCasePage(true), nil }
	items, err := g.run(t)
	if err != nil || len(items) != 1 || len(g.rec.got) != 1 || status(csGot(t, g, 0)) != contracts.EnumsV1FactStatusKNOWN {
		t.Errorf("an empty complete list from an answering source: items %d, err %v; want 1, nil, KNOWN (no cases)", len(items), err)
	}
}

// The UNKNOWN complaint item is recorded through the Recorder like any other: a refusal of it stops the call and returns the case item with the error (EC-E16).
func TestARefusedUnknownComplaintItemStopsTheCallAndReturnsTheCaseItem(t *testing.T) {
	g := csNew()
	g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) { return csCasePage(false), nil }
	g.rec.fail = 2
	items, err := g.run(t)
	if err == nil || len(items) != 1 {
		t.Errorf("items %d, err %v; want the case item with an error", len(items), err)
	}
}
