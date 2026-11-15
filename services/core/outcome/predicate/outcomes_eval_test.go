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
			// EL review at 8b37cc1, finding 1: the created_by_intent clause. Another intent's case on the same transaction is not this intent's duplicate,
			// and is not this intent's case either.
			{"this intent's case and another intent's on the same transaction", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c1", "OPEN", "Q", "tx_1", "int_1"), caseRow("c2", "OPEN", "Q", "tx_1", "int_2")}},
				Variables: map[string]ip.Value{"subject_resource": subject, "intent_id": intent}}, true},
			// The exists clause names the transaction: this intent's case there is closed, and another transaction's case is open.
			{"this transaction's case is closed and another transaction's is open", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c1", "CLOSED", "Q", "tx_1", "int_1"), caseRow("c2", "OPEN", "Q", "tx_2", "int_1")}},
				Variables: map[string]ip.Value{"subject_resource": subject, "intent_id": intent}}, false},
			{"only another intent's case on the transaction", ip.Env{Entities: map[string][]ip.Row{"case": {caseRow("c2", "OPEN", "Q", "tx_1", "int_2")}},
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
			// EL review finding 3: the attachment.case clause. The planned hash on another case's attachment is not this case's evidence.
			{"the planned hash is on another case's attachment", ip.Env{Entities: map[string][]ip.Row{"attachment": {row("case", ip.Ref("c2"), "payload_hash", ip.Str("sha256:aa"))}},
				Variables: map[string]ip.Value{"case_id": ip.Ref("c1"), "planned_payload_hash": ip.Str("sha256:aa")}}, false},
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
			// EL review finding 2: the subject clause. A complaint reference for another subject in the right epoch is not this subject's.
			{"one in the right epoch, for another subject", ip.Env{Entities: map[string][]ip.Row{"complaint_ref": {row("subject", ip.Ref("tx_2"), "epoch", ip.Int(3))}},
				Variables: map[string]ip.Value{"subject_resource": subject, "logical_resolution_epoch": ip.Int(3)}}, false},
			{"this subject's one and another subject's in the same epoch", ip.Env{Entities: map[string][]ip.Row{"complaint_ref": {row("subject", subject, "epoch", ip.Int(3)), row("subject", ip.Ref("tx_2"), "epoch", ip.Int(3))}},
				Variables: map[string]ip.Value{"subject_resource": subject, "logical_resolution_epoch": ip.Int(3)}}, true},
		},
		// status set
		"fin.outcome.exception.status_explained": {
			{"re-derived equals used and is a confirmed class", ip.Env{Variables: map[string]ip.Value{"classification_rederived": ip.Enum("SUCCESS_CONFIRMED"), "classification_used": ip.Enum("SUCCESS_CONFIRMED")}}, true},
			{"re-derived differs from the one used", ip.Env{Variables: map[string]ip.Value{"classification_rederived": ip.Enum("SUCCESS_CONFIRMED"), "classification_used": ip.Enum("REVERSED_CONFIRMED")}}, false},
			{"REVERSED_CONFIRMED on both sides is a confirmed class", ip.Env{Variables: map[string]ip.Value{"classification_rederived": ip.Enum("REVERSED_CONFIRMED"), "classification_used": ip.Enum("REVERSED_CONFIRMED")}}, true},
			{"REJECTED_CONFIRMED on both sides is a confirmed class", ip.Env{Variables: map[string]ip.Value{"classification_rederived": ip.Enum("REJECTED_CONFIRMED"), "classification_used": ip.Enum("REJECTED_CONFIRMED")}}, true},
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

// REQ-VER-002: the order of the rows never changes the result, for a predicate that counts and filters over several rows (case_opened), in both orders
// and with the deciding row first and last. (An unbound variable is UNKNOWN in the verifier, which checks the binding before it evaluates; equality
// with a null in the language itself is false, so that rule is not asserted here.)
func TestRowOrderDoesNotChangeAnOutcomesResult(t *testing.T) {
	pack, err := op.LoadPack(fintech(t))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := op.Compile(pack.Definitions["fin.outcome.exception.case_opened"].SuccessPredicate, pack.Scope)
	if err != nil {
		t.Fatal(err)
	}
	r := func(id, status, intent string) ip.Row {
		return row("id", ip.Ref(id), "status", ip.Enum(status), "owner_queue", ip.Ref("Q"), "subject", ip.Ref("tx_1"), "created_by_intent", ip.Ref(intent), "epoch", ip.Int(1))
	}
	vars := map[string]ip.Value{"subject_resource": ip.Ref("tx_1"), "intent_id": ip.Ref("int_1")}
	for name, rows := range map[string][]ip.Row{
		"mine and a closed one of another intent": {r("c1", "OPEN", "int_1"), r("c2", "CLOSED", "int_2")},
		"mine and an open one of another intent":  {r("c1", "OPEN", "int_1"), r("c2", "OPEN", "int_2")},
		"two of mine":                             {r("c1", "OPEN", "int_1"), r("c2", "ROUTED", "int_1")},
	} {
		fwd, errF := prog.EvalBool(ip.Env{Entities: map[string][]ip.Row{"case": rows}, Variables: vars})
		rev, errR := prog.EvalBool(ip.Env{Entities: map[string][]ip.Row{"case": {rows[1], rows[0]}}, Variables: vars})
		if errF != nil || errR != nil || fwd != rev {
			t.Errorf("%s: row order changed the result: %v %v (%v, %v)", name, fwd, rev, errF, errR)
		}
	}
}
