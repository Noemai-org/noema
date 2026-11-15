"""Normalise text or a final transcript into the canonical input.

Implements: REQ-INP-001, REQ-INP-002, REQ-VOI-001.

May import: noema_model.platform, noema_model.contracts.

Must never: hold business logic; decide; pass a partial transcript as final; assert identity (INV-12); read the wall clock
(it takes a Clock); log or put the text in an error.

Public surface: Gateway.canonicalise (the pure function of a raw body, a RequestContext and a Clock), RequestContext, InputInvalid, Reason.
"""

from noema_model.understanding.gateway.gateway import SCHEMA_VERSION, TEXT_LIMIT, Gateway, InputInvalid, Reason, RequestContext

__all__ = ["SCHEMA_VERSION", "TEXT_LIMIT", "Gateway", "InputInvalid", "Reason", "RequestContext"]
