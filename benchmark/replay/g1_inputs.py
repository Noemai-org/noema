"""`benchmark/replay/g1_inputs.json`: the inputs of five G1 cases, for C2's next compile run (recorded compile outputs for the replay provider).

`python -m benchmark.replay.g1_inputs --write` regenerates the file; `--check` fails when the committed file is not equal to a fresh export. It holds, for BEN-CLEAN-0001, -0014, -0027 and
BEN-TMO-0001, -0018 of the generated suite at seed 20261020, only what a model-side run needs: case id, channel, locale mix (a tag only) and its canonical locale (en-NG for all five: A460), the synthetic session's principal (id and roles), the candidate world rows the principal owns (A439: beneficiaries by
id, name, aliases and last four digits; transactions by reference, direction, amount, currency, age and beneficiary) and the utterances. No expectation, no oracle reference, no fault plan, no category,
no goal and none of the world's state (ledger, switch, destination, settlement) (INV-22; the generated case file carries an `expected` block, which is never copied). Digest-pinned: the generator and its version, the seed, the digest of the suite manifest (the pinned
value of `test_generate.py`), a digest of the whole suite tree (the rule of `benchmark.generator.reproduce`), the digest of each case file (from the suite manifest) and a digest of the exported inputs. SYNTHETIC (AGENT-25)."""
import argparse
import hashlib
import json
import sys
from pathlib import Path
from typing import Any

from benchmark.contracts import sha256_of
from benchmark.generator.generate import DEFAULT_SEED, Suite, generate
from benchmark.generator.reproduce import tree_digest

Json = dict[str, Any]
SEED = DEFAULT_SEED
CASES = ("BEN-CLEAN-0001", "BEN-CLEAN-0014", "BEN-CLEAN-0027", "BEN-TMO-0001", "BEN-TMO-0018")
PATH = Path(__file__).resolve().parent / "g1_inputs.json"
LABEL = "SYNTHETIC generated cases, inputs only: no expectation and no oracle reference (INV-22); for recording compile outputs for the replay provider"
BENEFICIARY_KEYS = ("beneficiary_id", "name", "aliases", "account_last4")
TRANSACTION_KEYS = ("ref", "direction", "amount_minor", "currency", "age_minutes", "beneficiary")
CANONICAL_LOCALE = "en-NG"
LOCALE_RULE = "canonical_locale is en-NG for every case (A460: what the UI sends); locale_mix stays a tag only and is not sent, and no locale hint is sent"


def canonical(doc: Json) -> bytes:
    return json.dumps(doc, indent=2, sort_keys=True, ensure_ascii=False).encode("utf-8")


def build(suite: Suite) -> Json:
    entries = {e["case_id"]: e for e in suite.manifest["cases"]}
    files = {name: hashlib.sha256(raw).hexdigest() for name, raw in suite.files.items()}
    cases = []
    for case_id in CASES:
        src = suite.cases[case_id]
        fx = suite.fixtures[src["known_facts_fixture"]]
        pid = src["principal"]["principal_id"]
        mix = list(src["locale_mix"])
        cases.append({"case_id": case_id, "channel": src["channel"], "locale_mix": mix, "canonical_locale": CANONICAL_LOCALE, "case_sha256": entries[case_id]["sha256"],
                      "principal": {"principal_id": pid, "roles": list(src["principal"]["roles"])},
                      "candidates": {"beneficiaries": [{k: b[k] for k in BENEFICIARY_KEYS} for b in fx["beneficiaries"] if b["owner"] == pid],
                                     "transactions": [{k: x[k] for k in TRANSACTION_KEYS} for x in fx["transactions"] if x["owner"] == pid]},
                      "utterances": [{"turn": u["turn"], "text": u["text"]} for u in src["utterances"]]})
    generator = suite.manifest["generator"]
    doc: Json = {"label": LABEL, "canonical_locale_rule": LOCALE_RULE, "suite": {"generator": generator["name"], "generator_version": generator["version"], "seed": suite.seed,
                                           "manifest_sha256": sha256_of(suite.manifest),
                                           "tree_sha256": "sha256:" + tree_digest(files)}, "cases": cases}
    doc["inputs_sha256"] = "sha256:" + hashlib.sha256(canonical(doc)).hexdigest()
    return doc


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m benchmark.replay.g1_inputs", description=__doc__)
    mode = p.add_mutually_exclusive_group(required=True)
    mode.add_argument("--write", action="store_true")
    mode.add_argument("--check", action="store_true")
    args = p.parse_args(argv)
    fresh = canonical(build(generate(SEED))) + b"\n"
    if args.write:
        PATH.write_bytes(fresh)
        sys.stdout.write(f"wrote {PATH.name}\n")
        return 0
    if not PATH.is_file() or PATH.read_bytes() != fresh:
        sys.stdout.write(f"{PATH.name} is not equal to a fresh export (run --write and commit it)\n")
        return 1
    sys.stdout.write(f"{PATH.name} equals a fresh export\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
