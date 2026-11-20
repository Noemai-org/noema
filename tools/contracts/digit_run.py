"""The digit-run pattern of every public free-text member (A154, A184, A194, A231, A258; REQ-SEC-003, REQ-API-001).

One definition. The schemas hold its output as literal characters (so Python's `re`, Pydantic's Rust regex and a JavaScript RegExp with the `u` flag read the same pattern), and
`test_public_api.py` and `rewrite_schemas()` below read it from here. Two rules, both narrowings of a free-text string:

1. Five digits of any script (Unicode category Nd), with any number of separators between two digits, are refused. The separators are the core's own set
   (`record/payloadguard` `separators`): the white space of `\\s`, vertical tab, the four information separators U+001C to U+001F (A308), NEL, every separator (Z), format (Cf), mark (M),
   dash (Pd) and bracket (Ps, Pe) character, dot, hyphen, slash, underscore, colon, the minus sign, the middle dot, the katakana middle dot and the full-width dot, slash, colon and
   low line.
2. Ten digits joined by those separators or by ASCII commas are refused (A317 to A328, interim). A comma still ends a run of the first rule ("12,345" and "20,000,000" are no run of five),
   but a comma-joined number of ten digits or more is refused: "1,234,567,890" and "4111,1111,1111,1111". The ruling exempts a string that is a whole money string of at most nine integer digits;
   in these patterns the exemption changes nothing, because such a string with ten digits or more has the point and two decimals after a group of three, five digits the first rule refuses
   already (a test enumerates the shapes), so the clause is not written into the pattern.

Categories are those of the running Python (3.13: Unicode 15.1); a test recomputes them, so a Python upgrade that adds characters fails instead of leaving one unmasked.
"""
import json
import sys
import unicodedata
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def _ranges(predicate) -> list[tuple[int, int]]:
    out: list[tuple[int, int]] = []
    for cp in range(sys.maxunicode + 1):
        if predicate(cp):
            if out and out[-1][1] == cp - 1:
                out[-1] = (out[-1][0], cp)
            else:
                out.append((cp, cp))
    return out


def _lit(cp: int) -> str:
    c = chr(cp)
    return "\\" + c if c in "\\]^-[" else c


def _class(ranges: list[tuple[int, int]], negate: bool = False) -> str:
    body = "".join(_lit(a) if a == b else f"{_lit(a)}-{_lit(b)}" for a, b in ranges)
    return "[" + ("^" if negate else "") + body + "]"


def _merge(ranges: list[tuple[int, int]]) -> list[tuple[int, int]]:
    out: list[tuple[int, int]] = []
    for a, b in sorted(set(ranges)):
        if out and a <= out[-1][1] + 1:
            out[-1] = (out[-1][0], max(out[-1][1], b))
        else:
            out.append((a, b))
    return out


SEPARATOR_CATEGORIES = ("Zs", "Zl", "Zp", "Cf", "Mn", "Mc", "Me", "Pd", "Ps", "Pe")
SEPARATOR_CODE_POINTS = [0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x1C, 0x1D, 0x1E, 0x1F, 0x85, 0x2E, 0x2D, 0x2F, 0x5F, 0x3A, 0x2212, 0xB7, 0x30FB, 0xFF0E, 0xFF0F, 0xFF1A, 0xFF3F]
ND = _ranges(lambda cp: unicodedata.category(chr(cp)) == "Nd")
SEPARATORS = _merge(_ranges(lambda cp: unicodedata.category(chr(cp)) in SEPARATOR_CATEGORIES) + [(cp, cp) for cp in SEPARATOR_CODE_POINTS])
SEPARATOR_CLASS = _class(SEPARATORS)
COMMA_JOIN_CLASS = _class(_merge(SEPARATORS + [(0x2C, 0x2C)]))  # the separators and the ASCII comma (U+002C): the join of the second rule
DIGIT = _class(ND)
NOT_DIGIT = _class(ND, negate=True)
# The two rules, each a negative look-ahead from the start: no five digits joined by separators, and no ten digits joined by separators or commas.
NO_FIVE_JOINED = rf"(?![\s\S]*{DIGIT}(?:{SEPARATOR_CLASS}*{DIGIT}){{4}})"
NO_TEN_COMMA_JOINED = rf"(?![\s\S]*{DIGIT}(?:{COMMA_JOIN_CLASS}*{DIGIT}){{9}})"
NO_RUN = NO_FIVE_JOINED + NO_TEN_COMMA_JOINED
DIGIT_RUN = rf"^{NO_RUN}[\s\S]*$"


# A278 (final; the Security Reviewer's challenge on noema#615): the rule for a fact value of reconstruction.v3 is by VALUE, under any key. The value is a core-minted ID, a resource
# reference with a letter in its body (`fin.transaction:TX88213` passes, `fin.account:0123456789012345` does not), or a string that meets the two rules above. The ID branch is exactly
# `<prefix>_<ULID>` (A317 to A328, interim; the follow-up of noema#664): a lower-case prefix of two to six letters, and a ULID of 26 characters, the first 0 to 7 and the rest
# Crockford base 32, with a letter in it (the ID rule of the Record guard, `typedIDs`). Every other typed ID is no exemption: `recev_1005` and `usr_8821` pass as strings with at most four digits in
# a row, `usr_88213` and `ten_4111111111111111a` do not. The reference branch is the ID rule of common.v1 ResourceRef and is NOT changed here (see status/CON-digit-tightenings.md).
_ID = r"[a-z]{2,6}_(?=[0-9]*[A-Za-z])[0-7][0-9A-HJKMNP-TV-Z]{25}"
_REF = r"[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:(?=[0-9_.-]*[A-Za-z])[A-Za-z0-9_.-]{1,64}"
FACT_VALUE = rf"^(?:{_ID}|{_REF}|{NO_RUN}[\s\S]*)$"


def rewrite_schemas(old: str) -> list[str]:
    """Replace the old digit-run pattern by this one in every schema that holds it. Returns the files changed."""
    changed = []
    for p in sorted((ROOT / "contracts" / "schemas").glob("*.json")):
        text = p.read_text(encoding="utf-8")
        for form in (json.dumps(old, ensure_ascii=False)[1:-1], json.dumps(old)[1:-1]):
            if form in text:
                new = json.dumps(DIGIT_RUN, ensure_ascii=False)[1:-1] if form == json.dumps(old, ensure_ascii=False)[1:-1] else json.dumps(DIGIT_RUN)[1:-1]
                p.write_text(text.replace(form, new), encoding="utf-8")
                changed.append(p.name)
                break
    return changed


if __name__ == "__main__":
    if len(sys.argv) == 2 and sys.argv[1] == "--print":
        print(DIGIT_RUN)
