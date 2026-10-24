"""The harness configuration, read strictly from data. Nothing is defaulted: a missing value is a ConfigError (AGENT-16).

Providers are named endpoints: a kind, an endpoint, a pinned model version, a timeout. The harness holds no code that
knows of any particular endpoint, so switching a default provider, or pointing a name at a hostile test endpoint, is a
configuration change (REQ-INT-002).
"""

import re
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import date
from pathlib import Path
from types import MappingProxyType
from typing import Any

from noema_model.harness.accounting import RateCard
from noema_model.platform.config import (
    ConfigError,
    reject_unknown,
    require_int,
    require_mapping,
    require_str,
)
from noema_model.providers.llm.base import AdapterSettings, DecisionProvider, LLMProvider, Transport
from noema_model.providers.llm.registry import KINDS, NATIVE_KINDS

_NAME = re.compile(r"^[a-z][a-z0-9_]{0,31}$")
_PIN = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$")
_ALIASES = ("latest", "default", "auto", "stable", "preview", "current", "newest")
_LOCALE = re.compile(r"^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$")


def check_pinned(model_version: str) -> None:
    """A pin names one snapshot. An alias or a floating tag is refused here, and a response that reports another
    version than the pin is refused at call time (the harness never accepts an unpinned model version)."""
    lowered = model_version.lower()
    if not _PIN.match(model_version) or not any(c.isdigit() for c in model_version):
        raise ConfigError("model_version must name one pinned snapshot")
    if any(alias in lowered for alias in _ALIASES):
        raise ConfigError("model_version must not be a floating alias")


@dataclass(frozen=True, slots=True)
class ProviderConfig:
    name: str
    kind: str
    endpoint: str
    model_version: str
    credential_file: str
    api_version: str | None
    timeout_ms: int
    max_output_tokens: int | None
    max_response_bytes: int
    temperature: float | None
    rate: RateCard | None


@dataclass(frozen=True, slots=True)
class BreakerConfig:
    failure_threshold: int
    cooldown_ms: int


@dataclass(frozen=True, slots=True)
class DataLimits:
    default_max_chars: int
    overrides: Mapping[str, int]

    def cap_for(self, field_name: str) -> int:
        return self.overrides.get(field_name, self.default_max_chars)


@dataclass(frozen=True, slots=True)
class Route:
    default: tuple[str, ...]
    by_locale: Mapping[str, tuple[str, ...]]

    def providers_for(self, locale: str | None) -> tuple[str, ...]:
        if locale is not None and locale in self.by_locale:
            return self.by_locale[locale]
        return self.default


@dataclass(frozen=True, slots=True)
class HarnessConfig:
    account_run_min_digits: int
    data_limits: DataLimits
    breaker: BreakerConfig
    providers: Mapping[str, ProviderConfig]
    routes: Mapping[str, Route]


def _provider(name: str, raw: Any) -> ProviderConfig:
    where = f"providers.{name}"
    src = require_mapping(raw, where)
    reject_unknown(
        src,
        frozenset({"kind", "endpoint", "model_version", "credential_file", "api_version", "timeout_ms",
                   "max_output_tokens", "max_response_bytes", "temperature", "rate"}),
        where,
    )
    if not _NAME.match(name):
        raise ConfigError(f"{where}: provider names are lower-case identifiers")
    kind = require_str(src, "kind", where)
    native = kind in NATIVE_KINDS
    if kind not in KINDS and not native:
        raise ConfigError(f"{where}.kind: unknown kind")
    endpoint = require_str(src, "endpoint", where)
    if not endpoint.startswith("https://"):
        raise ConfigError(f"{where}.endpoint: must be https")
    model_version = require_str(src, "model_version", where)
    check_pinned(model_version)
    api_version = src.get("api_version")
    if api_version is not None and (not isinstance(api_version, str) or not api_version):
        raise ConfigError(f"{where}.api_version: expected a non-empty string")
    if kind == "anthropic_messages" and api_version is None:
        raise ConfigError(f"{where}.api_version: required for anthropic_messages")
    if native and ("max_output_tokens" in src or "temperature" in src):
        raise ConfigError(f"{where}: a decision model has no max_output_tokens or temperature")
    temperature = src.get("temperature")
    if temperature is not None and (isinstance(temperature, bool) or not isinstance(temperature, (int, float))):
        raise ConfigError(f"{where}.temperature: expected a number")
    rate = None
    if "rate" in src:
        rate = _rate(src["rate"], f"{where}.rate")
    return ProviderConfig(
        name=name,
        kind=kind,
        endpoint=endpoint,
        model_version=model_version,
        credential_file=require_str(src, "credential_file", where),
        api_version=api_version,
        timeout_ms=require_int(src, "timeout_ms", where, minimum=1),
        max_output_tokens=None if native else require_int(src, "max_output_tokens", where, minimum=1),
        max_response_bytes=require_int(src, "max_response_bytes", where, minimum=1),
        temperature=None if temperature is None else float(temperature),
        rate=rate,
    )


def _rate(raw: Any, where: str) -> RateCard:
    src = require_mapping(raw, where)
    reject_unknown(src, frozenset({"input_micro_per_mtok", "output_micro_per_mtok", "currency", "as_of"}), where)
    as_of = require_str(src, "as_of", where)
    try:
        date.fromisoformat(as_of)
    except ValueError as exc:
        raise ConfigError(f"{where}.as_of: expected an ISO date") from exc
    return RateCard(
        input_micro_per_mtok=require_int(src, "input_micro_per_mtok", where, minimum=0),
        output_micro_per_mtok=require_int(src, "output_micro_per_mtok", where, minimum=0),
        currency=require_str(src, "currency", where),
        as_of=as_of,
    )


def _names(raw: Any, providers: Mapping[str, ProviderConfig], where: str) -> tuple[str, ...]:
    if not isinstance(raw, list) or not raw or not all(isinstance(n, str) for n in raw):
        raise ConfigError(f"{where}: expected a non-empty list of provider names")
    if len(set(raw)) != len(raw):
        raise ConfigError(f"{where}: a provider appears twice")
    unknown = [n for n in raw if n not in providers]
    if unknown:
        raise ConfigError(f"{where}: unknown providers {unknown}")
    return tuple(raw)


def load_harness_config(raw: Any) -> HarnessConfig:
    src = require_mapping(raw, "config")
    reject_unknown(src, frozenset({"masking", "data_limits", "breaker", "providers", "routes"}), "config")

    masking = require_mapping(src.get("masking"), "masking")
    reject_unknown(masking, frozenset({"account_run_min_digits"}), "masking")
    min_digits = require_int(masking, "account_run_min_digits", "masking", minimum=5)

    limits = require_mapping(src.get("data_limits"), "data_limits")
    reject_unknown(limits, frozenset({"default_max_chars", "overrides"}), "data_limits")
    overrides_raw = require_mapping(limits.get("overrides"), "data_limits.overrides")
    overrides: dict[str, int] = {}
    for key in overrides_raw:
        overrides[key] = require_int(overrides_raw, key, "data_limits.overrides", minimum=1)

    breaker = require_mapping(src.get("breaker"), "breaker")
    reject_unknown(breaker, frozenset({"failure_threshold", "cooldown_ms"}), "breaker")

    providers_raw = require_mapping(src.get("providers"), "providers")
    providers = {name: _provider(name, body) for name, body in providers_raw.items()}
    if not providers:
        raise ConfigError("providers: at least one provider is required")

    routes: dict[str, Route] = {}
    for route_name, body in require_mapping(src.get("routes"), "routes").items():
        where = f"routes.{route_name}"
        route_src = require_mapping(body, where)
        reject_unknown(route_src, frozenset({"default", "by_locale"}), where)
        by_locale_raw = require_mapping(route_src.get("by_locale"), f"{where}.by_locale")
        by_locale: dict[str, tuple[str, ...]] = {}
        for locale, names in by_locale_raw.items():
            if not _LOCALE.match(locale):
                raise ConfigError(f"{where}.by_locale: bad locale key")
            by_locale[locale] = _names(names, providers, f"{where}.by_locale.{locale}")
        routes[route_name] = Route(
            default=_names(route_src.get("default"), providers, f"{where}.default"),
            by_locale=MappingProxyType(by_locale),
        )

    return HarnessConfig(
        account_run_min_digits=min_digits,
        data_limits=DataLimits(
            default_max_chars=require_int(limits, "default_max_chars", "data_limits", minimum=1),
            overrides=MappingProxyType(overrides),
        ),
        breaker=BreakerConfig(
            failure_threshold=require_int(breaker, "failure_threshold", "breaker", minimum=1),
            cooldown_ms=require_int(breaker, "cooldown_ms", "breaker", minimum=1),
        ),
        providers=MappingProxyType(providers),
        routes=MappingProxyType(routes),
    )


def build_providers(config: HarnessConfig, transport: Transport) -> dict[str, LLMProvider | DecisionProvider]:
    """Instantiate each configured provider. Credentials are read from their mounted files here and held only by the
    adapter (INV-29). A missing or empty credential file is a ConfigError, so a provider without a key never starts."""
    built: dict[str, LLMProvider | DecisionProvider] = {}
    for name, cfg in config.providers.items():
        try:
            credential = Path(cfg.credential_file).read_text(encoding="utf-8").strip()
        except OSError as exc:
            raise ConfigError(f"providers.{name}.credential_file: unreadable") from exc
        if not credential:
            raise ConfigError(f"providers.{name}.credential_file: empty")
        settings = AdapterSettings(
            endpoint=cfg.endpoint,
            credential=credential,
            api_version=cfg.api_version,
            timeout_ms=cfg.timeout_ms,
            max_response_bytes=cfg.max_response_bytes,
        )
        built[name] = (NATIVE_KINDS[cfg.kind] if cfg.kind in NATIVE_KINDS else KINDS[cfg.kind])(name, settings, transport)
    return built
