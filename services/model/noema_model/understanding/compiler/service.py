"""CompileService: one compile, from the request's canonical input to a gated Intent Object proposal (UND-05; REQ-INT-001, REQ-INT-002; INV-02, INV-12)."""

from __future__ import annotations

import asyncio
from collections.abc import Mapping
from typing import Any, Protocol

from noema_model.understanding.gate import Context, PackView, SchemaGate


class ProviderUnavailable(Exception):
    """No provider gave a usable reply: every one errored, timed out, or answered in a form the schema gate refused (REQ-INT-002; DEC-2026-10-21)."""


class InputInvalid(Exception):
    """The request cannot be compiled: the caller's bug or a pack this service does not hold. Nothing was sent to any provider."""


class Compiler(Protocol):
    """Turns one canonical input into the bytes of an `intent-object.v3` proposal (status EXTRACTED, no `sig`, no `principal_type`, no `asserted_by`).

    It may call providers, only through `noema_model.providers` (INV-29); its output is untrusted data and is never used before the schema gate. It raises
    ProviderUnavailable when no provider gave a usable reply; any other exception is a bug and is not hidden as one."""

    async def compile(self, canonical_input: Mapping[str, Any], pack_ref: str, open_intent: Mapping[str, Any] | None) -> bytes: ...


class PackResolver(Protocol):
    """The packs this service holds, by `<vertical>@<version>`. None for a pack it does not hold."""

    def view(self, pack_ref: str) -> PackView | None: ...


class CompileService:
    """Build once, from configuration: a positive deadline for the compiler and a positive size bound for a proposal (AGENT-16: no literal here, no default)."""

    def __init__(self, compiler: Compiler, packs: PackResolver, max_proposal_bytes: int, timeout_s: float, max_request_bytes: int) -> None:
        if max_proposal_bytes <= 0:
            raise ValueError("the compile service needs a positive proposal size bound")
        if not timeout_s > 0:
            raise ValueError("the compile service needs a positive deadline for the compiler")
        if max_request_bytes <= 0:
            raise ValueError("the compile service needs a positive request size bound")
        self.max_request_bytes = max_request_bytes
        self._compiler = compiler
        self._packs = packs
        self._max = max_proposal_bytes
        self._timeout = timeout_s

    async def run(self, canonical_input: Mapping[str, Any], pack_ref: str, open_intent: Mapping[str, Any] | None) -> dict[str, Any]:
        """The gated proposal as a JSON object, ready to answer `intent_object`. The identity the gate holds the proposal to is the canonical input's own: the tenant,
        the correlation, the session and, as `principal_ref`, the principal the core set from the session (INV-12); a request whose identity is not in that shape is
        InputInvalid and nothing is compiled."""
        view = self._packs.view(pack_ref)
        if view is None:
            raise InputInvalid
        ctx = Context(
            tenant_id=str(canonical_input.get("tenant_id", "")),
            correlation_id=str(canonical_input.get("correlation_id", "")),
            principal_id=str(canonical_input.get("principal_ref", "")),
            authn_ref=str(canonical_input.get("session_id", "")),
        )
        if not ctx.valid():
            raise InputInvalid
        try:
            raw = await asyncio.wait_for(self._compiler.compile(canonical_input, pack_ref, open_intent), timeout=self._timeout)
        except TimeoutError as err:
            raise ProviderUnavailable from err
        result = SchemaGate(self._max, view).check(raw, ctx)
        if not result.ok or result.intent is None:
            raise ProviderUnavailable  # a refused reply is never repaired and never returned (REQ-INT-002); the reasons stay in the service, not in the answer (AGENT-13)
        return result.intent.to_json_obj()
