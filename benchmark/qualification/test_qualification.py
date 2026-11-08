"""UND-13, REQ-INT-006 (model-drift canary; DEC-2026-10-31 D25; ruling A17 of 6 Nov 2026): a model or speech version is used only with a qualification record. The record is made
from a scorecard that includes pass^k and from the drift run record. Interim QUALIFIED = the run is complete, Tier 0 is zero, the drift check is PASS and pass^k is not below the
baseline; a first qualification (no baseline) waits for a Founder-set floor. Written before the change to `record.py` and seen failing. SYNTHETIC scorecards from the real
scorer. Generated layer only (INV-22, REQ-AGT-004).

The benchmark owns the runner side: the record format (`contracts/schemas/qualification-record.v1.json`), how a record is made, and the lookup that refuses an unqualified ID.
The model service's configuration check (C2) calls the same lookup contract; it is not built here."""
import copy
import io
import json
import tempfile
import unittest
from collections.abc import Callable
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from typing import Any

from benchmark.contracts import Contracts
from benchmark.qualification.__main__ import main
from benchmark.qualification.pins import is_pinned
from benchmark.qualification.record import build_record, check_model, validate_record, verify_against_scorecard
from benchmark.qualification.run import build_baseline
from benchmark.qualification.test_run import NEW, PINNED, SHA, answers, baseline, run, subset
from benchmark.scoring.test_report import card_for, fixture

Json = dict[str, Any]
PROVIDER, MODEL, VERSION, DEC = NEW["provider"], NEW["model_id"], NEW["version"], "DEC-2026-11-06-qualify-decision-model-a"
CONTRACTS = Contracts()


def scorecard() -> Json:
    cases, oracles, results, _ = fixture()
    card = copy.deepcopy(card_for(cases, oracles, results))
    card["complete"] = True
    card["tier0"]["all_zero"] = True
    card["tier1"]["diagnostics"]["pass_k"] = {"k_required": 5, "runs_supplied": 5, "available": True, "reason": None,
                                              "rate": {"k": 9, "n": 10, "rate": 0.9}, "failing_cases": ["BEN-1-0001"]}
    return card


def record(card: Json | None = None, drift: Json | None = None, **changes: Any) -> Json:
    args: Json = {"kind": "llm", "provider": PROVIDER, "model_id": MODEL, "version": VERSION, "decision_record": DEC, "qualified_on": "2026-11-06", "baseline_pass_k": 0.8,
                  "floor": None, "floor_source": None}
    args.update(changes)
    return build_record(card or scorecard(), drift or run(), **args)


def first_record() -> Json:
    """A first qualification of the baseline version itself, which has no baseline and so needs a floor."""
    sub = subset()
    drift = run(answers(sub, PINNED["version"]), PINNED, sub, baseline(sub))
    return record(drift=drift, version=PINNED["version"], baseline_pass_k=None, floor=0.5, floor_source="DEC-2026-11-05-floor")


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
        self.assertEqual({"baseline_pass_k": 0.8, "floor": None, "floor_source": None}, rec["bar"])
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
        self.assertEqual("QUALIFIED", record(baseline_pass_k=None, floor=0.85, floor_source="DEC-2026-11-05-floor")["verdict"])
        self.assertEqual("NOT_QUALIFIED", record(baseline_pass_k=None, floor=0.95, floor_source="DEC-2026-11-05-floor")["verdict"])
        self.assertEqual("QUALIFIED", record(baseline_pass_k=0.8, floor=0.95, floor_source="DEC-2026-11-05-floor")["verdict"])
        self.assertEqual("NOT_QUALIFIED", record(baseline_pass_k=0.95, floor=0.5, floor_source="DEC-2026-11-05-floor")["verdict"])

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
            self.assertTrue(check_model([rec], PROVIDER, MODEL, VERSION), bad)

    def test_a_baseline_or_floor_that_is_not_a_finite_number_between_zero_and_one_is_refused(self) -> None:
        for field in ("baseline_pass_k", "floor"):
            for bad in (float("nan"), float("inf"), True, "0.9", -0.1, 1.1):
                rec = record()
                rec["bar"] = {**rec["bar"], field: bad}
                got = validate_record(rec)
                self.assertTrue(any(field in p or "bar" in p for p in got), (field, bad, got))
                self.assertTrue(check_model([rec], PROVIDER, MODEL, VERSION), (field, bad))

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
            self.assertTrue(check_model([rec], PROVIDER, MODEL, VERSION), reasons)


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
        self.assertTrue(check_model([], PROVIDER, MODEL, "stable-2"))

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
        got = check_model([good, broken], PROVIDER, MODEL, VERSION)
        self.assertTrue(any("invalid" in p for p in got), got)
        self.assertEqual([], check_model([good], PROVIDER, MODEL, VERSION))

    def test_an_invalid_record_for_another_identity_does_not_refuse_this_one(self) -> None:
        sub = subset()
        base_b = build_baseline(sub, {**PINNED, "model_id": "decision-model-b"}, answers(sub, PINNED["version"]), commit_sha=SHA)
        other = record(model_id="decision-model-b", drift=run(pinned={**NEW, "model_id": "decision-model-b"}, base=base_b))
        other["complete"] = "false"
        self.assertEqual([], check_model([record(), other], PROVIDER, MODEL, VERSION))


class ContractAndEvidence(unittest.TestCase):
    """Codex on 924c58d: the lookup trusted flags and counts the contract and the evidence do not support."""

    def test_pass_k_is_available_only_with_exactly_five_runs(self) -> None:
        for k_required, runs in ((1, 1), (5, 4), (4, 5), (5, 6), (0, 0)):
            rec = record()
            rec["pass_k"] = {**rec["pass_k"], "k_required": k_required, "runs_supplied": runs}
            got = validate_record(rec)
            self.assertTrue(any("five" in p and "pass^k" in p for p in got), (k_required, runs, got))
            self.assertTrue(check_model([rec], PROVIDER, MODEL, VERSION), (k_required, runs))

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
            got = check_model([rec], PROVIDER, MODEL, VERSION)
            self.assertTrue(got and any("invalid" in p for p in got), got)


class Refusing(unittest.TestCase):
    def test_no_record_means_refused(self) -> None:
        got = check_model([], PROVIDER, MODEL, VERSION)
        self.assertTrue(any("no qualification record" in p for p in got), got)

    def test_a_qualified_record_for_exactly_this_provider_model_and_version_allows(self) -> None:
        self.assertEqual([], check_model([record()], PROVIDER, MODEL, VERSION))

    def test_a_change_of_version_model_or_provider_is_refused(self) -> None:
        rec = record()
        for provider, model, version in ((PROVIDER, MODEL, "2026-11-02"), (PROVIDER, "decision-model-b", VERSION), ("anthropic", MODEL, VERSION)):
            got = check_model([rec], provider, model, version)
            self.assertTrue(any("no qualification record" in p for p in got), (provider, model, version, got))

    def test_an_unpinned_requested_version_is_refused_even_when_a_record_exists(self) -> None:
        for version in ("", "latest", "default"):
            got = check_model([record()], PROVIDER, MODEL, version)
            self.assertTrue(any("pinned" in p for p in got), (version, got))

    def test_a_not_qualified_record_never_allows(self) -> None:
        card = scorecard()
        card["tier0"]["all_zero"] = False
        self.assertTrue(check_model([record(card)], PROVIDER, MODEL, VERSION))

    def test_an_invalid_record_never_allows(self) -> None:
        rec = record()
        rec["complete"] = False
        self.assertTrue(check_model([rec], PROVIDER, MODEL, VERSION))

    def test_a_later_failed_requalification_revokes_an_earlier_qualification(self) -> None:
        bad = scorecard()
        bad["tier0"]["all_zero"] = False
        later = record(bad, qualified_on="2026-11-07")
        self.assertTrue(check_model([record(), later], PROVIDER, MODEL, VERSION))
        self.assertTrue(check_model([later, record()], PROVIDER, MODEL, VERSION))
        earlier_fail = record(bad, qualified_on="2026-11-01")
        self.assertEqual([], check_model([earlier_fail, record()], PROVIDER, MODEL, VERSION))

    def test_two_records_of_one_day_that_disagree_are_refused(self) -> None:
        bad = scorecard()
        bad["tier0"]["all_zero"] = False
        got = check_model([record(), record(bad)], PROVIDER, MODEL, VERSION)
        self.assertTrue(any("disagree" in p for p in got), got)

    def test_the_kind_must_match_when_asked(self) -> None:
        self.assertTrue(check_model([record()], PROVIDER, MODEL, VERSION, kind="speech"))
        self.assertEqual([], check_model([record()], PROVIDER, MODEL, VERSION, kind="llm"))


class Cli(unittest.TestCase):
    def run_main(self, *argv: str) -> tuple[int, str]:
        out = io.StringIO()
        with redirect_stdout(out), redirect_stderr(out):
            code = main(list(argv))
        return code, out.getvalue()

    def files(self, root: Path, card: Json | None = None) -> list[str]:
        (root / "scorecard.json").write_text(json.dumps(card or scorecard()), encoding="utf-8")
        (root / "run.json").write_text(json.dumps(run()), encoding="utf-8")
        (root / "baseline-record.json").write_text(json.dumps(first_record()), encoding="utf-8")
        (root / "records").mkdir()
        return ["--scorecard", str(root / "scorecard.json"), "--run", str(root / "run.json"), "--baseline-record", str(root / "baseline-record.json"), "--provider", PROVIDER,
                "--model-id", MODEL, "--version", VERSION, "--kind", "llm", "--decision-record", DEC, "--qualified-on", "2026-11-06"]

    def test_record_then_check_round_trip(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            code, text = self.run_main("record", *self.files(root), "--out", str(root / "records" / "r.json"))
            self.assertEqual(0, code, text)
            written = json.loads((root / "records" / "r.json").read_text(encoding="utf-8"))
            self.assertEqual(0.9, written["bar"]["baseline_pass_k"])  # read from the baseline version's own record
            base = ["--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION]
            self.assertEqual(0, self.run_main("check", "--records", str(root / "records"), *base)[0])
            code, text = self.run_main("check", "--records", str(root / "records"), "--provider", PROVIDER, "--model-id", MODEL, "--version", "2026-11-02")
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
            args = self.files(root)
            i = args.index("--baseline-record")
            del args[i:i + 2]
            out = root / "r.json"
            code, text = self.run_main("record", *args, "--out", str(out))
            self.assertEqual(1, code, text)
            self.assertTrue(any("floor" in r for r in json.loads(out.read_text(encoding="utf-8"))["reasons"]))
            code, text = self.run_main("record", *args, "--floor", "0.85", "--floor-source", "DEC-2026-11-05-floor", "--out", str(out))
            self.assertEqual(0, code, text)

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
            args = ("--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION)
            self.assertEqual(1, self.run_main("check", "--records", tmp, *args)[0])
            self.assertEqual(2, self.run_main("check", "--records", str(Path(tmp) / "nope"), *args)[0])

    def test_an_unreadable_record_is_an_input_error_not_a_pass(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp) / "bad.json").write_text("{not json", encoding="utf-8")
            self.assertEqual(2, self.run_main("check", "--records", tmp, "--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION)[0])

    def test_a_path_naming_the_held_out_set_is_refused(self) -> None:
        code, text = self.run_main("check", "--records", "benchmark-heldout/records", "--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION)
        self.assertEqual(2, code, text)
        self.assertIn("refused", text)


if __name__ == "__main__":
    unittest.main()
