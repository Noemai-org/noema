"""The run loop of the compile acceptance run (UND-16): each golden example once, in order, through one compile function, with the budget guard in front.

A stop (the guard refused a request) ends the run and stays: what was not reached is NOT_RUN. A run that made no provider call is NOT RUN (exit 3), never a pass: a green result with nothing behind it
is the worst thing this run could print. Any exception that is not a `CompileFailed` is a bug and is not hidden as a failed compile."""
from collections.abc import Awaitable, Callable, Mapping, Sequence
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from benchmark.bakeoff.budget import BudgetExceeded, BudgetGuard
from benchmark.compile_acceptance.golden import Golden, check
from benchmark.compile_acceptance.recording import LABEL, Recorder, dump


class CompileFailed(Exception):
    """The compile service gave no usable proposal (no provider answered, or the schema gate refused it). The message is fixed text and never holds a reply or an utterance."""


@dataclass(frozen=True, slots=True)
class CaseResult:
    case_id: str
    status: str  # MET, MISSED, ERROR or NOT_RUN
    problems: tuple[str, ...] = ()


@dataclass
class RunResult:
    cases: list[CaseResult] = field(default_factory=list)
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


async def run_cases(goldens: Sequence[Golden], compile_one: Callable[[Golden], Awaitable[Mapping[str, Any]]], guard: BudgetGuard, recorder: Recorder, *, calls: Callable[[], int]) -> RunResult:
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
    result.spent_micros, result.calls = guard.spent_micros, calls()
    return result


def write_run_record(out: Path, run_id: str, result: RunResult, limits: Mapping[str, int]) -> Path:
    """`run.json`: what was run and what came of it. It holds no utterance and no proposal, only ids, statuses and the problems' fixed texts."""
    out.mkdir(parents=True, exist_ok=True)
    doc = {
        "run_id": run_id, "label": LABEL, "exit_code": result.exit_code, "calls": result.calls, "spent_micros": result.spent_micros, "stopped": result.stopped, "limits": dict(limits),
        "cases": [{"case_id": c.case_id, "status": c.status, "problems": list(c.problems)} for c in result.cases],
    }
    path = out / "run.json"
    path.write_text(dump(doc), encoding="utf-8")
    return path
