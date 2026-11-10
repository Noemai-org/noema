"""BEN-16, benchmark-fixture.v2 (ruling A6; IL's review on noema#360): the cross-field rules a JSON Schema cannot state, checked by `tenant_rules.problems`. Written before it and seen failing.

Every `tenant_id` names a listed tenant; `subject_tenant` is listed; a transaction's owner and beneficiary and a case's transaction are in the same tenant as the object; a `ref` is unique in
the whole fixture, not per tenant; a leak-scan marker appears only in strings of its own tenant; an `enterprise_strings` target takes the tenant of the object it names. SYNTHETIC; generated layer only (INV-22).
"""
import copy
import json
import unittest
from pathlib import Path
from typing import Any

from benchmark.fixtures.tenant_rules import problems

ROOT = Path(__file__).resolve().parents[2] / "contracts" / "fixtures" / "valid" / "benchmark-fixture.v2"
Json = dict[str, Any]


def load(name: str) -> Json:
    out: Json = json.loads((ROOT / name).read_text(encoding="utf-8"))
    return out


def two() -> Json:
    return load("two_tenants_with_an_other_tenant_transaction.json")


class Rules(unittest.TestCase):
    def test_every_valid_fixture_passes(self) -> None:
        for path in sorted(ROOT.glob("*.json")):
            self.assertEqual([], problems(load(path.name)), path.name)

    def test_a_tenant_id_that_names_no_listed_tenant_is_refused(self) -> None:
        doc = two()
        doc["transactions"][0]["tenant_id"] = "ten_gamma"
        self.assertTrue(any("ten_gamma" in p for p in problems(doc)))

    def test_a_subject_tenant_that_is_not_listed_is_refused(self) -> None:
        doc = two()
        doc["subject_tenant"] = "ten_gamma"
        self.assertTrue(any("subject_tenant" in p for p in problems(doc)))

    def test_a_transaction_owned_by_a_principal_of_another_tenant_is_refused(self) -> None:
        doc = two()
        doc["transactions"][-1]["owner"] = doc["principals"][0]["principal_id"]  # the other-tenant transaction, owned by a ten_alpha principal
        self.assertTrue(any("owner" in p and "tenant" in p for p in problems(doc)))

    def test_a_beneficiary_or_case_transaction_in_another_tenant_is_refused(self) -> None:
        doc = two()
        doc["transactions"][-1]["beneficiary"] = doc["beneficiaries"][0]["beneficiary_id"]
        self.assertTrue(any("beneficiary" in p and "tenant" in p for p in problems(doc)))
        doc = two()
        doc["cases"][0]["transaction"] = doc["transactions"][-1]["ref"]
        self.assertTrue(any("case" in p and "tenant" in p for p in problems(doc)))

    def test_the_same_ref_in_two_tenants_is_refused(self) -> None:
        # IL: a wrong-tenant read must never be able to return a real object because two tenants share a ref.
        doc = two()
        doc["transactions"][-1]["ref"] = doc["transactions"][0]["ref"]
        self.assertTrue(any("ref" in p and "twice" in p for p in problems(doc)))

    def test_a_marker_in_another_tenant_s_narration_is_refused(self) -> None:
        doc = two()
        doc["enterprise_strings"].append({"target": doc["transactions"][-1]["ref"], "field": "narration", "text": "see " + doc["tenants"][0]["markers"][0]})
        self.assertTrue(any("marker" in p for p in problems(doc)))

    def test_a_marker_in_its_own_tenant_s_narration_is_accepted(self) -> None:
        doc = two()
        doc["enterprise_strings"].append({"target": doc["transactions"][-1]["ref"], "field": "narration", "text": "see " + doc["tenants"][1]["markers"][0]})
        self.assertEqual([], problems(doc))

    def test_a_string_planted_on_an_object_that_does_not_exist_is_refused(self) -> None:
        doc = copy.deepcopy(two())
        doc["enterprise_strings"].append({"target": "fin.transaction:NOPE", "field": "narration", "text": "x"})
        self.assertTrue(any("NOPE" in p for p in problems(doc)))

    def test_a_reference_to_an_object_the_fixture_does_not_hold_is_refused(self) -> None:
        # Codex P2 on ab7542d: a missing owner, beneficiary or case transaction passed because the membership guards skipped it.
        doc = two()
        doc["transactions"][0]["owner"] = "usr_missing"
        self.assertTrue(any("usr_missing" in p and "transaction" in p for p in problems(doc)), problems(doc))
        doc = two()
        doc["transactions"][0]["beneficiary"] = "ben_9999"
        self.assertTrue(any("ben_9999" in p for p in problems(doc)), problems(doc))
        doc = two()
        doc["cases"][0]["transaction"] = "fin.transaction:NOPE"
        self.assertTrue(any("NOPE" in p and "case" in p for p in problems(doc)), problems(doc))
        doc = two()
        doc["beneficiaries"][0]["owner"] = "usr_missing"
        self.assertTrue(any("usr_missing" in p and "beneficiary" in p for p in problems(doc)), problems(doc))

    def test_a_transaction_with_no_beneficiary_is_still_allowed(self) -> None:
        doc = two()
        doc["transactions"][0]["beneficiary"] = None
        self.assertEqual([], problems(doc))

    def test_another_tenant_s_marker_in_a_resource_reference_is_refused(self) -> None:
        # Codex P2 on ab7542d: only names and enterprise text were scanned, so a marker in a ref passed and a legitimate read could expose it as a cross-tenant leak.
        marker = two()["tenants"][1]["markers"][0]
        doc = two()
        old = doc["transactions"][0]["ref"]
        new = old + marker
        doc["transactions"][0]["ref"] = new
        for c in doc["cases"]:
            if c["transaction"] == old:
                c["transaction"] = new
        got = problems(doc)
        self.assertTrue(any(marker in p and new in p or "marker" in p and marker in p for p in got), got)

    def test_another_tenant_s_marker_in_any_string_field_of_an_object_is_refused(self) -> None:
        marker = two()["tenants"][1]["markers"][0]
        for kind in ("principals", "beneficiaries", "transactions", "cases"):
            doc = two()
            item = doc[kind][0]
            field = next(k for k, v in item.items() if isinstance(v, str) and k not in ("tenant_id", "ref", "principal_id", "beneficiary_id", "owner", "beneficiary", "transaction"))
            item[field] = item[field] + " " + marker
            self.assertTrue(any("marker" in p and marker in p for p in problems(doc)), (kind, field, problems(doc)))

    def test_a_marker_in_its_own_tenant_s_object_fields_is_accepted(self) -> None:
        doc = two()
        own = doc["tenants"][0]["markers"][0]
        doc["principals"][0]["display_name"] += " " + own
        self.assertEqual([], problems(doc))

    def test_a_marker_shared_by_two_tenants_is_refused(self) -> None:
        doc = two()
        doc["tenants"][1]["markers"] = list(doc["tenants"][0]["markers"])
        self.assertTrue(any("marker" in p for p in problems(doc)))


if __name__ == "__main__":
    unittest.main()
