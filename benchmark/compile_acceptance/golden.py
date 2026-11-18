"""The golden examples G-A to G-E of the Intent Compiler (PERD, "Golden examples"), as synthetic canonical inputs and the check each proposal must meet (UND-05, UND-16).

Every utterance is synthetic and is the PERD's own text; every identifier is invented (AGENT-25). The check reads only the proposal the compile service returned: it never reads the oracle or the held-out set (INV-22).
A problem message names a slot and what is wrong, never a number or a piece of the utterance, so a run record holds no digit run (AGENT-13)."""
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any

NODE = "fin.payments.exception.investigate"
RECEIVED_AT = "2026-11-16T12:30:00Z"  # a synthetic afternoon, so "this morning" and "yesterday" have a window behind them


@dataclass(frozen=True, slots=True)
class Golden:
    case_id: str
    text: str
    question_type: str
    remedy: str | None = None
    amount_minor: int | None = None
    counterparty: str | None = None
    when: str | None = None
    absent: tuple[str, ...] = ()
    candidates: tuple[tuple[str, int], ...] = ()  # (counterparty shown, amount in minor units): what the synthetic candidate source returns for this example
    request_id: str = ""


GOLDEN: tuple[Golden, ...] = (
    Golden("G-A", "Wetin happen to the 20k wey I send Tunde this morning?", "STATUS", amount_minor=2_000_000, counterparty="Tunde", when="this morning",
           candidates=(("Tunde Adeyemi", 2_000_000),), request_id="3f1a9c4e-8b2d-4e61-a7c5-0d9e2b6f1a01"),
    Golden("G-B", "Check the transfer to Tunde", "STATUS", counterparty="Tunde",
           candidates=(("Tunde Adeyemi", 2_000_000), ("Tunde Bello", 1_500_000)), request_id="3f1a9c4e-8b2d-4e61-a7c5-0d9e2b6f1a02"),
    Golden("G-C", "The money comot this morning but e never reach", "STATUS", when="this morning", absent=("amount", "counterparty"),
           candidates=(("Ada Obi", 3_000_000), ("Kemi Ade", 700_000)), request_id="3f1a9c4e-8b2d-4e61-a7c5-0d9e2b6f1a03"),
    Golden("G-D", "Has the 50k I sent yesterday been reversed?", "REVERSAL_STATUS", amount_minor=5_000_000, when="yesterday",
           candidates=(("Emeka Nwosu", 5_000_000),), request_id="3f1a9c4e-8b2d-4e61-a7c5-0d9e2b6f1a04"),
    Golden("G-E", "Should I send it again? Just resend am for me", "RESEND_ADVICE", remedy="fin.cap.transfer_funds",
           candidates=(("Tunde Adeyemi", 2_000_000),), request_id="3f1a9c4e-8b2d-4e61-a7c5-0d9e2b6f1a05"),
)


def canonical_input(g: Golden) -> dict[str, Any]:
    """The `canonical-input.v2` of one example. The same every time (INV-21): the request id is a fixed synthetic UUID and the instant is fixed. The ids carry letters only."""
    letter = g.case_id[-1].lower()
    return {
        "schema_version": "2.0", "request_id": g.request_id, "correlation_id": f"corr_golden{letter}", "tenant_id": "ten_synthetic", "session_id": "ses_synthetic",
        "principal_ref": "usr_synthetic", "channel": "text", "mode": "MODE_1", "text": g.text, "received_at": RECEIVED_AT,
    }


def _slot(proposal: Mapping[str, Any], name: str) -> Mapping[str, Any] | None:
    slots = proposal.get("slots")
    if not isinstance(slots, Mapping):
        return None
    slot = slots.get(name)
    return slot if isinstance(slot, Mapping) else None


def check(g: Golden, proposal: Mapping[str, Any]) -> list[str]:
    """What is wrong with `proposal` against the example's expectation; an empty list means the check is met."""
    problems: list[str] = []
    if not isinstance(proposal, Mapping) or proposal.get("intent_node") != NODE:
        problems.append("node: not the investigate node")
    if not isinstance(proposal, Mapping):
        return problems
    question = _slot(proposal, "question_type")
    if question is None:
        problems.append("question_type: missing")
    elif question.get("value") != g.question_type:
        problems.append("question_type: not the expected value")
    remedy = _slot(proposal, "requested_remedy")
    if g.remedy is None:
        if remedy is not None:
            problems.append("requested_remedy: present where none is expected")
    elif remedy is None:
        problems.append("requested_remedy: missing")
    elif remedy.get("value") != g.remedy:
        problems.append("requested_remedy: not the expected capability")
    amount = _slot(proposal, "amount")
    if g.amount_minor is not None:
        if amount is None:
            problems.append("amount: missing")
        elif amount.get("value") != g.amount_minor or amount.get("unit") != "NGN":
            problems.append("amount: not the expected amount")
    elif amount is not None and "amount" in g.absent:
        problems.append("amount: invented where the utterance gives none")
    counterparty = _slot(proposal, "counterparty")
    if g.counterparty is not None:
        if counterparty is None:
            problems.append("counterparty: missing")
        elif counterparty.get("surface_form") != g.counterparty:
            problems.append("counterparty: not the expected name")
    elif counterparty is not None and "counterparty" in g.absent:
        problems.append("counterparty: invented where the utterance gives none")
    when = _slot(proposal, "when")
    if g.when is not None:
        if when is None:
            problems.append("when: missing")
        elif when.get("surface_form") != g.when:
            problems.append("when: not the expected phrase")
    return problems
