package predicate_test

import (
	"testing"

	ip "github.com/Noemai-org/noema/services/core/internal/predicate"
	op "github.com/Noemai-org/noema/services/core/outcome/predicate"
)

// OUT-02, REQ-VER-002: every outcome definition of the fintech pack does not only type-check, it evaluates: true when the target holds the
// postcondition, false when it holds something else, and the order of the rows never matters. All data is SYNTHETIC.

type evalCase struct {
	name string
	env  ip.Env
	want bool
}

func row(kv ...any) ip.Row {
	r := ip.Row{}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1].(ip.Value)
	}
	return r
}

func TestEveryFintechOutcomeEvaluatesOnTheDataItNames(t *testing.T) {
	pack, err := op.LoadPack(fintech(t))
	if err != nil {
		t.Fatal(err)
	}
	subject, intent := ip.Ref("tx_1"), ip.Ref("int_1")
	caseRow := func(id, status, queue, tx, by string) ip.Row {
		return row("id", ip.Ref(id), "status", ip.Enum(status), "owner_queue", ip.Ref(queue), "subject", ip.Ref(tx), "created_by_intent", ip.Ref(by), "epoch", ip.Int(1))
	}
	tables := map[string][]evalCase{
		// case exists by key, status set, count equals
		"fin.outcome.exception.case_opened": {
			{"one open case made by this intent", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c1", "OPEN", "Q", "tx_1", "int_1")}},
				Variables: map[string]ip.Value{"subject_resource": subject, "intent_id": intent}}, true},
			{"a routed case also counts as open", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c1", "ROUTED", "Q", "tx_1", "int_1")}},
				Variables: map[string]ip.Value{"subject_resource": subject, "intent_id": intent}}, true},
			{"no case at all", ip.Env{Entities: map[string][]ip.Row{"case": {}}, Variables: map[string]ip.Value{"subject_resource": subject, "intent_id": intent}}, false},
			{"a closed case does not count", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c1", "CLOSED", "Q", "tx_1", "int_1")}},
				Variables: map[string]ip.Value{"subject_resource": subject, "intent_id": intent}}, false},
			{"two cases by this intent are a duplicate", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c1", "OPEN", "Q", "tx_1", "int_1"), caseRow("c2", "OPEN", "Q", "tx_1", "int_1")}},
				Variables: map[string]ip.Value{"subject_resource": subject, "intent_id": intent}}, false},
			{"a case of another transaction", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c1", "OPEN", "Q", "tx_2", "int_1")}},
				Variables: map[string]ip.Value{"subject_resource": subject, "intent_id": intent}}, false},
		},
		// owner queue equals
		"fin.outcome.exception.case_routed": {
			{"the case is in the planned queue", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c1", "ROUTED", "DISPUTES", "tx_1", "int_1")}},
				Variables: map[string]ip.Value{"case_id": ip.Ref("c1"), "planned_queue": ip.Ref("DISPUTES")}}, true},
			{"the case is in another queue", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c1", "ROUTED", "FRAUD", "tx_1", "int_1")}},
				Variables: map[string]ip.Value{"case_id": ip.Ref("c1"), "planned_queue": ip.Ref("DISPUTES")}}, false},
			{"another case is in the planned queue", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c2", "ROUTED", "DISPUTES", "tx_1", "int_1")}},
				Variables: map[string]ip.Value{"case_id": ip.Ref("c1"), "planned_queue": ip.Ref("DISPUTES")}}, false},
		},
		// attachment hash equals
		"fin.outcome.exception.evidence_recorded": {
			{"the attachment carries the planned hash", ip.Env{Entities: map[string][]ip.Row{"attachment": {row("case", ip.Ref("c1"), "payload_hash", ip.Str("sha256:aa"))}},
				Variables: map[string]ip.Value{"case_id": ip.Ref("c1"), "planned_payload_hash": ip.Str("sha256:aa")}}, true},
			{"the attachment carries another hash", ip.Env{Entities: map[string][]ip.Row{"attachment": {row("case", ip.Ref("c1"), "payload_hash", ip.Str("sha256:bb"))}},
				Variables: map[string]ip.Value{"case_id": ip.Ref("c1"), "planned_payload_hash": ip.Str("sha256:aa")}}, false},
			{"no attachment", ip.Env{Entities: map[string][]ip.Row{"attachment": {}}, Variables: map[string]ip.Value{"case_id": ip.Ref("c1"), "planned_payload_hash": ip.Str("sha256:aa")}}, false},
		},
		// count equals
		"fin.outcome.exception.complaint_logged": {
			{"exactly one complaint reference in this epoch", ip.Env{Entities: map[string][]ip.Row{"complaint_ref": {row("subject", subject, "epoch", ip.Int(3))}},
				Variables: map[string]ip.Value{"subject_resource": subject, "logical_resolution_epoch": ip.Int(3)}}, true},
			{"none", ip.Env{Entities: map[string][]ip.Row{"complaint_ref": {}}, Variables: map[string]ip.Value{"subject_resource": subject, "logical_resolution_epoch": ip.Int(3)}}, false},
			{"two in the epoch", ip.Env{Entities: map[string][]ip.Row{"complaint_ref": {row("subject", subject, "epoch", ip.Int(3)), row("subject", subject, "epoch", ip.Int(3))}},
				Variables: map[string]ip.Value{"subject_resource": subject, "logical_resolution_epoch": ip.Int(3)}}, false},
			{"one, but in another epoch", ip.Env{Entities: map[string][]ip.Row{"complaint_ref": {row("subject", subject, "epoch", ip.Int(2))}},
				Variables: map[string]ip.Value{"subject_resource": subject, "logical_resolution_epoch": ip.Int(3)}}, false},
		},
		// status set
		"fin.outcome.exception.status_explained": {
			{"re-derived equals used and is a confirmed class", ip.Env{Variables: map[string]ip.Value{"classification_rederived": ip.Enum("SUCCESS_CONFIRMED"), "classification_used": ip.Enum("SUCCESS_CONFIRMED")}}, true},
			{"re-derived differs from the one used", ip.Env{Variables: map[string]ip.Value{"classification_rederived": ip.Enum("SUCCESS_CONFIRMED"), "classification_used": ip.Enum("REVERSED_CONFIRMED")}}, false},
			{"both equal but not a confirmed class", ip.Env{Variables: map[string]ip.Value{"classification_rederived": ip.Enum("SETTLEMENT_PENDING"), "classification_used": ip.Enum("SETTLEMENT_PENDING")}}, false},
		},
	}
	if len(tables) != len(pack.Definitions) {
		t.Fatalf("%d outcomes have cases, the pack defines %d: a new outcome needs evaluation cases", len(tables), len(pack.Definitions))
	}
	for id, cases := range tables {
		def, ok := pack.Definitions[id]
		if !ok {
			t.Fatalf("%s is not in the pack", id)
		}
		prog, err := op.Compile(def.SuccessPredicate, pack.Scope)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		for _, c := range cases {
			got, err := prog.EvalBool(c.env)
			if err != nil || got != c.want {
				t.Errorf("%s / %s: got %v (err %v), want %v", id, c.name, got, err, c.want)
			}
		}
	}
}

// REQ-VER-002: the order of the rows never changes the result. (An unbound variable is UNKNOWN in the verifier, which checks the binding
// before it evaluates; equality with a null in the language itself is false, so that rule is not asserted here.)
func TestRowOrderDoesNotChangeAnOutcomesResult(t *testing.T) {
	pack, err := op.LoadPack(fintech(t))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := op.Compile(pack.Definitions["fin.outcome.exception.case_routed"].SuccessPredicate, pack.Scope)
	if err != nil {
		t.Fatal(err)
	}
	r := func(id, queue string) ip.Row {
		return row("id", ip.Ref(id), "status", ip.Enum("ROUTED"), "owner_queue", ip.Ref(queue), "subject", ip.Ref("tx_1"), "created_by_intent", ip.Ref("int_1"), "epoch", ip.Int(1))
	}
	vars := map[string]ip.Value{"case_id": ip.Ref("c1"), "planned_queue": ip.Ref("DISPUTES")}
	a, errA := prog.EvalBool(ip.Env{Entities: map[string][]ip.Row{"case": {r("c1", "DISPUTES"), r("c2", "FRAUD")}}, Variables: vars})
	b, errB := prog.EvalBool(ip.Env{Entities: map[string][]ip.Row{"case": {r("c2", "FRAUD"), r("c1", "DISPUTES")}}, Variables: vars})
	if errA != nil || errB != nil || !a || a != b {
		t.Fatalf("row order changed the result: %v %v (%v, %v)", a, b, errA, errB)
	}
}
