"""The proposer side of the swap test (status/ENV-08.md). Two proposers, a canned-proposal stub (A) and the OpenAI Responses adapter
replaying a canned response (B), answer the same canned decisions through the same harness and pipeline. SYNTHETIC; no network, no
API call (the sockets are closed for the whole module).

This validates the decision path only. It does not validate the unbuilt intake or the architecture as a whole.

The other half is the core's swap test (its integration tests): it reads the files this module writes
(`synthetic/proposer_swap/proposals/`) and runs the real core on them. The two halves meet only at those files, so this test
also proves they are current: the committed files must be exactly what the proposers produce now.
"""

import json
from collections.abc import Callable, Iterator
from typing import Any

import pytest

from tests.support_contract import SCHEMAS, violations
from tests.support_pipeline import PACK
from tests.support_swap import (
    DEFINITIONS,
    GOLDEN,
    PROPOSERS,
    SCENARIOS,
    canonical,
    deny_sockets,
    golden_name,
    load_canned,
    propose,
    render,
    sign,
)


CONTRACT = json.loads((SCHEMAS / "intent-object.v1.json").read_text(encoding="utf-8"))


@pytest.fixture(autouse=True)
def no_network() -> Iterator[None]:
    restore = deny_sockets()
    yield
    restore()


def intent(proposer: str, scenario: str) -> dict[str, Any] | None:
    value = propose(proposer, scenario).envelope["intent_object"]
    assert value is None or isinstance(value, dict)
    return value


@pytest.mark.parametrize("scenario", SCENARIOS)
def test_both_proposers_hand_over_the_same_proposal(scenario: str) -> None:
    a, b = intent("A", scenario), intent("B", scenario)
    assert (None if a is None else canonical(a)) == (None if b is None else canonical(b))


def test_the_proposals_are_the_three_cases_the_go_half_needs() -> None:
    valid, prohibited, invalid = (propose("A", s).envelope for s in SCENARIOS)
    assert valid["pipeline"] == {"outcome": "PROPOSED", "gate_errors": []}
    assert valid["intent_object"]["slots"]["subject_resource"]["resolved_ref"] == "fin.transaction:TX1"
    assert valid["intent_object"]["slots"]["question_type"]["value"] == "ESCALATION_REQUEST"
    assert "requested_remedy" not in valid["intent_object"]["slots"]
    assert prohibited["intent_object"]["slots"]["requested_remedy"]["value"] == "fin.cap.initiate_refund"
    assert invalid["pipeline"]["outcome"] == "REJECTED_AT_GATE" and invalid["pipeline"]["gate_errors"] and invalid["intent_object"] is None


@pytest.mark.parametrize("proposer", PROPOSERS)
@pytest.mark.parametrize("scenario", [s for s in SCENARIOS if s != "invalid"])
def test_an_intent_object_follows_the_contract_and_the_pack(proposer: str, scenario: str) -> None:
    """SoT §43.1 and PERD §20.1 by contracts/schemas/intent-object.v1.json; and the checks PERD §20.1 puts on the pack."""
    obj = intent(proposer, scenario)
    assert obj is not None
    assert violations(obj, CONTRACT) == []
    node = next(n for n in PACK["intent_nodes"] if n["node_id"] == obj["intent_node"])
    assert obj["pack"] == {"pack_id": PACK["pack_id"], "pack_version": PACK["pack_version"]}
    assert set(obj["slots"]) <= {s["name"] for s in node["slots"]}
    assert set(obj["desired_outcome_refs"]) <= set(node["outcome_definitions"])
    assert obj["status"] == "VALIDATED" and obj["sig"] == sign(obj)


def test_the_checker_rejects_what_the_contract_rejects() -> None:
    """The checker has teeth: an unknown field, a bad status and a resolved slot with no reference are all refused."""
    good = intent("A", "escalate")
    assert good is not None and violations(good, CONTRACT) == []
    mutations: list[Callable[[dict[str, Any]], object]] = [
        lambda o: o.update(model_version="x"),
        lambda o: o.update(status="ALLOWED"),
        lambda o: o["slots"]["subject_resource"].pop("resolved_ref"),
        lambda o: o["principal"].update(extra=1),
        lambda o: o.update(tenant_id="aa"),
    ]
    for mutate in mutations:
        broken = json.loads(json.dumps(good))
        mutate(broken)
        assert violations(broken, CONTRACT), mutate


@pytest.mark.parametrize("scenario", SCENARIOS)
def test_provenance_differs_between_the_proposers_and_stays_out_of_the_intent_object(scenario: str) -> None:
    a, b = propose("A", scenario), propose("B", scenario)
    assert a.envelope["proposer"] != b.envelope["proposer"]
    def providers(side: Any) -> set[str]:
        return {t["provider"] for c in side.envelope["proposer"]["calls"] for t in c["attempts"]}

    assert providers(a) == {"stub_a"} and providers(b) == {"openai_b"}
    provenance = json.dumps([a.envelope["proposer"], b.envelope["proposer"]])
    for token in ("stub_a", "openai_b", "model-stub_a-2026-01-01", "model-openai_b-2026-01-01"):
        assert token in provenance
        for side in (a, b):
            assert token not in json.dumps(side.envelope["intent_object"])


@pytest.mark.parametrize("scenario", SCENARIOS)
def test_proposer_b_is_the_openai_responses_adapter_with_no_network(scenario: str) -> None:
    b = propose("B", scenario)
    assert b.requests, "the adapter must have been called"
    assert all(url == "https://vendor.example.invalid/v1/responses" for url, _ in b.requests)
    assert all(body["store"] is False and body["text"]["format"]["strict"] is True for _, body in b.requests)
    assert propose("A", scenario).requests == ()


def test_every_canned_output_is_used_and_nothing_else_is_served() -> None:
    table = load_canned()
    for scenario in SCENARIOS:
        for proposer in PROPOSERS:
            served = set(propose(proposer, scenario).served)
            assert served == set(table[scenario]), (proposer, scenario)


def test_the_unused_scenario_definitions_are_listed() -> None:
    assert set(DEFINITIONS) == set(SCENARIOS)


@pytest.mark.parametrize("scenario", SCENARIOS)
@pytest.mark.parametrize("proposer", PROPOSERS)
def test_the_committed_files_are_what_the_proposers_produce_now(proposer: str, scenario: str) -> None:
    path = GOLDEN / golden_name(scenario, proposer)
    assert path.read_text(encoding="utf-8") == render(propose(proposer, scenario).envelope), (
        f"{path.name} is stale: run `python -m tests.support_swap --write` in services/model and commit the files")
