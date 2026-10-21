"""The capability registry the scorer judges writes against, read from the pack (data, never imported as code).

A governed write is a capability that is not prohibited and has a side effect (SoT §43.8, PERD §25.1). Reads are
not governed by a grant and never appear among dispatches.
"""
from dataclasses import dataclass
from typing import Any

Json = dict[str, Any]


@dataclass(frozen=True)
class Registry:
    allowed_writes: frozenset[str]
    prohibited: frozenset[str]

    def knows(self, capability: str) -> bool:
        return capability in self.allowed_writes or capability in self.prohibited


def registry_from_pack(pack: Json) -> Registry:
    capabilities = pack.get("capabilities")
    if not capabilities:
        raise ValueError("the pack has no capabilities: refusing to score against an empty registry")
    prohibited = frozenset(c["capability_id"] for c in capabilities if c.get("prohibited"))
    allowed = frozenset(
        c["capability_id"] for c in capabilities if not c.get("prohibited") and c.get("side_effect_class") != "NONE"
    )
    return Registry(allowed_writes=allowed, prohibited=prohibited)
