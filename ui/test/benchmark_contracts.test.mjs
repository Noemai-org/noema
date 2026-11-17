// REQ-UI-004, REQ-BEN-004, INV-22, A250, A258 (noema#677): the benchmark entry point (generated, tools/codegen/ts.json "benchmark") parses scorecard-view.v1 and nothing else, and is
// isolated from the customer and operator entry points. All fixture data is SYNTHETIC.
import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { ContractError, PARSERS } from "../build/js/contract_types/benchmark.js";
import { PARSERS as CUSTOMER_PARSERS } from "../build/js/contract_types/customer.js";
import { PARSERS as OPERATOR_PARSERS } from "../build/js/contract_types/operator.js";

const FIXTURES = fileURLToPath(new URL("../../contracts/fixtures/", import.meta.url));
const BUILD = fileURLToPath(new URL("../build/js/contract_types/", import.meta.url));
const VALID = join(FIXTURES, "valid", "scorecard-view");
const INVALID = join(FIXTURES, "invalid", "scorecard-view");
const parseView = PARSERS["scorecard-view.v1"];
const valid = (f) => JSON.parse(readFileSync(join(VALID, f), "utf8"));
const validFiles = () => readdirSync(VALID).filter((f) => f.endsWith(".json"));

// The modules an entry point imports, directly or through another (built JS), as names with the extension.
function closure(entry) {
  const seen = new Set();
  const todo = [entry];
  while (todo.length) {
    const f = todo.pop();
    if (seen.has(f)) continue;
    seen.add(f);
    for (const m of readFileSync(join(BUILD, f), "utf8").matchAll(/from "\.\/([a-z0-9_]+\.js)"/g)) todo.push(m[1]);
  }
  return seen;
}

test("the benchmark entry point parses scorecard-view.v1 and no other contract", () => {
  assert.deepEqual(Object.keys(PARSERS), ["scorecard-view.v1"]);
  assert.equal(typeof parseView, "function");
});

test("scorecard-view.v1: every valid fixture parses (and there are some)", () => {
  assert.ok(validFiles().length >= 3);
  for (const f of validFiles()) assert.doesNotThrow(() => parseView(valid(f)), `scorecard-view/${f}`);
});

// Every invalid fixture of the view breaks a shape or a bound (additionalProperties, type, required, minItems), none sits under an if/then or allOf (noema#682 does not apply),
// so a parser must refuse every one of them.
test("scorecard-view.v1: every invalid fixture is refused", () => {
  const expectations = JSON.parse(readFileSync(join(INVALID, "expectations.json"), "utf8"));
  const files = readdirSync(INVALID).filter((f) => f.endsWith(".json") && f !== "expectations.json");
  assert.deepEqual(files.sort(), Object.keys(expectations).sort(), "a fixture without an expectation, or the other way round");
  for (const [f, keyword] of Object.entries(expectations)) {
    const doc = JSON.parse(readFileSync(join(INVALID, f), "utf8"));
    assert.throws(() => parseView(doc), ContractError, `scorecard-view/${f} (${keyword}) was accepted`);
  }
});

test("the entry points are isolated: the benchmark one imports neither of the others' contracts, and neither of theirs reaches the view", () => {
  const bench = closure("benchmark.js");
  assert.deepEqual([...bench].sort(), ["_runtime.js", "benchmark.js", "scorecard_view_v1.js"]);
  for (const entry of ["customer.js", "operator.js"]) {
    const reached = closure(entry);
    for (const m of ["benchmark.js", "scorecard_view_v1.js"]) assert.ok(!reached.has(m), `${m} is reachable from ${entry}`);
  }
  assert.ok(!("scorecard-view.v1" in CUSTOMER_PARSERS) && !("scorecard-view.v1" in OPERATOR_PARSERS));
  for (const name of Object.keys(CUSTOMER_PARSERS).concat(Object.keys(OPERATOR_PARSERS))) assert.ok(!(name in PARSERS), `${name} is a benchmark contract`);
});

// INV-22: the view names no case and no oracle. The parser is closed, so a member the schema does not list is refused wherever it is put; these cases plant the members the
// view must never carry and require the refusal, on a valid document that parses without them (positive control).
const PLANTED = ["case_id", "case_ids", "failing_case_ids", "failures", "oracle_ref", "oracle_reference", "held_out_set", "expected", "tenant_id", "write_role", "writer_token"];
const WHERE = {
  "the top level": (d, k) => { d[k] = ["c1"]; },
  "a Tier 0 gate": (d, k) => { d.tier0.gates[0][k] = ["c1"]; },
  "a rate": (d, k) => { d.tier2.unsafe[k] = "c1"; },
  "pass^k": (d, k) => { d.tier1.diagnostics.pass_k[k] = ["c1"]; },
  "a category slice": (d, k) => { const slice = Object.values(d.by_category)[0] ?? Object.values(d.by_family)[0]; slice[k] = "c1"; },
};
test("planting a case, oracle, tenant or write-access member anywhere in a valid view is refused (INV-22)", () => {
  for (const f of validFiles()) {
    assert.doesNotThrow(() => parseView(valid(f)), `positive control: ${f}`);
    for (const [where, plant] of Object.entries(WHERE)) {
      for (const k of PLANTED) {
        const d = valid(f);
        try { plant(d, k); } catch { continue; }  // the fixture has no such place (no category slice): nothing to plant
        assert.throws(() => parseView(d), ContractError, `${f}: ${k} in ${where} was accepted`);
      }
    }
  }
});

// What the parser returns holds only the members the view declares: no key of any parsed fixture names a case, an oracle, a tenant or write access.
test("a parsed view carries no member that names a case, an oracle, a tenant or write access", () => {
  const forbidden = ["caseid", "oracle", "heldout", "expected", "failurelist", "failingcases", "structural", "evidence", "human", "tenant", "credential", "secret", "token", "role", "write", "grant", "session"];
  const keys = (v, out = []) => {
    if (Array.isArray(v)) v.forEach((x) => keys(x, out));
    else if (v !== null && typeof v === "object") for (const [k, x] of Object.entries(v)) { out.push(k); keys(x, out); }
    return out;
  };
  for (const f of validFiles()) {
    const all = keys(parseView(valid(f)));
    assert.ok(all.includes("failure_count"), "the scan sees the members it inspects");
    for (const k of all) assert.deepEqual(forbidden.filter((w) => k.toLowerCase().replace(/[^a-z0-9]/g, "").includes(w)), [], `${f}: ${k}`);
  }
});

test("the benchmark entry point and the view import only the runtime and the view (no client, no fetch, no other contract)", () => {
  for (const f of ["benchmark.js", "scorecard_view_v1.js"]) {
    const imports = [...readFileSync(join(BUILD, f), "utf8").matchAll(/from "([^"]+)"/g)].map((m) => m[1]);
    for (const i of imports) assert.ok(["./_runtime.js", "./scorecard_view_v1.js"].includes(i), `${f} imports ${i}`);
    assert.ok(!/\bfetch\b|XMLHttpRequest|WebSocket|EventSource/.test(readFileSync(join(BUILD, f), "utf8").replace(/\/\*\*[\s\S]*?\*\//g, "")), `${f} reaches the network`);
  }
});
