"""Builds the service's Harness from its mounted configuration (the composition step of REQ-INT-002; INV-29).

The configuration is one JSON file named by NOEMA_MODEL_HARNESS_CONFIG (the provider transport's worker count comes from
NOEMA_MODEL_LLM_POOL_SIZE; both are required, and nothing here defaults), in the shape `load_harness_config` reads. The longest unmasked digit run is not in that
file: it is the value of the pack's masking-limits.v1 document, the one the core reads, named by NOEMA_MODEL_MASKING_LIMITS_FILE (required; a missing, unreadable or
wrong document leaves no harness). Every
provider, pinned model version, route and rate is a value in that file; this module holds none of them (AGENT-16). It reads
credentials only through `build_providers`, from the files the configuration names.

Anything wrong leaves no harness and a reason: a half-built harness is never returned. The reason carries the configuration
path of the fault, never a value from the file and never a credential.
"""

import json
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path

from noema_model.harness.config import build_providers, load_harness_config
from noema_model.harness.core import Harness
from noema_model.platform.clock import Clock
from noema_model.platform.config import ConfigError, require_env_int
from noema_model.platform.masking import MaskingLimitsError, load_limits_file
from noema_model.platform.telemetry import Telemetry
from noema_model.providers.llm.base import Transport
from noema_model.providers.llm.transport import UrllibTransport

ENV_CONFIG = "NOEMA_MODEL_HARNESS_CONFIG"
ENV_LLM_POOL = "NOEMA_MODEL_LLM_POOL_SIZE"
ENV_MASKING_LIMITS = "NOEMA_MODEL_MASKING_LIMITS_FILE"  # the pack's masking_limits.json (masking-limits.v1): the one source of the longest unmasked digit run (A154, A162)


@dataclass(frozen=True, slots=True)
class Assembly:
    harness: Harness | None
    reason: str  # empty when the harness was built


def _refused(reason: str, telemetry: Telemetry) -> Assembly:
    telemetry.event("HARNESS_CONFIG_REJECTED", "service-start", {"reason": reason})
    return Assembly(None, reason)


def assemble_harness(
    environ: Mapping[str, str], clock: Clock, telemetry: Telemetry, transport: Transport | None = None
) -> Assembly:
    path = environ.get(ENV_CONFIG, "")
    if not path:
        return Assembly(None, f"{ENV_CONFIG} is not set")
    try:
        text = Path(path).read_text(encoding="utf-8")
    except OSError:
        return _refused("harness configuration file: unreadable", telemetry)
    limits_path = environ.get(ENV_MASKING_LIMITS, "")
    if not limits_path:
        return _refused(f"{ENV_MASKING_LIMITS} is not set", telemetry)
    try:
        masking = load_limits_file(Path(limits_path))
    except (OSError, MaskingLimitsError):  # the reason names the fault, never the document
        return _refused("masking limits file: unreadable or not masking-limits.v1", telemetry)
    try:
        config = load_harness_config(json.loads(text))
        if transport is None:
            # The pool size is configuration the service must be given: a missing or invalid one refuses to start (AGENT-16).
            workers = require_env_int(environ, ENV_LLM_POOL, minimum=1)
            transport = UrllibTransport(pool_size=workers)
            telemetry.event("TRANSPORT_POOL_CONFIGURED", "service-start", {"transport": "llm", "workers": workers})
        providers = build_providers(config, transport)
    except json.JSONDecodeError as exc:
        return _refused(f"harness configuration file: not valid JSON (line {exc.lineno})", telemetry)
    except ConfigError as exc:
        return _refused(f"harness configuration: {exc}", telemetry)
    return Assembly(Harness(config, providers, clock, telemetry, masking=masking), "")
