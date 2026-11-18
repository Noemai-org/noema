package intakeadapters

// The Clocks adapter of the intake (T12; REQ-TRU-006; A205 Q1, A225; INV-14, INV-16, INV-21). The shipped fintech pack, and the real clocks.Evaluate over what the adapter answers.
// The anchor source has no production implementation yet and is the test's own. Everything here is SYNTHETIC; the numbers are the test's, the adapter holds none.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// the test's own at-risk fraction (a quarter of the duration remaining) and routes
var (
	quarter = clocks.Ratio{Num: 1, Den: 4}
	routes  = map[clocks.Phase]string{clocks.AtRisk: "fin.reason.sla_at_risk", clocks.Breached: "fin.reason.sla_breached"}
)

type anchorCall struct{ tenant, snapshot, event string }

// anchors answers one time per anchor event, and records what it was asked.
type anchors struct {
	at    map[string]int64 // an event that is not in the map has not happened
	err   error
	calls []anchorCall
}

func (a *anchors) AnchorTime(_ context.Context, tenant, snapshot, event string) (int64, bool, error) {
	a.calls = append(a.calls, anchorCall{tenant, snapshot, event})
	if a.err != nil {
		return 0, false, a.err
	}
	at, ok := a.at[event]
	return at, ok, nil
}

// packClock is the shipped pack's first clock, read from the file so that no clock name is written here.
func packClock(t *testing.T) contracts.ClockV1 {
	t.Helper()
	var p struct {
		Clocks []contracts.ClockV1 `json:"clocks"`
	}
	if err := json.Unmarshal(realPack(t), &p); err != nil || len(p.Clocks) == 0 {
		t.Fatalf("the shipped pack has no clock: %v", err)
	}
	return p.Clocks[0]
}

func clockBook(t *testing.T, raw []byte) *ClockBook {
	t.Helper()
	b, err := LoadClockBook(raw)
	if err != nil {
		t.Fatalf("LoadClockBook: %v", err)
	}
	return b
}

func newClocks(t *testing.T, b *ClockBook, a Anchors, r map[clocks.Phase]string) *Clocks {
	t.Helper()
	c, err := NewClocks(ClocksConfig{Book: b, Anchors: a, AtRisk: quarter, Routes: r})
	if err != nil {
		t.Fatalf("NewClocks: %v", err)
	}
	return c
}

func clockQuery() intake.ClockQuery {
	return intake.ClockQuery{TenantID: tenantA, IntentNode: node, SnapshotID: "snap_1"}
}

// editPack is the shipped pack after f has changed its top-level object.
func editPack(t *testing.T, f func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(realPack(t), &m); err != nil {
		t.Fatal(err)
	}
	f(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// REQ-TRU-006, INV-14: the clocks of the intent node are the pack's: the node's trust profile names them. A node with no trust profile has none (an empty set, not an error). The adapter
// holds no clock of its own.
func TestTheClocksThatApplyAreTheOnesTheNodesTrustProfileNames(t *testing.T) {
	clk := packClock(t)
	c := newClocks(t, clockBook(t, realPack(t)), &anchors{}, nil)
	set, err := c.Applicable(ctx(t), clockQuery())
	if err != nil || len(set.Clocks) != 1 || set.Clocks[0].Clock != clk {
		t.Fatalf("clocks %+v, %v; want the pack's clock %+v", set.Clocks, err, clk)
	}
	none := clockQuery()
	none.IntentNode = "fin.meta.out_of_scope"
	if set, err := c.Applicable(ctx(t), none); err != nil || len(set.Clocks) != 0 {
		t.Fatalf("a node with no trust profile: %+v, %v; want an empty set", set, err)
	}
	// a node the pack does not know is not "no clock": the intake would skip every clock check for it
	ghost := clockQuery()
	ghost.IntentNode = "fin.payments.ghost"
	if set, err := c.Applicable(ctx(t), ghost); !errors.Is(err, ErrQuery) || len(set.Clocks) != 0 {
		t.Fatalf("an unknown node: %+v, %v; want ErrQuery", set, err)
	}
}

// ClockInstance.Anchor: nil when the anchor event has not happened (NOT_STARTED, never assumed), the event's time when it has. Each instance has a pointer of its own, and the source
// is asked for the tenant and the snapshot of the query and the event the pack's clock names.
func TestAnAnchorIsNilWhenTheEventHasNotHappenedAndTheEventsTimeWhenItHas(t *testing.T) {
	clk := packClock(t)
	a := &anchors{}
	c := newClocks(t, clockBook(t, realPack(t)), a, nil)
	set, err := c.Applicable(ctx(t), clockQuery())
	if err != nil || len(set.Clocks) != 1 || set.Clocks[0].Anchor != nil {
		t.Fatalf("the event has not happened: %+v, %v", set.Clocks, err)
	}
	if want := []anchorCall{{tenantA, "snap_1", clk.AnchorEvent}}; !reflect.DeepEqual(a.calls, want) {
		t.Fatalf("the source was asked %v, want %v", a.calls, want)
	}
	a.at = map[string]int64{clk.AnchorEvent: t2}
	first, err := c.Applicable(ctx(t), clockQuery())
	if err != nil || first.Clocks[0].Anchor == nil || *first.Clocks[0].Anchor != t2 {
		t.Fatalf("the event happened at %d: %+v, %v", t2, first.Clocks, err)
	}
	second, _ := c.Applicable(ctx(t), clockQuery())
	if first.Clocks[0].Anchor == second.Clocks[0].Anchor {
		t.Fatal("two answers share one anchor pointer")
	}
	*first.Clocks[0].Anchor = 1
	if third, _ := c.Applicable(ctx(t), clockQuery()); *third.Clocks[0].Anchor != t2 {
		t.Fatal("a caller's change to an anchor reached the adapter")
	}
}

// An anchor the source cannot give is an error, never NOT_STARTED; a time that is not a time is an error; the tenant and the snapshot are the query's.
func TestAnAnchorThatCannotBeReadOrIsNotATimeIsAnErrorAndNeverNotStarted(t *testing.T) {
	clk := packClock(t)
	boom := errors.New("the snapshot could not be read")
	c := newClocks(t, clockBook(t, realPack(t)), &anchors{err: boom}, nil)
	if set, err := c.Applicable(ctx(t), clockQuery()); !errors.Is(err, boom) || len(set.Clocks) != 0 {
		t.Fatalf("a failing source: %+v, %v", set, err)
	}
	for name, at := range map[string]int64{"zero": 0, "negative": -3} {
		c := newClocks(t, clockBook(t, realPack(t)), &anchors{at: map[string]int64{clk.AnchorEvent: at}}, nil)
		if set, err := c.Applicable(ctx(t), clockQuery()); !errors.Is(err, ErrAnchor) || len(set.Clocks) != 0 {
			t.Errorf("an anchor of %s: %+v, %v; want ErrAnchor", name, set, err)
		}
	}
}

// What the adapter answers is what clocks.Evaluate needs: not started with no anchor; running; at risk below the fraction the configuration gave; breached at the deadline. The fraction
// is the configuration's and nothing else (INV-14, A205 Q1).
func TestTheAnswerIsWhatEvaluateNeedsAndTheAtRiskFractionIsTheConfigurations(t *testing.T) {
	clk := packClock(t)
	a := &anchors{}
	c := newClocks(t, clockBook(t, realPack(t)), a, nil)
	now := late
	for name, c0 := range map[string]struct {
		anchor *int64
		want   clocks.Phase
	}{
		"no anchor":               {nil, clocks.NotStarted},
		"just started":            {ptr(now - 1), clocks.Running},
		"a quarter left":          {ptr(now - clk.DurationSeconds*3/4), clocks.Running}, // remaining is not below the quarter
		"just under a quarter":    {ptr(now - clk.DurationSeconds*3/4 - 1), clocks.AtRisk},
		"one second from the end": {ptr(now - clk.DurationSeconds + 1), clocks.AtRisk},
		"at the deadline":         {ptr(now - clk.DurationSeconds), clocks.Breached},
	} {
		a.at = map[string]int64{}
		if c0.anchor != nil {
			a.at[clk.AnchorEvent] = *c0.anchor
		}
		set, err := c.Applicable(ctx(t), clockQuery())
		if err != nil {
			t.Fatal(err)
		}
		if set.AtRisk != quarter {
			t.Fatalf("%s: the at-risk fraction is %+v, want the configuration's %+v", name, set.AtRisk, quarter)
		}
		st, err := clocks.Evaluate(set.Clocks[0].Clock, set.Clocks[0].Anchor, now, set.AtRisk)
		if err != nil || st.Phase != c0.want {
			t.Errorf("%s: phase %s (%v), want %s", name, st.Phase, err, c0.want)
		}
	}
	// another fraction is another answer: nothing is held here
	other := clocks.Ratio{Num: 1, Den: 2}
	o, err := NewClocks(ClocksConfig{Book: clockBook(t, realPack(t)), Anchors: a, AtRisk: other})
	if err != nil {
		t.Fatal(err)
	}
	if set, _ := o.Applicable(ctx(t), clockQuery()); set.AtRisk != other {
		t.Fatalf("the fraction is %+v, want %+v", set.AtRisk, other)
	}
}

func ptr(v int64) *int64 { return &v }

// A query the adapter cannot answer is refused before the source is asked.
func TestAClockQueryItCannotAnswerIsRefusedBeforeTheSourceIsAsked(t *testing.T) {
	a := &anchors{}
	c := newClocks(t, clockBook(t, realPack(t)), a, routes)
	for name, mut := range map[string]func(*intake.ClockQuery){
		"no tenant": func(q *intake.ClockQuery) { q.TenantID = "" }, "no node": func(q *intake.ClockQuery) { q.IntentNode = "" },
		"no snapshot": func(q *intake.ClockQuery) { q.SnapshotID = "" },
	} {
		q := clockQuery()
		mut(&q)
		if set, err := c.Applicable(ctx(t), q); !errors.Is(err, ErrQuery) || len(set.Clocks) != 0 {
			t.Errorf("Applicable with %s: %+v, %v; want ErrQuery", name, set, err)
		}
		if name != "no snapshot" {
			if r, err := c.Route(ctx(t), q, []clocks.Status{{Phase: clocks.Breached}}); !errors.Is(err, ErrQuery) || r.Escalate {
				t.Errorf("Route with %s: %+v, %v; want ErrQuery", name, r, err)
			}
		}
	}
	if len(a.calls) != 0 {
		t.Fatalf("a refused query asked the source %d times", len(a.calls))
	}
	// a node with no clock needs no snapshot
	none := intake.ClockQuery{TenantID: tenantA, IntentNode: "fin.meta.out_of_scope"}
	if _, err := c.Applicable(ctx(t), none); err != nil {
		t.Fatalf("a node with no clock and no snapshot: %v", err)
	}
}

// REQ-TRU-006, A225: the routing is the pack's: a phase it routes escalates with its reason code, the more severe phase wins, and a phase it does not route does not escalate. A breached clock with
// no route is not escalated here: it is the intake's own CLOCK_BREACHED_UNROUTED, so the adapter must answer "not escalating" and never invent a route.
func TestRoutingEscalatesOnlyForAPhaseThePackRoutesAndTheMoreSeverePhaseWins(t *testing.T) {
	st := func(p clocks.Phase) clocks.Status { return clocks.Status{Phase: p} }
	c := newClocks(t, clockBook(t, realPack(t)), &anchors{}, routes)
	for name, c0 := range map[string]struct {
		statuses []clocks.Status
		want     intake.ClockRouting
	}{
		"no statuses":            {nil, intake.ClockRouting{}},
		"not started":            {[]clocks.Status{st(clocks.NotStarted)}, intake.ClockRouting{}},
		"running":                {[]clocks.Status{st(clocks.Running)}, intake.ClockRouting{}},
		"at risk":                {[]clocks.Status{st(clocks.AtRisk)}, intake.ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_at_risk"}},
		"breached":               {[]clocks.Status{st(clocks.Breached)}, intake.ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_breached"}},
		"breached beats at risk": {[]clocks.Status{st(clocks.AtRisk), st(clocks.Breached)}, intake.ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_breached"}},
		"breached first":         {[]clocks.Status{st(clocks.Breached), st(clocks.AtRisk)}, intake.ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_breached"}},
		"running beside at risk": {[]clocks.Status{st(clocks.Running), st(clocks.AtRisk)}, intake.ClockRouting{Escalate: true, ReasonCode: "fin.reason.sla_at_risk"}},
	} {
		if got, err := c.Route(ctx(t), clockQuery(), c0.statuses); err != nil || got != c0.want {
			t.Errorf("%s: %+v, %v; want %+v", name, got, err, c0.want)
		}
	}
	// the pack routes only an at-risk clock: a breached one is not escalated here
	onlyAtRisk := newClocks(t, clockBook(t, realPack(t)), &anchors{}, map[clocks.Phase]string{clocks.AtRisk: "fin.reason.sla_at_risk"})
	if got, err := onlyAtRisk.Route(ctx(t), clockQuery(), []clocks.Status{st(clocks.Breached)}); err != nil || got.Escalate || got.ReasonCode != "" {
		t.Errorf("a breached clock the pack does not route: %+v, %v; want not escalating", got, err)
	}
	// no routing at all
	none := newClocks(t, clockBook(t, realPack(t)), &anchors{}, nil)
	for _, p := range []clocks.Phase{clocks.NotStarted, clocks.Running, clocks.AtRisk, clocks.Breached} {
		if got, err := none.Route(ctx(t), clockQuery(), []clocks.Status{st(p)}); err != nil || got.Escalate || got.ReasonCode != "" {
			t.Errorf("no routes, %s: %+v, %v; want not escalating", p, got, err)
		}
	}
	// a phase that is none of the four is not data the routing can read
	if got, err := c.Route(ctx(t), clockQuery(), []clocks.Status{st("SOON")}); !errors.Is(err, ErrQuery) || got.Escalate {
		t.Errorf("an unknown phase: %+v, %v; want ErrQuery", got, err)
	}
}

// The adapter holds no part it cannot run without, and a fraction or a route it cannot use is refused when it is built.
func TestTheClocksAdapterRefusesAnUnusableConfiguration(t *testing.T) {
	good := ClocksConfig{Book: clockBook(t, realPack(t)), Anchors: &anchors{}, AtRisk: quarter, Routes: routes}
	with := func(f func(*ClocksConfig)) ClocksConfig {
		c := good
		f(&c)
		return c
	}
	for name, c := range map[string]ClocksConfig{
		"no book":                             with(func(c *ClocksConfig) { c.Book = nil }),
		"no anchors":                          with(func(c *ClocksConfig) { c.Anchors = nil }),
		"no fraction":                         with(func(c *ClocksConfig) { c.AtRisk = clocks.Ratio{} }),
		"a zero denominator":                  with(func(c *ClocksConfig) { c.AtRisk = clocks.Ratio{Num: 1, Den: 0} }),
		"a negative numerator":                with(func(c *ClocksConfig) { c.AtRisk = clocks.Ratio{Num: -1, Den: 4} }),
		"a fraction above one":                with(func(c *ClocksConfig) { c.AtRisk = clocks.Ratio{Num: 5, Den: 4} }),
		"a route for an unknown phase":        with(func(c *ClocksConfig) { c.Routes = map[clocks.Phase]string{"SOON": "fin.reason.sla_at_risk"} }),
		"an empty reason code":                with(func(c *ClocksConfig) { c.Routes = map[clocks.Phase]string{clocks.AtRisk: ""} }),
		"a core reason code":                  with(func(c *ClocksConfig) { c.Routes = map[clocks.Phase]string{clocks.AtRisk: "INTERNAL_ERROR"} }),
		"a code with no name":                 with(func(c *ClocksConfig) { c.Routes = map[clocks.Phase]string{clocks.AtRisk: "fin.reason."} }),
		"a code without the reason namespace": with(func(c *ClocksConfig) { c.Routes = map[clocks.Phase]string{clocks.AtRisk: "fin.sla_at_risk"} }),
	} {
		if c0, err := NewClocks(c); !errors.Is(err, ErrConfig) || c0 != nil {
			t.Errorf("%s: %v, %v; want ErrConfig and no adapter", name, c0, err)
		}
	}
	// a fraction of zero (nothing is ever at risk) and of one are usable: the configuration decides
	for _, r := range []clocks.Ratio{{Num: 0, Den: 4}, {Num: 4, Den: 4}} {
		g := good
		g.AtRisk = r
		if _, err := NewClocks(g); err != nil {
			t.Errorf("fraction %+v: %v", r, err)
		}
	}
	// the routes are copied: the caller's map changed later does not change the adapter
	mine := map[clocks.Phase]string{clocks.Breached: "fin.reason.sla_breached"}
	c := newClocks(t, good.Book, good.Anchors, mine)
	mine[clocks.Breached] = "fin.reason.changed"
	delete(mine, clocks.Breached)
	if got, _ := c.Route(ctx(t), clockQuery(), []clocks.Status{{Phase: clocks.Breached}}); got.ReasonCode != "fin.reason.sla_breached" {
		t.Fatalf("the adapter's routes follow the caller's map: %+v", got)
	}
}

// The clock book is the pack's clocks as data, checked when it loads: anything the adapter would have to guess is ErrPack and no book.
func TestAPackWhoseClocksCannotBeUsedIsRefused(t *testing.T) {
	clk := packClock(t)
	badClock := func(edit func(c map[string]any)) []byte {
		return editPack(t, func(m map[string]any) {
			c := m["clocks"].([]any)[0].(map[string]any)
			edit(c)
		})
	}
	for name, raw := range map[string][]byte{
		"not JSON":                      []byte(`[`),
		"no clocks key":                 editPack(t, func(m map[string]any) { delete(m, "clocks") }),
		"no trust profiles":             editPack(t, func(m map[string]any) { delete(m, "trust_profiles") }),
		"no intent nodes":               editPack(t, func(m map[string]any) { delete(m, "intent_nodes") }),
		"a clock with a zero duration":  badClock(func(c map[string]any) { c["duration_seconds"] = 0 }),
		"a clock with an unknown label": badClock(func(c map[string]any) { c["label"] = "GUESSED" }),
		"a clock with no anchor event":  badClock(func(c map[string]any) { delete(c, "anchor_event") }),
		"a clock with an unknown field": badClock(func(c map[string]any) { c["at_risk"] = 0.25 }),
		"a clock defined twice":         editPack(t, func(m map[string]any) { m["clocks"] = append(m["clocks"].([]any), m["clocks"].([]any)[0]) }),
		"a profile naming an unknown clock": editPack(t, func(m map[string]any) {
			m["trust_profiles"].([]any)[0].(map[string]any)["clocks"] = []any{"fin.clock.ghost"}
		}),
		"a node naming an unknown profile": editPack(t, func(m map[string]any) {
			m["intent_nodes"].([]any)[0].(map[string]any)["trust_profile"] = "fin.trust.ghost"
		}),
		"a profile defined twice": editPack(t, func(m map[string]any) {
			m["trust_profiles"] = append(m["trust_profiles"].([]any), m["trust_profiles"].([]any)[0])
		}),
		"a node defined twice": editPack(t, func(m map[string]any) {
			m["intent_nodes"] = append(m["intent_nodes"].([]any), m["intent_nodes"].([]any)[0])
		}),
		"a profile naming one clock twice": editPack(t, func(m map[string]any) {
			m["trust_profiles"].([]any)[0].(map[string]any)["clocks"] = []any{clk.ClockId, clk.ClockId}
		}),
	} {
		if b, err := LoadClockBook(raw); !errors.Is(err, ErrPack) || b != nil {
			t.Errorf("%s: %v, %v; want ErrPack and no book", name, b, err)
		}
	}
}

// Two clocks of one profile are both answered, in the profile's order, each asked for its own anchor event.
func TestTwoClocksOfOneProfileAreBothAnsweredInTheProfilesOrder(t *testing.T) {
	first := packClock(t)
	second := contracts.ClockV1{ClockId: "fin.clock.second", AnchorEvent: "second_event", DurationSeconds: 3600, Label: contracts.ClockV1LabelSYNTHETIC}
	raw := editPack(t, func(m map[string]any) {
		m["clocks"] = append(m["clocks"].([]any), map[string]any{"clock_id": string(second.ClockId), "anchor_event": second.AnchorEvent, "duration_seconds": second.DurationSeconds, "label": "SYNTHETIC"})
		m["trust_profiles"].([]any)[0].(map[string]any)["clocks"] = []any{string(second.ClockId), string(first.ClockId)}
	})
	a := &anchors{at: map[string]int64{second.AnchorEvent: t3}}
	set, err := newClocks(t, clockBook(t, raw), a, nil).Applicable(ctx(t), clockQuery())
	if err != nil || len(set.Clocks) != 2 || set.Clocks[0].Clock != second || set.Clocks[1].Clock != first ||
		set.Clocks[0].Anchor == nil || *set.Clocks[0].Anchor != t3 || set.Clocks[1].Anchor != nil {
		t.Fatalf("%+v, %v", set.Clocks, err)
	}
}
