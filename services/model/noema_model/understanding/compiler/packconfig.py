"""The pack loader for the pipeline: `PipelineConfig` from the pack's `understanding` block, the candidate node's `disambiguation` and the matcher file the pack names
(UND-05; REQ-AMB-002, REQ-AMB-003, REQ-INT-005; INV-14, INV-21; AGENT-16).

Nothing is written in code and nothing is defaulted: a pack that is not whole fails here, at load, never at the first request. The margin and the limit of candidates
stay in the node's `disambiguation`, where the SoT shape puts them (`packs/fintech/understanding.md`); a JSON number there is read as its decimal (0.35 is 7/20, not the
binary 0.34999...), which gives the same value on every machine. The yes threshold of a native decision model is not in the pack: a benchmark and a founder decision
set it (AGENT-16), so it stays unset (`None`), which the pipeline reads as "a yes is a yes".
"""

import re
from collections.abc import Mapping
from decimal import Decimal
from fractions import Fraction
from pathlib import Path
from typing import Any

from noema_model.platform.config import ConfigError, reject_unknown, require_int, require_mapping, require_str
from noema_model.understanding.pipeline import FlagSpec, LocaleOption, NodeCatalogue, PipelineConfig
from noema_model.understanding.resolution import RankingConfig, RecipientMatcher, ResolutionState
from noema_model.understanding.resolution.config import exact_number

_FLAG = re.compile(r"[A-Z][A-Z0-9_]*")
_LOCALE = re.compile(r"[a-z]{2,3}(-[A-Za-z0-9]{2,8})*")
_MEMBERS = frozenset({"ranking", "proposable_flags", "locales", "material_flags", "intent_unclear_flag", "out_of_scope_flag", "state_flags"})
_OPTIONAL_MEMBERS = frozenset({"slot_gates"})  # UND-18: a pack that gates nothing may leave it out
_MARGIN_KEYS = ("auto_resolve_margin", "max_candidates_before_escalate")


def _flag(value: Any, where: str) -> str:
    if not isinstance(value, str) or not _FLAG.fullmatch(value):
        raise ConfigError(f"{where}: expected a flag name such as MULTIPLE_INTENTS")
    return value


def _list(source: Mapping[str, Any], key: str) -> list[Any]:
    value = source.get(key)
    if not isinstance(value, list):
        raise ConfigError(f"understanding.{key}: expected a list")
    return value


def _proposable_flags(source: Mapping[str, Any]) -> tuple[FlagSpec, ...]:
    out: list[FlagSpec] = []
    for index, raw in enumerate(_list(source, "proposable_flags")):
        where = f"understanding.proposable_flags[{index}]"
        item = require_mapping(raw, where)
        reject_unknown(item, frozenset({"flag", "question"}), where)
        out.append(FlagSpec(_flag(item.get("flag"), f"{where}.flag"), require_str(item, "question", where)))
    if len({f.flag for f in out}) != len(out):
        raise ConfigError("understanding.proposable_flags: a flag is named twice")
    return tuple(out)


def _locales(source: Mapping[str, Any]) -> tuple[LocaleOption, ...]:
    out: list[LocaleOption] = []
    for index, raw in enumerate(_list(source, "locales")):
        where = f"understanding.locales[{index}]"
        item = require_mapping(raw, where)
        reject_unknown(item, frozenset({"code", "label"}), where)
        code = require_str(item, "code", where)
        if not _LOCALE.fullmatch(code):
            raise ConfigError(f"{where}.code: expected a language code such as en-NG")
        out.append(LocaleOption(code, require_str(item, "label", where)))
    if len({o.code for o in out}) != len(out):
        raise ConfigError("understanding.locales: a code is named twice")
    return tuple(out)


def _material_flags(source: Mapping[str, Any]) -> frozenset[str]:
    flags = [_flag(f, f"understanding.material_flags[{i}]") for i, f in enumerate(_list(source, "material_flags"))]
    if len(set(flags)) != len(flags):
        raise ConfigError("understanding.material_flags: a flag is named twice")
    return frozenset(flags)


def _state_flags(source: Mapping[str, Any]) -> Mapping[ResolutionState, str]:
    raw = require_mapping(source.get("state_flags"), "understanding.state_flags")
    out: dict[ResolutionState, str] = {}
    for name, flag in raw.items():
        try:
            state = ResolutionState(name)
        except ValueError as err:
            raise ConfigError(f"understanding.state_flags: {name} is not a state of the ranking") from err
        out[state] = _flag(flag, f"understanding.state_flags.{name}")
    return out


def _slot_gates(source: Mapping[str, Any], pack: Mapping[str, Any]) -> Mapping[str, int]:
    """`understanding.slot_gates` (UND-18, ruling A603): slot name to its threshold in integer basis points (1 to 10000; a bool or a float is refused). A gate names an optional slot that has a closed list of
    values in some node, once; anything else fails here, at load, never at the first request. Absent means the pack gates nothing."""
    if "slot_gates" not in source:
        return {}
    raw = source["slot_gates"]
    if not isinstance(raw, list) or not raw:
        raise ConfigError("understanding.slot_gates: expected a non-empty list")
    slots: dict[str, list[Mapping[str, Any]]] = {}
    nodes_with: dict[str, int] = {}
    for node in pack.get("intent_nodes") or []:
        seen: set[str] = set()
        for slot in require_mapping(node, "intent_nodes[]").get("slots") or []:
            item = require_mapping(slot, "intent_nodes[].slots[]")
            name = require_str(item, "name", "slot")
            slots.setdefault(name, []).append(item)
            if name not in seen:
                seen.add(name)
                nodes_with[name] = nodes_with.get(name, 0) + 1
    out: dict[str, int] = {}
    for index, entry in enumerate(raw):
        where = f"understanding.slot_gates[{index}]"
        item = require_mapping(entry, where)
        reject_unknown(item, frozenset({"slot", "assertion_threshold_bp"}), where)
        name = require_str(item, "slot", where)
        threshold = item.get("assertion_threshold_bp")
        if isinstance(threshold, bool) or not isinstance(threshold, int) or not 1 <= threshold <= 10_000:
            raise ConfigError(f"{where}.assertion_threshold_bp: expected a whole number of basis points from 1 to 10000")
        if name in out:
            raise ConfigError(f"{where}.slot: a slot is gated twice")
        declared = slots.get(name)
        if not declared:
            raise ConfigError(f"{where}.slot: no node declares such a slot")
        if nodes_with[name] > 1:
            raise ConfigError(f"{where}.slot: the slot is declared by more than one node; a gate belongs to one node (the same name in a second node would receive it too)")
        if any(s.get("required") is True for s in declared):
            raise ConfigError(f"{where}.slot: only an optional slot may be gated")
        if any(s.get("vocabulary") is None and s.get("references") is None for s in declared):
            raise ConfigError(f"{where}.slot: the slot has no closed list of values")
        out[name] = threshold
    return out


def _candidate_node(pack: Mapping[str, Any]) -> Mapping[str, Any]:
    nodes = [
        require_mapping(n, "intent_nodes[]") for n in (pack.get("intent_nodes") or [])
        if any(isinstance(s, Mapping) and s.get("resolution") == "candidate_search" for s in (require_mapping(n, "intent_nodes[]").get("slots") or []))
    ]
    if len(nodes) != 1:
        raise ConfigError("the pack must have exactly one node with a candidate search (one ranking configuration)")
    return nodes[0]


def _margin(value: Any) -> str:
    """The margin as the exact fraction the ranking loader reads. A JSON number is its decimal; a bool, a negative or a non-number is refused."""
    if isinstance(value, bool) or not isinstance(value, (int, float, str)):
        raise ConfigError("disambiguation.auto_resolve_margin: expected a number")
    if isinstance(value, float):
        if value != value or value in (float("inf"), float("-inf")) or value < 0:
            raise ConfigError("disambiguation.auto_resolve_margin: expected a number from 0 to 1")
        exact = Fraction(Decimal(repr(value)))
    else:
        exact = exact_number(value, "disambiguation.auto_resolve_margin")
    return f"{exact.numerator}/{exact.denominator}"


def _matcher(node: Mapping[str, Any], pack_dir: Path) -> RecipientMatcher:
    ref = require_str(node, "matcher_ref", "disambiguation")
    base = pack_dir.resolve()
    target = (base / ref).resolve()
    if Path(ref).is_absolute() or not target.is_relative_to(base):
        raise ConfigError("disambiguation.matcher_ref: the file must be inside the pack's folder")
    return RecipientMatcher.from_file(target)


def load_pipeline_config(pack: Mapping[str, Any], pack_dir: Path, *, out_of_scope_node: str) -> PipelineConfig:
    """The pipeline's configuration from the loaded pack JSON and the folder it came from. Raises ConfigError for anything missing, repeated, unknown or out of shape."""
    src = require_mapping(pack, "pack")
    block = require_mapping(src.get("understanding"), "understanding")
    reject_unknown(block, _MEMBERS | _OPTIONAL_MEMBERS, "understanding")
    missing = sorted(_MEMBERS - set(block))
    if missing:
        raise ConfigError(f"understanding: missing {missing}")
    node = _candidate_node(src)
    disambiguation = require_mapping(node.get("disambiguation"), "disambiguation")
    ranking = dict(require_mapping(block["ranking"], "understanding.ranking"))
    if any(key in ranking for key in _MARGIN_KEYS):
        raise ConfigError("understanding.ranking: the margin and the limit live in the node's disambiguation, not here")
    if "auto_resolve_margin" not in disambiguation:
        raise ConfigError("disambiguation.auto_resolve_margin: missing")
    ranking["auto_resolve_margin"] = _margin(disambiguation["auto_resolve_margin"])
    ranking["max_candidates_before_escalate"] = require_int(disambiguation, "max_candidates_before_escalate", "disambiguation", minimum=1)
    return PipelineConfig(
        catalogue=NodeCatalogue.from_pack(src, out_of_scope_node=out_of_scope_node),
        ranking=RankingConfig.from_mapping(ranking),
        matcher=_matcher(disambiguation, pack_dir),
        proposable_flags=_proposable_flags(block),
        locales=_locales(block),
        material_flags=_material_flags(block),
        intent_unclear_flag=_flag(block["intent_unclear_flag"], "understanding.intent_unclear_flag"),
        out_of_scope_flag=_flag(block["out_of_scope_flag"], "understanding.out_of_scope_flag"),
        native_yes_threshold=None,
        state_flags=_state_flags(block),
        slot_gates=_slot_gates(block, src),
    )
