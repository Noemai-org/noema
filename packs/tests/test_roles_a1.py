"""EDG-01 / ruling A1 and A37: every role named in a pack is one of the four roles the identity layer issues.

A1: a token carries exactly one of customer, ops, approver, audit. A37 maps the older pack names: ops_agent -> ops, viewer -> audit (read-only).
A role the issuer cannot issue would make a benchmark case unrunnable (the issuer refuses it, deny by default) or a profile unreachable.
Run: python -m unittest discover -s packs/tests
"""
import json
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
A1_ROLES = {"customer", "ops", "approver", "audit"}


def roles_in(node):
    """Every string in every roles_allowed list of a pack document."""
    if isinstance(node, dict):
        for key, value in node.items():
            if key == "roles_allowed":
                yield from value
            else:
                yield from roles_in(value)
    elif isinstance(node, list):
        for item in node:
            yield from roles_in(item)


class PackRolesAreA1Roles(unittest.TestCase):
    def test_every_pack_names_only_roles_the_issuer_can_issue(self):
        packs = sorted((ROOT / "packs").glob("*.json")) + sorted((ROOT / "packs" / "conformance").glob("*.json"))
        self.assertTrue(packs)
        for path in packs:
            found = set(roles_in(json.loads(path.read_text())))
            self.assertLessEqual(found, A1_ROLES, f"{path.name} names roles outside A1: {sorted(found - A1_ROLES)}")

    def test_the_benchmark_generator_names_only_roles_the_issuer_can_issue(self):
        text = (ROOT / "benchmark" / "generator" / "lib_security.py").read_text()
        found = {r for group in re.findall(r"roles=\(([^)]*)\)", text) for r in re.findall(r'"([a-z_]+)"', group)}
        self.assertTrue(found)
        self.assertLessEqual(found, A1_ROLES, f"lib_security.py names roles outside A1: {sorted(found - A1_ROLES)}")


if __name__ == "__main__":
    unittest.main()
