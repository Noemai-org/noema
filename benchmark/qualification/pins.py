"""What counts as a pinned model or speech version (UND-13; REQ-INT-006, AGENT-28). Shared by the qualification record and the drift run record."""
from typing import Any

KINDS = ("llm", "speech")
ALIASES = frozenset({"", "latest", "default", "current", "stable", "auto", "*"})


def is_pinned(version: Any) -> bool:
    """A pinned version is a dated or numbered identifier, not an alias that the provider can move."""
    if not isinstance(version, str) or version != version.strip() or any(ch.isspace() for ch in version):
        return False
    lowered = version.lower()
    return lowered not in ALIASES and "latest" not in lowered
