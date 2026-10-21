"""All archetypes, by PERD §48.1 category. Keys are unique across the library (a key seeds its worlds)."""
from .archetype import Arc
from .lib_clean_ambiguity import AMBIGUITY, CLEAN
from .lib_exec import DUPLICATE, HOSTILITY, TIMEOUT
from .lib_security import AUTH, CLEAN_EXTRA, INJECTION, POLICY, SESSION
from .lib_state import STATE

BY_CATEGORY: dict[str, list[Arc]] = {
    "CLEAN_UNIQUE": [*CLEAN, *CLEAN_EXTRA],
    "AMBIGUITY": AMBIGUITY,
    "STATE_CONFLICT": STATE,
    "TIMEOUT_UNKNOWN": TIMEOUT,
    "DUPLICATE_REPLAY": DUPLICATE,
    "AUTHORISATION": AUTH,
    "POLICY_CLOCK": POLICY,
    "PROMPT_INJECTION": INJECTION,
    "STALE_SESSION": SESSION,
    "INTEGRATION_HOSTILITY": HOSTILITY,
}


def _check() -> None:
    keys = [a.key for arcs in BY_CATEGORY.values() for a in arcs]
    duplicated = sorted({k for k in keys if keys.count(k) > 1})
    if duplicated:
        raise ValueError(f"archetype keys must be unique: {duplicated}")
    for category, arcs in BY_CATEGORY.items():
        wrong = [a.key for a in arcs if a.cat != category]
        if wrong:
            raise ValueError(f"{wrong} are listed under {category} but declare another category")


_check()
