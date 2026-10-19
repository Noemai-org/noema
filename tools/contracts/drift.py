"""Enum drift guard (CON-01 C-2; DEC-2026-10-16 money-and-value-lists §2.4).

state-registry.md is the single normative home of the core value lists. This check fails when
contracts/schemas/enums.v1.json and contracts/state-registry.md (a vendored copy) differ in any way:
a value added, removed or renamed, a list with no registry row, or a registry row with no list.
It also checks that money.v1.json agrees with contracts/currencies.json and with the registry's money row.
"""
import json
import re
from pathlib import Path

CONTRACTS = Path(__file__).resolve().parent.parent.parent / "contracts"
REGISTRY = CONTRACTS / "state-registry.md"
PERD_EVENT_TYPES = CONTRACTS / "perd-32-2-event-types.md"

# registry row (start of the first cell) -> (enum definition, how to read the values cell)
ROWS = {
    "Decision Record · `outcome`": ("DecisionOutcome", "all"),
    "Decision Record · `authority`": ("AuthorityResult", "after:result ∈"),
    "Decision Record · `reason_codes`": ("CoreReasonCode", "before:**Pack codes**"),
    "Decision Record · `evidence_freshness[].limit_set_by`": ("LimitSetBy", "all"),
    "Policy rule template · `evaluation_time`": ("EvaluationTime", "all"),
    "Confirmation request · `approver_requirement`": ("ApproverRequirement", "all"),
    "Confirmation event response": ("ConfirmationResponse", "all"),
    "Confirmation request · terminal state on expiry": ("ConfirmationTerminalState", "all"),
    "Evidence item · `fact_status`": ("FactStatus", "all"),
    "Execution Receipt · `execution_state`": ("ExecutionState", "all"),
    "Capability descriptor · `retry_safety`": ("RetrySafety", "all"),
    "Capability descriptor · `concurrency_control`": ("ConcurrencyControl", "all"),
    "Capability descriptor · `consequence_profile` dimensions": ("ConsequenceLevel", "all"),
    "Capability descriptor · `consequence_profile.irreversibility`": ("Irreversibility", "all"),
    "Outcome Verdict · `verdict`": ("Verdict", "all"),
    "Intent Object · `status`": ("IntentStatus", "all"),
    "Finding severity": ("FindingSeverity", "all"),
}
# Definitions that are not value lists of the registry, each with the reason.
NOT_A_REGISTRY_LIST = {
    "DataSensitivity": "the registry gives examples only (state-registry §10); the schema constrains the shape",
    "Mode": "the registry names Mode 1 and Mode 2 in prose (§10); REQ-TRU-011 needs MODE_2 as a legal, rejected value",
}


def parse_rows(text: str) -> dict[str, str]:
    """first cell -> second cell, for every table row whose first cell starts a mapped row."""
    rows = {}
    for line in text.splitlines():
        if not line.startswith("|"):
            continue
        cells = [c.strip() for c in line.strip().strip("|").split(" | ")]
        if len(cells) >= 2:
            rows[cells[0]] = cells[1]
    return rows


def tokens(cell: str, mode: str) -> list[str]:
    if mode.startswith("after:"):
        cell = cell.split(mode[6:], 1)[1]
    elif mode.startswith("before:"):
        cell = cell.split(mode[7:], 1)[0]
    return re.findall(r"`([^`]+)`", cell)


def schema_values(definition: dict) -> list[str]:
    """Values of an enum definition, following oneOf. A ROLE:<role> pattern is read as that token."""
    values = list(definition.get("enum", []))
    for sub in definition.get("oneOf", []):
        values += schema_values(sub)
    pattern = definition.get("pattern", "")
    if pattern.startswith("^ROLE:"):
        values.append("ROLE:<role>")
    return values


def check_lists(enums: dict, registry_text: str) -> list[str]:
    problems = []
    rows = parse_rows(registry_text)
    defs = enums["$defs"]
    mapped = {name for name, _ in ROWS.values()}
    for key, (name, mode) in ROWS.items():
        cell = next((v for k, v in rows.items() if k.startswith(key)), None)
        if cell is None:
            problems.append(f"registry has no row '{key}' (for {name})")
            continue
        if name not in defs:
            problems.append(f"enums.v1.json has no definition {name} for registry row '{key}'")
            continue
        want = tokens(cell, mode)
        have = schema_values(defs[name])
        if len(want) != len(set(want)):
            problems.append(f"{name}: duplicate value in the registry row")
        if sorted(want) != sorted(have):
            problems.append(f"{name}: registry {sorted(set(want) - set(have))} missing from the schema, schema {sorted(set(have) - set(want))} missing from the registry")
    for name in sorted(set(defs) - mapped - set(NOT_A_REGISTRY_LIST)):
        problems.append(f"enums.v1.json defines {name}, which has no registry row (add one, or list it in NOT_A_REGISTRY_LIST with a reason)")
    return problems


def check_money(schemas: dict, registry_text: str) -> list[str]:
    problems = []
    table = json.loads((CONTRACTS / "currencies.json").read_text())["currencies"]
    money = schemas["money.v1.json"]["$defs"]
    if sorted(money["CurrencyCode"]["enum"]) != sorted(table):
        problems.append(f"money.v1.json CurrencyCode {money['CurrencyCode']['enum']} differs from currencies.json {sorted(table)}")
    for code, entry in table.items():
        if not isinstance(entry.get("exponent"), int) or isinstance(entry.get("exponent"), bool) or entry["exponent"] < 0:
            problems.append(f"currencies.json {code}: exponent must be a non-negative integer")
    amount = money["MinorAmount"]
    if (amount.get("type"), amount.get("minimum"), amount.get("maximum")) != ("integer", 0, 9007199254740991):
        problems.append("MinorAmount must be an integer bounded 0..9007199254740991 (M3, M4)")
    money_row = next((v for k, v in parse_rows(registry_text).items() if k.startswith("Currency amount")), "")
    if "9007199254740991" not in money_row:
        problems.append("the registry's money row no longer states the 9007199254740991 ceiling")
    return problems


def perd_event_types(text: str) -> list[str]:
    """Event type names in the PERD §32.2 table. SECURITY_EVENT{kind: ...} and AUDIO_RETAINED{consent_ref} count as their name."""
    names = []
    for line in text.splitlines():
        cells = [c.strip() for c in line.strip().strip("|").split(" | ")] if line.startswith("|") else []
        if len(cells) == 2 and cells[0] not in ("Group", "---"):
            for item in cells[1].split(" · "):
                names.append(re.sub(r"\{.*\}$", "", item.strip()))
    return names


def check_record_event_types(schemas: dict) -> list[str]:
    """The schema's Record event types must be exactly the PERD §32.2 table (no more, no fewer, no duplicates)."""
    want = perd_event_types(PERD_EVENT_TYPES.read_text(encoding="utf-8"))
    have = schemas["record-event.v1.json"]["$defs"]["RecordEventType"]["enum"]
    problems = []
    if not want or len(want) != len(set(want)):
        problems.append("PERD §32.2 extract is empty or lists an event type twice")
    if sorted(want) != sorted(have):
        problems.append(f"RecordEventType: PERD §32.2 {sorted(set(want) - set(have))} missing from the schema, schema {sorted(set(have) - set(want))} missing from PERD §32.2")
    return problems


def check(schemas: dict, _registry) -> list[str]:
    text = REGISTRY.read_text(encoding="utf-8")
    return check_lists(schemas["enums.v1.json"], text) + check_money(schemas, text) + check_record_event_types(schemas)
