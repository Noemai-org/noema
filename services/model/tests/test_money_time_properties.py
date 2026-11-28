"""DATA-02, property tests of money and time representation (REQ-DATA-002; DEC-2026-10-16 money-and-value-lists M1, M3, M4, M5; rulings A634 and A650, interim).

REQ-DATA-002: amounts are integers in minor units with a currency; floating point is never used for money; time windows are half-open `[start, end)`; every instant is UTC and the tenant zone
only decides a local day (A634: stored in UTC, converted only for display and local-day rules; A650: the zone is the pack's `tenant_zone`, `Africa/Lagos` for the synthetic tenant).

The oracles are independent of the code under test: an exact `Fraction` for conversion, the standard library's zone database for local days, and a numeral-word writer that lives in this file.
All data is SYNTHETIC. Nothing here sets a value: the exponent is read from `contracts/currencies.json`, the zone from the pack, the offset from the lexicon (AGENT-16).
"""

import json
from datetime import UTC, date, datetime, time, timedelta, timezone, tzinfo
from fractions import Fraction
from pathlib import Path
from typing import Any
from zoneinfo import ZoneInfo

import pytest
from hypothesis import HealthCheck, assume, example, given, settings
from hypothesis import strategies as st
from pydantic import ValidationError

from noema_model.contracts._base import validator_for
from noema_model.contracts.common_v1 import CommonV1Timestamp
from noema_model.contracts.money_v1 import MoneyV1Money
from noema_model.understanding.compiler.lexicon import MAX_SAFE_INT, Lexicon, Normaliser
from noema_model.understanding.pipeline import MentionProposal, Proposal
from tests.support_pipeline import make_input

ROOT = Path(__file__).resolve().parents[3]
LEXICON_FILE = ROOT / "packs" / "fintech" / "lexicons" / "fin.lex.en_ng.json"
LEXICON = Lexicon.from_mapping(json.loads(LEXICON_FILE.read_text(encoding="utf-8")))
CURRENCIES: dict[str, Any] = json.loads((ROOT / "contracts" / "currencies.json").read_text(encoding="utf-8"))["currencies"]
TENANT_ZONE: str = json.loads((ROOT / "packs" / "fintech_v1.json").read_text(encoding="utf-8"))["tenant_zone"]
SCALE: int = 10 ** CURRENCIES[LEXICON.currency_code]["exponent"]  # the one table (M1); never a literal here
PROP = settings(max_examples=250, deadline=None, suppress_health_check=[HealthCheck.filter_too_much, HealthCheck.too_slow])
SECOND_LEVEL = datetime(1990, 1, 1), datetime(2100, 1, 1)


def mention(surface: str) -> MentionProposal:
    return MentionProposal(present=True, surface_form=surface, proposed_value="x", confidence=0.9)


def proposal(**mentions: MentionProposal) -> Proposal:
    return Proposal(intent_node="fin.payments.exception.investigate", reference_slots={}, mentions=mentions, ambiguity_flags=(), locale="en-NG", code_switching=False, probabilities={})


def amount_of(surface: str) -> Any:
    now = datetime(2026, 11, 14, 10, 0, tzinfo=UTC)
    return Normaliser(LEXICON, now).normalise(make_input(f"I sent {surface} to Tunde"), proposal(amount=mention(surface))).amount_minor


def window_of(phrase: str, now: datetime) -> tuple[datetime, datetime] | None:
    return Normaliser(LEXICON, now).normalise(make_input(f"it was {phrase}"), proposal(when=mention(phrase))).window


def fixed_zones() -> st.SearchStrategy[tzinfo]:
    return st.integers(min_value=-14 * 60, max_value=14 * 60).map(lambda minutes: timezone(timedelta(minutes=minutes)))


def instants() -> st.SearchStrategy[datetime]:
    """An aware instant, written in any fixed offset, over a century."""
    return st.builds(lambda naive, zone: naive.replace(tzinfo=UTC).astimezone(zone), st.datetimes(min_value=SECOND_LEVEL[0], max_value=SECOND_LEVEL[1]), fixed_zones())


def exact_minor(text: str, multiplier: int = 1) -> Fraction:
    """The exact value in minor units of a written decimal amount: no float, no rounding."""
    return Fraction(text) * multiplier * SCALE


# ---- M1: one table, no hard-coded exponent ---------------------------------------------------------------------------------------------------------------------------------------------------


def test_the_lexicons_minor_per_major_is_the_currency_tables_exponent() -> None:
    assert LEXICON.currency_code in CURRENCIES
    assert LEXICON.minor_per_major == SCALE


# ---- M5: conversion in the lexicon, exact, with no rounding ---------------------------------------------------------------------------------------------------------------------------


@PROP
@given(n=st.integers(min_value=1, max_value=10**12), form=st.sampled_from(["{n}", "{n:,}", "₦{n:,}", "{n} naira", "{n:,} naira", "₦ {n}"]))
def test_a_written_whole_amount_converts_to_exactly_n_times_the_table_scale(n: int, form: str) -> None:
    result = amount_of(form.format(n=n))
    assert type(result) is int
    assert result == n * SCALE


@PROP
@given(mantissa=st.integers(min_value=1, max_value=10**6), word=st.sampled_from(sorted(LEXICON.multipliers)), glued=st.booleans())
def test_a_multiplier_word_converts_exactly(mantissa: int, word: str, glued: bool) -> None:
    surface = f"{mantissa}{word}" if glued else f"{mantissa} {word}"
    result = amount_of(surface)
    assert type(result) is int
    assert result == exact_minor(str(mantissa), LEXICON.multipliers[word])


@PROP
@given(
    whole=st.text(alphabet="0123456789", min_size=1, max_size=18),
    fraction=st.text(alphabet="0123456789", min_size=1, max_size=36),
    word=st.sampled_from(["", *sorted(LEXICON.multipliers)]),
)
@example(whole="1", fraction="0000000000000000000000000001", word="")  # 29 significant digits: more than a default decimal context holds
@example(whole="1", fraction="000000000000000000000000000000000000000000000000000000000000000000000000000000001", word="")
@example(whole="1234", fraction="567", word="")  # M10: 1234.567 NGN has excess precision
@example(whole="1234", fraction="50", word="")
@example(whole="1", fraction="5", word="k")
def test_a_decimal_amount_is_exact_or_unread_and_never_rounded(whole: str, fraction: str, word: str) -> None:
    surface = f"{whole}.{fraction}" + (f" {word}" if word else "")
    assume(len(surface) <= 64)  # the lexicon reads a surface of at most 64 characters (MAX_SURFACE)
    exact = exact_minor(f"{whole}.{fraction}", LEXICON.multipliers[word] if word else 1)
    assume(exact != 0)  # whether a zero amount is an amount is not decided here
    result = amount_of(surface)
    if exact.denominator != 1 or exact > MAX_SAFE_INT:
        assert result is None
    else:
        assert type(result) is int
        assert result == exact


@PROP
@given(n=st.integers(min_value=MAX_SAFE_INT // SCALE - 5, max_value=MAX_SAFE_INT // SCALE + 5))
def test_the_ceiling_of_two_to_the_53_minus_1_is_exact_in_minor_units(n: int) -> None:
    result = amount_of(str(n))
    if n * SCALE <= MAX_SAFE_INT:
        assert result == n * SCALE
    else:
        assert result is None


@PROP
@given(digits=st.text(alphabet="123456789", min_size=17, max_size=60))
def test_an_amount_far_above_the_ceiling_is_unread_never_wrapped_or_capped(digits: str) -> None:
    assume(int(digits) * SCALE > MAX_SAFE_INT)
    assert amount_of(digits) is None


@PROP
@given(n=st.integers(min_value=1, max_value=10**9), form=st.sampled_from(["-{n}", "+{n}", "{n}e3", "{n}E3", "{n}.0e2", "-{n}k", "{n}-", "({n})", "{n}.", ".{n}", "{n}%", "0x{n}"]))
def test_a_signed_exponent_or_otherwise_malformed_amount_is_unread(n: int, form: str) -> None:
    assert amount_of(form.format(n=n)) is None


_ONES = ["", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"]
_TENS = ["", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"]


def _below_hundred(n: int, hyphen: bool) -> str:
    if n < 20:
        return _ONES[n]
    tens, ones = divmod(n, 10)
    return _TENS[tens] + (("-" if hyphen else " ") + _ONES[ones] if ones else "")


def _below_thousand(n: int, joiner: bool, hyphen: bool) -> str:
    hundreds, rest = divmod(n, 100)
    parts: list[str] = []
    if hundreds:
        parts += [_ONES[hundreds], "hundred"]
    if rest:
        parts += (["and"] if joiner and hundreds else []) + [_below_hundred(rest, hyphen)]
    return " ".join(parts)


def words_of(n: int, joiner: bool, hyphen: bool) -> str:
    """Written English for 1 to 999,999,999: an oracle that does not read the lexicon."""
    millions, rest = divmod(n, 10**6)
    thousands, units = divmod(rest, 10**3)
    parts = []
    if millions:
        parts.append(_below_thousand(millions, joiner, hyphen) + " million")
    if thousands:
        parts.append(_below_thousand(thousands, joiner, hyphen) + " thousand")
    if units:
        parts.append(_below_thousand(units, joiner, hyphen))
    return " ".join(parts)


@PROP
@given(n=st.integers(min_value=1, max_value=999_999_999), joiner=st.booleans(), hyphen=st.booleans(), currency=st.booleans())
def test_numeral_words_convert_exactly_and_never_to_a_neighbour(n: int, joiner: bool, hyphen: bool, currency: bool) -> None:
    surface = words_of(n, joiner, hyphen) + (" naira" if currency else "")
    assume(len(surface) <= 64)
    result = amount_of(surface)
    assert type(result) is int
    assert result == n * SCALE


# ---- A634, A650: instants are UTC; the tenant zone only decides a local day --------------------------------------------------------------------------------------------------------


def test_the_lexicons_offset_equals_the_packs_tenant_zone_which_is_the_authority_over_a_century() -> None:
    # A729: the pack's `tenant_zone` is the authority for the tenant offset; the lexicon's fixed offset must equal it. If they differ, the lexicon is what is wrong.
    zone = ZoneInfo(TENANT_ZONE)
    assert TENANT_ZONE == "Africa/Lagos"  # A650, interim: the Founder may overrule (GOV-08)

    @PROP
    @given(at=instants())
    def check(at: datetime) -> None:
        assert at.astimezone(zone).utcoffset() == timedelta(minutes=LEXICON.utc_offset_minutes)

    check()


def local_day(at: datetime) -> date:
    return at.astimezone(ZoneInfo(TENANT_ZONE)).date()


def local_midnight(day: date) -> datetime:
    return datetime.combine(day, time(0, 0), ZoneInfo(TENANT_ZONE))


@PROP
@given(now=instants())
def test_yesterday_is_the_previous_local_day_as_a_half_open_utc_interval(now: datetime) -> None:
    window = window_of("yesterday", now)
    assert window is not None
    start, end = window
    expected_start = local_midnight(local_day(now) - timedelta(days=1)).astimezone(UTC)
    assert start.astimezone(UTC) == expected_start
    assert end.astimezone(UTC) == expected_start + timedelta(hours=24)
    assert start.utcoffset() is not None and end.utcoffset() is not None  # an aware pair, never a naive one


@PROP
@given(now=instants(), other=fixed_zones())
def test_the_same_instant_in_any_zone_gives_the_same_window(now: datetime, other: tzinfo) -> None:
    for phrase in ("yesterday", "this morning"):
        first, second = window_of(phrase, now), window_of(phrase, now.astimezone(other))
        assert first is not None and second is not None
        assert (first[0].astimezone(UTC), first[1].astimezone(UTC)) == (second[0].astimezone(UTC), second[1].astimezone(UTC))


@PROP
@given(now=instants(), probe=instants())
@example(now=datetime(2026, 11, 14, 10, 0, tzinfo=UTC), probe=datetime(2026, 11, 14, 11, 0, tzinfo=UTC))  # 12:00 in Lagos: the end of "this morning" is not inside it
@example(now=datetime(2026, 11, 14, 10, 0, tzinfo=UTC), probe=datetime(2026, 11, 14, 4, 0, tzinfo=UTC))  # 05:00 in Lagos: the start is inside
@example(now=datetime(2026, 11, 14, 10, 0, tzinfo=UTC), probe=datetime(2026, 11, 14, 3, 59, 59, 999999, tzinfo=UTC))  # one microsecond before
def test_this_morning_is_five_to_twelve_local_and_half_open(now: datetime, probe: datetime) -> None:
    window = window_of("this morning", now)
    assert window is not None
    start, end = window
    local = probe.astimezone(ZoneInfo(TENANT_ZONE))
    expected = local.date() == local_day(now) and time(5, 0) <= local.time() < time(12, 0)
    assert (start <= probe < end) == expected


@PROP
@given(now=instants())
def test_a_boundary_instant_belongs_to_exactly_one_local_day(now: datetime) -> None:
    """Half-open: the end of yesterday is the start of today, so an instant is in one local day only."""
    yesterday = window_of("yesterday", now)
    assert yesterday is not None
    today_start = local_midnight(local_day(now)).astimezone(UTC)
    assert yesterday[1].astimezone(UTC) == today_start
    assert not (yesterday[0] <= today_start < yesterday[1])


def test_a_reference_instant_without_a_zone_is_refused_not_guessed() -> None:
    with pytest.raises(ValueError):
        Normaliser(LEXICON, datetime(2026, 11, 14, 10, 0))


# ---- the generated contracts: money is a digit-only integer in range; an instant is UTC with a Z --------------------------------------------------------------------------------------


def money_json(literal: str) -> str:
    return f'{{"amount_minor":{literal},"currency":"{LEXICON.currency_code}"}}'


@PROP
@given(n=st.integers(min_value=-(10**3), max_value=2**63))
def test_an_integer_literal_is_money_exactly_when_it_is_within_zero_and_the_ceiling(n: int) -> None:
    if 0 <= n <= MAX_SAFE_INT:
        assert MoneyV1Money.from_json(money_json(str(n))).amount_minor == n
    else:
        with pytest.raises(ValidationError):
            MoneyV1Money.from_json(money_json(str(n)))


@PROP
@given(n=st.integers(min_value=0, max_value=MAX_SAFE_INT), form=st.sampled_from(["{n}.0", "{n}.00", "{n}e0", "{n}E0", "{n}.0e0", '"{n}"', "[{n}]", "{n}.5"]))
def test_a_float_a_quoted_number_or_a_container_is_never_an_amount(n: int, form: str) -> None:
    with pytest.raises(ValidationError):
        MoneyV1Money.from_json(money_json(form.format(n=n)))


@PROP
@given(code=st.from_regex(r"\A[A-Z]{3}\Z", fullmatch=True))
@example(code="NGN")
def test_a_currency_outside_the_table_is_refused(code: str) -> None:
    text = f'{{"amount_minor":1,"currency":"{code}"}}'
    if code in CURRENCIES:
        assert MoneyV1Money.from_json(text).currency.value == code
    else:
        with pytest.raises(ValidationError):
            MoneyV1Money.from_json(text)


@PROP
@given(at=instants())
def test_a_utc_instant_with_z_is_a_timestamp_and_the_same_instant_with_an_offset_is_not(at: datetime) -> None:
    check = validator_for(CommonV1Timestamp)
    utc = at.astimezone(UTC)
    assert check.validate_python(utc.strftime("%Y-%m-%dT%H:%M:%SZ")) == utc.strftime("%Y-%m-%dT%H:%M:%SZ")
    assert check.validate_python(utc.strftime("%Y-%m-%dT%H:%M:%S.%f") + "Z")
    if at.utcoffset() != timedelta(0):
        with pytest.raises(ValidationError):
            check.validate_python(at.isoformat())
    for bad in (utc.strftime("%Y-%m-%dT%H:%M:%Sz"), utc.strftime("%Y-%m-%dT%H:%M:%SZ") + "\n", utc.strftime("%Y-%m-%d %H:%M:%SZ"), utc.strftime("%Y-%m-%dT%H:%M:%S") + "+00:00"):
        with pytest.raises(ValidationError):
            check.validate_python(bad)


# A758 (DATA-02; REQ-DATA-002; INV-02, INV-21): a run of digits longer than 4300 characters is UNREAD, never an error. CPython refuses to turn a longer integer string into an int (ValueError), and the
# digit reader turned the customer's number into a Fraction without checking its length. Today the model's quote is cut at MAX_SURFACE (64) before the reader, so the public seam already answers "unread"; this
# pins the reader itself, so raising that cap or a second caller can never turn a long number into an exception. At exactly 4300 characters the reader still reads (the amount is then unread by the safe-integer ceiling).
LONG_RUN = 4300


@pytest.mark.parametrize("template", ["{d}", "{d}.5", "1.{d}", "{d}k", "{d}{d}", "1,{d}"])
def test_a_digit_run_over_4300_characters_is_unread_and_never_an_error(template: str) -> None:
    normaliser = Normaliser(LEXICON, datetime(2026, 11, 14, 10, 0, tzinfo=UTC))
    number = template.format(d="9" * (LONG_RUN + 1))
    assert normaliser._digits(number) is None


@pytest.mark.parametrize("template", ["{d}", "{d}.5", "1.{d}", "{d}k"])
def test_the_digit_reader_still_reads_a_run_at_the_limit(template: str) -> None:
    normaliser = Normaliser(LEXICON, datetime(2026, 11, 14, 10, 0, tzinfo=UTC))
    digits = LONG_RUN - len(template.replace("{d}", ""))
    assert digits > 0
    assert normaliser._digits(template.format(d="9" * digits)) is not None


@pytest.mark.parametrize("template", ["{d}", "naira {d}", "{d}k", "{d}.5"])
def test_a_quoted_amount_with_a_run_over_4300_characters_is_unread_at_the_seam(template: str) -> None:
    assert amount_of(template.format(d="9" * (LONG_RUN + 1))) is None


def test_the_limit_counts_the_digits_and_not_the_commas_that_group_them() -> None:
    normaliser = Normaliser(LEXICON, datetime(2026, 11, 14, 10, 0, tzinfo=UTC))
    grouped = "9" + ",999" * ((LONG_RUN - 1) // 3)  # exactly 4300 digits, written with 1433 commas
    assert len(grouped.replace(",", "")) == LONG_RUN and len(grouped) > LONG_RUN
    assert normaliser._digits(grouped) is not None
    assert normaliser._digits(grouped + ",999") is None  # 4303 digits
