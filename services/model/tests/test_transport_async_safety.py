"""A blocking provider or evidence call never runs on the event loop, and a full pool fails fast (UND-12).

REQ-TST-010 (async safety), REQ-NFR-002 (fail closed); INV-16, INV-29, AGENT-13, AGENT-16. Real sockets: a tiny loopback
server plays the target and holds, drips or answers on request. The transports are the real code.
"""

import asyncio
import re
import threading
import time
from collections.abc import Callable, Iterator, Mapping
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

import pytest

from noema_model.evidence import transport as evidence_transport
from noema_model.evidence.transport import UrllibEvidenceTransport
from noema_model.platform.config import ConfigError
from noema_model.providers.llm import transport as llm_transport
from noema_model.providers.llm.base import ProviderError, ProviderFailure
from noema_model.providers.llm.transport import UrllibTransport
from noema_model.understanding.pipeline.types import CandidateSourceError
from tests.support import run

SECRET = "Bearer sk-synthetic-not-a-real-key"
LOOP_LAG_BOUND_S = 0.25  # a loop blocked by one call would stall for the whole call timeout (0.4 s below)


class Target:
    """A loopback target. mode "hold": answer only when released. "drip": send the body slowly, a byte at a time, so each
    socket read succeeds while the whole call outlasts the caller's timeout. "ok": answer at once."""

    def __init__(self) -> None:
        self.mode = "ok"
        self.started = 0
        self.release = threading.Event()
        self._lock = threading.Lock()
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:  # noqa: N802
                self.rfile.read(int(self.headers.get("content-length", 0)))
                with outer._lock:
                    outer.started += 1
                if outer.mode == "hold":
                    outer.release.wait(timeout=20)
                body = b"0123456789"
                self.send_response(200)
                self.send_header("content-length", str(len(body)))
                self.end_headers()
                if outer.mode == "drip":
                    for byte in body:
                        self.wfile.write(bytes([byte]))
                        self.wfile.flush()
                        time.sleep(0.1)
                else:
                    self.wfile.write(body)

            def log_message(self, *args: Any) -> None:
                return None

        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self.httpd.server_address[1]}/"
        self._thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        self._thread.start()

    def close(self) -> None:
        self.release.set()
        self.httpd.shutdown()
        self.httpd.server_close()


@pytest.fixture
def target() -> Iterator[Target]:
    t = Target()
    yield t
    t.close()


@dataclass(frozen=True)
class Kit:
    name: str
    make: Callable[[int], Any]
    saturated: Callable[[BaseException], bool]
    timed_out: Callable[[BaseException], bool]


def _llm_is(kind: ProviderFailure) -> Callable[[BaseException], bool]:
    return lambda e: isinstance(e, ProviderError) and e.kind is kind


def _llm_saturated(e: BaseException) -> bool:
    # UNAVAILABLE, and marked local: no provider was contacted, so the harness must not charge a breaker for it.
    return isinstance(e, ProviderError) and e.kind is ProviderFailure.UNAVAILABLE and e.local


def _evidence_is(e: BaseException) -> bool:
    return isinstance(e, CandidateSourceError)


KITS = [
    Kit("llm", lambda n: UrllibTransport(pool_size=n, allow_loopback_http=True), _llm_saturated, _llm_is(ProviderFailure.TIMEOUT)),
    Kit("evidence", lambda n: UrllibEvidenceTransport(None, pool_size=n, allow_loopback_http=True), _evidence_is, _evidence_is),
]
BY_NAME = {k.name: k for k in KITS}
EVERY_KIT = pytest.mark.parametrize("kit", KITS, ids=lambda k: k.name)


def call(transport: Any, url: str, timeout_s: float) -> Any:
    return transport.post(url, {"authorization": SECRET}, b"{}", timeout_s, 1024)


async def settle_until(predicate: Callable[[], bool], limit_s: float = 5.0) -> None:
    deadline = time.monotonic() + limit_s
    while not predicate():
        if time.monotonic() > deadline:
            raise AssertionError("the condition did not hold in time")
        await asyncio.sleep(0.01)


# --- 1. the blocking call is never on the event loop -----------------------------------------------------------------


@EVERY_KIT
def test_the_blocking_call_never_runs_on_the_event_loop_thread(kit: Kit, target: Target) -> None:
    """REQ-TST-010: the thread that runs _post_blocking is never the thread of the loop that awaits the call."""
    seen: list[int] = []
    base = type(kit.make(1))

    class Spy(base):  # type: ignore[valid-type, misc]
        def _post_blocking(self, *args: Any) -> Any:
            seen.append(threading.get_ident())
            return super()._post_blocking(*args)

    transport = Spy(pool_size=1, allow_loopback_http=True) if kit.name == "llm" else Spy(None, pool_size=1, allow_loopback_http=True)

    async def scenario() -> int:
        response = await call(transport, target.url, 5.0)
        assert response.status == 200
        return threading.get_ident()

    loop_thread = run(scenario())
    assert seen and all(t != loop_thread for t in seen)


@EVERY_KIT
def test_the_loop_keeps_ticking_while_calls_are_held(kit: Kit, target: Target) -> None:
    """The loop's lag stays far below the 0.4 s call timeout while four calls are held; each ends in a timeout failure."""
    target.mode = "hold"
    transport = kit.make(4)

    async def scenario() -> tuple[float, list[BaseException]]:
        stop = asyncio.Event()
        worst = 0.0

        async def ticker() -> None:
            nonlocal worst
            last = time.monotonic()
            while not stop.is_set():
                await asyncio.sleep(0.01)
                now = time.monotonic()
                worst = max(worst, now - last - 0.01)
                last = now

        tick = asyncio.create_task(ticker())
        results = await asyncio.gather(*(call(transport, target.url, 0.4) for _ in range(4)), return_exceptions=True)
        stop.set()
        await tick
        return worst, [r for r in results if isinstance(r, BaseException)]

    worst, errors = run(scenario())
    assert len(errors) == 4 and all(kit.timed_out(e) for e in errors)
    assert worst < LOOP_LAG_BOUND_S, f"the loop stalled for {worst:.3f} s"


# --- 2. a full pool fails fast and never queues ----------------------------------------------------------------------


@EVERY_KIT
def test_a_full_pool_refuses_at_once_with_a_typed_failure_and_sends_nothing(kit: Kit, target: Target) -> None:
    """REQ-NFR-002: with every worker busy the next call fails at once; it is never queued, never sent."""
    target.mode = "hold"
    transport = kit.make(2)

    async def scenario() -> tuple[BaseException | None, float, int, list[Any]]:
        busy = [asyncio.ensure_future(call(transport, target.url, 10.0)) for _ in range(2)]
        await settle_until(lambda: target.started == 2)
        began = time.monotonic()
        refused: BaseException | None = None
        try:
            await call(transport, target.url, 10.0)
        except BaseException as exc:  # noqa: BLE001  (the test inspects whichever typed failure it is)
            refused = exc
        elapsed = time.monotonic() - began
        await asyncio.sleep(0.1)
        sent = target.started
        target.release.set()
        done = await asyncio.gather(*busy)
        return refused, elapsed, sent, done

    refused, elapsed, sent, done = run(scenario())
    assert refused is not None and kit.saturated(refused), f"got {refused!r}"
    assert elapsed < 0.5, f"the refusal took {elapsed:.3f} s: the call waited for a slot"
    assert sent == 2, "the refused call reached the target"
    assert [d.status for d in done] == [200, 200]


@EVERY_KIT
def test_a_slot_is_free_again_when_the_held_calls_finish(kit: Kit, target: Target) -> None:
    target.mode = "hold"
    transport = kit.make(1)

    async def scenario() -> int:
        first = asyncio.ensure_future(call(transport, target.url, 10.0))
        await settle_until(lambda: target.started == 1)
        target.release.set()
        await first
        target.mode = "ok"
        return int((await call(transport, target.url, 5.0)).status)

    assert run(scenario()) == 200


@EVERY_KIT
def test_a_timed_out_call_keeps_its_slot_until_its_thread_returns(kit: Kit, target: Target) -> None:
    """A thread cannot be cancelled. The caller's timeout fires while the thread still reads a slow body, so the slot stays
    taken and a new call is refused honestly; once the thread returns the slot is free."""
    target.mode = "drip"  # a byte every 0.1 s for 1 s: every socket read succeeds, the call outlasts a 0.3 s timeout
    transport = kit.make(1)

    async def scenario() -> tuple[BaseException | None, BaseException | None, int]:
        first: BaseException | None = None
        try:
            await call(transport, target.url, 0.3)
        except BaseException as exc:  # noqa: BLE001
            first = exc
        second: BaseException | None = None
        try:
            await call(transport, target.url, 0.3)
        except BaseException as exc:  # noqa: BLE001
            second = exc
        await asyncio.sleep(1.2)  # the dripping thread finishes
        target.mode = "ok"
        return first, second, int((await call(transport, target.url, 5.0)).status)

    first, second, last = run(scenario())
    assert first is not None and kit.timed_out(first)
    assert second is not None and kit.saturated(second)
    assert target.started == 2, "the call refused for a full pool was sent"
    assert last == 200


@EVERY_KIT
def test_a_slot_is_released_when_the_blocking_call_raises(kit: Kit, target: Target) -> None:
    transport = kit.make(1)
    dead = Target()
    dead.close()  # nothing listens on its port: connection refused, an exception in the worker thread

    async def scenario() -> tuple[BaseException | None, int]:
        failure: BaseException | None = None
        try:
            await call(transport, dead.url, 2.0)
        except BaseException as exc:  # noqa: BLE001
            failure = exc
        return failure, int((await call(transport, target.url, 5.0)).status)

    failure, status = run(scenario())
    assert failure is not None  # the refused connection is an error in the worker thread
    assert status == 200  # a leaked slot would have refused this call


@EVERY_KIT
def test_the_refusal_carries_no_header_body_or_credential(kit: Kit, target: Target) -> None:
    target.mode = "hold"
    transport = kit.make(1)

    async def scenario() -> BaseException:
        busy = asyncio.ensure_future(call(transport, target.url, 10.0))
        await settle_until(lambda: target.started == 1)
        try:
            await call(transport, target.url, 10.0)
        except BaseException as exc:  # noqa: BLE001
            target.release.set()
            await busy
            return exc
        raise AssertionError("the call was accepted")

    exc = run(scenario())
    text = f"{exc} {exc!r} {exc.__cause__!r} {exc.__context__!r}"
    assert "sk-synthetic" not in text and "Bearer" not in text and "127.0.0.1" not in text


# --- 3. no shared default executor, no unbounded queue ---------------------------------------------------------------


@pytest.mark.parametrize("module", [llm_transport, evidence_transport], ids=["llm", "evidence"])
def test_neither_transport_uses_the_shared_default_executor(module: Any) -> None:
    source = Path(module.__file__).read_text()
    code = "\n".join(line.split("#", 1)[0] for line in source.splitlines())
    assert "to_thread" not in code
    assert not re.search(r"run_in_executor\(\s*None", code)


# --- 4. the pool size is configuration, never a default in code ------------------------------------------------------


@pytest.mark.parametrize(
    "factory",
    # The missing argument is the point of the test, so the type checker is told to look away.
    [lambda: UrllibTransport(), lambda: UrllibEvidenceTransport(None)],  # type: ignore[call-arg]
    ids=["llm", "evidence"],
)
def test_a_transport_cannot_be_built_without_a_pool_size(factory: Callable[[], Any]) -> None:
    with pytest.raises(TypeError):
        factory()


@pytest.mark.parametrize("size", [0, -1, True, 1.5, "8"])
@EVERY_KIT
def test_a_pool_size_below_one_or_not_an_integer_is_refused(kit: Kit, size: Any) -> None:
    with pytest.raises(ConfigError):
        kit.make(size)


@pytest.mark.parametrize(
    ("environ", "ok"),
    [({"X": "16"}, 16), ({"X": "1"}, 1), ({}, None), ({"X": ""}, None), ({"X": "0"}, None), ({"X": "-3"}, None), ({"X": "2.5"}, None),
     ({"X": "abc"}, None), ({"X": " 8"}, None), ({"X": "０８"}, None)],
)
def test_the_pool_size_is_read_strictly_from_the_environment(environ: Mapping[str, str], ok: int | None) -> None:
    from noema_model.platform.config import require_env_int  # imported here: the red run must fail on this test, not on collection

    if ok is None:
        with pytest.raises(ConfigError) as info:
            require_env_int(environ, "X", minimum=1)
        assert "X" in str(info.value) and "abc" not in str(info.value)
    else:
        assert require_env_int(environ, "X", minimum=1) == ok


def test_the_service_refuses_to_build_its_transport_without_a_pool_size(tmp_path: Path) -> None:
    from noema_model.harness import assemble_harness
    from noema_model.harness.assemble import ENV_CONFIG, ENV_LLM_POOL
    from noema_model.platform.telemetry import InMemoryTelemetry
    from tests.support import ManualClock, raw_config
    import json

    path = tmp_path / "harness.json"
    path.write_text(json.dumps(raw_config(providers=("alpha",))))
    telemetry = InMemoryTelemetry()
    result = assemble_harness({ENV_CONFIG: str(path)}, ManualClock(), telemetry)
    assert result.harness is None and ENV_LLM_POOL in result.reason
    assert [e[0] for e in telemetry.events] == ["HARNESS_CONFIG_REJECTED"]


def test_the_pool_size_in_effect_is_written_to_the_start_up_log(tmp_path: Path) -> None:
    from noema_model.harness import assemble_harness
    from noema_model.harness.assemble import ENV_CONFIG, ENV_LLM_POOL
    from noema_model.platform.telemetry import InMemoryTelemetry
    from tests.support import ManualClock, raw_config
    import json

    path = tmp_path / "harness.json"
    path.write_text(json.dumps(raw_config(providers=("alpha",))))
    telemetry = InMemoryTelemetry()
    result = assemble_harness({ENV_CONFIG: str(path), ENV_LLM_POOL: "3"}, ManualClock(), telemetry)
    assert result.harness is not None and result.reason == ""
    assert ("TRANSPORT_POOL_CONFIGURED", "service-start", {"transport": "llm", "workers": 3}) in telemetry.events


def test_the_evidence_transport_reads_its_pool_size_from_its_own_variable() -> None:
    assert evidence_transport.ENV_EVIDENCE_POOL == "NOEMA_MODEL_EVIDENCE_POOL_SIZE"
    assert evidence_transport.evidence_pool_size({evidence_transport.ENV_EVIDENCE_POOL: "8"}) == 8
    with pytest.raises(ConfigError):
        evidence_transport.evidence_pool_size({})


def test_the_two_transports_have_separate_pools(target: Target) -> None:
    """One full pool does not refuse the other transport's calls."""
    target.mode = "hold"
    llm, evidence = BY_NAME["llm"].make(1), BY_NAME["evidence"].make(1)

    async def scenario() -> int:
        busy = asyncio.ensure_future(call(llm, target.url, 10.0))
        await settle_until(lambda: target.started == 1)
        other = asyncio.ensure_future(call(evidence, target.url, 10.0))
        await settle_until(lambda: target.started == 2)
        target.release.set()
        return len(await asyncio.gather(busy, other))

    assert run(scenario()) == 2
