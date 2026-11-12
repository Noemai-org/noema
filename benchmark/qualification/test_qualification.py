"""UND-13, REQ-INT-006 (model-drift canary; DEC-2026-10-31 D25; ruling A17 of 6 Nov 2026): a model or speech version is used only with a qualification record. The record is made
from a scorecard that includes pass^k and from the drift run record. Interim QUALIFIED = the run is complete, Tier 0 is zero, the drift check is PASS and pass^k is not below the
baseline; a first qualification (no baseline) waits for a Founder-set floor. Written before the change to `record.py` and seen failing. SYNTHETIC scorecards from the real
scorer. Generated layer only (INV-22, REQ-AGT-004).

The benchmark owns the runner side: the record format (`contracts/schemas/qualification-record.v1.json`), how a record is made, and the lookup that refuses an unqualified ID.
The model service's configuration check (C2) calls the same lookup contract; it is not built here."""
import copy
import hashlib
import functools
import io
import json
import tempfile
import unittest
from collections.abc import Callable
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts, sha256_of
from benchmark.qualification.__main__ import main
from benchmark.qualification.pins import is_pinned
from benchmark.qualification.record import build_record, check_model, validate_record, verify_against_scorecard
from benchmark.qualification.run import build_baseline
from benchmark.qualification.test_run import NEW, PINNED, SHA, answers, baseline, run, subset
from benchmark.scoring.test_report import card_for, fixture

Json = dict[str, Any]
PROVIDER, MODEL, VERSION, DEC = NEW["provider"], NEW["model_id"], NEW["version"], "DEC-2026-11-06-qualify-decision-model-a"
CONTRACTS = Contracts()


def repeat_cards(primary: Json | None = None) -> list[Json]:
    """The scorecards of the four other pass^k runs: each its own run id, complete, Tier 0 at zero (F6: Tier 0 holds in every run)."""
    out = []
    for i in range(2, 6):
        card = copy.deepcopy(primary or scorecard())
        card["run_id"] = f"run_repeat_{i}"
        out.append(card)
    return out


CONTEXT = "sha256:" + hashlib.sha256(b"cases, oracles and pack").hexdigest()


def results_sha(run_id: str) -> str:
    return "sha256:" + hashlib.sha256(run_id.encode()).hexdigest()


def provenance_for(card: Json, repeats: list[Json]) -> tuple[Json, list[Json]]:
    """The provenance files scoring writes for the primary run and the four repeats: the primary lists all five runs, each file names its own scorecard's digest."""
    runs = [{"run_id": c["run_id"], "results_sha256": results_sha(c["run_id"])} for c in (card, *repeats)]
    def one(c: Json, listed: list[Json] | None) -> Json:
        return {"schema": "benchmark-run-provenance.v1", "run_id": c["run_id"], "results_sha256": results_sha(c["run_id"]), "scorecard_sha256": sha256_of(c), "context_sha256": CONTEXT, "pass_k_runs": listed}
    return one(card, runs), [one(c, None) for c in repeats]


def first_card() -> Json:
    """A scorecard whose pass^k is measured on 40 cases (the first-qualification minimum of ruling F6)."""
    card = scorecard()
    card["tier1"]["diagnostics"]["pass_k"]["rate"] = {"k": 36, "n": 40, "rate": 0.9}
    card["n_cases"] = 40  # the denominator is a subset of the scored cases (Codex P2 on noema#199)
    return card


def scorecard() -> Json:
    cases, oracles, results, _ = fixture()
    card = copy.deepcopy(card_for(cases, oracles, results))
    card["complete"] = True
    card["tier0"]["all_zero"] = True
    card["tier1"]["diagnostics"]["pass_k"] = {"k_required": 5, "runs_supplied": 5, "available": True, "reason": None,
                                              "rate": {"k": 9, "n": 10, "rate": 0.9}, "failing_cases": ["BEN-1-0001"]}
    card["n_cases"] = 10  # the pass^k denominator is a subset of the scored cases (Codex P2 on noema#199)
    return card


def record(card: Json | None = None, drift: Json | None = None, **changes: Any) -> Json:
    args: Json = {"kind": "llm", "provider": PROVIDER, "model_id": MODEL, "version": VERSION, "decision_record": DEC, "qualified_on": "2026-11-06", "baseline_pass_k": 0.9,
                  "floor": None, "floor_source": None}
    args.update(changes)
    if args["baseline_pass_k"] is not None:
        args.setdefault("baseline_record_sha256", baseline_sha())
    card = card or scorecard()
    args.setdefault("repeat_cards", repeat_cards(card))
    if "provenance" not in args and len(args["repeat_cards"]) == 4:
        args["provenance"], args["repeat_provenance"] = provenance_for(card, args["repeat_cards"])
    return build_record(card, drift or run(), **args)


def first_drift() -> Json:
    """The drift run of the baseline version against itself: the run record of a first qualification."""
    sub = subset()
    return run(answers(sub, PINNED["version"]), PINNED, sub, baseline(sub))


def first(**changes: Any) -> Json:
    """A first qualification of the baseline version itself, which has no baseline and so needs the floor (A43)."""
    args: Json = {"card": first_card(), "drift": first_drift(), "version": PINNED["version"], "baseline_pass_k": None, "floor": 0.9, "floor_source": "DEC-2026-11-08-floor"}
    args.update(changes)
    return record(**args)


def first_record() -> Json:
    return first()


@functools.lru_cache(maxsize=1)
def _baseline_json() -> str:
    return json.dumps(first_record())


def baseline_record() -> Json:
    out: Json = json.loads(_baseline_json())
    return out


def baseline_sha() -> str:
    return sha256_of(baseline_record())


def lookup(records: list[Json], provider: str, model_id: str, version: str, kind: str | None = None) -> list[str]:
    """`check_model` over the records and the baseline record they name, as a records directory holds both."""
    return check_model([*records, baseline_record()], provider, model_id, version, kind)


class Making(unittest.TestCase):
    def test_a_complete_tier_zero_clean_drift_pass_run_with_pass_k_above_the_baseline_is_qualified_and_valid(self) -> None:
        rec = record()
        self.assertEqual("QUALIFIED", rec["verdict"])
        self.assertEqual([], rec["reasons"])
        self.assertEqual([], validate_record(rec))
        self.assertEqual({"k": 9, "n": 10, "rate": 0.9}, rec["pass_k"]["rate"])
        self.assertEqual("PASS", rec["drift"]["result"])

    def test_the_record_carries_the_numbers_it_was_made_from_and_the_digests(self) -> None:
        card, drift = scorecard(), run()
        rec = record(card, drift)
        for field in ("schema", "kind", "provider", "model_id", "version", "run_id", "n_cases", "complete", "tier0_all_zero", "pass_k", "scorecard_sha256", "drift", "bar", "decision_record",
                      "qualified_on", "verdict", "reasons"):
            self.assertIn(field, rec)
        self.assertEqual(card["run_id"], rec["run_id"])
        self.assertEqual({"baseline_pass_k": 0.9, "floor": None, "floor_source": None, "baseline_record_sha256": baseline_sha()}, rec["bar"])
        self.assertEqual(drift["subset_id"], rec["drift"]["subset_id"])
        self.assertEqual(drift["baseline"]["pinned"]["version"], rec["drift"]["baseline_version"])
        self.assertEqual([], verify_against_scorecard(rec, card))

    def test_an_incomplete_run_is_not_qualified(self) -> None:
        card = scorecard()
        card["complete"] = False
        rec = record(card)
        self.assertEqual("NOT_QUALIFIED", rec["verdict"])
        self.assertTrue(any("complete" in r for r in rec["reasons"]), rec["reasons"])

    def test_a_tier_zero_breach_is_not_qualified(self) -> None:
        card = scorecard()
        card["tier0"]["all_zero"] = False
        rec = record(card)
        self.assertEqual("NOT_QUALIFIED", rec["verdict"])
        self.assertTrue(any("Tier 0" in r for r in rec["reasons"]), rec["reasons"])

    def test_a_run_without_pass_k_is_not_qualified(self) -> None:
        card = scorecard()
        card["tier1"]["diagnostics"]["pass_k"] = {"k_required": 5, "runs_supplied": 1, "available": False, "reason": "1 run supplied; pass^k needs exactly 5", "rate": None, "failing_cases": []}
        rec = record(card)
        self.assertEqual("NOT_QUALIFIED", rec["verdict"])
        self.assertTrue(any("pass^k" in r for r in rec["reasons"]), rec["reasons"])
        self.assertEqual([], validate_record(rec))

    def test_drift_or_an_unavailable_provider_is_not_qualified(self) -> None:
        sub = subset()
        drifted = answers(sub, NEW["version"])
        drifted[sub["case_ids"][0]] = {"slots": {"intent": "x"}, "answered_by": NEW["version"]}
        down = answers(sub, NEW["version"])
        down[sub["case_ids"][0]] = None
        for ans, result in ((drifted, "DRIFT"), (down, "PROVIDER_UNAVAILABLE")):
            rec = record(drift=run(ans))
            self.assertEqual("NOT_QUALIFIED", rec["verdict"], result)
            self.assertTrue(any("drift" in r and result in r for r in rec["reasons"]), rec["reasons"])
            self.assertEqual([], validate_record(rec))

    def test_pass_k_equal_to_the_baseline_qualifies_and_below_it_does_not(self) -> None:
        self.assertEqual("QUALIFIED", record(baseline_pass_k=0.9)["verdict"])
        rec = record(baseline_pass_k=0.95)
        self.assertEqual("NOT_QUALIFIED", rec["verdict"])
        self.assertTrue(any("below" in r and "pass^k" in r for r in rec["reasons"]), rec["reasons"])

    def test_a_first_qualification_without_a_baseline_or_a_floor_waits_for_the_founders_floor(self) -> None:
        rec = record(baseline_pass_k=None)
        self.assertEqual("NOT_QUALIFIED", rec["verdict"])
        self.assertTrue(any("floor" in r for r in rec["reasons"]), rec["reasons"])
        self.assertEqual([], validate_record(rec))

    def test_the_floor_is_the_first_qualification_bar_and_afterwards_the_baseline_governs(self) -> None:
        # Founders' correction of 8 Nov 2026 (A17's wording): "not below the pinned baseline's own pass^k"; the "higher of floor and baseline" wording is dropped.
        self.assertEqual("QUALIFIED", first(floor=0.9)["verdict"])
        self.assertEqual("NOT_QUALIFIED", first(floor=0.99)["verdict"])
        self.assertEqual("QUALIFIED", record(baseline_pass_k=0.8, floor=0.95, floor_source="DEC-2026-11-05-floor")["verdict"])
        self.assertEqual("NOT_QUALIFIED", record(baseline_pass_k=0.95, floor=0.5, floor_source="DEC-2026-11-05-floor")["verdict"])

    def test_a_first_qualification_floor_below_0_90_does_not_qualify_a_model(self) -> None:
        # Ruling F6 (10 Nov 2026): the first-qualification floor is pass^5 of at least 0.90 on at least 40 cases; it can be raised and never lowered. `--floor 0.10` qualified a model.
        for floor in (0.1, 0.5, 0.85, 0.8999):
            rec = first(floor=floor)
            self.assertEqual("NOT_QUALIFIED", rec["verdict"], floor)
            self.assertTrue(any("0.9" in r and "floor" in r for r in rec["reasons"]), rec["reasons"])
            self.assertEqual([], validate_record(rec))

    def test_a_first_qualification_on_fewer_than_40_cases_does_not_qualify(self) -> None:
        card = first_card()
        card["tier1"]["diagnostics"]["pass_k"]["rate"] = {"k": 39, "n": 39, "rate": 1.0}
        rec = first(card=card)
        self.assertEqual("NOT_QUALIFIED", rec["verdict"])
        self.assertTrue(any("40" in r and "cases" in r for r in rec["reasons"]), rec["reasons"])
        self.assertEqual("QUALIFIED", record(baseline_pass_k=0.8)["verdict"], "a requalification is held to its baseline, not to the first-qualification minimum")

    def test_a_record_edited_to_qualify_below_the_first_qualification_floor_is_refused(self) -> None:
        rec = first()
        rec["bar"]["floor"] = 0.1
        self.assertTrue(any("floor" in p and "0.9" in p for p in validate_record(rec)), validate_record(rec))
        rec = first()
        rec["pass_k"]["rate"]["n"] = 10
        self.assertTrue(any("40" in p for p in validate_record(rec)), validate_record(rec))

    def test_tier_zero_and_completeness_hold_in_every_one_of_the_five_runs(self) -> None:
        # Codex P1 on 41f879b: only the primary run's Tier 0 was checked.
        changes: list[tuple[str, Callable[[Json], object]]] = [("tier0", lambda c: c["tier0"].update(all_zero=False)), ("complete", lambda c: c.update(complete=False))]
        for field, change in changes:
            cards = repeat_cards()
            change(cards[2])
            rec = record(repeat_cards=cards)
            self.assertEqual("NOT_QUALIFIED", rec["verdict"], field)
            self.assertFalse(rec["tier0_all_zero"] if field == "tier0" else rec["complete"], field)
            self.assertTrue(any("run_repeat_4" in r for r in rec["reasons"]), rec["reasons"])
            self.assertEqual([], validate_record(rec))

    def test_five_run_pass_k_needs_the_four_other_scorecards_with_distinct_run_ids(self) -> None:
        card = scorecard()
        for bad in ([], repeat_cards()[:3], repeat_cards() + repeat_cards()[:1]):
            with self.assertRaises(ValueError):
                record(card=card, repeat_cards=bad)
        same = repeat_cards()
        same[0]["run_id"] = card["run_id"]
        with self.assertRaises(ValueError):
            record(card=card, repeat_cards=same)
        twice = repeat_cards()
        twice[1]["run_id"] = twice[0]["run_id"]
        with self.assertRaises(ValueError):
            record(card=card, repeat_cards=twice)
        flag = repeat_cards()
        flag[0]["tier0"]["all_zero"] = "true"
        with self.assertRaises(ValueError):
            record(card=card, repeat_cards=flag)

    def test_a_floor_needs_the_decision_record_that_set_it(self) -> None:
        with self.assertRaises(ValueError):
            record(baseline_pass_k=None, floor=0.85, floor_source=None)
        with self.assertRaises(ValueError):
            record(floor=1.5, floor_source="DEC-2026-11-05-floor")
        with self.assertRaises(ValueError):
            record(baseline_pass_k=-0.1)

    def test_the_run_record_must_be_valid_and_be_for_this_provider_model_and_version(self) -> None:
        bad = run()
        bad["result"] = "DRIFT"
        with self.assertRaises(ValueError):
            record(drift=bad)
        changes: list[Json] = [{"version": "2026-12-01"}, {"model_id": "decision-model-b"}, {"provider": "anthropic"}, {"kind": "speech"}]
        for change in changes:
            with self.assertRaises(ValueError, msg=str(change)):
                record(**change)

    def test_an_unpinned_version_cannot_be_recorded(self) -> None:
        for version in ("", "latest", "LATEST", "default", "*", "gpt-latest", " 2026 "):
            with self.assertRaises(ValueError, msg=repr(version)):
                record(version=version)

    def test_a_record_needs_its_decision_record_and_a_real_date(self) -> None:
        with self.assertRaises(ValueError):
            record(decision_record="")
        with self.assertRaises(ValueError):
            record(qualified_on="yesterday")
        with self.assertRaises(ValueError):
            record(kind="vision")


class Contract(unittest.TestCase):
    """The shipped schemas in contracts/ and the code say the same thing."""

    def test_qualified_and_not_qualified_records_validate_against_the_contract(self) -> None:
        card = scorecard()
        card["tier0"]["all_zero"] = False
        for rec in (record(), record(card), record(baseline_pass_k=None), first_record()):
            self.assertEqual([], CONTRACTS.errors("qualification-record", rec), rec["reasons"])

    def test_run_records_validate_against_the_contract(self) -> None:
        sub = subset()
        down = answers(sub, NEW["version"])
        down[sub["case_ids"][0]] = None
        drifted = answers(sub, NEW["version"])
        drifted[sub["case_ids"][1]] = {"slots": {"intent": "x"}, "answered_by": "2026-11-09"}
        for rec in (run(), run(down), run(drifted)):
            self.assertEqual([], CONTRACTS.errors("qualification-run", rec))

    def test_the_contract_refuses_what_the_code_refuses(self) -> None:
        rec = record()
        mutations: list[Callable[[Json], object]] = [lambda r: r.pop("decision_record"), lambda r: r.update(verdict="MAYBE"), lambda r: r.update(qualified_on="yesterday"), lambda r: r["drift"].update(result="OK"),
                       lambda r: r.update(extra=1), lambda r: r.update(kind="vision")]
        for mutate in mutations:
            broken = copy.deepcopy(rec)
            mutate(broken)
            self.assertTrue(CONTRACTS.errors("qualification-record", broken), broken)
        bad_run = run()
        bad_run["layer"] = "HELD_OUT"
        self.assertTrue(CONTRACTS.errors("qualification-run", bad_run))

    def test_the_contracts_require_the_fields_the_validators_do(self) -> None:
        schemas = Path(__file__).resolve().parents[2] / "contracts" / "schemas"
        record_schema = json.loads((schemas / "qualification-record.v1.json").read_text(encoding="utf-8"))
        self.assertEqual(sorted(record()), sorted(record_schema["required"]))


class Validating(unittest.TestCase):
    def test_a_qualified_verdict_the_fields_do_not_support_is_invalid(self) -> None:
        for field, value in (("complete", False), ("tier0_all_zero", False)):
            rec = record()
            rec[field] = value
            self.assertTrue(any("QUALIFIED" in p for p in validate_record(rec)), (field, validate_record(rec)))
        rec = record()
        rec["pass_k"] = {**rec["pass_k"], "available": False}
        self.assertTrue(any("pass^k" in p for p in validate_record(rec)))
        rec = record()
        rec["drift"] = {**rec["drift"], "result": "DRIFT"}
        self.assertTrue(any("drift" in p for p in validate_record(rec)), validate_record(rec))
        rec = record()
        rec["bar"] = {**rec["bar"], "baseline_pass_k": 0.99}
        self.assertTrue(any("below" in p for p in validate_record(rec)), validate_record(rec))
        rec = record()
        rec["bar"] = {"baseline_pass_k": None, "floor": None, "floor_source": None}
        self.assertTrue(any("floor" in p for p in validate_record(rec)), validate_record(rec))

    def test_a_missing_field_is_named(self) -> None:
        rec = record()
        del rec["decision_record"]
        self.assertTrue(any("decision_record" in p for p in validate_record(rec)))

    def test_a_record_edited_after_the_scorecard_no_longer_verifies(self) -> None:
        card = scorecard()
        rec = record(card)
        card["n_cases"] += 1
        self.assertTrue(any("digest" in p for p in verify_against_scorecard(rec, card)))


class Malformed(unittest.TestCase):
    """C2 on noema#199 (issue #367): the same NaN fail-open as the service gate. Every comparison with NaN is false, so a NaN rate added no reason and a QUALIFIED verdict could stand."""

    BAD_NUMBERS = (float("nan"), float("inf"), float("-inf"), True, False, "0.9", None, -0.1, 1.1)

    def test_a_rate_that_is_not_a_finite_number_between_zero_and_one_never_stands_as_qualified(self) -> None:
        for bad in self.BAD_NUMBERS:
            rec = record()
            rec["pass_k"] = {**rec["pass_k"], "rate": {**rec["pass_k"]["rate"], "rate": bad}}
            got = validate_record(rec)
            self.assertTrue(any("rate" in p for p in got), (bad, got))
            self.assertTrue(lookup([rec], PROVIDER, MODEL, VERSION), bad)

    def test_a_baseline_or_floor_that_is_not_a_finite_number_between_zero_and_one_is_refused(self) -> None:
        for field in ("baseline_pass_k", "floor"):
            for bad in (float("nan"), float("inf"), True, "0.9", -0.1, 1.1):
                rec = record()
                rec["bar"] = {**rec["bar"], field: bad}
                got = validate_record(rec)
                self.assertTrue(any(field in p or "bar" in p for p in got), (field, bad, got))
                self.assertTrue(lookup([rec], PROVIDER, MODEL, VERSION), (field, bad))

    def test_building_a_record_refuses_a_number_that_is_not_finite_or_is_a_bool(self) -> None:
        for bad in (float("nan"), float("inf"), True):
            with self.assertRaises(ValueError):
                record(baseline_pass_k=bad)
            with self.assertRaises(ValueError):
                record(baseline_pass_k=None, floor=bad, floor_source="DEC-2026-11-05-floor")

    def test_a_digest_or_date_with_a_trailing_newline_is_not_a_digest_or_a_date(self) -> None:
        # A `$` in a pattern accepts a trailing newline under Python's re; a whole-string match does not.
        digests = [k for k, v in record().items() if isinstance(v, str) and v.startswith("sha256:")]
        self.assertTrue(digests)
        for key in digests:
            rec = record()
            rec[key] = rec[key] + "\n"
            self.assertTrue(any(key in p for p in validate_record(rec)), key)
        rec = record()
        rec["qualified_on"] = "2026-11-06\n"
        self.assertTrue(any("qualified_on" in p for p in validate_record(rec)))

    def test_a_reason_that_is_not_a_string_is_refused_not_raised(self) -> None:
        for reasons in ([1], [None], "not a list", [["x"]]):
            rec = record(baseline_pass_k=0.95)
            self.assertEqual("NOT_QUALIFIED", rec["verdict"])
            rec["reasons"] = reasons
            got = validate_record(rec)
            self.assertTrue(any("reasons" in p for p in got), (reasons, got))
            self.assertTrue(lookup([rec], PROVIDER, MODEL, VERSION), reasons)


class Strictness(unittest.TestCase):
    """Defects Codex found in the model-service port (noema#215) that were true of this reference code too (reported by C2 on noema#199)."""

    def test_a_flag_that_is_not_a_real_boolean_never_counts_as_true(self) -> None:
        for field in ("complete", "tier0_all_zero"):
            for bad in ("false", "true", 1, 0, None):
                rec = record()
                rec[field] = bad
                got = validate_record(rec)
                self.assertTrue(any(field in p and "boolean" in p for p in got), (field, bad, got))
        rec = record()
        rec["pass_k"] = {**rec["pass_k"], "available": "false"}
        self.assertTrue(any("available" in p and "boolean" in p for p in validate_record(rec)))

    def test_a_scorecard_flag_that_is_not_a_boolean_cannot_make_a_record(self) -> None:
        for field in ("complete",):
            card = scorecard()
            card[field] = "false"
            with self.assertRaises(ValueError):
                record(card)
        card = scorecard()
        card["tier0"]["all_zero"] = 1
        with self.assertRaises(ValueError):
            record(card)

    def test_the_pinned_rule_is_the_model_services_rule(self) -> None:
        # services/model/noema_model/harness/config.py check_pinned: the character set, a digit, and none of the floating words as a substring.
        for refused in ("stable-2", "model-default-1", "gpt-preview-3", "gpt-5-newest", "gpt-5-auto-1", "current-3", "LATEST-1", "gpt-five", "latest", "", "*", " 2026 ", "a b-1", "-2026-10-15"):
            self.assertFalse(is_pinned(refused), refused)
        for accepted in ("2026-10-15", "gpt-5-2026-08-07", "claude-5-20261001", "v2.1"):
            self.assertTrue(is_pinned(accepted), accepted)
            self.assertEqual([], validate_record(record(version=accepted, drift=run(pinned={**NEW, "version": accepted}, ans=answers(subset(), accepted)))))
        self.assertTrue(lookup([], PROVIDER, MODEL, "stable-2"))

    def test_a_date_must_be_written_yyyy_mm_dd_and_nothing_else(self) -> None:
        for bad in ("2026W455", "20261106", "2026-1-6", "2026-11-6", "2026-11-06T00:00:00", " 2026-11-06", "2026-13-01", "2026-02-30"):
            with self.assertRaises(ValueError, msg=bad):
                record(qualified_on=bad)
            rec = record()
            rec["qualified_on"] = bad
            self.assertTrue(any("qualified_on" in p for p in validate_record(rec)), bad)

    def test_an_invalid_record_for_this_identity_refuses_even_when_a_valid_one_exists(self) -> None:
        # Fail closed: a NOT_QUALIFIED record with a bad shape must still revoke, not be ignored.
        good = record(qualified_on="2026-11-05")
        broken = record(qualified_on="2026-11-06")
        broken["verdict"] = "NOT_QUALIFIED"
        broken["complete"] = "false"
        got = lookup([good, broken], PROVIDER, MODEL, VERSION)
        self.assertTrue(any("invalid" in p for p in got), got)
        self.assertEqual([], lookup([good], PROVIDER, MODEL, VERSION))

    def test_an_invalid_record_for_another_identity_does_not_refuse_this_one(self) -> None:
        sub = subset()
        base_b = build_baseline(sub, {**PINNED, "model_id": "decision-model-b"}, answers(sub, PINNED["version"]), commit_sha=SHA)
        other = record(model_id="decision-model-b", drift=run(pinned={**NEW, "model_id": "decision-model-b"}, base=base_b))
        other["complete"] = "false"
        self.assertEqual([], lookup([record(), other], PROVIDER, MODEL, VERSION))


class ContractAndEvidence(unittest.TestCase):
    """Codex on 924c58d: the lookup trusted flags and counts the contract and the evidence do not support."""

    def test_pass_k_is_available_only_with_exactly_five_runs(self) -> None:
        for k_required, runs in ((1, 1), (5, 4), (4, 5), (5, 6), (0, 0)):
            rec = record()
            rec["pass_k"] = {**rec["pass_k"], "k_required": k_required, "runs_supplied": runs}
            got = validate_record(rec)
            self.assertTrue(any("five" in p and "pass^k" in p for p in got), (k_required, runs, got))
            self.assertTrue(lookup([rec], PROVIDER, MODEL, VERSION), (k_required, runs))

    def test_a_scorecard_that_claims_pass_k_without_five_runs_is_not_qualified(self) -> None:
        card = scorecard()
        card["tier1"]["diagnostics"]["pass_k"] = {**card["tier1"]["diagnostics"]["pass_k"], "k_required": 1, "runs_supplied": 1}
        rec = record(card)
        self.assertEqual("NOT_QUALIFIED", rec["verdict"])
        self.assertTrue(any("five" in r for r in rec["reasons"]), rec["reasons"])

    def test_a_record_the_contract_refuses_never_allows_a_lookup(self) -> None:
        mutations: list[Callable[[Json], object]] = [
            lambda r: r["pass_k"]["rate"].update(rate=4),  # outside [0, 1]
            lambda r: r.update(extra=1),  # unknown member
            lambda r: r["drift"].pop("run_sha256"),  # nested required member
            lambda r: r["bar"].update(floor=1.5, floor_source="DEC-x"),
            lambda r: r.update(n_cases=0),
            lambda r: r["pass_k"].update(failing_cases=[""]),
        ]
        for mutate in mutations:
            rec = record()
            mutate(rec)
            self.assertTrue(CONTRACTS.errors("qualification-record", rec), "the mutation must break the contract")
            self.assertTrue(any("contract" in p for p in validate_record(rec)), validate_record(rec))
            got = lookup([rec], PROVIDER, MODEL, VERSION)
            self.assertTrue(got and any("invalid" in p for p in got), got)

    # Codex P2 on noema#199 (04a9a68): the rate is checked against its own counts, and the floor against its decision source, on every lookup.
    def test_a_pass_rate_that_disagrees_with_its_counts_is_invalid(self) -> None:
        for k, n, rate in ((0, 40, 0.9), (41, 40, 1.0), (-1, 40, 0.9), (36, 40, 0.95), (36, 0, 0.9), (36.0, 40, 0.9)):
            rec = first_record()
            rec["pass_k"]["rate"] = {**rec["pass_k"]["rate"], "k": k, "n": n, "rate": rate}
            got = validate_record(rec)
            self.assertTrue(any("pass_k.rate" in p for p in got), (k, n, rate, got))
            self.assertTrue(check_model([rec], PROVIDER, MODEL, rec["version"]), (k, n, rate))

    def test_a_consistent_pass_rate_is_still_accepted(self) -> None:
        self.assertEqual([], [p for p in validate_record(first_record()) if "pass_k.rate" in p])

    def test_a_floor_needs_its_decision_source_when_a_record_is_looked_up(self) -> None:
        for source in (None, "", "   "):
            rec = first_record()
            rec["bar"]["floor_source"] = source
            self.assertTrue(any("floor_source" in p for p in validate_record(rec)), (source, validate_record(rec)))
            self.assertTrue(check_model([rec], PROVIDER, MODEL, rec["version"]), source)
        rec = first_record()
        rec["bar"].update(floor=None, floor_source="DEC-x")
        self.assertTrue(any("floor_source" in p for p in validate_record(rec)), "a source with no floor is also unpaired")

    # Codex P2 on noema#199 (e846fe6): the pass^k denominator is a subset of the scored cases, so it cannot exceed n_cases.
    def test_a_pass_k_denominator_larger_than_the_scored_cases_is_invalid_and_cannot_be_built(self) -> None:
        rec = first_record()
        rec["n_cases"] = 6
        got = validate_record(rec)
        self.assertTrue(any("n_cases" in p and "pass_k" in p for p in got), got)
        self.assertTrue(check_model([rec], PROVIDER, MODEL, rec["version"]))
        card = first_card()
        card["n_cases"] = 6
        with self.assertRaisesRegex(ValueError, "n_cases"):
            first(card=card, repeat_cards=repeat_cards(card))

    def test_a_denominator_equal_to_the_scored_cases_is_accepted(self) -> None:
        self.assertEqual([], [p for p in validate_record(first_record()) if "n_cases" in p])
        self.assertEqual(40, first_record()["n_cases"])

    def test_the_shipped_first_qualification_fixture_obeys_the_bound(self) -> None:
        fixture_path = Path(__file__).resolve().parents[2] / "contracts" / "fixtures" / "valid" / "qualification-record" / "first_qualification_with_a_floor.json"
        shipped = json.loads(fixture_path.read_text(encoding="utf-8"))
        self.assertLessEqual(shipped["pass_k"]["rate"]["n"], shipped["n_cases"])


class RepeatBinding(unittest.TestCase):
    """Codex P1 on noema#199 (e846fe6): four unrelated clean scorecards must not stand in for the runs pass^k rests on."""

    def build(self, **changes: Any) -> Json:
        card = first_card()
        repeats = repeat_cards(card)
        prov, repeat_prov = provenance_for(card, repeats)
        args: Json = {"card": card, "drift": first_drift(), "version": PINNED["version"], "baseline_pass_k": None, "floor": 0.9, "floor_source": "DEC-2026-11-08-floor",
                      "repeat_cards": repeats, "provenance": prov, "repeat_provenance": repeat_prov}
        args.update(changes)
        return record(**args)

    def test_a_chain_that_matches_qualifies(self) -> None:
        self.assertEqual("QUALIFIED", self.build()["verdict"])

    def test_four_unrelated_clean_scorecards_are_refused(self) -> None:
        unrelated = []
        for i in range(2, 6):
            card = copy.deepcopy(first_card())
            card["run_id"] = f"other_{i}"
            unrelated.append(card)
        _, unrelated_prov = provenance_for(first_card(), unrelated)
        with self.assertRaisesRegex(ValueError, "repeat"):
            self.build(repeat_cards=unrelated, repeat_provenance=unrelated_prov)

    def test_a_repeat_scorecard_edited_after_scoring_is_refused(self) -> None:
        card = first_card()
        repeats = repeat_cards(card)
        prov, repeat_prov = provenance_for(card, repeats)
        repeats[0]["tier0"]["all_zero"] = False
        repeats[0]["tier0"]["all_zero"] = True
        repeats[0]["n_cases"] += 1
        with self.assertRaisesRegex(ValueError, "scorecard_sha256"):
            self.build(card=card, repeat_cards=repeats, provenance=prov, repeat_provenance=repeat_prov)

    def test_a_repeat_whose_results_digest_is_not_the_one_pass_k_listed_is_refused(self) -> None:
        card = first_card()
        repeats = repeat_cards(card)
        prov, repeat_prov = provenance_for(card, repeats)
        repeat_prov[1]["results_sha256"] = results_sha("a different result set")
        with self.assertRaisesRegex(ValueError, "results_sha256"):
            self.build(card=card, repeat_cards=repeats, provenance=prov, repeat_provenance=repeat_prov)

    def test_provenance_is_required_when_pass_k_is_available(self) -> None:
        with self.assertRaisesRegex(ValueError, "needs the provenance of the primary run"):
            self.build(provenance=None, repeat_provenance=[])

    def test_the_primary_provenance_must_be_for_the_primary_scorecard(self) -> None:
        card = first_card()
        repeats = repeat_cards(card)
        prov, repeat_prov = provenance_for(card, repeats)
        prov["scorecard_sha256"] = results_sha("another card")
        with self.assertRaisesRegex(ValueError, "scorecard_sha256"):
            self.build(card=card, repeat_cards=repeats, provenance=prov, repeat_provenance=repeat_prov)

    def test_a_repeat_scored_in_another_context_is_refused(self) -> None:
        """Codex P1 on noema#199 (3246f93): same run ids and result digests, but rescored against another pack registry, so its safety flags are not the primary's."""
        card = first_card()
        repeats = repeat_cards(card)
        prov, repeat_prov = provenance_for(card, repeats)
        repeat_prov[2]["context_sha256"] = results_sha("a pack that allows the prohibited capability")
        with self.assertRaisesRegex(ValueError, "context_sha256"):
            self.build(card=card, repeat_cards=repeats, provenance=prov, repeat_provenance=repeat_prov)

    def test_a_primary_listing_runs_in_a_context_it_does_not_share_is_refused(self) -> None:
        card = first_card()
        repeats = repeat_cards(card)
        prov, repeat_prov = provenance_for(card, repeats)
        for one in repeat_prov:
            one["context_sha256"] = results_sha("another context")
        with self.assertRaisesRegex(ValueError, "context_sha256"):
            self.build(card=card, repeat_cards=repeats, provenance=prov, repeat_provenance=repeat_prov)

    def test_the_listed_runs_must_be_exactly_five_distinct(self) -> None:
        card = first_card()
        repeats = repeat_cards(card)
        prov, repeat_prov = provenance_for(card, repeats)
        prov["pass_k_runs"] = prov["pass_k_runs"][:4]
        with self.assertRaisesRegex(ValueError, "five"):
            self.build(card=card, repeat_cards=repeats, provenance=prov, repeat_provenance=repeat_prov)


class Refusing(unittest.TestCase):
    def test_no_record_means_refused(self) -> None:
        got = lookup([], PROVIDER, MODEL, VERSION)
        self.assertTrue(any("no qualification record" in p for p in got), got)

    def test_a_qualified_record_for_exactly_this_provider_model_and_version_allows(self) -> None:
        self.assertEqual([], lookup([record()], PROVIDER, MODEL, VERSION))

    def test_a_change_of_version_model_or_provider_is_refused(self) -> None:
        rec = record()
        for provider, model, version in ((PROVIDER, MODEL, "2026-11-02"), (PROVIDER, "decision-model-b", VERSION), ("anthropic", MODEL, VERSION)):
            got = lookup([rec], provider, model, version)
            self.assertTrue(any("no qualification record" in p for p in got), (provider, model, version, got))

    def test_an_unpinned_requested_version_is_refused_even_when_a_record_exists(self) -> None:
        for version in ("", "latest", "default"):
            got = lookup([record()], PROVIDER, MODEL, version)
            self.assertTrue(any("pinned" in p for p in got), (version, got))

    def test_a_not_qualified_record_never_allows(self) -> None:
        card = scorecard()
        card["tier0"]["all_zero"] = False
        self.assertTrue(lookup([record(card)], PROVIDER, MODEL, VERSION))

    def test_an_invalid_record_never_allows(self) -> None:
        rec = record()
        rec["complete"] = False
        self.assertTrue(lookup([rec], PROVIDER, MODEL, VERSION))

    def test_a_later_failed_requalification_revokes_an_earlier_qualification(self) -> None:
        bad = scorecard()
        bad["tier0"]["all_zero"] = False
        later = record(bad, qualified_on="2026-11-07")
        self.assertTrue(lookup([record(), later], PROVIDER, MODEL, VERSION))
        self.assertTrue(lookup([later, record()], PROVIDER, MODEL, VERSION))
        earlier_fail = record(bad, qualified_on="2026-11-01")
        self.assertEqual([], lookup([earlier_fail, record()], PROVIDER, MODEL, VERSION))

    def test_two_records_of_one_day_that_disagree_are_refused(self) -> None:
        bad = scorecard()
        bad["tier0"]["all_zero"] = False
        got = lookup([record(), record(bad)], PROVIDER, MODEL, VERSION)
        self.assertTrue(any("disagree" in p for p in got), got)

    def test_the_kind_must_match_when_asked(self) -> None:
        self.assertTrue(lookup([record()], PROVIDER, MODEL, VERSION, kind="speech"))
        self.assertEqual([], lookup([record()], PROVIDER, MODEL, VERSION, kind="llm"))


class Lineage(unittest.TestCase):
    """SR N1 and N2, EL findings 1 and 2 and C1 on noema#199 at 004df05, and the citation of the committed record (A43 of DEC-2026-11-08-rulings-a41-onward)."""

    def test_a_bar_from_a_baseline_names_the_baseline_record_it_came_from(self) -> None:
        rec = record()
        self.assertEqual(baseline_sha(), rec["bar"]["baseline_record_sha256"])
        self.assertIsNone(first_record()["bar"]["baseline_record_sha256"])
        with self.assertRaises(ValueError):
            record(baseline_record_sha256=None)
        with self.assertRaises(ValueError):
            record(baseline_pass_k=None, floor=0.9, floor_source="DEC-2026-11-08-floor", card=first_card(), drift=first_drift(), version=PINNED["version"], baseline_record_sha256=baseline_sha())
        with self.assertRaises(ValueError):
            record(baseline_record_sha256="sha256:xyz")

    def test_a_hand_written_record_cannot_name_a_typed_baseline_bar(self) -> None:
        # SR N1: a record with baseline_pass_k 0.10 and a pass^5 of 0.20 on 3 cases validated.
        rec = record()
        rec["bar"]["baseline_pass_k"] = 0.1
        rec["pass_k"]["rate"] = {"k": 1, "n": 3, "rate": 0.2}
        rec["bar"]["baseline_record_sha256"] = "sha256:" + "a" * 64
        self.assertTrue(lookup([rec], PROVIDER, MODEL, VERSION), "the baseline record it names is not among the records")
        rec["bar"]["baseline_record_sha256"] = None
        self.assertTrue(any("baseline_record_sha256" in p for p in validate_record(rec)), validate_record(rec))

    def test_the_lookup_resolves_the_baseline_record_and_holds_it_to_the_same_rules(self) -> None:
        self.assertEqual([], lookup([record()], PROVIDER, MODEL, VERSION))
        self.assertTrue(check_model([record()], PROVIDER, MODEL, VERSION), "no baseline record among the records")
        weaker = record()
        weaker["bar"]["baseline_pass_k"] = 0.5  # a typed value that is not the baseline record's own pass^5
        self.assertTrue(any("baseline" in p for p in lookup([weaker], PROVIDER, MODEL, VERSION)))
        base_changes: list[Callable[[Json], object]] = [lambda b: b.update(verdict="NOT_QUALIFIED", reasons=["x"]), lambda b: b.update(kind="speech"), lambda b: b.update(provider="other"),
                                                        lambda b: b.update(model_id="other-model"), lambda b: b.update(version="2026-01-01")]
        for base_change in base_changes:
            base = baseline_record()
            base_change(base)
            rec = record(baseline_record_sha256=sha256_of(base))
            self.assertTrue(check_model([rec, base], PROVIDER, MODEL, VERSION), base)

    def test_a_record_lists_the_five_runs_it_rests_on(self) -> None:
        # SR N2: five runs on `reasons` text alone are not enough evidence.
        card = scorecard()
        cards = repeat_cards(card)
        rec = record(card, repeat_cards=cards)
        runs = rec["runs"]
        self.assertEqual(5, len(runs))
        self.assertEqual([card["run_id"], *(c["run_id"] for c in cards)], [r["run_id"] for r in runs])
        self.assertEqual([sha256_of(card), *(sha256_of(c) for c in cards)], [r["scorecard_sha256"] for r in runs])
        self.assertEqual(rec["scorecard_sha256"], runs[0]["scorecard_sha256"])
        self.assertEqual([], validate_record(rec))
        self.assertEqual([], CONTRACTS.errors("qualification-record", rec))

    def test_the_runs_are_checked_when_the_record_is_validated(self) -> None:
        run_changes: list[tuple[str, Callable[[Json], object]]] = [("count", lambda r: r["runs"].pop()), ("duplicate", lambda r: r["runs"][1].update(run_id=r["runs"][2]["run_id"])),
                             ("primary", lambda r: r["runs"][0].update(scorecard_sha256="sha256:" + "c" * 64)), ("digest", lambda r: r["runs"][2].update(scorecard_sha256="sha256:zz")),
                             ("tier0", lambda r: r["runs"][3].update(tier0_all_zero=False)), ("complete", lambda r: r["runs"][4].update(complete=False)), ("shape", lambda r: r["runs"].append("x"))]
        for name, change in run_changes:
            rec = record()
            change(rec)
            self.assertTrue(validate_record(rec) or CONTRACTS.errors("qualification-record", rec), name)
            self.assertTrue(lookup([rec], PROVIDER, MODEL, VERSION), name)

    def test_a_failing_repeat_run_is_in_the_runs_and_blocks_the_verdict(self) -> None:
        cards = repeat_cards()
        cards[1]["tier0"]["all_zero"] = False
        rec = record(repeat_cards=cards)
        self.assertEqual("NOT_QUALIFIED", rec["verdict"])
        self.assertFalse(rec["runs"][2]["tier0_all_zero"])
        self.assertEqual([], validate_record(rec))

    def test_a_malformed_kind_on_a_later_revoking_record_still_revokes(self) -> None:
        # EL finding 1: the kind filter ran before validation, so a revoking record with kind "LLM" was dropped and the model was allowed.
        later = record(qualified_on="2026-11-07")
        later["verdict"], later["reasons"], later["kind"] = "NOT_QUALIFIED", ["revoked"], "LLM"
        got = lookup([record(), later], PROVIDER, MODEL, VERSION, kind="llm")
        self.assertTrue(got, "a malformed record for this identity refuses the lookup whatever kind is asked for")
        self.assertEqual([], lookup([record()], PROVIDER, MODEL, VERSION, kind="llm"))

    def test_a_requalification_cannot_use_the_first_qualification_floor(self) -> None:
        # EL finding 2 (A17): the floor is for a FIRST qualification, whose drift run compares the version with itself.
        rec = record(card=first_card(), baseline_pass_k=None, floor=0.9, floor_source="DEC-2026-11-08-floor")  # default drift: the baseline is ANOTHER version
        self.assertNotEqual(rec["drift"]["baseline_version"], rec["version"])
        self.assertEqual("NOT_QUALIFIED", rec["verdict"])
        self.assertTrue(any("baseline" in r and "re-qualification" in r for r in rec["reasons"]), rec["reasons"])
        self.assertEqual([], validate_record(rec))
        edited = copy.deepcopy(rec)
        edited["verdict"], edited["reasons"] = "QUALIFIED", []
        self.assertTrue(any("re-qualification" in p for p in validate_record(edited)), validate_record(edited))
        self.assertEqual("QUALIFIED", first()["verdict"])

    def test_a_number_too_large_for_a_float_is_refused_not_raised(self) -> None:
        # EL 3: math.isfinite(10**400) raises OverflowError.
        with self.assertRaises(ValueError):
            record(baseline_pass_k=10 ** 400)
        rec = record()
        rec["pass_k"]["rate"]["rate"] = 10 ** 400
        self.assertTrue(validate_record(rec))
        self.assertTrue(lookup([rec], PROVIDER, MODEL, VERSION))

    def test_every_valid_fixture_is_valid_by_the_code_as_well_as_by_the_schema(self) -> None:
        # EL 4 and C1 2: a fixture called valid that validate_record refuses misleads.
        root = Path(__file__).resolve().parents[2] / "contracts" / "fixtures" / "valid" / "qualification-record"
        for path in sorted(root.glob("*.json")):
            doc = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual([], CONTRACTS.errors("qualification-record", doc), path.name)
            self.assertEqual([], validate_record(doc), path.name)

    def test_the_floor_reasons_cite_the_committed_record_not_a_session_label(self) -> None:
        rec = record(card=first_card(), drift=first_drift(), version=PINNED["version"], baseline_pass_k=None, floor=0.5, floor_source="DEC-2026-11-08-floor")
        text = " ".join(rec["reasons"])
        self.assertIn("A43", text)
        self.assertIn("DEC-2026-11-08-rulings-a41-onward", text)
        self.assertNotIn("F6", text)


class Cli(unittest.TestCase):
    def run_main(self, *argv: str) -> tuple[int, str]:
        out = io.StringIO()
        with redirect_stdout(out), redirect_stderr(out):
            code = main(list(argv))
        return code, out.getvalue()

    def files(self, root: Path, card: Json | None = None, repeats: list[Json] | None = None) -> list[str]:
        card = card or scorecard()
        repeats = repeats if repeats is not None else repeat_cards(card)
        (root / "scorecard.json").write_text(json.dumps(card), encoding="utf-8")
        prov, repeat_prov = provenance_for(card, repeats) if len(repeats) == 4 else (None, [])
        if prov is not None:
            (root / "provenance.json").write_text(json.dumps(prov), encoding="utf-8")
        for i, one in enumerate(repeats):
            (root / f"repeat-{i}").mkdir()
            (root / f"repeat-{i}" / "scorecard.json").write_text(json.dumps(one), encoding="utf-8")
            if repeat_prov:
                (root / f"repeat-{i}" / "provenance.json").write_text(json.dumps(repeat_prov[i]), encoding="utf-8")
        (root / "run.json").write_text(json.dumps(run()), encoding="utf-8")
        (root / "baseline-record.json").write_text(json.dumps(first_record()), encoding="utf-8")
        (root / "records").mkdir()
        return ["--scorecard", str(root / "scorecard.json"), "--run", str(root / "run.json"), "--baseline-record", str(root / "baseline-record.json"), "--provider", PROVIDER,
                "--model-id", MODEL, "--version", VERSION, "--kind", "llm", "--decision-record", DEC, "--qualified-on", "2026-11-06",
                *[a for i in range(len(repeats)) for a in ("--repeat-scorecard", str(root / f"repeat-{i}" / "scorecard.json"))]]

    def test_record_then_check_round_trip(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            code, text = self.run_main("record", *self.files(root, first_card()), "--out", str(root / "records" / "r.json"))
            self.assertEqual(0, code, text)
            (root / "records" / "baseline.json").write_text((root / "baseline-record.json").read_text(encoding="utf-8"), encoding="utf-8")  # a records directory holds the baseline too
            written = json.loads((root / "records" / "r.json").read_text(encoding="utf-8"))
            self.assertEqual(0.9, written["bar"]["baseline_pass_k"])
            self.assertEqual(sha256_of(baseline_record()), written["bar"]["baseline_record_sha256"])  # read from the baseline version's own record
            base = ["--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION, "--kind", "llm"]
            self.assertEqual(0, self.run_main("check", "--records", str(root / "records"), *base)[0])
            code, text = self.run_main("check", "--records", str(root / "records"), "--provider", PROVIDER, "--model-id", MODEL, "--version", "2026-11-02", "--kind", "llm")
            self.assertEqual(1, code, text)
            self.assertIn("no qualification record", text)

    def test_record_exits_one_when_the_run_is_not_qualified_but_still_writes_the_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            card = scorecard()
            card["tier0"]["all_zero"] = False
            out = root / "r.json"
            code, text = self.run_main("record", *self.files(root, card), "--out", str(out))
            self.assertEqual(1, code, text)
            self.assertEqual("NOT_QUALIFIED", json.loads(out.read_text(encoding="utf-8"))["verdict"])

    def test_without_a_baseline_record_or_a_floor_the_first_qualification_is_not_qualified(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            args = self.files(root, first_card())
            (root / "run.json").write_text(json.dumps(first_drift()), encoding="utf-8")  # a first qualification compares the version with itself
            args[args.index("--version") + 1] = PINNED["version"]
            i = args.index("--baseline-record")
            del args[i:i + 2]
            out = root / "r.json"
            code, text = self.run_main("record", *args, "--out", str(out))
            self.assertEqual(1, code, text)
            self.assertTrue(any("floor" in r for r in json.loads(out.read_text(encoding="utf-8"))["reasons"]))
            code, text = self.run_main("record", *args, "--floor", "0.10", "--floor-source", "DEC-2026-11-08-floor", "--out", str(out))
            self.assertEqual(1, code, text)  # A43: a floor below 0.90 does not qualify a model
            code, text = self.run_main("record", *args, "--floor", "0.90", "--floor-source", "DEC-2026-11-08-floor", "--out", str(out))
            self.assertEqual(0, code, text)

    def test_check_needs_the_kind(self) -> None:
        # C1 on noema#199: without --kind a record of any kind for the same provider, model id and version answered.
        with tempfile.TemporaryDirectory() as tmp, self.assertRaises(SystemExit):
            self.run_main("check", "--records", tmp, "--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION)

    def test_a_tier_zero_breach_in_a_repeat_run_makes_the_cli_exit_one_and_names_the_run(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            repeats = repeat_cards()
            repeats[3]["tier0"]["all_zero"] = False
            out = root / "r.json"
            code, text = self.run_main("record", *self.files(root, None, repeats), "--out", str(out))
            self.assertEqual(1, code, text)
            self.assertIn("run_repeat_5", text)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.assertEqual(2, self.run_main("record", *self.files(root, None, repeat_cards()[:2]), "--out", str(root / "r.json"))[0])  # too few runs: an input error

    def test_a_baseline_record_that_is_not_the_runs_baseline_is_an_input_error(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            args = self.files(root)
            other = first_record()
            other["version"] = "2026-09-01"
            (root / "baseline-record.json").write_text(json.dumps(other), encoding="utf-8")
            code, text = self.run_main("record", *args, "--out", str(root / "r.json"))
            self.assertEqual(2, code, text)
            self.assertFalse((root / "r.json").exists())

    def test_an_empty_records_directory_is_a_refusal_and_a_missing_one_is_an_input_error(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            args = ("--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION, "--kind", "llm")
            self.assertEqual(1, self.run_main("check", "--records", tmp, *args)[0])
            self.assertEqual(2, self.run_main("check", "--records", str(Path(tmp) / "nope"), *args)[0])

    def test_an_unreadable_record_is_an_input_error_not_a_pass(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "bad.json").write_text("{not json", encoding="utf-8")
            self.assertEqual(2, self.run_main("check", "--records", tmp, "--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION, "--kind", "llm")[0])

    def test_a_path_naming_the_held_out_set_is_refused(self) -> None:
        code, text = self.run_main("check", "--records", "benchmark-heldout/records", "--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION)
        self.assertEqual(2, code, text)
        self.assertIn("refused", text)


if __name__ == "__main__":
    unittest.main()
