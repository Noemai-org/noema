package portread_test

// DEBT-54 part 2 (ruling A520, interim; PERD 28.2: the logical resolution epoch "increments only when the prior logical action's case is verified CLOSED in the case system and a
// new intent is admitted" [REC]; INV-19, INV-12, INV-21): the reader answers the epoch of a new intent about a transaction from the case list, without recording anything (the
// Evidence stage's ReadCases records; this read happens at admission, before Evidence). Epoch = the highest epoch of a CLOSED case + 1; 1 when no case is closed. A list that is not
// usable and complete is not "1": the answer is unknown and the caller fails closed (a guessed 1 would reuse an earlier epoch's idempotency key). No database; fake ports.
// All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/portread"
)

func epochRun(t *testing.T, g *csRig, mutate func(*context.Context)) (int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if mutate != nil {
		mutate(&ctx)
	}
	return g.r.NextEpoch(ctx, scope, "TX1")
}

func closed(id string, epoch int64) adapters.CaseRecord {
	c := csCase(id)
	c.Status, c.Epoch = "CLOSED", epoch
	return c
}

func open(id string, epoch int64) adapters.CaseRecord {
	c := csCase(id)
	c.Epoch = epoch
	return c
}

func answers(g *csRig, page adapters.CasePage, err error) {
	g.cases.find = func(adapters.CaseQuery) (adapters.CasePage, error) { return page, err }
}

// No case at all, and only open cases: epoch 1.
func TestNoClosedCaseGivesEpochOne(t *testing.T) {
	for name, page := range map[string]adapters.CasePage{"no case": csCasePage(true), "an open case": csCasePage(true, open("c1", 4)), "two open cases": csCasePage(true, open("c1", 4), open("c2", 9))} {
		g := csNew()
		answers(g, page, nil)
		if e, err := epochRun(t, g, nil); err != nil || e != 1 {
			t.Errorf("%s: epoch %d err %v, want 1", name, e, err)
		}
	}
}

// One closed case of epoch k gives k+1; several closed cases give the highest + 1; an open case of a higher epoch does not count.
func TestTheEpochIsTheHighestClosedEpochPlusOne(t *testing.T) {
	for name, c := range map[string]struct {
		items []adapters.CaseRecord
		want  int
	}{
		"one closed epoch 1":                        {[]adapters.CaseRecord{closed("c1", 1)}, 2},
		"one closed epoch 3":                        {[]adapters.CaseRecord{closed("c1", 3)}, 4},
		"closed 2 and 5, in either order":           {[]adapters.CaseRecord{closed("c2", 5), closed("c1", 2)}, 6},
		"an open case of a higher epoch is ignored": {[]adapters.CaseRecord{closed("c1", 2), open("c2", 7)}, 3},
	} {
		g := csNew()
		answers(g, csCasePage(true, c.items...), nil)
		if e, err := epochRun(t, g, nil); err != nil || e != c.want {
			t.Errorf("%s: epoch %d err %v, want %d", name, e, err, c.want)
		}
	}
}

// A list that cannot be relied on is unknown, never 1 (INV-19): incomplete, unreadable, a case that is not this transaction's, this tenant's or this principal's, a repeated case id,
// a status that is empty, and a closed case with no usable epoch.
func TestAnUnusableCaseListIsUnknownAndNeverEpochOne(t *testing.T) {
	other := func(mut func(*adapters.CaseRecord)) adapters.CasePage {
		c := closed("c1", 1)
		mut(&c)
		return csCasePage(true, c)
	}
	dup := csCasePage(true, closed("c1", 1), closed("c1", 1))
	for name, c := range map[string]struct {
		page adapters.CasePage
		err  error
	}{
		"incomplete":              {csCasePage(false, closed("c1", 1)), nil},
		"the read failed":         {adapters.CasePage{}, &adapters.ReadError{Failure: adapters.ReadUnavailable}},
		"another tenant":          {other(func(c *adapters.CaseRecord) { c.TenantID = "ten_zz" }), nil},
		"another principal":       {other(func(c *adapters.CaseRecord) { c.PrincipalID = "usr_zz99" }), nil},
		"another transaction":     {other(func(c *adapters.CaseRecord) { c.TransactionRef = "TX2" }), nil},
		"an empty status":         {other(func(c *adapters.CaseRecord) { c.Status = "" }), nil},
		"a case id off its shape": {other(func(c *adapters.CaseRecord) { c.CaseID = "c 1" }), nil},
		"a repeated case id":      {dup, nil},
		"a closed case, epoch 0":  {other(func(c *adapters.CaseRecord) { c.Epoch = 0 }), nil},
		"a closed case, epoch -3": {other(func(c *adapters.CaseRecord) { c.Epoch = -3 }), nil},
		"the marker disagrees":    {adapters.CasePage{Items: []adapters.CaseRecord{closed("c1", 1)}, Complete: true, NextCursor: "cur_2", RawHash: hash}, nil},
	} {
		g := csNew()
		answers(g, c.page, c.err)
		e, err := epochRun(t, g, nil)
		if !errors.Is(err, portread.ErrEpochUnknown) || e != 0 {
			t.Errorf("%s: epoch %d err %v, want ErrEpochUnknown and no epoch", name, e, err)
		}
	}
}

// The epoch read records nothing and reads only the case list, once, for the session's tenant and principal and the given transaction (INV-12): no complaint list, no evidence item.
func TestTheEpochReadRecordsNothingAndAsksOnlyTheCaseListInTheSessionsScope(t *testing.T) {
	g := csNew()
	answers(g, csCasePage(true, closed("c1", 2)), nil)
	if _, err := epochRun(t, g, nil); err != nil {
		t.Fatal(err)
	}
	if len(g.rec.got) != 0 {
		t.Errorf("the epoch read recorded %d observations, want none", len(g.rec.got))
	}
	if len(g.records.queries) != 0 {
		t.Errorf("the epoch read asked the complaint list %d times, want none", len(g.records.queries))
	}
	if len(g.cases.queries) != 1 || g.cases.queries[0].TenantID != scope.TenantID || g.cases.queries[0].PrincipalID != scope.PrincipalID || g.cases.queries[0].TransactionRef != "TX1" {
		t.Errorf("case queries %+v, want one in the session's scope for TX1", g.cases.queries)
	}
	if g.cases.deadlines != 1 {
		t.Errorf("the case read ran with a deadline %d times, want 1", g.cases.deadlines)
	}
}

// The reader refuses what ReadCases refuses before reading: no deadline on the context, a bad scope, a bad transaction reference, a reader that is not configured for the case list.
func TestTheEpochReadRefusesWhatReadCasesRefusesBeforeReading(t *testing.T) {
	g := csNew()
	if _, err := g.r.NextEpoch(context.Background(), scope, "TX1"); err == nil || errors.Is(err, portread.ErrEpochUnknown) {
		t.Errorf("a context with no deadline: %v, want a refusal that is not 'unknown'", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := g.r.NextEpoch(ctx, scope, "TX 1"); err == nil || errors.Is(err, portread.ErrEpochUnknown) {
		t.Errorf("a bad transaction reference: %v, want a refusal", err)
	}
	if _, err := g.r.NextEpoch(ctx, adapters.Scope{}, "TX1"); err == nil {
		t.Error("an empty scope was accepted")
	}
	g.r.Cases = nil
	if _, err := g.r.NextEpoch(ctx, scope, "TX1"); err == nil {
		t.Error("a reader with no case port answered")
	}
	if len(g.cases.queries) != 0 {
		t.Errorf("a refused call asked the case port %d times", len(g.cases.queries))
	}
}
