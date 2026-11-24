"""AGT-01 (REQ-AGT-003, AGENT-27, AGENT-26): a PR body that carries a CONFLICT DETECTED marker must carry every field of the block
(affected IDs, the statements in conflict, evidence, proposed alternatives, the safer reading, the decision required) and no template
placeholder. This is the mechanical half of 'stop and file the block'; a real agent's stop is a founder-run exercise (status/AGT-01.md).
All bodies here are SYNTHETIC."""
import subprocess
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import check_pr_trace as trace  # noqa: E402

IDS = "Task: AGT-01\nREQ-AGT-003\nINV-14\n"
FIELDS = {
    "Affected IDs": "INV-20, SOT-C-12",
    "Statements in conflict": '"freezes all writes" (SoT §45) vs "escalates" (SoT §37)',
    "Evidence": "conflicts-register.md shows SOT-C-12 Open",
    "Proposed alternatives": "(1) adopt the allow-list; blast radius: INV-20 text",
    "Safer reading applied meanwhile (AGENT-20)": "refuse every write on the frozen target",
    "Decision required from": "founders",
}


def block(fields: dict[str, str], fence: bool = True) -> str:
    lines = ["CONFLICT DETECTED"] + [f"{k}: {v}" for k, v in fields.items()]
    text = "\n".join(lines)
    return f"```\n{text}\n```\n" if fence else text + "\n"


class ConflictBlock(unittest.TestCase):
    def gaps(self, body: str) -> list[str]:
        return trace.conflict_gaps(body)

    def test_a_body_without_a_marker_has_no_conflict_gaps(self):
        self.assertEqual(self.gaps(IDS + "No conflict found.\n"), [])

    def test_the_heading_alone_is_not_a_marker(self):
        self.assertEqual(self.gaps(IDS + "## CONFLICT DETECTED\nNone.\n"), [])

    def test_prose_that_mentions_the_words_is_not_a_marker(self):
        self.assertEqual(self.gaps(IDS + "I did not raise a CONFLICT DETECTED block because there was none.\n"), [])

    def test_a_complete_block_passes_fenced_and_unfenced(self):
        self.assertEqual(self.gaps(IDS + block(FIELDS)), [])
        self.assertEqual(self.gaps(IDS + block(FIELDS, fence=False)), [])

    def test_a_marker_with_no_fields_fails_on_all_six(self):
        self.assertEqual(len(self.gaps(IDS + "CONFLICT DETECTED\n")), 6)

    def test_each_missing_field_is_named(self):
        for name in FIELDS:
            partial = {k: v for k, v in FIELDS.items() if k != name}
            gaps = self.gaps(IDS + block(partial))
            self.assertEqual(len(gaps), 1, (name, gaps))
            self.assertIn(name.split(" (")[0], gaps[0])

    def test_a_missing_field_is_reported_as_missing_and_an_empty_one_as_empty(self):
        partial = {k: v for k, v in FIELDS.items() if k != "Evidence"}
        self.assertIn("is missing", self.gaps(IDS + block(partial))[0])
        self.assertIn("empty or still the template placeholder", self.gaps(IDS + block({**FIELDS, "Evidence": ""}))[0])

    def test_a_quoted_or_bold_marker_still_counts(self):
        for marker in ("> CONFLICT DETECTED", "**CONFLICT DETECTED**"):
            self.assertEqual(len(self.gaps(IDS + marker + "\n")), 6, marker)

    def test_an_empty_field_fails(self):
        for name in FIELDS:
            gaps = self.gaps(IDS + block({**FIELDS, name: "   "}))
            self.assertEqual(len(gaps), 1, (name, gaps))

    def test_the_placeholders_are_the_template_text_of_every_field(self):
        template = (HERE.parent.parent / ".github" / "pull_request_template.md").read_text()
        self.assertEqual(set(trace.CONFLICT_PLACEHOLDERS), set(FIELDS))
        for placeholder in trace.CONFLICT_PLACEHOLDERS.values():
            self.assertIn(placeholder, template)

    def test_an_unfilled_template_placeholder_fails(self):
        for name, placeholder in trace.CONFLICT_PLACEHOLDERS.items():
            gaps = self.gaps(IDS + block({**FIELDS, name: placeholder}))
            self.assertEqual(len(gaps), 1, (name, gaps))

    def test_the_unfilled_template_block_fails_on_all_six(self):
        template = (HERE.parent.parent / ".github" / "pull_request_template.md").read_text()
        self.assertEqual(len(self.gaps(template)), 6)

    def test_a_field_may_continue_on_following_lines(self):
        body = IDS + "CONFLICT DETECTED\n" + "".join(
            f"{k}:\n  {v}\n" for k, v in FIELDS.items()
        )
        self.assertEqual(self.gaps(body), [])

    def test_a_field_after_the_closing_fence_does_not_count(self):
        partial = {k: v for k, v in FIELDS.items() if k != "Evidence"}
        body = IDS + block(partial) + "Evidence: late\n"
        self.assertEqual(len(self.gaps(body)), 1)

    def test_the_command_line_refuses_an_incomplete_block_and_accepts_a_complete_one(self):
        script = str(HERE / "check_pr_trace.py")
        bad = subprocess.run([sys.executable, "-I", script], input=IDS + "CONFLICT DETECTED\n", capture_output=True, text=True)
        self.assertEqual(bad.returncode, 1)
        self.assertIn("pr-trace FAIL", bad.stdout)
        self.assertIn("CONFLICT DETECTED", bad.stdout)
        good = subprocess.run([sys.executable, "-I", script], input=IDS + block(FIELDS), capture_output=True, text=True)
        self.assertEqual((good.returncode, good.stdout.strip()), (0, "pr-trace OK"))


if __name__ == "__main__":
    unittest.main()
