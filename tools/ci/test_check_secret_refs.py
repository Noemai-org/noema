"""Tests for check_secret_refs.py: what it accepts, what it rejects, and that the real repository passes."""
import re
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_secret_refs as c  # noqa: E402

ROOT = Path(__file__).resolve().parent.parent.parent


def run(workflow: str, env: str) -> list[str]:
    with tempfile.TemporaryDirectory() as tmp:
        d = Path(tmp)
        (d / "wf").mkdir()
        (d / "wf" / "a.yml").write_text(workflow)
        (d / "env").write_text(env)
        return c.violations(d / "wf", d / "env")


# The one file that may carry public documentation links of the speech provider (A268; noema#625): the vendor's documentation notes, whose numbers must be traceable to an exact page (Codex P1
# on noema#625). The exemption is narrow: that one path; https only; no user, no port, no query; a host with no api, inference, stt, tts, ws, stream or sandbox label (an inference host is an
# endpoint, not documentation, and is never allowed); and only in a link's target. Everywhere else, and for every other kind of line, the rule stays strict. No host is named here.
DOCS_NOTES_FILE = "docs/voice/intron-docs-notes-DRAFT.md"
ENDPOINT = re.compile(r"(?i)https?://[^\s\"'<>)]*intron|\bintron[a-z0-9-]*\.(?:com|ai|io|net|org|co|africa|ng)\b")
DOCS_LINK = re.compile(r"(?i)^https://(?P<host>[a-z0-9.-]+)(?P<path>/[^\s?#\"'<>)]*)?(?:#[A-Za-z0-9_.-]*)?$")
ENDPOINT_LABELS = {"api", "inference", "stt", "tts", "ws", "wss", "stream", "sandbox", "grpc"}


def names_endpoint(rel: str, line: str) -> bool:
    """True when the line names a speech-provider endpoint the repository must not carry. In DOCS_NOTES_FILE a line whose every such name is a plain documentation link is allowed."""
    if not ENDPOINT.search(line):
        return False
    if rel != DOCS_NOTES_FILE:
        return True
    # Remove ONLY the permitted link targets, each by its own position: a bare URL that repeats or extends an allowed target elsewhere on the line stays and is still an endpoint
    # (Codex P2 on noema#728: removing every occurrence of the text hid a bare copy of it).
    spans: list[tuple[int, int]] = []
    for match in re.finditer(r"\]\((?P<md>[^)\s]+)\)|<(?P<angle>https?://[^>\s]+)>", line):
        group = "md" if match.group("md") else "angle"
        url = match.group(group)
        m = DOCS_LINK.match(url)
        if m and not (set(m.group("host").lower().split(".")) & ENDPOINT_LABELS) and ":" not in m.group("host"):
            spans.append((match.start(group), match.end(group)))
    rest = line
    for start, end in reversed(spans):
        rest = rest[:start] + rest[end:]
    return bool(ENDPOINT.search(rest))


class Cases(unittest.TestCase):
    def test_listed_secret_passes(self):
        self.assertEqual(run("x: ${{ secrets.OPENAI_API_KEY }}\n", "OPENAI_API_KEY=\n"), [])

    def test_unlisted_secret_fails(self):
        self.assertEqual(len(run("x: ${{ secrets.OPENAI_API_KEY }}\n", "ANTHROPIC_API_KEY=\n")), 1)

    def test_github_token_is_built_in(self):
        self.assertEqual(run("x: ${{ secrets.GITHUB_TOKEN }}\n", ""), [])

    def test_prefix_line_covers_the_family(self):
        self.assertEqual(run("x: ${{ secrets.TUSHE_ORACLE_CLIENT_ID }}\n", "TUSHE_ORACLE_*=\n"), [])
        self.assertEqual(len(run("x: ${{ secrets.TUSHE_FIXTURE_ID }}\n", "TUSHE_ORACLE_*=\n")), 1)

    def test_bracket_form_is_checked(self):
        self.assertEqual(len(run("x: ${{ secrets['NOPE'] }}\n", "")), 1)
        self.assertEqual(run("x: ${{ secrets['OK_KEY'] }}\n", "OK_KEY=\n"), [])

    def test_dynamic_lookup_fails(self):
        self.assertEqual(len(run("x: ${{ secrets[matrix.name] }}\n", "ANY=\n")), 1)

    def test_comments_are_ignored(self):
        self.assertEqual(run("# ${{ secrets.NOPE }}\nx: 1  # secrets.NOPE\n", ""), [])

    def test_a_name_that_only_starts_like_a_listed_one_fails(self):
        self.assertEqual(len(run("x: ${{ secrets.OPENAI_API_KEY_2 }}\n", "OPENAI_API_KEY=\n")), 1)

    def test_a_value_in_the_example_does_not_count_as_a_name_line_with_a_space(self):
        self.assertEqual(len(run("x: ${{ secrets.K }}\n", "# K=\n")), 1)

    def test_the_intron_key_and_endpoint_slot_are_named_and_empty(self):
        # A179: Intron Sahara v2.5 is the speech provider. The vendor register left its key name open; the setting is INTRON_API_KEY_FILE, a path (A249; the pattern of AETHEX_API_KEY). The provider's
        # endpoint is a setting the deployment gives, so it has a NAME here and no value: no key and no hostname in the repository.
        values = {}
        for line in (ROOT / ".env.example").read_text().splitlines():
            m = c.LINE.match(line.strip())
            if m:
                values[m.group(1)] = line.split("=", 1)[1].strip()
        for name in ("INTRON_API_KEY_FILE", "INTRON_API_BASE_URL"):
            self.assertIn(name, values, f"{name} is not named in .env.example")
            self.assertEqual(values[name], "", f"{name} must have no value in the repository")
        # A249: the key comes only from a file mount. There is no environment-variable name for it, so nothing can be written that reads it from the environment.
        self.assertNotIn("INTRON_API_KEY", values)

    def test_no_file_of_the_repository_names_an_intron_endpoint(self):
        import os
        import re

        skip = {".git", "node_modules", "__pycache__", ".venv"}
        found = []
        for base, dirs, files in os.walk(ROOT):
            dirs[:] = [d for d in dirs if d not in skip and not d.startswith(".venv")]  # any virtual environment, including the Makefile's .venv-bench (C2, P3)
            for f in files:
                path = Path(base) / f
                if path.suffix in {".png", ".jpg", ".gif", ".pdf", ".ico", ".woff", ".woff2", ".gz", ".zip", ".pyc", ".exe"} or path.stat().st_size > 2_000_000:
                    continue
                try:
                    text = path.read_text(encoding="utf-8")
                except (UnicodeDecodeError, OSError):
                    continue
                if path.name == "test_check_secret_refs.py":
                    continue
                for number, line in enumerate(text.splitlines(), 1):
                    if names_endpoint(str(path.relative_to(ROOT)), line):
                        found.append(f"{path.relative_to(ROOT)}:{number}")
        self.assertEqual(found, [], "a hostname or URL of the speech provider is in the repository: it belongs in the deployment's settings")

    def test_only_the_docs_notes_file_may_link_documentation_and_never_an_endpoint(self):
        # A268. Hosts here are placeholders built from the word the rule looks for; no real address is written.
        docs = "https://" + "intron" + "health.example/docs/intro"
        in_notes = lambda line: names_endpoint(DOCS_NOTES_FILE, line)  # noqa: E731
        self.assertFalse(in_notes(f"| Streaming | WebSocket only | VERIFIED | [Streaming]({docs}) |"))
        self.assertFalse(in_notes(f"Source: <{docs}#limits>"))
        self.assertTrue(names_endpoint("docs/voice/other.md", f"[Streaming]({docs})"), "another file")
        self.assertTrue(names_endpoint("status/VOI-01.md", f"[x]({docs})"), "the status file")
        self.assertTrue(in_notes("endpoint: https://" + "api.intron" + "health.example/v1/stt"), "an api host")
        self.assertTrue(in_notes("[x](https://" + "inference.intron" + "health.example/stt/v1/stream)"), "an inference host")
        self.assertTrue(in_notes("[x](https://" + "intron" + "health.example:8443/docs)"), "a port")
        self.assertTrue(in_notes("[x](http://" + "intron" + "health.example/docs)"), "plain http")
        self.assertTrue(in_notes("[x](https://" + "intron" + "health.example/docs?key=abc)"), "a query string")
        self.assertTrue(in_notes("use " + "intron" + "example.org directly"), "a bare host outside a link")
        self.assertTrue(in_notes(f"[ok]({docs}) and https://" + "intron" + "health.example/raw"), "a bare URL beside a link")
        # Codex P2 on noema#728: only the matched link target is removed, by position; a bare URL that repeats or extends an allowed target stays an endpoint
        self.assertTrue(in_notes(f"[docs]({docs}) endpoint: {docs}?key=abc"), "a bare URL that extends the allowed target")
        self.assertTrue(in_notes(f"[docs]({docs}) again: {docs}"), "a bare URL that repeats the allowed target")
        self.assertTrue(in_notes(f"{docs} then [docs]({docs})"), "the bare copy comes first")
        self.assertFalse(in_notes(f"[a]({docs}) and [b]({docs}#limits) and <{docs}>"), "the same permitted target linked three times")

    def test_the_real_repository_passes(self):
        self.assertEqual(c.violations(ROOT / ".github/workflows", ROOT / ".env.example"), [])


if __name__ == "__main__":
    unittest.main()
