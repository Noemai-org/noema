#!/usr/bin/env python3
"""
Noema Pack Checker (packcheck)
Enforces pack invariants per SoT §43.8, §43.9, REQ-CAP-001, REQ-AG-001, PCK-02.
"""

import json
import sys
from pathlib import Path
from typing import Any, Dict, List, Set


REQUIRED_CONSEQUENCE_DIMS = {
    "financial",
    "customer",
    "regulatory",
    "access",
    "operational",
    "irreversibility",
}

VALID_CONSEQUENCE_LEVELS = {"NONE", "LOW", "MEDIUM", "HIGH"}
VALID_IRREVERSIBILITY = {"REVERSIBLE", "COMPENSABLE", "IRREVERSIBLE"}

RISK_RANKS = {
    "R0_read": 0,
    "R0_internal": 0,
    "R1_append_only": 1,
    "R2_non_monetary_write": 2,
    "R3_regulatory_write": 3,
    "R4_monetary_write": 4,
}


def calculate_implied_risk_rank(consequence: Dict[str, str], side_effect_class: str) -> int:
    """Calculate the minimal risk rank implied by a consequence profile."""
    # Pure reads with no side effects have rank 0
    if side_effect_class == "NONE":
        return 0
    # Financial consequence implies R4 (monetary)
    if consequence.get("financial", "NONE") in {"LOW", "MEDIUM", "HIGH"}:
        return 4
    # Regulatory HIGH implies R3 (regulatory write)
    if consequence.get("regulatory", "NONE") == "HIGH":
        return 3
    # Append-only record with no customer impact is R1
    if side_effect_class == "APPENDS_RECORD" and consequence.get("customer", "NONE") == "NONE":
        return 1
    # Customer or operational impact implies at least R2 (non-monetary write)
    if consequence.get("customer") in {"LOW", "MEDIUM", "HIGH"} or consequence.get("operational") in {"LOW", "MEDIUM", "HIGH"}:
        return 2
    if consequence.get("regulatory") == "LOW":
        return 1
    return 0


def validate_pack(data: Dict[str, Any]) -> List[str]:
    errors = []

    # 1. Top level required metadata
    for req_field in ["pack_id", "domain", "version", "capabilities", "action_graph"]:
        if req_field not in data:
            errors.append(f"Missing required top-level field: '{req_field}'")

    if errors:
        return errors

    caps: Dict[str, Any] = data.get("capabilities", {})
    action_graph = data.get("action_graph", {})

    # 2. Capabilities validation
    for cap_id, cap in caps.items():
        if not isinstance(cap, dict):
            errors.append(f"Capability '{cap_id}' must be an object")
            continue

        prohibited = cap.get("prohibited", False)
        risk_class = cap.get("risk_class", "")
        side_effect_class = cap.get("side_effect_class", "")
        consequence = cap.get("consequence_profile")

        # Incomplete consequence profile check
        if not consequence or not isinstance(consequence, dict):
            errors.append(f"Capability '{cap_id}' consequence_profile is missing or invalid")
            continue

        missing_dims = REQUIRED_CONSEQUENCE_DIMS - set(consequence.keys())
        if missing_dims:
            errors.append(
                f"Capability '{cap_id}' consequence_profile missing required dimensions: {sorted(missing_dims)}"
            )

        # Check dimension values
        for dim, val in consequence.items():
            if dim == "irreversibility":
                if val not in VALID_IRREVERSIBILITY:
                    errors.append(f"Capability '{cap_id}' invalid irreversibility level: {val}")
            elif dim in REQUIRED_CONSEQUENCE_DIMS:
                if val not in VALID_CONSEQUENCE_LEVELS:
                    errors.append(f"Capability '{cap_id}' invalid consequence level for {dim}: {val}")

        # Check declared risk_class vs implied consequence profile (REQ-CAP-001)
        declared_rank = RISK_RANKS.get(risk_class, -1)
        if declared_rank == -1:
            errors.append(f"Capability '{cap_id}' declared unknown risk_class: '{risk_class}'")
        else:
            implied_rank = calculate_implied_risk_rank(consequence, side_effect_class)
            if not prohibited and declared_rank < implied_rank:
                errors.append(
                    f"Capability '{cap_id}' declared risk_class '{risk_class}' (rank {declared_rank}) "
                    f"is lower than implied risk from consequence profile (rank {implied_rank})"
                )

        # Financial consequence MUST have CONDITIONAL_WRITE (INV-27)
        if consequence.get("financial", "NONE") in {"LOW", "MEDIUM", "HIGH"}:
            if cap.get("concurrency_control") != "CONDITIONAL_WRITE":
                errors.append(
                    f"Capability '{cap_id}' has financial consequence but lacks CONDITIONAL_WRITE concurrency_control (INV-27)"
                )

        # Non-prohibited Write capabilities must have verification method and recovery list (REQ-CAP-001)
        is_write = declared_rank >= 1
        if is_write and not prohibited:
            ver = cap.get("verification", {})
            if not ver or not ver.get("method") or ver.get("method") == "none":
                errors.append(f"Write capability '{cap_id}' must define a non-empty verification method")

            rec = cap.get("recovery", [])
            if not rec or not isinstance(rec, list) or len(rec) == 0:
                errors.append(f"Write capability '{cap_id}' must define at least one recovery strategy")

    # 3. Action Graph validation (REQ-AG-001)
    nodes = action_graph.get("nodes", [])
    edges = action_graph.get("edges", [])
    node_ids: Set[str] = set()

    for node in nodes:
        nid = node.get("node_id")
        cid = node.get("capability_id")
        if not nid:
            errors.append("Action graph node missing 'node_id'")
            continue
        node_ids.add(nid)

        if not cid or cid not in caps:
            errors.append(f"Action graph node '{nid}' references non-existent capability '{cid}'")
            continue

        # Invariant: NO prohibited capability in Action Graph (INV-18, REQ-AG-001)
        if caps[cid].get("prohibited", False):
            errors.append(
                f"Action graph node '{nid}' references PROHIBITED capability '{cid}' (INV-18 violation)"
            )

    # Validate edges reference valid nodes
    adj: Dict[str, List[str]] = {nid: [] for nid in node_ids}
    for edge in edges:
        f = edge.get("from")
        t = edge.get("to")
        if f not in node_ids:
            errors.append(f"Action graph edge 'from' references unknown node '{f}'")
        if t not in node_ids:
            errors.append(f"Action graph edge 'to' references unknown node '{t}'")
        if f in node_ids and t in node_ids:
            adj[f].append(t)

    # Check for cycles in Action Graph (must be a DAG)
    visited: Dict[str, int] = {}  # 0: unvisited, 1: visiting, 2: visited

    def has_cycle(u: str) -> bool:
        visited[u] = 1
        for v in adj.get(u, []):
            if visited.get(v, 0) == 1:
                return True
            if visited.get(v, 0) == 0 and has_cycle(v):
                return True
        visited[u] = 2
        return False

    for nid in node_ids:
        if visited.get(nid, 0) == 0:
            if has_cycle(nid):
                errors.append("Action graph contains a directed cycle (must be acyclic)")
                break

    return errors


def main():
    if len(sys.argv) < 3 or sys.argv[1] != "validate":
        print("Usage: packcheck validate <path_to_pack.json>")
        sys.exit(1)

    pack_path = Path(sys.argv[2])
    if not pack_path.exists():
        print(f"Error: file not found: {pack_path}")
        sys.exit(1)

    try:
        with open(pack_path, "r", encoding="utf-8") as f:
            data = json.load(f)
    except Exception as e:
        print(f"Error: failed to parse JSON: {e}")
        sys.exit(1)

    errors = validate_pack(data)
    if errors:
        print(f"Pack validation FAILED for {pack_path}:")
        for err in errors:
            print(f"  - {err}")
        sys.exit(2)
    else:
        print(f"Pack validation PASSED for {pack_path}")
        sys.exit(0)


if __name__ == "__main__":
    main()
