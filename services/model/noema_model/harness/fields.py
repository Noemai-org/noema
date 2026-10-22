"""Masking, minimising and delimiting of every string that reaches a prompt (REQ-CTX-007, REQ-SEC-003, AGENT-09).

A string becomes a typed data field: control characters removed, account numbers masked to their last four digits,
length capped, and marked truncated when cut. The fields are then serialised as one JSON object between sentinel lines.
JSON quoting, with the angle brackets escaped, means no field content can close the block or pose as a sentinel.
"""

import json
import re
import unicodedata
from collections.abc import Mapping
from dataclasses import dataclass

from noema_model.harness.config import DataLimits
from noema_model.harness.errors import HarnessRejection, Reason
from noema_model.platform.redaction import Redactor

_FIELD_NAME = re.compile(r"^[a-z][a-z0-9_]{0,63}$")
_WHITESPACE_CONTROLS = {"\t", "\n", "\r", "\x0b", "\x0c", "\x85", " ", " "}

DATA_OPEN = "<<<DATA"
DATA_CLOSE = "DATA>>>"


@dataclass(frozen=True, slots=True)
class RenderedField:
    name: str
    text: str
    truncated: bool
    original_length: int


def clean(value: str) -> str:
    """NFC, then every control (Cc) and format (Cf) character removed; whitespace controls become one space.

    Format characters are removed too: a zero-width character between digits would otherwise hide an account number from
    the masker, and a bidirectional control can make text read differently from how it is stored.
    """
    out = []
    for ch in unicodedata.normalize("NFC", value):
        if ch in _WHITESPACE_CONTROLS:
            out.append(" ")
        elif unicodedata.category(ch) in ("Cc", "Cf"):
            continue
        else:
            out.append(ch)
    return "".join(out)


def render_fields(
    fields: Mapping[str, str], limits: DataLimits, redactor: Redactor
) -> tuple[RenderedField, ...]:
    rendered = []
    for name in sorted(fields):
        value = fields[name]
        if not _FIELD_NAME.match(name) or not isinstance(value, str):
            raise HarnessRejection(Reason.FIELD_INVALID)
        text = redactor.mask_account_numbers(clean(value))
        cap = limits.cap_for(name)
        truncated = len(text) > cap
        rendered.append(RenderedField(name, text[:cap], truncated, len(text)))
    return tuple(rendered)


def data_block(rendered: tuple[RenderedField, ...]) -> str:
    body = {f.name: {"text": f.text, "truncated": f.truncated} for f in rendered}
    encoded = json.dumps(body, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    encoded = encoded.replace("<", "\\u003c").replace(">", "\\u003e")
    return f"{DATA_OPEN}\n{encoded}\n{DATA_CLOSE}"
