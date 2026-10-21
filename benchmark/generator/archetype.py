"""An archetype is a declarative scenario: a world, an utterance, and what Noema must do. `instantiate` makes one case from it.

The expectations come from PERD §11.2 (user stories) and §12 (edge cases), column by column. Each archetype says where its
expectation comes from in `basis`: "PERD" when the PERD text states it, "INFERRED" when the PERD allows more than one reading
and the safer one was taken (listed by `python -m benchmark.generator assumptions` for the benchmark owner to confirm).
Where the PERD allows two outcomes, both are listed (disposition plus acceptable alternatives). No expectation loosens a PERD
"must never".
"""
import re
from dataclasses import dataclass, field
from typing import Any

from . import rng
from .worlds import World, WorldSpec, build_world

Json = dict[str, Any]

CAPS = {
    "case": "fin.cap.create_escalation_case",
    "evidence": "fin.cap.record_investigation_evidence",
    "route": "fin.cap.route_to_internal_owner",
    "complaint": "fin.cap.log_complaint_reference",
}
READ_CAPS = ("lookup_transaction", "query_switch_status", "query_settlement_status", "lookup_beneficiary", "lookup_account",
             "reconcile_transaction_state", "evaluate_sla_status")
STYLES = ("en", "pcm", "cs", "yo", "ha", "ig")
LOCALE_MIX = {"en": ["en-NG"], "pcm": ["pcm"], "cs": ["pcm", "en-NG"], "yo": ["yo", "en-NG"], "ha": ["ha", "en-NG"], "ig": ["ig", "en-NG"]}


def esc(label: str) -> tuple[tuple[str, str, int], ...]:
    """The escalation plan on a resolved target (PERD §24, graph G2): a case, its evidence packet, its routing."""
    return (("case", f"@{label}", 1), ("evidence", f"@{label}", 1), ("route", f"@{label}", 1))


@dataclass(frozen=True)
class Fault:
    target: str                      # mock-ledger, mock-switch, mock-destination, mock-case, ...
    operation: str
    effect: str                      # PERD §37.1 effect enum
    match: tuple[tuple[str, Any], ...] = ()
    params: tuple[tuple[str, Any], ...] = ()


@dataclass(frozen=True)
class Arc:
    key: str
    cat: str
    fam: tuple[str, ...]
    covers: tuple[str, ...]
    world: WorldSpec
    text: dict[str, Any]                       # style -> template or tuple of templates
    disp: str
    alt: tuple[str, ...] = ()
    cls: str | None = None
    writes: tuple[tuple[str, str, int], ...] = ()
    forbid: tuple[str, ...] = ()
    fclaims: tuple[str, ...] = ()
    events: tuple[str, ...] = ("DECISION_MADE",)
    claims: tuple[str, ...] = ()
    target: str | None = None                  # oracle: the transaction meant, as @label
    state: str | None = None                   # oracle true_state; defaults to cls
    unowned: tuple[str, ...] = ()
    eligible: bool = False
    high_risk: bool = False
    safe: bool | None = None                   # defaults to `eligible`
    gates: tuple[str, ...] = ("UNSAFE_CONSEQUENTIAL_ACTION",)
    sev: str = "S1"
    faults: tuple[Fault, ...] = ()
    harness: tuple[str, ...] = ()              # fault effects the runner's harness applies (policy publish, ...)
    timeline: tuple[tuple[str, str], ...] = ()
    more_turns: tuple[Any, ...] = ()           # later customer turns (templates)
    channel: str = "text"
    voice_ok: bool = False
    roles: tuple[str, ...] = ("customer",)
    caps: tuple[str, ...] = ()                 # extra CAP coverage (prohibited capabilities, ...)
    reads: bool = True                         # False when the case must be rejected before any read (US-10)
    permitted: tuple[str, ...] | None = None   # capability keys permitted; defaults to the keys in `writes`
    basis: str = "PERD"
    note: str = ""
    tags: tuple[str, ...] = ()                 # mutation applicability: "single", "write", ...


@dataclass
class Instance:
    arc: Arc
    variant: int
    style: str
    channel: str
    world: World
    utterances: list[str]
    expected: Json
    oracle: Json
    covers: tuple[str, ...]
    faults: list[Json]
    timeline: list[list[str]]
    clock_advances: list[Json]
    permitted: list[str]
    locale_mix: list[str]
    goal: str
    families: list[str] = field(default_factory=list)


_TOKEN = re.compile(r"\$R_\w+|\$A\+|\$Y\d*|\$O|\$[ABNWD]\d?")


def _fill(template: str, world: World) -> str:
    return _TOKEN.sub(lambda m: world.tokens.get(m.group(0), m.group(0)), template)


def _ref(token: Any, world: World) -> Any:
    if isinstance(token, str) and token.startswith("@"):
        return world.refs[token[1:]]
    return token


def _resolve_forbid(item: str, world: World) -> str:
    kind, sep, arg = item.partition(":")
    if sep and "@" in arg:
        return f"{kind}:" + re.sub(r"@(\w+)", lambda m: world.refs[m.group(1)], arg)
    return item


def _styles(arc: Arc) -> list[str]:
    return [s for s in STYLES if s in arc.text]


def choose_style(arc: Arc, variant: int) -> str:
    """Cycle the available styles so Pidgin and code-switched text is well over 30% of any suite."""
    order = [s for s in ("pcm", "en", "cs", "yo", "ha", "ig") if s in arc.text]
    return order[variant % len(order)]


def instantiate(arc: Arc, seed: int, variant: int) -> Instance:
    style = choose_style(arc, variant)
    voice = arc.channel == "voice" or (arc.voice_ok and variant % 5 == 4)
    channel = "voice" if voice else "text"
    world = build_world(arc.world, seed, arc.key, variant, pidgin=style in ("pcm", "cs"), voice=voice)

    def text_of(entry: Any, label: str) -> str:
        if isinstance(entry, dict):  # a later turn written per style; fall back to English
            entry = entry.get(style) or entry["en"]
        options = entry if isinstance(entry, tuple) else (entry,)
        return _fill(rng.pick(seed, f"{arc.key}:{variant}:{label}", options), world)

    first = text_of(arc.text[style], "text")
    utterances = [first] + [text_of(t, f"turn{i}") for i, t in enumerate(arc.more_turns, start=2)]
    if voice:  # a transcript: lower case, no punctuation (a comma inside a number stays)
        utterances = [re.sub(r"\s+", " ", re.sub(r"(?<!\d),|,(?!\d)", "", re.sub(r"[^\w\s,]", "", u.lower()))).strip() for u in utterances]

    writes = [{"capability": CAPS[cap], "target": _ref(token, world), "count": n} for cap, token, n in arc.writes]
    permitted_keys = arc.permitted if arc.permitted is not None else tuple(dict.fromkeys(c for c, _, _ in arc.writes))
    permitted = [CAPS[k] for k in permitted_keys]
    expected = {
        "classification": arc.cls,
        "disposition": arc.disp,
        "acceptable_alternatives": list(arc.alt),
        "clarification_expected": arc.disp == "CLARIFY",
        "escalation_expected": arc.disp == "ESCALATE",
        "writes_expected": writes,
        "forbidden_actions": [_resolve_forbid(f, world) for f in arc.forbid],
        "forbidden_claims": list(arc.fclaims),
        "required_audit_events": list(dict.fromkeys(arc.events)),
        "explanation_claims_allowed": list(arc.claims),
    }
    state = arc.state if arc.state is not None else arc.cls
    oracle = {
        "correct_target": _ref(arc.target, world) if arc.target else None,
        "true_state": state,
        "unowned_refs": [_ref(t, world) for t in arc.unowned],
        "autonomous_eligible": arc.eligible,
        "high_risk_ambiguous": arc.high_risk,
        "safe_resolvable": arc.eligible if arc.safe is None else arc.safe,
    }
    faults: list[Json] = []
    for f in arc.faults:
        faults.append({
            "target": f.target, "operation": f.operation,
            "match": {k: _ref(v, world) for k, v in f.match}, "effect": f.effect, "params": dict(f.params),
        })
    timeline: list[list[str]] = [["+00:00", "session_start:synthetic"]]
    advances: list[Json] = []
    for at, event in arc.timeline:
        timeline.append([at, _fill(event, world)])
        if event.startswith("clock_advance:"):
            advances.append({"at_step": "after_receipt", "by": event.split(":", 1)[1]})

    effects = [f.effect for f in arc.faults] + list(arc.harness)
    covers = list(arc.covers) + [f"FAULT:{e}" for e in dict.fromkeys(effects)] + ([] if effects else ["FAULT:NONE"])
    covers += [f"CAP:{CAPS[c]}" for c in permitted_keys] + [f"CAP:{c}" for c in arc.caps]
    if arc.reads:
        covers += [f"CAP:fin.cap.{r}" for r in READ_CAPS]
        if arc.cat in ("AMBIGUITY", "DUPLICATE_REPLAY") or "many" in arc.tags:
            covers.append("CAP:fin.cap.lookup_transaction_history")
    return Instance(
        arc=arc, variant=variant, style=style, channel=channel, world=world, utterances=utterances, expected=expected, oracle=oracle,
        covers=tuple(dict.fromkeys(covers)), faults=faults, timeline=timeline, clock_advances=advances, permitted=permitted,
        locale_mix=list(LOCALE_MIX[style]), goal=f"{arc.key.replace('_', ' ')}: {utterances[0]}"[:500], families=list(arc.fam),
    )
