"""Cross-field rules of benchmark-fixture.v2 that a JSON Schema cannot state (ruling A6; IL's review on noema#360).

The fixture holds more than one tenant so that a cross-tenant case (X6) names a real object of another tenant. The loader splits the fixture by `tenant_id` and loads each slice into
that tenant's own stand-in instance, so a wrong-tenant mix-up in the fixture would silently defeat the one zero-tolerance gate it exists to test. This module returns every problem it finds;
an empty list means the fixture may be loaded. It reads only the fixture document. SYNTHETIC; generated layer only (INV-22).
"""
from collections import Counter
from collections.abc import Mapping
from typing import Any

Json = dict[str, Any]
OBJECTS = (("principals", "principal_id"), ("beneficiaries", "beneficiary_id"), ("transactions", "ref"), ("cases", "ref"))


def _strings(node: Any) -> list[str]:
    """Every string in a JSON value, nested ones included."""
    if isinstance(node, str):
        return [node]
    if isinstance(node, Mapping):
        return [text for value in node.values() for text in _strings(value)]
    if isinstance(node, list):
        return [text for value in node for text in _strings(value)]
    return []


def problems(doc: Mapping[str, Any]) -> list[str]:
    out: list[str] = []
    tenants = {t["tenant_id"]: t for t in doc["tenants"]}
    if len(tenants) != len(doc["tenants"]):
        out.append("a tenant_id is listed twice")
    if doc["subject_tenant"] not in tenants:
        out.append(f"subject_tenant {doc['subject_tenant']!r} is not a listed tenant")

    owner: dict[str, str] = {}  # id or ref -> tenant_id, over the whole fixture
    for kind, key in OBJECTS:
        counts = Counter(item[key] for item in doc[kind])
        for ident, n in counts.items():
            if n > 1:
                out.append(f"{kind}: {key} {ident!r} appears twice (a ref or id is unique in the whole fixture, not per tenant)")
        for item in doc[kind]:
            if item["tenant_id"] not in tenants:
                out.append(f"{kind} {item[key]!r}: tenant_id {item['tenant_id']!r} is not a listed tenant")
            if item[key] in owner and owner[item[key]] != item["tenant_id"]:
                out.append(f"{item[key]!r} is used by two tenants")
            owner.setdefault(item[key], item["tenant_id"])

    principals = {p["principal_id"]: p["tenant_id"] for p in doc["principals"]}
    beneficiaries = {b["beneficiary_id"]: b["tenant_id"] for b in doc["beneficiaries"]}
    transactions = {t["ref"]: t["tenant_id"] for t in doc["transactions"]}
    for b in doc["beneficiaries"]:
        if b["owner"] not in principals:
            out.append(f"beneficiary {b['beneficiary_id']!r}: owner {b['owner']!r} is not a principal of the fixture")
        elif principals[b["owner"]] != b["tenant_id"]:
            out.append(f"beneficiary {b['beneficiary_id']!r}: owner {b['owner']!r} is in another tenant")
    for t in doc["transactions"]:
        if t["owner"] not in principals:
            out.append(f"transaction {t['ref']!r}: owner {t['owner']!r} is not a principal of the fixture")
        elif principals[t["owner"]] != t["tenant_id"]:
            out.append(f"transaction {t['ref']!r}: owner {t['owner']!r} is in another tenant")
        if t["beneficiary"] is None:
            continue  # a transaction may have no beneficiary
        if t["beneficiary"] not in beneficiaries:
            out.append(f"transaction {t['ref']!r}: beneficiary {t['beneficiary']!r} is not a beneficiary of the fixture")
        elif beneficiaries[t["beneficiary"]] != t["tenant_id"]:
            out.append(f"transaction {t['ref']!r}: beneficiary {t['beneficiary']!r} is in another tenant")
    for c in doc["cases"]:
        if c["transaction"] not in transactions:
            out.append(f"case {c['ref']!r}: transaction {c['transaction']!r} is not a transaction of the fixture")
        elif transactions[c["transaction"]] != c["tenant_id"]:
            out.append(f"case {c['ref']!r}: transaction {c['transaction']!r} is in another tenant")

    markers: dict[str, str] = {}
    for tid, tenant in tenants.items():
        for marker in tenant["markers"]:
            if marker in markers and markers[marker] != tid:
                out.append(f"marker {marker!r} is shared by tenants {markers[marker]!r} and {tid!r}")
            markers.setdefault(marker, tid)

    strings: list[tuple[str, str]] = []  # (tenant_id, text) for every string that belongs to a tenant: every string field of its objects, ids and refs included
    for kind, _ in OBJECTS:
        for item in doc[kind]:
            strings += [(item["tenant_id"], text) for text in _strings(item)]
    objects = {**{k: v for k, v in transactions.items()}, **{c["ref"]: c["tenant_id"] for c in doc["cases"]}, **beneficiaries, **principals}
    for s in doc["enterprise_strings"]:
        if s["target"] not in objects:
            out.append(f"enterprise_strings target {s['target']!r} names no object of the fixture")
        else:
            strings.append((objects[s["target"]], s["text"]))
    for tid, text in strings:
        for marker, mt in markers.items():
            if mt != tid and marker in text:
                out.append(f"marker {marker!r} of tenant {mt!r} appears in a string of tenant {tid!r}")
    return out
