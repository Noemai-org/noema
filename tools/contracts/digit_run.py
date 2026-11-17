"""The digit-run pattern of every public free-text member (A154, A184, A194, A231, A258; REQ-SEC-003, REQ-API-001).

One definition. The schemas hold its output as literal characters (so Python's `re`, Pydantic's Rust regex and a JavaScript RegExp with the `u` flag read the same pattern), and
`test_public_api.py` and `rewrite_schemas()` below read it from here. Five digits of any script (Unicode category Nd), with any number of separators between two digits, are
refused. The separators are the core's own set (`record/payloadguard` `separators`): the white space of `\\s`, vertical tab and NEL, every separator (Z), format (Cf), mark (M),
dash (Pd) and bracket (Ps, Pe) character, dot, hyphen, slash, underscore, colon, the minus sign, the middle dot, the katakana middle dot and the full-width dot, slash, colon and
low line. Categories are those of the running Python (3.13: Unicode 15.1); a test recomputes them, so a Python upgrade that adds characters fails instead of leaving one unmasked.
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
SEPARATOR_CODE_POINTS = [0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x85, 0x2E, 0x2D, 0x2F, 0x5F, 0x3A, 0x2212, 0xB7, 0x30FB, 0xFF0E, 0xFF0F, 0xFF1A, 0xFF3F]
ND = _ranges(lambda cp: unicodedata.category(chr(cp)) == "Nd")
SEPARATORS = _merge(_ranges(lambda cp: unicodedata.category(chr(cp)) in SEPARATOR_CATEGORIES) + [(cp, cp) for cp in SEPARATOR_CODE_POINTS])
SEPARATOR_CLASS = _class(SEPARATORS)
DIGIT = _class(ND)
NOT_DIGIT = _class(ND, negate=True)
DIGIT_RUN = rf"^(?![\s\S]*{DIGIT}(?:{SEPARATOR_CLASS}*{DIGIT}){{4}})[\s\S]*$"


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
