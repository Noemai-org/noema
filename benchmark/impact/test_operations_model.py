"""The Operations Model (benchmark suite definition v1, IMPACT layer): every parameter is labelled, ASSUMED ones carry no source and no claim, and the table agrees with the pack.

Written before the file was committed and seen failing (no file). SYNTHETIC. Generated layer only (INV-22)."""
import json
import unittest
from pathlib import Path
from typing import Any

HERE = Path(__file__).resolve().parent
MODEL = HERE / "operations-model.v1.json"
PACK = HERE.parents[1] / "packs" / "fintech_v1.json"
STATUSES = {"ASSUMED", "SOURCED", "MEASURED_FROM_RUN"}

Json = dict[str, Any]


def model() -> Json:
    loaded: Json = json.loads(MODEL.read_text(encoding="utf-8"))
    return loaded


class OperationsModel(unittest.TestCase):
    def test_the_file_says_all_parameters_are_assumed_until_the_founder_supplies_them(self) -> None:
        m = model()
        self.assertEqual("operations-model.v1", m["schema"])
        self.assertIn("ASSUMED", m["status"])
        self.assertTrue(m["identical_for_all_arms"])
        self.assertIn("modelled", m["labels_required_on_every_figure"])

    def test_every_parameter_has_a_status_and_an_assumed_one_has_no_source(self) -> None:
        for name, p in model()["parameters"].items():
            self.assertIn(p["status"], STATUSES, name)
            if p["status"] == "ASSUMED":
                self.assertIsNone(p["source"], f"{name}: an ASSUMED parameter must not claim a source")
            if p["status"] == "SOURCED":
                self.assertTrue(p["source"], f"{name}: a SOURCED parameter needs its source")

    def test_low_value_high_are_ordered_wherever_there_are_numbers(self) -> None:
        m = model()
        rows = [(n, p) for n, p in m["parameters"].items() if p["value"] is not None]
        for n, t in m["escalation_types"].items():
            if n.startswith("_"):
                continue
            rows += [(f"{n}.{k}", t[k]) for k in ("human_minutes", "touches", "sla_minutes")]
        for k in ("first_contact_resolution", "human_minutes_multiplier_over_escalation_table", "extra_clarification_minutes_per_exception"):
            rows.append((f"m0.{k}", m["m0_manual_baseline"][k]))
        self.assertTrue(rows)
        for name, p in rows:
            self.assertLessEqual(p["low"], p["value"], name)
            self.assertLessEqual(p["value"], p["high"], name)

    def test_the_escalation_table_has_exactly_the_packs_routing_destinations_and_the_clarification_handoff(self) -> None:
        pack = json.loads(PACK.read_text(encoding="utf-8"))
        table = {n for n in model()["escalation_types"] if not n.startswith("_")}
        self.assertEqual(set(pack["routing_allowlist"]) | {"CLARIFICATION_HANDOFF"}, table)
        for n in table:
            self.assertEqual("ASSUMED", model()["escalation_types"][n]["status"], n)

    def test_the_sensitivity_method_gives_a_range_never_a_point(self) -> None:
        s = model()["sensitivity"]
        self.assertEqual(3, len(s["method"]))
        self.assertIn("range", s["reported_as"])
        self.assertIsInstance(s["seed"], int)

    def test_the_manual_baseline_is_labelled_assumed_and_escalates_everything(self) -> None:
        m0 = model()["m0_manual_baseline"]
        self.assertEqual("ASSUMED", m0["status"])
        self.assertIsNone(m0["source"])
        self.assertEqual(1.0, m0["escalation_rate"])

    def test_every_impact_metric_of_the_definition_has_a_formula(self) -> None:
        wanted = {"escalation_rate", "first_contact_resolution", "human_investigation_minutes", "human_touches", "resolution_time_minutes", "sla_exposure", "cost_per_exception", "backlog",
                  "customer_effort"}
        self.assertTrue(wanted <= set(model()["impact_metrics"]))


if __name__ == "__main__":
    unittest.main()
