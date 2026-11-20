"""The run loop of the compile acceptance run (UND-16): each golden example once, in order, through one compile function, with the budget guard in front.

A stop (the guard refused a request) ends the run and stays: what was not reached is NOT_RUN. A run that made no provider call is NOT RUN (exit 3), never a pass: a green result with nothing behind it
is the worst thing this run could print. Any exception that is not a `CompileFailed` is a bug and is not hidden as a failed compile."""
from collections.abc import Awaitable, Callable, Mapping, Sequence
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from benchmark.bakeoff.budget import BudgetExceeded, BudgetGuard
from benchmark.compile_acceptance.golden import Golden, check
from benchmark.compile_acceptance.record_only import RecordOnlyInput
from benchmark.compile_acceptance.recording import LABEL, Recorder, dump


class CompileFailed(Exception):
    """The compile service gave no usable proposal (no provider answered, or the schema gate refused it). The message is fixed text and never holds a reply or an utterance."""


@dataclass(frozen=True, slots=True)
class CaseResult:
    case_id: str
    status: str  # MET, MISSED, ERROR or NOT_RUN
    problems: tuple[str, ...] = ()


@dataclass(frozen=True, slots=True)
class RecordOnlyResult:
    case_id: str
    status: str  # RECORDED, ERROR or NOT_RUN
    detail: str = ""


@dataclass
class RunResult:
    cases: list[CaseResult] = field(default_factory=list)
    record_only: list[RecordOnlyResult] = field(default_factory=list)
    spent_micros: int = 0
    stopped: bool = False
    calls: int = 0

    @property
    def exit_code(self) -> int:
        if self.calls == 0:
            return 3
        if self.stopped or any(c.status != "MET" for c in self.cases):
            return 1
        return 0


async def run_cases(goldens: Sequence[Golden], compile_one: Callable[[Golden], Awaitable[Mapping[str, Any]]], guard: BudgetGuard, recorder: Recorder, *, calls: Callable[[], int],
                    record_only: Sequence[RecordOnlyInput] = (), record_one: Callable[[RecordOnlyInput], Awaitable[Mapping[str, Any]]] | None = None) -> RunResult:
    if record_only and record_one is None:
        raise ValueError("a record-only pass needs the function that runs it")
    result = RunResult()
    for g in goldens:
        if result.stopped or guard.stopped:
            result.stopped = True
            result.cases.append(CaseResult(g.case_id, "NOT_RUN", ("the budget stop was reached",)))
            continue
        recorder.case_id = g.case_id
        try:
            proposal = await compile_one(g)
        except BudgetExceeded:
            result.stopped = True
            result.cases.append(CaseResult(g.case_id, "NOT_RUN", ("the budget stop was reached",)))
            continue
        except CompileFailed as err:
            result.cases.append(CaseResult(g.case_id, "ERROR", (str(err),)))
        else:
            problems = tuple(check(g, proposal))
            result.cases.append(CaseResult(g.case_id, "MISSED" if problems else "MET", problems))
        if guard.stopped:
            result.stopped = True
    if record_one is not None:
        await _record_only_pass(record_only, record_one, guard, recorder, result)
    result.spent_micros, result.calls = guard.spent_micros, calls()
    return result


async def _record_only_pass(inputs: Sequence[RecordOnlyInput], record_one: Callable[[RecordOnlyInput], Awaitable[Mapping[str, Any]]], guard: BudgetGuard, recorder: Recorder, result: RunResult) -> None:
    """After every golden: each input once, in order. What the model answers is not read (INV-22): no check, no verdict. A failure is recorded and shown, never hidden, and changes neither the goldens nor the exit
    code; the budget stop is kept. An unexpected exception is recorded by its type only (its message can hold the utterance, AGENT-13) and the pass goes on."""
    for inp in inputs:
        if result.stopped or guard.stopped:
            result.stopped = True
            result.record_only.append(RecordOnlyResult(inp.case_id, "NOT_RUN", "the budget stop was reached"))
            continue
        recorder.case_id = inp.case_id
        try:
            await record_one(inp)
        except BudgetExceeded:
            result.stopped = True
            result.record_only.append(RecordOnlyResult(inp.case_id, "NOT_RUN", "the budget stop was reached"))
            continue
        except CompileFailed as err:
            result.record_only.append(RecordOnlyResult(inp.case_id, "ERROR", str(err)))
        except Exception as err:  # noqa: BLE001 - recorded by type, not hidden: the goldens' results are already in hand and must not be lost to it
            result.record_only.append(RecordOnlyResult(inp.case_id, "ERROR", f"unexpected: {type(err).__name__}"))
        else:
            result.record_only.append(RecordOnlyResult(inp.case_id, "RECORDED"))
        if guard.stopped:
            result.stopped = True


def write_run_record(out: Path, run_id: str, result: RunResult, limits: Mapping[str, int]) -> Path:
    """`run.json`: what was run and what came of it. It holds no utterance and no proposal, only ids, statuses and the problems' fixed texts. The record-only pass (A361, A372) is listed apart from the goldens."""
    out.mkdir(parents=True, exist_ok=True)
    doc = {
        "run_id": run_id, "label": LABEL, "exit_code": result.exit_code, "calls": result.calls, "spent_micros": result.spent_micros, "stopped": result.stopped, "limits": dict(limits),
        "cases": [{"case_id": c.case_id, "status": c.status, "problems": list(c.problems)} for c in result.cases],
        "record_only_count": len(result.record_only),
        "record_only": [{"case_id": r.case_id, "status": r.status, "detail": r.detail} for r in result.record_only],
    }
    path = out / "run.json"
    path.write_text(dump(doc), encoding="utf-8")
    return path
