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
CORRELATIONS = {"BEN-TMO-0001": "corr_tmo", "BEN-CLEAN-0001": "corr_clean"}
RUN = "g1-20261120"
STAMP = {"sha": SHA, "run_id": RUN}
STAMP_LINE = f"stamp: {SHA} {RUN}\n"
SIX = ["BEN-A-0001", "BEN-A-0002", "BEN-CLEAN-0001", "BEN-CLEAN-1007", "BEN-TMO-0001", "BEN-TMO-1002"]


def inputs() -> Json:
    return {
        "dispatch_sha": SHA, "run_id": "g1-20261120", "fault_plan_id": "plan_1", "data_label": "stand-in", "us06_cases": ["BEN-TMO-0001"], "us01_cases": ["BEN-CLEAN-0001"], "tenant_id": "ten_alpha",
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
LEAK_OK = STAMP_LINE + f"probe: every planted kind was found ({KINDS}): the scanner can see\nleakscan OK: scanned 12 files, 340 lines\n"


FILES_OK = STAMP_LINE + "requests.jsonl\nreconstructions.json\nstamp.json\nnegative-probes.json\nmock-case-writes.json\ncorrelations.json\nscorecard-a.json\nscorecard-b.json\nreplay.json\ncheck-runs.json\nleak-scan.txt\nG1-EVIDENCE-PACKAGE.md\n"


LEAK_BIG = STAMP_LINE + f"probe: every planted kind was found ({KINDS}): the scanner can see\nleakscan OK: scanned 100 files, 340 lines\n"


def corr_of(case_id: str) -> str:
    return CORRELATIONS.get(case_id, "corr_" + case_id.lower())


def probe_row(case_id: str) -> Json:
    refused = {"status": 404, "code": "NOT_FOUND_OR_NOT_AUTHORIZED", "message": "not found", "ok": True}
    return {"case_id": case_id, "principal_id": "p1", "correlation_id": corr_of(case_id), "control": {"status": 200, "ok": True, "problems": []}, "wrong_role": dict(refused),
            "foreign_tenant": {"status": 401, "code": "SESSION_INVALID", "message": "bad session", "ok": True}, "unknown_correlation": dict(refused), "ok": True}


def probes(ids: list[str] | None = None) -> Json:
    return {"stamp": dict(STAMP), "met": True, "cases": [probe_row(c) for c in (ids if ids is not None else SIX)]}


def event(seq: int, kind: str) -> Json:
    return {"event_id": f"recev_{seq}", "seq": seq, "event_type": kind}


def section(kind: str, *facts: Json) -> Json:
    return {"kind": kind, "items": [{"record_event_id": "recev_1", "object_ref": "o", "basis": "RULE", "facts": f} for f in facts]}


US06_EVENTS = ["DECISION_MADE", "GRANT_ISSUED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "VERIFICATION_READ", "VERDICT"]


def recon06(kinds: list[str] | None = None, state: str = "OUTCOME_UNKNOWN", verdict: str = "ACHIEVED", corr: str = "corr_tmo", tenant: str = "ten_alpha") -> Json:
    return {"tenant_id": tenant, "correlation_id": corr, "events": [event(n, k) for n, k in enumerate(kinds or US06_EVENTS, start=1)],
            "sections": [section("DECISION", {"decision_outcome": "ALLOW"}), section("ACTION", {"execution_state": state}), section("VERIFICATION", {"verdict": verdict})]}


def recon01(kinds: list[str] | None = None, outcome: str = "ALLOW", corr: str = "corr_clean", tenant: str = "ten_alpha") -> Json:
    return {"tenant_id": tenant, "correlation_id": corr, "events": [event(n, k) for n, k in enumerate(kinds if kinds is not None else ["INTENT_ADMITTED", "DECISION_MADE"], start=1)],
            "sections": [section("DECISION", {"decision_outcome": outcome}), section("ACTION"), section("VERIFICATION")]}


def recons() -> Json:
    return {"BEN-TMO-0001": {"initial": recon06(), "after_recovery": recon06(), "after_replay": recon06()}, "BEN-CLEAN-0001": {"initial": recon01()}}


def wrap(kind: str, value: Any) -> Any:
    """A355/A444: the two files the runner writes for the package carry a stamp beside their content; a value already holding a stamp is used as given."""
    return value if isinstance(value, dict) and "stamp" in value else {"stamp": dict(STAMP), kind: value}


def art(**over: Any) -> Json:
    base: Json = {"check_runs": check_runs(), "leak_scan": LEAK_OK, "probes": probes(), "reconstructions": recons(), "mock_case_writes": {"BEN-TMO-0001": 1, "BEN-CLEAN-0001": 0},
                  "correlations": {**{c: corr_of(c) for c in SIX}, **CORRELATIONS}, "run_stamp": dict(STAMP), "artifact_files": FILES_OK, "scan_inventory": FILES_OK}
    base.update(over)
    base["reconstructions"] = wrap("cases", base["reconstructions"])
    base["mock_case_writes"] = wrap("writes", base["mock_case_writes"])
    return base


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
        for key in ("2", "5", "8", "9"):
            del i["rows"][key]
        text = make(i)
        for n in (2, 5, 8, 9):
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
            ("no probe line", STAMP_LINE + "leakscan OK: scanned 12 files, 340 lines\n"), ("no clean line", STAMP_LINE + f"probe: every planted kind was found ({KINDS}): the scanner can see\n"),
            ("a kind missing", LEAK_OK.replace(", bearer_token", "")), ("a finding", LEAK_OK + "LEAK SCAN FAILED: findings in the artifacts\n"),
            ("a failed scan", LEAK_OK + "leakscan FAIL: a.log:3: jwt (length 40)\n"), ("not run", LEAK_OK + "NOT RUN: the scan itself failed or looked at nothing (exit 2)\n"),
            ("refused", LEAK_OK + "refused: the scanner printed the canary\n"), ("zero files", LEAK_OK.replace("12 files", "0 files")), ("zero lines", LEAK_OK.replace("340 lines", "0 lines")),
            ("empty", ""), ("not text", None), ("a number", 7), ("only the stamp", STAMP_LINE)]
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


class RowTwo(unittest.TestCase):
    """A375 (A1): row 2 is computed. US-06: one GRANT_SPENT, one DISPATCH_ATTEMPTED, one RECEIPT classified OUTCOME_UNKNOWN, then a key-verified ACHIEVED, the same counts after a recovery sweep and
    after a replay, and one write in the mock-case log. US-01: ALLOW with zero grants, no GRANT_* event, no DISPATCH_ATTEMPTED and zero writes. A grant on US-01 fails the row."""

    def with_(self, **change: Any) -> str:
        r = recons()
        r.update(change.pop("recons", {}))
        writes = {"BEN-TMO-0001": 1, "BEN-CLEAN-0001": 0, **change.pop("writes", {})}
        return row(make(artifacts=art(reconstructions=r, mock_case_writes=writes)), 2)

    def test_the_clean_shape_is_met(self) -> None:
        self.assertIn("**MET**", row(make(), 2))
        self.assertIn("sha256:", row(make(), 2))

    def test_us_06_needs_exactly_one_spend_one_dispatch_and_one_receipt(self) -> None:
        for name, kinds in (("two spends", [*US06_EVENTS, "GRANT_SPENT"]), ("two dispatches", [*US06_EVENTS, "DISPATCH_ATTEMPTED"]), ("two receipts", [*US06_EVENTS, "RECEIPT"]),
                            ("no spend", [k for k in US06_EVENTS if k != "GRANT_SPENT"]), ("no dispatch", [k for k in US06_EVENTS if k != "DISPATCH_ATTEMPTED"]),
                            ("no receipt", [k for k in US06_EVENTS if k != "RECEIPT"]), ("no key read", [k for k in US06_EVENTS if k != "VERIFICATION_READ"]),
                            ("no verdict", [k for k in US06_EVENTS if k != "VERDICT"]), ("two verdicts", [*US06_EVENTS, "VERDICT"])):
            text = self.with_(recons={"BEN-TMO-0001": {"initial": recon06(kinds), "after_recovery": recon06(), "after_replay": recon06()}})
            self.assertIn("**NOT MET**", text, name)

    def test_us_06_needs_the_order_spend_then_dispatch_then_receipt_then_key_read_then_verdict(self) -> None:
        swapped = ["DECISION_MADE", "GRANT_ISSUED", "DISPATCH_ATTEMPTED", "GRANT_SPENT", "RECEIPT", "VERIFICATION_READ", "VERDICT"]
        verdict_first = ["DECISION_MADE", "GRANT_ISSUED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "VERDICT", "VERIFICATION_READ"]
        for kinds in (swapped, verdict_first):
            self.assertIn("**NOT MET**", self.with_(recons={"BEN-TMO-0001": {"initial": recon06(kinds), "after_recovery": recon06(), "after_replay": recon06()}}))

    def test_us_06_needs_the_receipt_classified_unknown_and_the_verdict_achieved(self) -> None:
        kwargs: Any
        for kwargs in ({"state": "ACKNOWLEDGED"}, {"state": "FAILED_TERMINAL"}, {"verdict": "UNKNOWN"}, {"verdict": "NOT_ACHIEVED"}, {"verdict": "PENDING_EXTERNAL"}):
            text = self.with_(recons={"BEN-TMO-0001": {"initial": recon06(None, **kwargs), "after_recovery": recon06(), "after_replay": recon06()}})
            self.assertIn("**NOT MET**", text, str(kwargs))

    def test_the_counts_must_be_the_same_after_a_recovery_sweep_and_after_a_replay(self) -> None:
        second = [*US06_EVENTS, "GRANT_SPENT"]
        for phase in ("after_recovery", "after_replay"):
            r = {"initial": recon06(), "after_recovery": recon06(), "after_replay": recon06(), phase: recon06(second)}
            self.assertIn("**NOT MET**", self.with_(recons={"BEN-TMO-0001": r}), phase)
        for missing in ("after_recovery", "after_replay"):
            r = {"initial": recon06(), "after_recovery": recon06(), "after_replay": recon06()}
            del r[missing]
            self.assertIn("**NOT MET**", self.with_(recons={"BEN-TMO-0001": r}), missing)

    def test_a_duplicate_is_refused_in_every_phase_not_only_by_a_difference_between_phases(self) -> None:
        for extra in ("GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "VERDICT"):
            doubled = recon06([*US06_EVENTS, extra])
            self.assertIn("**NOT MET**", self.with_(recons={"BEN-TMO-0001": {"initial": doubled, "after_recovery": doubled, "after_replay": doubled}}), extra)

    def test_phases_that_are_each_valid_but_differ_in_the_number_of_key_reads_are_not_the_same_counts(self) -> None:
        more_reads = ["DECISION_MADE", "GRANT_ISSUED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "VERIFICATION_READ", "VERIFICATION_READ", "VERDICT"]
        for phase in ("after_recovery", "after_replay"):
            r = {"initial": recon06(), "after_recovery": recon06(), "after_replay": recon06(), phase: recon06(more_reads)}
            self.assertIn("**NOT MET**", self.with_(recons={"BEN-TMO-0001": r}), phase)

    def test_a_boolean_seq_is_not_a_sequence_number(self) -> None:
        bad = recon06()
        bad["events"][0]["seq"] = True
        self.assertIn("**NOT MET**", self.with_(recons={"BEN-TMO-0001": {"initial": bad, "after_recovery": recon06(), "after_replay": recon06()}}))

    def test_the_mock_case_log_must_show_exactly_one_write_for_us_06(self) -> None:
        for count in (0, 2, True, "1", None):
            self.assertIn("**NOT MET**", self.with_(writes={"BEN-TMO-0001": count}), str(count))
        r = {"BEN-CLEAN-0001": 0}
        self.assertIn("**NOT MET**", row(make(artifacts=art(mock_case_writes=r)), 2), "no entry for US-06")

    def test_us_01_needs_allow_with_zero_grants_no_dispatch_and_zero_writes(self) -> None:
        for name, kinds in (("a GRANT_ISSUED", ["DECISION_MADE", "GRANT_ISSUED"]), ("a GRANT_SPENT", ["DECISION_MADE", "GRANT_SPENT"]), ("a GRANT_VOIDED", ["DECISION_MADE", "GRANT_VOIDED"]),
                            ("a dispatch", ["DECISION_MADE", "DISPATCH_ATTEMPTED"])):
            self.assertIn("**NOT MET**", self.with_(recons={"BEN-CLEAN-0001": {"initial": recon01(kinds)}}), name)
        for outcome in ("CONFIRM", "CLARIFY", "REJECT", "UNKNOWN"):
            self.assertIn("**NOT MET**", self.with_(recons={"BEN-CLEAN-0001": {"initial": recon01(outcome=outcome)}}), outcome)
        for count in (1, True, None, "0"):
            self.assertIn("**NOT MET**", self.with_(writes={"BEN-CLEAN-0001": count}), str(count))
        self.assertIn("**MET**", self.with_(recons={"BEN-CLEAN-0001": {"initial": recon01([])}}))  # no event at all is still zero grants

    def test_a_grant_on_us_01_fails_the_row_even_when_every_us_06_check_passes(self) -> None:
        text = self.with_(recons={"BEN-CLEAN-0001": {"initial": recon01(["INTENT_ADMITTED", "DECISION_MADE", "GRANT_ISSUED"])}})
        self.assertIn("**NOT MET**", text)
        self.assertIn("BEN-CLEAN-0001", text)

    def test_missing_or_unreadable_reconstructions_are_not_met_and_never_raise(self) -> None:
        bad: Any
        for bad in (None, "x", [], {}, {"BEN-TMO-0001": None, "BEN-CLEAN-0001": {"initial": recon01()}}, {"BEN-TMO-0001": {"initial": {"events": "x", "sections": []}}, "BEN-CLEAN-0001": {"initial": recon01()}},
                    {"BEN-TMO-0001": {"initial": {"events": [{"event_type": "X"}], "sections": []}}, "BEN-CLEAN-0001": {"initial": recon01()}}):
            self.assertIn("**NOT MET**", row(make(artifacts=art(reconstructions=bad)), 2), str(bad)[:40])
        for bad in (None, "x", [], {"BEN-TMO-0001": 1}):
            self.assertIn("**NOT MET**", row(make(artifacts=art(mock_case_writes=bad)), 2), str(bad))

    def test_the_us_01_cases_are_required_input_disjoint_from_the_us_06_cases_and_inside_the_g1_cases(self) -> None:
        mutations: list[Callable[[Json], Any]] = [lambda i: i.pop("us01_cases"), lambda i: i.update(us01_cases=[]), lambda i: i.update(us01_cases=["BEN-TMO-0001"]), lambda i: i.update(us01_cases=["BEN-NOT-IN-G1"])]
        for mutate in mutations:
            i = inputs()
            mutate(i)
            with self.assertRaises(PackageError):
                make(i)


class RowTwoBinding(unittest.TestCase):
    """Codex P2s on noema#717 at 3776f387: the 'no second grant' rule must hold in every US-06 phase, and the reconstructions must be the ones of THIS run's case (tenant and correlation id from the
    run's own correlations.json), the same in every phase; otherwise a replayed or cross-case document with equal counts satisfies the row."""

    def row2(self, **art_over: Any) -> str:
        return row(make(artifacts=art(**art_over)), 2)

    def phases(self, doc: Json) -> Json:
        return {"BEN-TMO-0001": {"initial": doc, "after_recovery": doc, "after_replay": doc}, "BEN-CLEAN-0001": {"initial": recon01()}}

    def test_a_second_grant_issued_is_not_met_when_it_is_consistent_in_every_phase(self) -> None:
        second = recon06([*US06_EVENTS, "GRANT_ISSUED"])
        self.assertIn("**NOT MET**", self.row2(reconstructions=self.phases(second)))
        self.assertIn("GRANT_ISSUED", self.row2(reconstructions=self.phases(second)))

    def test_a_us_06_case_with_no_grant_issued_at_all_is_not_met(self) -> None:
        none = recon06([k for k in US06_EVENTS if k != "GRANT_ISSUED"])
        self.assertIn("**NOT MET**", self.row2(reconstructions=self.phases(none)))

    def test_a_phase_of_another_correlation_or_tenant_is_not_met(self) -> None:
        for name, bad in (("another correlation", recon06(corr="corr_other")), ("another tenant", recon06(tenant="ten_other"))):
            for phase in ("initial", "after_recovery", "after_replay"):
                r = self.phases(recon06())
                r["BEN-TMO-0001"][phase] = bad
                self.assertIn("**NOT MET**", self.row2(reconstructions=r), f"{name} in {phase}")
            self.assertIn("**NOT MET**", self.row2(reconstructions=self.phases(bad)), f"{name} in every phase")

    def test_a_missing_or_unreadable_identity_is_not_met(self) -> None:
        for field in ("tenant_id", "correlation_id"):
            for value in (None, "", 7):
                doc = recon06()
                doc[field] = value
                self.assertIn("**NOT MET**", self.row2(reconstructions=self.phases(doc)), f"{field}={value!r}")
            doc = recon06()
            del doc[field]
            self.assertIn("**NOT MET**", self.row2(reconstructions=self.phases(doc)), f"no {field}")

    def test_a_cross_case_document_does_not_satisfy_the_row(self) -> None:
        r = self.phases(recon06())
        r["BEN-CLEAN-0001"] = {"initial": recon06()}  # a US-06 document offered for the US-01 case: right shape, wrong correlation
        self.assertIn("**NOT MET**", self.row2(reconstructions=r))
        swapped = self.phases(recon06(corr="corr_clean"))
        self.assertIn("**NOT MET**", self.row2(reconstructions=swapped))

    def test_the_us_01_reconstruction_is_bound_to_its_correlation_and_the_tenant(self) -> None:
        for bad in (recon01(corr="corr_other"), recon01(tenant="ten_other")):
            r = self.phases(recon06())
            r["BEN-CLEAN-0001"] = {"initial": bad}
            self.assertIn("**NOT MET**", self.row2(reconstructions=r))

    def test_the_run_correlations_are_required_and_must_name_every_us_06_and_us_01_case(self) -> None:
        bad: Any
        for bad in (None, "x", [], {}, {"BEN-TMO-0001": "corr_tmo"}, {"BEN-CLEAN-0001": "corr_clean"}, {"BEN-TMO-0001": "", "BEN-CLEAN-0001": "corr_clean"}, {"BEN-TMO-0001": 7, "BEN-CLEAN-0001": "corr_clean"}):
            self.assertIn("**NOT MET**", self.row2(correlations=bad), str(bad))

    def test_the_tenant_is_required_input(self) -> None:
        mutations: list[Callable[[Json], Any]] = [lambda i: i.pop("tenant_id"), lambda i: i.update(tenant_id=""), lambda i: i.update(tenant_id=7)]
        for mutate in mutations:
            i = inputs()
            mutate(i)
            with self.assertRaises(PackageError):
                make(i)


class Stamps(unittest.TestCase):
    """A444 (A-order of 20 Nov) and Codex's P2s on noema#717 at 9e4cc472: the dispatch SHA and the run id are stamped into every artifact, and the package refuses an artifact of another run;
    the probe document is bound to the run's correlations (an earlier run's passing probes no longer pass row 9)."""

    def test_every_artifact_must_carry_the_stamp_of_this_dispatch_and_this_run(self) -> None:
        for kind, rows in (("leak_scan", (8,)), ("probes", (9,)), ("reconstructions", (2,)), ("mock_case_writes", (2,)), ("run_stamp", (2, 9))):
            for name, stamp in (("another sha", {"sha": "b" * 40, "run_id": RUN}), ("another run", {"sha": SHA, "run_id": "g1-other"}), ("no stamp", None), ("a string", "x"), ("extra key", {"sha": SHA, "run_id": RUN, "x": 1})):
                a = art()
                if kind == "leak_scan":
                    body = LEAK_OK.split("\n", 1)[1]
                    if stamp is None:
                        a["leak_scan"] = body
                    elif isinstance(stamp, dict) and set(stamp) == {"sha", "run_id"}:
                        a["leak_scan"] = f"stamp: {stamp['sha']} {stamp['run_id']}\n" + body
                    else:
                        a["leak_scan"] = "stamp: x\n" + body  # a string or an extra key is not a valid stamp line
                elif kind == "run_stamp":
                    a["run_stamp"] = stamp
                else:
                    doc = dict(a[kind])
                    if stamp is None:
                        del doc["stamp"]
                    else:
                        doc["stamp"] = stamp
                    a[kind] = doc
                text = make(artifacts=a)
                for n in rows:
                    self.assertIn("**NOT MET**", row(text, n), f"{kind}: {name} (row {n})")

    def test_a_stamped_file_without_its_content_is_not_met_and_never_raises(self) -> None:
        for kind in ("reconstructions", "mock_case_writes"):
            self.assertIn("**NOT MET**", row(make(artifacts=art(**{kind: {"stamp": dict(STAMP)}})), 2), kind)
            self.assertIn("**NOT MET**", row(make(artifacts=art(**{kind: {"stamp": dict(STAMP), "other": {}}})), 2), kind)

    def test_the_stamped_run_is_met_and_the_reason_names_the_stamp(self) -> None:
        self.assertIn("**MET**", row(make(), 8))
        other = make(artifacts=art(probes={**probes(), "stamp": {"sha": SHA, "run_id": "g1-other"}}))
        self.assertIn("stamp", row(other, 9))

    def test_a_probe_document_of_another_run_is_refused_by_its_correlations(self) -> None:
        """Codex P2 at 9e4cc472, line 180: replacing every probe correlation with an unrelated run's still produced G1: MET."""
        doc = probes()
        for r in doc["cases"]:
            r["correlation_id"] = "corr_earlier_run"
        self.assertIn("**NOT MET**", row(make(artifacts=art(probes=doc)), 9))
        one = probes()
        one["cases"][2]["correlation_id"] = "corr_earlier_run"
        self.assertIn("**NOT MET**", row(make(artifacts=art(probes=one)), 9))

    def test_the_run_correlations_must_name_every_probed_expected_case(self) -> None:
        for bad in ({"BEN-TMO-0001": "corr_tmo", "BEN-CLEAN-0001": "corr_clean"}, {}):
            self.assertIn("**NOT MET**", row(make(artifacts=art(correlations=bad)), 9), str(bad))

    def test_the_reconstruction_documents_are_bound_to_the_stamped_run_and_its_correlations(self) -> None:
        """Codex P2 at 9e4cc472, line 273: documents of an earlier run, or three phases of different correlations, must not pass."""
        r = recons()
        for phase in ("initial", "after_recovery", "after_replay"):
            bad = recons()
            bad["BEN-TMO-0001"][phase] = recon06(corr="corr_earlier_run", tenant="ten_other")
            self.assertIn("**NOT MET**", row(make(artifacts=art(reconstructions=bad)), 2), phase)
        self.assertIn("**MET**", row(make(artifacts=art(reconstructions=r)), 2))

    def test_the_stamp_inputs_are_the_dispatch_sha_and_run_id_of_the_package_inputs(self) -> None:
        i = inputs()
        i["run_id"] = "g1-other"
        text = make(i)
        for n in (2, 8, 9):
            self.assertIn("**NOT MET**", row(text, n), n)


class RereadNotReplay(unittest.TestCase):
    """A476 (rulings of 20 Nov): `after_replay` is a SECOND READ of the same audit-only route, a re-read and not a replay; its document must equal the first read's, and the package never claims deterministic replay (INV-21) from this evidence."""

    def test_a_second_read_that_differs_from_the_first_is_not_met_even_with_equal_counts(self) -> None:
        changed = recon06()
        changed["sections"][0]["items"][0]["facts"] = {"decision_outcome": "ALLOW", "reason_codes": ["X"]}
        phases = {"initial": recon06(), "after_recovery": recon06(), "after_replay": changed}
        text = make(artifacts=art(reconstructions={"BEN-TMO-0001": phases, "BEN-CLEAN-0001": {"initial": recon01()}}))
        self.assertIn("**NOT MET**", row(text, 2))
        self.assertIn("re-read", row(text, 2))

    def test_key_order_is_not_a_difference(self) -> None:
        reordered = {k: recon06()[k] for k in reversed(list(recon06()))}
        phases = {"initial": recon06(), "after_recovery": recon06(), "after_replay": reordered}
        self.assertIn("**MET**", row(make(artifacts=art(reconstructions={"BEN-TMO-0001": phases, "BEN-CLEAN-0001": {"initial": recon01()}})), 2))

    def test_the_package_text_says_after_replay_means_a_re_read_and_claims_no_deterministic_replay(self) -> None:
        text = make()
        line = next(x for x in text.splitlines() if "after_replay" in x)
        self.assertIn("re-read", line)
        self.assertIn("not a replay", line)
        self.assertIn("INV-21", line)
        self.assertIn("not claimed", line)


class RowTwelve(unittest.TestCase):
    """Row 12 (No secret or key in any artifact or summary) is computed from the run's artifact file list, the scanner's own stamped inventory of the files it scanned, and the leak scan, not attested:
    the list is stamped, no listed file is named like a secret or a key, the leak scan is clean, and the uploaded set and the scanned set are the SAME set of names (Codex P2 on #875: a count
    cannot tell a scanned file from an unscanned one)."""

    def row12(self, **over: Any) -> str:
        """A test of one rule is never failed by another: the inventory follows the list unless the test gives its own, and the scan's file count follows the inventory."""
        files = over.get("artifact_files", FILES_OK)
        inventory = over.setdefault("scan_inventory", files)
        if "leak_scan" not in over and isinstance(inventory, str):
            n = max(inventory.count("\n") - 1, 1)
            over["leak_scan"] = STAMP_LINE + f"probe: every planted kind was found ({KINDS}): the scanner can see\nleakscan OK: scanned {n} files, 340 lines\n"
        return row(make(artifacts=art(**over)), 12)

    def test_a_stamped_list_of_plain_files_and_a_clean_scan_that_covered_them_is_met(self) -> None:
        self.assertIn("**MET**", self.row12())

    def test_no_file_list_is_not_met(self) -> None:
        base = art()
        del base["artifact_files"]
        self.assertIn("**NOT MET**", row(make(artifacts=base), 12))
        for text in ("", "\n", STAMP_LINE, 7, None):
            self.assertIn("**NOT MET**", self.row12(artifact_files=text), repr(text))

    def test_a_list_for_another_dispatch_or_run_or_without_a_stamp_line_is_not_met(self) -> None:
        for first in (f"stamp: {'b' * 40} {RUN}\n", f"stamp: {SHA} other-run\n", "requests.jsonl\n"):
            self.assertIn("**NOT MET**", self.row12(artifact_files=first + "requests.jsonl\n"), first)

    def test_a_file_named_like_a_secret_or_a_key_is_not_met(self) -> None:
        for name in ("idp.key", "server.pem", "id_ed25519", "bundle.p12", ".env", "tokens/usr_synth009", "credentials.json", "audit.token", "app-secret.txt", "dir/private.key", "key.pfx"):
            self.assertIn("**NOT MET**", self.row12(artifact_files=FILES_OK + name + "\n"), name)

    def test_an_absolute_or_escaping_path_is_not_met(self) -> None:
        for name in ("/etc/hosts", "../outside.txt", "a/../../b.txt", "C:/x.txt"):
            self.assertIn("**NOT MET**", self.row12(artifact_files=FILES_OK + name + "\n"), name)

    def test_the_uploaded_set_and_the_scanned_set_must_be_the_same_set(self) -> None:
        # Codex P2 on #875: a scan of 13 files said OK for 12 listed names, though one listed name was never scanned. The inventory is the scanner's own list of what it read.
        names = FILES_OK.split("\n")[1:-1]
        unscanned = self.row12(artifact_files=FILES_OK + "extra-upload.json\n", scan_inventory=FILES_OK, leak_scan=LEAK_BIG)
        self.assertIn("**NOT MET**", unscanned)
        self.assertIn("extra-upload.json", unscanned, "the file uploaded but not scanned is named")
        uploaded_less = self.row12(artifact_files=STAMP_LINE + "\n".join(names[:-1]) + "\n", scan_inventory=FILES_OK, leak_scan=LEAK_BIG)
        self.assertIn("**NOT MET**", uploaded_less)
        self.assertIn(names[-1], uploaded_less, "the file scanned but not uploaded is named too")
        same_count_other_name = STAMP_LINE + "\n".join(names[:-1] + ["other.json"]) + "\n"
        self.assertIn("**NOT MET**", self.row12(artifact_files=FILES_OK, scan_inventory=same_count_other_name))

    def test_the_order_of_the_names_does_not_matter_but_the_set_does(self) -> None:
        names = FILES_OK.split("\n")[1:-1]
        shuffled = STAMP_LINE + "\n".join(reversed(names)) + "\n"
        self.assertIn("**MET**", self.row12(artifact_files=FILES_OK, scan_inventory=shuffled))

    def test_no_inventory_or_an_unstamped_or_foreign_one_is_not_met(self) -> None:
        base = art()
        del base["scan_inventory"]
        self.assertIn("**NOT MET**", row(make(artifacts=base), 12))
        for bad in ("", "\n", STAMP_LINE, 7, None, "\n".join(FILES_OK.split("\n")[1:]), f"stamp: {'b' * 40} {RUN}\n" + "\n".join(FILES_OK.split("\n")[1:]), STAMP_LINE + FILES_OK.split("\n", 1)[1] + "\n"):
            self.assertIn("**NOT MET**", self.row12(scan_inventory=bad, leak_scan=LEAK_BIG), repr(bad))

    def test_the_scans_own_file_count_must_equal_the_inventory(self) -> None:
        listed = FILES_OK.count("\n") - 1
        for n in (listed - 1, listed + 1):
            scan = STAMP_LINE + f"probe: every planted kind was found ({KINDS}): the scanner can see\nleakscan OK: scanned {n} files, 340 lines\n"
            self.assertIn("**NOT MET**", self.row12(leak_scan=scan), n)
        scan = STAMP_LINE + f"probe: every planted kind was found ({KINDS}): the scanner can see\nleakscan OK: scanned {listed} files, 340 lines\n"
        self.assertIn("**MET**", self.row12(leak_scan=scan))

    def test_a_failing_leak_scan_is_not_met_for_row_12_too(self) -> None:
        self.assertIn("**NOT MET**", self.row12(leak_scan=STAMP_LINE + "leakscan FAIL: 1 finding\n"))
        self.assertIn("**NOT MET**", self.row12(leak_scan=LEAK_BIG + "leakscan FAIL: 1 finding\n"))  # a failure line beside an OK line is still a failure

    def test_a_list_with_a_blank_or_repeated_line_is_not_met(self) -> None:
        self.assertIn("**NOT MET**", self.row12(artifact_files=FILES_OK + "\nextra.txt\n"))
        self.assertIn("**NOT MET**", self.row12(artifact_files=FILES_OK + "requests.jsonl\n"))

    def test_the_row_no_longer_takes_an_attested_link(self) -> None:
        i = inputs()
        i["rows"] = {**i["rows"], "12": {"link": "https://x.example/1", "read_by_bo": True}}
        base = art()
        del base["artifact_files"]
        self.assertIn("**NOT MET**", row(make(i, artifacts=base), 12))


class StampAcrossArtifacts(unittest.TestCase):
    """A517 (#717): the dispatch SHA and the run id are compared ACROSS the artifacts, not only each against the inputs: the reconstructions, the mock-case write counts, the probe results, the run's stamp
    file, the leak-scan output and the artifact file list must all carry one and the same stamp, or the package says which differ."""

    OTHER = {"sha": "b" * 40, "run_id": RUN}

    def line(self, text: str) -> str:
        return next(x for x in text.splitlines() if x.startswith("Run stamp across artifacts"))

    def test_one_stamp_on_all_seven_artifacts_agrees(self) -> None:
        text = make()
        self.assertIn("AGREE", self.line(text))
        self.assertNotIn("DISAGREE", self.line(text))
        self.assertIn("G1: MET", text)

    def test_each_artifact_with_another_stamp_is_named_and_the_package_is_not_met(self) -> None:
        other_line = f"stamp: {'b' * 40} {RUN}\n"
        cases: dict[str, Json] = {"reconstructions": {"reconstructions": {"stamp": self.OTHER, "cases": recons()}},
                 "mock_case_writes": {"mock_case_writes": {"stamp": self.OTHER, "writes": {"BEN-TMO-0001": 1, "BEN-CLEAN-0001": 0}}},
                 "probes": {"probes": {**probes(), "stamp": self.OTHER}},
                 "run_stamp": {"run_stamp": self.OTHER},
                 "leak_scan": {"leak_scan": other_line + LEAK_OK.split("\n", 1)[1]},
                 "artifact_files": {"artifact_files": other_line + FILES_OK.split("\n", 1)[1]},
                 "scan_inventory": {"scan_inventory": other_line + FILES_OK.split("\n", 1)[1]}}
        for name, over in cases.items():
            text = make(artifacts=art(**over))
            self.assertIn("DISAGREE", self.line(text), name)
            self.assertIn(name, self.line(text), name)
            self.assertIn("G1: NOT MET", text, name)

    def test_an_artifact_with_no_stamp_is_named_too(self) -> None:
        base = art()
        del base["artifact_files"]
        text = make(artifacts=base)
        self.assertIn("DISAGREE", self.line(text))
        self.assertIn("artifact_files", self.line(text))

    def test_the_stamp_is_compared_to_the_inputs_as_well(self) -> None:
        i = inputs()
        i["run_id"] = "another-run"
        self.assertIn("DISAGREE", self.line(make(i)))


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
