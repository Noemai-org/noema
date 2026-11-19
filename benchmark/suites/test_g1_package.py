"""BEN-05 (G1 evidence package, noema#566 section 4): the package skeleton, rendered in minutes after the run from the run's own files. Written before `g1_package.py` and seen failing.
SYNTHETIC inputs. The renderer never says MET for anything it cannot tie to a link the Benchmark Owner opened and says it read; rows 5, 8, 9, 10 and 11 are computed from the run's artifacts (A355), not attested;
the headline never drops the stand-in label (Founder, 4 Nov 2026). It reads no oracle (INV-22)."""
import copy
import unittest
from collections.abc import Callable
from typing import Any

import json

from benchmark.suites.g1_package import ROWS, PackageError, render

Json = dict[str, Any]
SHA = "a" * 40
URL = "https://github.com/Noemai-org/noema/actions/runs/1"
SIX = ["BEN-A-0001", "BEN-A-0002", "BEN-CLEAN-0001", "BEN-CLEAN-1007", "BEN-TMO-0001", "BEN-TMO-1002"]


def inputs() -> Json:
    return {
        "dispatch_sha": SHA, "run_id": "g1-20261120", "fault_plan_id": "plan_1", "data_label": "stand-in", "us06_cases": ["BEN-TMO-0001"],
        "g1_cases": list(SIX), "required_checks": ["g1-stack"],
        "rows": {str(n): {"link": URL, "read_by_bo": True} for n in range(1, 13)},
        "not_tested": ["live Tushe (data is the stand-in)", "the model side is the replay provider"],
    }


def card(complete: bool = True, zero: bool = True, errored: list[str] | None = None, other: str | None = None) -> Json:
    incomplete: Json = {"missing": [], "errored": errored or [], "stale_version": [], "duplicate": [], "unknown": [], "invalid_case": []}
    if other:
        incomplete[other] = ["BEN-CLEAN-0001"]
    return {"complete": complete, "tier0": {"all_zero": zero}, "incomplete": incomplete}


def report(verdict: str = "IDENTICAL", excluded: list[str] | None = None, sha: str = SHA, cases: list[Json] | None = None) -> Json:
    ex = excluded or []
    rows = cases if cases is not None else [{"case_id": c, "result": "IDENTICAL"} for c in SIX if c not in ex]
    return {"verdict": verdict, "full_coverage": not ex, "case_coverage": {"covered": 6 - len(ex), "of": 6}, "open_exclusions": [{"case_id": c} for c in ex],
            "first": {"run_id": "g1-a"}, "second": {"run_id": "g1-b"}, "commit_sha": sha, "seed": 20261020, "cases": rows}


def leg(scorecard: Json, run_id: str) -> Json:
    """A scorecard of one replayed leg: it carries the leg's run id (scorecard.py)."""
    return {**scorecard, "run_id": run_id}


def check_runs(sha: str = SHA, runs: list[Any] | None = None, total: int | None = None) -> Json:
    """The shape of `gh api repos/.../commits/<sha>/check-runs`."""
    rows = runs if runs is not None else [{"name": "g1-stack", "head_sha": sha, "status": "completed", "conclusion": "success"},
                                          {"name": "core", "head_sha": sha, "status": "completed", "conclusion": "skipped"}]
    return {"total_count": len(rows) if total is None else total, "check_runs": rows}


KINDS = "canary, account_number, card_number, named_secret_value, jwt, bearer_token"
LEAK_OK = f"probe: every planted kind was found ({KINDS}): the scanner can see\nleakscan OK: scanned 12 files, 340 lines\n"


def probe_row(case_id: str) -> Json:
    refused = {"status": 404, "code": "NOT_FOUND_OR_NOT_AUTHORIZED", "message": "not found", "ok": True}
    return {"case_id": case_id, "principal_id": "p1", "correlation_id": "c1", "control": {"status": 200, "ok": True, "problems": []}, "wrong_role": dict(refused),
            "foreign_tenant": {"status": 401, "code": "SESSION_INVALID", "message": "bad session", "ok": True}, "unknown_correlation": dict(refused), "ok": True}


def probes(ids: list[str] | None = None) -> Json:
    return {"met": True, "cases": [probe_row(c) for c in (ids if ids is not None else SIX)]}


def art(**over: Any) -> Json:
    return {"check_runs": check_runs(), "leak_scan": LEAK_OK, "probes": probes(), **over}


def make(i: Json | None = None, a: Json | None = None, b: Json | None = None, r: Json | None = None, artifacts: Json | None = None) -> str:
    a, b = a or card(), b or card()
    return render(i or inputs(), a if "run_id" in a else leg(a, "g1-a"), b if "run_id" in b else leg(b, "g1-b"), r or report(), artifacts or art())


def row(text: str, n: int) -> str:
    return next(line for line in text.splitlines() if line.startswith(f"| {n} |"))


class Rows(unittest.TestCase):
    def test_a_complete_clean_run_marks_all_twelve_met_and_says_g1_met(self) -> None:
        text = make()
        for n in range(1, 13):
            self.assertIn("**MET**", row(text, n), n)
        self.assertIn("G1: MET", text)
        self.assertEqual(12, len(ROWS))

    def test_the_stand_in_label_is_in_the_headline_and_demands_a_rerun_against_tushe(self) -> None:
        text = make()
        self.assertIn("STAND-IN data, not Tushe", text.splitlines()[0])
        self.assertIn("rerun against live Tushe", text)

    def test_a_row_without_a_link_or_without_the_owners_read_is_not_met(self) -> None:
        i = inputs()
        del i["rows"]["3"]["link"]
        i["rows"]["4"]["read_by_bo"] = False
        i["rows"]["7"]["link"] = "not a url"
        text = make(i)
        for n in (3, 4, 7):
            self.assertIn("**NOT MET**", row(text, n), n)
        self.assertIn("G1: NOT MET", text)

    def test_a_missing_row_is_not_met_never_skipped(self) -> None:
        i = inputs()
        del i["rows"]["3"]
        self.assertIn("**NOT MET**", row(make(i), 3))


class ComputedRows(unittest.TestCase):
    def test_row_10_needs_both_legs_complete_with_tier_0_at_zero(self) -> None:
        self.assertIn("**NOT MET**", row(make(a=card(complete=False)), 10))
        self.assertIn("**NOT MET**", row(make(b=card(zero=False)), 10))
        self.assertIn("**MET**", row(make(), 10))

    def test_row_11_needs_an_identical_replay_and_the_unknown_outcome_case_inside_it(self) -> None:
        self.assertIn("**NOT MET**", row(make(r=report("DIFFERENT")), 11))
        self.assertIn("**NOT MET**", row(make(r=report("NOT_COMPARABLE")), 11))
        self.assertIn("**NOT MET**", row(make(r=report(excluded=["BEN-TMO-0001"])), 11))
        other = make(r=report(excluded=["BEN-CLEAN-1007"]))
        self.assertIn("**MET**", row(other, 11))
        self.assertIn("BEN-CLEAN-1007", other)  # an excluded case is named in the package

    def test_a_hand_written_met_cannot_override_a_computed_row(self) -> None:
        i = inputs()
        i["rows"]["10"]["verdict"] = "MET"
        self.assertIn("**NOT MET**", row(make(i, a=card(zero=False)), 10))


class AttestationAndExclusions(unittest.TestCase):
    """Codex P2s on noema#717 (ruling A322): row 5 needs the owner's attestation like every row, and row 10 accepts the registry-named exclusion exactly as `g1.accept_named_exclusions` does."""

    def test_rows_5_8_and_9_are_computed_so_they_need_no_attestation_and_no_link(self) -> None:
        """A355 supersedes the row-5 attestation of A322: these rows are computed from the artifacts, not attested."""
        i = inputs()
        for key in ("5", "8", "9"):
            del i["rows"][key]
        text = make(i)
        for n in (5, 8, 9):
            self.assertIn("**MET**", row(text, n), n)
        self.assertIn("G1: MET", text)

    def test_a_leg_whose_only_incompleteness_is_a_named_exclusion_is_met(self) -> None:
        a, b = card(False, errored=["BEN-CLEAN-1007"]), card(False, errored=["BEN-CLEAN-1007"])
        text = make(a=a, b=b, r=report(excluded=["BEN-CLEAN-1007"]))
        self.assertIn("**MET**", row(text, 10))
        self.assertIn("G1: MET", text)

    def test_an_error_that_is_not_a_named_exclusion_is_not_met(self) -> None:
        self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-CLEAN-1007", "BEN-CLEAN-0001"]), r=report(excluded=["BEN-CLEAN-1007"])), 10))
        self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-CLEAN-0001"]), r=report(excluded=["BEN-CLEAN-1007"])), 10))
        self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-CLEAN-1007"]), r=report()), 10), "the replay names no exclusion")

    def test_any_other_incomplete_reason_or_a_tier_0_gate_still_fails_even_with_the_exclusion(self) -> None:
        r = report(excluded=["BEN-CLEAN-1007"])
        for reason in ("missing", "stale_version", "duplicate", "unknown", "invalid_case"):
            self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-CLEAN-1007"], other=reason), r=r), 10), reason)
        self.assertIn("**NOT MET**", row(make(a=card(False, zero=False, errored=["BEN-CLEAN-1007"]), r=r), 10))
        self.assertIn("**NOT MET**", row(make(b=card(False, errored=["BEN-CLEAN-1007"], zero=False), r=r), 10))

    def test_the_unknown_outcome_case_is_never_an_accepted_exclusion(self) -> None:
        r = report(excluded=["BEN-TMO-0001"])
        self.assertIn("**NOT MET**", row(make(a=card(False, errored=["BEN-TMO-0001"]), r=r), 10))

    def test_an_incomplete_leg_with_no_errored_case_is_not_rescued(self) -> None:
        self.assertIn("**NOT MET**", row(make(a=card(False), r=report(excluded=["BEN-CLEAN-1007"])), 10))

    def test_a_malformed_scorecard_fails_closed(self) -> None:
        for bad in ({"complete": False}, {"complete": False, "tier0": {"all_zero": True}}, {"complete": False, "tier0": {"all_zero": True}, "incomplete": {"errored": "BEN-CLEAN-1007"}}):
            self.assertIn("**NOT MET**", row(make(a=bad, r=report(excluded=["BEN-CLEAN-1007"])), 10))


    def test_a_scorecard_of_the_wrong_shape_is_not_met_and_never_raises(self) -> None:
        wrong: list[Any] = [{"complete": True, "tier0": None}, {"complete": True, "tier0": "zero"}, {}, [], None]
        for bad in wrong:
            self.assertIn("**NOT MET**", row(render(inputs(), leg(bad, "g1-a") if isinstance(bad, dict) else bad, leg(card(), "g1-b"), report(), art()), 10))


class ReplayEvidence(unittest.TestCase):
    """Codex P2s on noema#717 at 088635bc: the replay must contain the unknown-outcome cases, and each scorecard must be the one of its replay leg."""

    def test_row_11_needs_every_unknown_outcome_case_in_the_replay_with_an_identical_result(self) -> None:
        limited = report(cases=[{"case_id": "BEN-CLEAN-0001", "result": "IDENTICAL"}])  # a replay limited to US-01: IDENTICAL, but it never compared US-06
        self.assertIn("**NOT MET**", row(make(r=limited), 11))
        self.assertIn("not in the replay", row(make(r=limited), 11))
        for bad in ("DIFFERENT", "NOT_COMPARABLE", None):
            r = report(cases=[{"case_id": "BEN-TMO-0001", "result": bad}])
            self.assertIn("**NOT MET**", row(make(r=r), 11), str(bad))

    def test_row_11_refuses_a_replay_without_a_readable_case_list(self) -> None:
        for broken in (None, "x", [{"result": "IDENTICAL"}], [None], [{"case_id": 7, "result": "IDENTICAL"}]):
            r = report()
            r["cases"] = broken
            self.assertIn("**NOT MET**", row(make(r=r), 11), str(broken))
        r = report()
        del r["cases"]
        self.assertIn("**NOT MET**", row(make(r=r), 11))

    def test_row_11_passes_when_every_unknown_outcome_case_is_compared_identical(self) -> None:
        self.assertIn("**MET**", row(make(r=report()), 11))

    def test_row_10_binds_each_scorecard_to_its_replay_leg(self) -> None:
        self.assertIn("**NOT MET**", row(make(a=leg(card(), "g1-other")), 10))
        self.assertIn("**NOT MET**", row(make(b=leg(card(), "g1-other")), 10))
        self.assertIn("**NOT MET**", row(make(a=leg(card(), "g1-b"), b=leg(card(), "g1-a")), 10), "the legs swapped")
        self.assertIn("**NOT MET**", row(render(inputs(), card(), leg(card(), "g1-b"), report(), art()), 10), "no run id")
        self.assertIn("**NOT MET**", row(render(inputs(), leg(card(), "g1-a"), {**card(), "run_id": 7}, report(), art()), 10), "a run id that is not a string")
        self.assertIn("**MET**", row(make(), 10))

    def test_an_empty_leg_id_matches_nothing_even_when_the_scorecard_carries_the_same_empty_id(self) -> None:
        r = report()
        r["first"] = {"run_id": ""}
        self.assertIn("**NOT MET**", row(render(inputs(), leg(card(), ""), leg(card(), "g1-b"), r, art()), 10))

    def test_every_unknown_outcome_case_must_be_in_the_replay_not_only_the_first(self) -> None:
        i = inputs()
        i["us06_cases"] = ["BEN-TMO-0001", "BEN-TMO-1002"]
        both = report()
        self.assertIn("**MET**", row(make(i, r=both), 11))
        second_missing = report(cases=[{"case_id": "BEN-TMO-0001", "result": "IDENTICAL"}])
        self.assertIn("**NOT MET**", row(make(i, r=second_missing), 11))
        self.assertIn("BEN-TMO-1002", row(make(i, r=second_missing), 11))

    def test_the_binding_also_holds_for_a_leg_accepted_through_a_named_exclusion(self) -> None:
        r = report(excluded=["BEN-CLEAN-1007"])
        ok = make(a=leg(card(False, errored=["BEN-CLEAN-1007"]), "g1-a"), b=leg(card(False, errored=["BEN-CLEAN-1007"]), "g1-b"), r=r)
        self.assertIn("**MET**", row(ok, 10))
        wrong = make(a=leg(card(False, errored=["BEN-CLEAN-1007"]), "g1-zzz"), b=leg(card(False, errored=["BEN-CLEAN-1007"]), "g1-b"), r=r)
        self.assertIn("**NOT MET**", row(wrong, 10))


class ReplayCoverage(unittest.TestCase):
    """Codex P2s on noema#717 at 6c42b563: a duplicate case id in the replay must not let a later IDENTICAL hide an earlier failure, and the replay must cover the G1 cases of both stories."""

    def test_a_case_listed_twice_is_not_met_even_when_the_last_row_is_identical(self) -> None:
        rows = [{"case_id": c, "result": "IDENTICAL"} for c in SIX]
        for first_bad in ("DIFFERENT", "NOT_COMPARABLE", "IDENTICAL"):
            doubled = [{"case_id": "BEN-TMO-0001", "result": first_bad}, *rows]
            text = make(r=report(cases=doubled))
            self.assertIn("**NOT MET**", row(text, 11), first_bad)
            self.assertIn("more than once", row(text, 11))

    def test_a_replay_of_only_the_unknown_outcome_story_is_not_met(self) -> None:
        only_us06 = report(cases=[{"case_id": "BEN-TMO-0001", "result": "IDENTICAL"}, {"case_id": "BEN-TMO-1002", "result": "IDENTICAL"}])
        text = make(r=only_us06)
        self.assertIn("**NOT MET**", row(text, 11))
        self.assertIn("BEN-A-0001", row(text, 11))

    def test_every_expected_case_must_be_identical_in_the_replay_not_only_the_unknown_outcome_ones(self) -> None:
        for bad in ("DIFFERENT", "NOT_COMPARABLE", None):
            rows = [{"case_id": c, "result": bad if c == "BEN-CLEAN-0001" else "IDENTICAL"} for c in SIX]
            self.assertIn("**NOT MET**", row(make(r=report(cases=rows)), 11), str(bad))

    def test_a_named_exclusion_is_the_only_way_for_an_expected_case_to_be_absent(self) -> None:
        self.assertIn("**MET**", row(make(r=report(excluded=["BEN-CLEAN-1007"])), 11))
        missing = report(cases=[{"case_id": c, "result": "IDENTICAL"} for c in SIX if c != "BEN-A-0002"])
        self.assertIn("**NOT MET**", row(make(r=missing), 11))

    def test_the_expected_g1_cases_are_required_input_and_include_the_unknown_outcome_cases(self) -> None:
        mutations: list[Callable[[Json], Any]] = [lambda i: i.pop("g1_cases"), lambda i: i.update(g1_cases=[]), lambda i: i.update(g1_cases=["BEN-A-0001"]), lambda i: i.update(g1_cases="x")]
        for mutate in mutations:
            i = inputs()
            mutate(i)
            with self.assertRaises(PackageError):
                make(i)


class ArtifactRows(unittest.TestCase):
    """A355: rows 5, 8 and 9 are computed from the run's artifacts (the check-runs JSON, the leak-scan output, the probe results), never attested."""

    def test_row_5_needs_every_check_run_at_the_dispatch_sha_completed_and_green(self) -> None:
        good = {"name": "g1-stack", "head_sha": SHA, "status": "completed", "conclusion": "success"}
        for name, runs in (("another head", [{**good, "head_sha": "b" * 40}]), ("in progress", [{**good, "status": "in_progress", "conclusion": None}]),
                           ("failure", [good, {**good, "name": "core", "conclusion": "failure"}]), ("cancelled", [good, {**good, "name": "core", "conclusion": "cancelled"}]),
                           ("timed out", [good, {**good, "name": "core", "conclusion": "timed_out"}]), ("no run", []), ("no conclusion", [good, {**good, "name": "x", "conclusion": None}]),
                           ("green but not completed", [good, {**good, "name": "x", "status": "queued", "conclusion": "success"}])):
            self.assertIn("**NOT MET**", row(make(artifacts=art(check_runs=check_runs(runs=runs))), 5), name)
        self.assertIn("**MET**", row(make(artifacts=art(check_runs=check_runs(runs=[good, {**good, "name": "ui", "conclusion": "neutral"}]))), 5))

    def test_row_5_needs_the_required_checks_present_and_successful_not_merely_skipped(self) -> None:
        skipped = check_runs(runs=[{"name": "g1-stack", "head_sha": SHA, "status": "completed", "conclusion": "skipped"}])
        self.assertIn("**NOT MET**", row(make(artifacts=art(check_runs=skipped)), 5))
        absent = check_runs(runs=[{"name": "core", "head_sha": SHA, "status": "completed", "conclusion": "success"}])
        self.assertIn("**NOT MET**", row(make(artifacts=art(check_runs=absent)), 5))
        i = inputs()
        i["required_checks"] = ["g1-stack", "joblog-scan"]
        self.assertIn("**NOT MET**", row(make(i), 5))
        i["required_checks"] = []
        with self.assertRaises(PackageError):
            make(i)

    def test_row_5_refuses_a_truncated_or_unreadable_check_runs_artifact(self) -> None:
        bad_name = {"total_count": 1, "check_runs": [{"name": 7, "head_sha": SHA, "status": "completed", "conclusion": "success"}]}
        for name, doc in (("a page of a longer list", check_runs(total=5)), ("none", None), ("a string", "ok"), ("no list", {"total_count": 1}),
                          ("a row that is not an object", {"total_count": 1, "check_runs": ["x"]}), ("a name that is not text", bad_name),
                          ("no total", {"check_runs": check_runs()["check_runs"]})):
            self.assertIn("**NOT MET**", row(make(artifacts=art(check_runs=doc)), 5), name)

    def test_row_5_merges_the_pages_of_a_paginated_read(self) -> None:
        a = {"name": "g1-stack", "head_sha": SHA, "status": "completed", "conclusion": "success"}
        b = {**a, "name": "core"}
        pages = [{"total_count": 2, "check_runs": [a]}, {"total_count": 2, "check_runs": [b]}]
        self.assertIn("**MET**", row(make(artifacts=art(check_runs=pages)), 5))
        self.assertIn("**NOT MET**", row(make(artifacts=art(check_runs=pages[:1])), 5), "one page of two")

    def test_row_5_needs_a_full_sha_and_a_run_made_at_it(self) -> None:
        short = inputs()
        short["dispatch_sha"] = "abc123"
        self.assertIn("**NOT MET**", row(make(short, r=report(sha="abc123"), artifacts=art(check_runs=check_runs(sha="abc123"))), 5))
        self.assertIn("**NOT MET**", row(make(r=report(sha="c" * 40)), 5))

    def test_rows_5_8_and_9_carry_the_digest_of_the_artifact_they_were_computed_from(self) -> None:
        text = make()
        for n in (5, 8, 9):
            self.assertIn("sha256:", row(text, n), n)
        self.assertEqual(row(make(), 8), row(make(), 8))
        self.assertNotEqual(row(make(), 8), row(make(artifacts=art(leak_scan=LEAK_OK + "\n")), 8))

    def test_row_8_needs_the_probe_line_with_every_kind_and_a_clean_scan_that_read_something(self) -> None:
        cases: list[tuple[str, Any]] = [
            ("no probe line", "leakscan OK: scanned 12 files, 340 lines\n"), ("no clean line", f"probe: every planted kind was found ({KINDS}): the scanner can see\n"),
            ("a kind missing", LEAK_OK.replace(", bearer_token", "")), ("a finding", LEAK_OK + "LEAK SCAN FAILED: findings in the artifacts\n"),
            ("a failed scan", LEAK_OK + "leakscan FAIL: a.log:3: jwt (length 40)\n"), ("not run", LEAK_OK + "NOT RUN: the scan itself failed or looked at nothing (exit 2)\n"),
            ("refused", LEAK_OK + "refused: the scanner printed the canary\n"), ("zero files", LEAK_OK.replace("12 files", "0 files")), ("zero lines", LEAK_OK.replace("340 lines", "0 lines")),
            ("empty", ""), ("not text", None), ("a number", 7)]
        for name, text in cases:
            self.assertIn("**NOT MET**", row(make(artifacts=art(leak_scan=text)), 8), name)
        self.assertIn("**MET**", row(make(), 8))

    def test_row_9_needs_the_two_separate_refusals_the_control_and_the_unknown_correlation_for_every_case(self) -> None:
        def broken(change: Callable[[Json], Any]) -> Json:
            doc = probes()
            change(doc["cases"][0])
            return doc
        cases: list[tuple[str, Callable[[Json], Any]]] = [
            ("wrong role served", lambda r: r["wrong_role"].update(status=200)), ("wrong role code", lambda r: r["wrong_role"].update(code="SESSION_INVALID")),
            ("foreign tenant 404", lambda r: r["foreign_tenant"].update(status=404, code="NOT_FOUND_OR_NOT_AUTHORIZED")), ("foreign tenant code", lambda r: r["foreign_tenant"].update(code="X")),
            ("control not 200", lambda r: r["control"].update(status=500)), ("control not ok", lambda r: r["control"].update(ok=False)),
            ("unknown answered differently", lambda r: r["unknown_correlation"].update(message="no such correlation")), ("unknown served", lambda r: r["unknown_correlation"].update(status=200)),
            ("a refusal missing", lambda r: r.pop("foreign_tenant")), ("a row that says not ok", lambda r: r.update(ok=False))]
        for name, change in cases:
            self.assertIn("**NOT MET**", row(make(artifacts=art(probes=broken(change))), 9), name)

    def test_row_9_recomputes_from_status_and_code_so_a_forged_ok_flag_does_not_pass(self) -> None:
        doc = probes()
        doc["cases"][1]["wrong_role"].update(status=200, ok=True)
        doc["cases"][1]["ok"] = True
        self.assertIn("**NOT MET**", row(make(artifacts=art(probes=doc)), 9))

    def test_row_9_needs_a_probe_for_every_expected_case_once_and_a_met_document(self) -> None:
        self.assertIn("**NOT MET**", row(make(artifacts=art(probes=probes(SIX[:-1]))), 9), "a case not probed")
        self.assertIn("**NOT MET**", row(make(artifacts=art(probes=probes([*SIX, SIX[0]]))), 9), "a case probed twice")
        self.assertIn("**NOT MET**", row(make(artifacts=art(probes=probes([]))), 9), "no case")
        self.assertIn("**NOT MET**", row(make(artifacts=art(probes={**probes(), "met": False})), 9), "the document says not met")
        for bad in (None, "x", {"met": True}, {"met": True, "cases": "x"}, {"met": True, "cases": [None]}):
            self.assertIn("**NOT MET**", row(make(artifacts=art(probes=bad)), 9), str(bad))
        self.assertIn("**MET**", row(make(artifacts=art(probes=probes([*SIX, "BEN-OTHER-0001"]))), 9), "a surplus probed case is harmless")

    def test_a_named_exclusion_needs_no_probe(self) -> None:
        r = report(excluded=["BEN-CLEAN-1007"])
        self.assertIn("**MET**", row(make(r=r, artifacts=art(probes=probes([c for c in SIX if c != "BEN-CLEAN-1007"]))), 9))
        self.assertIn("**NOT MET**", row(make(r=r, artifacts=art(probes=probes([c for c in SIX if c != "BEN-A-0001"]))), 9), "a case that is not excluded still needs one")


class Whole(unittest.TestCase):
    def test_what_was_not_tested_is_required_and_listed(self) -> None:
        self.assertIn("## What was not tested", make())
        self.assertIn("the model side is the replay provider", make())
        i = inputs()
        i["not_tested"] = []
        self.assertIn("G1: NOT MET", make(i))
        self.assertIn("section is empty", make(i))

    def test_a_bad_input_is_an_error_not_a_quiet_default(self) -> None:
        bad: list[Callable[[Json], Any]] = [lambda i: i.update(data_label="live"), lambda i: i.pop("dispatch_sha"), lambda i: i.update(us06_cases=[]), lambda i: i.update(rows="x")]
        for mutate in bad:
            i = inputs()
            mutate(i)
            with self.assertRaises(PackageError):
                make(i)

    def test_it_is_deterministic_and_carries_ids_and_links_only(self) -> None:
        a, b = make(), make(copy.deepcopy(inputs()))
        self.assertEqual(a, b)
        self.assertIn("g1-20261120", a)
        self.assertIn(URL, a)
        self.assertIn("plan_1", a)
        self.assertNotIn("oracle", a.lower().replace("no oracle", ""))


if __name__ == "__main__":
    unittest.main()
