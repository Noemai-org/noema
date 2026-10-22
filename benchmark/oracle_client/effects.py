"""What the case system really applied, read from the truth endpoint (REQ-ENV-002): the oracle's answer to "did a write happen", never the response a caller saw."""
from typing import Any

Json = dict[str, Any]

# mock-case operation -> the pack capability it carries out (packs/fintech_v1.json; a test pins these names to the pack).
CAPABILITY_OF_OPERATION = {
    "create_case": "fin.cap.create_escalation_case",
    "append_evidence": "fin.cap.record_investigation_evidence",
    "route_case": "fin.cap.route_to_internal_owner",
    "create_complaint": "fin.cap.log_complaint_reference",
}
_ROUTE_OK_BELOW = 300


def _new(before: list[Json], after: list[Json], key: str) -> list[Json]:
    seen = {row[key] for row in before}
    return [row for row in after if row[key] not in seen]


def effects_since(before: Json, after: Json, commands: list[Json]) -> list[Json]:
    """The writes applied since `before`, as {capability, target, count} sorted by capability and target.

    A new case, evidence attachment or complaint is the state difference, so a commit whose response was lost still counts and an
    idempotent replay does not. A route changes no row that says so, so it comes from the command log: committed, not a replay,
    and answered with a success status (a refused stale-version route is not an effect). The target of every effect is the
    transaction the case is about."""
    cases = {c["case_id"]: c["transaction_ref"] for c in [*before["cases"], *after["cases"]]}
    counts: dict[tuple[str, str], int] = {}

    def add(operation: str, target: str | None) -> None:
        if target is not None:
            key = (CAPABILITY_OF_OPERATION[operation], target)
            counts[key] = counts.get(key, 0) + 1

    for case in _new(before["cases"], after["cases"], "case_id"):
        add("create_case", case["transaction_ref"])
    for attachment in _new(before["evidence"], after["evidence"], "attachment_id"):
        add("append_evidence", cases.get(attachment["case_id"]))
    for complaint in _new(before["complaints"], after["complaints"], "complaint_ref"):
        add("create_complaint", cases.get(complaint["case_id"]))
    for command in commands:
        if command["operation"] == "route_case" and command["committed"] and not command["replay"] and command["status_code"] < _ROUTE_OK_BELOW:
            add("route_case", cases.get(command["case_id"] or ""))
    return [{"capability": cap, "target": target, "count": n} for (cap, target), n in sorted(counts.items())]
