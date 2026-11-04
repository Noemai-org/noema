package chain

// PLT-09 (REQ-TST-010): the actors table is loaded by a function that returns an error, not by an init() that panics. A table that
// cannot load makes every append and every verification fail closed. REQ-AUD-001, INV-11.

import (
	"errors"
	"strings"
	"testing"
)

func TestTheShippedActorsTableLoads(t *testing.T) {
	sets, err := loadActors(actorsJSON)
	if err != nil {
		t.Fatalf("the embedded actors.json must load: %v", err)
	}
	if len(sets.contractSet) != 17 || len(sets.emitters) != 16 || len(sets.principalKinds) == 0 {
		t.Fatalf("%d contract identities, %d emitters, %d principal kinds; want 17, 16 and some", len(sets.contractSet), len(sets.emitters), len(sets.principalKinds))
	}
	if actorsErr != nil {
		t.Fatalf("the package variables must hold the loaded table: %v", actorsErr)
	}
}

func TestMalformedActorsTablesAreRefusedWithAnError(t *testing.T) {
	const kinds = `"principal_kinds":[{"name":"customer","section":"s","note":"n"}]`
	emitter := func(name string) string {
		return `{"name":"` + name + `","section":"PERD 41.1","scope":"RUNTIME","note":"n"}`
	}
	cases := map[string]string{
		"not JSON":                 `actors`,
		"unknown field":            `{"emitters":[` + emitter("core-edge") + `],` + kinds + `,"surprise":1}`,
		"emitter name malformed":   `{"emitters":[` + emitter("Core Edge") + `],` + kinds + `}`,
		"emitter repeated":         `{"emitters":[` + emitter("core-edge") + `,` + emitter("core-edge") + `],` + kinds + `}`,
		"emitter without section":  `{"emitters":[{"name":"core-edge","section":"","scope":"RUNTIME","note":"n"}],` + kinds + `}`,
		"emitter without scope":    `{"emitters":[{"name":"core-edge","section":"s","scope":"","note":"n"}],` + kinds + `}`,
		"non-emitter malformed":    `{"emitters":[` + emitter("core-edge") + `],"not_emitters":[{"name":"X Y","section":"s","reason":"r"}],` + kinds + `}`,
		"non-emitter is emitter":   `{"emitters":[` + emitter("core-edge") + `],"not_emitters":[{"name":"core-edge","section":"s","reason":"r"}],` + kinds + `}`,
		"principal kind malformed": `{"emitters":[` + emitter("core-edge") + `],"principal_kinds":[{"name":"Bad Kind","section":"s","note":"n"}]}`,
		"principal kind repeated":  `{"emitters":[` + emitter("core-edge") + `],"principal_kinds":[{"name":"customer","section":"s","note":"n"},{"name":"customer","section":"s","note":"n"}]}`,
		"no emitters":              `{"emitters":[],` + kinds + `}`,
		"no principal kinds":       `{"emitters":[` + emitter("core-edge") + `],"principal_kinds":[]}`,
	}
	for name, data := range cases {
		if _, err := loadActors([]byte(data)); !errors.Is(err, ErrActorsTable) {
			t.Errorf("%s: want ErrActorsTable, got %v", name, err)
		}
	}
	if _, err := loadActors([]byte(`{"emitters":[` + emitter("core-edge") + `],` + kinds + `}`)); err != nil {
		t.Fatalf("the control table must load: %v", err)
	}
}

// Fail closed: when the table did not load, no identity is an emitter, no actor is valid, and the error says why.
func TestAFailedLoadRefusesEverything(t *testing.T) {
	savedSets, savedErr := actors, actorsErr
	t.Cleanup(func() { actors, actorsErr = savedSets, savedErr })
	actors, actorsErr = actorSets{}, errors.New("simulated bad table")

	if validActor("core-execution", "core-execution") || validActor("customer:cus_1", "core-execution") {
		t.Error("an actor is valid although no table loaded")
	}
	err := validate(Event{TenantID: "ten_demo", CorrelationID: "corr_1", ObjectRef: "grant:gr_1", WrittenBy: "core-execution",
		Actor: "core-execution", EventType: "GRANT_SPENT"})
	if !errors.Is(err, ErrActorsTable) || !strings.Contains(err.Error(), "simulated bad table") {
		t.Errorf("validate must refuse with ErrActorsTable and the cause, got %v", err)
	}
	if len(Emitters()) != 0 || len(ContractEmitters()) != 0 || len(PrincipalKinds()) != 0 {
		t.Error("the accessors must list nothing when no table loaded")
	}
}
