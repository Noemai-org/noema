"""UND-06, the lexicon's numerals, uncertain amounts and directions (REQ-INT-003; EC-A17 to EC-A20; INV-02, INV-14, INV-21; AGENT-16, AGENT-25). All data is SYNTHETIC.

Written before the lexicon reads numeral words, an uncertainty word or a direction phrase: these fail until it does. The rule that stands over every test here is INV-02: a lexicon never invents an
amount. A sequence of numeral words that does not make one number (wrong order, a repeated scale, a dangling "and", an unknown word) is left unread, never repaired and never read as a near number.
"""

import copy
import json
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import pytest

from noema_model.platform.config import ConfigError
from noema_model.understanding.compiler.lexicon import MAX_SURFACE, Lexicon, Normaliser, merge_lexicons
from noema_model.understanding.pipeline import MentionProposal, Proposal
from tests.support_pipeline import make_input

ROOT = Path(__file__).resolve().parents[3]
DIR = ROOT / "packs" / "fintech" / "lexicons"
GOLDEN: dict[str, Any] = json.loads((Path(__file__).parent / "fixtures" / "lexicon_golden.v1.json").read_text(encoding="utf-8"))
EN: dict[str, Any] = json.loads((DIR / "fin.lex.en_ng.json").read_text(encoding="utf-8"))
PCM: dict[str, Any] = json.loads((DIR / "fin.lex.pcm.json").read_text(encoding="utf-8"))
LEXICON = merge_lexicons([Lexicon.from_mapping(EN), Lexicon.from_mapping(PCM)])
NOW = datetime(2026, 11, 14, 10, 0, tzinfo=UTC)


def mention(surface: str) -> MentionProposal:
    return MentionProposal(present=True, surface_form=surface, proposed_value="x", confidence=0.9)


def read(text: str, surface: str | None = None) -> Any:
    mentions = {"amount": mention(surface)} if surface is not None else {}
    proposal = Proposal(intent_node="fin.payments.exception.investigate", reference_slots={}, mentions=mentions, ambiguity_flags=(), locale="en-NG", code_switching=False, probabilities={})
    return Normaliser(LEXICON, NOW).normalise(make_input(text), proposal)


def variant(edit: Any, base: dict[str, Any] = EN) -> dict[str, Any]:
    raw = copy.deepcopy(base)
    edit(raw)
    return raw


# --- the golden set (UND-06: "the lexicon golden set passes 100%") -------------------------------------------------------------------------

def test_the_golden_file_is_synthetic_and_holds_no_float() -> None:
    assert GOLDEN["note"].startswith("SYNTHETIC")

    def walk(node: Any) -> None:
        if isinstance(node, dict):
            for v in node.values():
                walk(v)
        elif isinstance(node, list):
            for v in node:
                walk(v)
        else:
            assert not isinstance(node, float), node

    walk(GOLDEN)


@pytest.mark.parametrize("row", GOLDEN["amounts"], ids=[r["id"] for r in GOLDEN["amounts"]])
def test_golden_amounts(row: dict[str, Any]) -> None:
    result = read(row["text"], row["surface"])
    assert result.amount_minor == row["minor"], row["id"]
    assert result.amount_uncertain is row["uncertain"], row["id"]
    assert (result.currency == "NGN") is (row["minor"] is not None), row["id"]


@pytest.mark.parametrize("row", GOLDEN["directions"], ids=[r["id"] for r in GOLDEN["directions"]])
def test_golden_directions(row: dict[str, Any]) -> None:
    assert read(row["text"]).expected_direction == row["direction"], row["id"]


def test_the_golden_set_reads_the_same_twice() -> None:
    for row in GOLDEN["amounts"]:
        assert read(row["text"], row["surface"]) == read(row["text"], row["surface"])


# --- EC-A19: the same Quantity in every written form ------------------------------------------------------------------------------------

def test_every_written_form_of_twenty_thousand_naira_is_one_quantity() -> None:
    forms = ["20k", "20,000", "20000", "20 thousand naira", "twenty thousand", "twenty thousand naira", "₦20,000", "Twenty Thousand", "twenty-thousand"]
    results = {f: read(f"I sent {f} to Tunde", f).amount_minor for f in forms}
    assert set(results.values()) == {2_000_000}, results


# --- the round trip of numerals (PERD: property tests, round-trip numerals) ---------------------------------------------------------------

_UNITS = ["", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"]
_TENS = ["", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"]


def spell(n: int) -> str:
    """A test-local speller for 1..999 (the oracle for the property, not the lexicon's code)."""
    words: list[str] = []
    if n >= 100:
        words += [_UNITS[n // 100], "hundred"]
        n %= 100
        if n:
            words.append("and")
    if n >= 20:
        words.append(_TENS[n // 10] + ("-" + _UNITS[n % 10] if n % 10 else ""))
    elif n:
        words.append(_UNITS[n])
    return " ".join(words)


@pytest.mark.parametrize("scale_word,scale", [("", 1), ("thousand", 1000), ("million", 1_000_000)])
def test_every_number_up_to_999_round_trips_through_its_words(scale_word: str, scale: int) -> None:
    for n in range(1, 1000):
        phrase = (spell(n) + " " + scale_word).strip()
        got = read(f"I sent {phrase} naira", phrase + " naira").amount_minor
        assert got == n * scale * 100, (phrase, got)


def test_a_thousands_group_and_a_remainder_round_trip() -> None:
    for high in (1, 7, 20, 99, 100, 150, 999):
        for low in (0, 1, 5, 11, 20, 45, 100, 250, 999):
            phrase = (spell(high) + " thousand " + (spell(low) if low else "")).strip()
            if len(phrase) > MAX_SURFACE:  # the surface bound (64) is the same as for digits: a longer quote is not read, whatever it says
                assert read(f"I sent {phrase}", phrase).amount_minor is None, phrase
                continue
            got = read(f"I sent {phrase}", phrase).amount_minor
            assert got == (high * 1000 + low) * 100, (phrase, got)


# --- INV-02: nothing is invented ----------------------------------------------------------------------------------------------------------

def test_a_numeral_word_the_customers_text_does_not_hold_is_never_taken_from_the_model() -> None:
    assert read("Wetin happen to my money", "twenty thousand").amount_minor is None


def test_the_largest_spoken_amount_that_fits_the_surface_bound_stays_far_under_the_safe_integer() -> None:
    phrase = "nine hundred and ninety nine million"
    assert read(f"I sent {phrase}", phrase).amount_minor == 999_000_000 * 100


# --- the data: loaded, marked, merged ------------------------------------------------------------------------------------------------------

def test_the_numerals_and_the_uncertainty_words_are_in_the_english_file_and_marked() -> None:
    for section in ("numerals", "uncertain_words"):
        assert EN[section] and all(e["mark"] == "[synthetic seed]" for e in EN[section]), section
    assert {e["word"] for e in EN["numerals"]} >= {"one", "nineteen", "twenty", "ninety"}
    assert {"hundred", "million"} <= {e["word"] for e in EN["multipliers"]}


@pytest.mark.parametrize("edit", [
    lambda r: r["numerals"][0].pop("mark"),
    lambda r: r["numerals"][0].update({"value": 0}),
    lambda r: r["numerals"][0].update({"value": 91}),
    lambda r: r["numerals"][0].update({"value": 1.5}),
    lambda r: r["numerals"].append(dict(r["numerals"][0])),
    lambda r: r["numerals"][0].update({"word": "Two"}),
    lambda r: r["numerals"][0].update({"word": "two words"}),
    lambda r: r["numerals"].append({"word": "thousand", "value": 5, "mark": "[synthetic seed]"}),
    lambda r: r["uncertain_words"][0].pop("mark"),
    lambda r: r["uncertain_words"].append(dict(r["uncertain_words"][0])),
    lambda r: r["uncertain_words"][0].update({"word": "About"}),
    lambda r: r["uncertain_words"].append({"word": "thousand", "mark": "[synthetic seed]"}),
    lambda r: r["uncertain_words"].append({"word": "twenty", "mark": "[synthetic seed]"}),
    lambda r: r["numerals"][0].update({"surprise": 1}),
    lambda r: r.update({"numerals": []}),
])
def test_a_lexicon_whose_numerals_or_uncertainty_words_are_not_whole_is_refused_at_load(edit: Any) -> None:
    with pytest.raises(ConfigError):
        Lexicon.from_mapping(variant(edit))


@pytest.mark.parametrize("edit", [
    lambda r: r["joiners"][0].pop("mark"),
    lambda r: r["joiners"].append(dict(r["joiners"][0])),
    lambda r: r["joiners"].append({"word": "twenty", "mark": "[synthetic seed]"}),
    lambda r: r["joiners"].append({"word": "thousand", "mark": "[synthetic seed]"}),
    lambda r: r["joiners"].append({"word": "about", "mark": "[synthetic seed]"}),
    lambda r: r["joiners"].append({"word": "Plus", "mark": "[synthetic seed]"}),
    lambda r: r["joiners"][0].update({"value": 1}),
])
def test_a_lexicon_whose_joiners_are_not_whole_is_refused_at_load(edit: Any) -> None:
    with pytest.raises(ConfigError):
        Lexicon.from_mapping(variant(edit))


def test_a_lexicon_without_a_joiner_does_not_read_a_joined_number() -> None:
    bare = Lexicon.from_mapping(variant(lambda r: r.pop("joiners")))
    proposal = Proposal(intent_node="x", reference_slots={}, mentions={"amount": mention("one hundred and fifty thousand")}, ambiguity_flags=(), locale="en-NG", code_switching=False, probabilities={})
    assert Normaliser(bare, NOW).normalise(make_input("I sent one hundred and fifty thousand"), proposal).amount_minor is None
    assert read("I sent one hundred and fifty thousand", "one hundred and fifty thousand").amount_minor == 15_000_000


def test_a_numeral_two_lexicons_define_with_two_meanings_is_refused_at_merge() -> None:
    other = variant(lambda r: r.update({"lexicon_id": "fin.lex.xx", "numerals": [{"word": "one", "value": 2, "mark": "[synthetic seed]"}]}), PCM)
    with pytest.raises(ConfigError):
        merge_lexicons([Lexicon.from_mapping(EN), Lexicon.from_mapping(other)])


def test_a_word_that_is_a_numeral_in_one_lexicon_and_a_multiplier_in_another_is_refused_at_merge() -> None:
    other = variant(lambda r: r.update({"lexicon_id": "fin.lex.xx", "multipliers": [{"word": "twenty", "value": 1000, "mark": "[synthetic seed]"}]}), PCM)
    with pytest.raises(ConfigError):
        merge_lexicons([Lexicon.from_mapping(EN), Lexicon.from_mapping(other)])


def test_the_pidgin_file_still_holds_no_english_entry() -> None:
    assert "numerals" not in PCM and "uncertain_words" not in PCM
    assert {e["phrase"] for e in PCM["relative_times"]} == {"dis morning"}


def test_the_merged_lexicon_digest_changes_when_a_numeral_changes() -> None:
    other = Lexicon.from_mapping(variant(lambda r: r["numerals"][0].update({"value": 2}) or r["numerals"][1].update({"value": 1})))
    assert other.digest != Lexicon.from_mapping(EN).digest


def test_a_direction_phrase_is_data_and_a_phrase_that_names_two_values_is_refused() -> None:
    with pytest.raises(ConfigError):
        Lexicon.from_mapping(variant(lambda r: r["directions"].append({"word": "sent me", "value": "OUTGOING", "mark": "[synthetic seed]"})))


# --- a hyphen joins two numeral words and nothing else (Codex's P2 on noema#951) ---------------------------------------------------------------
# "-twenty thousand" must not become 20,000: a sign is not a separator. Digits refuse the sign ("-20k"), so words refuse it too. The hyphen of "twenty-five" stays.

@pytest.mark.parametrize("surface", [
    "-twenty thousand", "twenty thousand-", "--twenty thousand", "twenty thousand --", "- twenty thousand", "twenty thousand -", "twenty--five thousand", "twenty - five thousand",
    "twenty -five thousand", "twenty- five thousand", "-", "--", "-twenty", "twenty-", "-twenty-five thousand", "twenty-five-thousand-",
])
def test_a_sign_or_a_stray_hyphen_is_not_a_separator_so_the_amount_is_unread(surface: str) -> None:
    assert read(f"I sent {surface} to Tunde", surface).amount_minor is None, surface


@pytest.mark.parametrize("surface,minor", [
    ("twenty-five thousand", 2_500_000), ("twenty-five thousand naira", 2_500_000), ("one hundred and twenty-five", 12_500), ("ninety-nine", 9_900), ("twenty-thousand", 2_000_000),
])
def test_a_single_hyphen_between_two_numeral_words_still_reads(surface: str, minor: int) -> None:
    assert read(f"I sent {surface} to Tunde", surface).amount_minor == minor, surface


def test_a_minus_sign_before_digits_and_before_words_agree() -> None:
    assert read("I sent -20k", "-20k").amount_minor is None and read("I sent -twenty thousand", "-twenty thousand").amount_minor is None
