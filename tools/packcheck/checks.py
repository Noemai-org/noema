"""packcheck: every pack check, with one code path for the command line, the tests and CI (SoT 15.4, REQ-CAP-001, REQ-AG-001, REQ-AG-002).

Each problem is (check, message). Checks: schema, references, capability, predicate, graph, routing, disambiguation, roles, reasons, templates, conformance.
Not checked here: 'a capability has no benchmark cases' (AGENT-06): no benchmark case set exists yet.
"""
from __future__ import annotations

import fnmatch
import hashlib
import importlib.util
import json
import os
import re
import sys
import unicodedata
import zoneinfo
from fractions import Fraction
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
# A555: what a candidate label may show. The fields are the candidates contract's (the allowed list is derived, so a field added there is never shown by accident: it must be allowed here
# by name); the candidate id, the resource reference and the alias flag are never in a label (REQ-AMB-004, AGENT-13). The currency is shown with the amount and is not asked by itself.
_CANDIDATE_FIELDS = frozenset(json.loads((ROOT / "contracts/schemas/candidates-response.v1.json").read_text())["$defs"]["Candidate"]["properties"])
LABEL_ATTRIBUTES = _CANDIDATE_FIELDS - {"candidate_id", "resource_ref", "beneficiary_alias_match"}
ASKABLE_ATTRIBUTES = LABEL_ATTRIBUTES - {"currency"}
LABEL_MEMBERS = ("options_key", "display_template", "attribute_order")
PLACEHOLDER = re.compile(r"\{attr:([a-z][a-z0-9_]*)\}")


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
    checker = validate.validator_for("action-graph.v2", schemas, registry)
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


def check_compensation(pack: dict) -> list[Problem]:
    """REQ-CAP-004 (A631, A647): recovery is declared, never improvised. A REVERSIBLE or COMPENSABLE capability declares a compensation; an IRREVERSIBLE one declares ESCALATE_ONLY and
    nothing else; an INVERSE_CAPABILITY names a capability that is itself registered, not prohibited and not the capability itself; a prohibited capability declares no compensation and
    has no adapter (BD-08, REQ-CAP-002). The shape (the three values, the mapping that goes with an inverse) is capability-descriptor.v2's."""
    out: list[Problem] = []
    caps = {c["capability_id"]: c for c in pack["capabilities"]}
    for cid, c in caps.items():
        if c["prohibited"]:
            if "compensation" in c or "inverse_mapping" in c:
                out.append(("compensation", f"{cid}: a prohibited capability declares no compensation and has no adapter"))
            continue
        declared = c.get("compensation")
        irreversibility = (c.get("consequence_profile") or {}).get("irreversibility")
        if declared is None:
            out.append(("compensation", f"{cid}: a {irreversibility or 'registered'} capability needs a declared compensation"))
            continue
        if irreversibility == "IRREVERSIBLE" and declared != "ESCALATE_ONLY":
            out.append(("compensation", f"{cid}: an IRREVERSIBLE capability declares ESCALATE_ONLY and nothing else (it declares {declared})"))
        if declared == "INVERSE_CAPABILITY":
            inverse = (c.get("inverse_mapping") or {}).get("capability_id")
            if inverse == cid:
                out.append(("compensation", f"{cid}: the inverse capability is itself"))
            elif inverse not in caps:
                out.append(("compensation", f"{cid}: the inverse capability {inverse!r} is not registered in the pack"))
            elif caps[inverse]["prohibited"]:
                out.append(("compensation", f"{cid}: the inverse capability {inverse} is prohibited"))
    return out


def check_tenant_zone(pack: dict) -> list[Problem]:
    """A647, A650 (REQ-DATA-002): `tenant_zone` is an IANA zone name the tz database knows. The synthetic tenant uses Africa/Lagos. Fails closed: with no tz database to ask, a zone is refused."""
    zone = pack.get("tenant_zone")
    if zone is None:
        return []
    try:
        zoneinfo.ZoneInfo(zone)
    except (zoneinfo.ZoneInfoNotFoundError, ValueError, OSError):
        return [("tenant_zone", f"tenant_zone {zone!r} is not an IANA zone name the tz database knows")]
    return []


def check_graph_link(pack: dict, graphs: dict[str, dict]) -> list[Problem]:
    """A645, A647: an intent node's `action_graph` names a graph the pack declares."""
    known = {g["graph_id"] for g in graphs.values()}
    return [("graph_link", f"{n['node_id']}: action_graph {n['action_graph']!r} is not a graph the pack declares")
            for n in pack["intent_nodes"] if "action_graph" in n and n["action_graph"] not in known]


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


def slot_type(slot: dict) -> predicate.Type:
    """The predicate type of an intent node's slot: a Reference with a closed vocabulary is an Enum of it, any other slot is opaque (compared only with null or, for a Reference, a string)."""
    if slot["type"] == "Reference":
        return ("Enum", frozenset(slot["vocabulary"])) if slot.get("vocabulary") else predicate.REF
    return (slot["type"],)


def check_write_when(pack: dict) -> list[Problem]:
    """A364 (interim, relayed): an intent node's `write_when` is a predicate over the node's own slots (an absent slot is null): it parses, is Boolean and names only those slots."""
    out: list[Problem] = []
    for n in pack["intent_nodes"]:
        if "write_when" not in n:
            continue
        where = f"{n['node_id']}.write_when"
        try:
            t = predicate.check(n["write_when"], predicate.Scope({}, {s["name"]: slot_type(s) for s in n["slots"]}))
        except predicate.PredicateError as exc:
            out.append(("predicate", f"{where}: {exc}"))
            continue
        if t != predicate.BOOL:
            out.append(("predicate", f"{where}: must be Boolean, found {t[0]}"))
    return out


def check_write_when_link(pack: dict, graphs: dict[str, dict]) -> list[Problem]:
    """REQ-TRU-004, REQ-CAP-001 (SR's request on noema#935; INV-01): the Writes rule reads an intent node with no `write_when` as never writing, so the pack data alone decides the
    direction that draws less scrutiny. A node whose `action_graph` calls an allowed capability that writes (a risk class above R0) therefore carries a `write_when`. A node with no
    graph, or a graph that only reads, needs none. A graph the pack does not declare is `graph_link`'s to refuse."""
    caps = {c["capability_id"]: c for c in pack["capabilities"]}
    by_id = {g["graph_id"]: g for g in graphs.values()}
    out: list[Problem] = []
    for n in pack["intent_nodes"]:
        graph = by_id.get(n.get("action_graph"))
        if graph is None or "write_when" in n:
            continue
        writers = sorted({node["capability_id"] for node in graph["nodes"]
                          if node["capability_id"] in caps and not caps[node["capability_id"]]["prohibited"] and RANK.get(caps[node["capability_id"]]["risk_class"], 0) >= 1})
        if writers:
            out.append(("write_when", f"{n['node_id']}: its action graph {graph['graph_id']} calls {', '.join(writers)}, which write, so the node needs a write_when (a node with none is read as never writing)"))
    return out


def check_recon_rules(doc: object) -> list[Problem]:
    """A659 (interim, relayed, not yet committed; REQ-RES-005): the pack's rules file is a `recon-rule-set.v1` document. From version 2 each compared pair and each single-valued field
    declares `convergence_window_seconds` (at least 0, A708); version 1 declares none. A message names the place and the rule, never a value of the document. That a window equals the smaller
    freshness limit of its pair (A641) is not checked here: it needs the evidence mapping's entity-to-source link."""
    schemas = validate.load_schemas()
    errs = validate.validator_for("recon-rule-set", schemas, validate.make_registry(schemas)).iter_errors(doc)
    found = sorted({(("/".join(map(str, e.absolute_path)) or "(root)"), e.message if e.validator == "required" else f"fails {e.validator}") for e in errs})
    return [("recon_rules", f"{where}: {what}") for where, what in found[:20]]


def check_outcome_bindings(pack: dict, pack_dir: Path) -> list[Problem]:
    """A723 (interim, relayed; REQ-VER-001, REQ-EXE-006; INV-14): the pack's optional `outcome_bindings`. A binding is of a declared, non-prohibited write capability (a risk class above R0),
    once; it names that capability's own `expected_outcome` and `retry_safety`; each variable is one the pack declares in `predicate_variables` and the outcome's success predicate reads;
    and a `PLAN_PARAM:<param>` source names an input of the capability's own input schema. A message names the place and the rule, never a value. A pack with none is valid."""
    out: list[Problem] = []
    caps = {c["capability_id"]: c for c in pack["capabilities"]}
    outcomes = {o["outcome_id"]: o for o in pack["outcome_definitions"]}
    declared = pack.get("predicate_variables", {})
    seen: set[str] = set()
    for b in pack.get("outcome_bindings", []):
        cap_id = b["capability_id"]
        where = f"outcome_bindings[{cap_id}]"
        if cap_id in seen:
            out.append(("outcome_bindings", f"{where}: bound twice"))
        seen.add(cap_id)
        cap = caps.get(cap_id)
        if cap is None:
            out.append(("outcome_bindings", f"{where}: the capability is not declared"))
            continue
        if cap["prohibited"] or RANK.get(cap["risk_class"], 0) < 1:
            out.append(("outcome_bindings", f"{where}: only a declared, non-prohibited write capability has a binding"))
            continue
        if b["outcome_id"] != cap["expected_outcome"]:
            out.append(("outcome_bindings", f"{where}: outcome_id is not the capability's expected_outcome"))
        if b["retry_safety"] != cap["retry_safety"]:
            out.append(("outcome_bindings", f"{where}: retry_safety is not the capability's own"))
        predicate = (outcomes.get(b["outcome_id"]) or {}).get("success_predicate", "")
        try:
            inputs = set(validate.load_strict(pack_dir / cap["input_schema"]).get("properties", {}))
        except (OSError, ValueError, KeyError):
            inputs = None
        for name, source in sorted(b["variables"].items()):
            if name not in declared:
                out.append(("outcome_bindings", f"{where}: {name} is not a predicate variable the pack declares"))
            elif not re.search(rf"(?<![\w.]){re.escape(name)}(?![\w])", predicate):
                out.append(("outcome_bindings", f"{where}: the outcome's success predicate never reads {name}"))
            if source.startswith("PLAN_PARAM:") and inputs is not None and source.split(":", 1)[1] not in inputs:
                out.append(("outcome_bindings", f"{where}: {name}: the param is not an input of {cap_id}"))
    return out


def check_subject_slot(pack: dict) -> list[Problem]:
    """A520 (interim; DEBT-54): an intent node's optional `subject_slot` names a Resource slot of the SAME node, the slot whose resolved reference is the subject of the intent (the
    core reads the name from the pack, INV-14). A node with no such slot declares none."""
    out: list[Problem] = []
    for n in pack["intent_nodes"]:
        if "subject_slot" not in n:
            continue
        name = n["subject_slot"]
        slot = next((s for s in n["slots"] if s["name"] == name), None)
        if slot is None:
            out.append(("subject", f"{n['node_id']}: subject_slot {name!r} is not a slot of the node"))
        elif slot["type"] != "Resource":
            out.append(("subject", f"{n['node_id']}: subject_slot {name!r} must be a Resource slot, found {slot['type']}"))
    return out


def check_graph_params(pack: dict, graphs: dict[str, dict], pack_dir: Path) -> list[Problem]:
    """A369 (interim, relayed): a node that declares `params` declares exactly the inputs of its capability (every required input covered, no extra), once each; its slots and map keys
    are the intent nodes' own, its map values are `case_reasons`, its facts are declared predicate variables and its policy params are carried by a PERMIT rule of the capability (and
    an owner_queue is in the routing allowlist). A node without `params` declares none (absent means no params)."""
    out: list[Problem] = []
    out += [("reasons", f"case_reasons: duplicate code {c!r}") for c in sorted({r["code"] for r in pack.get("case_reasons", [])}) if [x["code"] for x in pack["case_reasons"]].count(c) > 1]
    codes = {r["code"] for r in pack.get("case_reasons", [])}
    slots = {s["name"]: s for n in pack["intent_nodes"] for s in n["slots"]}
    caps = {c["capability_id"]: c for c in pack["capabilities"]}
    allow = set(pack.get("routing_allowlist", []))
    for g in graphs.values():
        for n in g["nodes"]:
            if "params" not in n:
                continue
            where = f"{g['graph_id']}.{n['node_id']}"
            names = [p["name"] for p in n["params"]]
            out += [("graph", f"{where}: declares param {d!r} more than once") for d in sorted({x for x in names if names.count(x) > 1})]
            cap = caps.get(n["capability_id"])
            if cap is None:
                continue
            path = pack_dir / cap["input_schema"]
            if not path.is_file():
                out.append(("graph", f"{where}: the input schema {cap['input_schema']} of {cap['capability_id']} does not exist"))
                continue
            schema = validate.load_strict(path)
            out += [("graph", f"{where}: required input {r!r} of {cap['capability_id']} has no param") for r in schema.get("required", []) if r not in names]
            out += [("graph", f"{where}: param {x!r} is not an input of {cap['capability_id']}") for x in sorted(set(names)) if x not in schema.get("properties", {})]
            for p in n["params"]:
                if "slot" in p and p["slot"] not in slots:
                    out.append(("graph", f"{where}: param {p['name']!r}: {p['slot']!r} is not a slot of any intent node"))
                if "map" in p:
                    m = p["map"]
                    if m["from"] not in slots:
                        out.append(("graph", f"{where}: param {p['name']!r}: {m['from']!r} is not a slot of any intent node"))
                    else:
                        vocab = set(slots[m["from"]].get("vocabulary", []))
                        out += [("graph", f"{where}: param {p['name']!r}: map key {k!r} is not in the vocabulary of slot {m['from']!r}") for k in m["values"] if k not in vocab]
                    out += [("graph", f"{where}: param {p['name']!r}: map value {v!r} is not in case_reasons") for v in m["values"].values() if v not in codes]
                if "fact" in p and p["fact"] not in pack.get("predicate_variables", {}):
                    out.append(("graph", f"{where}: param {p['name']!r}: fact {p['fact']!r} is not a declared predicate variable"))
                if "policy_param" in p:
                    carriers = [r for r in pack["policy"]["rules"] if r["effect"] == "PERMIT" and cap["capability_id"] in r["applies_to"] and p["policy_param"] in r.get("params", {})]
                    if not carriers:
                        out.append(("graph", f"{where}: param {p['name']!r}: no PERMIT rule of {cap['capability_id']} carries policy param {p['policy_param']!r}"))
                    if p["policy_param"] == "owner_queue":
                        out += [("graph", f"{where}: policy owner_queue {r['params']['owner_queue']!r} of {r['rule_id']} is not in routing_allowlist") for r in carriers if r["params"]["owner_queue"] not in allow]
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
        out += check_graph_shape(g, known)
    return out


def check_graph_permits(pack: dict, graphs: dict[str, dict]) -> list[Problem]:
    """Every capability a graph node names has a PERMIT rule in the policy (REQ-TRU-005; PERD 23.3 T11). Trust asks the policy for each capability of a plan, and a
    capability with no PERMIT ends NO_PERMITTING_RULE, so such a node could never be planned. Whether a rule's condition holds is decided at run time, not here."""
    permitted = {c for r in pack["policy"]["rules"] if r["effect"] == "PERMIT" for c in r["applies_to"]}
    return [("graph", f"{g['graph_id']}.{n['node_id']}: capability {n['capability_id']} has no PERMIT rule in the policy")
            for g in graphs.values() for n in g["nodes"] if n["capability_id"] not in permitted]


def check_graph_shape(g: dict, known: set[str]) -> list[Problem]:
    """A graph, never a list of endpoints (PERD v1.19 REQ-AG-001, DEC-2026-10-31 D9; roots and connectedness committed in
    DEC-2026-11-03-action-graph-shape D1 to D4). Edges naming unknown nodes are reported by check_graphs and ignored here."""
    out: list[Problem] = []
    gid = g["graph_id"]
    roots = set(g["roots"])
    for r in sorted(roots - known):
        out.append(("graph", f"{gid}: root {r!r} is not a node"))
    edges = [e for e in g["edges"] if e["node_id"] in known and e["depends_on"] in known]
    has_incoming = {e["node_id"] for e in edges}
    for n in g["nodes"]:
        where = f"{gid}.{n['node_id']}"
        if n["node_id"] not in roots and n["node_id"] not in has_incoming:
            out.append(("graph", f"{where}: not a declared root and has no incoming edge (depends_on)"))
        if n["node_id"] in roots and n["node_id"] in has_incoming:
            out.append(("graph", f"{where}: is a root and has an incoming edge; a root has none (DEC-2026-11-03-action-graph-shape D3)"))
        if not n["postconditions"]:
            out.append(("graph", f"{where}: has no postcondition"))
        if n["kind"] == "WRITE" and not n["preconditions"]:
            out.append(("graph", f"{where}: a WRITE node needs a precondition"))
    if len(known) > 1:
        component = {i: i for i in known}

        def find(x: str) -> str:
            while component[x] != x:
                component[x] = component[component[x]]
                x = component[x]
            return x

        for e in edges:
            component[find(e["node_id"])] = find(e["depends_on"])
        if len({find(i) for i in known}) > 1:
            out.append(("graph", f"{gid}: the graph is not connected by its edges (a list of endpoints is not a graph)"))
    return out


A1_ROLES = frozenset({"customer", "ops", "approver", "audit"})


def check_approver_roles(pack: dict) -> list[Problem]:
    """A377 (interim, relayed): ROLE:<role> names one of the four A1 roles. Read from a policy rule's approver_requirement and a trust profile's approvals."""
    out: list[Problem] = []
    found = [(f"{r['rule_id']}.approver_requirement", (r.get("params") or {}).get("approver_requirement")) for r in pack["policy"]["rules"]]
    found += [(f"{t['profile_id']}.approvals", a) for t in pack["trust_profiles"] for a in t.get("approvals", [])]
    for where, value in found:
        if isinstance(value, str) and value.startswith("ROLE:") and value[len("ROLE:"):] not in A1_ROLES:
            out.append(("roles", f"{where}: {value!r} names a role outside {sorted(A1_ROLES)}"))
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
    out += check_handoff_routes(pack, allow)
    return out


def check_handoff_routes(pack: dict, allow: set[str]) -> list[Problem]:
    """A531 (interim, relayed, not yet committed): a handoff route names a queue of the pack's own allowlist, a reason code at most once, and only intent nodes the pack declares.
    That the reason code is a core code is the schema's (the CoreReasonCode enum, check `schema`), so it is not checked twice."""
    out: list[Problem] = []
    nodes = {n["node_id"] for n in pack["intent_nodes"]}
    seen: set[str] = set()
    for route in pack.get("handoff_routes", []):
        code, queue = route["reason_code"], route["owner_queue"]
        if code in seen:
            out.append(("routing", f"handoff route {code!r} is declared twice"))
        seen.add(code)
        if queue not in allow:
            out.append(("routing", f"handoff route {code!r}: queue {queue!r} is not in the routing allowlist"))
        out += [("routing", f"handoff route {code!r}: intent node {n!r} is not declared by the pack") for n in route.get("intent_nodes", []) if n not in nodes]
    return out


def _label_template_problems(where: str, text: str, allowed: frozenset[str]) -> list[str]:
    """One label template: literal text and `{attr:<field>}` placeholders. Every placeholder names an allowed candidate field, there is at least one, no two touch (two numbers side by
    side could make a longer digit run than the masking rule allows) and the literal text holds no digit at all, in any script (so nothing in a label can be a number of the pack's own)."""
    out: list[str] = []
    names = PLACEHOLDER.findall(text)
    literal = PLACEHOLDER.sub("", text)
    if not names:
        out.append(f"{where}: the template names no {{attr:<field>}} placeholder")
    out += [f"{where}: {{attr:{n}}} is not a field this template may show" for n in names if n not in allowed]
    if "{" in literal or "}" in literal:
        out.append(f"{where}: a brace that is not a whole {{attr:<field>}} placeholder")
    if any(unicodedata.category(ch) in ("Nd", "Nl", "No") for ch in literal):  # a number of any kind: digits of any script, superscripts, circled digits, fractions, numerals
        out.append(f"{where}: the literal text holds a digit")
    if any(unicodedata.category(ch) in ("Cc", "Cf") for ch in text):  # a control, zero-width or bidi character could keep two placeholders apart for a program and together for a customer
        out.append(f"{where}: the template holds a control or invisible character")
    if re.search(r"\}\{", text):
        out.append(f"{where}: two placeholders side by side")
    return out


def check_disambiguation(pack: dict) -> list[Problem]:
    """A555 (REQ-AMB-004): a node that searches candidates declares the three label members together or none: the question key for OPTIONS mode, the OPTIONS label template and the fixed
    order of askable attributes, each with its own question key and label template. Names are the candidates contract's labelable fields, listed once; question keys are listed once and the
    OPTIONS key is none of them; an attribute's label shows only that attribute (the amount also shows its currency); the display shows any labelable field."""
    out: list[Problem] = []
    for node in pack["intent_nodes"]:
        d = node.get("disambiguation") or {}
        present = [m for m in LABEL_MEMBERS if m in d]
        if not present:
            continue
        where = f"{node['node_id']}: disambiguation"
        if len(present) != len(LABEL_MEMBERS):
            out.append(("disambiguation", f"{where}: declares {present} without {[m for m in LABEL_MEMBERS if m not in d]}; the three go together"))
            continue
        if not any(s.get("resolution") == "candidate_search" for s in node["slots"]):
            out.append(("disambiguation", f"{where}: the node has no candidate_search slot, so it has no candidate labels"))
        problems = [f"{where}.display_template: {m}" for m in _label_template_problems("template", d["display_template"], LABEL_ATTRIBUTES)]
        keys = [d["options_key"]]
        names: list[str] = []
        for i, a in enumerate(d["attribute_order"]):
            at = f"{where}.attribute_order[{i}]"
            if a["name"] not in ASKABLE_ATTRIBUTES:
                problems.append(f"{at}: {a['name']!r} is not an attribute a question may ask by")
            names.append(a["name"])
            keys.append(a["question_key"])
            problems += [f"{at}: {m}" for m in _label_template_problems("label_template", a["label_template"], frozenset({a["name"]} | ({"currency"} if a["name"] == "amount_minor" else set())))]
        problems += [f"{where}.attribute_order: {n!r} is listed twice" for n in sorted({n for n in names if names.count(n) > 1})]
        problems += [f"{where}: the question key {k!r} is used twice (options_key and attribute_order share one set)" for k in sorted({k for k in keys if keys.count(k) > 1})]
        out += [("disambiguation", m) for m in problems]
    return out


def check_understanding(pack: dict, pack_dir: Path, conformance: bool = False) -> list[Problem]:
    """REQ-AMB-003: a pack that ranks candidates carries its ranking data (missing weights fail pack validation at load), the weights sum to
    exactly 1, the floor and the tolerance lie in [0, 1], each ranking node names a matcher file that exists inside the pack's folder, and no
    locale is listed twice. Numbers are exact: they are strings, compared as fractions, never floats (INV-21).

    A conformance pack (SoT 15.4) exists to show the engine loads a pack of another industry with no code change. It carries no ranking data and
    no matcher yet, so the presence rules are skipped for it; whatever it does carry is still checked. That is a gap, raised in the pull request."""
    out: list[Problem] = []
    ranking_nodes = [n for n in pack["intent_nodes"] if (n.get("disambiguation") or {}).get("strategy") == "ranked_candidates"]
    und = pack.get("understanding")
    if ranking_nodes and not und and not conformance:
        out.append(("understanding", "a node ranks candidates but the pack has no understanding.ranking data"))
    for n in ([] if conformance else ranking_nodes):
        ref = n["disambiguation"].get("matcher_ref")
        if not ref:
            out.append(("understanding", f"{n['node_id']}: a node that ranks candidates needs a matcher_ref"))
        elif not (pack_dir / ref).is_file():
            out.append(("understanding", f"{n['node_id']}: matcher_ref {ref!r} is not a file in the pack folder"))
    if not und:
        return out
    ranking = und["ranking"]
    total = sum(Fraction(v) for v in ranking["weights"].values())
    if total != 1:
        out.append(("understanding", f"ranking weights sum to {total}, they must sum to exactly 1"))
    for key in ("absolute_floor", "amount_tolerance_ratio"):
        if not 0 <= Fraction(ranking[key]) <= 1:
            out.append(("understanding", f"{key} lies outside [0, 1]"))
    codes = [loc["code"] for loc in und["locales"]]
    for code in sorted({c for c in codes if codes.count(c) > 1}):
        out.append(("understanding", f"duplicate locale {code}"))
    return out + check_slot_gates(pack, und)


def check_slot_gates(pack: dict, und: dict) -> list[Problem]:
    """UND-18 (A603, interim; AGENT-16): `understanding.slot_gates` gates OPTIONAL slots that have a closed list of values (a `vocabulary`, or `references`). The threshold range is the
    schema's (integer basis points, 1 to 10000). Here: the slot exists in some node, no declaration of it is required, every declaration has a closed list, and a slot is gated once.
    A gate on a required slot would let doubt drop a value the node cannot do without; one without a closed list has nothing to pick from."""
    out: list[Problem] = []
    gates = und.get("slot_gates") or []
    declared: dict[str, list[dict]] = {}
    for node in pack["intent_nodes"]:
        for slot in node.get("slots", []):
            declared.setdefault(slot["name"], []).append(slot)
    names = [g["slot"] for g in gates]
    for name in sorted({n for n in names if names.count(n) > 1}):
        out.append(("understanding", f"slot gate on {name} is declared twice"))
    for name in dict.fromkeys(names):
        slots = declared.get(name)
        if not slots:
            out.append(("understanding", f"slot gate on {name}: no node declares such a slot"))
            continue
        if sum(1 for node in pack["intent_nodes"] if any(s["name"] == name for s in node.get("slots", []))) > 1:
            out.append(("understanding", f"slot gate on {name}: the slot is declared by more than one node; a gate belongs to one node"))
        if any(s.get("required") is True for s in slots):
            out.append(("understanding", f"slot gate on {name}: the slot is required in a node; only an optional slot may be gated"))
        if any(s.get("vocabulary") is None and s.get("references") is None for s in slots):
            out.append(("understanding", f"slot gate on {name}: the slot has no closed list of values to pick from"))
    return out


def pack_slug(pack: dict) -> str:
    """The pack's own folder under packs/: the first path component of its action graph files (fintech/graph/...)."""
    return Path(pack["action_graph_files"][0]).parts[0]


def template_locales(pack: dict) -> list[str]:
    """The locales the pack declares (understanding.locales), in the order it lists them."""
    return [loc["code"] for loc in (pack.get("understanding") or {}).get("locales", [])]


def required_template_keys(pack: dict, pack_dir: Path) -> list[str]:
    """Every <outcome_id>/<classification> a fallback template must exist for (REQ-EXP-002, PERD 22.3). The classifications are the ones the
    pack's reconciliation rules can produce: each rule's classification plus the fallback's. They are pack data, never listed in code."""
    rules = json.loads((pack_dir / pack_slug(pack) / "recon_rules" / "rules.v1.json").read_text())
    classes = sorted({r["classification"] for r in rules["rules"]} | {rules["fallback"]["classification"]})
    return [f"{o['outcome_id']}/{c}" for o in pack["outcome_definitions"] for c in classes]


# The parts of the customer's answer besides its summary (A271): a template for a part is keyed <outcome_id>/<classification>/<part>. The closed list is the Go loader's
# (services/core/explanation Parts); a test in that package compares the two. A part is optional for now: coverage of every part is required when the Founder-approved wording lands.
TEMPLATE_PARTS = ("found", "not_known_what", "not_known_why", "advice_do", "advice_do_not", "next_what", "next_when")


def check_templates(pack: dict, pack_dir: Path) -> list[Problem]:
    """REQ-EXP-002: one JSON file per locale, <pack>/templates/<locale>.json, an object keyed <outcome_id>/<classification> whose values are
    non-empty text. A missing file, a missing key, an empty text, a key that is not required and a file that is not JSON each fail, and each
    names the locale and the key. Coverage is exact: 100% of the required keys in every locale."""
    out: list[Problem] = []
    folder = pack_dir / pack_slug(pack) / "templates"
    required = set(required_template_keys(pack, pack_dir))
    for loc in template_locales(pack):
        path = folder / f"{loc}.json"
        if not path.is_file():
            out.append(("templates", f"{loc}: template file {path.name} is missing"))
            continue
        try:
            doc = json.loads(path.read_text())
        except ValueError:
            out.append(("templates", f"{loc}: {path.name} is not valid JSON"))
            continue
        if not isinstance(doc, dict):
            out.append(("templates", f"{loc}: {path.name} must be an object keyed <outcome_id>/<classification>"))
            continue
        for key in sorted(required - doc.keys()):
            out.append(("templates", f"{loc}: no template for {key}"))
        allowed = required | {f"{k}/{p}" for k in required for p in TEMPLATE_PARTS}
        for key in sorted(doc.keys() - allowed):
            out.append(("templates", f"{loc}: template key {key} is not an outcome/classification (or one of its parts) of this pack"))
        for key in sorted(k for k in doc.keys() & allowed if not isinstance(doc[k], str) or not doc[k].strip()):
            out.append(("templates", f"{loc}: the template for {key} is empty"))
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


MANIFEST_SCHEMA = ROOT / "packs/schemas/manifest.v1.json"
# Where each kind of file the manifest lists lives, relative to the pack directory: (folder, glob). A kind is every file the glob finds under the
# folder (recursively), so a second mapping or rule set cannot sit beside the pinned one unlisted. Only a kind with an entry in the manifest is
# checked for unlisted files; the manifest must list exactly one file of each of these.
MANIFEST_KINDS = {
    "pack": ("", "fintech_v*.json"),
    "evidence_mapping": ("fintech/evidence_mapping", "*.json"),
    "recon_rules": ("fintech/recon_rules", "*.json"),
    "action_graph": ("fintech/graph", "*.json"),  # A510
    "limits": ("fintech/limits", "*.json"),  # A544: every limits file of the folder is pinned
    "recipient_matcher": ("fintech/lexicons", "recipient_matcher*.json"),  # A710: exactly one from manifest version 5, none or one below it; the folder also holds the pack's other lexicons, so only a file of the matcher's own name stem is of this kind
    "injection_patterns": ("fintech/context", "*.json"),  # A645, A664: exactly one from manifest version 4, none or one below it, always with its expected id
    "priority_routing": ("fintech/routing", "*.json"),  # PCK-09, A644: at most one
}
# The kinds the manifest lists exactly once. `action_graph` is listed once for each path the pack file names in `action_graph_files` (A510), and for no other.
# `injection_patterns` is not here: A645 pins the injection-screen patterns too, with `expected.injection_patterns_id`, and A664 (interim, the A579 pattern) makes it required only from manifest
# version MANIFEST_INJECTION_FROM_VERSION: exactly one file from that version, none or one below it (see `_injection_problems`).
SINGLE_KINDS = ("pack", "evidence_mapping", "recon_rules")
# `limits` is listed once for each file of the pack's limits folder (A544) and may be listed for none: the kind is additive.
MANIFEST_INJECTION_FROM_VERSION = 4  # A664 (interim): the first manifest version that must list the injection patterns
MANIFEST_MATCHER_FROM_VERSION = 5  # A710 (A695, OD-P-117): the first manifest version that must list the recipient matcher's data
# `priority_routing` is listed at most once (PCK-09, A644); the pack that lists it is held to its schema and to the pack's own phases and queues.
OPTIONAL_KINDS = ("priority_routing",)
PRIORITY_ROUTING_SCHEMA = ROOT / "packs/schemas/priority_routing.v1.json"
WINDOWS_DRIVE = re.compile(r"^[A-Za-z]:")


def _shown(rel: str) -> str:
    return rel if rel.isprintable() else repr(rel)


def _manifest_path_problem(rel: str) -> str | None:
    """Why a manifest path is unsafe, or None. Independent of the schema: this is the rule that keeps a read inside the pack directory."""
    if rel.startswith("/") or rel.startswith("\\") or WINDOWS_DRIVE.match(rel) or os.path.isabs(rel):
        return "escapes the pack directory (absolute path)"
    if ".." in rel.split("/") or ".." in rel.split("\\"):
        return "escapes the pack directory ('..' component)"
    parts = rel.split("/")
    if rel == "" or "\\" in rel or "\x00" in rel or "\n" in rel or "~" in rel[:1] or any(p in ("", ".") for p in parts):
        return "is not a plain relative path"
    return None


def _symlink_in(root: Path, rel: str) -> str | None:
    """The first component of root/rel that is a symlink (the root itself is the caller's mount and is not examined)."""
    cur = root
    for part in rel.split("/"):
        cur = cur / part
        if cur.is_symlink():
            return str(cur.relative_to(root))
    return None


def _manifest_id(doc: dict, id_key: str) -> str | None:
    value, version = doc.get(id_key), doc.get("version")
    if not isinstance(value, str) or isinstance(version, bool) or not isinstance(version, int):
        return None
    return f"{value}@{version}"


def _graph_set_problems(pinned: list[dict], pack_file: Path | None) -> list[Problem]:
    """A510: the action graphs the manifest pins are exactly the ones the verified pack file names in `action_graph_files`, so no graph is served unpinned and none is pinned for nothing."""
    if pack_file is None:
        return []
    try:
        body = json.loads(pack_file.read_text(encoding="utf-8"))
    except (ValueError, UnicodeDecodeError):
        return [("manifest", "the pack file is not valid JSON, so the graphs it names cannot be read")]
    named = body.get("action_graph_files") if isinstance(body, dict) else None
    if named is None:
        named = []
    if not isinstance(named, list) or not all(isinstance(n, str) for n in named):
        return [("manifest", "the pack file's action_graph_files is not a list of paths")]
    out: list[Problem] = []
    if len(set(named)) != len(named):
        out.append(("manifest", "the pack file names an action graph twice"))
    pinned_paths = {f["path"] for f in pinned}
    for rel in sorted(set(named) - pinned_paths):
        out.append(("manifest", f"{_shown(rel)} is an action graph the pack names and the manifest does not pin"))
    for rel in sorted(pinned_paths - set(named)):
        out.append(("manifest", f"{_shown(rel)} is pinned as an action graph and the pack does not name it"))
    return out


MANIFEST_LIMITS_FROM_VERSION = 3  # A579 (interim): the first manifest version that must list the limits files


def _matcher_problems(doc) -> list[Problem]:
    """A710 (the A579 and A664 pattern): from manifest version MANIFEST_MATCHER_FROM_VERSION the manifest lists exactly one `recipient_matcher` file; below it none or one; never two. A manifest below the gate
    that lists none carries no matcher data, and the core then builds no matcher from the pack. The schema says the same; this names the version and the rule."""
    if not isinstance(doc, dict):
        return []
    version, files = doc.get("version"), doc.get("files")
    if not isinstance(version, int) or isinstance(version, bool) or not isinstance(files, list):
        return []  # the schema names these
    listed = sum(1 for f in files if isinstance(f, dict) and f.get("kind") == "recipient_matcher")
    if version >= MANIFEST_MATCHER_FROM_VERSION and listed != 1:
        return [("manifest", f"a manifest of version {version} must list exactly one recipient_matcher file (A710); it lists {listed}")]
    if version < MANIFEST_MATCHER_FROM_VERSION and listed > 1:
        return [("manifest", f"a manifest of version {version} lists none or one recipient_matcher file (A710); it lists {listed}")]
    return []


def _injection_problems(doc) -> list[Problem]:
    """A664 (interim; the A579 pattern): from manifest version MANIFEST_INJECTION_FROM_VERSION the manifest lists exactly one `injection_patterns` file and names `expected.injection_patterns_id`; below it
    both may be absent. The file and its id go together at every version, and no manifest lists two files of the kind. A manifest below the gate that lists none carries no pattern data, and the core then
    builds no screen from the pack (there is no empty Detector). The schema says the same; this names the version and the rule."""
    if not isinstance(doc, dict):
        return []
    version, files, expected = doc.get("version"), doc.get("files"), doc.get("expected")
    if not isinstance(version, int) or isinstance(version, bool) or not isinstance(files, list):
        return []  # the schema names these
    listed = sum(1 for f in files if isinstance(f, dict) and f.get("kind") == "injection_patterns")
    named = isinstance(expected, dict) and "injection_patterns_id" in expected
    gated = version >= MANIFEST_INJECTION_FROM_VERSION
    out: list[Problem] = []
    if gated and listed != 1:
        out.append(("manifest", f"a manifest of version {version} must list exactly one injection_patterns file (A664); it lists {listed}"))
    elif not gated and listed > 1:
        out.append(("manifest", f"a manifest of version {version} lists none or one injection_patterns file (A664); it lists {listed}"))
    if listed and not named:
        out.append(("manifest", "the manifest lists an injection_patterns file but names no expected.injection_patterns_id"))
    elif named and not listed:
        out.append(("manifest", "the manifest names expected.injection_patterns_id but lists no injection_patterns file"))
    elif gated and not named:
        out.append(("manifest", f"a manifest of version {version} must name expected.injection_patterns_id (A664)"))
    return out


def check_manifest(manifest_path: Path, root: Path) -> list[Problem]:
    """PCK-02 (A333, EVD-06 hash-pin of A320): the pack manifest lists the pack files the core loads with their SHA-256 and names the expected
    evidence-mapping and rule-set ids. Fails when a listed file's SHA-256 differs or the file is missing, a file of a listed kind is not listed,
    an id the manifest names is not the id the file carries, or a path escapes the pack directory (`..`, absolute, a symlink). `root` is the pack
    directory (`packs/`); paths are relative to it. Never reads a path that failed the escape rules."""
    try:
        doc = validate.load_strict(manifest_path)
    except ValueError as exc:
        return [("manifest", f"{manifest_path.name} is not valid JSON: {exc}")]
    # The escape rules run first and name the path; the schema also refuses these paths, so its duplicate complaint about the same path is dropped.
    out: list[Problem] = []
    named: set[tuple[str, ...]] = set()
    raw = doc.get("files") if isinstance(doc, dict) else None
    for i, f in enumerate(raw if isinstance(raw, list) else []):
        why = _manifest_path_problem(f["path"]) if isinstance(f, dict) and isinstance(f.get("path"), str) else None
        if why:
            out.append(("manifest", f"{_shown(f['path'])} {why}"))
            named.add(("files", str(i), "path"))
    # A579 (interim; SOT-C-33): a manifest of version 3 or later lists its limits files; there is no opt-in. The schema says the same; this names the version.
    version = doc.get("version") if isinstance(doc, dict) else None
    if isinstance(version, int) and not isinstance(version, bool) and version >= MANIFEST_LIMITS_FROM_VERSION and isinstance(raw, list) \
            and not any(isinstance(f, dict) and f.get("kind") == "limits" for f in raw):
        out.append(("manifest", f"a manifest of version {version} must list its limits files (A579); it lists none"))
        named.add(("files",))
    out += _injection_problems(doc)
    out += _matcher_problems(doc)
    errs = sorted(validate.StrictValidator(validate.load_strict(MANIFEST_SCHEMA)).iter_errors(doc), key=lambda e: list(map(str, e.absolute_path)))
    errs = [e for e in errs if tuple(map(str, e.absolute_path)) not in named]
    if errs or out:
        return out + [("manifest", f"{'/'.join(map(str, e.absolute_path)) or '(root)'}: {e.message[:140]}") for e in errs]
    files = doc["files"]
    by_kind: dict[str, list[dict]] = {k: [f for f in files if f["kind"] == k] for k in MANIFEST_KINDS}
    for kind, entries in by_kind.items():
        if kind in SINGLE_KINDS and len(entries) != 1:
            out.append(("manifest", f"the manifest must list exactly one {kind} file, it lists {len(entries)}"))
        if kind in OPTIONAL_KINDS and len(entries) > 1:
            out.append(("manifest", f"the manifest must list at most one {kind} file, it lists {len(entries)}"))

    seen: set[str] = set()
    readable: dict[str, Path] = {}
    for f in files:
        rel = f["path"]
        if rel in seen:
            out.append(("manifest", f"{rel} is listed twice"))
            continue
        seen.add(rel)
        why = _manifest_path_problem(rel)
        if why:
            out.append(("manifest", f"{_shown(rel)} {why}"))
            continue
        folder, glob = MANIFEST_KINDS[f["kind"]]
        parent = Path(rel).parent.as_posix().removeprefix(".")
        inside = parent == folder or (folder != "" and parent.startswith(folder + "/"))
        if not inside or not fnmatch.fnmatch(Path(rel).name, glob):
            out.append(("manifest", f"{rel} is listed as {f['kind']} but a {f['kind']} file lives at {(folder + '/') if folder else ''}{glob}"))
        link = _symlink_in(root, rel)
        if link:
            out.append(("manifest", f"{rel}: {link} is a symlink, which escapes the pack directory by design; a pinned file is a plain file"))
            continue
        path = root / rel
        if not path.exists():
            out.append(("manifest", f"{rel} does not exist"))
            continue
        if not path.is_file():
            out.append(("manifest", f"{rel} is not a regular file"))
            continue
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        if digest != f["sha256"]:
            out.append(("manifest", f"{rel}: sha256 is {digest}, the manifest pins {f['sha256']}"))
        readable[f["kind"]] = path
    for kind, (folder, glob) in MANIFEST_KINDS.items():
        base = root / folder
        if not base.is_dir():
            continue
        if kind == "injection_patterns" and not by_kind[kind] and doc["version"] < MANIFEST_INJECTION_FROM_VERSION:
            continue  # A664: a manifest below the gate that lists none carries no pattern data; the loader serves nothing it does not pin and does not scan for the kind either
        if kind == "recipient_matcher" and not by_kind[kind] and doc["version"] < MANIFEST_MATCHER_FROM_VERSION:
            continue  # A710: a manifest below the gate that lists none carries no matcher data; the loader serves nothing it does not pin
        for dirpath, dirnames, filenames in os.walk(base, followlinks=False):
            if folder == "":
                dirnames[:] = []  # the pack folder itself: only its own files, not the whole tree
            for name in sorted(dirnames + filenames):
                full = Path(dirpath) / name
                if full.is_symlink():
                    if folder == "" and not fnmatch.fnmatch(name, glob):
                        continue  # the pack folder holds other things; only a link that looks like a pack file counts
                elif name in dirnames or not fnmatch.fnmatch(name, glob):
                    continue
                rel = full.relative_to(root).as_posix()
                if rel in seen:
                    continue
                suffix = " (a symlink)" if full.is_symlink() else ""
                out.append(("manifest", f"{rel}{suffix} is a {kind} file and is not listed in the manifest"))
    out += _graph_set_problems(by_kind["action_graph"], readable.get("pack"))
    expected = doc["expected"]
    for kind, id_key, want_key in (("evidence_mapping", "mapping_id", "evidence_mapping_id"), ("recon_rules", "ruleset_id", "recon_rule_set_id"),
                                   ("injection_patterns", "pack_id", "injection_patterns_id")):
        path = readable.get(kind)
        if path is None:
            continue
        rel = path.relative_to(root).as_posix() if not path.is_symlink() else path.name
        try:
            body = json.loads(path.read_text(encoding="utf-8"))
        except (ValueError, UnicodeDecodeError):
            out.append(("manifest", f"{rel} is not valid JSON, so its id cannot be read"))
            continue
        carried = _manifest_id(body, id_key) if isinstance(body, dict) else None
        if carried is None:
            out.append(("manifest", f"{rel} carries no usable {id_key} and integer version"))
        elif carried != expected[want_key]:
            out.append(("manifest", f"{rel} carries {carried}, the manifest expects {want_key} {expected[want_key]}"))
        if kind == "recon_rules":
            out += [("manifest", f"{rel} is not a valid rule set: {m}") for _, m in check_recon_rules(body)]
        if kind == "evidence_mapping" and isinstance(body, dict) and body.get("ruleset") != expected["recon_rule_set_id"]:
            out.append(("manifest", f"{rel} feeds rule set {body.get('ruleset')!r}, the manifest expects {expected['recon_rule_set_id']}"))
        if kind == "evidence_mapping" and isinstance(body, dict):
            out += [("manifest", f"{rel} {m}") for m in _target_version_problems(body, readable.get("pack"))]
    out += _priority_routing_problems(readable.get("priority_routing"), readable.get("pack"), root)
    return out


def _target_version_problems(mapping: dict, pack_file: Path | None) -> list[str]:
    """EXP-01 (A556, A706, A710): the optional `target_versions` member of the evidence mapping is checked the way the Go loader checks it (trust/reconcile/mapping): a non-empty list of
    `{source, token, complete}`, each a non-empty text, the marker not the token, no other member, one declaration per source, and every source one of the pack's evidence sources."""
    if "target_versions" not in mapping:
        return []
    decls = mapping["target_versions"]
    if not isinstance(decls, list) or not decls:
        return ["target_versions must be a non-empty list of declarations"]
    sources: set[str] | None = None
    if pack_file is not None:
        try:
            pack = json.loads(pack_file.read_text(encoding="utf-8"))
            sources = {e["source_id"] for e in pack.get("evidence_sources", []) if isinstance(e, dict) and isinstance(e.get("source_id"), str)}
        except (ValueError, UnicodeDecodeError, OSError, AttributeError):
            sources = None
    problems: list[str] = []
    seen: set[str] = set()
    for i, d in enumerate(decls):
        where = f"target_versions[{i}]"
        if not isinstance(d, dict) or set(d) != {"source", "token", "complete"}:
            problems.append(f"{where} must be an object with exactly source, token and complete")
            continue
        if not all(isinstance(d[k], str) and d[k] for k in ("source", "token", "complete")):
            problems.append(f"{where} must name a source, a token member and a complete member as non-empty text")
            continue
        if d["token"] == d["complete"]:
            problems.append(f"{where}: the complete marker must be a member other than the token")
        if d["source"] in seen:
            problems.append(f"{where}: the source {d['source']} is declared twice")
        seen.add(d["source"])
        if sources is not None and d["source"] not in sources:
            problems.append(f"{where}: the source {d['source']} is not an evidence source of the pack")
    return problems


def _priority_routing_problems(member: Path | None, pack_file: Path | None, root: Path) -> list[Problem]:
    """PCK-09 (A644, A616): the priority routing member is valid against its schema (priority HIGH|NORMAL|LOW, a non-empty owner queue, nothing unknown), and every phase it
    routes is one the verified pack names in predicate_variables.sla_phase and every queue is one of the pack's routing_allowlist. A660: every phase of sla_phase
    (the clock statuses; a test ties the list to trust/clocks) must have an entry."""
    if member is None:
        return []
    rel = member.relative_to(root).as_posix() if not member.is_symlink() else member.name
    try:
        body = validate.load_strict(member)
    except (ValueError, UnicodeDecodeError) as exc:
        return [("manifest", f"{rel} (priority_routing) is not valid JSON: {str(exc)[:100]}")]
    errs = sorted(validate.StrictValidator(validate.load_strict(PRIORITY_ROUTING_SCHEMA)).iter_errors(body), key=lambda e: list(map(str, e.absolute_path)))
    if errs:
        return [("manifest", f"{rel} (priority_routing) {'/'.join(map(str, e.absolute_path)) or '(root)'}: {e.message[:140]}") for e in errs]
    if pack_file is None:
        return []
    try:
        pack = json.loads(pack_file.read_text(encoding="utf-8"))
    except (ValueError, UnicodeDecodeError):
        return []  # the pack file's own problems are reported elsewhere; its phases and queues cannot be read
    phases = ((pack.get("predicate_variables") or {}).get("sla_phase") or {}).get("values") if isinstance(pack, dict) else None
    queues = pack.get("routing_allowlist") if isinstance(pack, dict) else None
    out: list[Problem] = []
    for phase, e in body["phases"].items():
        if not isinstance(phases, list) or phase not in phases:
            out.append(("manifest", f"{rel} (priority_routing): phase {phase!r} is not one the pack's sla_phase names"))
        if not isinstance(queues, list) or e["owner_queue"] not in queues:
            out.append(("manifest", f"{rel} (priority_routing): phase {phase!r} routes to {e['owner_queue']!r}, which is not in the pack's routing_allowlist"))
    for phase in phases if isinstance(phases, list) else []:
        if phase not in body["phases"]:
            out.append(("manifest", f"{rel} (priority_routing): the pack's sla_phase {phase!r} has no entry"))
    return out


def check_limits_schema(limits: dict) -> list[Problem]:
    """A609 (interim; AGENT-15, AGENT-16): the shipped trust-limits file is validated against the trust-limits.v2 JSON Schema itself, so a file without the ceiling, a v1-shaped file and a
    file with an unknown member fail here whatever the hand-written routing rules say. The message names the member, never a value."""
    schemas = validate.load_schemas()
    registry = validate.make_registry(schemas)
    errs = validate.validator_for("trust-limits.v2", schemas, registry).iter_errors(limits)
    return [("trust_limits", f"{'/'.join(map(str, e.absolute_path)) or '(root)'}: {e.message[:140]}") for e in errs]


def check_trust_routing(pack: dict, limits: dict) -> list[Problem]:
    """A544 (interim; PERD v1.23 23.1, 23.3, 23.5, REQ-RES-002): the routing members of the pack's trust-limits document name only what the pack declares. An escalation reason of a classification
    is a catalogue code the pack produces from a state mapping, a clock phase's is one it produces from a clock mapping, a capability allowed under a freeze is a declared capability that is not
    prohibited, and an anchor event is one a pack clock names, read from an entity the pack's evidence descriptors declare, at a field that descriptor declares as a Timestamp (A575). A classification, a phase or an event is routed at most once.
    No value is checked for being right: a value the documents do not state is not in the file (AGENT-16), and is an open decision, not a check."""
    out: list[Problem] = []

    def bad(msg: str) -> None:
        out.append(("trust_routing", msg))

    if limits.get("schema_version") != "2.0":  # A596 (interim): trust-limits is v2 and a v1 file has no compatibility path
        bad(f"schema_version {limits.get('schema_version')!r} is not 2.0: the pack's trust-limits document is trust-limits.v2 and a v1 file is refused (A596)")
    if limits.get("pack_id") != pack["pack_id"]:
        bad(f"pack_id {limits.get('pack_id')!r} is not the pack's {pack['pack_id']!r}")
    produced = {r["code"]: r.get("produced_by") for r in pack["reason_catalogue"]}

    def reason(code: str, producer: str, where: str) -> None:
        if code not in produced:
            bad(f"{where}: reason code {code!r} is not in the pack's catalogue")
        elif produced[code] != producer:
            bad(f"{where}: reason code {code!r} is produced by {produced[code]}, not {producer}")

    def once(keys: list, what: str) -> None:
        for k in sorted({k for k in keys if keys.count(k) > 1}):
            bad(f"{what} {k!r} is routed twice")

    entries = limits.get("escalation_reasons", [])
    once([e["classification"] for e in entries], "classification")
    for e in entries:
        reason(e["reason_code"], "STATE_MAPPING", f"escalation_reasons[{e['classification']}]")
    caps = {c["capability_id"]: c for c in pack["capabilities"]}
    for cid in limits.get("allowed_under_freeze", []):
        if cid not in caps:
            bad(f"allowed_under_freeze: capability {cid!r} is not declared by the pack")
        elif caps[cid]["prohibited"]:
            bad(f"allowed_under_freeze: capability {cid!r} is prohibited")
    phases = limits.get("phase_routes", [])
    once([e["phase"] for e in phases], "phase")
    for e in phases:
        reason(e["reason_code"], "CLOCK_MAPPING", f"phase_routes[{e['phase']}]")
    anchors = limits.get("anchor_entities", [])
    once([e["event"] for e in anchors], "event")
    events = {c["anchor_event"] for c in pack.get("clocks", [])}
    descriptors = {d["entity"]: d for d in pack.get("evidence_descriptors", [])}
    for e in anchors:
        if e["event"] not in events:
            bad(f"anchor_entities: event {e['event']!r} is not the anchor event of any pack clock")
        if e["entity"] not in descriptors:
            bad(f"anchor_entities: entity {e['entity']!r} has no evidence descriptor in the pack")
            continue
        # A575 (interim): the field a clock's anchor is read from is a field the entity's descriptor declares, and a Timestamp.
        declared = descriptors[e["entity"]].get("fields", {})
        if e["field"] not in declared:
            bad(f"anchor_entities: field {e['field']!r} is not declared by the descriptor of entity {e['entity']!r}")
        elif declared[e["field"]].get("type") != "Timestamp":
            bad(f"anchor_entities: field {e['field']!r} of entity {e['entity']!r} is a {declared[e['field']].get('type')}, not a Timestamp")
    return out


def run(pack_path: Path, conformance: bool = False, pack: dict | None = None, graphs_override: dict[str, dict] | None = None,
        templates: str = "off") -> list[Problem]:
    """templates="enforce" adds the REQ-EXP-002 coverage check. It is off by default only until EXP-04 delivers the real pack's templates;
    EXP-04 cannot close until the real pack passes with it on (decision of 3 Nov 2026), and it turns it on in CI."""
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
    problems += check_references(pack) + check_reasons(pack) + check_capabilities(pack) + check_compensation(pack) + check_tenant_zone(pack) + check_graph_link(pack, graphs) + check_predicates(pack, graphs) + check_write_when(pack) + check_write_when_link(pack, graphs) + check_subject_slot(pack) + check_outcome_bindings(pack, pack_path.parent)
    problems += check_graphs(pack, graphs) + check_graph_params(pack, graphs, pack_path.parent) + check_graph_permits(pack, graphs) + check_routing(pack) + check_disambiguation(pack) + check_approver_roles(pack) + check_understanding(pack, pack_path.parent, conformance)
    if conformance:
        problems += check_conformance(pack)
    # A544: the pack's trust-limits document sits in the pack's own folder (`fintech_v1.json` is of `fintech/`); a pack with none (a conformance pack) has no routing to check.
    limits_path = pack_path.parent / pack_path.stem.rsplit("_v", 1)[0] / "limits" / "trust_limits.json"
    if limits_path.is_file():
        try:
            limits = validate.load_strict(limits_path)
        except ValueError as exc:
            problems.append(("trust_routing", str(exc)))
        else:
            problems += check_limits_schema(limits) + check_trust_routing(pack, limits)
    if templates == "enforce":
        problems += check_templates(pack, pack_path.parent)
    return problems
