"""The recorded-replay transport: serves committed, SYNTHETIC recordings by the SHA-256 of the request bytes (A619, A261, A188).

REQ-INT-002, REQ-TST-004, AGENT-12 (no mocked failure modes: the adapter and the harness types are the real code; only the
network is replaced by recordings), AGENT-14 (no new dependency), AGENT-25 (SYNTHETIC labels), INV-29 (no credential is read
or echoed). Every directory below is built by the test; the committed fixtures are only loaded, never edited.
"""

import ast
import copy
import hashlib
import json
import socket
from pathlib import Path
from typing import Any

import pytest

from noema_model.providers.llm.base import (
    AdapterSettings,
    HttpResponse,
    NativeChoice,
    NativeChoiceAnswer,
    NativePredicate,
    NativePredicateAnswer,
    NativeRequest,
    ProviderError,
    ProviderFailure,
)
from noema_model.providers.llm.openai_decisions import OpenAIDecisions
from noema_model.providers.llm.replay import ReplayFixturesRefused, ReplayMiss, ReplayTransport
from tests.support import run

FIXTURE_ROOT = Path(__file__).parent / "fixtures" / "replay"
URL = "https://replay.invalid/v1/decisions"  # never contacted: a replay opens no connection
SYNTHETIC_LABEL = "Understanding: recorded replay of live output"


def key_of(body: bytes) -> str:
    return "sha256:" + hashlib.sha256(body).hexdigest()


def reply_for(n: int) -> dict[str, Any]:
    return {"model": "model-alpha", "object": "response", "status": "completed", "n": n}


def write_dir(
    root: Path,
    bodies: list[bytes],
    *,
    unusable: tuple[int, ...] = (),
    synthetic: bool = True,
    mutate: Any = None,
) -> Path:
    """A fixtures directory the way `fixtures reduce` writes one: a manifest and one `rec-NNNN.json` per call."""
    root.mkdir(parents=True, exist_ok=True)
    rows: list[dict[str, Any]] = []
    for i, body in enumerate(bodies, start=1):
        rid = f"rec-{i:04d}"
        bad = i in unusable
        rows.append(
            {
                "case_id": f"C{i}",
                "cost_micros": 1,
                "failure": None,
                "recording_id": rid,
                "request_sha256": key_of(body),
                "status": 400 if bad else 200,
            }
        )
        doc = {
            "case_id": f"C{i}",
            "failure": None,
            "recording_id": rid,
            "reply": None if bad else reply_for(i),
            "fixture": {"labels": {"usable_as_fixture": not bad, "reason": "x"}, "synthetic": True},
        }
        (root / f"{rid}.json").write_text(json.dumps(doc), encoding="utf-8")
    manifest = {
        "calls": len(bodies),
        "date": "2026-11-24",
        "fixture": {"synthetic": synthetic, "label": SYNTHETIC_LABEL, "run_id": "replay-test", "reduced": True},
        "label": "test",
        "model_id": "model-alpha",
        "pin": {},
        "provider": "test",
        "recordings": rows,
        "reported_model_ids": [],
        "run_id": "replay-test",
    }
    if mutate is not None:
        mutate(root, manifest)
    (root / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
    return root


def post(transport: ReplayTransport, body: bytes, *, max_bytes: int = 1 << 20) -> HttpResponse:
    return run(transport.post(URL, {"authorization": "Bearer replay-test-credential"}, body, 5.0, max_bytes))


# ---- serving ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------


def test_a_recorded_request_is_answered_with_its_recorded_reply(tmp_path: Path) -> None:
    """REQ-INT-002: the reply is the recording's, as the vendor's JSON body, status 200."""
    t = ReplayTransport(write_dir(tmp_path / "f", [b'{"q":1}', b'{"q":2}']))
    got = post(t, b'{"q":2}')
    assert got.status == 200
    assert json.loads(got.body) == reply_for(2)


def test_the_key_is_the_exact_bytes_of_the_request(tmp_path: Path) -> None:
    """A619: one byte of difference is a miss (the recorded hash is of the bytes the transport sent, A261)."""
    t = ReplayTransport(write_dir(tmp_path / "f", [b'{"q":1}']))
    for other in (b'{"q": 1}', b'{"q":1} ', b'{"q":1}\n', b""):
        with pytest.raises(ReplayMiss) as e:
            post(t, other)
        assert e.value.code == "UNKNOWN_REQUEST"


def test_a_request_with_no_recording_fails_closed_and_opens_no_connection(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """AGENT-12, INV-29: never a live fall-through and never a default reply."""

    def boom(*_a: object, **_k: object) -> None:
        raise AssertionError("a replay opened a socket")

    t = ReplayTransport(write_dir(tmp_path / "f", [b"a"]))
    monkeypatch.setattr(socket.socket, "connect", boom)
    monkeypatch.setattr(socket, "create_connection", boom)
    with pytest.raises(ProviderError) as e:
        post(t, b"never recorded")
    assert isinstance(e.value, ReplayMiss)
    assert e.value.kind is ProviderFailure.REJECTED


def test_a_recorded_failure_is_evidence_and_is_never_served(tmp_path: Path) -> None:
    """A320: a failed call is negative evidence only; it is refused with its own code, and the text holds no body."""
    t = ReplayTransport(write_dir(tmp_path / "f", [b"ok", b"failed"], unusable=(2,)))
    with pytest.raises(ReplayMiss) as e:
        post(t, b"failed")
    assert e.value.code == "EVIDENCE_ONLY"
    assert "failed" not in str(e.value) and "alpha" not in str(e.value)
    assert post(t, b"ok").status == 200


def test_the_answer_is_deterministic_and_the_set_is_read_once(tmp_path: Path) -> None:
    """INV-21: the same bytes give the same answer; later changes of the files change nothing (no state shared with the disk)."""
    root = write_dir(tmp_path / "f", [b"a"])
    t = ReplayTransport(root)
    first = post(t, b"a")
    (root / "rec-0001.json").write_text("not json", encoding="utf-8")
    assert post(t, b"a") == first
    assert isinstance(first.body, bytes)


def test_an_answer_over_the_size_cap_is_malformed_like_the_real_transport(tmp_path: Path) -> None:
    t = ReplayTransport(write_dir(tmp_path / "f", [b"a"]))
    with pytest.raises(ProviderError) as e:
        post(t, b"a", max_bytes=8)
    assert e.value.kind is ProviderFailure.MALFORMED


def test_the_credential_header_is_never_read_into_an_answer_or_an_error(tmp_path: Path) -> None:
    """INV-29, AGENT-13."""
    t = ReplayTransport(write_dir(tmp_path / "f", [b"a"]))
    assert b"replay-test-credential" not in post(t, b"a").body
    with pytest.raises(ReplayMiss) as e:
        post(t, b"zzz")
    assert "replay-test-credential" not in str(e.value) and "replay.invalid" not in str(e.value)


# ---- the directory --------------------------------------------------------------------------------------------------------------------------------------------------------------------


def test_a_directory_that_is_missing_or_not_a_directory_is_refused(tmp_path: Path) -> None:
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(tmp_path / "absent")
    (tmp_path / "file").write_text("x", encoding="utf-8")
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(tmp_path / "file")


def test_a_manifest_that_is_a_link_is_refused(tmp_path: Path) -> None:
    """AGENT-13: regular files only, never a link."""
    root = write_dir(tmp_path / "f", [b"a"])
    real = tmp_path / "elsewhere.json"
    (root / "manifest.json").rename(real)
    (root / "manifest.json").symlink_to(real)
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(root)


def test_a_recording_that_is_a_link_is_refused(tmp_path: Path) -> None:
    root = write_dir(tmp_path / "f", [b"a"])
    real = tmp_path / "rec.json"
    (root / "rec-0001.json").rename(real)
    (root / "rec-0001.json").symlink_to(real)
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(root)


def test_a_file_over_one_mebibyte_is_refused(tmp_path: Path) -> None:
    root = write_dir(tmp_path / "f", [b"a"])
    (root / "rec-0001.json").write_text(" " * ((1 << 20) + 1), encoding="utf-8")
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(root)


@pytest.mark.parametrize("bad", ["{", "[]", '{"recordings": 1}', '{"a": NaN}', '{"a": 1, "a": 2}', ""])
def test_a_manifest_that_is_not_strict_json_of_the_expected_shape_is_refused(tmp_path: Path, bad: str) -> None:
    root = write_dir(tmp_path / "f", [b"a"])
    (root / "manifest.json").write_text(bad, encoding="utf-8")
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(root)


def test_a_non_finite_number_or_a_duplicate_key_in_an_otherwise_valid_file_is_refused(tmp_path: Path) -> None:
    """Strict JSON, as the recorder's own loader: NaN and a repeated key are refused wherever they are (manifest and recording)."""
    for index, injected in enumerate(('"extra": NaN', '"extra": Infinity', '"calls": 1')):
        root = write_dir(tmp_path / f"m{index}", [b"a"])
        text = (root / "manifest.json").read_text(encoding="utf-8")
        (root / "manifest.json").write_text(text[:-1] + ", " + injected + "}", encoding="utf-8")
        with pytest.raises(ReplayFixturesRefused):
            ReplayTransport(root)
        other = write_dir(tmp_path / f"r{index}", [b"a"])
        rec = (other / "rec-0001.json").read_text(encoding="utf-8")
        (other / "rec-0001.json").write_text(rec[:-1] + ", " + injected.replace('"calls": 1', '"case_id": "again"') + "}", encoding="utf-8")
        with pytest.raises(ReplayFixturesRefused):
            ReplayTransport(other)


def test_fixtures_that_are_not_labelled_synthetic_are_refused(tmp_path: Path) -> None:
    """AGENT-25."""
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(write_dir(tmp_path / "f", [b"a"], synthetic=False))


def test_two_recordings_of_one_request_are_refused(tmp_path: Path) -> None:
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(write_dir(tmp_path / "f", [b"a", b"a"]))


def test_a_missing_extra_or_mismatched_recording_file_is_refused(tmp_path: Path) -> None:
    root = write_dir(tmp_path / "missing", [b"a", b"b"])
    (root / "rec-0002.json").unlink()
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(root)
    extra = write_dir(tmp_path / "extra", [b"a"])
    (extra / "stray.json").write_text("{}", encoding="utf-8")
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(extra)
    other = write_dir(tmp_path / "other", [b"a"])
    doc = json.loads((other / "rec-0001.json").read_text(encoding="utf-8"))
    doc["recording_id"] = "rec-0009"
    (other / "rec-0001.json").write_text(json.dumps(doc), encoding="utf-8")
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(other)


def test_a_malformed_key_in_the_manifest_is_refused(tmp_path: Path) -> None:
    root = write_dir(tmp_path / "f", [b"a"])
    m = json.loads((root / "manifest.json").read_text(encoding="utf-8"))
    m["recordings"][0]["request_sha256"] = "sha256:abc"
    (root / "manifest.json").write_text(json.dumps(m), encoding="utf-8")
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(root)


def test_a_usable_row_without_a_reply_is_refused(tmp_path: Path) -> None:
    root = write_dir(tmp_path / "f", [b"a"])
    doc = json.loads((root / "rec-0001.json").read_text(encoding="utf-8"))
    doc["reply"] = None
    (root / "rec-0001.json").write_text(json.dumps(doc), encoding="utf-8")
    with pytest.raises(ReplayFixturesRefused):
        ReplayTransport(root)


def test_the_refusal_text_holds_a_code_and_a_file_name_never_content(tmp_path: Path) -> None:
    root = write_dir(tmp_path / "f", [b"a"])
    (root / "manifest.json").write_text('{"secret-looking-value": 1', encoding="utf-8")
    with pytest.raises(ReplayFixturesRefused) as e:
        ReplayTransport(root)
    assert "secret-looking-value" not in str(e.value)


# ---- the module's own boundary --------------------------------------------------------------------------------------------------------------------------------------


def test_the_module_imports_no_network_process_clock_or_random_module() -> None:
    """AGENT-14, INV-29: it can serve a recording and nothing else."""
    src = Path(__file__).parent.parent / "noema_model" / "providers" / "llm" / "replay.py"
    tree = ast.parse(src.read_text(encoding="utf-8"))
    names: set[str] = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            names |= {a.name.split(".")[0] for a in node.names}
        elif isinstance(node, ast.ImportFrom) and node.module:
            names.add(node.module.split(".")[0])
    forbidden = {"socket", "ssl", "urllib", "http", "subprocess", "asyncio", "random", "secrets", "time", "datetime", "os", "requests", "httpx"}
    assert not (names & forbidden), names & forbidden


# ---- as the transport of the real decision adapter: a choice question and a yes/no gate question -----------------------------------------------------------


class Spy:
    """Records the exact bytes the real adapter sends and answers once with a canned, valid reply (only to learn the bytes)."""

    def __init__(self, reply: dict[str, Any]) -> None:
        self.reply = reply
        self.seen: list[bytes] = []

    async def post(self, url: str, headers: Any, body: bytes, timeout_s: float, max_bytes: int) -> HttpResponse:
        self.seen.append(body)
        return HttpResponse(status=200, body=json.dumps(self.reply).encode())


SETTINGS = AdapterSettings(
    endpoint="https://replay.invalid", credential="replay-test-credential", api_version=None, timeout_ms=5000, max_response_bytes=1 << 20
)

CHOICE = NativeChoice(
    name="pick",
    instructions="Pick one.",
    choices=(("alpha", "first"), ("beta", "second"), ("none_or_other", "neither")),
)
GATE = NativePredicate(name="gate", instructions="Does the customer explicitly ask for something to be done for them?")
REQUEST = NativeRequest(model_version="model-alpha", input_text="synthetic phrasing", questions=(CHOICE, GATE))
DECISION_REPLY: dict[str, Any] = {
    "model": "model-alpha",
    "answers": [
        {
            "name": "pick",
            "type": "choice",
            "choice": "beta",
            "confidence": 0.61,
            "probabilities": [
                {"value": "alpha", "probability": 0.2},
                {"value": "beta", "probability": 0.61},
                {"value": "none_or_other", "probability": 0.19},
            ],
        },
        {"name": "gate", "type": "predicate", "probability": 0.8731},
    ],
    "usage": {"input_tokens": 10, "output_tokens": 3},
}


def decision_fixtures(tmp_path: Path) -> tuple[Path, bytes]:
    spy = Spy(DECISION_REPLY)
    run(OpenAIDecisions("decisions", SETTINGS, spy).decide(REQUEST))
    body = spy.seen[0]
    root = write_dir(tmp_path / "f", [body])
    doc = json.loads((root / "rec-0001.json").read_text(encoding="utf-8"))
    doc["reply"] = copy.deepcopy(DECISION_REPLY)
    (root / "rec-0001.json").write_text(json.dumps(doc), encoding="utf-8")
    return root, body


def test_the_real_decision_adapter_gets_a_choice_and_a_gate_answer_from_the_recording(tmp_path: Path) -> None:
    """A604: the replay serves the closed-vocabulary choice (with its probabilities, abstain included) and the yes/no gate."""
    root, _ = decision_fixtures(tmp_path)
    provider = OpenAIDecisions("decisions", SETTINGS, ReplayTransport(root))
    out = run(provider.decide(REQUEST))
    pick, gate = out.answers
    assert isinstance(pick, NativeChoiceAnswer) and pick.choice == "beta"
    assert dict(pick.probabilities) == {"alpha": 0.2, "beta": 0.61, "none_or_other": 0.19}
    assert isinstance(gate, NativePredicateAnswer) and gate.probability == 0.8731


def test_a_changed_question_is_a_miss_through_the_real_adapter(tmp_path: Path) -> None:
    root, _ = decision_fixtures(tmp_path)
    provider = OpenAIDecisions("decisions", SETTINGS, ReplayTransport(root))
    changed = NativeRequest(model_version="model-alpha", input_text="another phrasing", questions=(CHOICE, GATE))
    with pytest.raises(ReplayMiss):
        run(provider.decide(changed))


# ---- the committed recordings -----------------------------------------------------------------------------------------------------------------------------------------------------


def committed_dir() -> Path:
    dirs = sorted(p for p in FIXTURE_ROOT.iterdir() if p.is_dir())
    assert dirs, "no committed replay fixtures"
    return dirs[0]


def test_the_committed_recordings_load_and_only_the_usable_ones_are_served() -> None:
    """A261, A320: the 17 usable calls of run 44521380842 are served by their recorded key; the 8 failures are evidence only."""
    t = ReplayTransport(committed_dir())
    manifest = json.loads((committed_dir() / "manifest.json").read_text(encoding="utf-8"))
    usable = [r["request_sha256"] for r in manifest["recordings"] if r["status"] == 200 and r["failure"] is None]
    refused = [r["request_sha256"] for r in manifest["recordings"] if not (r["status"] == 200 and r["failure"] is None)]
    assert sorted(t.served_keys()) == sorted(usable) and len(usable) == 17
    assert sorted(t.evidence_only_keys()) == sorted(refused) and len(refused) == 8
    for key in usable:
        assert isinstance(json.loads(t.reply_for_key(key)), dict)
    for key in refused:
        with pytest.raises(ReplayMiss) as e:
            t.reply_for_key(key)
        assert e.value.code == "EVIDENCE_ONLY"
