"""RFC 8785 (JCS) for the Record, written from the RFC. Standard library only; a reference for the generators of the golden
vectors, not production code. DEC-2026-10-27-actor-and-payload-hash, PERD v1.17 §32.1.

Scope: objects, arrays, strings, booleans, null and integers from -(2^53-1) to 2^53-1. That is what a Record payload may hold:
any other number (a fraction, an exponent form, a larger integer) is refused, so no hash is ever computed over it.
Members sort by UTF-16 code unit, as the RFC requires (this differs from code point order for characters above U+FFFF).
"""
import hashlib
import json

MAX_SAFE = 2**53 - 1


class NotCanonical(ValueError):
    """The value holds something a Record payload may not: a non-integer number, a larger integer, or a non-JSON type."""


def jcs(value) -> str:
    if value is None or isinstance(value, bool):
        return json.dumps(value)
    if isinstance(value, int):
        if abs(value) > MAX_SAFE:
            raise NotCanonical(f"integer {value} is outside 2^53-1")
        return str(value)
    if isinstance(value, float):
        raise NotCanonical(f"number {value!r} is not an integer")
    if isinstance(value, str):
        return json.dumps(value, ensure_ascii=False)
    if isinstance(value, list):
        return "[" + ",".join(jcs(v) for v in value) + "]"
    if isinstance(value, dict):
        for k in value:
            if not isinstance(k, str):
                raise NotCanonical("object member names are strings")
        members = sorted(value.items(), key=lambda kv: kv[0].encode("utf-16-be"))
        return "{" + ",".join(json.dumps(k, ensure_ascii=False) + ":" + jcs(v) for k, v in members) + "}"
    raise NotCanonical(f"type {type(value).__name__} is not JSON")


def sha256_text(text: str) -> str:
    return "sha256:" + hashlib.sha256(text.encode("utf-8")).hexdigest()


def splice_family(text: str, key: str, family) -> str:
    """golden_vectors.json with the family `key` set to `family`; every other byte stays. A missing family is appended at the end."""
    marker = f',\n  "{key}": '
    value = json.dumps(family, indent=2, ensure_ascii=False).replace("\n", "\n  ")
    if marker in text:
        start = text.index(marker)
        _, used = json.JSONDecoder().raw_decode(text, start + len(marker))
        return text[:start] + marker + value + text[used:]
    body = text.rstrip()
    assert body.endswith("}"), "golden_vectors.json must end with a closing brace"
    return body[:-1].rstrip() + marker + value + "\n}\n"
