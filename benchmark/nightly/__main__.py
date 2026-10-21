"""python -m benchmark.nightly --scorecard FILE --results DIR --run-manifest FILE [--run-url URL] [--dry-run]

Files one issue per failing case that was run, de-duplicated by case ID. A run that was NOT_RUN files nothing and says so. Needs GH_TOKEN (the `gh` CLI) unless --dry-run."""
import argparse
import json
import sys
from pathlib import Path

from benchmark.contracts import load_strict

from .issues import GhCli, file_issues, plan_issues


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="benchmark.nightly", description=__doc__)
    p.add_argument("--run-manifest", type=Path, required=True)
    p.add_argument("--scorecard", type=Path)
    p.add_argument("--results", type=Path)
    p.add_argument("--run-url")
    p.add_argument("--dry-run", action="store_true")
    args = p.parse_args(argv)
    manifest = load_strict(args.run_manifest)
    if manifest["status"] == "NOT_RUN":
        print(f"not run: {manifest.get('not_run_reason')}; no issues are filed and nothing is scored")
        return 0
    if args.scorecard is None or args.results is None:
        print("input error: --scorecard and --results are required for a run that ran")
        return 2
    scorecard = load_strict(args.scorecard)
    results = [json.loads(f.read_text()) for f in sorted(args.results.glob("*.json"))]
    gh = GhCli()
    open_issues = [] if args.dry_run else gh.list_open()
    plan = plan_issues(scorecard, results, open_issues, run_id=manifest["run_id"], commit_sha=manifest.get("commit_sha"), run_url=args.run_url)
    for issue in plan.new:
        print(f"would file {issue.case_id}: {issue.title}" if args.dry_run else f"filing {issue.case_id}")
    for case_id, number in sorted(plan.already_open.items()):
        print(f"{case_id}: already open as #{number}")
    if plan.could_not_run:
        print(f"{sum(plan.could_not_run.values())} cases could not run (not regressions): {dict(sorted(plan.could_not_run.items()))}")
    filed = file_issues(plan, gh, dry_run=args.dry_run)
    for case_id, number in filed.items():
        print(f"filed {case_id} as #{number}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
