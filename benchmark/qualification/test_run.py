"""UND-13, REQ-INT-006 (model-drift canary; DEC-2026-10-31 D25): the qualification subset, the pinned baseline, the comparison and the run record the model service's canary
emits (C2). Written before `run.py` and seen failing. SYNTHETIC cases from the real generator fixtures. Generated layer only: held-out material never reaches this module
or the model service (INV-22, REQ-AGT-004).

The baseline is the structured output of the previously qualified, pinned version for each subset case. It is not the oracle's expectation: the oracle is not read here."""
import copy
import io
import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from typing import Any

from benchmark.qualification.__main__ import main
from benchmark.qualification.run import (REQUIRED_RUN, build_baseline, compare, select_subset, validate_run_record)
from benchmark.scoring.diagnostics import understanding_dependent
from benchmark.scoring.test_report import fixture

Json = dict[str, Any]
PINNED = {"kind": "llm", "provider": "openai", "model_id": "decision-model-a", "version": "2026-10-15"}
NEW = {**PINNED, "version": "2026-11-01"}
SHA = "0123456789abcdef0123456789abcdef01234567"


def cases() -> list[Json]:
    return fixture()[0]


def subset() -> Json:
    return select_subset(cases(), subset_id="qs-test")


def answers(sub: Json, version: str) -> dict[str, Any]:
    return {cid: {"slots": {"intent": "dispute_charge", "amount_minor": 1000 + i, "currency": "NGN"}, "answered_by": version} for i, cid in enumerate(sub["case_ids"])}


def baseline(sub: Json | None = None) -> Json:
    sub = sub or subset()
    return build_baseline(sub, PINNED, answers(sub, PINNED["version"]), commit_sha=SHA)


def run(ans: dict[str, Any] | None = None, pinned: Json | None = None, sub: Json | None = None, base: Json | None = None) -> Json:
    sub = sub or subset()
    return compare(sub, base or baseline(sub), pinned or NEW, ans if ans is not None else answers(sub, NEW["version"]), commit_sha=SHA, recorded_on="2026-11-06")


class Subset(unittest.TestCase):
    def test_it_holds_exactly_the_generated_understanding_dependent_cases_sorted(self) -> None:
        sub = subset()
        want = sorted(c["case_id"] for c in cases() if understanding_dependent(c))
        self.assertTrue(want)
        self.assertEqual(want, sub["case_ids"])
        self.assertEqual(len(want), sub["n_cases"])
        self.assertEqual("GENERATED", sub["layer"])
        self.assertEqual("qs-test", sub["subset_id"])

    def test_it_is_deterministic_and_its_digest_follows_the_cases(self) -> None:
        self.assertEqual(subset(), subset())
        changed = cases()
        target = next(c for c in changed if understanding_dependent(c))
        target["utterance"] = target.get("utterance", "") + " changed"
        self.assertNotEqual(subset()["cases_sha256"], select_subset(changed, subset_id="qs-test")["cases_sha256"])

    def test_a_held_out_case_anywhere_in_the_input_is_refused(self) -> None:
        held = fixture(held_out=2)[0]
        with self.assertRaises(ValueError) as ctx:
            select_subset(held, subset_id="qs-test")
        self.assertIn("held-out", str(ctx.exception))

    def test_no_understanding_dependent_case_is_an_error_not_an_empty_subset(self) -> None:
        none = [c for c in cases() if not understanding_dependent(c)]
        with self.assertRaises(ValueError):
            select_subset(none, subset_id="qs-test")


class Baseline(unittest.TestCase):
    def test_it_pins_the_version_the_cases_and_the_commit(self) -> None:
        base = baseline()
        self.assertEqual(PINNED, base["pinned"])
        self.assertEqual(subset()["cases_sha256"], base["cases_sha256"])
        self.assertEqual(SHA, base["commit_sha"])
        self.assertEqual(subset()["case_ids"], sorted(base["cases"]))

    def test_every_subset_case_needs_an_answer_and_an_unavailable_one_is_not_a_baseline(self) -> None:
        sub = subset()
        short = answers(sub, PINNED["version"])
        del short[sub["case_ids"][0]]
        with self.assertRaises(ValueError):
            build_baseline(sub, PINNED, short, commit_sha=SHA)
        down = answers(sub, PINNED["version"])
        down[sub["case_ids"][0]] = None
        with self.assertRaises(ValueError):
            build_baseline(sub, PINNED, down, commit_sha=SHA)
        extra = answers(sub, PINNED["version"])
        extra["BEN-9-9999"] = {"slots": {}, "answered_by": PINNED["version"]}
        with self.assertRaises(ValueError):
            build_baseline(sub, PINNED, extra, commit_sha=SHA)

    def test_a_baseline_answered_by_another_version_than_it_pins_is_refused(self) -> None:
        sub = subset()
        with self.assertRaises(ValueError):
            build_baseline(sub, PINNED, answers(sub, "2026-10-16"), commit_sha=SHA)

    def test_an_unpinned_version_cannot_be_a_baseline(self) -> None:
        sub = subset()
        with self.assertRaises(ValueError):
            build_baseline(sub, {**PINNED, "version": "latest"}, answers(sub, "latest"), commit_sha=SHA)


class Comparing(unittest.TestCase):
    def test_identical_structured_outputs_from_the_new_version_pass(self) -> None:
        rec = run()
        self.assertEqual("PASS", rec["result"])
        self.assertEqual({"MATCH"}, {c["status"] for c in rec["cases"]})
        self.assertEqual([NEW["version"]], rec["answered_versions"])
        self.assertEqual([], validate_run_record(rec))

    def test_the_record_carries_what_the_canary_must_emit(self) -> None:
        rec = run()
        for field in REQUIRED_RUN:
            self.assertIn(field, rec)
        self.assertEqual(NEW, rec["pinned"])
        self.assertEqual(PINNED["version"], rec["baseline"]["pinned"]["version"])
        self.assertEqual(SHA, rec["commit_sha"])
        self.assertEqual("2026-11-06", rec["recorded_on"])
        self.assertEqual("GENERATED", rec["layer"])

    def test_a_changed_slot_is_drift_and_names_the_differing_slot(self) -> None:
        sub = subset()
        ans = answers(sub, NEW["version"])
        first = sub["case_ids"][0]
        ans[first] = {"slots": {**ans[first]["slots"], "currency": "USD"}, "answered_by": NEW["version"]}
        rec = run(ans)
        self.assertEqual("DRIFT", rec["result"])
        by_id = {c["case_id"]: c for c in rec["cases"]}
        self.assertEqual("DRIFT", by_id[first]["status"])
        self.assertEqual([{"slot": "currency", "expected": "NGN", "observed": "USD"}], by_id[first]["differing_slots"])
        self.assertTrue(all(c["status"] == "MATCH" for cid, c in by_id.items() if cid != first))
        self.assertEqual([], validate_run_record(rec))

    def test_a_missing_slot_and_an_extra_slot_are_differing_slots(self) -> None:
        sub = subset()
        ans = answers(sub, NEW["version"])
        first = sub["case_ids"][0]
        slots = dict(ans[first]["slots"])
        del slots["currency"]
        slots["channel"] = "ussd"
        ans[first] = {"slots": slots, "answered_by": NEW["version"]}
        diffs = {d["slot"]: d for d in {c["case_id"]: c for c in run(ans)["cases"]}[first]["differing_slots"]}
        self.assertEqual({"currency": {"slot": "currency", "expected": "NGN"}, "channel": {"slot": "channel", "observed": "ussd"}}, diffs)

    def test_an_unavailable_provider_is_its_own_result_and_never_a_pass(self) -> None:
        sub = subset()
        ans = answers(sub, NEW["version"])
        ans[sub["case_ids"][0]] = None
        rec = run(ans)
        self.assertEqual("PROVIDER_UNAVAILABLE", rec["result"])
        self.assertEqual("UNAVAILABLE", {c["case_id"]: c for c in rec["cases"]}[sub["case_ids"][0]]["status"])
        self.assertEqual([], validate_run_record(rec))

    def test_drift_outranks_unavailable(self) -> None:
        sub = subset()
        ans = answers(sub, NEW["version"])
        ans[sub["case_ids"][0]] = None
        ans[sub["case_ids"][1]] = {"slots": {"intent": "other"}, "answered_by": NEW["version"]}
        self.assertEqual("DRIFT", run(ans)["result"])

    def test_a_version_that_answered_is_not_the_version_that_was_asked_for_even_when_the_slots_match(self) -> None:
        sub = subset()
        ans = answers(sub, NEW["version"])
        first = sub["case_ids"][0]
        ans[first] = {**ans[first], "answered_by": "2026-11-09"}
        rec = run(ans)
        self.assertEqual("DRIFT", rec["result"])
        case = {c["case_id"]: c for c in rec["cases"]}[first]
        self.assertEqual("DRIFT", case["status"])
        self.assertEqual("2026-11-09", case["answered_by"])
        self.assertTrue(case["version_mismatch"])
        self.assertEqual([], case["differing_slots"])
        self.assertEqual(sorted({NEW["version"], "2026-11-09"}), rec["answered_versions"])

    def test_a_different_subset_than_the_baselines_is_refused(self) -> None:
        sub = subset()
        other = copy.deepcopy(sub)
        other["cases_sha256"] = "sha256:" + "0" * 64
        with self.assertRaises(ValueError):
            compare(other, baseline(sub), NEW, answers(sub, NEW["version"]), commit_sha=SHA, recorded_on="2026-11-06")

    def test_a_baseline_of_another_provider_or_model_is_refused_but_another_version_is_the_point(self) -> None:
        for change in ({"provider": "anthropic"}, {"model_id": "decision-model-b"}, {"kind": "speech"}):
            with self.assertRaises(ValueError, msg=str(change)):
                run(pinned={**NEW, **change})
        self.assertEqual("PASS", run(pinned={**NEW, "version": "2026-12-01"}, ans=answers(subset(), "2026-12-01"))["result"])

    def test_an_unpinned_version_cannot_be_compared(self) -> None:
        with self.assertRaises(ValueError):
            run(pinned={**NEW, "version": "latest"})

    def test_an_answer_for_a_case_outside_the_subset_or_missing_is_refused(self) -> None:
        sub = subset()
        extra = answers(sub, NEW["version"])
        extra["BEN-9-9999"] = {"slots": {}, "answered_by": NEW["version"]}
        with self.assertRaises(ValueError):
            run(extra)
        short = answers(sub, NEW["version"])
        del short[sub["case_ids"][0]]
        with self.assertRaises(ValueError):
            run(short)


class Validating(unittest.TestCase):
    def test_a_result_the_cases_do_not_support_is_invalid(self) -> None:
        rec = run()
        rec["result"] = "DRIFT"
        self.assertTrue(any("result" in p for p in validate_run_record(rec)))
        rec = run()
        rec["cases"][0]["status"] = "DRIFT"
        rec["cases"][0]["differing_slots"] = [{"slot": "x", "expected": 1, "observed": 2}]
        self.assertTrue(any("result" in p for p in validate_run_record(rec)))

    def test_a_missing_field_a_bad_status_and_an_unpinned_version_are_named(self) -> None:
        rec = run()
        del rec["commit_sha"]
        self.assertTrue(any("commit_sha" in p for p in validate_run_record(rec)))
        rec = run()
        rec["cases"][0]["status"] = "MAYBE"
        self.assertTrue(any("MAYBE" in p for p in validate_run_record(rec)))
        rec = run()
        rec["pinned"]["version"] = "latest"
        self.assertTrue(any("pinned" in p for p in validate_run_record(rec)))

    def test_the_shipped_json_schema_requires_exactly_the_fields_the_validator_does(self) -> None:
        schema = json.loads((Path(__file__).parent / "schemas" / "qualification-run.v1.schema.json").read_text(encoding="utf-8"))
        self.assertEqual(sorted(REQUIRED_RUN), sorted(schema["required"]))
        self.assertEqual({"PASS", "DRIFT", "PROVIDER_UNAVAILABLE"}, set(schema["properties"]["result"]["enum"]))
        self.assertEqual({"MATCH", "DRIFT", "UNAVAILABLE"}, set(schema["$defs"]["case"]["properties"]["status"]["enum"]))


class Cli(unittest.TestCase):
    def run_main(self, *argv: str) -> tuple[int, str]:
        out = io.StringIO()
        with redirect_stdout(out), redirect_stderr(out):
            code = main(list(argv))
        return code, out.getvalue()

    def test_subset_baseline_and_compare_round_trip_with_drift_exit_codes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "cases.json").write_text(json.dumps(cases()), encoding="utf-8")
            code, text = self.run_main("subset", "--cases", str(root / "cases.json"), "--subset-id", "qs-test", "--out", str(root / "subset.json"))
            self.assertEqual(0, code, text)
            sub = json.loads((root / "subset.json").read_text(encoding="utf-8"))
            (root / "base-answers.json").write_text(json.dumps(answers(sub, PINNED["version"])), encoding="utf-8")
            pin = ["--kind", "llm", "--provider", "openai", "--model-id", "decision-model-a"]
            code, text = self.run_main("baseline", "--subset", str(root / "subset.json"), "--answers", str(root / "base-answers.json"), *pin, "--version", PINNED["version"],
                                       "--commit-sha", SHA, "--out", str(root / "baseline.json"))
            self.assertEqual(0, code, text)
            (root / "new-answers.json").write_text(json.dumps(answers(sub, NEW["version"])), encoding="utf-8")
            args = ("compare", "--subset", str(root / "subset.json"), "--baseline", str(root / "baseline.json"), *pin, "--version", NEW["version"], "--commit-sha", SHA,
                    "--recorded-on", "2026-11-06", "--out", str(root / "run.json"))
            code, text = self.run_main(*args, "--answers", str(root / "new-answers.json"))
            self.assertEqual(0, code, text)
            self.assertEqual("PASS", json.loads((root / "run.json").read_text(encoding="utf-8"))["result"])
            drifted = answers(sub, NEW["version"])
            drifted[sub["case_ids"][0]] = {"slots": {"intent": "x"}, "answered_by": NEW["version"]}
            (root / "drift-answers.json").write_text(json.dumps(drifted), encoding="utf-8")
            code, text = self.run_main(*args, "--answers", str(root / "drift-answers.json"))
            self.assertEqual(1, code, text)
            self.assertEqual("DRIFT", json.loads((root / "run.json").read_text(encoding="utf-8"))["result"])
            down = answers(sub, NEW["version"])
            down[sub["case_ids"][0]] = None
            (root / "down-answers.json").write_text(json.dumps(down), encoding="utf-8")
            code, text = self.run_main(*args, "--answers", str(root / "down-answers.json"))
            self.assertEqual(1, code, text)
            self.assertEqual("PROVIDER_UNAVAILABLE", json.loads((root / "run.json").read_text(encoding="utf-8"))["result"])

    def test_bad_input_is_exit_two(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            held = fixture(held_out=2)[0]
            (root / "cases.json").write_text(json.dumps(held), encoding="utf-8")
            code, text = self.run_main("subset", "--cases", str(root / "cases.json"), "--subset-id", "qs-test", "--out", str(root / "subset.json"))
            self.assertEqual(2, code, text)
            self.assertFalse((root / "subset.json").exists())


if __name__ == "__main__":
    unittest.main()
