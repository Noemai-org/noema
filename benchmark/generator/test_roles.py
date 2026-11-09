"""Ruling A37 (Block C, 7 Nov 2026), EDG-01 / ruling A1: the roles of the pack and of every generated case are exactly the four of the session
contract (customer, ops, approver, audit). The pack's ops_agent is ops; the cases' viewer is audit (read-only). A role outside the set is refused by the
issuer (deny by default), so a case or a pack that names one cannot run through the session provider. SYNTHETIC."""
import json
import unittest
from pathlib import Path

from benchmark.generator.generate import generate

A1_ROLES = {"customer", "ops", "approver", "audit"}
PACK = Path(__file__).resolve().parents[2] / "packs" / "fintech_v1.json"


def _pack_roles(node: object) -> set[str]:
    out: set[str] = set()
    if isinstance(node, dict):
        for key, value in node.items():
            if key == "roles_allowed" and isinstance(value, list):
                out |= {str(v) for v in value}
            else:
                out |= _pack_roles(value)
    elif isinstance(node, list):
        for item in node:
            out |= _pack_roles(item)
    return out


class RolesAreTheSessionRoles(unittest.TestCase):
    def test_every_pack_role_is_an_a1_role(self) -> None:
        roles = _pack_roles(json.loads(PACK.read_text()))
        self.assertTrue(roles, "the pack names no role at all: the walk lost them")
        self.assertEqual(roles - A1_ROLES, set(), "pack roles outside ruling A1")

    def test_every_generated_case_uses_an_a1_role(self) -> None:
        seen: set[str] = set()
        for case in generate(20261020).cases.values():
            seen |= set(case["principal"]["roles"])
        self.assertEqual(seen - A1_ROLES, set(), "case roles outside ruling A1")

    def test_the_read_only_case_runs_as_audit(self) -> None:
        """A37: viewer is audit, read-only: the case that tests a role lacking the capability still has a role that lacks it."""
        roles = [tuple(c["principal"]["roles"]) for c in generate(20261020).cases.values() if c["principal"]["roles"] != ["customer"]]
        self.assertIn(("audit",), roles)
        self.assertIn(("ops",), roles)


if __name__ == "__main__":
    unittest.main()
