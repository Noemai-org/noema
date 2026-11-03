"""CON-03, REQ-API-001, REQ-TST-003: the consumer-driven contract between the model service and the core on the candidate-read
API (REQ-CTX-006). Two recordings in tools/codegen/consumer/ hold the evidence:

  model-candidates-request.json   written here, by the real client (noema_model.evidence.client.build_request)
  core-candidates-response.json   written by the core's Go test (internal/contracts/consumer_test.go in the Go module)

The Go side decodes the first with its generated type; this side parses the second with the real client. A change to either side
that the other cannot read fails the other's test. Rewrite the recording with NOEMA_UPDATE_RECORDINGS=1 and review the diff.
"""
import json
import os
from datetime import UTC, datetime
from pathlib import Path

from noema_model.contracts.candidates_request_v1 import CandidatesRequestV1
from noema_model.contracts.candidates_response_v1 import CandidatesResponseV1
from noema_model.evidence import build_request, parse_response
from noema_model.understanding.pipeline import CandidateQuery

RECORDINGS = Path(__file__).resolve().parents[3] / "tools" / "codegen" / "consumer"
TENANT, CORRELATION = "ten_synth01", "corr_consumer01"


def the_query() -> CandidateQuery:
    return CandidateQuery(
        correlation_id=CORRELATION, principal_ref="usr_synth014", tenant_id=TENANT, resource_kind="test.transaction",
        amount_range=(100000, 250000), currency="NGN",
        time_window=(datetime(2026, 10, 25, tzinfo=UTC), datetime(2026, 11, 2, 12, 0, tzinfo=UTC)), counterparty_surface="ACME",
    )


def test_the_request_the_client_sends_is_valid_for_the_generated_model_and_is_the_recorded_one() -> None:
    sent = build_request(the_query())
    text = json.dumps(sent, sort_keys=True, indent=1) + "\n"
    path = RECORDINGS / "model-candidates-request.json"
    if os.environ.get("NOEMA_UPDATE_RECORDINGS") == "1":
        path.write_text(text)
    assert json.loads(path.read_text()) == sent, "the client now sends something else; rerun with NOEMA_UPDATE_RECORDINGS=1 and review the diff"
    model = CandidatesRequestV1.from_json(json.dumps(sent))
    assert model.to_json_obj() == sent


def test_the_response_the_core_sends_is_read_by_the_client_and_valid_for_the_generated_model() -> None:
    body = (RECORDINGS / "core-candidates-response.json").read_bytes()
    candidates = parse_response(body, tenant_id=TENANT, correlation_id=CORRELATION)
    assert [c.candidate_id for c in candidates] == ["cand_synth02", "cand_synth01"]  # newest first, as the contract requires
    assert candidates[0].beneficiary_alias_match is True and candidates[1].beneficiary_alias_match is None
    assert CandidatesResponseV1.from_json(body).to_json_obj() == json.loads(body)
