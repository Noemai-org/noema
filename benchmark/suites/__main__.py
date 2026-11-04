"""python -m benchmark.suites list|run NAME --suite DIR  |  g1 --suite DIR --out DIR --run-id ID [--reset] [--out DIR] [--run-id ID] [--commit-sha SHA] [--reset-done] [-- runner args]   (BEN-29)

`list` prints the case ids a named suite selects and their digest. `run` is the one command per suite: select, run the selection through the runner (the public API only),
score exactly those cases, and write `suite-run.json` with the run ID, the selection digest, the suite manifest digest, the seed and the commit (REQ-BEN-004, REQ-TST-005).
`--reset-done` states that the caller ran `make reset` first; it is recorded, not checked, and a run without it says so. Nothing is faked: with no environment the run is
NOT_RUN and exits 3. Exit codes follow the runner: 0 complete and Tier 0 all zero, 1 incomplete or a gate not zero, 2 bad input, 3 not run, no access.
Generated layer only; refuses any argument or environment value that names `benchmark-heldout` (INV-22, REQ-AGT-004)."""
import argparse
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

from benchmark.contracts import pretty_bytes
from benchmark.suites.select import SelectionError, load_families, load_suites, select, selection_digest
from benchmark.suites.subset import subset_suite

Json = dict[str, Any]
FORBIDDEN = "benchmark-heldout"
SUITES = Path(__file__).parent / "suites.json"
PACK = Path(__file__).resolve().parents[2] / "packs" / "fintech_v1.json"


def runner_command(suite: Path, out: Path, run_id: str, only_file: Path, passthrough: list[str]) -> list[str]:
    return [sys.executable, "-m", "benchmark.runner", "--suite", str(suite), "--out", str(out), "--run-id", run_id, "--only-file", str(only_file), *passthrough]


def scoring_command(subset: Path, results: Path, run_id: str, out: Path) -> list[str]:
    return [sys.executable, "-m", "benchmark.scoring", "--cases", str(subset / "cases"), "--oracles", str(subset / "oracles"), "--results", str(results),
            "--pack", str(PACK), "--manifest", str(subset / "manifest.json"), "--run-id", run_id, "--out", str(out)]


def _refused(argv: list[str]) -> list[str]:
    return [a for a in argv if FORBIDDEN in a] + [f"${k}" for k, v in os.environ.items() if FORBIDDEN in v]


def _select(args: argparse.Namespace) -> tuple[Json, list[str], Json]:
    manifest_path = Path(args.suite) / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    suites = load_suites(SUITES)
    if args.name not in suites:
        raise SelectionError(f"no suite named {args.name} (known: {', '.join(sorted(suites))})")
    families = load_families(Path(args.suite))
    return manifest, select(manifest, suites[args.name], args.name, families), suites[args.name]


def _run(args: argparse.Namespace, passthrough: list[str]) -> int:
    manifest, ids, _ = _select(args)
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    only_file = out / "selection.txt"
    only_file.write_text("\n".join(ids) + "\n", encoding="utf-8")
    manifest_bytes = (Path(args.suite) / "manifest.json").read_bytes()
    record: Json = {
        "suite": args.name, "run_id": args.run_id, "status": "NOT_RUN", "reset_done": bool(args.reset_done), "selected": len(ids),
        "selection_digest": selection_digest(ids), "suite_manifest_sha256": "sha256:" + hashlib.sha256(manifest_bytes).hexdigest(),
        "seed": manifest["generator"]["seed"], "generator": manifest["generator"]["name"] + " " + manifest["generator"]["version"],
        "commit_sha": args.commit_sha, "runner_exit": None, "score_exit": None,
    }
    commit = ["--commit-sha", args.commit_sha] if args.commit_sha else []
    ran = subprocess.run(runner_command(Path(args.suite), out / "run", args.run_id, only_file, [*commit, *passthrough]), check=False)
    record["runner_exit"] = ran.returncode
    if ran.returncode == 3:
        (out / "suite-run.json").write_bytes(pretty_bytes(record))
        print(f"suite {args.name}: not run, no access (run {args.run_id}); see {out / 'run' / 'run-manifest.json'}")
        return 3
    if ran.returncode >= 2:
        record["status"] = "INPUT_ERROR"
        (out / "suite-run.json").write_bytes(pretty_bytes(record))
        return int(ran.returncode)
    subset_suite(Path(args.suite), ids, out / "subset", args.name)
    scored = subprocess.run(scoring_command(out / "subset", out / "run" / "results", args.run_id, out / "score"), check=False)
    record["score_exit"] = scored.returncode
    record["status"] = "COMPLETE" if ran.returncode == 0 and scored.returncode == 0 else "INCOMPLETE_OR_GATE_NOT_ZERO"
    (out / "suite-run.json").write_bytes(pretty_bytes(record))
    print(f"suite {args.name}: run {args.run_id}, {len(ids)} cases, status {record['status']}; record {out / 'suite-run.json'}")
    return int(max(ran.returncode, scored.returncode))


def _g1(args: argparse.Namespace, passthrough: list[str]) -> int:
    import importlib.util

    from benchmark.suites.g1 import evidence_record, run_g1
    out = Path(args.out)

    def run_leg(leg: str, leg_out: Path) -> int:
        argv = ["run", "g1", "--suite", args.suite, "--out", str(leg_out), "--run-id", f"{args.run_id}-{leg}"]
        argv += ["--commit-sha", args.commit_sha] if args.commit_sha else []
        argv += ["--reset-done"] if args.reset else []
        return main(argv + (["--"] + passthrough if passthrough else []))

    def run_replay(first: Path, second: Path, replay_out: Path) -> int:
        if importlib.util.find_spec("benchmark.replay") is None:
            print("error: benchmark.replay is not in this checkout (noema#102 adds it); the G1 diff cannot be made", file=sys.stderr)
            return 2
        return subprocess.run([sys.executable, "-m", "benchmark.replay", "--suite", args.suite, "--first", str(first), "--second", str(second), "--story", "US-01",
                               "--story", "US-06", "--exclude-mutated", "--out", str(replay_out)], check=False).returncode

    def reset() -> int:
        return subprocess.run(["make", "reset"], check=False).returncode

    code = run_g1(out, run_leg, run_replay, reset if args.reset else None)
    replay_json = out / "replay" / "replay.json"
    report = json.loads(replay_json.read_text(encoding="utf-8")) if replay_json.is_file() else None
    record = evidence_record(args.data_source, args.run_id, code, report)
    out.mkdir(parents=True, exist_ok=True)
    (out / "g1-evidence.json").write_bytes(pretty_bytes(record))
    print(record["headline"])
    return code


def main(argv: list[str] | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    passthrough: list[str] = []
    if "--" in argv:
        split = argv.index("--")
        argv, passthrough = argv[:split], argv[split + 1:]
    refused = _refused(argv + passthrough)
    if refused:
        print(f"refused: {', '.join(refused)} names the held-out set, which this tool never reads (INV-22, REQ-AGT-004)", file=sys.stderr)
        return 2
    parser = argparse.ArgumentParser(prog="benchmark.suites", description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    for command in ("list", "run", "g1"):
        p = sub.add_parser(command)
        if command != "g1":
            p.add_argument("name", help="us, chaos, evil, full or g1 (benchmark/suites/suites.json)")
        p.add_argument("--suite", required=True, help="a suite root written by `python -m benchmark.generator generate`")
        if command == "g1":
            p.add_argument("--out", required=True)
            p.add_argument("--run-id", required=True, help="the pair's id; the legs are <id>-a and <id>-b")
            p.add_argument("--commit-sha", default=None)
            p.add_argument("--reset", action="store_true", help="run `make reset` before each leg and record reset_done")
            p.add_argument("--data-source", required=True, choices=["stand-in", "tushe"],
                           help="where the scenario data came from; the evidence is labelled with it (Option B: stand-in, rerun against Tushe once its OpenAPI is frozen)")
        if command == "run":
            p.add_argument("--out", required=True)
            p.add_argument("--run-id", required=True)
            p.add_argument("--commit-sha", default=None)
            p.add_argument("--reset-done", action="store_true", help="the caller ran `make reset` first (recorded, not checked)")
    args = parser.parse_args(argv)
    try:
        if args.command == "list":
            _, ids, _ = _select(args)
            print("\n".join(ids))
            print(f"{len(ids)} cases; {selection_digest(ids)}")
            return 0
        if args.command == "g1":
            return _g1(args, passthrough)
        return _run(args, passthrough)
    except (SelectionError, ValueError, OSError, KeyError, json.JSONDecodeError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
