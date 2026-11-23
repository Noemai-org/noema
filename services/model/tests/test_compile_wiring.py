"""UND-05, the wiring: the compile service is built from a mounted configuration file, only on a listener that requires a client certificate, and the process serves it
(REQ-INT-001, REQ-INT-002, REQ-API-005, REQ-SEC-002; INV-02, INV-12, INV-29; AGENT-16). All data is SYNTHETIC.

Written before `noema_model.understanding.compiler.wiring` exists: these fail with an import error until then. The Security Reviewer's condition on noema#544 and noema#555 is the first group:
no compile service is built on a plain listener. The last tests run a real request through the route, the service, the compiler, the pipeline (a scripted provider and a fake Evidence
transport are the only doubles) and the schema gate.
"""

import json
from pathlib import Path
from types import SimpleNamespace
from typing import Any

import pytest
from fastapi.testclient import TestClient

from noema_model.main import create_app
from noema_model.platform.config import ConfigError
from noema_model.understanding.gate import UNSIGNED
from noema_model.understanding.compiler.wiring import (
    _letters,
    COMPILE_CONFIG_ENV,
    DerivedMinter,
    PackFileResolver,
    build_compile_service,
    compile_requested,
    require_mtls_listener,
)
from noema_model.evidence.transport import HttpResponse
from tests.support_pipeline import G_A, GOOD_MENTIONS, ROOT, ScriptedModel, harness_for

PACK_FILE = ROOT / "packs" / "fintech_v1.json"
LEXICON_DIR = ROOT / "packs" / "fintech" / "lexicons"
MTLS = {"UVICORN_SSL_CERT_REQS": "2", "UVICORN_SSL_CERTFILE": "/run/tls/model.crt", "UVICORN_SSL_KEYFILE": "/run/tls/model.key", "UVICORN_SSL_CA_CERTS": "/run/tls/ca.crt"}
ROUTE = "/internal/v1/understanding:compile"
CANONICAL: dict[str, Any] = {
    "schema_version": "2.0", "request_id": "0b8f3c1e-52a4-4d7e-9a61-5f1d2b8c4a97", "correlation_id": "corr_test", "tenant_id": "ten_test", "session_id": "ses_test",
    "principal_ref": "usr_session", "channel": "text", "mode": "MODE_1", "text": "Wetin happen to the 20k wey I send Tunde this morning?",
    "received_at": "2026-10-21T08:00:00Z",
}


def config_doc(tmp_path: Path, **over: Any) -> dict[str, Any]:
    cert = tmp_path / "client.crt"
    for name in ("client.crt", "client.key", "ca.crt"):
        (tmp_path / name).write_text("synthetic", encoding="utf-8")
    doc: dict[str, Any] = {
        "pack_file": str(PACK_FILE), "lexicon_dir": str(LEXICON_DIR), "out_of_scope_node": "fin.meta.out_of_scope",
        "timeout_s": 30, "max_proposal_bytes": 65536, "max_request_bytes": 65536,
        "evidence": {"endpoint": "https://core:8080", "timeout_ms": 5000, "max_response_bytes": 65536},
        "evidence_tls": {"cert_file": str(cert), "key_file": str(tmp_path / "client.key"), "ca_file": str(tmp_path / "ca.crt")},
    }
    doc.update(over)
    return doc


def environ_for(tmp_path: Path, doc: dict[str, Any] | None = None, **env: str) -> dict[str, str]:
    path = tmp_path / "compile.json"
    path.write_text(json.dumps(config_doc(tmp_path) if doc is None else doc), encoding="utf-8")
    return {COMPILE_CONFIG_ENV: str(path), "NOEMA_MODEL_EVIDENCE_POOL_SIZE": "4", **MTLS, **env}


class FakeEvidence:
    """The core's candidate API as a transport: it answers the contract's shape for the request it was given (tenant and correlation echoed), with one transaction."""

    def __init__(self, status: int = 200, rows: int = 1) -> None:
        self.status, self.rows = status, rows
        self.requests: list[dict[str, Any]] = []

    async def post(self, url: str, headers: Any, body: bytes, timeout_s: float, max_bytes: int) -> HttpResponse:
        request = json.loads(body)
        self.requests.append({"url": url, **request})
        candidates = [{"candidate_id": "cand_001", "resource_ref": "fin.transaction:TX88001", "amount_minor": 2000000, "currency": "NGN", "occurred_at": "2026-10-21T08:00:00Z",
                       "counterparty_display": "T. A.", "account_last4": "4821", "direction": "OUTGOING"}][: self.rows]
        answer = {"schema_version": "1.0", "tenant_id": request["tenant_id"], "correlation_id": request["correlation_id"], "candidates": candidates}
        return HttpResponse(self.status, json.dumps(answer).encode())


def service_for(tmp_path: Path, model: ScriptedModel | None = None, evidence: FakeEvidence | None = None) -> tuple[Any, ScriptedModel, FakeEvidence]:
    model = model or ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS)
    evidence = evidence or FakeEvidence()
    harness, _ = harness_for(model)
    return build_compile_service(environ_for(tmp_path), harness, transport=evidence), model, evidence


# --- the Security Reviewer's condition: no compile service on a plain listener (noema#544, noema#555) ------------------------------------------------------

def test_nothing_is_built_when_the_configuration_is_not_named() -> None:
    assert compile_requested({}) is False
    assert create_app(environ={}).state.compile_service is None  # as before: the route answers 503


@pytest.mark.parametrize("bad", [{"UVICORN_SSL_CERT_REQS": "0"}, {"UVICORN_SSL_CERT_REQS": "1"}, {"UVICORN_SSL_CERT_REQS": ""}, {"UVICORN_SSL_CERT_REQS": "2 "},
                                 {"UVICORN_SSL_CERT_REQS": "CERT_REQUIRED"}, {"UVICORN_SSL_CERT_REQS": "02"}])
def test_a_listener_that_does_not_require_a_client_certificate_refuses_to_start(bad: dict[str, str]) -> None:
    with pytest.raises(ConfigError, match="mutual TLS"):
        require_mtls_listener({**MTLS, **bad})


@pytest.mark.parametrize("missing", ["UVICORN_SSL_CERT_REQS", "UVICORN_SSL_CERTFILE", "UVICORN_SSL_KEYFILE", "UVICORN_SSL_CA_CERTS"])
def test_a_listener_with_a_tls_setting_missing_or_empty_refuses_to_start(missing: str) -> None:
    gone = {k: v for k, v in MTLS.items() if k != missing}
    empty = {**MTLS, missing: ""}
    for env in (gone, empty):
        with pytest.raises(ConfigError, match="mutual TLS"):
            require_mtls_listener(env)


def test_the_full_set_of_listener_settings_is_accepted() -> None:
    require_mtls_listener(MTLS)


def test_a_plain_listener_never_gets_a_service_even_with_a_complete_configuration(tmp_path: Path) -> None:
    env = environ_for(tmp_path)
    for name in MTLS:
        env.pop(name)
    harness, _ = harness_for(ScriptedModel())
    with pytest.raises(ConfigError, match="mutual TLS"):
        build_compile_service(env, harness, transport=FakeEvidence())
    with pytest.raises(ConfigError, match="mutual TLS"):
        create_app(environ=env)


# --- the configuration is data with no default (AGENT-16) ----------------------------------------------------------------------------------------------

@pytest.mark.parametrize("missing", ["pack_file", "lexicon_dir", "out_of_scope_node", "timeout_s", "max_proposal_bytes", "max_request_bytes", "evidence", "evidence_tls"])
def test_a_missing_member_refuses_to_start(tmp_path: Path, missing: str) -> None:
    doc = config_doc(tmp_path)
    doc.pop(missing)
    harness, _ = harness_for(ScriptedModel())
    with pytest.raises(ConfigError):
        build_compile_service(environ_for(tmp_path, doc), harness, transport=FakeEvidence())


@pytest.mark.parametrize("change", [{"extra": 1}, {"timeout_s": 0}, {"timeout_s": -1}, {"timeout_s": True}, {"timeout_s": "30"}, {"max_proposal_bytes": 0}, {"max_request_bytes": 0},
                                    {"max_request_bytes": 1.5}, {"pack_file": "/no/such/pack.json"}, {"lexicon_dir": "/no/such/lexicons"}, {"lexicon_file": str(LEXICON_DIR / "fin.lex.en_ng.json")}, {"out_of_scope_node": "fin.nope"}])
def test_a_value_that_is_unknown_or_out_of_range_refuses_to_start(tmp_path: Path, change: dict[str, Any]) -> None:
    harness, _ = harness_for(ScriptedModel())
    with pytest.raises(ConfigError):
        build_compile_service(environ_for(tmp_path, config_doc(tmp_path, **change)), harness, transport=FakeEvidence())


def test_an_evidence_link_that_is_not_https_or_has_no_pool_refuses_to_start(tmp_path: Path) -> None:
    harness, _ = harness_for(ScriptedModel())
    plain = config_doc(tmp_path, evidence={"endpoint": "http://core:8080", "timeout_ms": 5000, "max_response_bytes": 65536})
    with pytest.raises(ConfigError):
        build_compile_service(environ_for(tmp_path, plain), harness, transport=FakeEvidence())
    env = environ_for(tmp_path)
    env.pop("NOEMA_MODEL_EVIDENCE_POOL_SIZE")
    with pytest.raises(ConfigError):
        build_compile_service(env, harness)  # the real transport reads its pool size from the environment, with no default


def test_a_service_needs_a_harness(tmp_path: Path) -> None:
    with pytest.raises(ConfigError, match="harness"):
        build_compile_service(environ_for(tmp_path), None, transport=FakeEvidence())


# --- the pack as the gate sees it -----------------------------------------------------------------------------------------------------------------------

def test_the_pack_view_is_the_packs_own_reference_and_nodes() -> None:
    packs = PackFileResolver.from_file(PACK_FILE, out_of_scope_node="fin.meta.out_of_scope")
    view = packs.view("fin@1.0.0")
    assert view is not None and view.ref() == ("fin", "1.0.0")
    node = view.node("fin.payments.exception.investigate")
    assert node is not None and "amount" in node.slots and node.outcomes == frozenset()  # the model never proposes an outcome (INV-01)
    assert view.node("fin.nope") is None
    for other in ("fin@2.0.0", "fin@1.0", "other@1.0.0", "", "fin"):
        assert packs.view(other) is None


# --- the identifiers nobody owns yet (noema#580): a default, derived, never random -------------------------------------------------------------------------

def test_the_minter_derives_the_identifiers_from_the_request_and_never_from_a_clock_or_randomness() -> None:
    minter = DerivedMinter()
    first, again = minter.identifiers(CANONICAL), minter.identifiers(dict(CANONICAL))
    assert first == again
    assert first.version == 1 and first.supersedes is None and first.created_at == CANONICAL["received_at"] and first.raw_ref == "unset"
    other = minter.identifiers({**CANONICAL, "request_id": "1b8f3c1e-52a4-4d7e-9a61-5f1d2b8c4a97"})
    assert other.intent_id != first.intent_id
    assert minter.identifiers({**CANONICAL, "tenant_id": "ten_other"}).intent_id != first.intent_id
    assert minter.identifiers({**CANONICAL, "correlation_id": "corr_other"}).intent_id != first.intent_id


def test_a_derived_id_has_no_digit_so_the_record_guard_never_sees_a_digit_run() -> None:
    minter = DerivedMinter()
    for n in range(40):
        request = {**CANONICAL, "request_id": f"{n:08x}-52a4-4d7e-9a61-5f1d2b8c4a97"}
        assert minter.identifiers(request).intent_id.startswith("int_") and minter.identifiers(request).intent_id[4:].isalpha()
        assert len(minter.identifiers(request).intent_id) == 30
        set_id = minter.candidate_set_id(request)
        assert set_id.startswith("cns_") and set_id[4:].isalpha() and len(set_id) == 30


def test_the_candidate_set_id_differs_from_the_intent_id_hash() -> None:
    minter = DerivedMinter()
    assert minter.candidate_set_id(CANONICAL)[4:] != minter.identifiers(CANONICAL).intent_id[4:]


# --- through the real route ------------------------------------------------------------------------------------------------------------------------------

def test_a_request_through_the_route_is_compiled_gated_and_returned(tmp_path: Path) -> None:
    service, model, evidence = service_for(tmp_path)
    client = TestClient(create_app(environ={}, compile_service=service))
    answer = client.post(ROUTE, json={"canonical_input": CANONICAL, "pack_ref": "fin@1.0.0"})
    assert answer.status_code == 200, answer.text
    intent = answer.json()["intent_object"]
    assert intent["status"] == "VALIDATED" and intent["intent_node"] == "fin.payments.exception.investigate" and intent["slots"]["amount"]["value"] == 2000000
    assert (intent["principal"]["principal_id"], intent["principal"]["authn_ref"]) == ("usr_session", "ses_test") and intent["source"]["raw_ref"] == "unset"
    assert intent["principal"]["principal_type"] == "unset" and intent["principal"]["asserted_by"] == "unset"  # the core sets these (A166); the model service never does
    assert intent["created_at"] == CANONICAL["received_at"] and intent["sig"] == UNSIGNED.model_dump(mode="json")
    assert model.requests and len(evidence.requests) == 1
    assert evidence.requests[0]["url"] == "https://core:8080/internal/v1/evidence/candidates" and evidence.requests[0]["tenant_id"] == "ten_test"


def test_an_unknown_pack_is_refused_and_nothing_is_called(tmp_path: Path) -> None:
    service, model, evidence = service_for(tmp_path)
    client = TestClient(create_app(environ={}, compile_service=service))
    answer = client.post(ROUTE, json={"canonical_input": CANONICAL, "pack_ref": "fin@2.0.0"})
    assert answer.status_code == 422 and model.requests == [] and evidence.requests == []


def test_an_evidence_api_that_refuses_is_a_provider_failure_never_a_guessed_intent(tmp_path: Path) -> None:
    for status in (500, 503):
        service, _, _ = service_for(tmp_path, evidence=FakeEvidence(status=status))
        client = TestClient(create_app(environ={}, compile_service=service))
        answer = client.post(ROUTE, json={"canonical_input": CANONICAL, "pack_ref": "fin@1.0.0"})
        assert answer.status_code == 502, (status, answer.text)


def test_a_process_that_names_the_configuration_but_has_no_harness_refuses_to_start(tmp_path: Path) -> None:
    with pytest.raises(ConfigError, match="harness"):
        create_app(environ=environ_for(tmp_path), evidence_transport=FakeEvidence())


# --- what the first version of these tests let through (hand mutants) -------------------------------------------------------------------------------------

def test_a_member_written_twice_in_the_configuration_refuses_to_start(tmp_path: Path) -> None:
    path = tmp_path / "compile.json"
    text = json.dumps(config_doc(tmp_path))
    path.write_text(text[:-1] + ', "timeout_s": 1}', encoding="utf-8")  # the second timeout would win in a plain json.loads
    harness, _ = harness_for(ScriptedModel())
    with pytest.raises(ConfigError, match="repeated"):
        build_compile_service({COMPILE_CONFIG_ENV: str(path), "NOEMA_MODEL_EVIDENCE_POOL_SIZE": "4", **MTLS}, harness, transport=FakeEvidence())


def test_an_unknown_member_of_the_evidence_tls_block_refuses_to_start(tmp_path: Path) -> None:
    doc = config_doc(tmp_path)
    doc["evidence_tls"] = {**doc["evidence_tls"], "insecure_skip_verify": True}
    harness, _ = harness_for(ScriptedModel())
    with pytest.raises(ConfigError):
        build_compile_service(environ_for(tmp_path, doc), harness, transport=FakeEvidence())


def test_a_pack_whose_id_or_version_is_not_a_reference_refuses_to_start() -> None:
    pack = json.loads(PACK_FILE.read_text(encoding="utf-8"))
    for change in ({"pack_id": "Fin"}, {"pack_id": "fin@x"}, {"pack_version": "1.0"}, {"pack_version": "1.0.0\n"}, {"pack_id": ""}):
        with pytest.raises(ConfigError):
            PackFileResolver.from_mapping({**pack, **change}, out_of_scope_node="fin.meta.out_of_scope")


def test_the_hash_of_the_parts_cannot_be_moved_across_a_boundary() -> None:
    assert _letters("ab", "c") != _letters("a", "bc")
    assert _letters("a", "b", "c") != _letters("abc")


def test_each_configured_bound_reaches_the_service_it_belongs_to(tmp_path: Path) -> None:
    doc = config_doc(tmp_path, timeout_s=7.5, max_proposal_bytes=70001, max_request_bytes=80002)
    harness, _ = harness_for(ScriptedModel())
    service = build_compile_service(environ_for(tmp_path, doc), harness, transport=FakeEvidence())
    assert service.max_request_bytes == 80002 and service._max == 70001 and service._timeout == 7.5


def test_a_service_handed_in_is_never_rebuilt_from_the_environment(tmp_path: Path) -> None:
    given, _, _ = service_for(tmp_path)
    app = create_app(environ=environ_for(tmp_path), compile_service=given)  # no harness here: a rebuild would refuse to start
    assert app.state.compile_service is given


def test_the_process_hands_its_own_harness_to_the_builder(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    seen: list[Any] = []

    def spy(environ: Any, harness: Any, *, transport: Any = None) -> Any:
        seen.append((environ, harness, transport))
        return "built"

    sentinel = object()
    monkeypatch.setattr("noema_model.main.build_compile_service", spy)
    monkeypatch.setattr("noema_model.main.assemble_harness", lambda env, clock, telemetry: SimpleNamespace(harness=sentinel, reason=""))
    transport = FakeEvidence()
    app = create_app(environ=environ_for(tmp_path), evidence_transport=transport)
    assert app.state.compile_service == "built" and len(seen) == 1
    assert seen[0][1] is sentinel and seen[0][2] is transport


def test_a_pidgin_request_is_compiled_through_the_route_with_the_lexicons_the_pack_names(tmp_path: Path) -> None:  # UND-16, run #6 (A532): `dis morning` is Pidgin, read from fin.lex.pcm
    mentions = {**GOOD_MENTIONS, "when": {**GOOD_MENTIONS["when"], "surface_form": "dis morning"}}
    service, model, evidence = service_for(tmp_path, ScriptedModel(answers=dict(G_A), mentions=mentions))
    client = TestClient(create_app(environ={}, compile_service=service))
    answer = client.post(ROUTE, json={"canonical_input": {**CANONICAL, "text": "Wetin happen to the 20k wey I send Tunde dis morning?"}, "pack_ref": "fin@1.0.0"})
    assert answer.status_code == 200 and len(evidence.requests) == 1
