"""packcheck validate [--conformance] [--templates-required] <pack.json>  (python tools/packcheck validate ...). Exit 1 on any problem.
packcheck manifest <manifest.json> [<pack-dir>]: the pack manifest (PCK-02, A333); <pack-dir> defaults to the folder above the manifest's folder (packs/)."""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import checks  # noqa: E402

USAGE = "usage: packcheck validate [--conformance] [--templates-required] <pack.json> | packcheck manifest <manifest.json> [<pack-dir>]"


def report(path: Path, problems: list[tuple[str, str]]) -> int:
    for check, message in problems:
        print(f"packcheck FAIL [{check}] {path.name}: {message}")
    if not problems:
        print(f"packcheck OK: {path}")
    return 1 if problems else 0


def manifest(args: list[str]) -> int:
    if len(args) not in (1, 2):
        print(USAGE, file=sys.stderr)
        return 2
    path = Path(args[0])
    if not path.is_file():
        print(f"packcheck: no such file: {path}", file=sys.stderr)
        return 2
    root = Path(args[1]) if len(args) == 2 else path.resolve().parent.parent
    if not root.is_dir():
        print(f"packcheck: no such pack directory: {root}", file=sys.stderr)
        return 2
    return report(path, checks.check_manifest(path, root))


def main(argv: list[str]) -> int:
    if argv and argv[0] == "manifest":
        return manifest(argv[1:])
    args = [a for a in argv if a not in ("--conformance", "--templates-required")]
    if len(args) != 2 or args[0] != "validate":
        print(USAGE, file=sys.stderr)
        return 2
    path = Path(args[1])
    if not path.exists():
        print(f"packcheck: no such file: {path}", file=sys.stderr)
        return 2
    return report(path, checks.run(path, conformance="--conformance" in argv, templates="enforce" if "--templates-required" in argv else "off"))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
