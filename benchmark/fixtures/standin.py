"""The data contract of the Noema-side stand-in for the US-01 and US-06 data (Option B for G1, Founder, 4 Nov 2026).

`derive(fixture)` turns a `benchmark-fixture.v1` into the PERD 34.4 normalised payloads the core's adapters will read (`fin.ledger_entry`, `fin.switch_record`,
`fin.destination_status`), plus the principal context and a scoped candidate list. A pure function: no I/O, no clock, the same fixture gives the same bytes. The stand-in
service itself is only a read-store loaded with this output; the meaning of every fixture value lives here, once, and is tested.

Evidence from a run that reads this data is labelled "stand-in" and is not evidence about Tushe; the same scenarios rerun against live Tushe once its OpenAPI is frozen. Field
names follow the PERD 34.4 [REC] shapes, which are fixed when the Tushe OpenAPI is frozen: this is the stand-in's reading, not Tushe's. SYNTHETIC (AGENT-25)."""
from datetime import datetime, timedelta
from typing import Any

Json = dict[str, Any]
LABEL = "stand-in"
CLOCK_START = "2026-10-01T09:40:00+01:00"  # the same start as benchmark/runner/faultplan.py, SYNTHETIC

# fixture value -> (semantic_status, is_terminal, raw_code). The raw code is labelled SYNTHETIC and carries no real switch meaning.
SWITCH = {"APPROVED": ("ACCEPTED", True, "SYN-00"), "DECLINED": ("REJECTED", True, "SYN-05"), "TIMED_OUT": ("TIMED_OUT", False, "SYN-91"), "PENDING": ("PENDING", False, "SYN-09")}
LEDGER = ("POSTED", "NOT_POSTED", "REVERSED", "UNAVAILABLE")
DESTINATION = ("CREDITED", "NOT_CREDITED", "PENDING", "UNAVAILABLE")
SETTLEMENT = ("SETTLED", "PENDING", "REVERSAL_PENDING", "REVERSED", "UNAVAILABLE")


def _need(value: str, allowed: Any, what: str) -> str:
    if value not in allowed:
        raise ValueError(f"unknown {what} status {value!r}; the stand-in refuses to guess")
    return value


def _at(minutes_before: int) -> str:
    return (datetime.fromisoformat(CLOCK_START) - timedelta(minutes=minutes_before)).isoformat()


def _ledger(tx: Json) -> Json:
    status = _need(tx["ledger"], LEDGER, "ledger")
    if status == "UNAVAILABLE":
        return {"unavailable": True, "entries": []}
    kind = "DEBIT" if tx["direction"] == "OUTGOING" else "CREDIT"
    base = {"account_ref": f"acc_{tx['owner']}", "transaction_ref": tx["ref"], "amount_minor": tx["amount_minor"], "currency": tx["currency"], "posted_at": _at(tx["age_minutes"])}
    if status == "NOT_POSTED":
        entries: list[Json] = []
    elif status == "POSTED":
        entries = [{**base, "entry_id": f"le_{tx['ref'].split(':')[-1]}_1", "entry_type": kind, "posting_status": "POSTED", "version": 1}]
    else:
        entries = [{**base, "entry_id": f"le_{tx['ref'].split(':')[-1]}_1", "entry_type": kind, "posting_status": "REVERSED", "version": 2},
                   {**base, "entry_id": f"le_{tx['ref'].split(':')[-1]}_2", "entry_type": "REVERSAL", "posting_status": "POSTED", "version": 1,
                    "posted_at": _at(max(tx["age_minutes"] - 1, 0))}]
    return {"unavailable": False, "entries": entries}


def _switch(tx: Json) -> Json:
    status = _need(tx["switch"], (*SWITCH, "UNAVAILABLE"), "switch")
    if status == "UNAVAILABLE":
        return {"unavailable": True}
    semantic, terminal, raw = SWITCH[status]
    return {"unavailable": False, "switch_ref": f"sw_{tx['ref'].split(':')[-1]}", "transaction_ref": tx["ref"], "raw_code": raw, "raw_code_label": "SYNTHETIC",
            "semantic_status": semantic, "is_terminal": terminal, "destination_ack": tx["destination"] == "CREDITED", "received_at": _at(tx["age_minutes"]),
            "raw_payload_hash": "sha256:" + "0" * 64}


def _destination(tx: Json) -> Json:
    credit, settlement = _need(tx["destination"], DESTINATION, "destination"), _need(tx["settlement"], SETTLEMENT, "settlement")
    if "UNAVAILABLE" in (credit, settlement):
        return {"unavailable": True, "transaction_ref": tx["ref"], "credit_state": credit, "settlement_state": settlement, "effective_at": _at(tx["age_minutes"]), "observed_at": CLOCK_START}
    return {"unavailable": False, "transaction_ref": tx["ref"], "credit_state": credit, "settlement_state": settlement, "effective_at": _at(tx["age_minutes"]), "observed_at": CLOCK_START}


def derive(fixture: Json) -> Json:
    transactions = fixture["transactions"]
    principals = {p["principal_id"]: {"display_name": p["display_name"], "roles": list(p["roles"]), "accounts": [f"acc_{p['principal_id']}"],
                                      "beneficiaries": [b for b in fixture["beneficiaries"] if b["owner"] == p["principal_id"]]} for p in fixture["principals"]}
    candidates: dict[str, list[Json]] = {pid: [] for pid in principals}
    for tx in transactions:
        if tx["owner"] not in candidates:
            raise ValueError(f"transaction {tx['ref']} belongs to {tx['owner']}, who is not a principal of the fixture")
        candidates[tx["owner"]].append({"transaction_ref": tx["ref"], "account_ref": f"acc_{tx['owner']}", "direction": tx["direction"], "amount_minor": tx["amount_minor"],
                                        "currency": tx["currency"], "counterparty": tx["beneficiary"], "occurred_at": _at(tx["age_minutes"])})
    return {
        "data_source": LABEL, "synthetic": True, "fixture_id": fixture["fixture_id"], "clock_start": CLOCK_START, "existing_cases": len(fixture["cases"]),
        "principals": principals, "candidates": candidates, "candidate_lists_complete": True,
        "ledger_entries": {tx["ref"]: _ledger(tx) for tx in transactions},
        "switch_records": {tx["ref"]: _switch(tx) for tx in transactions},
        "destination_statuses": {tx["ref"]: _destination(tx) for tx in transactions},
    }
