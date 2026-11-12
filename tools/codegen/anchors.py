"""CON-03, REQ-API-001, A138: the whole-string form of a contract pattern, for Python's `re`. Standard library only.

A contract pattern means "the whole string": in JSON Schema's ECMA 262 dialect and in Go, `$` without the multi-line flag matches only at the end
of the text. In Python's `re`, `$` also matches before a final newline, so `^[a-z]+$` accepts "ab\\n". Replacing every unescaped `^` with `\\A` and every
unescaped `$` with `\\Z` outside a character class gives the contract's meaning in Python; the rest of the pattern is untouched.
"""


def fully_anchored(pattern: str) -> str:
    out: list[str] = []
    i, n = 0, len(pattern)
    while i < n:
        ch = pattern[i]
        if ch == "\\":
            out.append(pattern[i : i + 2])
            i += 2
            continue
        if ch == "[":
            j = i + 1
            if j < n and pattern[j] == "^":
                j += 1
            if j < n and pattern[j] == "]":  # a ] first in a class is a literal
                j += 1
            while j < n and pattern[j] != "]":
                j += 2 if pattern[j] == "\\" else 1
            out.append(pattern[i : j + 1])
            i = j + 1
            continue
        out.append(r"\A" if ch == "^" else r"\Z" if ch == "$" else ch)
        i += 1
    return "".join(out)
