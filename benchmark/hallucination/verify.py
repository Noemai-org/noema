"""A reference checker for the four checks of PERD 31.2, so the corpus verifies itself (BEN-14). It is NOT the product validator (that is Go, in core, REQ-EXP-002).

The four checks: (1) every sentence maps to a fact or a template, (2) every number, time and name in the text appears in the facts, (3) no forbidden-claim pattern for the
locale, (4) no unmasked digit run longer than 4. The patterns here are the corpus's own oracle for its own items, not pack data. SYNTHETIC."""
import re
from typing import Any

Json = dict[str, Any]
FORBIDDEN = (
    re.compile(r"money (is )?safe", re.IGNORECASE),
    re.compile(r"reversed in \d+", re.IGNORECASE),
    re.compile(r"receiving bank (has )?failed|bank don fail", re.IGNORECASE),
    re.compile(r"reverse inside \d+", re.IGNORECASE),
)
SUCCESS = re.compile(r"\b(done|resolved|fixed)\b|don (resolve|fix)", re.IGNORECASE)
NUMBER = re.compile(r"(?<![\w*])\d[\d,]*(?:[.:]\d+)?")
NAME = re.compile(r"\b(?:Mr|Mrs|Ms|Dr|Chief|Alhaji|Alhaja)\.? [A-Z][a-z]+")
SENTENCE = re.compile(r"[^.!?]+[.!?]?")


def _sentences(text: str) -> list[str]:
    return [s.strip() for s in SENTENCE.findall(text) if s.strip()]


def _fact_text(facts: Json) -> str:
    return " ".join(f"{f['value_masked']} {f['as_of']}" for f in facts["facts"])


def violated_checks(facts: Json, rendered: Json) -> set[int]:
    text = rendered["text"]
    out: set[int] = set()
    spans = [c["span"] for c in rendered["claims"]]
    if any(not any(s in span or span in s for span in spans) for s in _sentences(text)):
        out.add(1)
    allowed = _fact_text(facts)
    stripped = text
    for token in NUMBER.findall(stripped):
        if token.strip(",.:") and token not in allowed:
            out.add(2)
    for name in NAME.findall(text):
        if name not in allowed:
            out.add(2)
    achieved = any(a["verdict"] == "ACHIEVED" for a in facts["actions"])
    if any(p.search(text) for p in FORBIDDEN) or (SUCCESS.search(text) and not achieved):
        out.add(3)
    if re.search(r"(?<!\*)\d{5,}", text):
        out.add(4)
    return out
