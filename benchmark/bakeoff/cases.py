"""The bake-off case generator (T4 item 9a): synthetic decision-model cases for the six eligible understanding tasks.

Deterministic: the same seed and `per_cell` give byte-identical cases on any platform. Randomness is a SHA-256 counter, not
`random`, so a Python release cannot change the set. Generated layer only; nothing here reads or infers anything held out.
"""
import hashlib
import itertools
import re
from typing import Any

from benchmark.bakeoff import seeds
from benchmark.contracts import sha256_of

Json = dict[str, Any]

GENERATOR_NAME = "noema-casegen-decisions"
GENERATOR_VERSION = "0.1.0"
TASKS = ("INTENT_NODE", "ENUM_SLOT", "AMBIGUITY_FLAG", "LOCALE", "ANSWER_BINDING", "INSTRUCTION_SCREEN")
BUCKETS = ("en-NG", "pcm", "code-switched", "voice-transcript")
CODES = {"INTENT_NODE": "INT", "ENUM_SLOT": "SLT", "AMBIGUITY_FLAG": "AMB", "LOCALE": "LOC", "ANSWER_BINDING": "BND", "INSTRUCTION_SCREEN": "INJ"}
PERTURBATIONS = ("CASE", "WHITESPACE", "PUNCTUATION", "FILLER", "ASR_STYLE")
VOICE_PERTURBATIONS = ("CASE", "WHITESPACE", "FILLER")

def _hash(seed: int, label: str) -> int:
    return int.from_bytes(hashlib.sha256(f"{seed}:{label}".encode()).digest()[:8], "big")


def _below(seed: int, label: str, n: int) -> int:
    return _hash(seed, label) % n


# --- perturbations --------------------------------------------------------------------------------------------------

def perturb(text: str, kind: str) -> str:
    """A meaning-preserving rewrite of `text`. Pure."""
    if kind == "CASE":
        return text.upper() if text.upper() != text else text.lower()
    if kind == "WHITESPACE":
        return text.replace(" ", "  ") + " "
    if kind == "PUNCTUATION":
        stripped = re.sub(r"(?<!\d),|,(?!\d)", "", text)  # a comma inside a number is part of the number
        return stripped[:-1] if stripped[-1:] in ".?!" else stripped + "."
    if kind == "FILLER":
        return "please " + text
    if kind == "ASR_STYLE":
        # lower case, no punctuation; a comma survives only inside a number (1,000), as a number does in a transcript
        kept = re.sub(r"[^\w\s,]", "", text.lower())
        return re.sub(r"\s+", " ", re.sub(r"(?<!\d),|,(?!\d)", "", kept)).strip()
    raise ValueError(f"unknown perturbation {kind!r}")


def asr_style(text: str) -> str:
    return perturb(text, "ASR_STYLE")


def _perturbations(text: str, bucket: str, seed: int, label: str) -> list[Json]:
    kinds = VOICE_PERTURBATIONS if bucket == "voice-transcript" else PERTURBATIONS
    order = sorted(kinds, key=lambda k: _hash(seed, f"{label}:perturb:{k}"))
    out: list[Json] = []
    for kind in order:
        changed = perturb(text, kind)
        if changed != text and changed:
            out.append({"kind": kind, "text": changed})
        if len(out) == 2:
            break
    return out


# --- rendering templates --------------------------------------------------------------------------------------------

Rendered = tuple[str, str, str, str | None]  # question id, text, expected, prior turn


def _fillers(template: str, bucket: str) -> list[dict[str, str]]:
    whens = seeds.WHEN_PCM if bucket == "pcm" else seeds.WHEN_EN
    amounts = [seeds.SPOKEN_AMOUNTS[a] for a in seeds.AMOUNTS] if bucket == "voice-transcript" else list(seeds.AMOUNTS)
    wanted = {
        "name": seeds.NAMES if "{name}" in template else ("",),
        "amt": amounts if "{amt}" in template else ("",),
        "when": whens if "{when}" in template else ("",),
    }
    return [{"name": n, "amt": a, "when": w} for n, a, w in itertools.product(wanted["name"], wanted["amt"], wanted["when"])]


def _render(entries: list[seeds.Entry], bucket: str) -> list[Rendered]:
    out: list[Rendered] = []
    for qid, template, expected in entries:
        for fill in _fillers(template, bucket):
            text = template.format(**fill)
            out.append((qid, asr_style(text) if bucket == "voice-transcript" else text, expected, None))
    return out


def _render_contradictions(bucket: str) -> list[Rendered]:
    base = "en-NG" if bucket == "voice-transcript" else bucket
    out: list[Rendered] = []
    for prior, template, expected in seeds.CONTRADICTION[base]:
        for name, amt, when in itertools.product(seeds.NAMES, seeds.AMOUNTS, seeds.WHEN_PCM if base == "pcm" else seeds.WHEN_EN):
            name2 = seeds.NAMES[(seeds.NAMES.index(name) + 1) % len(seeds.NAMES)]
            amt2 = seeds.AMOUNTS[(seeds.AMOUNTS.index(amt) + 1) % len(seeds.AMOUNTS)]
            values = {"name": name, "name2": name2, "amt": amt, "amt2": amt2, "when": when}
            text, prior_text = template.format(**values), prior.format(**values)
            if bucket == "voice-transcript":
                text, prior_text = asr_style(text), asr_style(prior_text)
            out.append(("contradicts_prior_turn", text, expected, prior_text))
    return out


def _bank(task: str, bucket: str) -> list[Rendered]:
    """Every rendering of every template for a task and bucket. Voice transcripts are the English and Pidgin banks, spoken."""
    banks: dict[str, tuple[list[seeds.Entry], list[seeds.Entry], list[seeds.Entry]]] = {
        "INTENT_NODE": (seeds.INTENT_EN, seeds.INTENT_PCM, seeds.INTENT_CS),
        "ENUM_SLOT": (seeds.SLOT_EN, seeds.SLOT_PCM, seeds.SLOT_CS),
        "AMBIGUITY_FLAG": (seeds.FLAG_EN, seeds.FLAG_PCM, seeds.FLAG_CS),
        "LOCALE": (seeds.LOCALE_EN, seeds.LOCALE_PCM, seeds.LOCALE_CS),
        "INSTRUCTION_SCREEN": (seeds.SCREEN_EN, seeds.SCREEN_PCM, seeds.SCREEN_CS),
    }
    en, pcm, cs = banks[task]
    if bucket == "voice-transcript":
        rendered = _render(en, bucket) + _render(pcm, bucket)
    else:
        rendered = _render({"en-NG": en, "pcm": pcm, "code-switched": cs}[bucket], bucket)
    if task == "AMBIGUITY_FLAG":
        rendered += _render_contradictions(bucket)
    return sorted(set(rendered), key=lambda r: (r[0], r[2], r[1], r[3] or ""))


def _select(rendered: list[Rendered], per_cell: int, seed: int, label: str) -> list[Rendered]:
    """Round-robin across (question, expected) groups so classes stay balanced; within a group, pseudo-random order."""
    groups: dict[tuple[str, str], list[Rendered]] = {}
    for r in rendered:
        groups.setdefault((r[0], r[2]), []).append(r)
    ordered = {k: sorted(v, key=lambda r: _hash(seed, f"{label}:{r[0]}:{r[1]}")) for k, v in sorted(groups.items())}
    chosen: list[Rendered] = []
    cursor = 0
    while len(chosen) < per_cell and any(ordered.values()):
        for key in ordered:
            if ordered[key] and len(chosen) < per_cell:
                chosen.append(ordered[key].pop(0))
        cursor += 1
    return chosen


# --- answer binding -----------------------------------------------------------------------------------------------

HOURS = (8, 9, 10, 11, 14, 15, 16, 17)
MINUTES = (0, 10, 15, 30, 45)
BINDING_AMOUNTS = (("NGN 2,500", "N2,500"), ("NGN 5,000", "5,000 naira"), ("NGN 10,000", "10k"), ("NGN 20,000", "20k"), ("NGN 50,000", "N50,000"))
BINDING_KINDS = ("time", "last4", "ordinal", "none", "ambiguous")


def _binding_case(bucket: str, index: int, seed: int) -> tuple[str, list[Json], str, str]:
    """(utterance, options, expected option id or 'none', prior unused)."""
    label = f"bind:{bucket}:{index}"
    kind = BINDING_KINDS[index % len(BINDING_KINDS)]
    base = ("en-NG", "pcm")[index % 2] if bucket == "voice-transcript" else bucket
    count = 2 + _below(seed, f"{label}:count", 2)
    hours: list[int] = []
    for step in range(40):
        h = HOURS[_below(seed, f"{label}:hour:{step}", len(HOURS))]
        if len(hours) < count and all(h % 12 != x % 12 for x in hours):
            hours.append(h)
    hours.sort()
    last4s: list[str] = []
    for step in range(40):
        v = str(1000 + _below(seed, f"{label}:l4:{step}", 9000))
        if len(last4s) < count and v not in last4s:
            last4s.append(v)
    amounts = [BINDING_AMOUNTS[_below(seed, f"{label}:amt:{i}", len(BINDING_AMOUNTS))] for i in range(count)]
    if kind == "ambiguous":
        amounts = [amounts[0]] * count
    elif kind in ("time", "last4", "ordinal"):
        pass
    minutes = [MINUTES[_below(seed, f"{label}:min:{i}", len(MINUTES))] for i in range(count)]
    options = [{"id": f"opt_{i + 1}", "label": f"{hours[i]:02d}:{minutes[i]:02d}, {amounts[i][0]}, ending {last4s[i]}"} for i in range(count)]
    target = _below(seed, f"{label}:target", count)
    expected = options[target]["id"]
    if kind == "time":
        h = hours[target]
        part = "morning" if h < 12 else "afternoon"
        part_unique = sum(1 for x in hours if (x < 12) == (h < 12)) == 1
        phrases = [p for p in seeds.BINDING_TIME[base] if part_unique or "{part}" not in p]
        text = phrases[_below(seed, f"{label}:phrase", len(phrases))].format(h=h % 12 or 12, part=part)
    elif kind == "last4":
        phrases = seeds.BINDING_LAST4[base]
        text = phrases[_below(seed, f"{label}:phrase", len(phrases))].format(l4=last4s[target])
    elif kind == "ordinal":
        phrases2 = seeds.BINDING_ORDINAL[base]
        text, ordinal = phrases2[_below(seed, f"{label}:phrase", len(phrases2))]
        ordinal = min(ordinal, count - 1)
        expected = options[ordinal]["id"]
    elif kind == "none":
        phrases = seeds.BINDING_NONE[base]
        text = phrases[_below(seed, f"{label}:phrase", len(phrases))]
        expected = "none"
    else:
        phrases = seeds.BINDING_AMBIGUOUS[base]
        text = phrases[_below(seed, f"{label}:phrase", len(phrases))].format(amt=amounts[0][1])
        expected = "none"
    if bucket == "voice-transcript":
        text = asr_style(text)
    return text, options, expected, kind


def _binding_cases(bucket: str, per_cell: int, seed: int) -> list[Json]:
    out: list[Json] = []
    seen: set[str] = set()
    index = 0
    while len(out) < per_cell and index < per_cell * 20:
        text, options, expected, _ = _binding_case(bucket, index, seed)
        key = json_key(text, options)
        index += 1
        if key in seen:
            continue
        seen.add(key)
        question = {"id": "answer_binding", "type": "choice", "prompt": "Which of the offered options does the customer's answer select?",
                    "options": [o["id"] for o in options] + ["none"]}
        out.append({"_task": "ANSWER_BINDING", "_sort": (text, expected), "bucket": bucket, "question": question, "text": text,
                    "expected": expected, "context": {"options": options}})
    return out


def json_key(text: str, options: list[Json]) -> str:
    return text + "|" + "|".join(o["label"] for o in options)


# --- assembling ---------------------------------------------------------------------------------------------------

def _case(task: str, bucket: str, number: int, seed: int, question: Json, text: str, expected: str, context: Json | None) -> Json:
    code = CODES[task]
    case_id = f"BDC-{code}-{number:04d}"
    input_: Json = {"channel": "voice_transcript" if bucket == "voice-transcript" else "text", "text": text}
    if context:
        input_["context"] = context
    return {
        "case_id": case_id,
        "version": 1,
        "task": task,
        "language_bucket": bucket,
        "generator": {"name": GENERATOR_NAME, "version": GENERATOR_VERSION, "seed": seed},
        "input": input_,
        "question": question,
        "expected": expected,
        "perturbations": _perturbations(text, bucket, seed, case_id),
        "provenance": "generated",
    }


def generate(seed: int, per_cell: int = 20) -> list[Json]:
    """All cases: `per_cell` for every (task, language bucket) cell, in case-id order."""
    if per_cell < 1:
        raise ValueError("per_cell must be at least 1")
    cases: list[Json] = []
    for task in TASKS:
        counter = 0
        for bucket in BUCKETS:
            if task == "ANSWER_BINDING":
                for item in _binding_cases(bucket, per_cell, seed):
                    counter += 1
                    cases.append(_case(task, bucket, counter, seed, item["question"], item["text"], item["expected"], item["context"]))
                continue
            rendered = _select(_bank(task, bucket), per_cell, seed, f"{task}:{bucket}")
            for qid, text, expected, prior in sorted(rendered, key=lambda r: (r[0], r[2], r[1])):
                counter += 1
                question = {"id": qid, **seeds.QUESTIONS[qid]}
                context = {"prior_turn": prior} if prior else None
                cases.append(_case(task, bucket, counter, seed, question, text, expected, context))
    return sorted(cases, key=lambda c: c["case_id"])


def cases_digest(cases: list[Json]) -> str:
    """SHA-256 over the canonical bytes of the cases in id order: ties a run to the exact set it answered."""
    return sha256_of(sorted(cases, key=lambda c: c["case_id"]))
