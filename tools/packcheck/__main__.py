"""packcheck validate [--conformance] <pack.json>  (python tools/packcheck validate ...). Exit 1 on any problem."""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import checks  # noqa: E402


def main(argv: list[str]) -> int:
    args = [a for a in argv if a != "--conformance"]
    if len(args) != 2 or args[0] != "validate":
        print("usage: packcheck validate [--conformance] <pack.json>", file=sys.stderr)
        return 2
    path = Path(args[1])
    if not path.exists():
        print(f"packcheck: no such file: {path}", file=sys.stderr)
        return 2
    problems = checks.run(path, conformance="--conformance" in argv)
    for check, message in problems:
        print(f"packcheck FAIL [{check}] {path.name}: {message}")
    if not problems:
        print(f"packcheck OK: {path}")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
