"""The speech provider capability matrix, read from capability_matrix.json (VOI-01, REQ-VOI-002, REQ-INT-006).

The matrix is data: a provider's locales, TTS coverage, latency and retention terms are never written in code (AGENT-16).
Nothing is defaulted. A missing cell, an unknown key or an unknown value is a ConfigError and the service does not start,
because a defaulted cell would be a claim nobody checked.
"""

import json
from enum import StrEnum
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from noema_model.platform.config import (
    ConfigError,
    reject_unknown,
    require_bool,
    require_int,
    require_mapping,
    require_str,
)
from noema_model.providers.speech.base import Locale, PipelineMode, ProviderCapabilities, Support

MATRIX_PATH = Path(__file__).with_name("capability_matrix.json")
_TOP_KEYS = frozenset({"matrix_version", "as_of", "basis", "providers"})
_PROVIDER_KEYS = frozenset(
    {"name", "mode", "streaming_asr", "asr_locales", "tts_locales", "latency_ms_p95", "retention_terms",
     "pinned_model_version", "qualified"}
)


def _enum[E: StrEnum](kind: type[E], source: Mapping[str, Any], key: str, where: str) -> E:
    raw = require_str(source, key, where)
    try:
        return kind(raw)
    except ValueError:
        raise ConfigError(f"{where}.{key}: unknown value") from None


def _locales(source: Mapping[str, Any], key: str, where: str) -> tuple[tuple[Locale, Support], ...]:
    cells = require_mapping(source.get(key), f"{where}.{key}")
    reject_unknown(cells, frozenset(item.value for item in Locale), f"{where}.{key}")
    return tuple((locale, _enum(Support, cells, locale.value, f"{where}.{key}")) for locale in Locale)


def _latency(source: Mapping[str, Any], where: str) -> int | None:
    if "latency_ms_p95" not in source:
        raise ConfigError(f"{where}.latency_ms_p95: missing (null is allowed, absent is not)")
    if source["latency_ms_p95"] is None:
        return None
    return require_int(source, "latency_ms_p95", where, minimum=1)


def _pinned(source: Mapping[str, Any], where: str) -> str | None:
    if "pinned_model_version" not in source:
        raise ConfigError(f"{where}.pinned_model_version: missing (null is allowed, absent is not)")
    if source["pinned_model_version"] is None:
        return None
    return require_str(source, "pinned_model_version", where)


def _provider(raw: object, index: int) -> ProviderCapabilities:
    where = f"providers[{index}]"
    source = require_mapping(raw, where)
    reject_unknown(source, _PROVIDER_KEYS, where)
    caps = ProviderCapabilities(
        name=require_str(source, "name", where),
        mode=_enum(PipelineMode, source, "mode", where),
        streaming_asr=_enum(Support, source, "streaming_asr", where),
        asr_locales=_locales(source, "asr_locales", where),
        tts_locales=_locales(source, "tts_locales", where),
        latency_ms_p95=_latency(source, where),
        retention_terms=_enum(Support, source, "retention_terms", where),
        pinned_model_version=_pinned(source, where),
        qualified=require_bool(source, "qualified", where),
    )
    if caps.qualified and (caps.pinned_model_version is None or caps.retention_terms is not Support.YES):
        # REQ-INT-006 and the retention rule of PERD §45.4: no provider is qualified without a pinned version and
        # confirmed data terms.
        raise ConfigError(f"{where}.qualified: a qualified provider needs a pinned model version and confirmed retention terms")
    return caps


def parse_matrix(document: object) -> Mapping[str, ProviderCapabilities]:
    top = require_mapping(document, "matrix")
    reject_unknown(top, _TOP_KEYS, "matrix")
    if require_int(top, "matrix_version", "matrix", minimum=1) != 1:
        raise ConfigError("matrix.matrix_version: unsupported")
    require_str(top, "as_of", "matrix")
    require_str(top, "basis", "matrix")
    rows = top.get("providers")
    if not isinstance(rows, list) or not rows:
        raise ConfigError("matrix.providers: expected a non-empty list")
    parsed = [_provider(row, index) for index, row in enumerate(rows)]
    names = [caps.name for caps in parsed]
    if len(set(names)) != len(names):
        raise ConfigError("matrix.providers: duplicate name")
    return {caps.name: caps for caps in parsed}


def load_matrix(path: Path = MATRIX_PATH) -> Mapping[str, ProviderCapabilities]:
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        raise ConfigError(f"matrix: cannot read {path.name}") from error
    return parse_matrix(document)
