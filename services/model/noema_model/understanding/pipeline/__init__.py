"""Stateless pure-function steps composed as a fixed DAG: normalise input, compile the node and slots, run the schema gate, request candidates, rank deterministically (D3).

Implements: REQ-INT-001, REQ-AMB-003, REQ-NFR-001.

May import: noema_model.understanding.{gateway,compiler,resolution}, noema_model.harness, and the core Evidence API client.

Must never: hold workflow state or module-level mutable state; import a database, Temporal, HTTP or stateful agent-framework client (the pipeline-purity lint); decide; write a clarification question; check policy.

Public surface: run_pipeline, propose_binding, PipelineConfig and the plain data types. The caller supplies the three
seams the pipeline does not own: a CandidateSource (the Evidence API client), a Normalise function (the deterministic
lexicon) and a Gate (the schema gate).
"""

from noema_model.understanding.pipeline.binding import BindingProposal, propose_binding
from noema_model.understanding.pipeline.catalogue import NodeCatalogue
from noema_model.understanding.pipeline.run import build_query, run_pipeline
from noema_model.understanding.pipeline.types import (
    CandidateQuery,
    CandidateRequestInvalid,
    CandidateScopeError,
    CandidateSource,
    CandidateSourceError,
    FlagSpec,
    LocaleOption,
    MentionProposal,
    NormalisedSlots,
    Outcome,
    PipelineConfig,
    PipelineInput,
    PipelineResult,
    Proposal,
)

__all__ = (
    "BindingProposal", "CandidateQuery", "CandidateRequestInvalid", "CandidateScopeError", "CandidateSource",
    "CandidateSourceError", "FlagSpec", "LocaleOption",
    "MentionProposal", "NodeCatalogue", "NormalisedSlots", "Outcome", "PipelineConfig", "PipelineInput",
    "PipelineResult", "Proposal", "build_query", "propose_binding", "run_pipeline",
)
