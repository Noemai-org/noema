"""BEN-05 replay diff: two runs of the same cases through the public API give identical Trust decisions and verdicts (REQ-NFR-001, REQ-BEN-002, INV-21).

These tests were written before `compare.py` and seen failing. SYNTHETIC data only."""
import copy
import unittest
from typing import Any

from benchmark.replay.compare import ReplayInputError, compare

Json = dict[str, Any]
SUITE_DIGEST = "sha256:" + "a" * 64
COMMIT = "b" * 40


CATEGORIES = ("CLEAN_UNIQUE", "AMBIGUITY", "STATE_CONFLICT", "TIMEOUT_UNKNOWN", "DUPLICATE_REPLAY", "AUTHORISATION", "POLICY_CLOCK", "PROMPT_INJECTION", "STALE_SESSION", "INTEGRATION_HOSTILITY")


def suite_manifest() -> Json:
    def entry(case_id: str, category: str, covers: list[str], mutation: Json | None = None) -> Json:
        return {"case_id": case_id, "category": category, "covers": covers, "mutation": mutation, "base_case_id": None if not mutation else "BEN-CLEAN-0001",
                "sha256": SUITE_DIGEST, "suite_layer": "GENERATED", "version": 1}
    cases = [
        entry("BEN-CLEAN-0001", "CLEAN_UNIQUE", ["US-01", "FAULT:NONE"]),
        entry("BEN-CLEAN-1007", "CLEAN_UNIQUE", ["US-01", "FAULT:INJECTION_PAYLOAD"], {"kind": "FAMILY", "name": "INJECTION_IN_NAME"}),
        entry("BEN-TMO-0001", "TIMEOUT_UNKNOWN", ["US-06", "FAULT:TIMEOUT_AFTER_COMMIT"]),
        entry("BEN-AMB-0001", "AMBIGUITY", ["US-02"]),
    ]
    return {"suite_id": "generated", "suite_version": "1.3.0", "generator": {"name": "noema-casegen", "version": "1.3.0", "seed": 20261020},
            "layer_counts": {"GENERATED": 4, "HELD_OUT": 0, "PARTNER": 0},
            "category_counts": {c: sum(1 for e in cases if e["category"] == c) for c in CATEGORIES},
            "pidgin_or_code_switched": {"count": 0, "of": 4}, "cases": cases}


def run_manifest(run_id: str, *, status: str = "COMPLETE", commit: str | None = COMMIT, digest: str = SUITE_DIGEST, seed: int = 20261020) -> Json:
    return {"run_id": run_id, "status": status, "not_run_reason": None if status != "NOT_RUN" else "no access: core",
            "suite": {"suite_id": "generated", "suite_version": "1.3.0", "manifest_sha256": digest,
                      "generator": {"name": "noema-casegen", "version": "1.3.0", "seed": seed}},
            "pack_version": "fin@1.0.0", "policy_version": "fin.pol@1.0", "commit_sha": commit, "providers": [],
            "counter_sources": {"oracle_accesses_by_runtime": "synthetic", "model_governance_changes": "synthetic", "monetary_calls": "synthetic"},
            "counts": {"total": 3, "run": 3, "completed": 3, "error": 0, "invalid": 0}, "errors_by_code": {}, "invalid_case_ids": [],
            "clock_start": "2026-10-01T09:40:00+01:00"}


def result(case_id: str, run_id: str, *, disposition: str = "ALLOW", grant: str = "VALID", verdict: str = "ACHIEVED", effects: list[Json] | None = None) -> Json:
    target = "fin.transaction:TX0001"
    return {"run_id": run_id, "case_id": case_id, "case_version": 1, "status": "COMPLETED", "error": None,
            "observed": {"disposition": disposition, "classification": "SUCCESS_CONFIRMED", "resolved_target": target, "clarification_asked": False,
                         "escalation_opened": False, "dispatches": [{"capability": "fin.cap.x", "target": target, "grant": grant}],
                         "verdicts": [{"capability": "fin.cap.x", "target": target, "verdict": verdict}],
                         "audit_events": ["INPUT_RECEIVED", "DECISION_MADE"], "claims": [], "disclosed_refs": [target]},
            "environment": {"effects": effects if effects is not None else [{"capability": "fin.cap.x", "target": target, "count": 1}],
                            "oracle_accesses_by_runtime": 0, "model_governance_changes": 0, "monetary_calls": 0},
            "path": "CLEAN", "latency_ms": 900 if run_id == "run_a" else 1100, "cost_usd_micros": None, "outcome_digest": "sha256:" + "1" * 64}


def pair(**changes: Any) -> tuple[Json, Json]:
    a = {"BEN-CLEAN-0001": result("BEN-CLEAN-0001", "run_a"), "BEN-CLEAN-1007": result("BEN-CLEAN-1007", "run_a"), "BEN-TMO-0001": result("BEN-TMO-0001", "run_a")}
    b = {k: result(k, "run_b") for k in a}
    for key, value in changes.items():
        b[key] = value
    return a, b


def go(a: dict[str, Json] | None = None, b: dict[str, Json] | None = None, *, manifest_b: Json | None = None, stories: tuple[str, ...] = ("US-01", "US-06"),
       exclude_mutated: bool = False) -> Json:
    pa, pb = pair()
    return compare(suite_manifest(), run_manifest("run_a"), a if a is not None else pa, manifest_b or run_manifest("run_b"), b if b is not None else pb, stories,
                   exclude_mutated=exclude_mutated)


class Identical(unittest.TestCase):
    def test_two_equal_runs_are_identical_and_latency_is_not_compared(self) -> None:
        report = go()
        self.assertEqual("IDENTICAL", report["verdict"])
        self.assertEqual({"selected": 3, "compared": 3, "identical": 3, "different": 0, "not_comparable": 0}, report["counts"])
        self.assertEqual({"US-01": 2, "US-06": 1}, report["stories_covered"])

    def test_selection_includes_mutated_cases_and_nothing_outside_the_stories(self) -> None:
        self.assertEqual(["BEN-CLEAN-0001", "BEN-CLEAN-1007", "BEN-TMO-0001"], [c["case_id"] for c in go()["cases"]])

    def test_report_names_both_runs_the_seed_the_commit_and_the_suite(self) -> None:
        report = go()
        self.assertEqual(("run_a", "run_b"), (report["first"]["run_id"], report["second"]["run_id"]))
        self.assertEqual(20261020, report["seed"])
        self.assertEqual(COMMIT, report["commit_sha"])
        self.assertEqual(SUITE_DIGEST, report["suite_manifest_sha256"])

    def test_report_is_deterministic(self) -> None:
        self.assertEqual(go(), go())


class Selection(unittest.TestCase):
    def test_default_selection_is_every_case_covering_the_stories_and_says_so(self) -> None:
        self.assertEqual("ALL_CASES_COVERING_THE_STORIES", go()["selection"])

    def test_excluding_mutated_cases_drops_them_and_the_report_records_the_choice(self) -> None:
        report = go(exclude_mutated=True)
        self.assertEqual(["BEN-CLEAN-0001", "BEN-TMO-0001"], [c["case_id"] for c in report["cases"]])
        self.assertEqual("BASE_CASES_ONLY", report["selection"])
        self.assertEqual(["BEN-CLEAN-1007"], report["excluded_mutated"])

    def test_the_report_states_the_coverage_as_n_of_m_and_names_each_exclusion_as_open(self) -> None:
        report = go(exclude_mutated=True)
        self.assertEqual({"covered": 2, "of": 3}, report["case_coverage"])
        self.assertEqual([{"case_id": "BEN-CLEAN-1007", "status": "OPEN", "mutation": "INJECTION_IN_NAME", "faults": ["FAULT:INJECTION_PAYLOAD"],
                           "covers": ["US-01"]}], report["open_exclusions"])

    def test_the_default_selection_covers_all_and_has_no_exclusions(self) -> None:
        report = go()
        self.assertEqual({"covered": 3, "of": 3}, report["case_coverage"])
        self.assertEqual([], report["open_exclusions"])

    def test_an_errored_mutated_case_does_not_block_the_base_only_verdict_but_stays_listed(self) -> None:
        pa, pb = pair()
        for run_id, results in (("run_a", pa), ("run_b", pb)):
            r = results["BEN-CLEAN-1007"]
            r.update({"status": "ERROR", "error": {"code": "FAULT_UNSUPPORTED", "message": "x"}, "observed": None, "environment": None, "outcome_digest": None})
        self.assertEqual("NOT_COMPARABLE", go(pa, pb)["verdict"])
        report = go(pa, pb, exclude_mutated=True)
        self.assertEqual("IDENTICAL", report["verdict"])
        self.assertEqual(["BEN-CLEAN-1007"], report["excluded_mutated"])


class Different(unittest.TestCase):
    def check(self, changed: Json, field: str) -> None:
        pa, pb = pair(**{"BEN-TMO-0001": changed})
        report = go(pa, pb)
        self.assertEqual("DIFFERENT", report["verdict"])
        self.assertEqual(1, report["counts"]["different"])
        row = next(c for c in report["cases"] if c["case_id"] == "BEN-TMO-0001")
        self.assertEqual("DIFFERENT", row["result"])
        self.assertEqual([field], [d["field"] for d in row["differences"]])

    def test_a_different_trust_decision_is_a_difference(self) -> None:
        self.check(result("BEN-TMO-0001", "run_b", disposition="ESCALATE"), "disposition")

    def test_a_different_grant_state_is_a_difference(self) -> None:
        self.check(result("BEN-TMO-0001", "run_b", grant="SPENT"), "dispatches")

    def test_a_different_verdict_is_a_difference(self) -> None:
        self.check(result("BEN-TMO-0001", "run_b", verdict="UNKNOWN"), "verdicts")

    def test_a_different_effect_is_a_difference(self) -> None:
        self.check(result("BEN-TMO-0001", "run_b", effects=[]), "effects")

    def test_the_order_of_dispatches_and_verdicts_is_not_a_difference(self) -> None:
        a = result("BEN-TMO-0001", "run_a")
        a["observed"]["dispatches"].append({"capability": "fin.cap.y", "target": "txn_0001", "grant": "VALID"})
        b = copy.deepcopy(a)
        b["run_id"] = "run_b"
        b["observed"]["dispatches"].reverse()
        pa, pb = pair(**{"BEN-TMO-0001": b})
        pa["BEN-TMO-0001"] = a
        self.assertEqual("IDENTICAL", go(pa, pb)["verdict"])

    def test_a_difference_shows_both_values(self) -> None:
        pa, pb = pair(**{"BEN-TMO-0001": result("BEN-TMO-0001", "run_b", disposition="ESCALATE")})
        diff = next(c for c in go(pa, pb)["cases"] if c["case_id"] == "BEN-TMO-0001")["differences"][0]
        self.assertEqual(("ALLOW", "ESCALATE"), (diff["first"], diff["second"]))


class NotComparable(unittest.TestCase):
    def test_an_errored_case_is_never_identical_even_when_it_errors_the_same_way(self) -> None:
        def errored(run_id: str) -> Json:
            r = result("BEN-CLEAN-1007", run_id)
            r.update({"status": "ERROR", "error": {"code": "FAULT_UNSUPPORTED", "message": "x"}, "observed": None, "environment": None, "outcome_digest": None})
            return r
        pa, pb = pair(**{"BEN-CLEAN-1007": errored("run_b")})
        pa["BEN-CLEAN-1007"] = errored("run_a")
        report = go(pa, pb)
        self.assertEqual("NOT_COMPARABLE", report["verdict"])
        row = next(c for c in report["cases"] if c["case_id"] == "BEN-CLEAN-1007")
        self.assertEqual("NOT_COMPARABLE", row["result"])
        self.assertIn("FAULT_UNSUPPORTED", row["reason"])

    def test_a_case_missing_from_one_run_is_not_comparable(self) -> None:
        pa, pb = pair()
        del pb["BEN-TMO-0001"]
        report = go(pa, pb)
        self.assertEqual("NOT_COMPARABLE", report["verdict"])
        self.assertEqual(1, report["counts"]["not_comparable"])

    def test_a_difference_outranks_a_not_comparable_case(self) -> None:
        pa, pb = pair(**{"BEN-TMO-0001": result("BEN-TMO-0001", "run_b", disposition="REJECT")})
        del pb["BEN-CLEAN-0001"]
        self.assertEqual("DIFFERENT", go(pa, pb)["verdict"])

    def test_a_story_with_no_comparable_case_is_not_identical(self) -> None:
        pa, pb = pair()
        del pa["BEN-TMO-0001"], pb["BEN-TMO-0001"]
        report = go(pa, pb)
        self.assertEqual("NOT_COMPARABLE", report["verdict"])
        self.assertEqual({"US-01": 2, "US-06": 0}, report["stories_covered"])


class Refusals(unittest.TestCase):
    def refused(self, manifest_b: Json | None = None, **kw: Any) -> str:
        with self.assertRaises(ReplayInputError) as ctx:
            go(manifest_b=manifest_b, **kw)
        return str(ctx.exception)

    def test_the_same_run_id_twice_is_refused(self) -> None:
        self.assertIn("run_id", self.refused(run_manifest("run_a")))

    def test_a_different_suite_is_refused(self) -> None:
        self.assertIn("suite", self.refused(run_manifest("run_b", digest="sha256:" + "c" * 64)))

    def test_a_different_seed_is_refused(self) -> None:
        self.assertIn("seed", self.refused(run_manifest("run_b", seed=1)))

    def test_a_different_commit_is_refused(self) -> None:
        self.assertIn("commit", self.refused(run_manifest("run_b", commit="d" * 40)))

    def test_a_run_with_no_commit_is_refused_because_the_evidence_cannot_name_the_code(self) -> None:
        self.assertIn("commit", self.refused(run_manifest("run_b", commit=None)))

    def test_a_run_that_was_not_run_is_refused(self) -> None:
        self.assertIn("NOT_RUN", self.refused(run_manifest("run_b", status="NOT_RUN")))

    def test_an_unknown_story_is_refused(self) -> None:
        self.assertIn("US-99", self.refused(stories=("US-99",)))

    def test_no_stories_is_refused(self) -> None:
        self.assertIn("stories", self.refused(stories=()))

    def test_a_result_from_another_run_is_refused(self) -> None:
        pa, pb = pair()
        pb["BEN-TMO-0001"]["run_id"] = "run_other"
        with self.assertRaises(ReplayInputError):
            go(pa, pb)


if __name__ == "__main__":
    unittest.main()
