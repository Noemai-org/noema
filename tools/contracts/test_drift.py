"""The drift guard must fail on every kind of difference (money-and-value-lists §2.4).

Each test mutates a copy in memory, never the files, and requires check_lists or check_money to report it.
"""
import copy
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import drift  # noqa: E402
from validate import load_schemas  # noqa: E402

SCHEMAS = load_schemas()
ENUMS = SCHEMAS["enums.v1.json"]
REGISTRY = drift.REGISTRY.read_text(encoding="utf-8")


class DriftGuard(unittest.TestCase):
    def test_clean_tree_passes(self):
        self.assertEqual(drift.check_lists(ENUMS, REGISTRY), [])
        self.assertEqual(drift.check_money(SCHEMAS, REGISTRY), [])

    def test_record_event_types_equal_perd_32_2(self):
        self.assertEqual(drift.check_record_event_types(SCHEMAS), [])

    def test_record_event_type_added_to_the_schema(self):
        changed = copy.deepcopy(SCHEMAS)
        changed["record-event.v1.json"]["$defs"]["RecordEventType"]["enum"].append("RECORD_ACCESSED")
        self.assertTrue(any("RECORD_ACCESSED" in p for p in drift.check_record_event_types(changed)))

    def test_record_event_type_removed_from_the_schema(self):
        changed = copy.deepcopy(SCHEMAS)
        changed["record-event.v1.json"]["$defs"]["RecordEventType"]["enum"].remove("CONFIG_CHANGED")
        self.assertTrue(any("CONFIG_CHANGED" in p for p in drift.check_record_event_types(changed)))

    def test_record_event_types_added_to_or_removed_from_the_v2_schema(self):
        added = copy.deepcopy(SCHEMAS)
        added["record-event.v2.json"]["$defs"]["RecordEventType"]["enum"].append("RECORD_ACCESSED")
        self.assertTrue(any("record-event.v2.json" in p and "RECORD_ACCESSED" in p for p in drift.check_record_event_types(added)))
        removed = copy.deepcopy(SCHEMAS)
        removed["record-event.v2.json"]["$defs"]["RecordEventType"]["enum"].remove("CONFIG_CHANGED")
        self.assertTrue(any("record-event.v2.json" in p and "CONFIG_CHANGED" in p for p in drift.check_record_event_types(removed)))

    def test_grant_void_reasons_of_v2_and_v3_must_be_the_nine_of_perd_27_1(self):
        for mutate in (lambda e: e.remove("EXPIRED"), lambda e: e.append("SOMETHING_ELSE"), lambda e: e.append("EXPIRED")):
            for name in ("record-event.v2.json", "record-event.v3.json"):
                changed = copy.deepcopy(SCHEMAS)
                mutate(changed[name]["$defs"]["GrantVoidReason"]["enum"])
                self.assertTrue(any(name in p and "GrantVoidReason" in p for p in drift.check_record_event_types(changed)), name)

    def test_record_event_types_added_to_or_removed_from_the_v3_schema(self):
        added = copy.deepcopy(SCHEMAS)
        added["record-event.v3.json"]["$defs"]["RecordEventType"]["enum"].append("RECORD_ACCESSED")
        self.assertTrue(any("record-event.v3.json" in p and "RECORD_ACCESSED" in p for p in drift.check_record_event_types(added)))
        removed = copy.deepcopy(SCHEMAS)
        removed["record-event.v3.json"]["$defs"]["RecordEventType"]["enum"].remove("CONFIG_CHANGED")
        self.assertTrue(any("record-event.v3.json" in p and "CONFIG_CHANGED" in p for p in drift.check_record_event_types(removed)))

    def test_value_added_to_the_registry(self):
        mutated = REGISTRY.replace("`ALLOW`, `CLARIFY`", "`ALLOW`, `PROCEED`, `CLARIFY`", 1)
        self.assertNotEqual(mutated, REGISTRY)
        self.assertTrue(any("DecisionOutcome" in p for p in drift.check_lists(ENUMS, mutated)))

    def test_value_removed_from_the_registry(self):
        mutated = REGISTRY.replace("`EXPIRED`", "`NONE_SUCH`", 1)
        self.assertTrue(any("ConfirmationTerminalState" in p for p in drift.check_lists(ENUMS, mutated)))

    def test_value_renamed_in_the_schema(self):
        changed = copy.deepcopy(ENUMS)
        changed["$defs"]["Verdict"]["enum"][0] = "ACHEIVED"
        self.assertTrue(any("Verdict" in p for p in drift.check_lists(changed, REGISTRY)))

    def test_value_added_to_the_schema(self):
        changed = copy.deepcopy(ENUMS)
        changed["$defs"]["ExecutionState"]["enum"].append("SUCCEEDED")
        self.assertTrue(any("ExecutionState" in p for p in drift.check_lists(changed, REGISTRY)))

    def test_core_reason_code_removed_from_the_schema(self):
        changed = copy.deepcopy(ENUMS)
        changed["$defs"]["CoreReasonCode"]["enum"].remove("MODE_NOT_ENABLED")
        self.assertTrue(any("CoreReasonCode" in p for p in drift.check_lists(changed, REGISTRY)))

    def test_list_without_a_registry_row(self):
        changed = copy.deepcopy(ENUMS)
        changed["$defs"]["Invented"] = {"type": "string", "enum": ["A"]}
        self.assertTrue(any("Invented" in p for p in drift.check_lists(changed, REGISTRY)))

    def test_registry_row_without_a_list(self):
        changed = copy.deepcopy(ENUMS)
        del changed["$defs"]["FactStatus"]
        self.assertTrue(any("FactStatus" in p for p in drift.check_lists(changed, REGISTRY)))

    def test_currency_table_and_schema_differ(self):
        changed = copy.deepcopy(SCHEMAS)
        changed["money.v1.json"]["$defs"]["CurrencyCode"]["enum"].append("USD")
        self.assertTrue(any("currencies.json" in p for p in drift.check_money(changed, REGISTRY)))

    def test_amount_ceiling_changed(self):
        changed = copy.deepcopy(SCHEMAS)
        changed["money.v1.json"]["$defs"]["MinorAmount"]["maximum"] = 9007199254740992
        self.assertTrue(any("MinorAmount" in p for p in drift.check_money(changed, REGISTRY)))

    def test_amount_made_a_number(self):
        changed = copy.deepcopy(SCHEMAS)
        changed["money.v1.json"]["$defs"]["MinorAmount"]["type"] = "number"
        self.assertTrue(any("MinorAmount" in p for p in drift.check_money(changed, REGISTRY)))

    def test_every_core_enum_is_a_distinct_type(self):
        """OD-P-10: no two lists may share the same set of values, or one could be assigned to the other."""
        sets = {}
        for name, definition in ENUMS["$defs"].items():
            values = frozenset(drift.schema_values(definition))
            if values:
                self.assertNotIn(values, sets, f"{name} and {sets.get(values)} have identical values")
                sets[values] = name


if __name__ == "__main__":
    unittest.main()
