"""packcheck: every pack check, with one code path for the command line, the tests and CI (SoT 15.4, REQ-CAP-001, REQ-AG-001, REQ-AG-002).

Each problem is (check, message). Checks: schema, references, capability, predicate, graph, routing, reasons, conformance.
Not checked here: 'a capability has no benchmark cases' (AGENT-06): no benchmark case set exists yet.
"""
from __future__ import annotations

import importlib.util
import json
import re
import sys
from pathlib import Path

import predicate

ROOT = Path(__file__).resolve().parent.parent.parent
sys.path.insert(0, str(ROOT / "tools/contracts"))  # validate.py imports drift.py from its own folder
_spec = importlib.util.spec_from_file_location("contracts_validate", ROOT / "tools/contracts/validate.py")
validate = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(validate)  # type: ignore[union-attr]

Problem = tuple[str, str]
CONSEQUENCE = ("financial", "customer", "regulatory", "access", "operational")
RANK = {"R0_read": 0, "R0_internal": 0, "R1_append_only": 1, "R2_non_monetary_write": 2, "R3_regulatory_write": 3, "R4_monetary_write": 4}
KIND_RANKS = {"READ": {"R0_read"}, "INTERNAL": {"R0_internal"}, "WRITE": {"R1_append_only", "R2_non_monetary_write", "R3_regulatory_write"}}
FREE_FORM = re.compile(r"[@:/\\ ]|^https?", re.I)
CORE_REASON_CODES: set[str] = set()


def implied_rank(consequence: dict, side_effect: str) -> int:
    """The least risk class a consequence profile allows (SoT 43.8: the declared class may never be lower).

    Reused as ported from the 15 October packcheck (commit ce8897e); the heuristic is not in the SoT or PERD,
    so it is kept exactly and listed as an open decision in the T2 report.
    """
    if side_effect in ("", "NONE"):
        return 0
    if consequence["financial"] != "NONE":
        return 4
    if consequence["regulatory"] == "HIGH":
        return 3
    if side_effect == "APPENDS_RECORD" and consequence["customer"] == "NONE":
        return 1
    if consequence["customer"] != "NONE" or consequence["operational"] != "NONE":
        return 2
    if consequence["regulatory"] == "LOW":
        return 1
    return 0


def load_graphs(pack: dict, base: Path) -> tuple[dict[str, dict], list[Problem]]:
    graphs, problems = {}, []
    schemas = validate.load_schemas()
    registry = validate.make_registry(schemas)
    checker = validate.validator_for("action-graph", schemas, registry)
    for rel in pack.get("action_graph_files", []):
        path = base / rel
        if not path.exists():
            problems.append(("schema", f"graph file {rel} does not exist"))
            continue
        try:
            doc = validate.load_strict(path)
        except ValueError as exc:
            problems.append(("schema", f"{rel}: {exc}"))
            continue
        errs = list(checker.iter_errors(doc))
        problems += [("schema", f"{rel}: {'/'.join(map(str, e.absolute_path)) or '(root)'}: {e.message[:140]}") for e in errs]
        if not errs:
            graphs[rel] = doc
    return graphs, problems


def check_schema(pack: dict) -> list[Problem]:
    schemas = validate.load_schemas()
    registry = validate.make_registry(schemas)
    errs = validate.validator_for("pack", schemas, registry).iter_errors(pack)
    return [("schema", f"{'/'.join(map(str, e.absolute_path)) or '(root)'}: {e.message[:140]}") for e in errs]


def check_references(pack: dict) -> list[Problem]:
    out: list[Problem] = []
    caps = {c["capability_id"] for c in pack["capabilities"]}
    outs = {o["outcome_id"] for o in pack["outcome_definitions"]}
    srcs = {s["source_id"] for s in pack["evidence_sources"]}
    clocks = {c["clock_id"] for c in pack["clocks"]}
    profiles = {t["profile_id"] for t in pack["trust_profiles"]}
    rules = {r["rule_id"] for r in pack["policy"]["rules"]}
    nodes = {n["node_id"] for n in pack["intent_nodes"]}
    codes = {r["code"] for r in pack["reason_catalogue"]}

    def need(kind, ref, known, where):
        if ref not in known:
            out.append(("references", f"{where}: {kind} {ref!r} is not defined in the pack"))

    for n in pack["intent_nodes"]:
        if n["trust_profile"] is not None:
            need("trust profile", n["trust_profile"], profiles, n["node_id"])
        for o in n["outcome_definitions"]:
            need("outcome definition", o, outs, n["node_id"])
        for s in n["slots"]:
            if s.get("references") == "capability" and not caps:
                out.append(("references", f"{n['node_id']}: slot {s['name']} references capabilities but none is registered"))
    for t in pack["trust_profiles"]:
        for kind, ids, known in (("capability", t["prohibited_capabilities"], caps), ("evidence source", t["evidence_required"], srcs),
                                 ("clock", t["clocks"], clocks), ("policy template", t["policy_templates"], rules)):
            for i in ids:
                need(kind, i, known, t["profile_id"])
        for i in t["prohibited_capabilities"]:
            c = next((c for c in pack["capabilities"] if c["capability_id"] == i), None)
            if c is not None and not c["prohibited"]:
                out.append(("references", f"{t['profile_id']}: prohibited_capabilities names {i}, which is registered as allowed"))
    for o in pack["outcome_definitions"]:
        for i in o["intent_nodes"]:
            need("intent node", i, nodes, o["outcome_id"])
        for v in o["verification_sources"]:
            need("evidence source", v["source"], srcs, o["outcome_id"])
        if "sla" in o:
            need("clock", o["sla"]["clock"], clocks, o["outcome_id"])
    for c in pack["capabilities"]:
        need("outcome definition", c["expected_outcome"], outs, c["capability_id"])
        need("evidence source", c["verification"]["source"], srcs, c["capability_id"])
        for i in c["policy_templates"]:
            need("policy template", i, rules, c["capability_id"])
    for r in pack["policy"]["rules"]:
        for i in r["applies_to"]:
            need("capability or intent node", i, caps | nodes, r["rule_id"])
    for d in pack.get("evidence_descriptors", []):
        need("evidence source", d["source"], srcs, f"descriptor {d['entity']}")
    return out


def check_reasons(pack: dict) -> list[Problem]:
    """DEC-2026-10-16 money-and-value-lists 2.3 / 2.4: a pack code stays in the pack's namespace and never reuses a core code."""
    out: list[Problem] = []
    ns = f"{pack['pack_id']}.reason."
    codes = set()
    for r in pack["reason_catalogue"]:
        if not r["code"].startswith(ns):
            out.append(("reasons", f"reason code {r['code']!r} is outside the pack namespace {ns!r}"))
        if r["code"] in CORE_REASON_CODES:
            out.append(("reasons", f"reason code {r['code']!r} reuses a core code"))
        codes.add(r["code"])
    for r in pack["policy"]["rules"]:
        code = r["reason_code"]
        if code in CORE_REASON_CODES:
            continue
        if code not in codes:
            out.append(("reasons", f"{r['rule_id']}: reason code {code!r} is neither a core code nor in the pack's catalogue"))
    return out


def check_capabilities(pack: dict) -> list[Problem]:
    out: list[Problem] = []
    for c in pack["capabilities"]:
        cid, cp = c["capability_id"], c.get("consequence_profile")
        if not cp or any(k not in cp for k in (*CONSEQUENCE, "irreversibility")):
            out.append(("capability", f"{cid}: consequence profile is incomplete"))
            continue
        declared = RANK.get(c["risk_class"])
        if declared is None:
            out.append(("capability", f"{cid}: unknown risk class {c['risk_class']!r}"))
            continue
        implied = implied_rank(cp, c.get("side_effect_class", ""))
        if not c["prohibited"] and declared < implied:
            out.append(("capability", f"{cid}: declared {c['risk_class']} is lower than the class its consequence profile implies (R{implied})"))
        if cp["financial"] != "NONE" and not c["prohibited"] and c.get("concurrency_control") != "CONDITIONAL_WRITE":
            out.append(("capability", f"{cid}: a financial-consequence capability needs CONDITIONAL_WRITE (INV-27)"))
        if declared >= 1 and not c["prohibited"]:
            if not c.get("verification", {}).get("method") or c["verification"]["method"] == "none":
                out.append(("capability", f"{cid}: a write capability needs a verification method"))
            if not c.get("recovery"):
                out.append(("capability", f"{cid}: a write capability needs a recovery list"))
        if c["prohibited"] and (c.get("recovery") or c["verification"]["method"] == "independent_read"):
            out.append(("capability", f"{cid}: a prohibited capability carries no executable path"))
    return out


def predicates_of(pack: dict, graphs: dict[str, dict]):
    for o in pack["outcome_definitions"]:
        yield f"{o['outcome_id']}.success_predicate", o["success_predicate"]
    for r in pack["policy"]["rules"]:
        yield f"{r['rule_id']}.condition", r["condition"]
    for c in pack["capabilities"]:
        for i, p in enumerate(c.get("preconditions", [])):
            yield f"{c['capability_id']}.preconditions[{i}]", p
    for rel, g in graphs.items():
        yield f"{g['graph_id']}.applies_when", g["applies_when"]
        for n in g["nodes"]:
            for i, p in enumerate(n["preconditions"]):
                yield f"{g['graph_id']}.{n['node_id']}.preconditions[{i}]", p
            for i, p in enumerate(n["postconditions"]):
                yield f"{g['graph_id']}.{n['node_id']}.postconditions[{i}]", p
        for e in g["edges"]:
            if e["condition"]:
                yield f"{g['graph_id']}.edge[{e['node_id']}<-{e['depends_on']}].condition", e["condition"]


def check_predicates(pack: dict, graphs: dict[str, dict]) -> list[Problem]:
    items = list(predicates_of(pack, graphs))
    if items and "evidence_descriptors" not in pack:
        return [("predicate", "the pack holds predicates but declares no evidence_descriptors to type-check them against")]
    scope = predicate.Scope.from_pack(pack)
    out: list[Problem] = []
    for where, text in items:
        try:
            t = predicate.check(text, scope)
        except predicate.PredicateError as exc:
            out.append(("predicate", f"{where}: {exc}"))
            continue
        if t != predicate.BOOL:
            out.append(("predicate", f"{where}: must be Boolean, found {t[0]}"))
    return out


def check_graphs(pack: dict, graphs: dict[str, dict]) -> list[Problem]:
    out: list[Problem] = []
    caps = {c["capability_id"]: c for c in pack["capabilities"]}
    for rel, g in graphs.items():
        ids = [n["node_id"] for n in g["nodes"]]
        if len(set(ids)) != len(ids):
            out.append(("graph", f"{g['graph_id']}: duplicate node_id"))
        known = set(ids)
        for n in g["nodes"]:
            where = f"{g['graph_id']}.{n['node_id']}"
            c = caps.get(n["capability_id"])
            if c is None:
                out.append(("graph", f"{where}: capability {n['capability_id']!r} is not registered"))
                continue
            if c["prohibited"]:
                out.append(("graph", f"{where}: capability {n['capability_id']} is PROHIBITED and can never be a node (INV-18)"))
                continue
            if c["risk_class"] not in KIND_RANKS[n["kind"]]:
                out.append(("graph", f"{where}: a {n['kind']} node cannot hold {n['capability_id']} ({c['risk_class']})"))
            if n["kind"] == "WRITE":
                if n["recovery_ref"] is None:
                    out.append(("graph", f"{where}: a WRITE node needs a recovery_ref"))
                elif n["recovery_ref"] not in c.get("recovery", []):
                    out.append(("graph", f"{where}: recovery_ref {n['recovery_ref']!r} is not one of {n['capability_id']}'s recovery strategies"))
                if n["escalation_ref"] is None:
                    out.append(("graph", f"{where}: a WRITE node needs an escalation_ref"))
            elif n["recovery_ref"] is not None or n["escalation_ref"] is not None:
                out.append(("graph", f"{where}: only a WRITE node carries recovery_ref and escalation_ref"))
        adj: dict[str, list[str]] = {i: [] for i in known}
        for e in g["edges"]:
            for end in (e["node_id"], e["depends_on"]):
                if end not in known:
                    out.append(("graph", f"{g['graph_id']}: edge names unknown node {end!r}"))
            if e["node_id"] in known and e["depends_on"] in known:
                adj[e["depends_on"]].append(e["node_id"])
        state: dict[str, int] = {}

        def cyclic(u: str) -> bool:
            state[u] = 1
            for v in adj[u]:
                if state.get(v) == 1 or (state.get(v) is None and cyclic(v)):
                    return True
            state[u] = 2
            return False

        if any(state.get(i) is None and cyclic(i) for i in sorted(known)):
            out.append(("graph", f"{g['graph_id']}: the graph has a cycle"))
    return out


def check_routing(pack: dict) -> list[Problem]:
    """REQ-AG-002: routing targets come only from the pack's allowlist; free-form destinations are not representable."""
    out: list[Problem] = []
    allow = set(pack.get("routing_allowlist", []))
    routes = [c for c in pack["capabilities"] if c["capability_id"].endswith(".route_to_internal_owner") and not c["prohibited"]]
    if routes and not allow:
        out.append(("routing", "ROUTE_TO_INTERNAL_OWNER is registered but the pack declares no routing_allowlist"))
    for r in pack["policy"]["rules"]:
        for key, value in (r.get("params") or {}).items():
            if key == "queue" or key.endswith("_queue"):
                if not isinstance(value, str) or FREE_FORM.search(value):
                    out.append(("routing", f"{r['rule_id']}: {key} is a free-form destination"))
                elif value not in allow:
                    out.append(("routing", f"{r['rule_id']}: {key}={value!r} is not in the routing allowlist"))
    return out


def check_conformance(pack: dict) -> list[Problem]:
    """SoT 15.4 cap: at most 3 intent nodes, 1 trust profile, 1 outcome definition, no adapters, no executed capabilities."""
    out: list[Problem] = []
    if len(pack["intent_nodes"]) > 3:
        out.append(("conformance", "more than 3 intent nodes"))
    if len(pack["trust_profiles"]) != 1:
        out.append(("conformance", "a conformance pack has exactly 1 trust profile"))
    if len(pack["outcome_definitions"]) != 1:
        out.append(("conformance", "a conformance pack has exactly 1 outcome definition"))
    for c in pack["capabilities"]:
        if not c["prohibited"] and c["risk_class"] != "R0_read":
            out.append(("conformance", f"{c['capability_id']}: a conformance pack executes no write capability"))
    return out


def run(pack_path: Path, conformance: bool = False, pack: dict | None = None, graphs_override: dict[str, dict] | None = None) -> list[Problem]:
    global CORE_REASON_CODES
    if not CORE_REASON_CODES:
        enums = validate.load_strict(validate.SCHEMAS / "enums.v1.json")
        CORE_REASON_CODES = set(enums["$defs"]["CoreReasonCode"]["enum"])
    if pack is None:
        try:
            pack = validate.load_strict(pack_path)
        except ValueError as exc:
            return [("schema", str(exc))]
    problems = check_schema(pack)
    graphs, graph_problems = ({}, []) if graphs_override is not None else load_graphs(pack, pack_path.parent)
    if graphs_override is not None:
        graphs = graphs_override
    problems += graph_problems
    if problems:  # the other checks assume a well-formed pack
        return problems
    problems += check_references(pack) + check_reasons(pack) + check_capabilities(pack) + check_predicates(pack, graphs)
    problems += check_graphs(pack, graphs) + check_routing(pack)
    if conformance:
        problems += check_conformance(pack)
    return problems
