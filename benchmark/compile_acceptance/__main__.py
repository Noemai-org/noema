"""python -m benchmark.compile_acceptance [--dry-run] --pack-ref fin@1.0.0 --provider openai --hard-stop-usd N --prior-spent-micros M --out DIR --run-id ID  (UND-16; noema#546; A198)

It is started only by the `bench-live-compile` workflow. The key is the process environment's `OPENAI_API_KEY`; nothing here reads it except the model service's provider adapter (INV-29), prints it or writes it.
Exit codes: 0 every example met its check; 1 a miss, an error or the stop; 2 a bad input; 3 NOT RUN (no key, or no provider call was made: that is never a pass).
`--dry-run` calls no provider and needs no key: it prints what the first request of each example would cost and the ceiling, so the reviewer sees the cost before approving the paid job. It makes no provider call, so it always exits 3 (A285): a script that gates on the exit code cannot read an unrun acceptance as a pass."""
import argparse
import asyncio
import re
import sys
from collections.abc import Callable, Mapping
from datetime import UTC, datetime
from pathlib import Path
from typing import Any, Protocol

from benchmark.bakeoff.budget import BudgetGuard
from benchmark.compile_acceptance.golden import GOLDEN, Golden
from benchmark.compile_acceptance.record_only import DEFAULT_PATH, RecordOnlyInput, RecordOnlyRefused, load_inputs
from benchmark.compile_acceptance.recording import LABEL, Recorder
from benchmark.compile_acceptance.runner import CompileFailed, RunResult, run_cases, write_run_record

KEY_VARIABLE = "OPENAI_API_KEY"
_PACK_REF = re.compile(r"[a-z][a-z0-9_]*@[0-9]+\.[0-9]+\.[0-9]+")
_RUN_ID = re.compile(r"[A-Za-z0-9_.-]{1,64}")
MICRO = 1_000_000
NOT_RUN = 3  # the documented exit for "no key, or no provider call was made" (A285)


class Backend(Protocol):
    async def compile_one(self, g: Golden) -> Mapping[str, Any]: ...
    async def record_one(self, inp: RecordOnlyInput) -> Mapping[str, Any]: ...
    def calls(self) -> int: ...
    def pin(self) -> dict[str, Any]: ...
    def dry_costs(self) -> list[tuple[str, int]]: ...


Factory = Callable[[bool, Mapping[str, str], BudgetGuard, Recorder, Any], Backend]


def _pattern(rx: re.Pattern[str]) -> Callable[[str], str]:
    def check(value: str) -> str:
        if rx.fullmatch(value) is None:
            raise argparse.ArgumentTypeError("not in the accepted form")
        return value
    return check


def _prior(value: str) -> int:
    if not value.isascii() or not value.isdigit():
        raise argparse.ArgumentTypeError("must be a non-negative integer")
    return int(value)


def parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(prog="python -m benchmark.compile_acceptance", description=__doc__)
    p.add_argument("--dry-run", action="store_true", help="price the first request of each example; call no provider")
    p.add_argument("--pack-ref", required=True, type=_pattern(_PACK_REF))
    p.add_argument("--provider", required=True, choices=("openai",))
    p.add_argument("--hard-stop-usd", required=True, type=int, choices=(1, 2, 3, 4, 5), help="whole dollars, counted with the prior spending")
    p.add_argument("--prior-spent-micros", required=True, type=_prior, help="spending already made against the same cap by earlier runs")
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--run-id", required=True, type=_pattern(_RUN_ID))
    p.add_argument("--record-only-inputs", type=Path, default=DEFAULT_PATH, help="BO's g1_inputs.json: five inputs recorded after the goldens, with no expectation (A361, A372); g1_canonical_inputs.json (the Interpreter port's canonical inputs, pinned to it) must sit beside it (A460)")
    p.add_argument("--skip-record-only", action="store_true", help="run the goldens only (tests and the dry run of the goldens)")
    p.add_argument("--harness-config", default="benchmark/decisions/harness-gpt-6-luna.json", help="the frozen harness configuration whose single provider is asked (the model, its rate card and its token ceiling are the file's)")
    return p


def default_factory(dry: bool, environ: Mapping[str, str], guard: BudgetGuard, recorder: Recorder, args: Any) -> Backend:
    from benchmark.compile_acceptance.build import build_backend
    return build_backend(dry, environ, guard, recorder, args)


def main(argv: list[str], environ: Mapping[str, str], factory: Factory | None = None) -> int:
    args = parser().parse_args(argv)
    if "benchmark-heldout" in " ".join(argv) or any("benchmark-heldout" in v for v in environ.values()):
        sys.stderr.write("refused: nothing here reads benchmark-heldout\n")
        return 2
    inputs: tuple[RecordOnlyInput, ...] = ()
    if not args.skip_record_only:
        try:
            inputs = load_inputs(args.record_only_inputs)
        except RecordOnlyRefused as err:  # before anything is built or written: a run whose inputs are not proven does not start (fail closed)
            sys.stderr.write(f"refused: the record-only inputs file is not usable ({err.code})\n")
            return 2
    if not args.dry_run and not environ.get(KEY_VARIABLE, "").strip():
        sys.stderr.write(f"not run: no access: {KEY_VARIABLE} is not set in the environment\n")
        return NOT_RUN
    limit = args.hard_stop_usd * MICRO
    guard = BudgetGuard(limit_micros=limit, spent_micros=args.prior_spent_micros)
    recorder = Recorder(args.out, args.run_id, args.provider, "unpinned", lambda: datetime.now(UTC))
    backend = (factory or default_factory)(args.dry_run, environ, guard, recorder, args)
    recorder.model_id = str(backend.pin().get("pinned_model_id", "unpinned"))
    if args.dry_run:
        return _dry(backend, limit, args.prior_spent_micros, inputs)
    result = asyncio.run(run_cases(GOLDEN, backend.compile_one, guard, recorder, calls=backend.calls, record_only=inputs, record_one=backend.record_one))
    recorder.write_manifest(backend.pin())
    write_run_record(args.out, args.run_id, result, {"hard_stop_micros": limit, "prior_spent_micros": args.prior_spent_micros})
    lines = [LABEL, *(f"{c.case_id} {c.status}" + (f": {'; '.join(c.problems)}" if c.problems else "") for c in result.cases),
             *record_only_lines(result), f"exit code {result.exit_code}; provider calls {result.calls}; stopped {result.stopped}", f"spent_micros_total={result.spent_micros}"]
    sys.stdout.write("\n".join(lines) + "\n")
    return result.exit_code


def record_only_lines(result: RunResult) -> list[str]:
    """The pass is named apart from the goldens, and says it is recorded, not judged (A361, A372)."""
    if not result.record_only:
        return []
    return [f"record-only: {len(result.record_only)} (recorded, not judged: no expectation)", *(f"{r.case_id} {r.status}" + (f": {r.detail}" if r.detail else "") for r in result.record_only)]


def _dry(backend: Backend, limit: int, prior: int, inputs: tuple[RecordOnlyInput, ...] = ()) -> int:
    async def go() -> None:
        for g in GOLDEN:
            try:
                await backend.compile_one(g)
            except CompileFailed:
                pass
        for inp in inputs:
            try:
                await backend.record_one(inp)
            except CompileFailed:
                pass
    asyncio.run(go())
    lines = ["dry run: no provider was called and no key was read (SYNTHETIC inputs)", "worst-case cost of the FIRST request of each example, in micro-dollars (later requests of an example are priced when they are made):"]
    lines += [f"  {case_id}: {micros}" for case_id, micros in backend.dry_costs()]
    lines += [f"hard stop: {limit} micro-dollars, prior spent {prior}, remaining {max(limit - prior, 0)}", "A request that would pass the stop is refused before it is sent, and the stop stays."]
    sys.stdout.write("\n".join(lines) + "\n")
    return NOT_RUN


if __name__ == "__main__":
    import os
    sys.exit(main(sys.argv[1:], os.environ))
