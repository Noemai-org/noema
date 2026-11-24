"""The deterministic lexicon and the normaliser that reads it (UND-05; REQ-INT-003; INV-14, INV-21; AGENT-16, AGENT-25).

`Lexicon` is pack data (`packs/fintech/lexicons/fin.lex.en_ng.json`, loaded, never imported as code, SoT §51): the currency, the multiplier words (`k`, `thousand`), the relative-time phrases with
their windows, and the direction words. Every entry is a `[synthetic seed]`: invented for the POC, not reviewed by a native speaker (OQ-12); the loader refuses an entry without the mark, and
the values move only through a benchmark run and a founder decision (AGENT-16). No industry or country word is in this code. All numbers are integers, so the same file means the same thing on
every machine (INV-21).

`Normaliser` is the pipeline's `Normalise` seam, built per request with the request's reference instant (the canonical input's `received_at`, the core's own clock, so replay gives the same
window). Its values win over a model's (REQ-INT-003) and nothing is invented: an amount, a time or a name that the customer's own words do not hold is left unread, never taken from the
model. It reads digits ("20k", "20,000", "20 thousand naira") and, from UND-06 slice 1 (ruling A634), numeral words ("twenty thousand naira", "one hundred and fifty thousand"): a sequence of words that does
not make one number (wrong order, a repeated or ascending scale, a dangling joiner, an unknown word) is left unread, never repaired (INV-02). A word that marks an amount as uncertain ("about 20k") sets
`amount_uncertain`, and a direction may be a phrase of up to three words ("sent me" is money received, EC-A20).
"""

import hashlib
import json
import logging
import re
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from datetime import date, datetime, time, timedelta, timezone
from decimal import Decimal, localcontext
from pathlib import Path
from typing import Any

from noema_model.platform.config import ConfigError, reject_unknown, require_mapping, require_str
from noema_model.understanding.pipeline import NormalisedSlots, PipelineInput, Proposal

logger = logging.getLogger(__name__)
SKIPPED_CODE = "LEXICON_REF_WITHOUT_FILE"

MARK = "[synthetic seed]"
MAX_SAFE_INT = 9007199254740991
MAX_SURFACE = 64
_MAX_OFFSET_MINUTES = 14 * 60
_DIRECTIONS = frozenset({"OUTGOING", "INCOMING"})
_CODE = re.compile(r"[A-Z]{3}")
_CLOCK = re.compile(r"([0-9]{2}):([0-9]{2})")
_LETTERS = re.compile(r"[^\W\d_]+")
_PRECEDING_WORD = re.compile(r"([^\W\d_]+)\s*$")
_TOP = frozenset({"lexicon_id", "version", "mark", "note", "utc_offset_minutes", "currency", "multipliers", "relative_times", "directions", "numerals", "uncertain_words", "joiners"})
_MAX_PHRASE_WORDS = 3


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
    numerals: Mapping[str, int]
    uncertain_words: frozenset[str]
    joiners: frozenset[str]
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
            word = " ".join(_word(item, "word", where, spaces=True).split())
            if word in directions or item.get("value") not in _DIRECTIONS or len(word.split()) > _MAX_PHRASE_WORDS:
                raise ConfigError(f"{where}: a word is named twice, it is longer than {_MAX_PHRASE_WORDS} words, or its value is not OUTGOING or INCOMING")
            directions[word] = str(item["value"])
        numerals: dict[str, int] = {}
        for index, item in enumerate(_items(src, "numerals")):
            where = f"lexicon.numerals[{index}]"
            _marked(item, where)
            reject_unknown(item, frozenset({"word", "value", "mark"}), where)
            word = _word(item, "word", where)
            if word in numerals or word in multipliers:
                raise ConfigError(f"{where}: a word is named twice, or it is also a multiplier")
            numerals[word] = _int(item.get("value"), f"{where}.value", low=1, high=90)
        uncertain = _plain_words(src, "uncertain_words", forbidden=(numerals, multipliers))
        joiners = _plain_words(src, "joiners", forbidden=(numerals, multipliers, uncertain))
        minor = _int(currency.get("minor_per_major"), "lexicon.currency.minor_per_major", low=1, high=10**6)
        digest = hashlib.sha256(json.dumps(src, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        return cls(require_str(src, "lexicon_id", "lexicon"), require_str(src, "version", "lexicon"), offset, code, minor, currency_words, multipliers, times, directions, numerals,
                   frozenset(uncertain), frozenset(joiners), digest, _amount_pattern(currency_words, tuple(multipliers)))


def merge_lexicons(parts: Sequence[Lexicon]) -> Lexicon:
    """One lexicon from the lexicons of one pack (UND-16, run #6; A532). They must agree on the offset and the currency; a word or phrase that two of them define must mean the same in both. Anything else is
    refused, never resolved by order: a language is never mapped onto another by a merge. Each language keeps its own file; the entry for a Pidgin phrase is in the Pidgin file."""
    if not parts:
        raise ConfigError("lexicon: there is nothing to merge")
    first = parts[0]
    for other in parts[1:]:
        if (other.utc_offset_minutes, other.currency_code, other.minor_per_major) != (first.utc_offset_minutes, first.currency_code, first.minor_per_major):
            raise ConfigError(f"lexicon {other.lexicon_id}: the offset and the currency are not those of {first.lexicon_id}")
    multipliers: dict[str, int] = {}
    times: dict[str, RelativeTime] = {}
    directions: dict[str, str] = {}
    numerals: dict[str, int] = {}
    for part in parts:
        for target, source, what in ((multipliers, part.multipliers, "word"), (times, part.times, "phrase"), (directions, part.directions, "word"), (numerals, part.numerals, "numeral")):
            for key, value in source.items():
                if target.setdefault(key, value) != value:  # type: ignore[arg-type]
                    raise ConfigError(f"lexicon {part.lexicon_id}: a {what} is defined with another meaning in another lexicon")
    uncertain = frozenset(w for part in parts for w in part.uncertain_words)
    joiners = frozenset(w for part in parts for w in part.joiners)
    # A word may be one kind of word only: two lexicons may not make the same word a numeral in one and a multiplier or a marker in another.
    kinds = (frozenset(numerals), frozenset(multipliers), uncertain, joiners)
    if sum(len(k) for k in kinds) != len(frozenset().union(*kinds)):
        raise ConfigError("lexicon: a word is a numeral, a multiplier, an uncertainty word or a joiner in one lexicon and another kind in another")
    for section, merged_section in (("multipliers", multipliers), ("relative_times", times), ("directions", directions)):
        if not merged_section:  # a supplemental file may omit a section; the merged lexicon may not (C1's and Codex's finding on noema#901). Names the section only, never a value
            raise ConfigError(f"lexicon.{section}: the merged lexicon holds no entry; a pack's lexicons must together hold a non-empty list")
    words = tuple(sorted({w for part in parts for w in part.currency_words}, key=lambda w: (-len(w), w)))
    ids = [part.lexicon_id for part in parts]
    digest = hashlib.sha256("\n".join(part.digest for part in parts).encode()).hexdigest()
    return Lexicon("+".join(ids), "+".join(part.version for part in parts), first.utc_offset_minutes, first.currency_code, first.minor_per_major, words, multipliers, times, directions, numerals,
                   uncertain, joiners, digest, _amount_pattern(words, tuple(multipliers)))


_REF = re.compile(r"[a-z][a-z0-9_]*(?:\.[a-z0-9_]+)*")


def load_pack_lexicons(pack: Mapping[str, Any], directory: Path) -> Lexicon:
    """The lexicon of a pack: the files its nodes name in `lexicon_refs`, in the order they are first named, merged (UND-16, run #6; A532).

    A ref is a plain id, never a path: the file is `<directory>/<ref>.json` and its own `lexicon_id` must be the ref. A ref with no file is skipped, as the refs for the languages with no lexicon yet always were
    (`yo`, `ha`, `ig` at this point), and their ids are named once in a warning at start-up so a declared language with no file is visible; a pack none of whose refs has a file is refused. A file the pack does not name is never read."""
    refs: list[str] = []
    for node in require_mapping(pack, "pack").get("intent_nodes") or []:
        for ref in require_mapping(node, "pack.intent_nodes[]").get("lexicon_refs") or []:
            if not isinstance(ref, str) or _REF.fullmatch(ref) is None:
                raise ConfigError("pack.lexicon_refs: expected plain lexicon ids")
            if ref not in refs:
                refs.append(ref)
    parts: list[Lexicon] = []
    without_file: list[str] = []
    for ref in refs:
        path = directory / f"{ref}.json"
        if not path.is_file():
            without_file.append(ref)
            continue
        part = Lexicon.from_file(path)
        if part.lexicon_id != ref:
            raise ConfigError(f"lexicon {ref}: the file holds another lexicon id")
        parts.append(part)
    if not parts:
        raise ConfigError("pack: none of its lexicon_refs has a lexicon file")
    if without_file:
        logger.warning(SKIPPED_CODE, extra={"lexicon_refs": sorted(without_file)})  # ids only, once: this runs at start-up, never per request
    return merge_lexicons(parts)


def _items(src: Mapping[str, Any], key: str) -> list[Mapping[str, Any]]:
    """The entries of one section. A section may be absent (a lexicon of one language adds only what that language needs, UND-16); a section that is there is a non-empty list."""
    if key not in src:
        return []
    value = src[key]
    if not isinstance(value, list) or not value:
        raise ConfigError(f"lexicon.{key}: expected a non-empty list")
    return [require_mapping(v, f"lexicon.{key}[]") for v in value]


def _plain_words(src: Mapping[str, Any], key: str, *, forbidden: Sequence[Mapping[str, Any] | frozenset[str] | set[str] | list[str]]) -> list[str]:
    """A section of single marked words with no value (the uncertainty words, the joiners). A word is named once and is not a numeral, a multiplier or a word of another such section."""
    words: list[str] = []
    for index, item in enumerate(_items(src, key)):
        where = f"lexicon.{key}[{index}]"
        _marked(item, where)
        reject_unknown(item, frozenset({"word", "mark"}), where)
        word = _word(item, "word", where)
        if word in words or any(word in other for other in forbidden):
            raise ConfigError(f"{where}: a word is named twice, or it is a numeral, a multiplier or a word of another section")
        words.append(word)
    return words


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
        amount, uncertain = self._amount(proposal, text)
        return NormalisedSlots(
            amount_minor=amount,
            currency=self._lexicon.currency_code if amount is not None else None,
            amount_uncertain=uncertain and amount is not None,
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

    def _amount(self, proposal: Proposal, text: str) -> tuple[int | None, bool]:
        """The amount in minor units and whether the customer said it was not exact. Digits first, then numeral words; anything else is unread (INV-02)."""
        surface = self._said(proposal, "amount", text)
        if not surface:
            return None, False
        tokens = surface.split()
        uncertain = False
        while tokens and tokens[0] in self._lexicon.uncertain_words:  # "roughly 20k": the marker is the customer's, the amount is what follows
            tokens, uncertain = tokens[1:], True
        core = " ".join(tokens)
        if not core:
            return None, False
        major = self._digits(core)
        if major is None:
            major = self._spoken(core)
        if major is None:
            return None, False
        value = major * self._lexicon.minor_per_major
        if value != value.to_integral_value() or value > MAX_SAFE_INT:
            return None, False
        return int(value), uncertain or self._marked_uncertain(core, text)

    def _digits(self, core: str) -> Decimal | None:
        match = self._lexicon.amount_pattern.fullmatch(core)
        if match is None:
            return None
        with localcontext() as context:
            context.prec = 80
            return Decimal(match.group("number").replace(",", "")) * self._lexicon.multipliers.get(match.group("multiplier") or "", 1)

    def _spoken(self, core: str) -> Decimal | None:
        """Numeral words to one whole number, or nothing. Small words build a group under a thousand (twenty five; one hundred and fifty), a scale of a thousand or more closes the group, and scales must fall
        (twenty thousand five hundred, never five hundred thousand thousand). A word that is not in the lexicon, an order that is not a number, or a joiner that is not between a hundred and its remainder
        makes the whole thing unread: the lexicon never repairs and never takes the nearest number."""
        lex = self._lexicon
        tokens = [t for t in re.split(r"[\s\-]+", core) if t]
        if tokens and tokens[-1] in lex.currency_words:
            tokens.pop()
        if tokens and tokens[0] in lex.currency_words:
            tokens.pop(0)
        if not tokens:
            return None
        total, hundreds, small = 0, 0, 0
        last_scale = MAX_SAFE_INT
        after_hundred = joining = False
        for token in tokens:
            if token in lex.joiners:
                if not after_hundred:
                    return None
                after_hundred, joining = False, True
                continue
            joining = False
            if token in lex.numerals:
                value = lex.numerals[token]
                if (value < 10 and small not in (0, 20, 30, 40, 50, 60, 70, 80, 90)) or (value >= 10 and small != 0):
                    return None
                small += value
                after_hundred = False
            elif token in lex.multipliers:
                scale = lex.multipliers[token]
                if scale == 100:
                    if not 1 <= small <= 9 or hundreds:
                        return None
                    hundreds, small, after_hundred = small, 0, True
                elif scale >= 1000:
                    group = hundreds * 100 + small
                    if group == 0 or scale >= last_scale:
                        return None
                    total += group * scale
                    hundreds, small, last_scale, after_hundred = 0, 0, scale, False
                else:
                    return None
            else:
                return None
        if joining:
            return None
        total += hundreds * 100 + small
        return Decimal(total) if total > 0 else None

    def _marked_uncertain(self, core: str, text: str) -> bool:
        """The customer's own word just before the amount ("I sent about 20k") marks it as not exact. Any place the amount is written counts, so a doubt anywhere widens the search, never narrows it."""
        start = text.find(core)
        while start != -1:
            before = _PRECEDING_WORD.search(text[:start])
            if before is not None and before.group(1) in self._lexicon.uncertain_words:
                return True
            start = text.find(core, start + 1)
        return False

    def _window(self, proposal: Proposal, text: str) -> tuple[datetime, datetime] | None:
        surface = self._said(proposal, "when", text)
        phrase = self._lexicon.times.get(surface) if surface else None
        if phrase is None:
            return None
        midnight = datetime.combine(self._today + timedelta(days=phrase.day_offset), time(0, 0), self._zone)
        return midnight + timedelta(minutes=phrase.start_minute), midnight + timedelta(minutes=phrase.end_minute)

    def _direction(self, text: str) -> str | None:
        """The one direction the customer's words give, or nothing. A phrase wins over its own words ("sent me" is money received, EC-A20, not "sent": money sent), longest first."""
        words = _LETTERS.findall(text)
        found: set[str] = set()
        index = 0
        while index < len(words):
            for length in range(_MAX_PHRASE_WORDS, 0, -1):
                value = self._lexicon.directions.get(" ".join(words[index:index + length]))
                if value is not None:
                    found.add(value)
                    index += length
                    break
            else:
                index += 1
        return next(iter(found)) if len(found) == 1 else None

    @staticmethod
    def _counterparty(proposal: Proposal, text: str) -> str | None:
        said = Normaliser._said(proposal, "counterparty", text)
        return proposal.mentions["counterparty"].surface_form.strip() if said else None


__all__ = ("Lexicon", "Normaliser", "RelativeTime", "MARK", "load_pack_lexicons", "merge_lexicons")
