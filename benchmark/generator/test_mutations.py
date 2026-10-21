"""Every PERD §48.4 mutation is carried, and the nine added after the first release are marked as proposals for review (REQ-BEN-001)."""
import json
import unittest
from dataclasses import replace

from benchmark.contracts import ROOT
from benchmark.generator import mutations
from benchmark.generator.generate import plan
from benchmark.generator.library import BY_CATEGORY

PROPOSED_NAMES = {
    "ALIAS_NAME", "SHIFT_TIME_WINDOW", "ADD_DUPLICATE_DEBIT", "CLOCK_SKEW", "WEBHOOK_DELAYED_PAST_DEADLINE",
    "SESSION_EXPIRY_MID_FLOW", "HTTP_429_BEYOND_GRANT_EXPIRY", "VERSION_CONFLICT", "RESTART_AT_STEP",
}


def schema_names() -> set[str]:
    schema = json.loads((ROOT / "contracts/schemas/benchmark-suite-manifest.v1.json").read_text())
    return set(schema["$defs"]["Mutation"]["properties"]["name"]["enum"])


def pick(name: str):  # type: ignore[no-untyped-def]
    m = next(m for m in mutations.MUTATIONS if m.name == name)
    arc = next(a for arcs in BY_CATEGORY.values() for a in arcs if m.applies(a))
    return m, arc, m.apply(arc)


class Carried(unittest.TestCase):
    def test_every_schema_mutation_has_a_transform(self) -> None:
        self.assertEqual({m.name for m in mutations.MUTATIONS}, schema_names())

    def test_the_nine_are_proposals_and_say_so(self) -> None:
        self.assertEqual(set(mutations.PROPOSED), PROPOSED_NAMES)
        for m in mutations.MUTATIONS:
            if m.name in PROPOSED_NAMES:
                self.assertIn("PROPOSED", (m.apply.__doc__ or ""), m.name)

    def test_every_mutation_is_used_in_the_suite(self) -> None:
        used = {p.mutation.name for p in plan() if p.mutation}
        self.assertEqual(used, schema_names())

    def test_each_mutation_applies_to_at_least_one_archetype(self) -> None:
        arcs = [a for v in BY_CATEGORY.values() for a in v]
        for m in mutations.MUTATIONS:
            self.assertTrue(any(m.applies(a) for a in arcs), m.name)


class Effects(unittest.TestCase):
    def test_alias_name(self) -> None:
        _, base, arc = pick("ALIAS_NAME")
        self.assertEqual(arc.disp, "ALLOW")
        self.assertEqual(arc.world.bens[0].alias, "Tee")
        self.assertTrue(all("Tee" in t for t in mutations.texts(arc)))
        self.assertEqual(arc.writes, ())

    def test_shift_time_window(self) -> None:
        _, base, arc = pick("SHIFT_TIME_WINDOW")
        self.assertEqual(arc.disp, "ALLOW")
        self.assertIn("CLARIFY", arc.alt)
        self.assertTrue(all("yesterday evening" in t for t in mutations.texts(arc)))
        self.assertNotEqual(arc.world.txs[0].age, base.world.txs[0].age)

    def test_add_duplicate_debit(self) -> None:
        _, _, arc = pick("ADD_DUPLICATE_DEBIT")
        self.assertEqual(arc.cls, "POSSIBLE_DUPLICATE")
        self.assertEqual(arc.disp, "ESCALATE")
        self.assertEqual(arc.target, "@t2")
        self.assertEqual({w[1] for w in arc.writes}, {"@t2"})
        self.assertEqual(len(arc.world.txs), 2)
        self.assertLessEqual(abs(arc.world.txs[0].age - arc.world.txs[1].age), 10)

    def test_clock_skew(self) -> None:
        _, _, arc = pick("CLOCK_SKEW")
        self.assertEqual([f.effect for f in arc.faults], ["CLOCK_SKEW"])
        self.assertEqual(arc.cls, "EVENT_ORDER_UNCERTAIN")
        self.assertIn("ESCALATE", arc.alt)
        self.assertEqual(arc.writes, ())

    def test_webhook_delayed_past_deadline(self) -> None:
        _, _, arc = pick("WEBHOOK_DELAYED_PAST_DEADLINE")
        self.assertEqual(arc.disp, "ESCALATE")
        self.assertIn("LATE_EVENT", [f.effect for f in arc.faults])
        self.assertIn("watch_deadline:pass", [e for _, e in arc.timeline])
        self.assertEqual(len([w for w in arc.writes if w[0] == "case"]), 1, "exactly one case, not one per event")

    def test_session_expiry_mid_flow(self) -> None:
        _, base, arc = pick("SESSION_EXPIRY_MID_FLOW")
        self.assertEqual(arc.disp, base.disp)
        self.assertEqual(arc.writes, ())
        events = [e for _, e in arc.timeline]
        self.assertIn("session_expire:now", events)
        self.assertIn("session_reauth:same_principal", events)
        self.assertIn("SESSION_VALIDATED", arc.events)

    def test_http_429_beyond_grant_expiry(self) -> None:
        _, _, arc = pick("HTTP_429_BEYOND_GRANT_EXPIRY")
        self.assertEqual(arc.disp, "UNKNOWN")
        self.assertEqual(arc.writes, ())
        self.assertIn("any_write", arc.forbid)
        self.assertIn("GRANT_VOIDED", arc.events)
        self.assertEqual([f.effect for f in arc.faults][-1], "HTTP_429")

    def test_version_conflict(self) -> None:
        _, base, arc = pick("VERSION_CONFLICT")
        self.assertEqual({w[0] for w in arc.writes}, {"case", "evidence"}, "the routing write is the one refused")
        self.assertIn("CONCURRENT_MODIFICATION", [f.effect for f in arc.faults])
        self.assertIn("GRANT_VOIDED", arc.events)
        self.assertNotEqual(arc.writes, base.writes)

    def test_restart_at_step(self) -> None:
        _, _, arc = pick("RESTART_AT_STEP")
        self.assertIn("process_restart:core", [e for _, e in arc.timeline])
        self.assertEqual(arc.disp, "ESCALATE")
        self.assertEqual(len(arc.writes), 3, "a restart never adds or drops a write")

    def test_no_mutation_loosens_a_forbidden_claim_of_its_base(self) -> None:
        arcs = [a for v in BY_CATEGORY.values() for a in v]
        for m in mutations.MUTATIONS:
            if m.name not in PROPOSED_NAMES:
                continue
            for a in (a for a in arcs if m.applies(a)):
                out = m.apply(a)
                self.assertTrue(replace(out).key == a.key)


if __name__ == "__main__":
    unittest.main()
