"""Support for the proposer swap test (status/ENV-08.md). Everything here is SYNTHETIC (AGENT-25) and runs without a network.

Two proposers answer the same canned decision through the same harness and the same pipeline:

  A: a canned-proposal stub. It returns the canned provider output for each request, in the forced schema.
  B: the OpenAI Responses adapter (`OpenAIResponses`) over a replay transport. The transport plays back the same canned output
     inside a `/v1/responses` document. No socket is opened.

What comes out of each is an Intent Object, assembled here from the pipeline's result by the shape of SoT §43.1 and PERD §20.1
and checked against `contracts/schemas/intent-object.v1.json`. The assembly is a stand-in for the compiler's assembly and the
schema gate, which are not built (the pipeline's own header says so); it is not that code, and it validates the decision path
only, not the unbuilt intake or the architecture as a whole.

The Intent Object has no field for who proposed it. What differs between A and B (provider, model version, tokens) is kept
outside it, in the envelope's `proposer` block, so the Go side can show that none of it reaches a decision or the Record.
"""

import asyncio
import base64
import hashlib
import hmac
import json
import socket
import sys
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from noema_model.harness import Harness
from noema_model.harness.config import load_harness_config
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.llm.base import AdapterSettings, FinishReason, HttpResponse, ProviderRequest, ProviderResponse
from noema_model.providers.llm.openai_responses import OpenAIResponses
from noema_model.understanding.pipeline import Outcome, PipelineResult, run_pipeline
from noema_model.understanding.resolution import Candidate, ResolutionState
from tests.support import ManualClock, raw_config
from tests.support_pipeline import PACK, T0, FakeSource, make_input, no_errors, normaliser, pipeline_config

ROOT = Path(__file__).resolve().parents[3]
CANNED = Path(__file__).parent / "fixtures" / "proposer_swap" / "canned_outputs.json"
GOLDEN = ROOT / "synthetic" / "proposer_swap" / "proposals"
CONTRACTS = ROOT / "contracts" / "schemas"

# The synthetic model-service key. PERD §20.1: the signature is an HMAC over the canonical JSON using the model-service key
# [REC] (SoT-C-07). The Go side signs and checks with the same value; it is not a secret.
SIGNING_KEY = b"synthetic-swap-test-key-not-a-secret"
KEY_ID = "und-swap-2026"

# Session values: the principal and tenant come from the authenticated session, never from text (INV-12). They are the
# values the first end-to-end case (the core's end-to-end test) already uses.
TENANT = "ten_aa"
PRINCIPAL = "usr_100"
SESSION = "ses_swap01"
CORRELATION = "corr_g2case1"
CANDIDATE_SET = "cns_swap01"
INTENT_ID = "int_swap01"
CREATED_AT = "2026-10-27T09:00:00Z"  # a simulated clock reading (PERD §20.1: simulated time in benchmarks)
RAW_REF = "raw_swap01"

SCENARIOS = ("escalate", "prohibited", "invalid")
PROPOSERS = ("A", "B")


@dataclass(frozen=True, slots=True)
class Scenario:
    name: str
    text: str
    transaction: Candidate | None
    desired_outcomes: tuple[str, ...]


TX1 = Candidate("TX1", "fin.transaction:TX1", 2_000_000, "NGN", T0, "Tunde Adeyemi", "0001", "OUTGOING", True)
DEFINITIONS: Mapping[str, Scenario] = {
    # The customer asks for the case to be escalated. PERD §20.1: desired_outcome_refs is a subset of the node's
    # outcome_definitions; the case is opened and routed (the pack's fin.outcome.exception.case_opened and case_routed).
    "escalate": Scenario("escalate", "I sent 20k to Tunde this morning and he has not got it. Please open a case and escalate it.", TX1,
                         ("fin.outcome.exception.case_opened", "fin.outcome.exception.case_routed")),
    # The customer asks for a refund: a remedy the pack marks prohibited (packs/fintech_v1.json, fin.cap.initiate_refund).
    "prohibited": Scenario("prohibited", "I sent 20k to Tunde this morning and he has not got it. Refund me now.", TX1,
                           ("fin.outcome.exception.case_opened",)),
    # The model's answer to the first question is not a valid answer: the harness refuses it, so there is no proposal.
    "invalid": Scenario("invalid", "I sent 20k to Tunde this morning and he has not got it.", TX1, ()),
}

# The canned decision per scenario is the content of canned_outputs.json. A request is matched to its canned output by the
# schema name and the question ids it carries, so the output never depends on which proposer asked.


def canned_key(schema_name: str, user_text: str) -> str:
    if schema_name != "harness_decide":
        return schema_name
    body = user_text.split("QUESTIONS:\n", 1)[1].split("\n\n<<<DATA", 1)[0]
    return ",".join(str(q["question_id"]) for q in json.loads(body))


def load_canned() -> dict[str, dict[str, Any]]:
    data = json.loads(CANNED.read_text(encoding="utf-8"))
    return {name: dict(table) for name, table in data["scenarios"].items()}


class CannedStub:
    """Proposer A. It returns the canned output for the request in the forced schema, reads nothing from the network, and
    keeps no state but a record of what it served."""

    def __init__(self, name: str, table: Mapping[str, Any], version: str) -> None:
        self.name, self._table, self._version = name, table, version
        self.served: list[str] = []

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        key = canned_key(request.schema_name, request.user)
        self.served.append(key)
        return ProviderResponse(json.dumps(self._table[key]), self._version, 50, 10, FinishReason.COMPLETE)


class ReplayTransport:
    """The transport under proposer B. It plays back a canned `/v1/responses` document for each request and opens no
    socket: the document is built from the same canned output, the way the endpoint reference shapes it (a reasoning item
    that the adapter drops, then one message with one output_text)."""

    def __init__(self, table: Mapping[str, Any], version: str) -> None:
        self._table, self._version = table, version
        self.served: list[str] = []
        self.requests: list[tuple[str, dict[str, Any]]] = []

    async def post(self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int) -> HttpResponse:
        sent = json.loads(body)
        key = canned_key(str(sent["text"]["format"]["name"]), str(sent["input"]))
        self.served.append(key)
        self.requests.append((url, sent))
        document = {
            "id": "resp_synthetic_swap", "object": "response", "status": "completed", "incomplete_details": None, "model": self._version,
            "output": [
                {"id": "rs_synthetic_swap", "type": "reasoning", "summary": []},
                {"id": "msg_synthetic_swap", "type": "message", "status": "completed", "role": "assistant",
                 "content": [{"type": "output_text", "text": json.dumps(self._table[key]), "annotations": []}]},
            ],
            "usage": {"input_tokens": 120, "output_tokens": 340, "output_tokens_details": {"reasoning_tokens": 310}, "total_tokens": 460},
        }
        return HttpResponse(200, json.dumps(document).encode())


@dataclass(frozen=True, slots=True)
class Proposed:
    """What one proposer produced for one scenario, with the objects the tests inspect."""

    envelope: dict[str, Any]
    result: PipelineResult
    served: tuple[str, ...]
    requests: tuple[tuple[str, dict[str, Any]], ...]


def _harness(provider_name: str, kind: str, provider: Any) -> Harness:
    raw = raw_config(providers=(provider_name,))
    raw["providers"][provider_name]["kind"] = kind
    if kind == "openai_responses":
        raw["providers"][provider_name]["reasoning_effort"] = "medium"
    return Harness(load_harness_config(raw), {provider_name: provider}, ManualClock(), InMemoryTelemetry())


def propose(proposer: str, scenario: str, table: Mapping[str, Mapping[str, Any]] | None = None) -> Proposed:
    """Run the pipeline for one scenario under proposer "A" (stub) or "B" (OpenAI Responses adapter over replay)."""
    canned = (table or load_canned())[scenario]
    definition = DEFINITIONS[scenario]
    config = pipeline_config()
    if proposer == "A":
        name, version = "stub_a", "model-stub_a-2026-01-01"
        stub = CannedStub(name, canned, version)
        harness = _harness(name, "openai_chat", stub)
        served, requests, kind = stub.served, [], "canned_stub"
    else:
        name, version = "openai_b", "model-openai_b-2026-01-01"
        transport = ReplayTransport(canned, version)
        adapter = OpenAIResponses(name, AdapterSettings("https://vendor.example.invalid/", "CRED-VALUE", None, 1500, 65536, "medium"), transport)
        harness = _harness(name, "openai_responses", adapter)
        served, requests, kind = transport.served, transport.requests, "openai_responses_replay"
    rows = (definition.transaction,) if definition.transaction else ()
    inp = make_input(definition.text, correlation_id=CORRELATION, principal_ref=PRINCIPAL, tenant_id=TENANT,
                     candidate_set_id=CANDIDATE_SET)
    result = asyncio.run(run_pipeline(inp, config, harness, FakeSource(rows), normaliser, no_errors))
    intent = assemble(definition, result, config.locales)
    envelope = {
        "synthetic": True,
        "scenario": scenario,
        "proposer": {"id": proposer, "kind": kind, "calls": [
            {"route": c.route, "provider": c.provider, "model_version": c.model_version, "tokens_in": c.tokens_in, "tokens_out": c.tokens_out,
             "attempts": [{"provider": t.provider, "model_version": t.model_version, "outcome": t.outcome.value,
                           "tokens_in": t.tokens_in, "tokens_out": t.tokens_out} for t in c.attempts]}
            for c in result.calls]},
        "pipeline": {"outcome": result.outcome.value, "gate_errors": list(result.gate_errors)},
        "intent_object": intent,
    }
    return Proposed(envelope, result, tuple(served), tuple(requests))


def iso(moment: datetime) -> str:
    return moment.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def canonical(value: Any) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def sign(intent: dict[str, Any]) -> dict[str, str]:
    mac = hmac.new(SIGNING_KEY, canonical({k: v for k, v in intent.items() if k != "sig"}), hashlib.sha256).digest()
    return {"key_id": KEY_ID, "value": base64.b64encode(mac).decode()}


def assemble(definition: Scenario, result: PipelineResult, locales: Any) -> dict[str, Any] | None:
    """The Intent Object of SoT §43.1 / PERD §20.1 for a proposal, or None when the pipeline produced none. Each field is
    set as PERD §20.1 says who sets it; nothing is added that the contract does not list."""
    proposal, normalised = result.proposal, result.normalised
    if result.outcome is not Outcome.PROPOSED or proposal is None or normalised is None or result.resolution is None or proposal.intent_node is None:
        return None
    slots: dict[str, Any] = {}
    resolution = result.resolution
    top = resolution.ranked[0] if resolution.ranked else None
    if resolution.state is ResolutionState.AUTO_RESOLVED and top is not None:
        slots["subject_resource"] = {"type": "Resource", "status": "RESOLVED", "resolved_ref": top.candidate.resource_ref,
                                     "candidates_ref": resolution.candidate_set_id, "confidence": round(float(top.score), 4)}
    elif resolution.state in (ResolutionState.AMBIGUOUS, ResolutionState.TOO_MANY_CANDIDATES):
        slots["subject_resource"] = {"type": "Resource", "status": "AMBIGUOUS", "candidates_ref": resolution.candidate_set_id}
    else:
        slots["subject_resource"] = {"type": "Resource", "status": "UNRESOLVED", "candidates_ref": None}
    mentions = proposal.mentions
    if mentions["amount"].present and normalised.amount_minor is not None and normalised.currency is not None:
        slots["amount"] = {"type": "Quantity", "value": normalised.amount_minor, "unit": normalised.currency,
                           "confidence": mentions["amount"].confidence}
    if mentions["counterparty"].present:
        slots["counterparty"] = {"type": "Party", "surface_form": mentions["counterparty"].surface_form,
                                 "confidence": mentions["counterparty"].confidence}
    if mentions["when"].present and normalised.window is not None:
        slots["when"] = {"type": "TimeRef", "surface_form": mentions["when"].surface_form,
                         "window": [iso(normalised.window[0]), iso(normalised.window[1])], "confidence": mentions["when"].confidence}
    for name, value in proposal.reference_slots.items():
        if value is not None:
            slots[name] = {"type": "Reference", "value": value, "status": "RESOLVED"}
    code = {o.label: o.code for o in locales}.get(proposal.locale or "", proposal.locale)
    intent: dict[str, Any] = {
        "schema_version": "1.0", "intent_id": INTENT_ID, "version": 1, "supersedes": None,
        "tenant_id": TENANT, "correlation_id": CORRELATION, "created_at": CREATED_AT,
        "principal": {"principal_id": PRINCIPAL, "principal_type": "end_user", "authn_ref": SESSION, "asserted_by": "enterprise_idp"},
        "source": {"mode": "MODE_1", "channel": "text", "locale_detected": [code] if code else [], "asr_confidence": None, "raw_ref": RAW_REF},
        "pack": {"pack_id": PACK["pack_id"], "pack_version": PACK["pack_version"]},
        "intent_node": proposal.intent_node,
        "slots": slots,
        "ambiguity_flags": sorted(result.ambiguity_flags),
        "desired_outcome_refs": list(definition.desired_outcomes),
        "status": "VALIDATED",
    }
    intent["sig"] = sign(intent)
    return intent


def golden_name(scenario: str, proposer: str) -> str:
    return f"{scenario}.{proposer}.json"


def render(envelope: Mapping[str, Any]) -> str:
    return json.dumps(envelope, indent=2, sort_keys=True, ensure_ascii=False) + "\n"


def write_goldens() -> list[Path]:
    GOLDEN.mkdir(parents=True, exist_ok=True)
    written = []
    for scenario in SCENARIOS:
        for proposer in PROPOSERS:
            path = GOLDEN / golden_name(scenario, proposer)
            path.write_text(render(propose(proposer, scenario).envelope), encoding="utf-8")
            written.append(path)
    return written


def deny_sockets() -> Callable[[], None]:
    """Make any attempt to open a connection raise. Returns the function that restores sockets."""
    original = socket.socket.connect

    def refuse(self: socket.socket, address: Any) -> None:
        raise AssertionError(f"the swap test must not open a network connection (tried {address!r})")

    setattr(socket.socket, "connect", refuse)

    def restore() -> None:
        setattr(socket.socket, "connect", original)

    return restore


if __name__ == "__main__":
    if sys.argv[1:] == ["--write"]:
        for p in write_goldens():
            print(p.relative_to(ROOT))
    else:
        print("usage: python -m tests.support_swap --write", file=sys.stderr)
        sys.exit(2)
