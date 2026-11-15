#!/usr/bin/env python3
"""Derive packs/fintech/role_capabilities.json from packs/fintech_v1.json (REQ-CAP-001, REQ-SEC-006; Architect's ruling A3 and noema#341).

The map lists, for each of the four A1 roles (customer, ops, approver, audit), the capabilities its `authority.roles_allowed` names, EXCLUDING every capability marked
`prohibited`: a prohibited capability has no adapter and no callable path (BD-08, REQ-CAP-002), so it is never "permitted", even though the pack names roles for it.
They are listed apart under `prohibited`. A role name outside A1 stops the derivation (A37 maps the older pack names at the benchmark generator only).
Run: python3 tools/derive_role_capabilities.py --check | --write"""
import json
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parent.parent
PACK = ROOT / "packs" / "fintech_v1.json"
OUT = ROOT / "packs" / "fintech" / "role_capabilities.json"
A1_ROLES = ("approver", "audit", "customer", "ops")


def derive(pack: dict[str, Any]) -> dict[str, Any]:
    roles: dict[str, set[str]] = {r: set() for r in A1_ROLES}
    prohibited: set[str] = set()
    for cap in pack["capabilities"]:
        cap_id = cap["capability_id"]
        if cap.get("prohibited"):
            prohibited.add(cap_id)
        for role in cap.get("authority", {}).get("roles_allowed", []):
            if role not in roles:
                raise ValueError(f"{cap_id} names the role {role!r}, which is not one of {A1_ROLES}")
            if not cap.get("prohibited"):
                roles[role].add(cap_id)
    return {
        "schema_version": "1.0",
        "pack_id": pack["pack_id"],
        "label": "SYNTHETIC",
        "derived_from": "packs/fintech_v1.json capabilities[].authority.roles_allowed, without capabilities marked prohibited; regenerate with tools/derive_role_capabilities.py --write",
        "roles": {r: sorted(roles[r]) for r in A1_ROLES},
        "prohibited": sorted(prohibited),
    }


def render(doc: dict[str, Any]) -> str:
    return json.dumps(doc, indent=2) + "\n"


def main(argv: list[str]) -> int:
    doc = derive(json.loads(PACK.read_text()))
    if "--write" in argv:
        OUT.write_text(render(doc))
        return 0
    if OUT.read_text() != render(doc):
        print("packs/fintech/role_capabilities.json is out of date; run: python3 tools/derive_role_capabilities.py --write")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
