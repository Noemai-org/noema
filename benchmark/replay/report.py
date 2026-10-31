"""The human report of a replay diff. It prints the verdict the comparison reached and the numbers behind it, never a pass or fail of its own (BEN-05)."""
from typing import Any

Json = dict[str, Any]


def _cell(value: Any) -> str:
    return str(value).replace("|", "\\|").replace("\n", " ")


def render_markdown(report: Json) -> str:
    counts = report["counts"]
    lines = [
        f"# Replay diff · {report['first']['run_id']} and {report['second']['run_id']}",
        "",
        f"**Verdict: {report['verdict']}.** SYNTHETIC data (AGENT-25). Generated layer only.",
        "",
        "| | |",
        "|---|---|",
        f"| First run | `{report['first']['run_id']}` ({report['first']['status']}) |",
        f"| Second run | `{report['second']['run_id']}` ({report['second']['status']}) |",
        f"| Stories | {', '.join(report['stories'])} |",
        f"| Selection | {report['selection']}" + (f" (excluded: {', '.join(report['excluded_mutated'])})" if report["excluded_mutated"] else "") + " |",
        f"| Generator and seed | {report['generator']['name']} {report['generator']['version']}, seed {report['seed']} |",
        f"| Suite manifest | `{report['suite_manifest_sha256']}` |",
        f"| Commit under test | `{report['commit_sha']}` |",
        f"| Pack and policy | {report['pack_version']}, {report['policy_version']} |",
        f"| n | {counts['selected']} selected, {counts['compared']} compared, {counts['identical']} identical, {counts['different']} different, "
        f"{counts['not_comparable']} not comparable |",
        "",
        "Cases compared per story: " + ", ".join(f"{s} {n}" for s, n in report["stories_covered"].items()) + ".",
        "",
        "## Cases",
        "",
        "| Case | Covers | Mutation | Result | Detail |",
        "|---|---|---|---|---|",
    ]
    for row in report["cases"]:
        if row["differences"]:
            detail = "; ".join(f"{d['field']}: {d['first']} / {d['second']}" for d in row["differences"])
        else:
            detail = row["reason"] or ""
        lines.append(f"| {row['case_id']} | {', '.join(row['covers'])} | {row['mutation'] or ''} | {row['result']} | {_cell(detail)} |")
    lines += ["", "## What was compared", "", "Compared: " + ", ".join(report["compared_fields"]) + ".", "",
              "Not compared: " + ", ".join(report["not_compared"]) + " (they legitimately differ between runs).", "", "## Limits", ""]
    lines += [f"- {text}" for text in report["limitations"]]
    return "\n".join(lines) + "\n"
