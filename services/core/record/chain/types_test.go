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
	raw, err := os.ReadFile("../../../../contracts/schemas/record-event.v2.json")
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

// The writer's closed sets are the contract's enums: the writer, the verifier and record-event.v2.json cannot drift apart.
func TestWriterSetsEqualTheV2SchemaEnums(t *testing.T) {
	s := loadSchema(t)
	if got, want := chain.EventTypes(), sortedCopy(s.Defs["RecordEventType"].Enum); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types: writer %v, schema %v", got, want)
	}
	if got, want := chain.GrantVoidReasons(), sortedCopy(s.Defs["GrantVoidReason"].Enum); !reflect.DeepEqual(got, want) {
		t.Fatalf("grant-void reasons: writer %v, schema %v", got, want)
	}
	if n := len(s.Defs["GrantVoidReason"].Enum); n != 9 {
		t.Fatalf("the schema lists %d grant-void reasons, want the nine of PERD 27.1", n)
	}
}
