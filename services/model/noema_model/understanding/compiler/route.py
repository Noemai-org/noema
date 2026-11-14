"""The compile route: `POST /internal/v1/understanding:compile` (PERD §39.3; the core's own compile client calls it)."""

from __future__ import annotations

import json
import re
from typing import Any

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from pydantic import ValidationError

from noema_model.contracts.canonical_input_v2 import CanonicalInputV2
from noema_model.contracts.errors_v2 import ErrorsV2ErrorBody, ErrorsV2ErrorCode, ErrorsV2ErrorEnvelope
from noema_model.understanding.compiler.service import CompileService, InputInvalid, ProviderUnavailable

ROUTE = "/internal/v1/understanding:compile"
_PACK_REF = re.compile(r"[a-z][a-z0-9]*@[0-9]+\.[0-9]+\.[0-9]+")
_CORRELATION = re.compile(r"corr_[0-9A-Za-z]{2,32}")
_NO_CORRELATION = "corr_NONE"  # the fixed answer when the request carried none, as the core's own error answers do
_MEMBERS = {"canonical_input", "pack_ref", "open_intent"}


class _Repeated(Exception):
    """A member named twice in one object."""


def _pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    for key, value in pairs:
        if key in out:
            raise _Repeated
        out[key] = value
    return out


def _envelope(status: int, code: ErrorsV2ErrorCode, message: str, retryable: bool, correlation_id: str) -> JSONResponse:
    """An errors.v2 envelope. The message is a fixed sentence: it never quotes the request, the proposal or a reason (AGENT-13)."""
    body = ErrorsV2ErrorEnvelope(error=ErrorsV2ErrorBody(code=code, message=message, retryable=retryable, correlation_id=correlation_id))
    return JSONResponse(body.to_json_obj(), status_code=status)


def _correlation_of(doc: Any) -> str:
    if isinstance(doc, dict) and isinstance(doc.get("canonical_input"), dict):
        value = doc["canonical_input"].get("correlation_id")
        if isinstance(value, str) and _CORRELATION.fullmatch(value):
            return value
    return _NO_CORRELATION


def register(app: FastAPI) -> None:
    """Mount the route on `app`. The service is read from `app.state.compile_service` for each request; with none the route answers 503, never 404."""

    @app.exception_handler(Exception)
    async def _bug(_: Request, __: Exception) -> JSONResponse:
        # A bug in the compiler or the route is not a provider failure and is not hidden as one: it is an internal safety block with no detail. Starlette still re-raises the
        # exception to the server's own log after this answer.
        return _envelope(500, ErrorsV2ErrorCode.INTERNAL_SAFETY_BLOCK, "The request was blocked by an internal check.", False, _NO_CORRELATION)

    @app.post(ROUTE)
    async def compile_route(request: Request) -> JSONResponse:
        service: CompileService | None = getattr(request.app.state, "compile_service", None)
        if service is None:  # nothing to compile with: the body is not read at all (the bound below comes from the service's configuration)
            return _envelope(503, ErrorsV2ErrorCode.DEPENDENCY_UNAVAILABLE, "The compile service is not configured.", True, _NO_CORRELATION)
        limit = service.max_request_bytes
        too_large = _envelope(413, ErrorsV2ErrorCode.INPUT_INVALID, "The compile request is too large.", False, _NO_CORRELATION)
        declared = request.headers.get("content-length")
        if declared is not None:
            if not (declared.isascii() and declared.isdigit()):
                return _envelope(422, ErrorsV2ErrorCode.INPUT_INVALID, "The compile request is not valid.", False, _NO_CORRELATION)
            if int(declared) > limit:
                return too_large  # refused by its declared length: not one byte of it is read
        chunks: list[bytes] = []
        total = 0
        async for chunk in request.stream():  # a body that declares no length (or lies) is cut here, as it arrives
            total += len(chunk)
            if total > limit:
                return too_large
            chunks.append(chunk)
        raw = b"".join(chunks)
        try:
            doc = json.loads(raw.decode("utf-8"), object_pairs_hook=_pairs)
        except (ValueError, RecursionError, _Repeated):  # not JSON, too deep, or a member named twice
            doc = None
        corr = _correlation_of(doc)
        invalid = _envelope(422, ErrorsV2ErrorCode.INPUT_INVALID, "The compile request is not valid.", False, corr)
        if not isinstance(doc, dict) or not {"canonical_input", "pack_ref"} <= set(doc) or set(doc) - _MEMBERS:
            return invalid
        pack_ref, open_intent = doc["pack_ref"], doc.get("open_intent")
        if not isinstance(pack_ref, str) or not _PACK_REF.fullmatch(pack_ref) or (open_intent is not None and not isinstance(open_intent, dict)):
            return invalid
        try:
            canonical = CanonicalInputV2.from_json(json.dumps(doc["canonical_input"])).to_json_obj()
        except (ValidationError, ValueError, RecursionError):
            return invalid
        if not str(canonical.get("schema_version", "")).startswith("2."):
            return invalid  # the contract's schema_version is the generic X.Y shape; this route takes canonical-input major 2 and no other
        try:
            intent_object = await service.run(canonical, pack_ref, open_intent)
        except InputInvalid:
            return invalid
        except ProviderUnavailable:
            return _envelope(502, ErrorsV2ErrorCode.PROVIDER_UNAVAILABLE, "No provider gave a usable answer.", True, corr)
        return JSONResponse({"intent_object": intent_object, "canonical_input": canonical})
