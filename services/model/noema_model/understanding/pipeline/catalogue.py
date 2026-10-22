"""The pack's node catalogue as the model sees it: IDs, slot schemas and display names, and nothing else.

No policy, threshold, trust profile, outcome definition or capability behaviour is read here (PERD §19.2: a model never
sees policy). The structured-output models for slot values are built once, when the catalogue is built, so the harness
sees the same type object on every call.
"""

import re
from collections.abc import Mapping
from dataclasses import dataclass
from types import MappingProxyType
from typing import Any

from pydantic import BaseModel, ConfigDict, Field, create_model

from noema_model.platform.config import ConfigError, require_mapping, require_str

# What the model is told each slot type means. Fixed text about types, never about a vertical.
_TYPE_HELP: Mapping[str, str] = MappingProxyType(
    {
        "Quantity": "A number the customer mentions, such as an amount of money.",
        "Party": "A person or organisation the customer mentions.",
        "TimeRef": "A time or period the customer mentions.",
    }
)
_ID_PART = re.compile(r"[^a-z0-9_]")


class Mention(BaseModel):
    """One slot as the customer said it. Every field is required, so an absent mention is present=false, not a default."""

    model_config = ConfigDict(extra="forbid", strict=True, frozen=True)
    present: bool = Field(description="True only if the customer actually mentioned it.")
    surface_form: str = Field(description="The customer's exact words for it, or an empty string.")
    proposed_value: str = Field(description="Your reading of it as plain text, or an empty string.")
    confidence: float = Field(allow_inf_nan=False, description="Your confidence between 0 and 1.")


@dataclass(frozen=True, slots=True)
class SlotInfo:
    name: str
    slot_type: str
    required: bool
    vocabulary: tuple[str, ...] | None
    references: str | None
    candidate_search: bool
    resource_kind: str | None


@dataclass(frozen=True, slots=True)
class NodeInfo:
    node_id: str
    display_name: str
    slots: tuple[SlotInfo, ...]
    mention_slots: tuple[str, ...]
    mention_model: type[BaseModel] | None


@dataclass(frozen=True, slots=True)
class NodeCatalogue:
    nodes: Mapping[str, NodeInfo]
    capability_ids: tuple[str, ...]
    out_of_scope_node: str

    @classmethod
    def from_pack(cls, pack: Any, *, out_of_scope_node: str) -> "NodeCatalogue":
        src = require_mapping(pack, "pack")
        nodes: dict[str, NodeInfo] = {}
        for index, raw in enumerate(src.get("intent_nodes") or []):
            where = f"intent_nodes[{index}]"
            node = require_mapping(raw, where)
            node_id = require_str(node, "node_id", where)
            slots = tuple(_slot(s, f"{where}.slots") for s in (node.get("slots") or []))
            mention_slots = tuple(s.name for s in slots if s.slot_type in _TYPE_HELP)
            model = None
            if mention_slots:
                fields: dict[str, Any] = {
                    s.name: (Mention, Field(description=_TYPE_HELP[s.slot_type])) for s in slots if s.name in mention_slots
                }
                model = create_model(
                    "SlotMentions_" + _ID_PART.sub("_", node_id.lower()),
                    __config__=ConfigDict(extra="forbid", strict=True, frozen=True),
                    **fields,
                )
            nodes[node_id] = NodeInfo(node_id, require_str(node, "display_name", where), slots, mention_slots, model)
        if out_of_scope_node not in nodes:
            raise ConfigError("the pack has no out-of-scope node under that id")
        capabilities = tuple(sorted(
            require_str(require_mapping(c, "capabilities"), "capability_id", "capabilities")
            for c in (src.get("capabilities") or [])
        ))
        return cls(nodes=nodes, capability_ids=capabilities, out_of_scope_node=out_of_scope_node)

    def vocabulary_for(self, slot: SlotInfo) -> tuple[str, ...]:
        """The closed set of values a Reference slot may take, in pack order (or sorted, for registry IDs)."""
        if slot.vocabulary is not None:
            return slot.vocabulary
        if slot.references == "capability":
            return self.capability_ids
        raise ConfigError(f"slot {slot.name} has no closed vocabulary")


def _slot(raw: Any, where: str) -> SlotInfo:
    src = require_mapping(raw, where)
    vocabulary = src.get("vocabulary")
    if vocabulary is not None and (not isinstance(vocabulary, list) or not all(isinstance(v, str) and v for v in vocabulary)):
        raise ConfigError(f"{where}.vocabulary: a list of strings")
    return SlotInfo(
        name=require_str(src, "name", where),
        slot_type=require_str(src, "type", where),
        required=src.get("required") is True,
        vocabulary=tuple(vocabulary) if vocabulary is not None else None,
        references=src.get("references") if isinstance(src.get("references"), str) else None,
        candidate_search=src.get("resolution") == "candidate_search",
        resource_kind=src.get("resource_kind") if isinstance(src.get("resource_kind"), str) else None,
    )
