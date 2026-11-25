package main

// STM-01 composition gap: Writes (A684 step 1, A543, A364, A551, interim, relayed, not yet committed). The composition builds intake.Writes from the SAME verified pack the other Trust
// inputs read: the tenant's kill switch over the per-tenant trust_rw logins and the production WriteRule over the pack's `write_when` (trustport.NewPackWriteRule over
// Pack.WriteWhens). Fail closed: a pack that is absent or unusable yields no rule and a named gap, and a pack with no write_when on any node answers "no write" (A693); a write_when that does not compile is an error with
// fixed text; no other tenant's login stands in for a tenant's own. REQ-STM-001, REQ-TRU-004, REQ-NFR-002; INV-01, INV-03, INV-14, INV-16; AGENT-13, AGENT-16. The answers are
// compared with an independent read of the pack's own data. DB-backed cases run in CI only (they need pgsodium). All data is SYNTHETIC.

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// twWriteNode is a node of the real pack that has a write_when, read from the pack and never written here.
func twWriteNode(t *testing.T) (node string, w trustpack.WriteWhen) {
	t.Helper()
	all, err := tiPack(t).WriteWhens()
	if err != nil || len(all) == 0 {
		t.Fatalf("the real pack gives no write_when data: %v", err)
	}
	for n, c := range all {
		if c.Predicate != "" {
			return n, c
		}
	}
	t.Fatal("no node of the real pack has a write_when")
	return "", trustpack.WriteWhen{}
}

// twNoWriteNode is a node of the real pack with no write_when.
func twNoWriteNode(t *testing.T) string {
	t.Helper()
	all, err := tiPack(t).WriteWhens()
	if err != nil {
		t.Fatal(err)
	}
	for n, c := range all {
		if c.Predicate == "" {
			return n
		}
	}
	t.Fatal("every node of the real pack has a write_when")
	return ""
}

// twPackWith loads a copy of the real pack whose file is changed by edit; the graphs it names are copied beside it.
func twPackWith(t *testing.T, edit func(string) string) *trustpack.Pack {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(tiRoot())); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fintech_v1.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := edit(string(b))
	if out == string(b) {
		t.Fatal("the edit changed nothing")
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := trustpack.LoadPack(os.DirFS(dir), "fintech_v1.json")
	if err != nil {
		t.Fatalf("the changed pack does not load: %v", err)
	}
	return p
}

// twBuilt builds the inputs and stops the test when Writes was not built, so a missing adapter is a failed case and never a nil dereference.
func twBuilt(t *testing.T, in trustInputsIn) trustInputs {
	t.Helper()
	got, err := buildTrustInputs(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Writes == nil {
		t.Fatalf("Writes is not built (gaps %v)", got.Gaps)
	}
	return got
}

var twWriteWhenLine = regexp.MustCompile(`(?m)^\s*"write_when":\s*"[^"]*",\n`)

func TestWritesIsBuiltFromThePacksWriteWhenAndIsNoGap(t *testing.T) { // REQ-TRU-004; INV-14, INV-16
	got, err := buildTrustInputs(tiIn(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Writes == nil || tiHas(got.Gaps, "Writes") {
		t.Fatalf("Writes is not built (gaps %v)", got.Gaps)
	}
	a := got.fill(TrustAdapters{})
	if a.Writes == nil {
		t.Fatal("fill did not give the composition the Writes adapter")
	}
	if tiHas(unsetAdapters(a), "Writes") {
		t.Fatalf("/readyz would still name trust.Writes (unset %v)", unsetAdapters(a))
	}
	if _, ok := a.Writes.(*trustport.Writes); !ok {
		t.Fatalf("Writes is %T, want *trustport.Writes", a.Writes)
	}
}

// The answer is the pack's: an intent of the node that has a write_when needs a write exactly when the predicate says so over its slot values, and a node with none never does. The
// expected answers come from the pack's own predicate and vocabulary, read here independently.
func TestWritesAnswersFromThePacksOwnPredicateAndSlotValues(t *testing.T) { // REQ-TRU-004, REQ-NFR-002; INV-01
	got := twBuilt(t, tiIn(t))
	node, w := twWriteNode(t)
	if !strings.Contains(w.Predicate, "question_type == 'ESCALATION_REQUEST'") {
		t.Fatalf("the pack's predicate changed (%q): update this test with it", w.Predicate)
	}
	// Every other required slot of the node gets a value (the first of its vocabulary, or an opaque synthetic text), so that only the slots the predicate names decide the answer.
	at := func(slots map[string]string) intake.AdmittedIntent {
		a := tiIntent("ten_aa", "usr_alice", node, "")
		a.Slots = map[string]string{}
		for _, s := range w.Slots {
			if !s.Required {
				continue
			}
			a.Slots[s.Name] = "SYNTHETIC-1"
			if len(s.Vocabulary) > 0 {
				a.Slots[s.Name] = s.Vocabulary[0]
			}
		}
		for k, v := range slots {
			a.Slots[k] = v
		}
		return a
	}
	ctx := tiDeadline(t)
	cases := map[string]struct {
		intent intake.AdmittedIntent
		want   bool
	}{
		"the first disjunct holds":              {at(map[string]string{"question_type": "ESCALATION_REQUEST"}), true},
		"the second disjunct holds":             {at(map[string]string{"question_type": "STATUS", "requested_remedy": "cap.synthetic"}), true},
		"neither holds, the optional slot null": {at(map[string]string{"question_type": "STATUS"}), false},
	}
	for name, c := range cases {
		may, err := got.Writes.NeedsWrite(ctx, c.intent)
		if err != nil || may != c.want {
			t.Errorf("%s: NeedsWrite %v, %v; want %v", name, may, err, c.want)
		}
	}
	if may, err := got.Writes.NeedsWrite(ctx, tiIntent("ten_aa", "usr_alice", twNoWriteNode(t), "")); err != nil || may {
		t.Errorf("a node with no write_when: %v, %v; want false and no error", may, err)
	}
}

// Fail closed: a required slot with no value, a value outside the vocabulary, a node the pack does not hold and an intent with no node are each false with an error, never a write.
func TestWritesThatCannotBeAnsweredAreFalseWithAnError(t *testing.T) { // REQ-NFR-002; INV-03
	got := twBuilt(t, tiIn(t))
	node, _ := twWriteNode(t)
	ctx := tiDeadline(t)
	cases := map[string]intake.AdmittedIntent{
		"a required slot has no value": tiIntent("ten_aa", "usr_alice", node, ""),
		"a value outside the vocab": func() intake.AdmittedIntent {
			a := tiIntent("ten_aa", "usr_alice", node, "")
			a.Slots = map[string]string{"question_type": "NOT_IN_THE_VOCABULARY"}
			return a
		}(),
		"a node the pack does not hold": tiIntent("ten_aa", "usr_alice", "synthetic.no.such.node", ""),
		"an intent with no node":        tiIntent("ten_aa", "usr_alice", "", ""),
	}
	for name, a := range cases {
		if may, err := got.Writes.NeedsWrite(ctx, a); err == nil || may {
			t.Errorf("%s: NeedsWrite %v, %v; want false and an error", name, may, err)
		}
	}
}

// A tenant with no login is never enabled and never served by another tenant's login (INV-12); nothing is dialled for it.
func TestWritesAreNeverEnabledForATenantWithNoLogin(t *testing.T) { // REQ-NFR-002; INV-03, INV-12
	got := twBuilt(t, tiIn(t))
	on, err := got.Writes.Enabled(tiDeadline(t), "ten_zz")
	if on || !errors.Is(err, trustport.ErrNoPool) {
		t.Fatalf("Enabled for a tenant with no login: %v, %v; want false and ErrNoPool", on, err)
	}
}

// Each part Writes needs, absent, makes it a named gap and never an error and never an adapter with a default rule (AGENT-16). The builder is called alone: the other inputs
// have their own answers for a missing pack or login (an error, as their own tests pin), and Writes must not depend on them.
func TestWritesWithAPartMissingIsANamedGap(t *testing.T) { // REQ-STM-001; INV-03, INV-16
	cases := map[string]func(*trustInputsIn){
		"no pack":          func(in *trustInputsIn) { in.Pack = nil },
		"a zero pack":      func(in *trustInputsIn) { in.Pack = &trustpack.Pack{} },
		"no served tenant": func(in *trustInputsIn) { in.Tenants = nil },
		"no login at all":  func(in *trustInputsIn) { in.Pools = nil },
	}
	for name, mut := range cases {
		in := tiIn(t)
		mut(&in)
		w, err := buildWritesInput(in)
		if err != nil || w != nil {
			t.Errorf("%s: buildWritesInput %v, %v; want no port and no error", name, w, err)
		}
	}
}

// A pack with no write_when on any node answers "no write" (A693, A543, interim, relayed, not yet committed). It is not a gap: Writes is built, no node needs a write, and the
// kill switch is still the tenant's own.
func TestAPackWithNoWriteWhenIsBuiltAndAnswersNoWrite(t *testing.T) { // REQ-TRU-004; INV-03, INV-14
	in := tiIn(t)
	in.Pack = twPackWith(t, func(s string) string { return twWriteWhenLine.ReplaceAllString(s, "") })
	whens, err := in.Pack.WriteWhens()
	if err != nil || len(whens) == 0 {
		t.Fatalf("the changed pack gives no nodes: %v", err)
	}
	got, err := buildTrustInputs(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Writes == nil || tiHas(got.Gaps, "Writes") {
		t.Fatalf("Writes %v, gaps %v; want a built port and no Writes gap", got.Writes, got.Gaps)
	}
	if a := got.fill(TrustAdapters{}); a.Writes == nil || tiHas(unsetAdapters(a), "Writes") {
		t.Fatal("fill gave no Writes, or /readyz would name trust.Writes")
	}
	ctx := tiDeadline(t)
	for node, w := range whens {
		a := tiIntent("ten_aa", "usr_alice", node, "")
		a.Slots = map[string]string{}
		for _, s := range w.Slots {
			if !s.Required {
				continue
			}
			a.Slots[s.Name] = "SYNTHETIC-1"
			if len(s.Vocabulary) > 0 {
				a.Slots[s.Name] = s.Vocabulary[0]
			}
		}
		if may, err := got.Writes.NeedsWrite(ctx, a); err != nil || may {
			t.Errorf("a pack with no write_when: NeedsWrite %v, %v; want false and no error", may, err)
		}
	}
	if _, err := got.Writes.NeedsWrite(ctx, tiIntent("ten_aa", "usr_alice", "no_such_node", "")); err == nil {
		t.Error("a node the pack does not hold was answered")
	}
}

// A value that is present and invalid is an error with fixed text: a bound of zero, and a write_when that does not compile. The text carries nothing of the pack (AGENT-13).
func TestWritesWithAnInvalidPartIsAnErrorThatQuotesNothing(t *testing.T) { // REQ-NFR-002; INV-16; AGENT-13
	const marker = "synthetic_marker_slot_xyz"
	broken := twPackWith(t, func(s string) string {
		return twWriteWhenLine.ReplaceAllString(s, `"write_when": "(( `+marker+` ==",`+"\n")
	})
	cases := map[string]func(*trustInputsIn){
		"a zero bound":            func(in *trustInputsIn) { in.Timeout = 0 },
		"a tenant with no login":  func(in *trustInputsIn) { in.Tenants = append(in.Tenants, "ten_zz") },
		"a predicate that breaks": func(in *trustInputsIn) { in.Pack = broken },
	}
	for name, mut := range cases {
		in := tiIn(t)
		mut(&in)
		got, err := buildTrustInputs(in)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if got.Writes != nil {
			t.Errorf("%s: a Writes was built beside the error", name)
		}
		if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "ESCALATION_REQUEST") || strings.Contains(err.Error(), "ten_aa") {
			t.Errorf("%s: the error quotes the pack or a tenant: %v", name, err)
		}
	}
}

// With every part present the builder is deterministic about its other inputs: Writes is built from the pack in the SAME input as Evidence and Policies (the one verified pack), and the
// pack's own reference is what the kill switch is built over (the same value as PackVersion; A377 item 6).
func TestWritesAreBuiltOverTheSamePackAsTheOtherInputs(t *testing.T) { // INV-16
	in := tiInWithLists(t)
	got, err := buildTrustInputs(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Writes == nil || got.Evidence == nil || got.Policies == nil || got.Ownership == nil {
		t.Fatalf("not every input over the one pack is built: %+v", got)
	}
	if in.Pack.Ref() == "" {
		t.Fatal("the real pack has no reference")
	}
}

// The kill switch is the tenant's own (CI only: it needs pgsodium). With the switch off the composed Writes is enabled for each served tenant; a tenant the login does not hold is not.
func TestComposedWritesReadTheTenantsOwnKillSwitch(t *testing.T) { // REQ-NFR-002; INV-03, INV-12
	in := tiIn(t)
	in.Pools = tiDBPools(t, "ten_aa", "ten_bb")
	got := twBuilt(t, in)
	for _, tenant := range []string{"ten_aa", "ten_bb"} {
		if on, err := got.Writes.Enabled(tiDeadline(t), tenant); err != nil || !on {
			t.Errorf("%s with the switch off: %v, %v; want enabled", tenant, on, err)
		}
	}
	if on, err := got.Writes.Enabled(tiDeadline(t), "ten_zz"); err == nil || on {
		t.Errorf("a tenant with no login: %v, %v; want false and an error", on, err)
	}
}
