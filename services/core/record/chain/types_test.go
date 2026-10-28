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
	// The contract lists every identity of the table; the core accepts the ones the table marks as written by the core.
	if got, want := chain.ContractEmitters(), sortedCopy(s.Defs["Emitter"].Enum); !reflect.DeepEqual(got, want) {
		t.Fatalf("contract emitters: table %v, schema %v", got, want)
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

type actorsFile struct {
	Emitters []struct {
		Name  string `json:"name"`
		Scope string `json:"scope"`
	} `json:"emitters"`
	NotEmitters []struct {
		Name string `json:"name"`
	} `json:"not_emitters"`
	PrincipalKinds []struct {
		Name string `json:"name"`
	} `json:"principal_kinds"`
}

func loadActors(t *testing.T) actorsFile {
	t.Helper()
	raw, err := os.ReadFile("actors.json")
	if err != nil {
		t.Fatal(err)
	}
	var f actorsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// The core's sets are derived from the table (actors.json, embedded) and from nothing else: what the core accepts is exactly the table's
// emitters with the scope RUNTIME, the contract lists every emitter of the table, and the difference is exactly the identities the
// table does not mark as written by the core. The names of that difference come from the table, never from this file or the writer.
func TestTheCoreAcceptsExactlyTheRuntimeEmittersOfTheTable(t *testing.T) {
	table, schema := loadActors(t), loadSchema(t)
	var runtime, all, refused []string
	for _, e := range table.Emitters {
		all = append(all, e.Name)
		if e.Scope == "RUNTIME" {
			runtime = append(runtime, e.Name)
		} else {
			refused = append(refused, e.Name)
		}
	}
	if got := chain.Emitters(); !reflect.DeepEqual(got, sortedCopy(runtime)) {
		t.Fatalf("the core accepts %v, the table marks %v as written by the core", got, sortedCopy(runtime))
	}
	if got := chain.ContractEmitters(); !reflect.DeepEqual(got, sortedCopy(all)) || !reflect.DeepEqual(got, sortedCopy(schema.Defs["Emitter"].Enum)) {
		t.Fatalf("the contract lists %v, the table %v", sortedCopy(schema.Defs["Emitter"].Enum), sortedCopy(all))
	}
	if len(all) != 17 || len(runtime) != 16 || len(refused) != 1 {
		t.Fatalf("%d emitters in the contract, %d accepted by the core, %d refused; want 17, 16 and 1", len(all), len(runtime), len(refused))
	}
	for _, n := range table.NotEmitters {
		for _, set := range [][]string{chain.Emitters(), chain.ContractEmitters(), schema.Defs["Emitter"].Enum} {
			for _, have := range set {
				if have == n.Name {
					t.Fatalf("%q is a non-emitter in the table and appears in an emitter set", n.Name)
				}
			}
		}
	}
	var kinds []string
	for _, k := range table.PrincipalKinds {
		kinds = append(kinds, k.Name)
	}
	if got := chain.PrincipalKinds(); !reflect.DeepEqual(got, sortedCopy(kinds)) || !reflect.DeepEqual(got, sortedCopy(schema.Defs["PrincipalKind"].Enum)) {
		t.Fatalf("principal kinds: core %v, table %v, schema %v", got, sortedCopy(kinds), sortedCopy(schema.Defs["PrincipalKind"].Enum))
	}
}
