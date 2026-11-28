"""A772 item 4 (interim; amends A605; UND-16; INV-22, REQ-TST-006, AGENT-13, AGENT-16): the labelled probe of the `requested_remedy` gate.

BO's 28 labelled phrasings (`remedy_labelled.v1.json`: SYNTHETIC, a development set, neither the five goldens nor the held-out set) are each run ONCE through the compile service, and the gate's decision for each
is read from the A602 line the compiler prints for it (`slot=requested_remedy_gate`: the answer the model selected, the probability of yes and the pack's threshold). The gate asserted a phrasing when the model
answered yes and the probability of yes is at least the threshold. The result is scored against the label and reported in four groups, apart: a FALSE ASSERTION (a phrasing that is not an explicit ask, asserted), a FALSE
NEGATIVE (an explicit ask that was not asserted), and the same two for the items labelled borderline. Run 9 is allowed only if no phrasing among the 14 non-explicit ones is asserted, borderline ones included.

The probe decides nothing and sets no threshold. It prints ids, counts and numbers only: never a phrasing (AGENT-13). The text of a phrasing travels only in the request to the model, as a golden's does; it is
not copied into any prompt, and the probe reads no expectation from the compiler's proposal (the proposal is not looked at)."""
import json
import re
from collections.abc import Awaitable, Callable, Mapping, Sequence
from dataclasses import dataclass, field
from datetime import UTC, datetime
from fractions import Fraction
from pathlib import Path
from typing import Any

from benchmark.bakeoff.budget import BudgetExceeded, BudgetGuard
from benchmark.compile_acceptance.golden import RECEIVED_AT
from benchmark.compile_acceptance.recording import LABEL, Recorder, dump
from benchmark.compile_acceptance.remedy_labelled import Item, load
from benchmark.compile_acceptance.runner import CompileFailed

LABELLED_PATH = Path(__file__).resolve().parent / "remedy_labelled.v1.json"
GATE_SLOT = "requested_remedy_gate"
_GATE_LINE = re.compile(r"CHOICE (?P<case>[A-Z][A-Z0-9-]{0,31}|invalid_id) slot=" + GATE_SLOT + r" chosen=(?P<chosen>[a-z][a-z0-9_.]{0,63}) top5=(?P<top>[^ ]+) threshold=(?P<limit>[0-9.]+|n/a)")
_YES = re.compile(r"yes:([0-9]\.[0-9]{4})")


@dataclass(frozen=True, slots=True)
class ProbeCase:
    item: Item
    canonical: dict[str, Any]


def _letters(n: int) -> str:
    return chr(ord("a") + n // 26 % 26) + chr(ord("a") + n % 26)


def cases(path: Path = LABELLED_PATH) -> tuple[ProbeCase, ...]:
    """One case per labelled item, in the file's order. The canonical input is the goldens' (`canonical-input.v2`, a fixed instant, fixed synthetic ids); only the text is the item's. No label and no
    expectation travels in it (INV-22)."""
    out = []
    for n, item in enumerate(load(path)):
        canonical = {
            "schema_version": "2.0", "request_id": f"3f1a9c4e-8b2d-4e61-a7c5-{n + 1:012x}", "correlation_id": f"corr_probe{_letters(n)}", "tenant_id": "ten_synthetic",
            "session_id": "ses_synthetic", "principal_ref": "usr_synthetic", "channel": "text", "mode": "MODE_1", "text": item.text, "received_at": RECEIVED_AT,
        }
        out.append(ProbeCase(item, canonical))
    return tuple(out)


def parse_gate(line: str) -> tuple[str, str, Fraction | None, Fraction | None] | None:
    """(case id, the model's answer, the probability of yes, the threshold) of one gate line, or None for any other line. A probability or threshold that is not a decimal is None."""
    m = _GATE_LINE.fullmatch(line.strip())
    if m is None:
        return None
    yes = _YES.fullmatch(m["top"].split(",")[0]) if m["top"] != "none" else None
    p = Fraction(yes.group(1)) if yes else None
    limit = None if m["limit"] == "n/a" else Fraction(m["limit"])
    return m["case"], m["chosen"], p, limit


@dataclass(frozen=True, slots=True)
class ProbeReport:
    total: int
    non_explicit: int
    asserted: Mapping[str, bool | None]  # by item id: True asserted, False not, None no gate line
    false_positive: tuple[str, ...]
    false_negative: tuple[str, ...]
    borderline_false_positive: tuple[str, ...]
    borderline_false_negative: tuple[str, ...]
    no_gate: tuple[str, ...]

    @property
    def no_false_assertion(self) -> bool:
        """The condition of run 9 (A772): none of the non-explicit phrasings is asserted, the borderline ones counted."""
        return not self.false_positive and not self.borderline_false_positive

    def render(self) -> str:
        wrong = len(self.false_positive) + len(self.borderline_false_positive)
        rows = [
            ("false assertions (not an explicit ask, asserted)", self.false_positive),
            ("false assertions, borderline items", self.borderline_false_positive),
            ("false negatives (an explicit ask, not asserted)", self.false_negative),
            ("false negatives, borderline items", self.borderline_false_negative),
            ("no gate line for the item", self.no_gate),
        ]
        lines = [f"{LABEL}", f"A772 labelled probe: {self.total} phrasings, each run once (a development set: not the goldens, not the held-out set)",
                 f"false assertions on the non-explicit phrasings: {wrong} of {self.non_explicit}", "", "| group | count | ids |", "|---|---|---|"]
        lines += [f"| {name} | {len(ids)} | {' '.join(ids) if ids else 'none'} |" for name, ids in rows]
        lines += ["", "Run 9 is allowed only if the first line above reads 0 (A772)."]
        return "\n".join(lines) + "\n"


def asserted_by(lines: Sequence[str]) -> bool | None:
    """Whether the gate asserted, from one item's lines: the model answered yes and the probability of yes is at least the threshold. None: there is no usable gate line (never a pass)."""
    for line in lines:
        got = parse_gate(line)
        if got is None:
            continue
        _, chosen, p, limit = got
        if p is None or limit is None:
            return None
        return chosen == "yes" and p >= limit
    return None


def score(items: Sequence[Item], lines: Mapping[str, Sequence[str]]) -> ProbeReport:
    asserted: dict[str, bool | None] = {}
    fp: list[str] = []
    fn: list[str] = []
    bfp: list[str] = []
    bfn: list[str] = []
    none: list[str] = []
    for item in items:
        got = asserted_by(lines.get(item.item_id, ()))
        asserted[item.item_id] = got
        if got is None:
            none.append(item.item_id)
        if item.explicit_ask and got is not True:  # no gate line is a miss for an explicit ask: nothing was asserted
            (bfn if item.borderline else fn).append(item.item_id)
        if not item.explicit_ask and got is True:
            (bfp if item.borderline else fp).append(item.item_id)
    return ProbeReport(len(items), sum(1 for i in items if not i.explicit_ask), asserted, tuple(fp), tuple(fn), tuple(bfp), tuple(bfn), tuple(none))


@dataclass
class ProbeRun:
    status: dict[str, str] = field(default_factory=dict)  # DONE, ERROR or NOT_RUN, by item id
    lines: dict[str, list[str]] = field(default_factory=dict)
    spent_micros: int = 0
    calls: int = 0
    stopped: bool = False


async def run_probe(built: Sequence[ProbeCase], compile_one: Callable[[ProbeCase], Awaitable[Mapping[str, Any]]], guard: BudgetGuard, recorder: Recorder, *, calls: Callable[[], int],
                    evidence: Callable[[str], Sequence[str]] | None = None) -> ProbeRun:
    """Each phrasing once, in order, with the budget guard in front. A stop ends the run and stays: what was not reached is NOT_RUN. What the model answers is read only through the gate's line."""
    run = ProbeRun()
    for c in built:
        item_id = c.item.item_id
        if run.stopped or guard.stopped:
            run.stopped = True
            run.status[item_id] = "NOT_RUN"
            continue
        recorder.case_id = item_id
        try:
            await compile_one(c)
        except BudgetExceeded:
            run.stopped = True
            run.status[item_id] = "NOT_RUN"
            continue
        except CompileFailed:
            run.status[item_id] = "ERROR"
        else:
            run.status[item_id] = "DONE"
        run.lines[item_id] = list(evidence(item_id)) if evidence is not None else []
        if guard.stopped:
            run.stopped = True
    run.spent_micros, run.calls = guard.spent_micros, calls()
    return run


def exit_code(run: ProbeRun, report: ProbeReport) -> int:
    if run.calls == 0:
        return 3
    if run.stopped or any(s != "DONE" for s in run.status.values()) or not report.no_false_assertion:
        return 1
    return 0


def write_probe(out: Path, run_id: str, run: ProbeRun, report: ProbeReport, items: Sequence[Item], limits: Mapping[str, int]) -> None:
    """`probe.json` and `probe.md`: ids, statuses, labels' flags and numbers. No phrasing (AGENT-13)."""
    out.mkdir(parents=True, exist_ok=True)
    doc = {
        "run_id": run_id, "label": LABEL, "written_at": datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ"), "calls": run.calls, "spent_micros": run.spent_micros, "stopped": run.stopped, "limits": dict(limits),
        "no_false_assertion": report.no_false_assertion,
        "items": [{"id": i.item_id, "status": run.status.get(i.item_id, "NOT_RUN"), "explicit_ask": i.explicit_ask, "borderline": i.borderline, "asserted": report.asserted.get(i.item_id)} for i in items],
        "groups": {"false_positive": list(report.false_positive), "borderline_false_positive": list(report.borderline_false_positive), "false_negative": list(report.false_negative),
                   "borderline_false_negative": list(report.borderline_false_negative), "no_gate": list(report.no_gate)},
    }
    (out / "probe.json").write_text(dump(doc), encoding="utf-8")
    remaining = max(limits["hard_stop_micros"] - run.spent_micros, 0)
    (out / "probe.md").write_text(report.render() + f"\nSpent against the hard stop: {run.spent_micros} micro-dollars of {limits['hard_stop_micros']} (remaining {remaining}); provider calls {run.calls}; stopped {str(run.stopped).lower()}.\n", encoding="utf-8")


def console(run: ProbeRun, report: ProbeReport, code: int, limit: int) -> str:
    remaining = max(limit - run.spent_micros, 0)
    return report.render() + f"exit code {code}; provider calls {run.calls}; stopped {run.stopped}\nspent_micros_total={run.spent_micros} hard_stop_micros={limit} remaining_micros={remaining}\n"
