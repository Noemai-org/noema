"""Every TEN_RESULT line the repository prints must be CHECKED by a workflow (ruling A274; the Security Reviewer's note 3 on noema#578).

A TEN test prints `TEN_RESULT id=TEN-nn ... pass=...`. A job that only prints it would still pass if the test were renamed (go's -run would match nothing) or if the line stopped being
printed. So each printed id (and, for TEN-02, each part) needs a `grep` in a workflow for its line with `pass=true`. This reads the sources and the workflows as text."""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SKIP = {".git", "node_modules", "__pycache__", ".venv", "tools"}


def printed() -> set[tuple[str, str]]:
    """(id, part) of every TEN_RESULT the code can print. `part` is empty when the line has none. A line built from a variable id (the mock-case helper) is read from its callers."""
    found: set[tuple[str, str]] = set()
    literal = re.compile(r"TEN_RESULT id=(TEN-\d\d)(?: part=([a-z-]+))?")
    helper = re.compile(r'tenResult\(t, "(TEN-\d\d)"')
    for path in ROOT.rglob("*"):
        if any(part in SKIP or part.startswith(".venv") for part in path.relative_to(ROOT).parts) or path.suffix not in {".go", ".py"} or not path.is_file():
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        for m in literal.finditer(text):
            found.add((m.group(1), m.group(2) or ""))
        for m in helper.finditer(text):
            found.add((m.group(1), ""))
    return found


def checked() -> set[tuple[str, str]]:
    found: set[tuple[str, str]] = set()
    grep = re.compile(r"grep -Eq '([^']*TEN_RESULT id=(TEN-\d\d)[^']*pass=true[^']*)'")
    for path in (ROOT / ".github" / "workflows").glob("*.y*ml"):
        for m in grep.finditer(path.read_text(encoding="utf-8")):
            part = re.search(r"part=([a-z-]+)", m.group(1))
            found.add((m.group(2), part.group(1) if part else ""))
    return found


class TenResultGrep(unittest.TestCase):
    def test_every_printed_ten_result_is_checked_by_a_workflow(self) -> None:
        printed_lines, checked_lines = printed(), checked()
        self.assertTrue(printed_lines, "no TEN_RESULT line found in the sources: the reader is broken")
        self.assertEqual(sorted(printed_lines - checked_lines), [], "a TEN_RESULT line is printed but no workflow greps it with pass=true")

    def test_the_reader_sees_the_known_lines(self) -> None:
        for want in (("TEN-01", ""), ("TEN-02", "trust-decisions"), ("TEN-02", "outcome-verdicts"), ("TEN-03", ""), ("TEN-04", ""), ("TEN-06", "")):
            self.assertIn(want, printed(), want)
            self.assertIn(want, checked(), want)


if __name__ == "__main__":
    unittest.main()
