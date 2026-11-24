"""A604 (UND-18, run 7): SYNTHETIC replay recordings of the compile route at the boundary of the `requested_remedy` gate.

`python -m benchmark.compile_acceptance.synthetic_boundary --out DIR` runs the real compile service (the model service's pipeline, harness, provider adapter and recorder) on the golden input G-A once per
boundary below, with an INNER TRANSPORT THAT ANSWERS FROM A SCRIPT, not a provider: no network, no key, no cost. Each boundary changes one thing, the number the "model" wrote as the probability of yes for the
gate question, and nothing else. The recorder then writes the same `rec-NNNN.json` files and `manifest.json` a live run writes (`recording.py`), so the replay provider (`replay.py`) and the fixture reduction
(`fixtures.py`) read them as they read a live run's. Every file says SYNTHETIC: this is not live model output, it is not evidence of what any model does, and it is never to be passed off as a live
recording (A261). It exists so the gate's arithmetic can be fixed at its boundary with the replay provider before any live run (A604, A605).

The EXPECTED outcome of each boundary is computed here from the TEXT the provider wrote, by exact integer and fraction arithmetic and the rule of A612 (a plain decimal in [0, 1], floored to whole basis
points, compared with the pack's threshold; anything else, a missing value included, abstains). It does not read the implementation's answer. The run then compares the two and prints every disagreement,
so a divergence is a finding for the gate's owner and is never hidden. Each boundary is asked about a DIFFERENT utterance, the next item of the A604 labelled set (`remedy_labelled.v1.json`, in file order): the replay provider keys a recording by the SHA-256 of the exact request,
and the gate's request is a fixed sentence, so two boundaries on one utterance would be one request with two answers. The scripted answers do not depend on the utterance's label; the utterance only makes the request unique
and plausible. The tool, the pack and the lexicon are G-A's.

The script needs the gate (UND-18) on the model service's path; without it the script refuses and writes nothing."""
import argparse
import asyncio
import json
import re
import sys
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, replace
from datetime import UTC, datetime
from fractions import Fraction
from importlib import import_module
from pathlib import Path
from types import SimpleNamespace
from typing import Any

from benchmark.bakeoff.budget import BudgetGuard
from benchmark.compile_acceptance.build import build_backend
from benchmark.compile_acceptance.golden import GOLDEN
from benchmark.compile_acceptance.recording import Recorder
from benchmark.compile_acceptance.remedy_labelled import load as load_labelled

SYNTHETIC_LABEL = "SYNTHETIC boundary recordings of the requested_remedy gate (A604): scripted replies, not live model output"
THRESHOLD_BP = 9000  # the Architect's interim value (A603), a Founder decision to confirm; read here only to compute the expected outcome, never set (AGENT-16)
GATE_ID, PICK_ID, SLOT = "requested_remedy_gate", "requested_remedy", "requested_remedy"
LOOKUP = "fin.cap.lookup_transaction"
PLAIN = re.compile(r"[0-9]+(?:\.[0-9]+)?")
LONG_RUN = re.compile(r"[0-9]{5,}")  # the leak rule (REQ-SEC-003, A154): a digit run of five or more is a finding wherever it is written, so such a boundary is generated only on request
URL = "https://scripted.invalid/v1/responses"
HARNESS_CONFIG = "benchmark/decisions/harness-gpt-6-luna.json"
PACK_REF = "fin@1.0.0"
G_A_ANSWERS = {"intent_node": "fin.payments.exception.investigate", "question_type": "STATUS", "locale": "Nigerian Pidgin"}
MENTIONS = {
    "amount": {"present": True, "surface_form": "20k", "proposed_value": "20k", "confidence": 0.9},
    "counterparty": {"present": True, "surface_form": "Tunde", "proposed_value": "Tunde", "confidence": 0.9},
    "when": {"present": True, "surface_form": "this morning", "proposed_value": "morning", "confidence": 0.8},
}


@dataclass(frozen=True, slots=True)
class Boundary:
    boundary_id: str
    written: str | None  # the JSON text of the probability of yes the scripted model writes; None: the member is left out
    selected: str = "yes"
    why: str = ""


BOUNDARIES: tuple[Boundary, ...] = (
    Boundary("B01", "0.8999", why="one basis point below the threshold"),
    Boundary("B02", "0.89999", why="floors to 8999, never rounds up to 9000"),
    Boundary("B03", "0.9", why="exactly the threshold"),
    Boundary("B04", "0.90", why="the same value written with a trailing zero"),
    Boundary("B05", "0.9000", why="the same value written to four places"),
    Boundary("B06", "0.90001", why="floors to 9000"),
    Boundary("B07", "0.9001", why="one basis point above"),
    Boundary("B08", "0.5", why="doubt"),
    Boundary("B09", "0", why="zero as an integer"),
    Boundary("B10", "1", why="one as an integer"),
    Boundary("B11", "1.0", why="one with a decimal point"),
    Boundary("B12", "1.00009", why="above one by less than a basis point: never floored up to one"),
    Boundary("B13", "1e-05", why="an exponent: not a plain decimal"),
    Boundary("B14", "9e-1", why="an exponent for 0.9: not a plain decimal, whatever it equals"),
    Boundary("B15", "-0.1", why="a sign"),
    Boundary("B16", "NaN", why="not a number"),
    Boundary("B17", "Infinity", why="not a finite number"),
    Boundary("B18", "null", why="the member is present and null"),
    Boundary("B19", None, why="the probability member is missing"),
    Boundary("B20", '"0.95"', why="a string where a number is required"),
    Boundary("B21", "0.95", selected="no", why="the model's own answer is no: a no whatever its probability says"),
    Boundary("B22", "0.6", why="the model says yes but gives it 0.6: doubt, the harmful case"),
)


def expected_bp(written: str | None) -> int | None:
    """Whole basis points of the text the provider wrote by the rule of A612, or None when it is malformed or missing. Fractions and integers only."""
    if written is None or PLAIN.fullmatch(written) is None:
        return None
    value = Fraction(written)
    if value > 1:
        return None
    return int(value * 10_000)  # floor, by the integer division of a non-negative fraction


def expected_passes(b: Boundary) -> bool:
    bp = expected_bp(b.written)
    return b.selected == "yes" and bp is not None and bp >= THRESHOLD_BP


def _answer(question: Mapping[str, Any], boundary: Boundary, abstain: str) -> dict[str, Any]:
    qid, kind = question["question_id"], question["kind"]
    if kind == "yes_no" and qid == GATE_ID:
        member = "" if boundary.written is None else f', "p": {boundary.written}'
        return {"__raw__": f'{{"question_id": "{qid}", "selected": "{boundary.selected}", "probabilities": [{{"option": "yes"{member}}}, {{"option": "no", "p": 0.1}}]}}'}
    if kind == "yes_no":
        return {"question_id": qid, "selected": "no", "probabilities": [{"option": "yes", "p": 0.1}, {"option": "no", "p": 0.9}]}
    want = LOOKUP if qid == PICK_ID else G_A_ANSWERS.get(qid)
    pick = abstain
    for option in question["options"]:
        if want is not None and (want == option["label"] or f"({want})" in option["label"]):
            pick = option["option_id"]
    return {"question_id": qid, "selected": pick, "probabilities": [{"option": o["option_id"], "p": 0.95 if o["option_id"] == pick else 0.0} for o in question["options"]]}


class ScriptedInner:
    """The transport under the recorder: a scripted answer to each request, in the provider's wire format. It reads the request body the pipeline built and answers its questions; it keeps nothing."""

    def __init__(self, abstain: str) -> None:
        self.boundary: Boundary = BOUNDARIES[0]
        self._abstain = abstain

    async def post(self, url: str, headers: Mapping[str, str], body: bytes, timeout_s: float, max_bytes: int) -> Any:
        request = json.loads(body)
        schema = request["text"]["format"]["name"]
        if schema == "pipeline_slot_mentions":
            names = list(request["text"]["format"]["schema"]["properties"])
            empty = {"present": False, "surface_form": "", "proposed_value": "", "confidence": 0.0}
            text = json.dumps({n: MENTIONS.get(n, empty) for n in names})
        else:
            questions = json.loads(request["input"].split("QUESTIONS:\n", 1)[1].split("\n\n<<<DATA", 1)[0])
            parts = [_answer(q, self.boundary, self._abstain) for q in questions]
            text = '{"answers": [' + ", ".join(p["__raw__"] if "__raw__" in p else json.dumps(p) for p in parts) + "]}"
        message = {"type": "message", "content": [{"type": "output_text", "text": text}]}
        reply = json.dumps({"id": "resp_scripted", "model": "gpt-6-luna", "status": "completed", "output": [{"type": "reasoning", "summary": []}, message],
                            "usage": {"input_tokens": 100, "output_tokens": 20}}).encode()
        return SimpleNamespace(status=200, body=reply)


def _remedy_slot(proposal: Mapping[str, Any]) -> str | None:
    slot = (proposal.get("slots") or {}).get(SLOT)
    return slot.get("value") if isinstance(slot, Mapping) else None


LABELLED_PATH = Path(__file__).resolve().parent / "remedy_labelled.v1.json"


async def generate(out: Path, now: datetime, include_long: bool = False) -> list[dict[str, Any]]:
    harness = import_module("noema_model.harness")
    steps = import_module("noema_model.understanding.pipeline.steps")
    if not hasattr(steps, "gate_asserted") or not hasattr(steps, "probability_bp"):
        raise SystemExit("refused: the gate (UND-18) is not on the model service's path, so there is nothing to record")
    inner = ScriptedInner(harness.ABSTAIN)
    guard = BudgetGuard(limit_micros=5_000_000, spent_micros=0)
    recorder = Recorder(out, "synthetic-boundary", "openai", "gpt-6-luna", lambda: now)
    args = SimpleNamespace(harness_config=HARNESS_CONFIG, pack_ref=PACK_REF)
    backend = build_backend(False, {"OPENAI_API_KEY": "synthetic-not-a-key"}, guard, recorder, args, inner)
    g_a = next(g for g in GOLDEN if g.case_id == "G-A")
    items = load_labelled(LABELLED_PATH)
    if len(items) < len(BOUNDARIES):
        raise SystemExit("refused: the labelled set has fewer items than there are boundaries")
    findings: list[dict[str, Any]] = []
    for n, b in enumerate(BOUNDARIES):
        if b.written is not None and LONG_RUN.search(b.written) and not include_long:
            findings.append({"boundary": b.boundary_id, "skipped": "its probability text holds a digit run of five or more, which the leak rule refuses in a committed file; generate it with --include-long-digit-runs", "why": b.why})
            continue
        inner.boundary = b
        item = items[n]
        case = replace(g_a, case_id=f"SB-{chr(ord('A') + n)}", text=item.text)  # the letter keeps the identifiers letters-only, as golden.canonical_input expects
        try:
            proposal = await backend.compile_one(case)
            got: str | None = "ABSENT" if _remedy_slot(proposal) is None else "PRESENT"
        except Exception as exc:  # the class only: a recorded finding, never hidden
            got = f"STOPPED:{type(exc).__name__}" + (f" {exc}" if type(exc).__name__ == "CompileFailed" else "")  # CompileFailed's text is built from a closed set (build.py), never from the request
        want = "PRESENT" if expected_passes(b) else "ABSENT"
        findings.append({"boundary": b.boundary_id, "case_id": case.case_id, "labelled_item": item.item_id, "written": b.written, "selected": b.selected, "expected": want, "implementation": got, "agree": got == want, "why": b.why})
    recorder.write_manifest(backend.pin())
    return findings


def main(argv: Sequence[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.compile_acceptance.synthetic_boundary", description=__doc__, allow_abbrev=False)
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--include-long-digit-runs", action="store_true", help="also generate the boundaries whose probability text holds five or more digits in a row (not for a committed file: the leak rule)")
    p.add_argument("--date", default="2026-11-24T00:00:00Z", help="the fixed recorded_at, so the output is reproducible (INV-21)")
    a = p.parse_args(argv)
    findings = asyncio.run(generate(a.out, datetime.strptime(a.date, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=UTC), a.include_long_digit_runs))
    (a.out / "boundary-findings.json").write_text(json.dumps({"label": SYNTHETIC_LABEL, "threshold_bp": THRESHOLD_BP, "boundaries": findings}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    bad = [f for f in findings if f.get("agree") is False]
    for f in (f for f in findings if "agree" in f):
        print(f"{f['boundary']} written={f['written']!s:10} selected={f['selected']:3} expected={f['expected']:7} implementation={f['implementation']}{'' if f['agree'] else '  <-- DISAGREES'}")
    print(f"{sum('agree' in f for f in findings)} boundaries generated, {sum('skipped' in f for f in findings)} skipped, {len(bad)} disagreement(s)")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
