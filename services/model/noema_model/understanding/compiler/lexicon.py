"""The deterministic lexicon and the normaliser that reads it (UND-05; REQ-INT-003; INV-14, INV-21; AGENT-16, AGENT-25).

`Lexicon` is pack data (`packs/fintech/lexicons/fin.lex.en_ng.json`, loaded, never imported as code, SoT §51): the currency, the multiplier words (`k`, `thousand`), the relative-time phrases with
their windows, and the direction words. Every entry is a `[synthetic seed]`: invented for the POC, not reviewed by a native speaker (OQ-12); the loader refuses an entry without the mark, and
the values move only through a benchmark run and a founder decision (AGENT-16). No industry or country word is in this code. All numbers are integers, so the same file means the same thing on
every machine (INV-21).

`Normaliser` is the pipeline's `Normalise` seam, built per request with the request's reference instant (the canonical input's `received_at`, the core's own clock, so replay gives the same
window). Its values win over a model's (REQ-INT-003) and nothing is invented: an amount, a time or a name that the customer's own words do not hold is left unread, never taken from the
model. v0 reads digits only ("20k", "20,000", "20 thousand naira"); numeral words are UND-06's.
"""

import hashlib
import json
import re
from collections.abc import Mapping
from dataclasses import dataclass
from datetime import date, datetime, time, timedelta, timezone
from decimal import Decimal, localcontext
from pathlib import Path
from typing import Any

from noema_model.platform.config import ConfigError, reject_unknown, require_mapping, require_str
from noema_model.understanding.pipeline import NormalisedSlots, PipelineInput, Proposal

MARK = "[synthetic seed]"
MAX_SAFE_INT = 9007199254740991
MAX_SURFACE = 64
_MAX_OFFSET_MINUTES = 14 * 60
_DIRECTIONS = frozenset({"OUTGOING", "INCOMING"})
_CODE = re.compile(r"[A-Z]{3}")
_CLOCK = re.compile(r"([0-9]{2}):([0-9]{2})")
_LETTERS = re.compile(r"[^\W\d_]+")
_TOP = frozenset({"lexicon_id", "version", "mark", "note", "utc_offset_minutes", "currency", "multipliers", "relative_times", "directions"})


def _int(value: Any, where: str, *, low: int, high: int) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or not low <= value <= high:
        raise ConfigError(f"{where}: expected an integer from {low} to {high}")
    return value


def _marked(item: Mapping[str, Any], where: str) -> None:
    if item.get("mark") != MARK:
        raise ConfigError(f"{where}: every entry carries the mark {MARK}")


def _word(item: Mapping[str, Any], key: str, where: str, *, spaces: bool = False) -> str:
    word = require_str(item, key, where)
    if word != word.casefold() or word != word.strip() or (not spaces and any(c.isspace() for c in word)):
        raise ConfigError(f"{where}.{key}: expected lower case with no surrounding space" + ("" if spaces else " and no space inside"))
    return word


def _minutes(value: Any, where: str) -> int:
    match = _CLOCK.fullmatch(value) if isinstance(value, str) else None
    if match is None:
        raise ConfigError(f"{where}: expected HH:MM")
    hours, minutes = int(match.group(1)), int(match.group(2))
    if minutes > 59 or hours > 24 or (hours == 24 and minutes != 0):
        raise ConfigError(f"{where}: not a time of day")
    return hours * 60 + minutes


@dataclass(frozen=True, slots=True)
class RelativeTime:
    phrase: str
    day_offset: int
    start_minute: int
    end_minute: int


@dataclass(frozen=True, slots=True)
class Lexicon:
    lexicon_id: str
    version: str
    utc_offset_minutes: int
    currency_code: str
    minor_per_major: int
    currency_words: tuple[str, ...]
    multipliers: Mapping[str, int]
    times: Mapping[str, RelativeTime]
    directions: Mapping[str, str]
    digest: str
    amount_pattern: "re.Pattern[str]"

    @classmethod
    def from_file(cls, path: Path) -> "Lexicon":
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as err:
            raise ConfigError("lexicon file unreadable or not JSON") from err
        return cls.from_mapping(data)

    @classmethod
    def from_mapping(cls, raw: Any) -> "Lexicon":
        src = require_mapping(raw, "lexicon")
        reject_unknown(src, _TOP, "lexicon")
        _marked(src, "lexicon")
        offset = _int(src.get("utc_offset_minutes"), "lexicon.utc_offset_minutes", low=-_MAX_OFFSET_MINUTES, high=_MAX_OFFSET_MINUTES)
        currency = require_mapping(src.get("currency"), "lexicon.currency")
        reject_unknown(currency, frozenset({"code", "minor_per_major", "words", "mark"}), "lexicon.currency")
        _marked(currency, "lexicon.currency")
        code = require_str(currency, "code", "lexicon.currency")
        if not _CODE.fullmatch(code):
            raise ConfigError("lexicon.currency.code: expected a three-letter code such as NGN")
        words = currency.get("words")
        if not isinstance(words, list) or not words or len(set(words)) != len(words):
            raise ConfigError("lexicon.currency.words: expected a list of distinct words")
        currency_words = tuple(sorted((_word({"word": w}, "word", "lexicon.currency.words") for w in words), key=lambda w: (-len(w), w)))
        multipliers: dict[str, int] = {}
        for index, item in enumerate(_items(src, "multipliers")):
            where = f"lexicon.multipliers[{index}]"
            _marked(item, where)
            reject_unknown(item, frozenset({"word", "value", "mark"}), where)
            word = _word(item, "word", where)
            if word in multipliers:
                raise ConfigError(f"{where}: a word is named twice")
            multipliers[word] = _int(item.get("value"), f"{where}.value", low=1, high=10**9)
        times: dict[str, RelativeTime] = {}
        for index, item in enumerate(_items(src, "relative_times")):
            where = f"lexicon.relative_times[{index}]"
            _marked(item, where)
            reject_unknown(item, frozenset({"phrase", "day_offset", "from", "to", "mark"}), where)
            phrase = " ".join(_word(item, "phrase", where, spaces=True).split())
            start, end = _minutes(item.get("from"), f"{where}.from"), _minutes(item.get("to"), f"{where}.to")
            if phrase in times or start >= end:
                raise ConfigError(f"{where}: a phrase is named twice, or its window does not run forward")
            times[phrase] = RelativeTime(phrase, _int(item.get("day_offset"), f"{where}.day_offset", low=-366, high=366), start, end)
        directions: dict[str, str] = {}
        for index, item in enumerate(_items(src, "directions")):
            where = f"lexicon.directions[{index}]"
            _marked(item, where)
            reject_unknown(item, frozenset({"word", "value", "mark"}), where)
            word = _word(item, "word", where)
            if word in directions or item.get("value") not in _DIRECTIONS:
                raise ConfigError(f"{where}: a word is named twice, or its value is not OUTGOING or INCOMING")
            directions[word] = str(item["value"])
        minor = _int(currency.get("minor_per_major"), "lexicon.currency.minor_per_major", low=1, high=10**6)
        digest = hashlib.sha256(json.dumps(src, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        return cls(require_str(src, "lexicon_id", "lexicon"), require_str(src, "version", "lexicon"), offset, code, minor, currency_words, multipliers, times, directions, digest,
                   _amount_pattern(currency_words, tuple(multipliers)))


def _items(src: Mapping[str, Any], key: str) -> list[Mapping[str, Any]]:
    value = src.get(key)
    if not isinstance(value, list) or not value:
        raise ConfigError(f"lexicon.{key}: expected a non-empty list")
    return [require_mapping(v, f"lexicon.{key}[]") for v in value]


def _amount_pattern(currency_words: tuple[str, ...], multipliers: tuple[str, ...]) -> "re.Pattern[str]":
    currency = "|".join(re.escape(w) for w in currency_words)
    multiplier = "|".join(re.escape(w) for w in sorted(multipliers, key=lambda w: (-len(w), w)))
    number = r"[0-9]{1,3}(?:,[0-9]{3})+(?:\.[0-9]+)?|[0-9]+(?:\.[0-9]+)?"
    return re.compile(rf"(?:(?:{currency})\s*)?(?P<number>{number})(?:\s*(?P<multiplier>{multiplier}))?(?:\s*(?:{currency}))?")


def _squash(text: str) -> str:
    return " ".join(text.casefold().split())


class Normaliser:
    """The `Normalise` seam for one request: `Normaliser(lexicon, received_at).normalise` is what `run_pipeline` is given."""

    def __init__(self, lexicon: Lexicon, now: datetime) -> None:
        if now.tzinfo is None or now.utcoffset() is None:
            raise ValueError("the reference instant needs a time zone")
        self._lexicon = lexicon
        self._zone = timezone(timedelta(minutes=lexicon.utc_offset_minutes))
        self._today: date = now.astimezone(self._zone).date()

    def normalise(self, inp: PipelineInput, proposal: Proposal) -> NormalisedSlots:
        text = _squash(inp.text)
        amount = self._amount(proposal, text)
        return NormalisedSlots(
            amount_minor=amount,
            currency=self._lexicon.currency_code if amount is not None else None,
            amount_uncertain=False,
            window=self._window(proposal, text),
            counterparty_surface=self._counterparty(proposal, text),
            expected_direction=self._direction(text),
        )

    @staticmethod
    def _said(proposal: Proposal, name: str, text: str) -> str | None:
        """The model's quote for a slot, only if the model says it is present and the customer's own words hold it. Anything else is unread (nothing is invented)."""
        mention = proposal.mentions.get(name)
        if mention is None or not mention.present:
            return None
        surface = _squash(mention.surface_form)
        return surface if surface and len(surface) <= MAX_SURFACE and surface in text else None

    def _amount(self, proposal: Proposal, text: str) -> int | None:
        surface = self._said(proposal, "amount", text)
        match = self._lexicon.amount_pattern.fullmatch(surface) if surface else None
        if match is None:
            return None
        with localcontext() as context:
            context.prec = 80
            value = Decimal(match.group("number").replace(",", "")) * self._lexicon.multipliers.get(match.group("multiplier") or "", 1) * self._lexicon.minor_per_major
            if value != value.to_integral_value() or value > MAX_SAFE_INT:
                return None
            return int(value)

    def _window(self, proposal: Proposal, text: str) -> tuple[datetime, datetime] | None:
        surface = self._said(proposal, "when", text)
        phrase = self._lexicon.times.get(surface) if surface else None
        if phrase is None:
            return None
        midnight = datetime.combine(self._today + timedelta(days=phrase.day_offset), time(0, 0), self._zone)
        return midnight + timedelta(minutes=phrase.start_minute), midnight + timedelta(minutes=phrase.end_minute)

    def _direction(self, text: str) -> str | None:
        found = {self._lexicon.directions[w] for w in _LETTERS.findall(text) if w in self._lexicon.directions}
        return next(iter(found)) if len(found) == 1 else None

    @staticmethod
    def _counterparty(proposal: Proposal, text: str) -> str | None:
        said = Normaliser._said(proposal, "counterparty", text)
        return proposal.mentions["counterparty"].surface_form.strip() if said else None


__all__ = ("Lexicon", "Normaliser", "RelativeTime", "MARK")
