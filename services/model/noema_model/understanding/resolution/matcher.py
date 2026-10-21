"""The composed recipient matcher (REQ-AMB-003, D5). Every piece of name knowledge is pack data; the engine holds none.

Tokens are compared in this order, and the best level wins: exact, folded (case, diacritics and transliteration),
alias (nickname to full name, or spelling variants), phonetic (voice path only) and initial ("T." for Tunde). Token order
does not matter: the query's tokens are matched one-to-one to the display name's tokens so that the total is the
largest possible, and the score is that total divided by the number of query tokens. Everything is exact fractions, so
the same inputs give the same score on every machine (INV-21).
"""

import hashlib
import json
import unicodedata
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from fractions import Fraction
from pathlib import Path
from typing import Any

from noema_model.platform.config import ConfigError, reject_unknown, require_int, require_mapping, require_str
from noema_model.understanding.resolution.config import exact_number

# Names with more tokens than this are cut to their first tokens. The cut can only lower or keep a score.
MAX_TOKENS = 8
_APOSTROPHES = frozenset("'’ʼ")
ACCEPTED_MARKS = frozenset({"[synthetic seed]", "[native-speaker reviewed]"})
_TIER_ORDER = ("exact", "folded", "alias", "phonetic", "initial")


def _mark(entry: Mapping[str, Any], where: str) -> None:
    if entry.get("mark") not in ACCEPTED_MARKS:
        raise ConfigError(f"{where}: every entry must carry a mark of {sorted(ACCEPTED_MARKS)}")


@dataclass(frozen=True, slots=True)
class _Token:
    raw: str
    folded: str


class RecipientMatcher:
    def __init__(self, data: Mapping[str, Any]) -> None:
        src = require_mapping(data, "matcher")
        reject_unknown(src, frozenset({"matcher_id", "version", "mark", "note", "tiers", "fold", "nicknames", "variants", "phonetic"}), "matcher")
        _mark(src, "matcher")
        self.matcher_id = require_str(src, "matcher_id", "matcher")
        self.version = require_str(src, "version", "matcher")

        tiers_src = require_mapping(src.get("tiers"), "tiers")
        reject_unknown(tiers_src, frozenset({"mark", *_TIER_ORDER}), "tiers")
        _mark(tiers_src, "tiers")
        self.tiers = {name: exact_number(tiers_src.get(name), f"tiers.{name}") for name in _TIER_ORDER}
        scores = [self.tiers[n] for n in _TIER_ORDER]
        if any(not 0 < s <= 1 for s in scores) or any(a < b for a, b in zip(scores, scores[1:], strict=False)):
            raise ConfigError("tiers: scores must lie in (0, 1] and never rise from exact down to initial")

        fold_src = require_mapping(src.get("fold"), "fold")
        reject_unknown(fold_src, frozenset({"mark", "map"}), "fold")
        _mark(fold_src, "fold")
        fold_map = require_mapping(fold_src.get("map"), "fold.map")
        if any(not isinstance(k, str) or len(k) != 1 or not isinstance(v, str) for k, v in fold_map.items()):
            raise ConfigError("fold.map: single characters to strings")
        self._fold_map = dict(fold_map)

        phon = require_mapping(src.get("phonetic"), "phonetic")
        reject_unknown(phon, frozenset({"mark", "min_key_length", "vowels", "rules"}), "phonetic")
        _mark(phon, "phonetic")
        self._min_key = require_int(phon, "min_key_length", "phonetic", minimum=1)
        self._vowels = frozenset(require_str(phon, "vowels", "phonetic"))
        rules = phon.get("rules")
        if not isinstance(rules, list) or not all(
            isinstance(r, list) and len(r) == 2 and all(isinstance(x, str) for x in r) and r[0] for r in rules
        ):
            raise ConfigError("phonetic.rules: a list of [from, to] pairs")
        self._rules = tuple((r[0], r[1]) for r in rules)

        pairs: set[tuple[str, str]] = set()
        for index, entry in enumerate(_entries(src.get("nicknames"), "nicknames")):
            where = f"nicknames[{index}]"
            reject_unknown(entry, frozenset({"nickname", "full_names", "locale", "mark"}), where)
            _mark(entry, where)
            nick = self._fold_text(require_str(entry, "nickname", where))
            fulls = entry.get("full_names")
            if not isinstance(fulls, list) or not fulls or not all(isinstance(n, str) and n for n in fulls):
                raise ConfigError(f"{where}.full_names: a non-empty list of names")
            for full in fulls:
                pairs.add((nick, self._fold_text(full)))
                pairs.add((self._fold_text(full), nick))
        for index, entry in enumerate(_entries(src.get("variants"), "variants")):
            where = f"variants[{index}]"
            reject_unknown(entry, frozenset({"names", "locale", "mark"}), where)
            _mark(entry, where)
            names = entry.get("names")
            if not isinstance(names, list) or len(names) < 2 or not all(isinstance(n, str) and n for n in names):
                raise ConfigError(f"{where}.names: at least two spellings")
            folded = [self._fold_text(n) for n in names]
            pairs.update((a, b) for a in folded for b in folded if a != b)
        self._aliases = frozenset(pairs)
        self.digest = hashlib.sha256(
            json.dumps(src, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()

    @classmethod
    def from_file(cls, path: Path) -> "RecipientMatcher":
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError) as exc:
            raise ConfigError("matcher file unreadable or not JSON") from exc
        return cls(data)

    # -- folding and tokens ---------------------------------------------------------------------------------------

    def _fold_text(self, text: str) -> str:
        decomposed = unicodedata.normalize("NFKD", unicodedata.normalize("NFC", text).casefold())
        kept = (c for c in decomposed if unicodedata.category(c) != "Mn")
        return "".join(self._fold_map.get(c, c) for c in kept)

    def _tokens(self, name: str) -> tuple[_Token, ...]:
        words: list[str] = []
        current: list[str] = []
        for ch in unicodedata.normalize("NFC", name):
            if unicodedata.category(ch)[0] in ("L", "M") or ch in _APOSTROPHES:
                current.append(ch)
            elif current:
                words.append("".join(current))
                current = []
        if current:
            words.append("".join(current))
        tokens = []
        for word in words:
            folded = self._fold_text(word)
            if folded:
                tokens.append(_Token(unicodedata.normalize("NFC", word).casefold(), folded))
        return tuple(tokens[:MAX_TOKENS])

    def phonetic_key(self, folded: str) -> str:
        text = folded
        for old, new in self._rules:
            text = text.replace(old, new)
        collapsed = [c for i, c in enumerate(text) if i == 0 or c != text[i - 1]]
        key = "".join(c for i, c in enumerate(collapsed) if i == 0 or c not in self._vowels)
        return key

    # -- scoring --------------------------------------------------------------------------------------------------

    def _token_score(self, q: _Token, c: _Token, phonetic: bool) -> Fraction:
        if q.raw == c.raw:
            return self.tiers["exact"]
        if q.folded == c.folded:
            return self.tiers["folded"]
        if (q.folded, c.folded) in self._aliases:
            return self.tiers["alias"]
        if phonetic:
            key = self.phonetic_key(q.folded)
            if len(key) >= self._min_key and key == self.phonetic_key(c.folded):
                return self.tiers["phonetic"]
        short, long_ = sorted((q.folded, c.folded), key=len)
        if len(short) == 1 and len(long_) > 1 and long_.startswith(short):
            return self.tiers["initial"]
        return Fraction(0)

    def score(self, query: str | None, display: str, *, phonetic: bool) -> Fraction:
        """The Recipient feature in [0, 1]: how well `display` matches what the customer said. 0 for no query."""
        if not query:
            return Fraction(0)
        q_tokens, c_tokens = self._tokens(query), self._tokens(display)
        if not q_tokens or not c_tokens:
            return Fraction(0)
        grid = [[self._token_score(q, c, phonetic) for c in c_tokens] for q in q_tokens]
        n, m = len(q_tokens), len(c_tokens)
        best: dict[tuple[int, int], Fraction] = {}

        def solve(i: int, used: int) -> Fraction:
            if i == n:
                return Fraction(0)
            key = (i, used)
            if key not in best:
                options = [solve(i + 1, used)]  # this query token stays unmatched
                options += [grid[i][j] + solve(i + 1, used | (1 << j)) for j in range(m) if not used & (1 << j)]
                best[key] = max(options)
            return best[key]

        return solve(0, 0) / n


def _entries(value: Any, where: str) -> Sequence[Mapping[str, Any]]:
    if not isinstance(value, list):
        raise ConfigError(f"{where}: expected a list")
    return [require_mapping(v, where) for v in value]
