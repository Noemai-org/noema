"""UND-05, the minimal lexicon (`fin.lex.en_ng` v0) and the normaliser that reads it: the deterministic reading of an amount, a time and a direction (REQ-INT-003; INV-14, INV-21; AGENT-16,
AGENT-25). All data is SYNTHETIC, and every entry of the lexicon is a `[synthetic seed]` that a benchmark and a founder decision set.

Written before `noema_model.understanding.compiler.lexicon` and `packs/fintech/lexicons/fin.lex.en_ng.json` exist: these fail with an import error until then. The shape is the one proposed on
noema#583. The lexicon's value wins over a model's (REQ-INT-003) and nothing is ever invented: an amount or a name the customer's own words do not hold is left unread.
"""

import copy
import json
from datetime import UTC, datetime, timedelta, timezone
from pathlib import Path
from typing import Any

import pytest

from noema_model.platform.config import ConfigError
from noema_model.understanding.compiler.lexicon import Lexicon, Normaliser
from noema_model.understanding.pipeline import MentionProposal, Proposal
from tests.support_pipeline import make_input

ROOT = Path(__file__).resolve().parents[3]
FILE = ROOT / "packs" / "fintech" / "lexicons" / "fin.lex.en_ng.json"
RAW: dict[str, Any] = json.loads(FILE.read_text(encoding="utf-8"))
LEXICON = Lexicon.from_mapping(RAW)
WAT = timezone(timedelta(hours=1))
NOW = datetime(2026, 11, 14, 10, 0, tzinfo=UTC)  # 11:00 in Lagos


def mention(surface: str, present: bool = True) -> MentionProposal:
    return MentionProposal(present=present, surface_form=surface, proposed_value="x", confidence=0.9)


def proposal(**mentions: MentionProposal) -> Proposal:
    return Proposal(intent_node="fin.payments.exception.investigate", reference_slots={}, mentions=mentions, ambiguity_flags=(), locale="en-NG", code_switching=False, probabilities={})


def read(text: str, now: datetime = NOW, **mentions: MentionProposal) -> Any:
    return Normaliser(LEXICON, now).normalise(make_input(text), proposal(**mentions))


def amount_of(surface: str, text: str | None = None) -> int | None:
    result = read(text if text is not None else f"I sent {surface} to Tunde", amount=mention(surface))
    return result.amount_minor  # type: ignore[no-any-return]


def variant(edit: Any) -> dict[str, Any]:
    raw = copy.deepcopy(RAW)
    edit(raw)
    return raw


# --- the data ---------------------------------------------------------------------------------------------------------------------------

def test_the_file_is_marked_synthetic_seed_everywhere_and_carries_no_float() -> None:
    assert RAW["lexicon_id"] == "fin.lex.en_ng" and RAW["mark"] == "[synthetic seed]"

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

    walk(RAW)


@pytest.mark.parametrize("edit", [
    lambda r: r["multipliers"][0].pop("mark"),
    lambda r: r["relative_times"][0].pop("mark"),
    lambda r: r["directions"][0].pop("mark"),
    lambda r: r.update({"surprise": 1}),
    lambda r: r.pop("utc_offset_minutes"),
    lambda r: r.update({"utc_offset_minutes": 24 * 60}),
    lambda r: r.update({"utc_offset_minutes": 1.5}),
    lambda r: r["currency"].update({"code": "naira"}),
    lambda r: r["currency"].update({"minor_per_major": 0}),
    lambda r: r["multipliers"].append(dict(r["multipliers"][0])),
    lambda r: r["multipliers"][0].update({"value": 0}),
    lambda r: r["relative_times"].append(dict(r["relative_times"][0])),
    lambda r: r["relative_times"][0].update({"from": "5am"}),
    lambda r: r["relative_times"][0].update({"from": "12:00", "to": "05:00"}),
    lambda r: r["relative_times"][0].update({"to": "25:00"}),
    lambda r: r["relative_times"][0].update({"to": "24:30"}),
    lambda r: r["relative_times"][0].update({"day_offset": 0.5}),
    lambda r: r["directions"].append(dict(r["directions"][0])),
    lambda r: r["directions"][0].update({"value": "SIDEWAYS"}),
])
def test_a_lexicon_that_is_not_whole_is_refused_at_load(edit: Any) -> None:
    with pytest.raises(ConfigError):
        Lexicon.from_mapping(variant(edit))


def test_the_file_loads_from_disk_and_a_missing_file_is_refused(tmp_path: Path) -> None:
    assert Lexicon.from_file(FILE).digest == LEXICON.digest
    with pytest.raises(ConfigError):
        Lexicon.from_file(tmp_path / "missing.json")


# --- amounts: the digits the customer wrote, in minor units (REQ-INT-003) --------------------------------------------------------------------

@pytest.mark.parametrize("surface,minor", [
    ("20k", 2_000_000), ("50k", 5_000_000), ("20K", 2_000_000), ("20,000", 2_000_000), ("20000", 2_000_000), ("20 thousand naira", 2_000_000),
    ("₦20,000", 2_000_000), ("N20,000", 2_000_000), ("20,000 naira", 2_000_000), ("2.5k", 250_000), ("20,000.50", 2_000_050), ("1,250k", 125_000_000),
])
def test_an_amount_is_read_from_the_digits_and_the_multiplier_in_minor_units(surface: str, minor: int) -> None:
    assert amount_of(surface) == minor


def test_the_amount_carries_the_lexicons_currency_and_is_not_uncertain() -> None:
    result = read("Wetin happen to the 20k wey I send Tunde", amount=mention("20k"))
    assert result.currency == "NGN" and result.amount_uncertain is False


@pytest.mark.parametrize("surface", ["sharp-sharp", "twenty thousand", "a lot", "", "k", "20k30", "20 dollars", "-20k", "20k k", "0x20", "2e3", "1,23,456"])
def test_what_has_no_readable_amount_is_left_unread_not_guessed(surface: str) -> None:
    # "twenty thousand" is UND-06's numeral table; v0 reads digits only, and says so by reading nothing.
    assert amount_of(surface, text=f"I sent {surface} to Tunde") is None


def test_a_fraction_of_a_minor_unit_is_not_an_amount() -> None:
    assert amount_of("20.001") is None


def test_a_surface_form_over_the_length_bound_is_not_read_so_a_huge_string_costs_nothing() -> None:
    assert amount_of("0" * 80 + "1") is None and amount_of("1") == 100


def test_an_amount_over_the_contracts_safe_integer_is_not_an_amount() -> None:
    assert amount_of("99999999999999999999k") is None


def test_an_amount_the_customers_own_words_do_not_hold_is_never_taken_from_the_model() -> None:
    result = read("Wetin happen to my money", amount=mention("999k"))
    assert result.amount_minor is None and result.currency is None


def test_an_amount_the_model_says_is_absent_is_unread_even_if_digits_are_in_the_text() -> None:
    result = read("I sent 20k to Tunde", amount=mention("20k", present=False))
    assert result.amount_minor is None


# --- times: a window, from the request's own instant (REQ-INT-003, INV-21) --------------------------------------------------------------------

def test_this_morning_is_the_morning_window_of_the_local_day() -> None:
    window = read("the money comot this morning", when=mention("this morning")).window
    assert window == (datetime(2026, 11, 14, 5, 0, tzinfo=WAT), datetime(2026, 11, 14, 12, 0, tzinfo=WAT))


def test_yesterday_is_the_whole_local_day_before() -> None:
    window = read("the 50k I sent yesterday", when=mention("yesterday")).window
    assert window == (datetime(2026, 11, 13, 0, 0, tzinfo=WAT), datetime(2026, 11, 14, 0, 0, tzinfo=WAT))


def test_the_day_is_the_local_day_not_the_utc_day() -> None:
    late = datetime(2026, 11, 13, 23, 30, tzinfo=UTC)  # 00:30 on the 14th in Lagos
    window = read("this morning", late, when=mention("this morning")).window
    assert window is not None and window[0] == datetime(2026, 11, 14, 5, 0, tzinfo=WAT)


def test_the_same_instant_gives_the_same_window_and_the_window_is_aware() -> None:
    a = read("this morning", when=mention("This Morning")).window
    b = read("this morning", when=mention("this morning")).window
    assert a == b and a is not None and a[0].tzinfo is not None and a[0] < a[1]


@pytest.mark.parametrize("surface", ["last week sometime", "", "soon", "morning", "this morning or so"])
def test_a_time_the_lexicon_has_no_phrase_for_has_no_window(surface: str) -> None:
    assert read(f"it was {surface}", when=mention(surface)).window is None


def test_a_time_the_customers_words_do_not_hold_is_never_taken_from_the_model() -> None:
    assert read("Wetin happen to my money", when=mention("yesterday")).window is None


def test_a_reference_instant_without_a_zone_is_refused() -> None:
    with pytest.raises(ValueError):
        Normaliser(LEXICON, datetime(2026, 11, 14, 10, 0))


# --- direction and counterparty ---------------------------------------------------------------------------------------------------------

@pytest.mark.parametrize("text,direction", [
    ("Wetin happen to the 20k wey I send Tunde", "OUTGOING"), ("the 50k I sent yesterday", "OUTGOING"), ("I never received the 20k", "INCOMING"), ("Check the transfer to Tunde", None), ("Should I send it again? Just resend am for me", "OUTGOING"),
])
def test_direction_is_read_from_the_whole_words_of_the_request(text: str, direction: str | None) -> None:
    assert read(text).expected_direction == direction


def test_a_request_with_both_directions_has_none() -> None:
    assert read("I sent it and he received nothing").expected_direction is None


def test_a_word_that_only_contains_a_direction_word_is_not_one() -> None:
    assert read("my resending failed").expected_direction is None


def test_the_counterparty_is_the_customers_words_only() -> None:
    assert read("the 20k I send Tunde", counterparty=mention("Tunde")).counterparty_surface == "Tunde"
    assert read("the 20k I send him", counterparty=mention("Tunde")).counterparty_surface is None
    assert read("the 20k I send Tunde", counterparty=mention("Tunde", present=False)).counterparty_surface is None


def test_the_lexicons_reading_is_deterministic() -> None:
    first = read("the 20k I sent yesterday", amount=mention("20k"), when=mention("yesterday"))
    second = read("the 20k I sent yesterday", amount=mention("20k"), when=mention("yesterday"))
    assert first == second
