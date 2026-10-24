"""The candidate-read client against contracts/schemas/candidates-{request,response}.v1.json (REQ-CTX-006, REQ-AMB-002,
REQ-SEC-002, INV-12). Every fixture Session A published runs through this code, and the constants the client holds are
compared with the schemas, so a contract change that the client does not follow fails here."""

import json
import ssl
import subprocess
import threading
from collections.abc import Iterator
from datetime import UTC, datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

import pytest
from hypothesis import given, settings
from hypothesis import strategies as st

from noema_model.evidence import (
    CandidateResponseInvalid,
    EvidenceClient,
    EvidenceClientConfig,
    UrllibEvidenceTransport,
    build_request,
    parse_response,
)
from noema_model.evidence import client as client_module
from noema_model.platform.config import ConfigError
from noema_model.platform.mtls import client_ssl_context
from noema_model.understanding.pipeline import (
    CandidateQuery,
    CandidateRequestInvalid,
    CandidateScopeError,
    CandidateSourceError,
    Outcome,
    run_pipeline,
)
from noema_model.understanding.resolution import ResolutionState
from tests.support import run
from tests.support_pipeline import (
    G_A,
    GOOD_MENTIONS,
    FakeSource,
    ScriptedModel,
    candidate,
    harness_for,
    make_input,
    no_errors,
    normaliser,
    pipeline_config,
)

CONTRACTS = Path(__file__).resolve().parents[3] / "contracts"
SCHEMAS = CONTRACTS / "schemas"
FIX = CONTRACTS / "fixtures"
REQ_SCHEMA = json.loads((SCHEMAS / "candidates-request.v1.json").read_text())
RESP_SCHEMA = json.loads((SCHEMAS / "candidates-response.v1.json").read_text())


def load(*parts: str) -> Any:
    return json.loads(FIX.joinpath(*parts).read_text())


def utc(text: str) -> datetime:
    return datetime.fromisoformat(text).astimezone(UTC)


# --- the client's constants equal the schemas --------------------------------------------------------------------------

def test_the_constants_the_client_holds_equal_the_contract() -> None:
    cand = RESP_SCHEMA["$defs"]["Candidate"]
    assert client_module.MAX_CANDIDATES == RESP_SCHEMA["properties"]["candidates"]["maxItems"]
    assert client_module.SURFACE_MAX == REQ_SCHEMA["properties"]["filters"]["properties"]["counterparty_surface"]["maxLength"]
    assert client_module.DIRECTIONS == set(cand["properties"]["direction"]["enum"])
    assert client_module._CANDIDATE_KEYS == set(cand["properties"])
    assert client_module._REQUIRED_CANDIDATE_KEYS == set(cand["required"])
    assert cand["additionalProperties"] is False and RESP_SCHEMA["additionalProperties"] is False
    assert set(RESP_SCHEMA["required"]) == set(RESP_SCHEMA["properties"]) == {"schema_version", "tenant_id", "correlation_id", "candidates"}
    assert set(REQ_SCHEMA["properties"]) == {"schema_version", "tenant_id", "correlation_id", "principal_ref", "resource_kind", "filters"}
    for name, pattern in (("candidate_id", client_module._CANDIDATE_ID), ("account_last4", client_module._LAST4)):
        assert cand["properties"][name]["pattern"] == pattern.pattern
    common = json.loads((SCHEMAS / "common.v1.json").read_text())["$defs"]
    for name, pattern in (("TenantId", client_module._TENANT), ("CorrelationId", client_module._CORRELATION),
                          ("PrincipalId", client_module._PRINCIPAL), ("ResourceRef", client_module._RESOURCE_REF),
                          ("Timestamp", client_module._TIMESTAMP)):
        assert common[name]["pattern"] == pattern.pattern, name
    money = json.loads((SCHEMAS / "money.v1.json").read_text())["$defs"]["MinorAmount"]
    assert money["maximum"] == client_module.MAX_MINOR
    assert client_module.SCHEMA_VERSION == load("valid", "candidates-request", "time_window_only.json")["schema_version"]


# --- every published fixture ---------------------------------------------------------------------------------------------

@pytest.mark.parametrize("name", sorted(p.name for p in (FIX / "valid" / "candidates-response").glob("*.json")))
def test_every_valid_response_fixture_is_accepted(name: str) -> None:
    body = (FIX / "valid" / "candidates-response" / name).read_bytes()
    doc = json.loads(body)
    got = parse_response(body, tenant_id=doc["tenant_id"], correlation_id=doc["correlation_id"])
    assert [c.candidate_id for c in got] == [c["candidate_id"] for c in doc["candidates"]]


@pytest.mark.parametrize("name", sorted(json.loads((FIX / "invalid" / "candidates-response" / "expectations.json").read_text())))
def test_every_invalid_response_fixture_is_refused_whole(name: str) -> None:
    body = (FIX / "invalid" / "candidates-response" / name).read_bytes()
    doc = json.loads(body)
    with pytest.raises(CandidateResponseInvalid):
        parse_response(body, tenant_id=doc.get("tenant_id", "ten_demo"), correlation_id=doc.get("correlation_id", "corr_4c21f9"))


def test_the_client_reads_the_fixture_values_exactly() -> None:
    body = (FIX / "valid" / "candidates-response" / "two_newest_first.json").read_bytes()
    first, second = parse_response(body, tenant_id="ten_demo", correlation_id="corr_4c21f9")
    assert (first.amount_minor, first.currency, first.account_last4, first.direction, first.beneficiary_alias_match) == (2_000_000, "NGN", "4821", "OUTGOING", True)
    assert first.occurred_at == utc("2026-10-01T09:30:00+00:00") and first.resource_ref == "fin.transaction:TX88001"
    assert second.beneficiary_alias_match is None and second.direction == "INCOMING"


def test_the_empty_answer_is_no_candidates_not_an_error() -> None:
    body = (FIX / "valid" / "candidates-response" / "empty_no_match.json").read_bytes()
    doc = json.loads(body)
    assert parse_response(body, tenant_id=doc["tenant_id"], correlation_id=doc["correlation_id"]) == ()


def query(**over: Any) -> CandidateQuery:
    base: dict[str, Any] = dict(
        correlation_id="corr_4c21f9", principal_ref="usr_88213", tenant_id="ten_demo", resource_kind="fin.transaction",
        amount_range=(1_980_000, 2_020_000), currency="NGN",
        time_window=(utc("2026-10-01T07:00:00+00:00"), utc("2026-10-01T11:00:00+00:00")), counterparty_surface="Ada",
    )
    return CandidateQuery(**{**base, **over})


def test_the_request_equals_the_published_fixtures() -> None:
    assert build_request(query()) == load("valid", "candidates-request", "amount_and_counterparty.json")
    bare = query(amount_range=None, currency=None, counterparty_surface=None)
    assert build_request(bare) == load("valid", "candidates-request", "time_window_only.json")


@pytest.mark.parametrize(
    "over",
    [
        {"time_window": None},                                  # missing_time_window
        {"principal_ref": "prn_88213"},                         # principal_wrong_prefix
        {"principal_ref": "usr_8"},
        {"tenant_id": "tenant_demo"},
        {"correlation_id": "cor_4c21f9"},
        {"resource_kind": "transaction"},                       # resource_kind_without_vertical
        {"resource_kind": None},
        {"amount_range": (2_020_000, 1_980_000)},
        {"amount_range": (-1, 5)},
        {"amount_range": (0, 2**53)},
        {"amount_range": (1.5, 3)},                             # amount_range_float
        {"amount_range": (True, 3)},
        {"currency": "naira"},
        {"time_window": (utc("2026-10-01T11:00:00+00:00"), utc("2026-10-01T07:00:00+00:00"))},
        {"time_window": (datetime(2026, 10, 1), datetime(2026, 10, 2))},
    ],
)
def test_a_query_the_contract_cannot_carry_is_refused_before_anything_is_sent(over: dict[str, Any]) -> None:
    with pytest.raises(CandidateRequestInvalid):
        build_request(query(**over))


def test_an_empty_counterparty_filter_is_refused_not_dropped() -> None:
    for empty in ("", "   ", "\u200b\x00", "\n\t"):  # empty, blank, or nothing left after control characters are removed
        with pytest.raises(CandidateRequestInvalid):
            build_request(query(counterparty_surface=empty))


def test_an_over_long_counterparty_filter_is_refused_not_dropped() -> None:
    for long in ("x" * 65, "x" * 500, "y" * 64 + "z"):
        with pytest.raises(CandidateRequestInvalid):
            build_request(query(counterparty_surface=long))
    with pytest.raises(CandidateRequestInvalid):
        build_request(query(counterparty_surface="x" * 60 + "\u200b" * 10 + "abcde"))  # 65 real characters after cleaning


def test_a_counterparty_filter_that_fits_is_sent_and_no_filter_means_none_was_named() -> None:
    assert build_request(query(counterparty_surface="x" * 64))["filters"]["counterparty_surface"] == "x" * 64
    assert build_request(query(counterparty_surface="Ad\u200ba\n"))["filters"]["counterparty_surface"] == "Ada"
    assert "counterparty_surface" not in build_request(query(counterparty_surface=None))["filters"]


def test_an_amount_range_without_a_currency_is_left_out() -> None:
    assert "amount_range" not in build_request(query(currency=None))["filters"]


def test_the_request_holds_only_the_contract_fields_and_never_a_free_text_field() -> None:
    request = build_request(query())
    assert set(request) == set(REQ_SCHEMA["properties"]) and set(request["filters"]) <= set(REQ_SCHEMA["properties"]["filters"]["properties"])


# --- answers that are not for this request, or not newest first ----------------------------------------------------------------

def body_with(**over: Any) -> bytes:
    doc = load("valid", "candidates-response", "two_newest_first.json")
    return json.dumps({**doc, **over}).encode()


def test_an_answer_for_another_tenant_or_correlation_is_refused() -> None:
    for over in ({"tenant_id": "ten_other"}, {"correlation_id": "corr_other"}):
        with pytest.raises(CandidateResponseInvalid):
            parse_response(body_with(**over), tenant_id="ten_demo", correlation_id="corr_4c21f9")


def test_order_duplicates_and_extra_top_level_fields_are_refused() -> None:
    doc = load("valid", "candidates-response", "two_newest_first.json")
    flipped = {**doc, "candidates": list(reversed(doc["candidates"]))}
    dup = {**doc, "candidates": [doc["candidates"][0], doc["candidates"][0]]}
    tie_wrong = {**doc, "candidates": [{**doc["candidates"][1], "candidate_id": "cand_009", "occurred_at": doc["candidates"][0]["occurred_at"]}, doc["candidates"][0]]}
    for bad in (flipped, dup, tie_wrong, {**doc, "extra": 1}, {k: v for k, v in doc.items() if k != "candidates"}):
        with pytest.raises(CandidateResponseInvalid):
            parse_response(json.dumps(bad).encode(), tenant_id="ten_demo", correlation_id="corr_4c21f9")


@pytest.mark.parametrize("body", [b"", b"not json", b"[]", b"null", b'{"candidates": NaN}', b"\xff"])
def test_garbage_is_refused(body: bytes) -> None:
    with pytest.raises(CandidateResponseInvalid):
        parse_response(body, tenant_id="ten_demo", correlation_id="corr_4c21f9")


JSON_VALUES = st.recursive(
    st.none() | st.booleans() | st.integers() | st.text(max_size=10),
    lambda inner: st.lists(inner, max_size=3) | st.dictionaries(st.text(max_size=10), inner, max_size=5),
    max_leaves=10,
)


@settings(max_examples=300, deadline=None)
@given(JSON_VALUES)
def test_property_nothing_but_the_contract_shape_is_ever_returned(value: Any) -> None:
    try:
        got = parse_response(json.dumps(value).encode(), tenant_id="ten_demo", correlation_id="corr_4c21f9")
    except CandidateResponseInvalid:
        return
    assert isinstance(value, dict) and len(got) <= client_module.MAX_CANDIDATES


@settings(max_examples=200, deadline=None)
@given(st.integers(0, 40), st.integers(0, 40))
def test_property_a_valid_answer_of_any_allowed_size_round_trips(n: int, extra: int) -> None:
    rows = [
        {"candidate_id": f"cand_{i:03d}", "resource_ref": f"fin.transaction:TX{i}", "amount_minor": i, "currency": "NGN",
         "occurred_at": f"2026-10-01T{23 - i // 60:02d}:{59 - i % 60:02d}:00Z", "counterparty_display": "A. O.", "account_last4": "0001",
         "direction": "OUTGOING"}
        for i in range(min(n, 20) + (extra if n > 20 else 0))
    ]
    body = json.dumps({"schema_version": "1.0", "tenant_id": "ten_demo", "correlation_id": "corr_4c21f9", "candidates": rows}).encode()
    if len(rows) > 20:
        with pytest.raises(CandidateResponseInvalid):
            parse_response(body, tenant_id="ten_demo", correlation_id="corr_4c21f9")
    else:
        assert len(parse_response(body, tenant_id="ten_demo", correlation_id="corr_4c21f9")) == len(rows)


def test_config_is_strict_and_https_only() -> None:
    ok = {"endpoint": "https://core.example.invalid", "timeout_ms": 500, "max_response_bytes": 65536}
    assert EvidenceClientConfig.from_mapping(ok).timeout_ms == 500
    for bad in ({**ok, "endpoint": "http://core"}, {**ok, "extra": 1}, {k: v for k, v in ok.items() if k != "timeout_ms"}, {**ok, "timeout_ms": 0}):
        with pytest.raises(ConfigError):
            EvidenceClientConfig.from_mapping(bad)


# --- on a real socket --------------------------------------------------------------------------------------------------------

class Core:
    """A local stand-in for core's endpoint that records what it was sent. Plain HTTP on loopback, or mutual TLS."""

    def __init__(self, tls: ssl.SSLContext | None = None) -> None:
        self.status, self.body, self.delay = 200, b"", 0.0
        self.seen: list[tuple[str, dict[str, Any]]] = []
        self.peer_cert: list[Any] = []
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:  # noqa: N802
                raw = self.rfile.read(int(self.headers.get("content-length", 0)))
                outer.seen.append((self.path, json.loads(raw)))
                if isinstance(self.connection, ssl.SSLSocket):
                    outer.peer_cert.append(self.connection.getpeercert())
                import time
                time.sleep(outer.delay)
                self.send_response(outer.status)
                self.send_header("content-length", str(len(outer.body)))
                self.end_headers()
                self.wfile.write(outer.body)

            def log_message(self, *a: Any) -> None:
                return None

        class Server(ThreadingHTTPServer):
            def handle_error(self, request: Any, client_address: Any) -> None:
                return None

        self.httpd = Server(("127.0.0.1", 0), Handler)
        if tls is not None:
            self.httpd.socket = tls.wrap_socket(self.httpd.socket, server_side=True)
        port = self.httpd.server_address[1]
        self.url = f"{'https' if tls else 'http'}://localhost:{port}" if tls else f"http://127.0.0.1:{port}"
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    def close(self) -> None:
        self.httpd.shutdown()
        self.httpd.server_close()


@pytest.fixture
def core() -> Iterator[Core]:
    c = Core()
    c.body = (FIX / "valid" / "candidates-response" / "two_newest_first.json").read_bytes()
    yield c
    c.close()


def client_for(url: str, transport: Any = None, timeout_ms: int = 500, max_bytes: int = 65536) -> EvidenceClient:
    config = EvidenceClientConfig(url, timeout_ms, max_bytes)
    return EvidenceClient(config, transport or UrllibEvidenceTransport(None, allow_loopback_http=True))


def test_a_real_round_trip_sends_the_contract_request_and_returns_candidates(core: Core) -> None:
    got = run(client_for(core.url).fetch(query()))
    assert [c.candidate_id for c in got] == ["cand_001", "cand_002"]
    path, sent = core.seen[0]
    assert path == "/internal/v1/evidence/candidates" and sent == load("valid", "candidates-request", "amount_and_counterparty.json")


@pytest.mark.parametrize(("status", "error"), [(403, CandidateScopeError), (400, CandidateRequestInvalid), (422, CandidateRequestInvalid),
                                               (429, CandidateSourceError), (500, CandidateSourceError), (503, CandidateSourceError), (404, CandidateSourceError)])
def test_http_statuses_map_to_typed_errors(core: Core, status: int, error: type[Exception]) -> None:
    core.status, core.body = status, b'{"detail": "SECRET-MARKER"}'
    with pytest.raises(error) as caught:
        run(client_for(core.url).fetch(query()))
    assert "SECRET-MARKER" not in str(caught.value)
    if error is CandidateSourceError:
        assert type(caught.value) is CandidateSourceError


def test_a_slow_core_hits_the_deadline_and_a_big_answer_is_refused(core: Core) -> None:
    core.delay = 1.0
    with pytest.raises(CandidateSourceError):
        run(client_for(core.url, timeout_ms=100).fetch(query()))
    core.delay = 0.0
    with pytest.raises(CandidateSourceError):
        run(client_for(core.url, max_bytes=100).fetch(query()))


def test_a_closed_port_is_a_source_error() -> None:
    dead = Core()
    url = dead.url
    dead.close()
    with pytest.raises(CandidateSourceError):
        run(client_for(url).fetch(query()))


def test_the_default_transport_refuses_plaintext_and_needs_a_client_certificate() -> None:
    for url in ("http://core.example.invalid", "http://127.0.0.1:1", "https://core.example.invalid"):
        with pytest.raises(CandidateSourceError, match="client certificate"):  # refused before any connection is tried
            run(UrllibEvidenceTransport(None).post(url, {}, b"{}", 1.0, 100))
    with pytest.raises(CandidateSourceError, match="client certificate"):
        run(UrllibEvidenceTransport(None, allow_loopback_http=True).post("http://example.invalid", {}, b"{}", 1.0, 100))


def test_a_mismatched_answer_over_the_wire_is_refused(core: Core) -> None:
    core.body = body_with(tenant_id="ten_other")
    with pytest.raises(CandidateResponseInvalid):
        run(client_for(core.url).fetch(query()))


# --- mutual TLS, for real --------------------------------------------------------------------------------------------------------

def openssl(*args: str, cwd: Path) -> None:
    subprocess.run(["openssl", *args], cwd=cwd, check=True, capture_output=True)


@pytest.fixture
def pki(tmp_path: Path) -> Path:
    try:
        openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", "ca.key", "-out", "ca.pem", "-days", "2", "-subj", "/CN=test-ca", cwd=tmp_path)
        for name, ext in (("server", "subjectAltName=DNS:localhost,IP:127.0.0.1"), ("client", "subjectAltName=DNS:model")):
            openssl("req", "-newkey", "rsa:2048", "-nodes", "-keyout", f"{name}.key", "-out", f"{name}.csr", "-subj", f"/CN={name}", cwd=tmp_path)
            (tmp_path / f"{name}.ext").write_text(ext + "\n")
            openssl("x509", "-req", "-in", f"{name}.csr", "-CA", "ca.pem", "-CAkey", "ca.key", "-CAcreateserial", "-out", f"{name}.pem", "-days", "2", "-extfile", f"{name}.ext", cwd=tmp_path)
        openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", "rogue.key", "-out", "rogue.pem", "-days", "2", "-subj", "/CN=rogue", cwd=tmp_path)
    except (OSError, subprocess.CalledProcessError) as exc:
        pytest.skip(f"openssl unavailable: {exc}")
    return tmp_path


def server_context(pki: Path) -> ssl.SSLContext:
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.load_cert_chain(str(pki / "server.pem"), str(pki / "server.key"))
    ctx.load_verify_locations(str(pki / "ca.pem"))
    ctx.verify_mode = ssl.CERT_REQUIRED
    return ctx


def test_mutual_tls_presents_the_client_certificate_and_verifies_the_server(pki: Path) -> None:
    core = Core(server_context(pki))
    core.body = (FIX / "valid" / "candidates-response" / "two_newest_first.json").read_bytes()
    try:
        context = client_ssl_context(str(pki / "client.pem"), str(pki / "client.key"), str(pki / "ca.pem"))
        assert context.verify_mode == ssl.CERT_REQUIRED and context.check_hostname and context.minimum_version >= ssl.TLSVersion.TLSv1_2
        got = run(client_for(core.url, UrllibEvidenceTransport(context)).fetch(query()))
        assert len(got) == 2
        assert core.peer_cert and core.peer_cert[0]["subject"][0][0] == ("commonName", "client")
    finally:
        core.close()


def test_without_a_client_certificate_or_with_the_wrong_ca_nothing_is_read(pki: Path) -> None:
    core = Core(server_context(pki))
    core.body = (FIX / "valid" / "candidates-response" / "two_newest_first.json").read_bytes()
    try:
        no_cert = ssl.create_default_context(cafile=str(pki / "ca.pem"))
        with pytest.raises(CandidateSourceError):
            run(client_for(core.url, UrllibEvidenceTransport(no_cert)).fetch(query()))
        wrong_ca = client_ssl_context(str(pki / "client.pem"), str(pki / "client.key"), str(pki / "rogue.pem"))
        with pytest.raises(CandidateSourceError):
            run(client_for(core.url, UrllibEvidenceTransport(wrong_ca)).fetch(query()))
        assert core.seen == []
    finally:
        core.close()


def test_missing_or_unusable_mtls_material_is_a_config_error(pki: Path, tmp_path: Path) -> None:
    with pytest.raises(ConfigError):
        client_ssl_context(str(tmp_path / "none.pem"), str(tmp_path / "none.key"), str(pki / "ca.pem"))
    with pytest.raises(ConfigError):
        client_ssl_context(str(pki / "client.pem"), str(pki / "server.key"), str(pki / "ca.pem"))  # key does not match


# --- through the pipeline -------------------------------------------------------------------------------------------------------

def good_model() -> ScriptedModel:
    return ScriptedModel(answers=G_A, mentions=GOOD_MENTIONS)


def pipeline(source: Any) -> Any:
    harness, _ = harness_for(good_model())
    return run(run_pipeline(make_input(), pipeline_config(), harness, source, normaliser, no_errors))


def test_the_client_drives_the_pipeline_to_an_auto_resolve_on_a_contract_answer(core: Core) -> None:
    core.body = json.dumps({
        "schema_version": "1.0", "tenant_id": "ten_test", "correlation_id": "corr_test",
        "candidates": [{"candidate_id": "cand_001", "resource_ref": "fin.transaction:TX1", "amount_minor": 2_000_000, "currency": "NGN",
                        "occurred_at": "2026-10-21T08:00:00Z", "counterparty_display": "Tunde Adeyemi", "account_last4": "0001",
                        "direction": "OUTGOING", "beneficiary_alias_match": True}],
    }).encode()
    result = pipeline(client_for(core.url))
    assert result.outcome is Outcome.PROPOSED and result.resolution is not None
    assert result.resolution.state is ResolutionState.AUTO_RESOLVED and result.resolution.resolved_candidate_id == "cand_001"
    _, sent = core.seen[0]
    assert (sent["principal_ref"], sent["tenant_id"], sent["correlation_id"], sent["resource_kind"]) == ("usr_session", "ten_test", "corr_test", "fin.transaction")


def test_a_403_from_core_stops_the_turn_with_the_scope_code() -> None:
    result = pipeline(FakeSource(raises=CandidateScopeError()))
    assert result.outcome is Outcome.REJECTED_AT_GATE and result.gate_errors == ("TENANT_SCOPE_MISMATCH",) and result.resolution is None


def test_a_refused_query_is_not_retryable_input_invalid() -> None:
    assert pipeline(FakeSource(raises=CandidateRequestInvalid())).gate_errors == ("INPUT_INVALID",)


def test_a_contract_breaking_answer_is_a_dependency_error_and_nothing_is_ranked() -> None:
    assert pipeline(FakeSource(raises=CandidateResponseInvalid("x"))).gate_errors == ("DEPENDENCY_UNAVAILABLE",)


def test_with_no_time_window_nothing_is_read_and_the_request_is_input_invalid() -> None:
    from dataclasses import replace
    source = FakeSource((candidate("a"),))

    def no_window(inp: Any, proposal: Any) -> Any:
        return replace(normaliser(inp, proposal), window=None)

    harness, _ = harness_for(good_model())
    result = run(run_pipeline(make_input(), pipeline_config(), harness, source, no_window, no_errors))
    assert source.queries == [] and result.outcome is Outcome.REJECTED_AT_GATE and result.gate_errors == ("INPUT_INVALID",)
    assert result.resolution is None and result.query is None  # no search ran, so there is no resolution to report
    assert "NO_TRANSACTION_MATCH" not in result.ambiguity_flags  # that code is for a search that came back empty (REQ-AMB-002)


def test_an_empty_answer_to_a_real_search_is_still_no_transaction_match() -> None:
    result = pipeline(FakeSource(()))
    assert result.resolution is not None and result.resolution.state is ResolutionState.NO_CANDIDATES
    assert "NO_TRANSACTION_MATCH" in result.ambiguity_flags and result.gate_errors == ()


# --- the filter defect, through the pipeline -------------------------------------------------------------------------------

def _surface_normaliser(surface: str) -> Any:
    from dataclasses import replace

    def normalise(inp: Any, proposal: Any) -> Any:
        return replace(normaliser(inp, proposal), counterparty_surface=surface)

    return normalise


@pytest.mark.parametrize("surface", ["", "   ", "x" * 65])
def test_a_counterparty_filter_the_contract_cannot_carry_stops_the_turn_with_input_invalid(surface: str, core: Core) -> None:
    harness, _ = harness_for(good_model())
    result = run(run_pipeline(make_input(), pipeline_config(), harness, client_for(core.url), _surface_normaliser(surface), no_errors))
    assert result.outcome is Outcome.REJECTED_AT_GATE and result.gate_errors == ("INPUT_INVALID",)
    assert core.seen == [] and result.resolution is None  # nothing was sent, with or without the filter


def test_the_evidence_client_never_sends_a_request_without_a_filter_it_was_asked_to_apply(core: Core) -> None:
    with pytest.raises(CandidateRequestInvalid):
        run(client_for(core.url).fetch(query(counterparty_surface="")))
    with pytest.raises(CandidateRequestInvalid):
        run(client_for(core.url).fetch(query(counterparty_surface="x" * 65)))
    assert core.seen == []


# --- boundaries of the package ----------------------------------------------------------------------------------------------------

def test_the_evidence_package_sits_in_the_model_service_and_keeps_its_boundaries() -> None:
    import ast
    root = Path(__file__).resolve().parents[1] / "noema_model"
    package = root / "evidence"
    assert package.is_dir() and (package / "client.py").is_file()
    for path in package.glob("*.py"):
        for node in ast.walk(ast.parse(path.read_text())):
            modules = [a.name for a in node.names] if isinstance(node, ast.Import) else [node.module or ""] if isinstance(node, ast.ImportFrom) else []
            for module in modules:
                parts = module.split(".")
                assert "providers" not in parts and "harness" not in parts, f"{path.name} imports {module}"
    for path in (root / "understanding" / "pipeline").glob("*.py"):
        assert "noema_model.evidence" not in path.read_text(), f"{path.name} imports the evidence client"


# --- the mutation test: the drop-the-filter bug must fail the suite --------------------------------------------------------------------

def test_the_drop_the_filter_bug_fails_the_suite(tmp_path: Path) -> None:
    """Copy the service, put the bug back (an unusable filter is silently left out), and run the refusal tests against the
    copy. They must pass on the real code and fail on the mutant, so this defect cannot return unnoticed."""
    import shutil
    import sys

    repo = Path(__file__).resolve().parents[3]
    work = tmp_path / "mutant"
    (work / "services").mkdir(parents=True)
    shutil.copytree(repo / "services" / "model", work / "services" / "model", ignore=shutil.ignore_patterns("__pycache__", ".pytest_cache", ".mypy_cache"))
    for shared in ("contracts", "packs"):
        (work / shared).symlink_to(repo / shared, target_is_directory=True)
    model = work / "services" / "model"
    selection = ["-k", "refused_not_dropped or stops_the_turn_with_input_invalid or never_sends_a_request_without_a_filter"]

    def run_suite() -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, "-m", "pytest", "-q", "-x", "-p", "no:cacheprovider", "tests/test_evidence_client.py", *selection],
            cwd=model, capture_output=True, text=True, env={"PATH": "/usr/bin:/bin", "PYTHONPATH": str(model), "HOME": str(tmp_path)},
        )

    assert run_suite().returncode == 0, "the refusal tests must pass on the real code"
    client = model / "noema_model" / "evidence" / "client.py"
    source = client.read_text()
    refusal = '        raise CandidateRequestInvalid("the counterparty filter is empty or longer than the contract allows")\n    return cleaned\n'
    bug = '        return ""\n    return cleaned\n'  # the old behaviour: hand back nothing, and the caller drops the filter
    assert refusal in source
    client.write_text(source.replace(refusal, bug, 1).replace(
        'filters["counterparty_surface"] = _surface(query.counterparty_surface)',
        'filters["counterparty_surface"] = _surface(query.counterparty_surface)\n        if not filters["counterparty_surface"]:\n            del filters["counterparty_surface"]', 1))
    mutant = run_suite()
    assert mutant.returncode != 0, "the drop-the-filter bug passed the suite:\n" + mutant.stdout[-800:]
