"""The model service's client of core's principal-scoped candidate-read API (REQ-CTX-006), over mutual TLS.

Implements: REQ-CTX-006, REQ-AMB-002, REQ-SEC-002.

May import: noema_model.platform, noema_model.understanding (its data types and errors).

Must never: be imported by the pipeline (the pipeline is given a CandidateSource and imports no HTTP); import providers or
the harness; send a field the contract does not define; use a response that does not match the contract.

`EvidenceClient` implements the pipeline's CandidateSource. This package is not listed in PERD §64.2 (the layout names the
client only as "the core Evidence API client" that the pipeline and resolution may import); it is raised in the PR.
"""

from noema_model.evidence.client import (
    CandidateResponseInvalid,
    EvidenceClient,
    EvidenceClientConfig,
    build_request,
    parse_response,
)
from noema_model.evidence.transport import EvidenceTransport, HttpResponse, UrllibEvidenceTransport

__all__ = (
    "CandidateResponseInvalid", "EvidenceClient", "EvidenceClientConfig", "EvidenceTransport", "HttpResponse",
    "UrllibEvidenceTransport", "build_request", "parse_response",
)
