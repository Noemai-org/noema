package contracts_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// CON-03, REQ-API-001, REQ-TST-003: the consumer-driven contract between the model service and the core, on the candidate-read
// API (REQ-CTX-006). Two recordings in tools/codegen/consumer/ hold the evidence:
//
//	model-candidates-request.json   written by the model service's real client (services/model/tests/test_contracts_consumer.py)
//	core-candidates-response.json   written here, from the core's generated types
//
// The Go side decodes the first with its generated type; the Python side parses the second with the real client. A change to
// either side that the other cannot read fails the other's test.

var update = flag.Bool("update", false, "rewrite tools/codegen/consumer/core-candidates-response.json")

var recordings = filepath.Join("..", "..", "..", "..", "tools", "codegen", "consumer")

const (
	tenant      = "ten_synth01"
	correlation = "corr_consumer01"
)

func TestTheRequestTheModelServiceSendsDecodesIntoTheGeneratedType(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(recordings, "model-candidates-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var req contracts.CandidatesRequestV1
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("the core cannot read the request the model service sends: %v", err)
	}
	if req.TenantId != tenant || req.CorrelationId != correlation || req.PrincipalRef != "usr_synth014" || req.ResourceKind != "test.transaction" {
		t.Fatalf("decoded the wrong request: %+v", req)
	}
	out, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(generic(t, raw), generic(t, out)) {
		t.Fatalf("decoding and encoding the request changed it:\n in:  %s\n out: %s", compact(raw), out)
	}
	// The same request with a member the contract does not have is refused (the model service must never send one).
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["principal_name"] = json.RawMessage(`"x"`)
	extra, _ := json.Marshal(m)
	if err := json.Unmarshal(extra, new(contracts.CandidatesRequestV1)); err == nil {
		t.Fatal("a request with an unknown member was accepted")
	}
}

func candidate(id, ref string, amount int64, at, display string, dir contracts.CandidatesResponseV1CandidateDirection) contracts.CandidatesResponseV1Candidate {
	return contracts.CandidatesResponseV1Candidate{
		CandidateId: id, ResourceRef: contracts.CommonV1ResourceRef(ref), AmountMinor: contracts.MoneyV1MinorAmount(amount), Currency: "NGN",
		OccurredAt: contracts.CommonV1Timestamp(at), CounterpartyDisplay: display, AccountLast4: "6789", Direction: dir,
	}
}

// The core builds its answer from the generated types, newest first as the contract requires.
func coreResponse() contracts.CandidatesResponseV1 {
	yes := true
	newest := candidate("cand_synth02", "test.transaction:TX0002", 200000, "2026-11-02T10:15:00Z", "ACME SUPPLIES", contracts.CandidatesResponseV1CandidateDirectionOUTGOING)
	newest.BeneficiaryAliasMatch = &yes
	return contracts.CandidatesResponseV1{
		SchemaVersion: "1.0", TenantId: tenant, CorrelationId: correlation,
		Candidates: []contracts.CandidatesResponseV1Candidate{
			newest,
			candidate("cand_synth01", "test.transaction:TX0001", 150000, "2026-11-01T09:00:00Z", "ACME SUPPLIES LTD", contracts.CandidatesResponseV1CandidateDirectionINCOMING),
		},
	}
}

func TestTheResponseTheCoreSendsIsTheRecordedOneTheModelServiceReads(t *testing.T) {
	raw, err := json.Marshal(coreResponse())
	if err != nil {
		t.Fatal(err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, mustSorted(t, raw), "", " "); err != nil {
		t.Fatal(err)
	}
	indented.WriteByte('\n')
	path := filepath.Join(recordings, "core-candidates-response.json")
	if *update {
		if err := os.WriteFile(path, indented.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	recorded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the recording (run with -update to create it): %v", err)
	}
	if !reflect.DeepEqual(generic(t, recorded), generic(t, raw)) {
		t.Fatalf("the core's response is no longer the recording the model service's test reads; run with -update and review the diff:\n recorded: %s\n now:      %s", compact(recorded), raw)
	}
	// And it is valid under the generated type, with a candidate list the client can bound.
	if err := json.Unmarshal(recorded, new(contracts.CandidatesResponseV1)); err != nil {
		t.Fatal(err)
	}
}

// mustSorted returns the document with object members in sorted order, so the recording does not depend on struct field order.
func mustSorted(t *testing.T, raw []byte) []byte {
	t.Helper()
	out, err := json.Marshal(generic(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	return out
}
