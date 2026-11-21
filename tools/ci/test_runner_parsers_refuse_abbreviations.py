"""The runner parsers refuse an abbreviated flag (A362; docs#213 item 7).

argparse accepts any unique prefix of a flag unless a parser says `allow_abbrev=False`, so `--harness-c x` would select a configuration through `--harness-config` without the full flag ever appearing
in a workflow. The parsers of the benchmark runner (every non-test module of `benchmark/runner`, subparsers included) and of the compile-acceptance runner (the one that takes `--harness-config`)
turn abbreviations off. The check reads the source, so a parser added later without the setting fails here."""
import ast
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
FILES = sorted(p for p in (ROOT / "benchmark" / "runner").glob("*.py") if not p.name.startswith("test_")) + [ROOT / "benchmark" / "compile_acceptance" / "__main__.py"]


def parser_calls(path: Path) -> list[tuple[int, str, bool]]:
    tree = ast.parse(path.read_text(encoding="utf-8"))
    found = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Call):
            f = node.func
            name = f.attr if isinstance(f, ast.Attribute) else getattr(f, "id", "")
            if name in ("ArgumentParser", "add_parser"):
                kw = {k.arg: getattr(k.value, "value", None) for k in node.keywords}
                found.append((node.lineno, name, kw.get("allow_abbrev") is False))
    return found


class RunnerParsers(unittest.TestCase):
    def test_there_are_parsers_to_read(self) -> None:
        total = sum(len(parser_calls(p)) for p in FILES)
        self.assertGreaterEqual(total, 7, "evidence (1 + 2 subparsers), package_artifacts, tushe_partner_probe, runner and compile_acceptance")
        self.assertIn("evidence.py", [p.name for p in FILES])

    def test_every_parser_turns_abbreviations_off(self) -> None:
        for path in FILES:
            for line, name, off in parser_calls(path):
                self.assertTrue(off, f"{path.relative_to(ROOT).as_posix()}:{line}: {name} must pass allow_abbrev=False")

    def test_the_setting_does_what_it_says(self) -> None:
        import argparse

        loose = argparse.ArgumentParser()
        loose.add_argument("--harness-config")
        self.assertEqual(loose.parse_args(["--harness-c", "x"]).harness_config, "x", "argparse accepts a prefix by default: this is the hole")
        strict = argparse.ArgumentParser(allow_abbrev=False)
        strict.add_argument("--harness-config")
        with self.assertRaises(SystemExit):
            strict.parse_args(["--harness-c", "x"])


if __name__ == "__main__":
    unittest.main()
