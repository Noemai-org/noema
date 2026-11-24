"""The live side of the compile acceptance run: the model service's own compile service, built from the pack, the lexicon, the masking limits and one frozen harness configuration, with a recording
transport under the provider adapter and the budget guard in front of it (UND-16; REQ-INT-001, REQ-INT-002; INV-01, INV-02, INV-22, INV-29).

Nothing here talks to a provider except the model service's adapter, over `RecordingTransport` (INV-29): the key is read there, from the process environment, and never stored here. The model service's
package is imported when a backend is built, not at import time, as the bake-off does. The candidates are a SYNTHETIC in-memory source (the core is not involved); the identifiers of a proposal are derived
from the request (noema#580, A219). The dry transport sends nothing.

Two small pieces below repeat what `noema_model.understanding.compiler.wiring` holds (the pack view and the minter): that module is noema#635 and is not on `main` yet. When it is, they are replaced by an import
(debt, named in the PR)."""
import hashlib
import json
from collections.abc import Mapping
from datetime import UTC, datetime
from importlib import import_module
from pathlib import Path
from typing import Any

from benchmark.bakeoff.budget import BudgetExceeded, BudgetGuard, projected_micros
from benchmark.compile_acceptance.evidence import format_choice
from benchmark.compile_acceptance.golden import Golden, canonical_input
from benchmark.compile_acceptance.record_only import RecordOnlyInput
from benchmark.compile_acceptance.recording import Recorder
from benchmark.compile_acceptance.runner import CompileFailed

ROOT = Path(__file__).resolve().parents[2]
PACK_FILE = ROOT / "packs" / "fintech_v1.json"
LEXICON_DIR = ROOT / "packs" / "fintech" / "lexicons"  # the files the pack names in its `lexicon_refs` are read from here and merged (UND-16, run #6)
MASKING_LIMITS = ROOT / "packs" / "fintech" / "limits" / "masking_limits.json"
OUT_OF_SCOPE_NODE = "fin.meta.out_of_scope"
# Settings of this run, not safety thresholds: the pool is the number of provider calls that may be in flight at once; the three bounds are the compile service's own (noema#635 reads them from a file).
# The pipeline starts five calls together (run.py step 2) and a full pool refuses a call at once, so the pool is five plus one spare for a call whose caller timed out and whose thread still holds its
# worker. Four refused the injection screen in run 44521380842 (UND-16); test_pool.py reads the five from the pipeline.
POOL_SIZE = 6
COMPILE_TIMEOUT_S = 240.0
MAX_PROPOSAL_BYTES = 65_536
MAX_REQUEST_BYTES = 65_536
CANDIDATES_AT = datetime(2026, 11, 16, 8, 0, tzinfo=UTC)  # inside "this morning" of the examples' synthetic day
DRY_PLACEHOLDER = "dry-run-nothing-is-sent"


def _usage_micros(body: bytes, input_rate: int, output_rate: int) -> int | None:
    """The cost the reply's own usage gives, rounded up; None when the reply reports no usable usage (the guard then settles at the projection)."""
    try:
        usage = json.loads(body).get("usage")
        tokens_in, tokens_out = usage["input_tokens"], usage["output_tokens"]
    except (ValueError, AttributeError, KeyError, TypeError):
        return None
    for n in (tokens_in, tokens_out):
        if isinstance(n, bool) or not isinstance(n, int) or n < 0:
            return None
    return int(-(-(tokens_in * input_rate + tokens_out * output_rate) // 1_000_000))


class RecordingTransport:
    """The transport under the provider adapter: the guard decides before a request is sent, and each call is recorded. Headers are passed on and never kept."""

    def __init__(self, inner: Any, guard: BudgetGuard, recorder: Recorder, *, input_micro_per_mtok: int, output_micro_per_mtok: int, max_output_tokens: int) -> None:
        self._inner, self._guard, self._recorder = inner, guard, recorder
        self._in, self._out, self._max_out = input_micro_per_mtok, output_micro_per_mtok, max_output_tokens
        self.calls = 0

    async def post(self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int) -> Any:
        errors = import_module("noema_model.providers.llm.base")
        projected = projected_micros(len(body), self._max_out, self._in, self._out)
        try:
            self._guard.authorize(projected)
        except BudgetExceeded as err:
            raise errors.ProviderError(errors.ProviderFailure.UNAVAILABLE, local=True) from err
        try:
            reply = await self._inner.post(url, headers, body, timeout_s, max_bytes)
        except errors.ProviderError as err:
            self._guard.settle(None, projected)
            # A372: a refusal that came from this service's own full pool (`local`: nothing was sent) is its own label, not a provider outage. Accounting is unchanged (open question in the PR).
            label = "POOL_REFUSED" if err.local else err.kind.name
            self._recorder.record_call(url=url, request=body, status=None, reply=None, failure=label, projected_micros=projected, cost_micros=projected)
            self.calls += 1
            raise
        actual = _usage_micros(reply.body, self._in, self._out)
        self._guard.settle(actual, projected)
        self._recorder.record_call(url=url, request=body, status=reply.status, reply=reply.body, failure=None, projected_micros=projected, cost_micros=projected if actual is None else actual)
        self.calls += 1
        return reply


class DryTransport:
    """Prices the first request of each example and sends nothing; every call fails as an unavailable provider, locally, so the pipeline stops there."""

    def __init__(self, *, input_micro_per_mtok: int, output_micro_per_mtok: int, max_output_tokens: int) -> None:
        self._in, self._out, self._max_out = input_micro_per_mtok, output_micro_per_mtok, max_output_tokens
        self.case_id = ""
        self._first: dict[str, int] = {}

    async def post(self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int) -> Any:
        errors = import_module("noema_model.providers.llm.base")
        self._first.setdefault(self.case_id, projected_micros(len(body), self._max_out, self._in, self._out))
        raise errors.ProviderError(errors.ProviderFailure.UNAVAILABLE, local=True)

    def first_costs(self) -> list[tuple[str, int]]:
        return list(self._first.items())


class _Candidates:
    """The synthetic candidate source: what the core's candidate API would return for the example being run. The core is not involved."""

    def __init__(self) -> None:
        self.rows: tuple[Any, ...] = ()

    async def fetch(self, query: Any) -> tuple[Any, ...]:
        return self.rows


def _letters(*parts: str) -> str:
    digest = hashlib.sha256(b"".join(len(p.encode()).to_bytes(4, "big") + p.encode() for p in parts)).digest()
    number, out = int.from_bytes(digest, "big"), []
    for _ in range(26):
        number, rest = divmod(number, 26)
        out.append(chr(ord("a") + rest))
    return "".join(out)


class _Choices:
    """The closed-vocabulary choices the compiler reports (UND-17, A602), filed under the example whose correlation id they carry. A report for a correlation id no example used is dropped, never filed under
    another example. What is kept is the printed line: option ids and numbers only."""

    def __init__(self) -> None:
        self._case_of: dict[str, str] = {}
        self._lines: dict[str, list[str]] = {}

    def expect(self, correlation_id: str, case_id: str) -> None:
        self._case_of[correlation_id] = case_id
        self._lines.pop(case_id, None)

    def note(self, correlation_id: str, choices: Any) -> None:
        case_id = self._case_of.get(correlation_id)
        if case_id is not None:
            self._lines[case_id] = [format_choice(case_id, c.slot, c.chosen, c.probabilities, c.threshold) for c in choices]

    def lines(self, case_id: str) -> list[str]:
        return list(self._lines.get(case_id, []))


class LiveBackend:
    def __init__(self, service: Any, pack_ref: str, candidates: _Candidates, transport: Any, recorder: Recorder, pin: dict[str, Any], dry: bool, choices: _Choices | None = None) -> None:
        self._service, self._pack_ref, self._candidates, self._transport, self._recorder, self._pin, self._dry = service, pack_ref, candidates, transport, recorder, pin, dry
        self._choices = choices if choices is not None else _Choices()

    async def compile_one(self, g: Golden) -> Mapping[str, Any]:
        types = import_module("noema_model.understanding.resolution")
        rows = tuple(types.Candidate(f"c{n}", f"fin.transaction:TX{chr(ord('A') + n)}", amount, "NGN", CANDIDATES_AT, who, "0001", "OUTGOING", True) for n, (who, amount) in enumerate(g.candidates))
        return await self._compile(g.case_id, canonical_input(g), rows)

    async def record_one(self, inp: RecordOnlyInput) -> Mapping[str, Any]:
        """One input of the record-only pass (A361, A372, A439, A460): the same service and recorder, the canonical input the core's Interpreter port built (the fixture's), and the candidates of the case's world
        rows (BO's file), newest first, so the candidate source answers what the core's would. Nothing is invented for it."""
        types = import_module("noema_model.understanding.resolution")
        rows = tuple(types.Candidate(r["candidate_id"], r["resource_ref"], r["amount_minor"], r["currency"], datetime.strptime(r["occurred_at"], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=UTC),
                                     r["counterparty_display"], r["account_last4"], r["direction"], r["beneficiary_alias_match"]) for r in inp.candidates)
        return await self._compile(inp.case_id, inp.canonical, rows)

    async def _compile(self, case_id: str, canonical: Mapping[str, Any], rows: tuple[Any, ...]) -> Mapping[str, Any]:
        self._transport.case_id = case_id
        self._recorder.case_id = case_id
        self._choices.expect(str(canonical.get("correlation_id", "")), case_id)
        self._candidates.rows = rows
        compiler = import_module("noema_model.understanding.compiler.service")
        try:
            return dict(await self._service.run(dict(canonical), self._pack_ref, None))
        except compiler.InputInvalid as err:
            raise CompileFailed(f"the compile service gave no usable proposal (InputInvalid:{err.reason.value})") from None  # the class, from a closed set: never a piece of the request
        except compiler.ProviderUnavailable as err:
            raise CompileFailed(f"the compile service gave no usable proposal ({type(err).__name__})") from None

    def choice_lines(self, case_id: str) -> list[str]:
        """The choice lines of an example: what the model chose for each closed-vocabulary slot, with the probabilities behind it (A602). Empty when the turn stopped before any choice."""
        return self._choices.lines(case_id)

    def calls(self) -> int:
        return 0 if self._dry else int(self._transport.calls)

    def pin(self) -> dict[str, Any]:
        return dict(self._pin)

    def dry_costs(self) -> list[tuple[str, int]]:
        return list(self._transport.first_costs()) if self._dry else []


def build_backend(dry: bool, environ: Mapping[str, str], guard: BudgetGuard, recorder: Recorder, args: Any, inner: Any = None) -> LiveBackend:
    """The compile service on the real harness for the configuration file `args.harness_config` names. Raises the model service's ConfigError for anything wrong (a missing key included)."""
    harness = import_module("noema_model.harness")
    config_mod = import_module("noema_model.platform.config")
    clock = import_module("noema_model.platform.clock")
    telemetry = import_module("noema_model.platform.telemetry")
    masking = import_module("noema_model.platform.masking")
    pipeline_compiler = import_module("noema_model.understanding.compiler.compiler")
    service_mod = import_module("noema_model.understanding.compiler.service")
    pipeline = import_module("noema_model.understanding.pipeline")
    gate = import_module("noema_model.understanding.gate")
    assemble = import_module("noema_model.understanding.compiler.assemble")
    lexicon_mod = import_module("noema_model.understanding.compiler.lexicon")
    packconfig = import_module("noema_model.understanding.compiler.packconfig")
    wire = import_module("noema_model.providers.llm.transport")

    config_path = (ROOT / args.harness_config) if not Path(args.harness_config).is_absolute() else Path(args.harness_config)
    raw = json.loads(config_path.read_text(encoding="utf-8"))
    loaded = harness.load_harness_config(raw)
    (name, provider), = loaded.providers.items()
    if provider.rate is None or provider.max_output_tokens is None:
        raise config_mod.ConfigError("the harness configuration needs a rate card and max_output_tokens for a budgeted run")
    rates = {"input_micro_per_mtok": provider.rate.input_micro_per_mtok, "output_micro_per_mtok": provider.rate.output_micro_per_mtok, "max_output_tokens": provider.max_output_tokens}
    if dry:
        wire_transport: Any = DryTransport(**rates)
        provider_env = {provider_env_name(raw, name): DRY_PLACEHOLDER}
    else:
        wire_transport = RecordingTransport(inner if inner is not None else wire.UrllibTransport(pool_size=POOL_SIZE), guard, recorder, **rates)
        provider_env = dict(environ)
    providers = harness.build_providers(loaded, wire_transport, provider_env)
    the_harness = harness.Harness(loaded, providers, clock.SystemClock(), telemetry.NullTelemetry(), masking=masking.load_limits_file(MASKING_LIMITS))

    pack = json.loads(PACK_FILE.read_text(encoding="utf-8"))
    pack_ref = f"{pack['pack_id']}@{pack['pack_version']}"
    if pack_ref != args.pack_ref:
        raise config_mod.ConfigError("the pack file is not the pack reference asked for")
    catalogue = pipeline.NodeCatalogue.from_pack(pack, out_of_scope_node=OUT_OF_SCOPE_NODE)
    nodes = {node_id: gate.Node(slots={s.name: s.slot_type for s in info.slots}, outcomes=frozenset()) for node_id, info in catalogue.nodes.items()}
    candidates = _Candidates()
    choices = _Choices()
    lexicon = lexicon_mod.load_pack_lexicons(pack, LEXICON_DIR)
    the_compiler = pipeline_compiler.PipelineCompiler({pack_ref: packconfig.load_pipeline_config(pack, PACK_FILE.parent, out_of_scope_node=OUT_OF_SCOPE_NODE)}, lexicon,
                                              the_harness, candidates, _Minter(assemble.Identifiers, gate.UNSET), diagnostics=choices.note)
    service = service_mod.CompileService(the_compiler, _Packs(pack_ref, pack["pack_id"], pack["pack_version"], nodes), max_proposal_bytes=MAX_PROPOSAL_BYTES, timeout_s=COMPILE_TIMEOUT_S, max_request_bytes=MAX_REQUEST_BYTES)
    pin = {
        "pinned_model_id": provider.model_version, "reasoning_effort": raw["providers"][name].get("reasoning_effort"), "max_output_tokens": provider.max_output_tokens,
        "rate_card": {"input_micro_per_mtok": rates["input_micro_per_mtok"], "output_micro_per_mtok": rates["output_micro_per_mtok"], "as_of": raw["providers"][name]["rate"]["as_of"]},
        "harness_config_sha256": hashlib.sha256(config_path.read_bytes()).hexdigest(), "pack_ref": pack_ref, "pack_sha256": hashlib.sha256(PACK_FILE.read_bytes()).hexdigest(),
        "lexicon_sha256": lexicon.digest, "masking_limits_sha256": hashlib.sha256(MASKING_LIMITS.read_bytes()).hexdigest(),
    }
    return LiveBackend(service, pack_ref, candidates, wire_transport, recorder, pin, dry, choices)


def provider_env_name(raw: Mapping[str, Any], name: str) -> str:
    return str(raw["providers"][name]["credential_env"])


class _View:
    def __init__(self, pack_id: str, version: str, nodes: Mapping[str, Any]) -> None:
        self._ref, self._nodes = (pack_id, version), dict(nodes)

    def ref(self) -> tuple[str, str]:
        return self._ref

    def node(self, node_id: str) -> Any:
        return self._nodes.get(node_id)


class _Packs:
    def __init__(self, pack_ref: str, pack_id: str, version: str, nodes: Mapping[str, Any]) -> None:
        self._pack_ref, self._view = pack_ref, _View(pack_id, version, nodes)

    def view(self, pack_ref: str) -> Any:
        return self._view if pack_ref == self._pack_ref else None


class _Minter:
    """The identifiers nobody owns yet, as noema#580 / A219 settle them: derived from the request, no clock, no randomness; `raw_ref` is the neutral marker the core overwrites."""

    def __init__(self, identifiers: Any, unset: Any) -> None:
        self._identifiers, self._unset = identifiers, unset

    def identifiers(self, canonical_input: Mapping[str, Any]) -> Any:
        key = (str(canonical_input["tenant_id"]), str(canonical_input["correlation_id"]), str(canonical_input["request_id"]))
        return self._identifiers(intent_id="int_" + _letters("intent", *key), version=1, supersedes=None, created_at=str(canonical_input["received_at"]), raw_ref=self._unset)

    def candidate_set_id(self, canonical_input: Mapping[str, Any]) -> str:
        return "cns_" + _letters("candidate-set", str(canonical_input["tenant_id"]), str(canonical_input["correlation_id"]), str(canonical_input["request_id"]))
