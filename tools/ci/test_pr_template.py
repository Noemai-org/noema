"""AGT-01 (REQ-AGT-001, REQ-AGT-003; AGENTS section 11): the pull request template follows the order of AGENTS section 11, carries the SoT section 52
questions, the hand-off report and the CONFLICT block, and an unfilled template fails the trace check on all three IDs (its own examples must not satisfy it)."""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_pr_trace as trace  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent.parent
TEMPLATE = (ROOT / ".github" / "pull_request_template.md").read_text()

# AGENTS section 11, "The PR description, in this order".
ORDER = ["## Task, REQ and SoT IDs", "## Plan", "## SoT section 52 questions", "## Checklist", "## Hand-off report", "## CONFLICT DETECTED"]


class Template(unittest.TestCase):
    def test_sections_follow_the_order_of_agents_section_11(self):
        positions = [TEMPLATE.find(h) for h in ORDER]
        self.assertTrue(all(p >= 0 for p in positions), f"missing sections: {[h for h, p in zip(ORDER, positions) if p < 0]}")
        self.assertEqual(positions, sorted(positions))

    def test_an_unfilled_template_fails_the_trace_check_on_every_id(self):
        self.assertEqual(len(trace.missing(TEMPLATE)), 3, trace.missing(TEMPLATE))

    def test_the_plan_names_tests_first_faults_and_record_events(self):
        for word in ("tests written first", "faults exercised", "Record events asserted"):
            self.assertIn(word.lower(), TEMPLATE.lower())

    def test_the_section_52_questions_include_the_unsafe_autonomy_surface(self):
        self.assertIn("unsafe autonomy surface", TEMPLATE)

    def test_the_conflict_block_has_the_fields_of_req_agt_003(self):
        for field in ("Affected IDs", "Statements in conflict", "Evidence", "Proposed alternatives", "Safer reading applied meanwhile", "Decision required from"):
            self.assertIn(field, TEMPLATE)


class CardTemplates(unittest.TestCase):
    def test_the_task_card_and_conflict_block_templates_are_present_and_dated(self):
        for name in ("task-card.md", "conflict-block.md"):
            text = (ROOT / "docs" / "templates" / name).read_text()
            self.assertIn("Copy of Noemai-org/docs", text, f"{name} must say it is a copy and which original wins")


if __name__ == "__main__":
    unittest.main()
