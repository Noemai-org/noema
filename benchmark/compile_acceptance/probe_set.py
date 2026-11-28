"""BEN-23 (A772): the 28 labelled phrasings as the offline probe set for the `requested_remedy` gate, and the B14 and B15 to B20 evidence in one table.

`python -m benchmark.compile_acceptance.probe_set --out benchmark/compile_acceptance/boundary-a604/probe-set.v1.json` writes, for each phrasing of `remedy_labelled.v1.json`: the boundary it was asked at (one for each boundary, in order; the rest
have no recording), the recording that answers its gate question and what replay does with that recording: SERVED, EVIDENCE_ONLY (a reply the harness refuses as a whole, so replay never delivers it) or NOT_COMMITTED (a boundary
whose probability text holds a digit run of five or more, which the leak rule keeps out of a committed file; a test generates it). It is computed from the committed findings and fixture and invents nothing. SYNTHETIC (AGENT-25)."""
import argparse
import json
import sys
from collections.abc import Sequence
from pathlib import Path
from typing import Any

from benchmark.compile_acceptance.remedy_labelled import load
from benchmark.compile_acceptance.replay import ReplayRefused, ReplayStore, load_recordings

Json = dict[str, Any]
HERE = Path(__file__).resolve().parent
DIR = HERE / "boundary-a604"
EVIDENCE = ("B14", "B15", "B16", "B17", "B18", "B19", "B20")
LABEL = "SYNTHETIC offline probe set for the requested_remedy gate (A772): invented phrasings and scripted replies, not live model output (A261)"


def _gate_recordings(fixture: Path) -> dict[str, str]:
    """case id -> the recording id of its gate question."""
    out: dict[str, str] = {}
    for path in sorted(fixture.glob("rec-*.json")):
        doc = json.loads(path.read_text(encoding="utf-8"))
        text = json.dumps(doc["reply"])
        if "requested_remedy_gate" in text and "probabilities" in text:
            out[doc["case_id"]] = path.stem
    return out


def build() -> Json:
    findings = {f["boundary"]: f for f in json.loads((DIR / "boundary-findings.json").read_text(encoding="utf-8"))["boundaries"]}
    by_item = {f["labelled_item"]: f for f in findings.values() if "labelled_item" in f}
    skipped_items = {i: b for b, f in findings.items() if "skipped" in f for i in [_item_of(b, findings)]}
    recordings = load_recordings(DIR / "fixture")
    store = ReplayStore(recordings)
    by_id = {r.recording_id: r for r in recordings.recordings}
    gate = _gate_recordings(DIR / "fixture")

    def replay(boundary: str) -> tuple[str, str | None]:
        finding = findings[boundary]
        if "skipped" in finding:
            return "NOT_COMMITTED", None
        recording_id = gate[finding["case_id"]]
        try:
            store.lookup(by_id[recording_id].request_sha256)
        except ReplayRefused as refused:
            return refused.code, recording_id
        return "SERVED", recording_id

    items: list[Json] = []
    for n, item in enumerate(load(HERE / "remedy_labelled.v1.json")):
        boundary = by_item.get(item.item_id, {}).get("boundary") or skipped_items.get(item.item_id)
        status, recording = replay(boundary) if boundary else ("NO_RECORDING", None)
        items.append({"item_id": item.item_id, "locale": item.locale, "text": item.text, "boundary": boundary, "gate_recording": recording, "replay": status})
        _ = n
    evidence = []
    for boundary in EVIDENCE:
        finding = findings[boundary]
        status, recording = replay(boundary)
        evidence.append({"boundary": boundary, "item_id": finding["labelled_item"], "written": finding["written"], "expected": finding["expected"], "gate_outcome": finding["implementation"],
                         "agree": finding["agree"], "gate_recording": recording, "replay": status, "why": finding["why"]})
    return {"label": LABEL, "set": "remedy_labelled.v1.json", "threshold_bp": 9000, "items": items, "evidence": evidence}


def _item_of(boundary: str, findings: dict[str, Json]) -> str:
    """The labelled item a boundary is asked at: the boundaries take the set's items in order, B01 the first (`synthetic_boundary.generate`)."""
    return f"RL-{int(boundary[1:]):03d}"


def render(doc: Json) -> str:
    return json.dumps(doc, indent=2, sort_keys=True, ensure_ascii=False) + "\n"


def main(argv: Sequence[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.compile_acceptance.probe_set", description=__doc__, allow_abbrev=False)
    p.add_argument("--out", required=True, type=Path)
    a = p.parse_args(argv)
    doc = build()
    a.out.write_text(render(doc), encoding="utf-8")
    sys.stdout.write(f"{len(doc['items'])} phrasings, {len(doc['evidence'])} evidence rows\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
