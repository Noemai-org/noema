"""UND-16, the findings of the live compile run #6 (noema#338; rulings A532, A533). All data is SYNTHETIC.

Run #6 (45120524373) ended with exit 1: goldens G-A, G-C and G-D MISSED (a `requested_remedy` present where none was expected), G-B and G-E ERROR (InputInvalid), and three of the five record-only
inputs ERROR (InputInvalid). What these tests pin, from the code and from the committed recording of the earlier live run (`tests/fixtures/replay/compile-gh44521380842`, rec-0006: for G-A the model
chose `fin.cap.lookup_transaction` for `requested_remedy` at p 0.78, a capability the customer never asked for):

1. The three failing record-only inputs say `dis morning`, which the lexicon had no phrase for, so there was no window and the pipeline refused the turn at the gate. The phrase is
   Pidgin: it goes in the Pidgin lexicon `fin.lex.pcm` (named by the intent node's `lexicon_refs`), never into the English one (A532: a locale is never silently mapped to English); the
   file is pack data, minimal, and labelled SYNTHETIC (A538). A request with no usable time is no longer refused at all (A535): nothing is read, the proposal carries the slots unresolved, Trust asks.
2. An optional closed-vocabulary slot (`requested_remedy`) is asked as "does the customer explicitly ask for one of these?", with the abstention that every choice question already offers (A533: the
   schema and the prompt change, not the golden).
3. An `InputInvalid` carries a closed internal reason, so the next run says WHY (not text, unknown pack, ...). The public answer does not change.
"""

import copy
import dataclasses
import json
from collections.abc import Mapping
from datetime import UTC, datetime
from pathlib import Path
from typing import Any, cast

import pytest
from fastapi.testclient import TestClient

from noema_model.main import create_app
from noema_model.platform.config import ConfigError
from noema_model.understanding.compiler.compiler import PipelineCompiler
from noema_model.understanding.compiler.lexicon import Lexicon, Normaliser, load_pack_lexicons
from noema_model.understanding.compiler.service import CompileService, InputInvalid
from noema_model.understanding.pipeline import (
    CandidateRequestInvalid,
    InputInvalidReason,
    NodeCatalogue,
    NormalisedSlots,
    Outcome,
    Proposal,
    run_pipeline,
)
from noema_model.harness import ABSTAIN
from noema_model.understanding.resolution import Candidate
from tests.support import run
from tests.support_pipeline import (
    G_A,
    GOOD_MENTIONS,
    NODE,
    OUT_OF_SCOPE,
    PACK,
    ROOT,
    FakeSource,
    ScriptedModel,
    harness_for,
    make_input,
    no_errors,
    normaliser,
    pipeline_config,
)
from tests.test_compile_compiler import CANONICAL, IDS, Minter, Packs, g_a

LEXICONS = ROOT / "packs" / "fintech" / "lexicons"
PCM_FILE = LEXICONS / "fin.lex.pcm.json"
EN_FILE = LEXICONS / "fin.lex.en_ng.json"
G1 = json.loads((ROOT / "benchmark" / "replay" / "g1_inputs.json").read_text(encoding="utf-8"))
CONFIG = pipeline_config()
PIDGIN = "Wetin happen to the N2,500 wey I send Chidi dis morning?"
PIDGIN_MENTIONS = {
    "amount": {"present": True, "surface_form": "N2,500", "proposed_value": "2500", "confidence": 0.9},
    "counterparty": {"present": True, "surface_form": "Chidi", "proposed_value": "Chidi", "confidence": 0.9},
    "when": {"present": True, "surface_form": "dis morning", "proposed_value": "morning", "confidence": 0.8},
}


def copy_lexicon(into: Path, name: str, **edit: Any) -> dict[str, Any]:
    raw: dict[str, Any] = json.loads(EN_FILE.read_text(encoding="utf-8"))
    raw.update({"lexicon_id": name, **edit})
    (into / f"{name}.json").write_text(json.dumps(raw), encoding="utf-8")
    return raw


# --- 1. the Pidgin lexicon (A532) --------------------------------------------------------------------------------------------------------------

def test_the_phrase_dis_morning_is_in_the_pidgin_file_and_never_in_the_english_one() -> None:
    pcm = json.loads(PCM_FILE.read_text(encoding="utf-8"))
    english = json.loads(EN_FILE.read_text(encoding="utf-8"))
    assert pcm["lexicon_id"] == "fin.lex.pcm" and pcm["mark"] == "[synthetic seed]"
    phrases = {t["phrase"]: t for t in pcm["relative_times"]}
    this_morning = next(t for t in english["relative_times"] if t["phrase"] == "this morning")
    assert {k: phrases["dis morning"][k] for k in ("day_offset", "from", "to")} == {k: this_morning[k] for k in ("day_offset", "from", "to")}
    assert "dis morning" not in {t["phrase"] for t in english["relative_times"]}  # A532: a locale is never mapped to English


def test_the_pidgin_file_is_whole_alone_and_every_entry_is_a_marked_synthetic_seed() -> None:
    lexicon = Lexicon.from_file(PCM_FILE)
    assert lexicon.lexicon_id == "fin.lex.pcm" and "dis morning" in lexicon.times

    def walk(node: Any) -> None:
        if isinstance(node, dict):
            if "word" in node or "phrase" in node:
                assert node.get("mark") == "[synthetic seed]", node
            for v in node.values():
                walk(v)
        elif isinstance(node, list):
            for v in node:
                walk(v)
        else:
            assert not isinstance(node, float), node  # INV-21

    walk(json.loads(PCM_FILE.read_text(encoding="utf-8")))


def test_the_pidgin_file_holds_only_what_the_g1_inputs_need() -> None:
    texts = " ".join(u["text"].casefold() for case in G1["cases"] for u in case["utterances"])
    pcm = json.loads(PCM_FILE.read_text(encoding="utf-8"))
    entries = [e["phrase"] for e in pcm.get("relative_times", [])] + [e["word"] for key in ("multipliers", "directions") for e in pcm.get(key, [])]
    assert entries and all(e in texts for e in entries), entries  # nothing is added that no input needs (AGENT-16)


def test_the_pack_loader_merges_the_lexicons_the_nodes_name_and_reads_the_pidgin_phrase() -> None:
    merged = load_pack_lexicons(PACK, LEXICONS)
    now = datetime(2026, 11, 16, 12, 30, tzinfo=UTC)
    for text, phrase in (("Wetin happen to the 20k wey I send Tunde dis morning?", "dis morning"), ("Where is the 20k I sent to Tunde this morning?", "this morning")):
        proposal = Proposal(intent_node=NODE, reference_slots={}, mentions={"when": _mention(phrase)}, ambiguity_flags=(), locale="en-NG", code_switching=False, probabilities={})
        window = Normaliser(merged, now).normalise(make_input(text), proposal).window
        assert window == (datetime(2026, 11, 16, 4, 0, tzinfo=UTC), datetime(2026, 11, 16, 11, 0, tzinfo=UTC)), phrase  # 05:00 to 12:00 at +01:00
    only_english = Lexicon.from_file(EN_FILE)
    proposal = Proposal(intent_node=NODE, reference_slots={}, mentions={"when": _mention("dis morning")}, ambiguity_flags=(), locale="pcm", code_switching=False, probabilities={})
    assert Normaliser(only_english, now).normalise(make_input("x dis morning"), proposal).window is None  # the English file alone has no such phrase


def _mention(surface: str) -> Any:
    from noema_model.understanding.pipeline import MentionProposal
    return MentionProposal(present=True, surface_form=surface, proposed_value="x", confidence=0.9)


def test_the_merged_lexicon_keeps_the_english_amounts_and_directions() -> None:
    merged = load_pack_lexicons(PACK, LEXICONS)
    english = Lexicon.from_file(EN_FILE)
    assert merged.currency_code == "NGN" and merged.minor_per_major == 100 and merged.utc_offset_minutes == 60
    assert set(merged.multipliers) >= set(english.multipliers) and set(merged.directions) >= set(english.directions) and set(merged.times) >= set(english.times)
    assert merged.lexicon_id != english.lexicon_id and merged.digest != english.digest


def test_a_ref_with_no_file_is_skipped_as_it_always_was_and_a_pack_with_no_file_at_all_is_refused(tmp_path: Path) -> None:
    copy_lexicon(tmp_path, "fin.lex.en_ng")  # the pack also names pcm, yo, ha and ig; none has a file here
    assert load_pack_lexicons(PACK, tmp_path).lexicon_id == "fin.lex.en_ng"
    empty = tmp_path / "empty"
    empty.mkdir()
    with pytest.raises(ConfigError):
        load_pack_lexicons(PACK, empty)


def test_a_file_the_pack_does_not_name_is_never_loaded(tmp_path: Path) -> None:
    copy_lexicon(tmp_path, "fin.lex.en_ng")
    copy_lexicon(tmp_path, "fin.lex.zz", currency={"code": "GBP", "minor_per_major": 100, "words": ["pound"], "mark": "[synthetic seed]"})
    assert load_pack_lexicons(PACK, tmp_path).currency_code == "NGN"


def test_a_file_whose_id_is_not_the_ref_it_answers_to_is_refused(tmp_path: Path) -> None:
    raw = copy_lexicon(tmp_path, "fin.lex.en_ng")
    (tmp_path / "fin.lex.pcm.json").write_text(json.dumps({**raw, "lexicon_id": "fin.lex.yo"}), encoding="utf-8")
    with pytest.raises(ConfigError):
        load_pack_lexicons(PACK, tmp_path)


@pytest.mark.parametrize("edit", [
    {"relative_times": [{"phrase": "this morning", "day_offset": 0, "from": "06:00", "to": "12:00", "mark": "[synthetic seed]"}]},  # one phrase, two windows
    {"multipliers": [{"word": "k", "value": 999, "mark": "[synthetic seed]"}]},  # one word, two values
    {"directions": [{"word": "send", "value": "INCOMING", "mark": "[synthetic seed]"}]},
    {"currency": {"code": "GBP", "minor_per_major": 100, "words": ["pound"], "mark": "[synthetic seed]"}},  # one pack, two currencies
    {"currency": {"code": "NGN", "minor_per_major": 1000, "words": ["naira"], "mark": "[synthetic seed]"}},
    {"utc_offset_minutes": 0},
])
def test_two_lexicons_that_disagree_are_refused_not_merged(tmp_path: Path, edit: dict[str, Any]) -> None:
    copy_lexicon(tmp_path, "fin.lex.en_ng")
    copy_lexicon(tmp_path, "fin.lex.pcm", **edit)
    with pytest.raises(ConfigError):
        load_pack_lexicons(PACK, tmp_path)


def write_without(into: Path, name: str, section: str) -> None:
    raw: dict[str, Any] = json.loads(EN_FILE.read_text(encoding="utf-8"))
    raw["lexicon_id"] = name
    del raw[section]
    (into / f"{name}.json").write_text(json.dumps(raw), encoding="utf-8")


@pytest.mark.parametrize("section", ["multipliers", "relative_times", "directions"])
def test_a_merged_lexicon_with_an_empty_section_is_refused_for_the_english_file_alone(tmp_path: Path, section: str) -> None:
    """C1's finding on noema#901 (A535 made a missing section load as empty, so nothing refused a truncated base): the section the Normaliser needs must be there after the merge."""
    write_without(tmp_path, "fin.lex.en_ng", section)
    with pytest.raises(ConfigError, match=section):
        load_pack_lexicons(PACK, tmp_path)


@pytest.mark.parametrize("section", ["multipliers", "relative_times", "directions"])
def test_a_merged_lexicon_with_an_empty_section_is_refused_for_the_english_and_the_pidgin_pair(tmp_path: Path, section: str) -> None:
    write_without(tmp_path, "fin.lex.en_ng", section)
    write_without(tmp_path, "fin.lex.pcm", section)  # a supplemental file may omit a section, but not when the base omits it too
    with pytest.raises(ConfigError, match=section):
        load_pack_lexicons(PACK, tmp_path)


@pytest.mark.parametrize("section", ["multipliers", "relative_times", "directions"])
def test_a_supplemental_file_may_omit_a_section_when_the_base_holds_it(tmp_path: Path, section: str) -> None:
    copy_lexicon(tmp_path, "fin.lex.en_ng")
    write_without(tmp_path, "fin.lex.pcm", section)
    merged = load_pack_lexicons(PACK, tmp_path)
    assert merged.multipliers and merged.times and merged.directions


def test_two_lexicons_that_agree_on_a_phrase_merge(tmp_path: Path) -> None:
    copy_lexicon(tmp_path, "fin.lex.en_ng")
    copy_lexicon(tmp_path, "fin.lex.pcm")  # the same entries, the same meanings
    assert "this morning" in load_pack_lexicons(PACK, tmp_path).times


@pytest.mark.parametrize("ref", ["../fin.lex.en_ng", "fin/lex", "FIN.LEX", ""])
def test_a_ref_that_is_not_a_plain_id_is_refused_even_when_a_file_is_there_to_be_found(tmp_path: Path, ref: str) -> None:
    directory = tmp_path / "d"
    (directory / "fin").mkdir(parents=True)
    raw: dict[str, Any] = json.loads(EN_FILE.read_text(encoding="utf-8"))
    for where in (tmp_path / "fin.lex.en_ng.json", directory / "fin" / "lex.json", directory / "FIN.LEX.json", directory / ".json"):
        where.write_text(json.dumps({**raw, "lexicon_id": ref}), encoding="utf-8")  # whatever the path built from the ref reaches carries the ref as its id, so only the ref's shape can refuse it
    pack = copy.deepcopy(PACK)
    pack["intent_nodes"][0]["lexicon_refs"] = [ref]
    with pytest.raises(ConfigError):
        load_pack_lexicons(pack, directory)


def candidate_row(at: datetime) -> Candidate:
    return Candidate("c1", "fin.transaction:TXA", 250_000, "NGN", at, "Chidi Okonkwo", "3644", "OUTGOING", True)


def test_a_pidgin_request_compiles_with_the_merged_lexicon_and_without_the_pidgin_file_the_time_is_unresolved_and_nothing_is_read() -> None:
    now = {**CANONICAL, "text": PIDGIN, "received_at": "2026-11-16T12:30:00Z"}
    source = FakeSource((candidate_row(datetime(2026, 11, 16, 10, 0, tzinfo=UTC)),))
    merged = PipelineCompiler({"fin@1.0.0": CONFIG}, load_pack_lexicons(PACK, LEXICONS), harness_for(g_a(mentions=PIDGIN_MENTIONS))[0], source, Minter())
    doc = through_service_with(merged, now)
    when = next(s for s in doc["slots"].values() if s["type"] == "TimeRef")
    assert when["window"] == ["2026-11-16T04:00:00Z", "2026-11-16T11:00:00Z"] and when["surface_form"] == "dis morning"
    # Without the Pidgin file the phrase has no window. A535: that is not a refusal. The candidates are not read, the time slot stays UNRESOLVED and says what the customer said,
    # the resource slot is UNRESOLVED with no candidate set, and Trust asks (CLARIFY).
    english_only_source = FakeSource((candidate_row(datetime(2026, 11, 16, 10, 0, tzinfo=UTC)),))
    english_only = PipelineCompiler({"fin@1.0.0": CONFIG}, Lexicon.from_file(EN_FILE), harness_for(g_a(mentions=PIDGIN_MENTIONS))[0], english_only_source, Minter())
    unresolved = through_service_with(english_only, now)
    slot = next(s for s in unresolved["slots"].values() if s["type"] == "TimeRef")
    assert slot["status"] == "UNRESOLVED" and "window" not in slot and slot["surface_form"] == "dis morning"
    assert english_only_source.queries == []  # no window, no read (the Evidence API's contract needs one; nothing is sent against it)
    resource = next(s for s in unresolved["slots"].values() if s["type"] == "Resource")
    assert resource == {"type": "Resource", "status": "UNRESOLVED"}


def through_service_with(compiler: PipelineCompiler, canonical: dict[str, Any]) -> dict[str, Any]:
    service = CompileService(compiler, Packs(), max_proposal_bytes=65536, timeout_s=30.0, max_request_bytes=65536)
    return run(service.run(canonical, "fin@1.0.0", None))


# --- 2. the optional slot is asked as an explicit request (A533) ---------------------------------------------------------------------------------

def questions(model: ScriptedModel, config: Any = CONFIG) -> dict[str, dict[str, Any]]:
    harness, _ = harness_for(model)
    run(run_pipeline(make_input(), config, harness, FakeSource(()), normaliser, no_errors))
    for request in model.requests:
        if request.schema_name == "harness_decide" and "requested_remedy" in request.user:
            body = request.user.split("QUESTIONS:\n", 1)[1].split("\n\n<<<DATA", 1)[0]
            return {q["question_id"]: q for q in json.loads(body)}
    raise AssertionError("the enumerated slots were not asked")


def test_an_optional_closed_slot_is_asked_whether_the_customer_explicitly_asks_for_one_and_may_abstain() -> None:
    asked = questions(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS))
    remedy = asked["requested_remedy"]
    assert "explicitly ask" in remedy["prompt"] and ABSTAIN in remedy["prompt"]
    assert "Which value" not in remedy["prompt"] and "express" not in remedy["prompt"]
    assert any(o["option_id"] == ABSTAIN for o in remedy["options"])  # closed: the schema still expresses none
    for word in ("Tunde", "resend", "transfer", "morning", "20k", "G-"):
        assert word not in remedy["prompt"]  # data-neutral: no example from a golden (A533)


def test_a_required_closed_slot_keeps_its_wording() -> None:
    asked = questions(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS))
    assert asked["question_type"]["prompt"] == "Which value of 'question_type' does the customer's message express?"


def test_the_wording_follows_the_slots_required_flag_and_not_its_name() -> None:
    pack = copy.deepcopy(PACK)
    for slot in next(n for n in pack["intent_nodes"] if n["node_id"] == NODE)["slots"]:
        if slot["name"] in ("question_type", "requested_remedy"):
            slot["required"] = slot["name"] == "requested_remedy"
    swapped = dataclasses.replace(CONFIG, catalogue=NodeCatalogue.from_pack(pack, out_of_scope_node=OUT_OF_SCOPE))
    asked = questions(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS), swapped)
    assert asked["requested_remedy"]["prompt"] == "Which value of 'requested_remedy' does the customer's message express?"
    assert "explicitly ask" in asked["question_type"]["prompt"]


def test_an_abstention_on_the_optional_slot_is_no_remedy() -> None:
    harness, _ = harness_for(ScriptedModel(answers={**G_A, "requested_remedy": ABSTAIN}, mentions=GOOD_MENTIONS))
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource(()), normaliser, no_errors))
    assert result.proposal is not None and result.proposal.reference_slots["requested_remedy"] is None


def test_an_explicit_request_still_reaches_the_slot() -> None:  # G-E must still be able to carry its remedy
    model = ScriptedModel(answers={**G_A, "requested_remedy": "fin.cap.transfer_funds"}, mentions=GOOD_MENTIONS)
    harness, _ = harness_for(model)
    result = run(run_pipeline(make_input(), CONFIG, harness, FakeSource(()), normaliser, no_errors))
    assert result.proposal is not None and result.proposal.reference_slots["requested_remedy"] == "fin.cap.transfer_funds"


# --- 3. InputInvalid says why, inside the service only (the public answer is unchanged) ---------------------------------------------------------

def test_the_reasons_are_a_closed_set() -> None:
    assert {r.value for r in InputInvalidReason} == {
        "NOT_FINAL_TRANSCRIPT", "CANDIDATE_REQUEST_INVALID", "UNKNOWN_PACK", "NOT_TEXT_CHANNEL", "BAD_RECEIVED_AT", "IDENTITY_INVALID",
    }


def _no_window(inp: Any, proposal: Proposal) -> NormalisedSlots:
    return NormalisedSlots(amount_minor=None, currency=None, amount_uncertain=False, window=None, counterparty_surface=None, expected_direction=None)


def piped(norm: Any = normaliser, source: Any = None, **over: Any) -> Any:
    harness, _ = harness_for(ScriptedModel(answers=dict(G_A), mentions=GOOD_MENTIONS))
    return run(run_pipeline(make_input(**over), CONFIG, harness, source or FakeSource((candidate_row(datetime(2026, 10, 21, 8, 0, tzinfo=UTC)),)), norm, no_errors))


def test_the_pipeline_names_why_it_refused_as_invalid_input() -> None:
    voice = piped(channel="voice", transcript_is_final=False)
    assert voice.input_invalid is InputInvalidReason.NOT_FINAL_TRANSCRIPT
    refused = piped(source=FakeSource(raises=CandidateRequestInvalid()))
    assert refused.input_invalid is InputInvalidReason.CANDIDATE_REQUEST_INVALID


def test_a_request_with_no_time_window_is_proposed_with_nothing_read_and_no_refusal() -> None:
    """A535 (interim): no default window and no refusal. The turn is PROPOSED, no read is made, there is no resolution to report, and the flag for an empty search is not raised."""
    source = FakeSource((candidate_row(datetime(2026, 10, 21, 8, 0, tzinfo=UTC)),))
    none = piped(_no_window, source=source)
    assert none.outcome is Outcome.PROPOSED and none.gate_errors == () and none.input_invalid is None
    assert source.queries == [] and none.query is None and none.resolution is None
    assert "NO_TRANSACTION_MATCH" not in none.ambiguity_flags  # that code is for a search that ran and came back empty (REQ-AMB-002)
    assert none.proposal is not None and none.proposal.intent_node == NODE


def test_a_turn_that_is_not_invalid_input_names_no_reason() -> None:
    assert piped().input_invalid is None
    assert piped(source=FakeSource(fail=True)).input_invalid is None  # an Evidence outage is a dependency error, not invalid input


def compile_raises(canonical: dict[str, Any], pack_ref: str = "fin@1.0.0", **kw: Any) -> InputInvalid:
    compiler = PipelineCompiler({"fin@1.0.0": CONFIG}, Lexicon.from_file(EN_FILE), harness_for(g_a(**kw))[0], FakeSource((candidate_row(datetime(2026, 10, 21, 8, 0, tzinfo=UTC)),)), Minter())
    with pytest.raises(InputInvalid) as caught:
        run(compiler.compile(canonical, pack_ref, None))
    return caught.value


def test_the_compiler_names_each_refusal() -> None:
    assert compile_raises(CANONICAL, "fin@2.0.0").reason is InputInvalidReason.UNKNOWN_PACK
    assert compile_raises({**CANONICAL, "channel": "voice"}).reason is InputInvalidReason.NOT_TEXT_CHANNEL
    for bad in ("2026-10-21T08:00:00", "yesterday", None):
        assert compile_raises({**CANONICAL, "received_at": bad}).reason is InputInvalidReason.BAD_RECEIVED_AT


def test_the_service_names_its_own_refusals() -> None:
    compiler = PipelineCompiler({"fin@1.0.0": CONFIG}, Lexicon.from_file(EN_FILE), harness_for(g_a())[0], FakeSource(()), Minter())
    service = CompileService(compiler, Packs(), max_proposal_bytes=65536, timeout_s=30.0, max_request_bytes=65536)
    with pytest.raises(InputInvalid) as unknown:
        run(service.run(CANONICAL, "fin@2.0.0", None))
    assert unknown.value.reason is InputInvalidReason.UNKNOWN_PACK
    with pytest.raises(InputInvalid) as identity:
        run(service.run({**CANONICAL, "tenant_id": ""}, "fin@1.0.0", None))
    assert identity.value.reason is InputInvalidReason.IDENTITY_INVALID


class Refusing:
    max_request_bytes = 65536

    def __init__(self, reason: InputInvalidReason) -> None:
        self.reason = reason

    async def run(self, canonical_input: Mapping[str, Any], pack_ref: str, open_intent: Mapping[str, Any] | None) -> dict[str, Any]:
        raise InputInvalid(self.reason)


def test_the_public_answer_is_the_same_for_every_reason_and_names_none() -> None:
    from tests.test_compile_route import CANONICAL as ROUTE_CANONICAL, PACK_REF
    bodies = []
    for reason in InputInvalidReason:
        client = TestClient(create_app(environ={}, compile_service=cast(Any, Refusing(reason))))
        answer = client.post("/internal/v1/understanding:compile", json={"canonical_input": ROUTE_CANONICAL, "pack_ref": PACK_REF})
        assert answer.status_code == 422 and answer.json()["error"]["code"] == "INPUT_INVALID"
        assert all(name.value not in answer.text for name in InputInvalidReason)
        bodies.append(answer.text)
    assert len(set(bodies)) == 1


NO_MENTION = {"present": False, "surface_form": "", "proposed_value": "", "confidence": 0.0}
NO_WHEN = {"when": NO_MENTION}


def test_a_request_with_no_time_reference_compiles_with_the_slots_unresolved_and_nothing_is_read() -> None:
    """A535: the proposal is the contract's own shape for 'not known': no `when` slot (a TimeRef needs the words the customer used, and there are none) and a Resource slot that is
    UNRESOLVED with no candidate set. The service's schema gate accepts it."""
    source = FakeSource((candidate_row(datetime(2026, 10, 21, 8, 0, tzinfo=UTC)),))
    compiler = PipelineCompiler({"fin@1.0.0": CONFIG}, Lexicon.from_file(EN_FILE), harness_for(g_a(mentions={**GOOD_MENTIONS, **NO_WHEN}))[0], source, Minter())
    doc = through_service_with(compiler, {**CANONICAL, "text": "Wetin happen to the 20k wey I send Tunde?"})
    assert source.queries == []
    assert not [s for s in doc["slots"].values() if s["type"] == "TimeRef"]
    assert next(s for s in doc["slots"].values() if s["type"] == "Resource") == {"type": "Resource", "status": "UNRESOLVED"}
    assert doc["intent_node"] == NODE and "NO_TRANSACTION_MATCH" not in doc["ambiguity_flags"]


def test_goldens_b_and_e_are_met_by_a_proposal_with_no_time_slot() -> None:
    """The goldens stay as they are (A535): neither expects a time slot, and both expect a proposal."""
    import sys
    sys.path.insert(0, str(ROOT))
    from benchmark.compile_acceptance.golden import GOLDEN, check  # type: ignore[import-not-found,unused-ignore]
    by_id = {g.case_id: g for g in GOLDEN}
    for case_id, answers in (("G-B", {}), ("G-E", {"question_type": "RESEND_ADVICE", "requested_remedy": "fin.cap.transfer_funds"})):
        golden = by_id[case_id]
        mentions = {**GOOD_MENTIONS, **NO_WHEN, "amount": NO_MENTION}
        if golden.counterparty is None:
            mentions["counterparty"] = NO_MENTION
        compiler = PipelineCompiler({"fin@1.0.0": CONFIG}, Lexicon.from_file(EN_FILE), harness_for(g_a(answers=answers, mentions=mentions))[0], FakeSource(()), Minter())
        doc = through_service_with(compiler, {**CANONICAL, "text": golden.text})
        assert check(golden, doc) == [], (case_id, check(golden, doc))


def test_the_pidgin_file_and_its_readme_say_synthetic() -> None:
    """A538 / AGENT-25: the lexicon is pack data labelled SYNTHETIC, in the file's own note and where the files are described."""
    pcm = json.loads(PCM_FILE.read_text(encoding="utf-8"))
    assert "SYNTHETIC" in pcm["note"]
    readme = (LEXICONS / "README.md").read_text(encoding="utf-8")
    assert "SYNTHETIC" in readme[readme.index("fin.lex.*.json"):readme.index("## Not here yet")]


# --- 4. a declared language with no lexicon file is visible at start-up (SR's read of noema#901) ----------------------------------------------------

LEXICON_LOGGER = "noema_model.understanding.compiler.lexicon"
SKIPPED_CODE = "LEXICON_REF_WITHOUT_FILE"


def _skipped_records(caplog: pytest.LogCaptureFixture) -> list[Any]:
    return [r for r in caplog.records if r.name == LEXICON_LOGGER]


def test_the_refs_with_no_file_are_named_once_at_start_up_ids_only(tmp_path: Path, caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level("DEBUG", logger=LEXICON_LOGGER)
    copy_lexicon(tmp_path, "fin.lex.en_ng")
    copy_lexicon(tmp_path, "fin.lex.pcm", **{k: v for k, v in json.loads(PCM_FILE.read_text(encoding="utf-8")).items() if k != "lexicon_id"})
    load_pack_lexicons(PACK, tmp_path)  # the pack also names yo, ha and ig; none has a file here
    records = _skipped_records(caplog)
    assert len(records) == 1, [r.getMessage() for r in records]  # once, not once per ref
    assert records[0].getMessage() == SKIPPED_CODE  # one fixed code, no free text
    assert getattr(records[0], "lexicon_refs") == ["fin.lex.ha", "fin.lex.ig", "fin.lex.yo"]  # sorted, only the refs with no file
    assert str(tmp_path) not in " ".join(str(v) for v in vars(records[0]).values())  # no path


def test_nothing_is_logged_when_every_named_ref_has_a_file(tmp_path: Path, caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level("DEBUG", logger=LEXICON_LOGGER)
    refs = sorted({r for node in PACK["intent_nodes"] for r in node.get("lexicon_refs") or []})
    for ref in refs:
        copy_lexicon(tmp_path, ref)
    only = {"intent_nodes": [{"lexicon_refs": ["fin.lex.en_ng"]}]}
    load_pack_lexicons(only, tmp_path)
    assert _skipped_records(caplog) == []


def test_using_the_loaded_lexicon_logs_nothing_more(tmp_path: Path, caplog: pytest.LogCaptureFixture) -> None:
    caplog.set_level("DEBUG", logger=LEXICON_LOGGER)
    copy_lexicon(tmp_path, "fin.lex.en_ng")
    lexicon = load_pack_lexicons(PACK, tmp_path)
    caplog.clear()
    now = datetime(2026, 11, 16, 12, 30, tzinfo=UTC)
    proposal = Proposal(intent_node=NODE, reference_slots={}, mentions={"when": _mention("this morning")}, ambiguity_flags=(), locale="en-NG", code_switching=False, probabilities={})
    for _ in range(3):
        Normaliser(lexicon, now).normalise(make_input("where is it this morning"), proposal)
    assert _skipped_records(caplog) == []  # per request: silent
