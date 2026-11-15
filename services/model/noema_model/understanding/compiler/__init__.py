"""Compile canonical input into an Intent Object proposal using the pack's Intent Taxonomy; every proposal crosses the schema gate.

Implements: REQ-INT-001, REQ-INT-002, REQ-INT-003, REQ-INT-004, REQ-INT-005.

May import: noema_model.platform, noema_model.harness, noema_model.contracts and noema_model.understanding.gate (the model service's side of the schema gate).

Must never: act; hold credentials; see policy rules or thresholds; set identity; compile to a node the pack does not define.

What is built (UND-05, slice 1): the compile route `POST /internal/v1/understanding:compile` and the `CompileService` it calls. The route is the seam the core's client
(the core's compile client) speaks to: it validates the request, calls a `Compiler`, holds the reply to the schema gate and answers `{intent_object, canonical_input}`
or an errors.v2 envelope. The compiler itself (prompt templates, the lexicon, the pipeline's assembly of an `intent-object.v3` proposal) is the next slice; the route takes it through
the `Compiler` protocol and is complete without it.
"""

from noema_model.understanding.compiler.service import (
    Compiler,
    CompileService,
    InputInvalid,
    PackResolver,
    ProviderUnavailable,
)

__all__ = ["CompileService", "Compiler", "InputInvalid", "PackResolver", "ProviderUnavailable"]
