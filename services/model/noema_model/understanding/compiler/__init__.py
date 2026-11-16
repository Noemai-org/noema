"""Compile canonical input into an Intent Object proposal using the pack's Intent Taxonomy; every proposal crosses the schema gate.

Implements: REQ-INT-001, REQ-INT-002, REQ-INT-003, REQ-INT-004, REQ-INT-005.

May import: noema_model.platform, noema_model.harness, noema_model.contracts and noema_model.understanding.gate (the model service's side of the schema gate).

Must never: act; hold credentials; see policy rules or thresholds; set identity; compile to a node the pack does not define.

What is built (UND-05, slice 1): the compile route `POST /internal/v1/understanding:compile` and the `CompileService` it calls. The route is the seam the core's client
(the core's compile client) speaks to: it validates the request, calls a `Compiler`, holds the reply to the schema gate and answers `{intent_object, canonical_input}`
or an errors.v2 envelope. The compiler (`compiler.py`: `PipelineCompiler`) joins the pipeline, the lexicon and the assembler behind the `Compiler` protocol; the route is complete without it, and it is not wired in `main.py`.
"""

from noema_model.understanding.compiler.service import (
    Compiler,
    CompileService,
    InputInvalid,
    PackResolver,
    ProviderUnavailable,
)

__all__ = ["CompileService", "Compiler", "InputInvalid", "PackResolver", "ProviderUnavailable"]  # PipelineCompiler is imported from .compiler (it needs the pipeline and the lexicon)
