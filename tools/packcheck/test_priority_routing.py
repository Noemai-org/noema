"""packcheck against the pack's priority routing member (PCK-09; ruling A644, interim, relayed, not yet committed; A616; A333; INV-14; REQ-CAP-001).

The member maps each SLA phase to a priority (HIGH, NORMAL or LOW) and an owner queue. packcheck must fail when a priority is outside that
vocabulary, an owner queue is empty or is not one the pack's routing allowlist names, a phase is not one the pack's `sla_phase` names, the label
is not SYNTHETIC, a member is unknown, or the file is not pinned (changed, unlisted, listed twice). Each test builds its own small pack tree and
breaks one thing. All data SYNTHETIC.
"""
import copy
import json
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import test_manifest as tm  # noqa: E402

PRIO = "fintech/routing/priority_routing.v1.json"
PACKS = tm.PACKS


def real() -> dict:
    return json.loads((PACKS / PRIO).read_text(encoding="utf-8"))


class Tree(tm.Base):
    """A pack tree that lists the member, as the committed manifest does."""

    def member_tree(self):
        root, manifest = self.tree()
        (root / PRIO).parent.mkdir(parents=True, exist_ok=True)
        (root / PRIO).write_text((PACKS / PRIO).read_text(encoding="utf-8"), encoding="utf-8")
        self.edit(root, manifest, lambda d: d["files"].append({"kind": "priority_routing", "path": PRIO, "sha256": tm.sha256(root / PRIO)}))
        return root, manifest

    def change(self, root: Path, manifest: Path, fn) -> None:
        doc = real()
        fn(doc)
        tm.write(root / PRIO, doc)
        self.edit(root, manifest, lambda d: tm.entry_of(d, PRIO).update(sha256=tm.sha256(root / PRIO)))


class CommittedMember(Tree):
    def test_the_committed_manifest_pins_the_member(self):
        doc = json.loads(tm.REAL_MANIFEST.read_text(encoding="utf-8"))
        self.assertEqual([f["path"] for f in doc["files"] if f["kind"] == "priority_routing"], [PRIO])
        self.assertEqual(self.check(tm.REAL_MANIFEST, PACKS), [])

    def test_it_is_synthetic_and_routes_exactly_the_packs_sla_phases_to_allowed_queues(self):
        m = real()
        pack = json.loads((PACKS / "fintech_v1.json").read_text(encoding="utf-8"))
        self.assertEqual(m["label"], "SYNTHETIC")
        self.assertEqual(sorted(m["phases"]), sorted(pack["predicate_variables"]["sla_phase"]["values"]))
        for phase, e in m["phases"].items():
            self.assertIn(e["priority"], ("HIGH", "NORMAL", "LOW"), phase)
            self.assertIn(e["owner_queue"], pack["routing_allowlist"], phase)

    def test_a_good_tree_with_the_member_passes(self):
        root, manifest = self.member_tree()
        self.assertEqual(self.check(manifest, root), [])


class BadMember(Tree):
    def test_a_priority_outside_the_vocabulary_fails(self):
        for bad in ("URGENT", "high", "", " HIGH", "HIGH\n", None, 1):
            with self.subTest(bad):
                root, manifest = self.member_tree()
                self.change(root, manifest, lambda d: d["phases"]["BREACHED"].update(priority=bad))
                self.fails(root, manifest, "priority_routing", "priority")

    def test_an_empty_owner_queue_fails(self):
        for bad in ("", " ", None, 3):
            with self.subTest(bad):
                root, manifest = self.member_tree()
                self.change(root, manifest, lambda d: d["phases"]["RUNNING"].update(owner_queue=bad))
                self.fails(root, manifest, "priority_routing", "owner_queue")

    def test_a_missing_priority_or_queue_fails(self):
        for key in ("priority", "owner_queue"):
            with self.subTest(key):
                root, manifest = self.member_tree()
                self.change(root, manifest, lambda d: d["phases"]["RUNNING"].pop(key))
                self.fails(root, manifest, "priority_routing", key)

    def test_a_queue_the_pack_does_not_allow_fails(self):
        root, manifest = self.member_tree()
        self.change(root, manifest, lambda d: d["phases"]["RUNNING"].update(owner_queue="NOT_A_QUEUE"))
        self.fails(root, manifest, "priority_routing", "routing_allowlist")

    def test_a_phase_the_pack_does_not_name_fails(self):
        root, manifest = self.member_tree()
        self.change(root, manifest, lambda d: d["phases"].update(INVENTED=copy.deepcopy(d["phases"]["RUNNING"])))
        self.fails(root, manifest, "priority_routing", "sla_phase", "INVENTED")

    def test_an_unknown_member_a_wrong_label_or_no_phases_fails(self):
        for name, fn in (("extra", lambda d: d.update(extra=1)), ("entry extra", lambda d: d["phases"]["RUNNING"].update(weight=1)),
                         ("label", lambda d: d.update(label="PRODUCTION")), ("no phases", lambda d: d.update(phases={}))):
            with self.subTest(name):
                root, manifest = self.member_tree()
                self.change(root, manifest, fn)
                self.fails(root, manifest, "priority_routing")

    def test_a_changed_member_fails_its_pin(self):
        root, manifest = self.member_tree()
        doc = real()
        doc["phases"]["RUNNING"]["priority"] = "HIGH" if doc["phases"]["RUNNING"]["priority"] != "HIGH" else "LOW"
        tm.write(root / PRIO, doc)
        self.fails(root, manifest, PRIO, "sha256")

    def test_a_member_that_is_not_listed_fails(self):
        root, manifest = self.member_tree()
        self.edit(root, manifest, lambda d: d["files"].remove(tm.entry_of(d, PRIO)))
        self.fails(root, manifest, PRIO, "not listed")

    def test_a_second_file_beside_the_member_fails(self):
        root, manifest = self.member_tree()
        (root / "fintech/routing/priority_routing.v2.json").write_text("{}", encoding="utf-8")
        self.fails(root, manifest, "priority_routing.v2.json", "not listed")

    def test_two_entries_of_the_kind_fail(self):
        root, manifest = self.member_tree()
        (root / "fintech/routing/second.json").write_text((root / PRIO).read_text(encoding="utf-8"), encoding="utf-8")
        self.edit(root, manifest, lambda d: d["files"].append({"kind": "priority_routing", "path": "fintech/routing/second.json", "sha256": tm.sha256(root / "fintech/routing/second.json")}))
        self.fails(root, manifest, "priority_routing")

    def test_a_member_that_is_not_json_fails(self):
        root, manifest = self.member_tree()
        (root / PRIO).write_text("{", encoding="utf-8")
        self.edit(root, manifest, lambda d: tm.entry_of(d, PRIO).update(sha256=tm.sha256(root / PRIO)))
        self.fails(root, manifest, "priority_routing")


if __name__ == "__main__":
    unittest.main()
