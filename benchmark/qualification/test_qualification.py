"""UND-13, REQ-INT-006 (model-drift canary; DEC-2026-10-31 D25): a model or speech version is used only with a qualification record, and the record is made from a scorecard
that includes pass^k. Written before `record.py` and seen failing. SYNTHETIC scorecards from the real scorer. Generated layer only (INV-22, REQ-AGT-004).

The benchmark owns the runner side: the record format, how a record is made from a scorecard, and the lookup that refuses an unqualified ID. The model service's
configuration check (C2) calls the same lookup contract; it is not built here."""
import copy
import io
import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from typing import Any

from benchmark.qualification.__main__ import main
from benchmark.qualification.record import build_record, check_model, validate_record, verify_against_scorecard
from benchmark.scoring.test_report import card_for, fixture

Json = dict[str, Any]
PROVIDER, MODEL, VERSION, DEC = "openai", "decision-model-a", "2026-10-15", "DEC-2026-11-06-qualify-decision-model-a"


def scorecard() -> Json:
    cases, oracles, results, _ = fixture()
    card = copy.deepcopy(card_for(cases, oracles, results))
    card["complete"] = True
    card["tier0"]["all_zero"] = True
    card["tier1"]["diagnostics"]["pass_k"] = {"k_required": 5, "runs_supplied": 5, "available": True, "reason": None,
                                              "rate": {"k": 9, "n": 10, "rate": 0.9}, "failing_cases": ["BEN-1-0001"]}
    return card


def record(card: Json | None = None, **changes: Any) -> Json:
    args: Json = {"kind": "llm", "provider": PROVIDER, "model_id": MODEL, "version": VERSION, "decision_record": DEC, "qualified_on": "2026-11-06"}
    args.update(changes)
    return build_record(card or scorecard(), **args)


class Making(unittest.TestCase):
    def test_a_complete_tier_zero_clean_pass_k_run_is_qualified_and_valid(self) -> None:
        rec = record()
        self.assertEqual("QUALIFIED", rec["verdict"])
        self.assertEqual([], rec["reasons"])
        self.assertEqual([], validate_record(rec))
        self.assertEqual({"k": 9, "n": 10, "rate": 0.9}, rec["pass_k"]["rate"])

    def test_the_record_carries_the_numbers_it_was_made_from_and_the_scorecard_digest(self) -> None:
        card = scorecard()
        rec = record(card)
        for field in ("kind", "provider", "model_id", "version", "run_id", "n_cases", "complete", "tier0_all_zero", "pass_k", "scorecard_sha256", "decision_record", "qualified_on", "verdict", "reasons"):
            self.assertIn(field, rec)
        self.assertEqual(card["run_id"], rec["run_id"])
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


class Validating(unittest.TestCase):
    def test_a_qualified_verdict_the_fields_do_not_support_is_invalid(self) -> None:
        for field, value in (("complete", False), ("tier0_all_zero", False)):
            rec = record()
            rec[field] = value
            self.assertTrue(any("QUALIFIED" in p for p in validate_record(rec)), (field, validate_record(rec)))
        rec = record()
        rec["pass_k"] = {**rec["pass_k"], "available": False}
        self.assertTrue(any("pass^k" in p for p in validate_record(rec)))

    def test_a_missing_field_is_named(self) -> None:
        rec = record()
        del rec["decision_record"]
        self.assertTrue(any("decision_record" in p for p in validate_record(rec)))

    def test_a_record_edited_after_the_scorecard_no_longer_verifies(self) -> None:
        card = scorecard()
        rec = record(card)
        card["n_cases"] += 1
        self.assertTrue(any("digest" in p for p in verify_against_scorecard(rec, card)))


class Refusing(unittest.TestCase):
    def test_no_record_means_refused(self) -> None:
        got = check_model([], PROVIDER, MODEL, VERSION)
        self.assertTrue(any("no qualification record" in p for p in got), got)

    def test_a_qualified_record_for_exactly_this_provider_model_and_version_allows(self) -> None:
        self.assertEqual([], check_model([record()], PROVIDER, MODEL, VERSION))

    def test_a_change_of_version_model_or_provider_is_refused(self) -> None:
        rec = record()
        for provider, model, version in ((PROVIDER, MODEL, "2026-11-01"), (PROVIDER, "decision-model-b", VERSION), ("anthropic", MODEL, VERSION)):
            got = check_model([rec], provider, model, version)
            self.assertTrue(any("no qualification record" in p for p in got), (provider, model, version, got))

    def test_an_unpinned_requested_version_is_refused_even_when_a_record_exists(self) -> None:
        for version in ("", "latest", "default"):
            got = check_model([record()], PROVIDER, MODEL, version)
            self.assertTrue(any("pinned" in p for p in got), (version, got))

    def test_a_not_qualified_record_never_allows(self) -> None:
        card = scorecard()
        card["tier0"]["all_zero"] = False
        got = check_model([record(card)], PROVIDER, MODEL, VERSION)
        self.assertTrue(got)

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

    def test_record_then_check_round_trip(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "scorecard.json").write_text(json.dumps(scorecard()), encoding="utf-8")
            (root / "records").mkdir()
            base = ["--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION]
            code, text = self.run_main("record", "--scorecard", str(root / "scorecard.json"), *base, "--kind", "llm", "--decision-record", DEC, "--qualified-on", "2026-11-06", "--out", str(root / "records" / "r.json"))
            self.assertEqual(0, code, text)
            self.assertEqual(0, self.run_main("check", "--records", str(root / "records"), *base)[0])
            code, text = self.run_main("check", "--records", str(root / "records"), "--provider", PROVIDER, "--model-id", MODEL, "--version", "2026-11-01")
            self.assertEqual(1, code, text)
            self.assertIn("no qualification record", text)

    def test_record_exits_one_when_the_run_is_not_qualified_but_still_writes_the_record(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            card = scorecard()
            card["tier0"]["all_zero"] = False
            (root / "scorecard.json").write_text(json.dumps(card), encoding="utf-8")
            out = root / "r.json"
            code, text = self.run_main("record", "--scorecard", str(root / "scorecard.json"), "--provider", PROVIDER, "--model-id", MODEL, "--version", VERSION, "--kind", "llm",
                                       "--decision-record", DEC, "--qualified-on", "2026-11-06", "--out", str(out))
            self.assertEqual(1, code, text)
            self.assertEqual("NOT_QUALIFIED", json.loads(out.read_text(encoding="utf-8"))["verdict"])

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
