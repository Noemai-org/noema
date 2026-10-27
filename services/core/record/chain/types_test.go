package chain_test

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

type schemaDefs struct {
	Defs map[string]struct {
		Enum []string `json:"enum"`
	} `json:"$defs"`
}

func loadSchema(t *testing.T) schemaDefs {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/schemas/record-event.v3.json")
	if err != nil {
		t.Fatal(err)
	}
	var s schemaDefs
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// The writer's closed sets are the contract's enums: the writer, the verifier and record-event.v3.json cannot drift apart.
func TestWriterSetsEqualTheV3SchemaEnums(t *testing.T) {
	s := loadSchema(t)
	if got, want := chain.EventTypes(), sortedCopy(s.Defs["RecordEventType"].Enum); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types: writer %v, schema %v", got, want)
	}
	if got, want := chain.GrantVoidReasons(), sortedCopy(s.Defs["GrantVoidReason"].Enum); !reflect.DeepEqual(got, want) {
		t.Fatalf("grant-void reasons: writer %v, schema %v", got, want)
	}
	// The writer accepts every emitter of the contract but one: the benchmark runner's identity, which the core never writes as and
	// which the architecture lint keeps out of core code (see types.go). Any other difference is drift.
	writer := map[string]bool{}
	for _, e := range chain.Emitters() {
		writer[e] = true
	}
	var missing []string
	schema := map[string]bool{}
	for _, e := range s.Defs["Emitter"].Enum {
		schema[e] = true
		if !writer[e] {
			missing = append(missing, e)
		}
	}
	for e := range writer {
		if !schema[e] {
			t.Fatalf("the writer accepts %q, which the schema does not list", e)
		}
	}
	if len(missing) != 1 {
		t.Fatalf("emitters the schema lists and the writer refuses: %v, want exactly one", missing)
	}
	if got, want := chain.PrincipalKinds(), sortedCopy(s.Defs["PrincipalKind"].Enum); !reflect.DeepEqual(got, want) {
		t.Fatalf("principal kinds: writer %v, schema %v", got, want)
	}
	if n := len(s.Defs["GrantVoidReason"].Enum); n != 9 {
		t.Fatalf("the schema lists %d grant-void reasons, want the nine of PERD 27.1", n)
	}
}

// The sets are the ones PERD v1.17 section 32.1 names, typed here from the PERD and not read from the schema or the writer:
// the ten identities of the section 41.1 table, the seven section 34.1 logical services with an adapter identity, and the six
// principal kinds (the Founder's decision of 27 October 2026). fault-controller and fixture-loader are benchmark-only and are not emitters.
func TestTheClosedSetsAreThoseOfPERD32_1(t *testing.T) {
	// 16 of the 17: the benchmark runner's identity (section 41.1) is in the contract and not accepted by the core writer (types.go).
	wantEmitters := sortedCopy([]string{"core-edge", "core-trust", "core-execution", "core-outcome", "core-record", "core-evidence", "core-workflows",
		"model-service", "admin", "mock-ledger", "mock-switch", "mock-destination", "mock-customer", "mock-policy", "mock-case", "mock-clock"})
	if got := chain.Emitters(); !reflect.DeepEqual(got, wantEmitters) {
		t.Fatalf("emitters %v, want %v", got, wantEmitters)
	}
	wantKinds := sortedCopy([]string{"customer", "operator", "approver", "auditor", "regulator", "admin"})
	if got := chain.PrincipalKinds(); !reflect.DeepEqual(got, wantKinds) {
		t.Fatalf("principal kinds %v, want %v", got, wantKinds)
	}
}
