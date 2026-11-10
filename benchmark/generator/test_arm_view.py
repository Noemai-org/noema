"""BEN-16, the arm view (noema#307, Security Reviewer F-14; noema#246): the projection of a generated case that an arm is shown, `benchmark-arm-case.v1`. Written before `arm_view.py` and seen failing.

F-14: a per-case `permitted_capabilities` encodes the answer. The generated cases carry a list built from what each case expects to write (several different lists over the suite); the arm view must carry
the principal's ROLE-level set instead, the same for every case of that role, and never a prohibited capability (EL on noema#341). Nothing the Security Reviewer found leaking (goal, category, case id,
expected, oracle ref) reaches the view. SYNTHETIC; generated layer only; no oracle or held-out material is read (INV-22, REQ-AGT-004)."""
import copy
import json
import re
import unittest
from collections import defaultdict
from datetime import datetime, timezone
from functools import lru_cache
from typing import Any

from benchmark.contracts import Contracts
from benchmark.fixtures.standin import CLOCK_START
from benchmark.generator.arm_view import ARM_CLOCK_START, arm_case
from benchmark.generator.generate import PACK, Suite, generate

Json = dict[str, Any]
CONTRACTS = Contracts()
SALT = "run-private-salt-for-tests"
ROLES = ("customer", "ops", "approver", "audit")
LEAKS = ("case_id", "goal", "category", "families", "suite_family", "expected", "oracle_ref", "safety_gate_relevance", "severity_if_failed", "provenance", "fault_plan", "events_timeline", "harness")


@lru_cache(maxsize=None)
def suite() -> Suite:
    return generate(20261020)


def pack() -> Json:
    out: Json = json.loads(PACK.read_text(encoding="utf-8"))
    return out


def role_map() -> dict[str, list[str]]:
    """The role-to-capability map as noema#359 derives it: `roles_allowed` of each capability, without the capabilities marked prohibited (a stand-in until that file is on main)."""
    out: dict[str, set[str]] = {r: set() for r in ROLES}
    for cap in pack()["capabilities"]:
        if not cap["prohibited"]:
            for role in cap["authority"]["roles_allowed"]:
                out[role].add(cap["capability_id"])
    return {r: sorted(v) for r, v in out.items()}


def views() -> dict[str, Json]:
    return {cid: arm_case(case, role_map(), salt=SALT) for cid, case in suite().cases.items()}


class View(unittest.TestCase):
    def test_every_generated_case_projects_to_a_valid_arm_case(self) -> None:
        for cid, view in views().items():
            self.assertEqual([], CONTRACTS.errors("benchmark-arm-case.v1", view), cid)

    def test_the_permitted_capabilities_do_not_reveal_the_expected_writes(self) -> None:
        # Security Reviewer F-14, the test SR wrote on test/SEC-06-resist-security-review: within one role the set never varies.
        by_role: dict[tuple[str, ...], set[tuple[str, ...]]] = defaultdict(set)
        for cid, view in views().items():
            by_role[tuple(view["principal"]["roles"])].add(tuple(view["permitted_capabilities"]))
        self.assertTrue(by_role)
        for roles, sets in by_role.items():
            self.assertEqual(1, len(sets), (roles, sets))

    def test_the_generated_cases_do_carry_a_per_case_list_that_varies_which_is_what_the_view_must_not_copy(self) -> None:
        own = {tuple(sorted(c["permitted_capabilities"])) for c in suite().cases.values() if c["principal"]["roles"] == ["customer"]}
        self.assertGreater(len(own), 1, "the case's own list varies by what it expects: that is the leak")
        shown = {tuple(v["permitted_capabilities"]) for v in views().values() if v["principal"]["roles"] == ["customer"]}
        self.assertEqual(1, len(shown))

    def test_the_set_is_the_roles_from_the_map_and_never_a_prohibited_capability(self) -> None:
        prohibited = {c["capability_id"] for c in pack()["capabilities"] if c["prohibited"]}
        self.assertTrue(prohibited)
        for cid, view in views().items():
            roles = view["principal"]["roles"]
            expected = sorted({cap for r in roles for cap in role_map()[r]})
            self.assertEqual(expected, view["permitted_capabilities"], cid)
            self.assertEqual(set(), prohibited & set(view["permitted_capabilities"]), cid)

    def test_the_audit_and_approver_roles_are_offered_nothing(self) -> None:
        for view in views().values():
            if view["principal"]["roles"] in (["audit"], ["approver"]):
                self.assertEqual([], view["permitted_capabilities"])

    def test_nothing_the_security_reviewer_found_leaking_reaches_the_view(self) -> None:
        for cid, view in views().items():
            self.assertEqual(set(), set(LEAKS) & set(view), cid)
            text = json.dumps(view)
            self.assertNotIn(cid, text)
            self.assertIsNone(re.search(r"BEN-[A-Z0-9]+-\d+", text), cid)

    def test_refs_are_opaque_salted_and_distinct(self) -> None:
        a = {cid: v["case_ref"] for cid, v in views().items()}
        self.assertEqual(len(a), len(set(a.values())))
        other = {cid: arm_case(c, role_map(), salt="another-salt")["case_ref"] for cid, c in suite().cases.items()}
        self.assertTrue(all(a[cid] != other[cid] for cid in a))
        for cid, ref in a.items():
            self.assertNotIn(cid.split("-")[1].lower(), ref)

    def test_the_same_case_and_salt_give_the_same_view(self) -> None:
        cid, case = next(iter(suite().cases.items()))
        self.assertEqual(arm_case(case, role_map(), salt=SALT), arm_case(copy.deepcopy(case), role_map(), salt=SALT))
        self.assertEqual(views()[cid], arm_case(case, role_map(), salt=SALT))

    def test_the_view_does_not_change_the_case_and_carries_the_utterances_and_channel_as_they_are(self) -> None:
        cid, case = next(iter(suite().cases.items()))
        before = copy.deepcopy(case)
        view = arm_case(case, role_map(), salt=SALT)
        self.assertEqual(before, case)
        self.assertEqual(case["utterances"], view["utterances"])
        self.assertEqual(case["channel"], view["channel"])
        self.assertEqual(case["principal"]["roles"], view["principal"]["roles"])

    def test_time_is_the_simulated_clock_start_in_utc(self) -> None:
        self.assertEqual(datetime.fromisoformat(CLOCK_START).astimezone(timezone.utc), datetime.fromisoformat(ARM_CLOCK_START.replace("Z", "+00:00")))
        for view in views().values():
            self.assertEqual(ARM_CLOCK_START, view["received_at"])

    def test_a_role_the_map_does_not_hold_or_no_role_or_no_salt_fails_closed(self) -> None:
        _, case = next(iter(suite().cases.items()))
        bad = copy.deepcopy(case)
        bad["principal"]["roles"] = ["superuser"]
        with self.assertRaises(ValueError):
            arm_case(bad, role_map(), salt=SALT)
        bad["principal"]["roles"] = []
        with self.assertRaises(ValueError):
            arm_case(bad, role_map(), salt=SALT)
        with self.assertRaises(ValueError):
            arm_case(case, role_map(), salt="")
        with self.assertRaises(ValueError):
            arm_case(case, {}, salt=SALT)

    def test_a_map_that_offers_a_capability_twice_is_refused(self) -> None:
        _, case = next(iter(suite().cases.items()))
        doubled = {r: [*caps, *caps[:1]] for r, caps in role_map().items()}
        with self.assertRaises(ValueError):
            arm_case(case, doubled, salt=SALT)


if __name__ == "__main__":
    unittest.main()
