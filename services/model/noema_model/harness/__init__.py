"""The only road to a model: versioned prompt templates, schema-forced output, masking and data-delimiting, per-call accounting, spans, timeouts, ordered failover, a circuit breaker per provider, and the generate and decide interfaces (D2, D9).

Implements: REQ-INT-002, REQ-CTX-007, REQ-NFR-007, REQ-OBS-002, REQ-BEN-005.

May import: noema_model.platform and noema_model.providers (it is the only importer of providers; the harness-only-providers lint).

Must never: repair a mismatching output (reject); turn a probability into a decision; log provider reasoning output; hold code that knows of the evil provider (endpoints are configuration); accept an unpinned model version.

Public surface: Harness (generate, decide), load_harness_config and build_providers, the decide types and the typed
rejection. Everything else in this package is internal to it.
"""

from noema_model.harness.accounting import AttemptOutcome, AttemptRecord, CallRecord, Failover, RateCard
from noema_model.harness.config import HarnessConfig, build_providers, load_harness_config
from noema_model.harness.core import DecideRequest, DecideResult, GenerateRequest, GenerateResult, Harness
from noema_model.harness.decide_types import (
    ABSTAIN,
    Answer,
    ChoiceAnswer,
    ChoiceQuestion,
    DecisionTask,
    Option,
    Question,
    ScoreAnswer,
    ScoreQuestion,
    YesNoAnswer,
    YesNoQuestion,
)
from noema_model.harness.errors import HarnessRejection, Reason
from noema_model.harness.templates import PromptTemplate

__all__ = [
    "ABSTAIN", "Answer", "AttemptOutcome", "AttemptRecord", "CallRecord", "ChoiceAnswer", "ChoiceQuestion",
    "DecideRequest", "DecideResult", "DecisionTask", "Failover", "GenerateRequest", "GenerateResult", "Harness",
    "HarnessConfig", "HarnessRejection", "Option", "PromptTemplate", "Question", "RateCard", "Reason", "ScoreAnswer",
    "ScoreQuestion", "YesNoAnswer", "YesNoQuestion", "build_providers", "load_harness_config",
]
