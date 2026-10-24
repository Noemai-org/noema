"""Test doubles for the pipeline: a model that answers the harness's questions from a script, a candidate source, and the
caller-supplied seams (normaliser, gate). The model double answers like a model would, in the forced schema; faults come
from the ScriptedProvider and the harness config, as in the harness tests."""

import json
from dataclasses import dataclass, field
from fractions import Fraction
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

from noema_model.harness import ABSTAIN, Harness
from noema_model.platform.telemetry import InMemoryTelemetry
from noema_model.providers.llm.base import FinishReason, ProviderRequest, ProviderResponse
from noema_model.understanding.pipeline import (
    CandidateQuery,
    CandidateSourceError,
    FlagSpec,
    LocaleOption,
    NodeCatalogue,
    NormalisedSlots,
    PipelineConfig,
    PipelineInput,
    Proposal,
)
from noema_model.understanding.resolution import Candidate, RankingConfig, RecipientMatcher, ResolutionState
from tests.support import ManualClock, make_config, raw_config
from noema_model.harness.config import load_harness_config

ROOT = Path(__file__).resolve().parents[3]
PACK = json.loads((ROOT / "packs" / "fintech_v1.json").read_text(encoding="utf-8"))
MATCHER = RecipientMatcher.from_file(ROOT / "packs" / "fintech" / "lexicons" / "recipient_matcher.json")
NODE = "fin.payments.exception.investigate"
OUT_OF_SCOPE = "fin.meta.out_of_scope"
T0 = datetime(2026, 10, 21, 8, 0, tzinfo=UTC)
WINDOW = (T0 - timedelta(hours=3), T0 + timedelta(hours=4))
VERSION = "model-alpha-2026-01-01"

RANKING = RankingConfig.from_mapping({
    "weights": {"amount": "0.35", "recipient": "0.25", "temporal": "0.20", "relationship": "0.10", "type_direction": "0.10"},
    "auto_resolve_margin": "0.35", "absolute_floor": "0.70", "max_candidates_before_escalate": 4, "max_raw_candidates": 20,
    "amount_tolerance_ratio": "0.01", "time_tolerance_seconds": 7200,
})


def pipeline_config() -> PipelineConfig:
    """Test pipeline configuration. Flag names are from PERD §20.3; which are material is a test choice."""
    return PipelineConfig(
        catalogue=NodeCatalogue.from_pack(PACK, out_of_scope_node=OUT_OF_SCOPE),
        ranking=RANKING,
        matcher=MATCHER,
        proposable_flags=(
            FlagSpec("MULTIPLE_INTENTS", "Does the customer ask for more than one thing?"),
            FlagSpec("CONTRADICTS_PRIOR_TURN", "Does the customer contradict something said earlier?"),
        ),
        locales=(LocaleOption("en-NG", "Nigerian English"), LocaleOption("pcm", "Nigerian Pidgin"), LocaleOption("yo", "Yoruba")),
        material_flags=frozenset({"MULTIPLE_INTENTS", "CONTRADICTS_PRIOR_TURN", "INTENT_UNCLEAR", "OUT_OF_SCOPE_REQUEST"}),
        intent_unclear_flag="INTENT_UNCLEAR",
        out_of_scope_flag="OUT_OF_SCOPE_REQUEST",
        native_yes_threshold=Fraction(1, 2),  # a TEST value; the real one is a founder decision after the benchmark (AGENT-16)
        state_flags={
            ResolutionState.NO_CANDIDATES: "NO_TRANSACTION_MATCH",
            ResolutionState.AMBIGUOUS: "MULTIPLE_TRANSACTIONS_MATCH",
            ResolutionState.TOO_MANY_CANDIDATES: "MULTIPLE_TRANSACTIONS_MATCH",
        },
    )


@dataclass
class ScriptedModel:
    """Answers each question from `answers` (question id -> option text, or a bool for yes/no); anything unscripted
    abstains (choice) or says no (yes/no). `mentions` is the slot-mentions reply."""

    answers: dict[str, Any] = field(default_factory=dict)
    mentions: dict[str, dict[str, Any]] = field(default_factory=dict)
    probability: float = 0.9
    raw_mentions: str | None = None
    name: str = "alpha"
    version: str = VERSION
    requests: list[ProviderRequest] = field(default_factory=list)

    async def complete(self, request: ProviderRequest) -> ProviderResponse:
        self.requests.append(request)
        if request.schema_name == "pipeline_slot_mentions":
            out = self.raw_mentions if self.raw_mentions is not None else json.dumps(self._mentions(request))
        else:
            out = json.dumps(self._decide(request))
        return ProviderResponse(out, self.version, 50, 10, FinishReason.COMPLETE)

    def _mentions(self, request: ProviderRequest) -> dict[str, Any]:
        names = list(request.output_schema["properties"])
        empty = {"present": False, "surface_form": "", "proposed_value": "", "confidence": 0.0}
        return {n: self.mentions.get(n, empty) for n in names}

    def _decide(self, request: ProviderRequest) -> dict[str, Any]:
        body = request.user.split("QUESTIONS:\n", 1)[1].split("\n\n<<<DATA", 1)[0]
        answers = []
        for q in json.loads(body):
            qid, want = q["question_id"], self.answers.get(q["question_id"])
            if q["kind"] == "choice":
                pick = ABSTAIN
                for option in q["options"]:
                    if want is not None and want != ABSTAIN and (want == option["label"] or f"({want})" in option["label"]):
                        pick = option["option_id"]
                probs = [{"option": o["option_id"], "p": self.probability if o["option_id"] == pick else 0.0} for o in q["options"]]
                answers.append({"question_id": qid, "selected": pick, "probabilities": probs})
            elif q["kind"] == "yes_no":
                yes = bool(want)
                answers.append({"question_id": qid, "selected": "yes" if yes else "no",
                                "probabilities": [{"option": "yes", "p": self.probability if yes else 0.1}, {"option": "no", "p": 0.1 if yes else self.probability}]})
            else:
                answers.append({"question_id": qid, "selected": "score", "probabilities": [{"option": "score", "p": 0.5}]})
        return {"answers": answers}


def harness_for(model: ScriptedModel, *, routes: dict[str, Any] | None = None, extra: Any = None) -> tuple[Harness, InMemoryTelemetry]:
    telemetry = InMemoryTelemetry()
    if routes is None:
        config = make_config(providers=("alpha",))
    else:
        config = load_harness_config(raw_config(providers=("alpha",), routes=routes))
    providers: dict[str, Any] = {"alpha": model}
    if extra:
        providers.update(extra)
    return Harness(config, providers, ManualClock(), telemetry), telemetry


def full_routes(drop: tuple[str, ...] = ()) -> dict[str, Any]:
    names = ["slot_values", *(f"decide.{t}" for t in (
        "intent_node_classification", "enumerated_slot", "ambiguity_flag", "locale_detection", "answer_binding", "instruction_likeness"))]
    return {n: {"default": ["alpha"], "by_locale": {}} for n in names if n not in drop}


def make_input(text: str = "Wetin happen to the 20k wey I send Tunde this morning?", **over: Any) -> PipelineInput:
    base: dict[str, Any] = dict(
        correlation_id="corr_test", principal_ref="usr_session", tenant_id="ten_test", channel="text", text=text,
        locale_hint=None, transcript_is_final=True, key_slot_low_asr=False, candidate_set_id="cs_test", candidate_set_version=1,
    )
    return PipelineInput(**{**base, **over})


def normaliser(inp: PipelineInput, proposal: Proposal) -> NormalisedSlots:
    """A stand-in for the deterministic lexicon (UND-05): reads the model's proposed values for this test's fixtures."""
    amount = proposal.mentions.get("amount")
    return NormalisedSlots(
        amount_minor=2_000_000 if amount and amount.present else None,
        currency="NGN" if amount and amount.present else None,
        amount_uncertain=False,
        window=WINDOW,
        counterparty_surface=(proposal.mentions["counterparty"].surface_form or None) if "counterparty" in proposal.mentions else None,
        expected_direction="OUTGOING",
    )


def no_errors(proposal: Proposal, normalised: NormalisedSlots) -> tuple[str, ...]:
    return ()


def candidate(cid: str, who: str = "Tunde Adeyemi", *, amount: int = 2_000_000, at: datetime = T0, alias: bool | None = True) -> Candidate:
    return Candidate(cid, f"res_{cid}", amount, "NGN", at, who, "0001", "OUTGOING", alias)


@dataclass
class FakeSource:
    rows: tuple[Candidate, ...] = ()
    fail: bool = False
    raises: Exception | None = None
    queries: list[CandidateQuery] = field(default_factory=list)

    async def fetch(self, query: CandidateQuery) -> tuple[Candidate, ...]:
        self.queries.append(query)
        if self.raises is not None:
            raise self.raises
        if self.fail:
            raise CandidateSourceError()
        return self.rows


GOOD_MENTIONS = {
    "amount": {"present": True, "surface_form": "20k", "proposed_value": "20000", "confidence": 0.9},
    "counterparty": {"present": True, "surface_form": "Tunde", "proposed_value": "Tunde", "confidence": 0.9},
    "when": {"present": True, "surface_form": "this morning", "proposed_value": "morning", "confidence": 0.8},
}
G_A = {"intent_node": NODE, "question_type": "STATUS", "locale": "Nigerian Pidgin"}
