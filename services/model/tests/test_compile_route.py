"""UND-05 / UND-04 (model half), REQ-INT-001, REQ-INT-002, REQ-INT-005, REQ-API-002; INV-02, INV-12, AGENT-09: the compile route `POST /internal/v1/understanding:compile`
that the core's client calls (services/core/understanding/client.go).

The route takes `{canonical_input, pack_ref, open_intent?}` and answers `{intent_object, canonical_input}` (200) or an errors.v2 envelope whose code the core reads:
`INPUT_INVALID` for a request it refuses, `PROVIDER_UNAVAILABLE` when no provider gave a usable reply. The intent object is an `intent-object.v3` proposal (confidence as integer
basis points, ruling A173) that has crossed the model service's side of the schema gate; the route never repairs, signs or hashes it (A166). All data is SYNTHETIC.

Written before the route: against `main` the first test fails on the status (the route does not exist) and the others on the missing `compiler` seam of `create_app`.
"""

import json
from pathlib import Path
from typing import Any

import pytest
from fastapi.testclient import TestClient

from noema_model.contracts.errors_v2 import ErrorsV2ErrorCode, ErrorsV2ErrorEnvelope
from noema_model.contracts.intent_object_v3 import IntentObjectV3
from noema_model.main import create_app

ROUTE = "/internal/v1/understanding:compile"
FIXTURES = Path(__file__).resolve().parents[3] / "contracts" / "fixtures" / "valid" / "canonical-input.v2"
CANONICAL = json.loads((FIXTURES / "text_request.json").read_text())
PACK_REF = "fintech@1.0.0"
NODE = "fin.payments.exception.investigate"


def proposal() -> dict[str, Any]:
    """What a compiler proposes: status EXTRACTED, no sig, identity as the canonical input names it, confidence in basis points (A173)."""
    return {
        "schema_version": "3.0", "intent_id": "int_01J9X3", "version": 1, "supersedes": None, "tenant_id": CANONICAL["tenant_id"],
        "correlation_id": CANONICAL["correlation_id"], "created_at": "2026-11-13T10:00:00Z",
        "principal": {"principal_id": "usr_88213", "authn_ref": CANONICAL["session_id"]},
        "source": {"mode": "MODE_1", "channel": "text", "locale_detected": ["en-NG"], "asr_confidence": None, "raw_ref": "inp_abc123"},
        "pack": {"pack_id": "fintech", "pack_version": "1.0.0"}, "intent_node": NODE,
        "slots": {"amount": {"type": "Quantity", "value": 2000000, "unit": "NGN", "surface_form": "20k", "confidence": 9300}},
        "ambiguity_flags": [], "desired_outcome_refs": [], "status": "EXTRACTED",
    }


class Compiler:
    """The seam the route calls: it returns the proposal's JSON bytes, or raises ProviderUnavailable. A test double of the route's collaborator, not of a failure mode."""

    def __init__(self, reply: bytes | None = None, error: Exception | None = None) -> None:
        self.reply, self.error, self.calls = reply, error, []

    async def compile(self, canonical_input: dict[str, Any], pack_ref: str, open_intent: dict[str, Any] | None) -> bytes:
        self.calls.append((canonical_input, pack_ref, open_intent))
        if self.error is not None:
            raise self.error
        assert self.reply is not None
        return self.reply


def client(compiler: Compiler | None = None) -> TestClient:
    # With no compiler the app is built exactly as main builds it today, so the first test fails on the status, not on a missing argument.
    app = create_app(check=lambda: (True, "ok")) if compiler is None else create_app(check=lambda: (True, "ok"), compiler=compiler)
    return TestClient(app, raise_server_exceptions=False)


def post(c: TestClient, body: Any) -> Any:
    return c.post(ROUTE, content=json.dumps(body), headers={"content-type": "application/json"})


# REQ-API-002: a route the core calls must exist; with no compiler wired it must say it cannot serve, never 404 (a 404 reads as a client error the core calls MALFORMED).
def test_the_compile_route_exists_and_without_a_compiler_it_refuses_to_serve() -> None:
    r = post(client(), {"canonical_input": CANONICAL, "pack_ref": PACK_REF})
    assert r.status_code == 503, "no compiler is wired: the route must answer 503, not 404"
    ErrorsV2ErrorEnvelope.model_validate(r.json())


def test_a_correct_request_gets_the_intent_object_and_the_canonical_input_and_nothing_else() -> None:
    c = Compiler(reply=json.dumps(proposal()).encode())
    r = post(client(c), {"canonical_input": CANONICAL, "pack_ref": PACK_REF})
    assert r.status_code == 200
    body = r.json()
    assert set(body) == {"intent_object", "canonical_input"}, "exactly the two members the core's decoder accepts"
    assert body["canonical_input"] == CANONICAL
    obj = IntentObjectV3.from_json(json.dumps(body["intent_object"]))
    assert obj.schema_version.startswith("3."), "an intent-object.v3 proposal (A173)"
    assert c.calls == [(CANONICAL, PACK_REF, None)]


@pytest.mark.parametrize(
    "body",
    [
        {"pack_ref": PACK_REF},                                              # no canonical input
        {"canonical_input": CANONICAL},                                      # no pack ref
        {"canonical_input": CANONICAL, "pack_ref": "fintech"},               # not <vertical>@<x.y.z>
        {"canonical_input": CANONICAL, "pack_ref": PACK_REF, "extra": 1},    # an unknown member
        {"canonical_input": {**CANONICAL, "schema_version": "1.0"}, "pack_ref": PACK_REF},  # not canonical-input.v2
        {"canonical_input": {k: v for k, v in CANONICAL.items() if k != "tenant_id"}, "pack_ref": PACK_REF},  # a required member missing
        [],                                                                  # not an object
    ],
)
def test_a_request_that_is_not_the_contracts_is_refused_as_input_invalid_and_no_compiler_is_called(body: Any) -> None:
    c = Compiler(reply=json.dumps(proposal()).encode())
    r = post(client(c), body)
    assert r.status_code in (400, 422)
    assert ErrorsV2ErrorEnvelope.model_validate(r.json()).error.code is ErrorsV2ErrorCode.INPUT_INVALID
    assert c.calls == []
    assert "SYNTHETIC" not in r.text, "an error never quotes the input (AGENT-13)"


def test_a_reply_that_fails_the_schema_gate_is_never_repaired_and_reads_as_a_provider_failure() -> None:
    bad = proposal()
    bad["slots"]["amount"]["confidence"] = 0.93  # a fractional confidence is a shape error, never rounded (A173)
    r = post(client(Compiler(reply=json.dumps(bad).encode())), {"canonical_input": CANONICAL, "pack_ref": PACK_REF})
    assert r.status_code == 502
    assert ErrorsV2ErrorEnvelope.model_validate(r.json()).error.code is ErrorsV2ErrorCode.PROVIDER_UNAVAILABLE
    assert "0.93" not in r.text and "9300" not in r.text


def test_a_provider_failure_is_provider_unavailable_and_retryable() -> None:
    from noema_model.understanding.compiler import ProviderUnavailable

    r = post(client(Compiler(error=ProviderUnavailable())), {"canonical_input": CANONICAL, "pack_ref": PACK_REF})
    env = ErrorsV2ErrorEnvelope.model_validate(r.json())
    assert r.status_code == 502 and env.error.code is ErrorsV2ErrorCode.PROVIDER_UNAVAILABLE and env.error.retryable is True


def test_a_bug_in_the_compiler_is_not_hidden_as_a_provider_failure() -> None:
    r = post(client(Compiler(error=RuntimeError("boom"))), {"canonical_input": CANONICAL, "pack_ref": PACK_REF})
    assert r.status_code == 500
    assert ErrorsV2ErrorEnvelope.model_validate(r.json()).error.code is ErrorsV2ErrorCode.INTERNAL_SAFETY_BLOCK
    assert "boom" not in r.text
