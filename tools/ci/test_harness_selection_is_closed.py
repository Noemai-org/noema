"""No workflow, Compose file or env file can select the Terra harness configuration (A362; REQ-AGT-005, AGENT-16; ruling relayed in docs#213, item 7).

The frozen harness configuration `benchmark/decisions/harness-gpt-6.1-terra.json` is not selectable from anything that starts a run. The guard is CONTENT-based: it does not model one flag syntax, it reads
every workflow (`.yml` and `.yaml`, reusable and composite ones included), every Compose file, every env file, the Makefile and the Compose shell scripts, and fails on any of these, whatever the spelling:
  1. the name Terra anywhere in them;
  2. a `harness-gpt-` name other than the one fixed luna file, or a `harness-` name with an expansion in it;
  3. a `--harness-config` flag, written in full, as an argparse prefix of it (`--harness-c`, `--harn`), quoted, split from its value by a line break, a backslash continuation or a folded YAML scalar,
     whose value is anything but the one fixed luna path (so a variable, a command substitution or an expression in the value is refused, and so is another path);
  4. a YAML key, or an `inputs` reference, with `harness` in its name (a dispatch input, a reusable workflow's input or a `with:` key that could carry the choice).
The runner parsers refuse an abbreviated flag as well (`allow_abbrev=False`; see test_runner_parsers_refuse_abbreviations.py), so a prefix cannot select a configuration there either."""
import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
FIXED = "benchmark/decisions/harness-gpt-6-luna.json"
FLAG_FULL = "--harness-config"

TERRA = re.compile(r"(?i)terra(?!form)")
HARNESS_NAME_BAD = re.compile(r"(?i)harness-gpt-(?!6-luna\.json)|harness-[${`]")
# a flag token that is a prefix of --harness-config (at least `--ha`, so `--help` is not one), optionally quoted, then its value: after white space, line breaks or backslash continuations, or `=`
FLAG = re.compile(r"""["']?(--h[A-Za-z-]{2,})["']?(?:(?:\s|\\(?=\n))+|=)?("[^"]*"|'[^']*'|[^\s\\]\S*)?""")
YAML_KEY = re.compile(r"""(?mi)^\s*(?:-\s+)?["']?[A-Za-z0-9_.-]*harness[A-Za-z0-9_.-]*["']?\s*:""")
INPUT_REF = re.compile(r"""(?i)inputs(?:\.|\[\s*['"])[A-Za-z0-9_.-]*harness""")


def violations(text: str) -> list[str]:
    """What is wrong with one file's text; empty when nothing selects, names or could carry a harness configuration other than the fixed luna one."""
    found = []
    if TERRA.search(text):
        found.append("it names Terra")
    if HARNESS_NAME_BAD.search(text):
        found.append("it names a harness configuration other than the fixed luna file, or builds a harness name with an expansion")
    for m in FLAG.finditer(text):
        token = m.group(1)
        if not FLAG_FULL.startswith(token):
            continue
        value = (m.group(2) or "").strip("\"'")
        if value != FIXED:
            found.append(f"{token} is not given exactly {FIXED}")
    if YAML_KEY.search(text):
        found.append("a YAML key with `harness` in its name (a dispatch, reusable-workflow or with: input could carry the choice)")
    if INPUT_REF.search(text):
        found.append("an inputs reference with `harness` in its name")
    return found


def scanned() -> list[Path]:
    patterns = (".github/**/*.yml", ".github/**/*.yaml", "deploy/**/*.yml", "deploy/**/*.yaml", "deploy/**/.env*", "deploy/**/*.env", "deploy/**/*.sh", ".env*", "Makefile", "*.mk")
    found: set[Path] = set()
    for pattern in patterns:
        found.update(p for p in ROOT.glob(pattern) if p.is_file())
    return sorted(found)


class NoTerraAnywhere(unittest.TestCase):
    def test_there_are_files_to_read(self) -> None:
        names = [p.name for p in scanned()]
        workflows = [p for p in scanned() if ".github/workflows" in p.as_posix()]
        self.assertGreaterEqual(len(workflows), 10, "a glob that finds nothing proves nothing")
        self.assertTrue(any(n.startswith("docker-compose") for n in names) and "bench-stack.yml" in names)
        self.assertIn("Makefile", names)

    def test_no_workflow_compose_or_env_file_selects_or_names_the_terra_harness(self) -> None:
        for path in scanned():
            problems = violations(path.read_text(encoding="utf-8", errors="replace"))
            self.assertEqual(problems, [], f"{path.relative_to(ROOT).as_posix()}: {problems}")

    def test_the_fixed_luna_path_alone_is_accepted(self) -> None:
        for good in (f"python -m x {FLAG_FULL} {FIXED} --out y", f'python -m x {FLAG_FULL} "{FIXED}"', f"python -m x {FLAG_FULL}={FIXED}", f"run: >\n  python -m x {FLAG_FULL}\n    {FIXED} --out y\n",
                     f"python -m x {FLAG_FULL} \\\n  {FIXED} \\\n  --out $OUT\n", "python -m x --help", "python -m x --out y # harness in a comment"):
            self.assertEqual(violations(good), [], good)


class EveryWayAroundIsCaught(unittest.TestCase):
    """The spellings the Codex and Security Reviewer rounds found, and the ones the ruling lists: each must be a violation. The same detector reads the real files."""

    def assert_caught(self, text: str, what: str) -> None:
        self.assertTrue(violations(text), what + ": " + text)

    def test_the_name_in_any_form(self) -> None:
        for text in ("benchmark/decisions/harness-gpt-6.1-terra.json", "model: gpt-6.1-terra", "TERRA=1", "harness-gpt-6.1-terra", "x: Terra", "--model=terra"):
            self.assert_caught(text, "the name")

    def test_another_frozen_file_or_a_built_name(self) -> None:
        for text in ("harness-gpt-7-sol.json", "benchmark/decisions/harness-$H.json", "harness-${{ inputs.x }}.json", "harness-`cat f`.json"):
            self.assert_caught(text, "another name")

    def test_the_flag_in_every_syntax(self) -> None:
        cases = {
            "an expansion in the value": f'{FLAG_FULL} "benchmark/decisions/harness-$HARNESS.json"',
            "a bare variable": f"{FLAG_FULL} $X",
            "another path": f"{FLAG_FULL} benchmark/decisions/other.json",
            "an argparse prefix": f"python -m x --harness-c benchmark/decisions/other.json",
            "a shorter prefix": f"python -m x --harn {FIXED}x",
            "the = form": f"{FLAG_FULL}=benchmark/decisions/$H.json",
            "a quoted flag": f'python -m x "{FLAG_FULL}" "$X"',
            "a single-quoted flag": f"python -m x '{FLAG_FULL}' $X",
            "a quoted prefix": 'python -m x "--harness-conf" "$X"',
            "a value on the next line": f"{FLAG_FULL}\n$X --out y",
            "a backslash continuation": f"{FLAG_FULL} \\\n  benchmark/decisions/harness-$M.json",
            "a folded scalar": f'run: >\n  python -m x {FLAG_FULL}\n    "benchmark/decisions/harness-${{M}}.json"',
            "no value at all": f"python -m x {FLAG_FULL}",
            "an expression": f'{FLAG_FULL} "${{{{ inputs.x }}}}"',
        }
        for what, text in cases.items():
            self.assert_caught(text, what)

    def test_a_key_or_an_input_that_could_carry_the_choice(self) -> None:
        cases = {
            "a dispatch input": "on:\n  workflow_dispatch:\n    inputs:\n      harness:\n        type: string\n",
            "a reusable workflow input": "on:\n  workflow_call:\n    inputs:\n      harness_config:\n        type: string\n",
            "a with key": "jobs:\n  a:\n    uses: ./.github/workflows/x.yml\n    with:\n      harness-config: y\n",
            "an env key": "env:\n  HARNESS: ${{ inputs.x }}\n",
            "an inputs reference": "run: echo ${{ inputs.harness_choice }}\n",
            "a bracket reference": "run: echo ${{ inputs['harness-config'] }}\n",
            "a list item key": "steps:\n  - harness: y\n",
        }
        for what, text in cases.items():
            self.assert_caught(text, what)

    def test_the_guard_finds_a_violation_in_a_real_file_of_each_kind(self) -> None:
        # Not vacuous on the real tree: each kind of file the guard reads, with a violation appended, is a violation.
        real = {p.suffix or p.name: p for p in scanned()}
        for key in (".yml", ".sh", "Makefile"):
            self.assertIn(key, real)
            text = real[key].read_text(encoding="utf-8", errors="replace")
            self.assertEqual(violations(text), [], real[key].as_posix())
            self.assertTrue(violations(text + f"\n{FLAG_FULL} benchmark/decisions/harness-gpt-6.1-terra.json\n"), real[key].as_posix())


if __name__ == "__main__":
    unittest.main()
