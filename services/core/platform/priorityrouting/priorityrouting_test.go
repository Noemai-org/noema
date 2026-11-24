package priorityrouting_test

// PCK-09 (ruling A644, interim, relayed, not yet committed; A640; A616; A551; INV-14, INV-21; REQ-CAP-001): the pack's priority routing member maps each SLA phase to a priority and an
// owner queue. A phase with no entry is NORMAL. A priority outside HIGH|NORMAL|LOW and an empty owner queue are refused. The accessor is read only and hands out copies. All values SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/platform/priorityrouting"
)

const good = `{"table_id":"demo.priority_routing","version":1,"label":"SYNTHETIC","note":"synthetic",
"phases":{"BREACHED":{"priority":"HIGH","owner_queue":"QUEUE_A"},"AT_RISK":{"priority":"HIGH","owner_queue":"QUEUE_B"},"RUNNING":{"priority":"LOW","owner_queue":"QUEUE_A"}}}`

func mustParse(t *testing.T, raw string) *priorityrouting.Table {
	t.Helper()
	tab, err := priorityrouting.Parse([]byte(raw))
	if err != nil || tab == nil {
		t.Fatalf("Parse refused a good member: %v", err)
	}
	return tab
}

func with(t *testing.T, edit func(m map[string]any)) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(good), &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func phase(m map[string]any, name string) map[string]any {
	return m["phases"].(map[string]any)[name].(map[string]any)
}

// A644: a phase with an entry returns its priority and its owner queue.
func TestAPhaseWithAnEntryReturnsItsPriorityAndQueue(t *testing.T) {
	tab := mustParse(t, good)
	for name, want := range map[string]priorityrouting.Entry{
		"BREACHED": {Priority: priorityrouting.High, OwnerQueue: "QUEUE_A"},
		"AT_RISK":  {Priority: priorityrouting.High, OwnerQueue: "QUEUE_B"},
		"RUNNING":  {Priority: priorityrouting.Low, OwnerQueue: "QUEUE_A"},
	} {
		got, ok := tab.Lookup(name)
		if !ok || got != want {
			t.Errorf("%s: got %+v, %v; want %+v, true", name, got, ok, want)
		}
	}
}

// A644: a phase with no entry is NORMAL, with no queue, and the answer says it was not listed.
func TestAPhaseWithNoEntryIsNormal(t *testing.T) {
	tab := mustParse(t, good)
	for _, name := range []string{"NOT_STARTED", "", "breached", " BREACHED", "BREACHED ", "UNKNOWN_PHASE"} {
		got, ok := tab.Lookup(name)
		if ok || got.Priority != priorityrouting.Normal || got.OwnerQueue != "" {
			t.Errorf("%q: got %+v, %v; want NORMAL, no queue, false", name, got, ok)
		}
	}
	var none *priorityrouting.Table
	if got, ok := none.Lookup("BREACHED"); ok || got.Priority != priorityrouting.Normal {
		t.Errorf("a nil table: got %+v, %v; want NORMAL, false", got, ok)
	}
}

// A616, A644: a priority outside the vocabulary is refused, in every spelling that is not exactly HIGH, NORMAL or LOW.
func TestAPriorityOutsideTheVocabularyIsRefused(t *testing.T) {
	for _, p := range []any{"URGENT", "high", "High", "", " HIGH", "HIGH ", "HIGH\n", "CRITICAL", nil, 1, true, []any{"HIGH"}} {
		raw := with(t, func(m map[string]any) { phase(m, "BREACHED")["priority"] = p })
		if _, err := priorityrouting.Parse([]byte(raw)); !errors.Is(err, priorityrouting.ErrTable) {
			t.Errorf("priority %#v: got %v; want ErrTable", p, err)
		}
	}
	raw := with(t, func(m map[string]any) { delete(phase(m, "BREACHED"), "priority") })
	if _, err := priorityrouting.Parse([]byte(raw)); !errors.Is(err, priorityrouting.ErrTable) {
		t.Errorf("a missing priority: got %v; want ErrTable", err)
	}
}

// A616, A644: an empty or unusable owner queue is refused.
func TestAnEmptyOrUnusableOwnerQueueIsRefused(t *testing.T) {
	for _, q := range []any{"", " ", "queue_a", "QUEUE A", "QUEUE_A\n", nil, 7, strings.Repeat("A", 65)} {
		raw := with(t, func(m map[string]any) { phase(m, "RUNNING")["owner_queue"] = q })
		if _, err := priorityrouting.Parse([]byte(raw)); !errors.Is(err, priorityrouting.ErrTable) {
			t.Errorf("owner_queue %#v: got %v; want ErrTable", q, err)
		}
	}
	raw := with(t, func(m map[string]any) { delete(phase(m, "RUNNING"), "owner_queue") })
	if _, err := priorityrouting.Parse([]byte(raw)); !errors.Is(err, priorityrouting.ErrTable) {
		t.Errorf("a missing owner_queue: got %v; want ErrTable", err)
	}
}

// INV-21 (no silent choice): a member that is not exactly its shape is refused whole, never partly read.
func TestAMemberThatIsNotExactlyItsShapeIsRefused(t *testing.T) {
	cases := map[string]string{
		"empty":                "",
		"not an object":        `[]`,
		"trailing data":        good + `{}`,
		"unknown top member":   with(t, func(m map[string]any) { m["extra"] = 1 }),
		"unknown entry member": with(t, func(m map[string]any) { phase(m, "RUNNING")["weight"] = 1 }),
		"other label":          with(t, func(m map[string]any) { m["label"] = "PRODUCTION" }),
		"no label":             with(t, func(m map[string]any) { delete(m, "label") }),
		"version zero":         with(t, func(m map[string]any) { m["version"] = 0 }),
		"version a string":     with(t, func(m map[string]any) { m["version"] = "1" }),
		"version a fraction":   with(t, func(m map[string]any) { m["version"] = 1.5 }),
		"no table id":          with(t, func(m map[string]any) { delete(m, "table_id") }),
		"no phases":            with(t, func(m map[string]any) { delete(m, "phases") }),
		"empty phases":         with(t, func(m map[string]any) { m["phases"] = map[string]any{} }),
		"phases a list":        with(t, func(m map[string]any) { m["phases"] = []any{} }),
		"phase name lower":     with(t, func(m map[string]any) { m["phases"].(map[string]any)["breached"] = phase(m, "RUNNING") }),
		"entry null":           with(t, func(m map[string]any) { m["phases"].(map[string]any)["RUNNING"] = nil }),
		"repeated phase":       strings.Replace(good, `"RUNNING":{"priority":"LOW","owner_queue":"QUEUE_A"}`, `"RUNNING":{"priority":"LOW","owner_queue":"QUEUE_A"},"RUNNING":{"priority":"HIGH","owner_queue":"QUEUE_A"}`, 1),
		"repeated member":      strings.Replace(good, `"version":1,`, `"version":1,"version":2,`, 1),
		"repeated entry field": strings.Replace(good, `"priority":"LOW"`, `"priority":"LOW","priority":"HIGH"`, 1),
	}
	for name, raw := range cases {
		if _, err := priorityrouting.Parse([]byte(raw)); !errors.Is(err, priorityrouting.ErrTable) {
			t.Errorf("%s: got %v; want ErrTable", name, err)
		}
	}
	if strings.Contains(priorityrouting.ErrTable.Error(), "QUEUE") {
		t.Error("the refusal text holds content")
	}
}

// A551: the accessor is read only. Phases is a sorted copy; changing it, or the bytes handed to Parse, changes nothing.
func TestTheAccessorIsReadOnlyAndHandsOutCopies(t *testing.T) {
	raw := []byte(good)
	tab, err := priorityrouting.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		raw[i] = 'x'
	}
	phases := tab.Phases()
	want := []string{"AT_RISK", "BREACHED", "RUNNING"}
	if !reflect.DeepEqual(phases, want) || !sort.StringsAreSorted(phases) {
		t.Fatalf("Phases = %v; want %v", phases, want)
	}
	phases[0] = "TAMPERED"
	if again := tab.Phases(); !reflect.DeepEqual(again, want) {
		t.Errorf("a caller changed the table through Phases: %v", again)
	}
	e, _ := tab.Lookup("BREACHED")
	e.Priority, e.OwnerQueue = priorityrouting.Low, "OTHER"
	if again, _ := tab.Lookup("BREACHED"); again.Priority != priorityrouting.High || again.OwnerQueue != "QUEUE_A" {
		t.Errorf("a caller changed the table through an Entry: %+v", again)
	}
	if _, ok := tab.Lookup("TAMPERED"); ok {
		t.Error("a phase appeared that the member does not list")
	}
}

// INV-21: the same bytes give the same table, whatever the order the phases were written in.
func TestTheSameMemberGivesTheSameTable(t *testing.T) {
	a := mustParse(t, good)
	b := mustParse(t, `{"table_id":"demo.priority_routing","version":1,"label":"SYNTHETIC","phases":{"RUNNING":{"owner_queue":"QUEUE_A","priority":"LOW"},"AT_RISK":{"priority":"HIGH","owner_queue":"QUEUE_B"},"BREACHED":{"priority":"HIGH","owner_queue":"QUEUE_A"}}}`)
	if !reflect.DeepEqual(a.Phases(), b.Phases()) {
		t.Fatal("phases differ")
	}
	for _, p := range a.Phases() {
		x, _ := a.Lookup(p)
		y, _ := b.Lookup(p)
		if x != y {
			t.Errorf("%s: %+v vs %+v", p, x, y)
		}
	}
}

// A644, A333: the committed member is pinned by the manifest, loads through the verified loader and routes every SLA phase the pack names (and no other), to a queue the pack allows.
func TestTheCommittedMemberCoversThePacksSLAPhases(t *testing.T) {
	packs := filepath.Join("..", "..", "..", "..", "packs")
	p, err := packload.Load(packs, "fintech/manifest.v1.json", 1<<22)
	if err != nil {
		t.Fatalf("the committed pack does not load: %v", err)
	}
	if len(p.PriorityRouting) == 0 {
		t.Fatal("the committed manifest pins no priority routing member")
	}
	tab, err := priorityrouting.Parse(p.PriorityRouting)
	if err != nil {
		t.Fatalf("the committed member is not usable: %v", err)
	}
	var pack struct {
		Vars struct {
			SLAPhase struct {
				Values []string `json:"values"`
			} `json:"sla_phase"`
		} `json:"predicate_variables"`
		Allowlist []string `json:"routing_allowlist"`
	}
	if err := json.Unmarshal(p.Pack, &pack); err != nil {
		t.Fatal(err)
	}
	phases := append([]string(nil), pack.Vars.SLAPhase.Values...)
	sort.Strings(phases)
	if len(phases) == 0 || !reflect.DeepEqual(tab.Phases(), phases) {
		t.Fatalf("the member routes %v; the pack's sla_phase is %v", tab.Phases(), phases)
	}
	allowed := map[string]bool{}
	for _, q := range pack.Allowlist {
		allowed[q] = true
	}
	for _, ph := range tab.Phases() {
		if e, _ := tab.Lookup(ph); !allowed[e.OwnerQueue] {
			t.Errorf("%s routes to %q, which the pack does not allow", ph, e.OwnerQueue)
		}
	}
}

// A551, A644: a pack that fails verification yields no member: Load returns nothing, and nothing is left to parse.
func TestAPackThatFailsVerificationYieldsNoTable(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "..", "..", "packs"), dir)
	member := filepath.Join(dir, "fintech", "routing", "priority_routing.v1.json")
	b, err := os.ReadFile(member)
	if err != nil {
		t.Fatalf("the member is not at fintech/routing/priority_routing.v1.json: %v", err)
	}
	if err := os.WriteFile(member, []byte(strings.Replace(string(b), `"HIGH"`, `"LOW"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := packload.Load(dir, "fintech/manifest.v1.json", 1<<22)
	if !errors.Is(err, packload.ErrHash) {
		t.Fatalf("got %v; want ErrHash", err)
	}
	if p.PriorityRouting != nil || p.Pack != nil {
		t.Error("a refused pack returned content")
	}
	if tab, err := priorityrouting.Parse(p.PriorityRouting); err == nil || tab != nil {
		t.Errorf("a table came out of a refused pack: %v", err)
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.Walk(from, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
