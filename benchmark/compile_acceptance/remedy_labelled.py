"""A604 (UND-16, run 7): the reader of the offline labelled set for the `requested_remedy` decision (`remedy_labelled.v1.json`).

The set is SYNTHETIC (AGENT-25), separate from the five goldens and from the held-out set (INV-22), and carries no oracle. It is for measuring the compiler's decision "does the customer explicitly ask
for something to be done for them?" offline, before any further live run. The reader is strict: a file that is not exactly the declared shape is refused, never repaired. It decides nothing and sets no
threshold (AGENT-16)."""
import json
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any

LOCALES = ("en-NG", "pcm")
# A human-readable kind of what an explicit ask is for. It is NOT a capability id: which capability a kind maps to is the pack's business.
ASKS_FOR = ("resend", "refund", "reverse", "escalate", "cancel")

_TOP = {"set_id", "label", "ruling", "purpose", "rule", "held_out", "goldens", "items"}
_ITEM = {"id", "locale", "text", "explicit_ask", "asks_for", "borderline", "why"}


class LabelledSetRefused(ValueError):
    """The file is not the labelled set."""


@dataclass(frozen=True, slots=True)
class Item:
    item_id: str
    locale: str
    text: str
    explicit_ask: bool
    asks_for: str | None
    borderline: bool
    why: str


def _item(raw: Any, at: int) -> Item:
    if not isinstance(raw, Mapping) or set(raw) != _ITEM:
        raise LabelledSetRefused(f"item {at}: not exactly the members {sorted(_ITEM)}")
    if raw["locale"] not in LOCALES:
        raise LabelledSetRefused(f"item {at}: locale is not one of {LOCALES}")
    for name in ("id", "text", "why"):
        if not isinstance(raw[name], str):
            raise LabelledSetRefused(f"item {at}: {name} is not a string")
    if not isinstance(raw["explicit_ask"], bool) or not isinstance(raw["borderline"], bool):
        raise LabelledSetRefused(f"item {at}: explicit_ask and borderline are booleans")
    kind = raw["asks_for"]
    if raw["explicit_ask"] != (kind is not None) or (kind is not None and kind not in ASKS_FOR):
        raise LabelledSetRefused(f"item {at}: an ask names one kind of {ASKS_FOR}; a non-ask names none")
    return Item(raw["id"], raw["locale"], raw["text"], raw["explicit_ask"], kind, raw["borderline"], raw["why"])


def parse(doc: Any) -> tuple[Item, ...]:
    if not isinstance(doc, Mapping) or set(doc) != _TOP:
        raise LabelledSetRefused(f"the file is not exactly the members {sorted(_TOP)}")
    if doc["held_out"] is not False or doc["goldens"] is not False:
        raise LabelledSetRefused("the set must declare that it is neither held-out nor goldens (INV-22)")
    items = doc["items"]
    if not isinstance(items, list) or not items:
        raise LabelledSetRefused("the set has no items")
    return tuple(_item(raw, at) for at, raw in enumerate(items))


def load(path: Path) -> tuple[Item, ...]:
    try:
        doc = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise LabelledSetRefused(f"cannot read the set: {exc.__class__.__name__}") from exc
    return parse(doc)
