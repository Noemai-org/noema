"""`python -m benchmark.generator reproduce`: generate the suite twice, in two separate processes, and compare (BEN-04; G0-N N10 "the fixture run log twice with identical output").

Each run writes into its own empty directory (`<out>/run1`, `<out>/run2`). Every file is compared by sha256; the manifest digest each generator printed is compared too. Any difference, a generator
exit other than 0, or an empty output is a failure: nothing identical is claimed from two empty trees. The summary text holds the two logs, counts and digests, never a generated file's contents. A passing summary names no file; a failing one lists the
differing files by name (names only, never contents; the names can include `oracle/` paths because the oracle documents are generated beside the cases, INV-22). The summary is not scanned:
it is the generator's own output plus digests, and the same text is printed to the job log. Synthetic data only (AGENT-25)."""
import hashlib
import re
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, Mapping

REPO = Path(__file__).resolve().parents[2]
MANIFEST = re.compile(r"manifest (sha256:[0-9a-f]{64})")

Runner = Callable[[int, Path], tuple[int, str]]


def run_generator(seed: int, out: Path) -> tuple[int, str]:
    """One real generation in its own process: (exit code, stdout and stderr)."""
    done = subprocess.run([sys.executable, "-m", "benchmark.generator", "generate", "--seed", str(seed), "--out", str(out)], cwd=REPO, capture_output=True, text=True, check=False)
    return done.returncode, done.stdout + done.stderr


def tree_files(root: Path) -> dict[str, str]:
    """Every file under root, by forward-slash relative path, with the sha256 of its bytes."""
    return {p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(root.rglob("*")) if p.is_file()}


def tree_digest(files: Mapping[str, str]) -> str:
    """sha256 of the lines `<relative path> <sha256>`, sorted by path, joined by a newline, no trailing newline."""
    return hashlib.sha256("\n".join(f"{k} {v}" for k, v in sorted(files.items())).encode()).hexdigest()


@dataclass
class Result:
    seed: int
    logs: list[str] = field(default_factory=list)
    codes: list[int] = field(default_factory=list)
    manifest_digests: list[str] = field(default_factory=list)
    tree_digests: list[str] = field(default_factory=list)
    n_files: int = 0
    problems: list[str] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.problems


def reproduce(seed: int, out: Path, runner: Runner = run_generator) -> Result:
    dirs = [out / "run1", out / "run2"]
    for d in dirs:
        if d.exists() and any(d.iterdir()):
            raise ValueError(f"{d} is not empty: the two runs need empty directories")
        d.mkdir(parents=True, exist_ok=True)
    result = Result(seed)
    trees: list[dict[str, str]] = []
    for i, d in enumerate(dirs, start=1):
        code, log = runner(seed, d)
        result.codes.append(code)
        result.logs.append(log)
        found = MANIFEST.findall(log)
        result.manifest_digests.append(found[-1] if found else "")
        files = tree_files(d)
        trees.append(files)
        result.tree_digests.append(tree_digest(files))
        if code != 0:
            result.problems.append(f"run {i}: the generator exited with code {code}")
        if not files:
            result.problems.append(f"run {i}: no file was written")
        if not found:
            result.problems.append(f"run {i}: the log names no manifest digest")
    result.n_files = len(trees[0])
    if result.manifest_digests[0] != result.manifest_digests[1]:
        result.problems.append(f"the manifest digests differ: {result.manifest_digests[0]!r} and {result.manifest_digests[1]!r}")
    a, b = trees
    for name in sorted(set(a) | set(b)):
        if name not in b:
            result.problems.append(f"{name}: only in run 1")
        elif name not in a:
            result.problems.append(f"{name}: only in run 2")
        elif a[name] != b[name]:
            result.problems.append(f"{name}: the bytes differ between the runs")
    return result


def render(result: Result) -> str:
    """The summary: both logs, digests and counts. No file name appears in a passing summary; a failing one names the differing files so they can be found."""
    lines = [f"noema-casegen reproduce: seed {result.seed}, two separate processes, two empty directories (SYNTHETIC)", ""]
    for i, log in enumerate(result.logs, start=1):
        lines += [f"run {i} (exit {result.codes[i - 1]}):", *["  " + line for line in log.rstrip("\n").split("\n")], ""]
    lines += [f"manifest digests: {', '.join(result.manifest_digests)}", f"tree digests: {', '.join(result.tree_digests)}", f"files: {result.n_files}"]
    if result.ok:
        lines.append("PASS: the two runs are byte-identical")
    else:
        lines.append("FAIL: " + str(len(result.problems)) + " problem(s)")
        lines += ["  " + p for p in result.problems[:50]]
    return "\n".join(lines) + "\n"
