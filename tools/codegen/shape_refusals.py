"""Which invalid fixtures the generated types must refuse (CON-03, REQ-TST-003).

The generated Go and Pydantic types model shape; the JSON Schema validator models everything else. An invalid fixture belongs to the
shape when the validator rejects it for `required`, `additionalProperties`, `enum`, `const` or `type` at a place with no
`if`/`then`/`else`/`allOf`/`oneOf`/`anyOf`/`not` above it in the schema path. Those fixtures must be refused by the generated types
in both languages. The list is derived here from the validator, so no one keeps it by hand, and `--check` (run by
tools/contracts/test_shape_refusals.py) fails when it is stale.

Needs jsonschema, so it runs in the contracts environment:  .venv-contracts/bin/python tools/codegen/shape_refusals.py --write|--check
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
sys.path.insert(0, str(ROOT / "tools" / "contracts"))

from validate import FIXTURES, load_schemas, load_strict, make_registry, target  # noqa: E402

OUT = HERE / "shape_refusals.json"
EXCLUDED = json.loads((HERE / "excluded.json").read_text(encoding="utf-8"))
SHAPE = {"required", "additionalProperties", "enum", "const", "type"}
CONDITIONAL = {"allOf", "anyOf", "oneOf", "not", "if", "then", "else"}


def classify() -> dict[str, str]:
    schemas = load_schemas()
    registry = make_registry(schemas)
    out: dict[str, str] = {}
    for folder in sorted(p for p in (FIXTURES / "invalid").iterdir() if p.is_dir()):
        key = folder.name if ".v" in folder.name else folder.name + ".v1"
        base = key.rsplit(".v", 1)[0]
        if base in EXCLUDED["names"] or any(base.startswith(pre) for pre in EXCLUDED["prefixes"]):
            continue
        exp_path = folder / "expectations.json"
        expectations = load_strict(exp_path) if exp_path.exists() else {}
        for path in sorted(folder.glob("*.json")):
            if path.name == "expectations.json" or path.name.endswith(".clarification.json"):
                continue
            kw = expectations.get(path.name)
            if kw not in SHAPE:
                continue
            try:
                errors = list(target(folder.name, path, schemas, registry).iter_errors(load_strict(path)))
            except ValueError:
                continue  # rejected by the strict decoder before any schema sees it
            hits = [e for e in errors if e.validator in SHAPE and not (CONDITIONAL & {str(s) for s in e.absolute_schema_path})]
            if hits:
                out[f"{folder.name}/{path.name}"] = hits[0].validator
    return out


def main(argv: list[str]) -> int:
    got = classify()
    text = json.dumps(got, indent=1, sort_keys=True) + "\n"
    if argv == ["--write"]:
        OUT.write_text(text, encoding="utf-8")
        print(f"wrote {len(got)} fixtures to {OUT.relative_to(ROOT)}")
        return 0
    if argv == ["--check"]:
        if not OUT.exists() or OUT.read_text(encoding="utf-8") != text:
            print(f"shape-refusals FAIL: {OUT.relative_to(ROOT)} is stale; run: .venv-contracts/bin/python tools/codegen/shape_refusals.py --write", file=sys.stderr)
            return 1
        print(f"shape-refusals OK: {len(got)} fixtures")
        return 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
