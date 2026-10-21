"""Rank candidate targets from principal-scoped evidence by deterministic code: score = sum of weight times feature, with the composed recipient matcher (PERD v1.11).

Implements: REQ-AMB-002, REQ-AMB-003, REQ-AMB-007.

May import: noema_model.platform and the core Evidence API client.

Must never: call a model to rank; break a tie; pick a winner below the margin or the floor; search outside the principal's scope.

Public surface: rank (the pure function), RankingConfig, RecipientMatcher, and the plain data types. Ranking proposes a
state (auto-resolved, ambiguous, too many, none); Trust decides what happens next.
"""

from noema_model.understanding.resolution.config import RankingConfig
from noema_model.understanding.resolution.matcher import RecipientMatcher
from noema_model.understanding.resolution.ranking import candidates_resolved_payload, rank
from noema_model.understanding.resolution.types import (
    Candidate,
    Context,
    Resolution,
    ResolutionState,
    ScoredCandidate,
    Target,
    Why,
)

__all__ = [
    "Candidate", "Context", "RankingConfig", "RecipientMatcher", "Resolution", "ResolutionState", "ScoredCandidate",
    "Target", "Why", "candidates_resolved_payload", "rank",
]
