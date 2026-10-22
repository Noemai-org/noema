"""Versioned configuration loading for the model service: provider defaults, timeouts and limits as data, never in code.

Implements: REQ-OPS-003.

May import: the standard library and declared dependencies.

Must never: embed a threshold, TTL or provider default in code (AGENT-16, AGENT-28).

This module holds only the strict readers every config parser uses. A missing field, a wrong type or an unknown key
is a ConfigError: nothing is defaulted, because a default here would be a threshold written in code.
"""

from collections.abc import Mapping
from typing import Any


class ConfigError(ValueError):
    """The configuration is missing, malformed or unsafe. The service must not start on it."""


def require_mapping(source: Any, where: str) -> Mapping[str, Any]:
    if not isinstance(source, Mapping):
        raise ConfigError(f"{where}: expected an object")
    return source


def reject_unknown(source: Mapping[str, Any], allowed: frozenset[str], where: str) -> None:
    extra = sorted(set(source) - allowed)
    if extra:
        raise ConfigError(f"{where}: unknown keys {extra}")


def require_str(source: Mapping[str, Any], key: str, where: str) -> str:
    value = source.get(key)
    if not isinstance(value, str) or not value:
        raise ConfigError(f"{where}.{key}: expected a non-empty string")
    return value


def require_int(source: Mapping[str, Any], key: str, where: str, *, minimum: int) -> int:
    value = source.get(key)
    if isinstance(value, bool) or not isinstance(value, int) or value < minimum:
        raise ConfigError(f"{where}.{key}: expected an integer of at least {minimum}")
    return value


def require_bool(source: Mapping[str, Any], key: str, where: str) -> bool:
    value = source.get(key)
    if not isinstance(value, bool):
        raise ConfigError(f"{where}.{key}: expected true or false")
    return value
