"""SchemaGate: the compiler's proposal in, a VALIDATED Intent Object or a closed list of reasons out."""

from __future__ import annotations

import json
import re
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import datetime
from enum import StrEnum
from typing import Any, Protocol

from pydantic import ValidationError

from noema_model.contracts.common_v1 import CommonV1Sig
from noema_model.contracts.enums_v1 import EnumsV1IntentStatus, EnumsV1Mode
from noema_model.contracts.intent_object_v1 import IntentObjectV1
from noema_model.contracts.intent_slot_v1 import (
    IntentSlotV1PartySlot,
    IntentSlotV1QuantitySlot,
    IntentSlotV1ReferenceSlot,
    IntentSlotV1ResourceSlot,
    IntentSlotV1SlotStatus,
    IntentSlotV1TimeRefSlot,
)

SCHEMA_VERSION = "1.0"
MAX_SAFE_INT = 9007199254740991
MAX_RAW_REF = 256

# The signature slot of a validated intent. The contract requires `sig`; the gate runs before the model service signs, so it holds a marker that no verifier
# accepts as a signature. Whoever signs replaces it.
UNSIGNED = CommonV1Sig(key_id="unsigned", value="unsigned")

_TENANT = re.compile(r"ten_[0-9A-Za-z]{2,32}")
_CORRELATION = re.compile(r"corr_[0-9A-Za-z]{2,32}")
_PRINCIPAL = re.compile(r"usr_[0-9A-Za-z]{2,32}")
_SESSION = re.compile(r"ses_[0-9A-Za-z]{2,32}")
_SLOT_NAME = re.compile(r"[a-z][a-z0-9_]*")
_INTENT_REF = re.compile(r"(int_[0-9A-Za-z]{2,32})@([0-9]+)")


class Reason(StrEnum):
    """Why a proposal was refused. A closed list; a reason never carries text of the object (AGENT-13). The names follow the core gate's (noema#453)."""

    SIZE = "SIZE"
    NOT_JSON = "NOT_JSON"
    SHAPE = "SHAPE"  # an unknown, repeated, missing or null member, a wrong type, or a value outside its pattern or range
    CONTEXT_INVALID = "CONTEXT_INVALID"  # the caller's identity context is incomplete: a bug of the caller, never the model's
    STATUS_NOT_EXTRACTED = "STATUS_NOT_EXTRACTED"
    SCHEMA_VERSION = "SCHEMA_VERSION"
    VERSION = "VERSION"
    SUPERSEDES = "SUPERSEDES"
    CREATED_AT = "CREATED_AT"
    IDENTITY_MISMATCH = "IDENTITY_MISMATCH"
    CORRELATION_MISMATCH = "CORRELATION_MISMATCH"
    MODE_NOT_ENABLED = "MODE_NOT_ENABLED"
    SOURCE = "SOURCE"
    PACK_MISMATCH = "PACK_MISMATCH"
    SIG_SET_BY_MODEL = "SIG_SET_BY_MODEL"
    AMBIGUITY_FLAG = "AMBIGUITY_FLAG"
    UNKNOWN_NODE = "UNKNOWN_NODE"
    SLOT_NAME = "SLOT_NAME"
    UNKNOWN_SLOT = "UNKNOWN_SLOT"
    SLOT_TYPE = "SLOT_TYPE"
    SLOT_INVALID = "SLOT_INVALID"
    OUTCOME_NOT_IN_NODE = "OUTCOME_NOT_IN_NODE"


@dataclass(frozen=True)
class Context:
    """What the canonical input says the proposal must be about: the session's tenant and principal, the session id and the correlation id (INV-12). The model
    service cannot know `principal_type` or `asserted_by`, so they are not compared (the open point of noema#484)."""

    tenant_id: str
    correlation_id: str
    principal_id: str
    authn_ref: str

    def valid(self) -> bool:
        return bool(
            _TENANT.fullmatch(self.tenant_id)
            and _CORRELATION.fullmatch(self.correlation_id)
            and _PRINCIPAL.fullmatch(self.principal_id)
            and _SESSION.fullmatch(self.authn_ref)
        )


@dataclass(frozen=True)
class Node:
    """What the pack says about one intent node: its slots by name and type, and its outcome definition ids."""

    slots: Mapping[str, str]
    outcomes: frozenset[str]


class PackView(Protocol):
    """The loaded pack as the gate needs it. The caller adapts the pack it loaded; the gate reads no file."""

    def ref(self) -> tuple[str, str]: ...

    def node(self, node_id: str) -> Node | None: ...


@dataclass(frozen=True)
class GateResult:
    """ok with the VALIDATED object, or not ok with reasons (sorted, unique, at least one) and no object."""

    ok: bool
    reasons: tuple[Reason, ...]
    intent: IntentObjectV1 | None


class _Repeated(Exception):
    """A member named twice in one object."""


def _pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    for key, value in pairs:
        if key in out:
            raise _Repeated
        out[key] = value
    return out


def _constant(name: str) -> Any:
    raise ValueError(name)  # NaN, Infinity and -Infinity are not JSON


def _refuse(*reasons: Reason) -> GateResult:
    return GateResult(False, tuple(sorted(set(reasons), key=lambda r: r.value)), None)


def _instant(text: str) -> bool:
    try:
        datetime.fromisoformat(text)
    except ValueError:
        return False
    return True


class SchemaGate:
    """Build once with a positive size bound from configuration (AGENT-16: no literal here) and the loaded pack."""

    def __init__(self, max_bytes: int, pack: PackView) -> None:
        if max_bytes <= 0:
            raise ValueError("the schema gate needs a positive size bound")
        self._max = max_bytes
        self._pack = pack

    def check(self, raw: bytes, ctx: Context) -> GateResult:
        """Total: any bytes give a GateResult, never an exception."""
        if not ctx.valid():
            return _refuse(Reason.CONTEXT_INVALID)
        if len(raw) == 0 or len(raw) > self._max:
            return _refuse(Reason.SIZE)
        try:
            doc = json.loads(raw.decode("utf-8"), object_pairs_hook=_pairs, parse_constant=_constant)
        except _Repeated:
            return _refuse(Reason.SHAPE)
        except (ValueError, RecursionError):  # includes a bad UTF-8 sequence, trailing data, NaN, and an integer too long to read
            return _refuse(Reason.NOT_JSON)
        if not isinstance(doc, dict):
            return _refuse(Reason.SHAPE)
        reasons: set[Reason] = set()
        if "sig" in doc:
            reasons.add(Reason.SIG_SET_BY_MODEL)  # the model service signs, after the gate; a model that signs is refused
        doc["sig"] = {"key_id": UNSIGNED.key_id, "value": UNSIGNED.value}
        try:
            obj = IntentObjectV1.from_json(json.dumps(doc, allow_nan=False))
        except (ValidationError, ValueError, RecursionError):
            return _refuse(Reason.SHAPE)
        self._rules(obj, ctx, reasons)
        if reasons:
            return _refuse(*reasons)
        return GateResult(True, (), obj.model_copy(update={"status": EnumsV1IntentStatus.VALIDATED}))

    def _rules(self, o: IntentObjectV1, ctx: Context, r: set[Reason]) -> None:
        if o.status != EnumsV1IntentStatus.EXTRACTED:
            r.add(Reason.STATUS_NOT_EXTRACTED)
        if o.schema_version != SCHEMA_VERSION:
            r.add(Reason.SCHEMA_VERSION)
        self._chain(o, r)
        if not _instant(o.created_at):
            r.add(Reason.CREATED_AT)
        if o.tenant_id != ctx.tenant_id or o.principal.principal_id != ctx.principal_id or o.principal.authn_ref != ctx.authn_ref:
            r.add(Reason.IDENTITY_MISMATCH)
        if o.correlation_id != ctx.correlation_id:
            r.add(Reason.CORRELATION_MISMATCH)
        self._source(o, r)
        pack_id, pack_version = self._pack.ref()
        if o.pack.pack_id != pack_id or o.pack.pack_version != pack_version:
            r.add(Reason.PACK_MISMATCH)
        if len(set(o.ambiguity_flags)) != len(o.ambiguity_flags):
            r.add(Reason.AMBIGUITY_FLAG)
        node = self._pack.node(o.intent_node)
        if node is None:
            r.add(Reason.UNKNOWN_NODE)
        self._slots(o, node, r)
        if len(set(o.desired_outcome_refs)) != len(o.desired_outcome_refs) or (node is not None and any(x not in node.outcomes for x in o.desired_outcome_refs)):
            r.add(Reason.OUTCOME_NOT_IN_NODE)

    @staticmethod
    def _chain(o: IntentObjectV1, r: set[Reason]) -> None:
        if o.version < 1 or o.version > MAX_SAFE_INT:
            r.add(Reason.VERSION)
            return
        if o.version == 1:
            if o.supersedes is not None:
                r.add(Reason.SUPERSEDES)
            return
        m = _INTENT_REF.fullmatch(o.supersedes) if o.supersedes is not None else None
        if m is None or m.group(1) != o.intent_id or int(m.group(2)) != o.version - 1:
            r.add(Reason.SUPERSEDES)

    @staticmethod
    def _source(o: IntentObjectV1, r: set[Reason]) -> None:
        if o.source.mode == EnumsV1Mode.MODE_2:
            r.add(Reason.MODE_NOT_ENABLED)
        elif o.source.mode != EnumsV1Mode.MODE_1:
            r.add(Reason.SOURCE)
        if len(o.source.raw_ref) > MAX_RAW_REF:
            r.add(Reason.SOURCE)

    @staticmethod
    def _slots(o: IntentObjectV1, node: Node | None, r: set[Reason]) -> None:
        for name in sorted(o.slots):
            slot = o.slots[name]
            if not _SLOT_NAME.fullmatch(name):
                r.add(Reason.SLOT_NAME)
                continue
            if node is not None:
                want = node.slots.get(name)
                if want is None:
                    r.add(Reason.UNKNOWN_SLOT)
                    continue
                if want != slot.type:
                    r.add(Reason.SLOT_TYPE)
                    continue
            if not _slot_valid(slot):
                r.add(Reason.SLOT_INVALID)


def _slot_valid(slot: Any) -> bool:
    """The rules of intent-slot.v1 that the generated closed shape does not carry."""
    if isinstance(slot, IntentSlotV1ResourceSlot):
        resolved = slot.resolved_ref is not None
        return resolved == (slot.status == IntentSlotV1SlotStatus.RESOLVED)  # RESOLVED only with a resolved_ref, and a resolved_ref only when RESOLVED (INV-06)
    if isinstance(slot, IntentSlotV1TimeRefSlot):
        window = slot.window
        return window is None or (len(window) == 2 and all(_instant(w) for w in window))
    return isinstance(slot, (IntentSlotV1QuantitySlot, IntentSlotV1PartySlot, IntentSlotV1ReferenceSlot))
