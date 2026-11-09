"""transitions.yaml against PERD §28.2 (REQ-STM-001): every arrow of the PERD has a row, and every row is an arrow of the PERD.

The PERD text is the vendored extract perd-28-2-state-machine.md, re-pointed by tools/vendor/repoint_docs.py. Run:
python3 -I -m unittest discover -s services/core/orchestrator -p 'test_*.py'
"""
import re
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROW = re.compile(r'^  - \{from: ([A-Z_]+), to: ([A-Z_]+), on: "([^"]+)"\}$')
STATE = re.compile(r"\b[A-Z][A-Z_]{2,}\b")
# Capitalised words of an exit line that name an event or a routing target, never a state: not arrows (PERD v1.21, EXPLAINING_BLOCKED).
NOT_STATES = {"DECISION_MADE", "OPS"}


def yaml_rows():
    rows = []
    for line in (HERE / "transitions.yaml").read_text().splitlines():
        if line.startswith("  - "):
            m = ROW.match(line)
            assert m, f"unreadable row: {line}"
            rows.append(m.groups())
    return rows


def perd_arrows():
    """(from, to) pairs read from every state's 'Exit and allowed next states' line.

    The targets are the capitalised words of the line's first sentence (parentheses and tags removed: they carry causes), up to the first '. ' or end of
    line. Later sentences state prohibitions ("It never returns to EXPLAINING"), which are not arrows. A word that is not a state of the PERD must be in
    NOT_STATES (an event or a routing target); any other unknown word fails, so a mistyped target cannot be silently dropped.
    """
    text = (HERE / "perd-28-2-state-machine.md").read_text()
    lines = text.splitlines()
    states = [m.group(1) for m in (re.match(r"^\*\*([A-Z_]+)\*\*", l) for l in lines) if m and m.group(1) != "FROZEN"]  # FROZEN is a flag, not a state
    arrows, current = set(), None
    for line in lines:
        head = re.match(r"^\*\*([A-Z_]+)\*\*", line)
        if head and head.group(1) != "FROZEN":
            current = head.group(1)
        elif current and line.startswith("- Exit and allowed next states:"):
            body = line.split(":", 1)[1]
            body = re.sub(r"\([^)]*\)|\[[^\]]*\]", " ", body)
            body = re.split(r"\.(?:\s|$)", body.strip(), maxsplit=1)[0]
            for word in STATE.findall(body):
                if word in states:
                    arrows.add((current, word))
                elif word not in NOT_STATES:
                    raise AssertionError(f"{current}: '{word}' in the exit line is neither a state nor in NOT_STATES")
    return states, arrows


class Transitions(unittest.TestCase):
    def test_every_perd_arrow_has_a_row_and_every_row_is_a_perd_arrow(self):
        _, arrows = perd_arrows()
        rows = {(f, t) for f, t, _ in yaml_rows()}
        self.assertEqual(sorted(arrows - rows), [], "PERD arrows with no row")
        self.assertEqual(sorted(rows - arrows), [], "rows that are not PERD arrows")

    def test_the_perd_has_the_expected_number_of_arrows(self):
        _, arrows = perd_arrows()
        self.assertEqual(len(arrows), 43)  # distinct (from, to) pairs; AWAITING_CONFIRMATION to EVALUATING has two causes (ACCEPT, EXPIRED), so 44 rows

    def test_every_state_of_the_perd_is_declared_and_every_declared_state_is_used(self):
        states, _ = perd_arrows()
        declared = re.search(r"^states: \[(.*)\]$", (HERE / "transitions.yaml").read_text(), re.M).group(1).split(", ")
        self.assertEqual(sorted(declared), sorted(set(states)))
        used = {x for f, t, _ in yaml_rows() for x in (f, t)}
        self.assertLessEqual(used, set(declared))

    def test_every_row_has_a_cause_and_no_row_repeats(self):
        rows = yaml_rows()
        self.assertTrue(all(cause.strip() for _, _, cause in rows))
        self.assertEqual(len(rows), len(set(rows)))

    def test_nothing_leaves_expired_and_only_listed_states_are_terminal(self):
        rows = yaml_rows()
        self.assertEqual([r for r in rows if r[0] == "EXPIRED"], [])
        leaving = {f for f, _, _ in rows}
        self.assertEqual(sorted(set(re.search(r"^states: \[(.*)\]$", (HERE / "transitions.yaml").read_text(), re.M).group(1).split(", ")) - leaving),
                         ["COMPLETED", "EXPIRED", "REJECTED"])  # EXPLAINING_BLOCKED is no longer terminal (E4)


if __name__ == "__main__":
    unittest.main()
