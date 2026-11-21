// REQ-UI-001, REQ-TRU-009, INV-12, INV-25, A258 (noema#760, after #758): the approver entry point (generated, tools/codegen/ts.json "approver") parses confirmation-answer.v1 (the closed
// {request_id, response} body an approver sends) and nothing else, and is isolated from the customer, operator and benchmark entry points both ways. All fixture data is SYNTHETIC.
import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { dirReader, reach } from "./support/module_graph.mjs";
import { ContractError as CustomerContractError, PARSERS as CUSTOMER_PARSERS } from "../build/js/contract_types/customer.js";
import { PARSERS as OPERATOR_PARSERS } from "../build/js/contract_types/operator.js";
import { PARSERS as BENCHMARK_PARSERS } from "../build/js/contract_types/benchmark.js";

const FIXTURES = fileURLToPath(new URL("../../contracts/fixtures/", import.meta.url));
const BUILD = fileURLToPath(new URL("../build/js/", import.meta.url));
const TYPES = join(BUILD, "contract_types");
const VALID = join(FIXTURES, "valid", "confirmation-answer");
const INVALID = join(FIXTURES, "invalid", "confirmation-answer");

// The entry point is loaded without a static import so that its absence is an assertion failure of the tests below, not an error of the file.
const approver = await import("../build/js/contract_types/approver.js").catch(() => null);
const entry = () => {
  assert.ok(approver !== null, "the approver entry point (ui/src/contract_types/approver.ts) is not generated: tools/codegen/ts.json has no approver root");
  return approver;
};
const parseAnswer = (v) => entry().PARSERS["confirmation-answer.v1"](v);
const valid = (f) => JSON.parse(readFileSync(join(VALID, f), "utf8"));
const validFiles = () => readdirSync(VALID).filter((f) => f.endsWith(".json"));

// The contract modules a built contract module imports, directly or through another, as names with the extension.
function closure(entryFile) {
  return new Set([...reach(dirReader(BUILD), [`contract_types/${entryFile}`]).reached].map((f) => f.replace(/^contract_types\//, "")));
}

test("the approver entry point parses confirmation-answer.v1 and no other contract", () => {
  assert.deepEqual(Object.keys(entry().PARSERS), ["confirmation-answer.v1"]);
  assert.equal(typeof parseAnswer, "function");
  assert.equal(entry().ContractError, CustomerContractError, "one runtime error class for every entry point");
});

test("confirmation-answer.v1: every valid fixture parses to what it holds (and there are some)", () => {
  assert.ok(validFiles().length >= 2);
  for (const f of validFiles()) assert.deepEqual(parseAnswer(valid(f)), valid(f), `confirmation-answer/${f}`);
});

// Every invalid fixture breaks a shape or a bound a parser holds (additionalProperties, type, required, pattern, enum); none sits under an if/then or allOf (noema#682 does not apply),
// so a parser must refuse every one of them.
test("confirmation-answer.v1: every invalid fixture is refused with a ContractError, and the fixtures are the ones expectations.json lists", () => {
  const expectations = JSON.parse(readFileSync(join(INVALID, "expectations.json"), "utf8"));
  const files = readdirSync(INVALID).filter((f) => f.endsWith(".json") && f !== "expectations.json");
  assert.ok(files.length >= 30, "the scan sees the invalid fixtures");
  assert.deepEqual(files.sort(), Object.keys(expectations).sort(), "a fixture without an expectation, or the other way round");
  let refused = 0;
  for (const [f, keyword] of Object.entries(expectations)) {
    const doc = JSON.parse(readFileSync(join(INVALID, f), "utf8"));
    assert.throws(() => parseAnswer(doc), entry().ContractError, `confirmation-answer/${f} (${keyword}) was accepted`);
    refused++;
  }
  assert.equal(refused, files.length, "every invalid fixture was run through the parser");
});

// INV-12: the principal, roles, channel, time, tenant and session come from the authenticated session, never from this body. The parser is closed, so a member the schema does not list is
// refused wherever it comes from; positive control: the same body without it parses.
test("a member that names an identity, a role, a tenant, a session or a grant is refused on a valid answer (INV-12, INV-25)", () => {
  for (const f of validFiles()) {
    assert.doesNotThrow(() => parseAnswer(valid(f)), `positive control: ${f}`);
    for (const k of ["approver_id", "principal_id", "approver_roles", "role", "roles", "tenant_id", "session", "session_id", "channel", "at", "grant", "grant_id", "decision_id", "note", "schema_version"]) {
      const d = valid(f);
      d[k] = "x";
      assert.throws(() => parseAnswer(d), entry().ContractError, `${f}: ${k} was accepted`);
    }
  }
});

// A433: a customer answers a SELF confirmation, so the customer entry point exports confirmation-answer.v1 (and the operator one, which holds the customer set); the approver entry point stays
// for the approver's pages and reaches nothing of another audience; no entry point other than itself reaches it.
test("the approver entry point reaches no customer, operator or benchmark module, none of theirs reaches it, and the customer and operator ones export the answer (A433)", () => {
  entry();
  const modules = [...closure("approver.js")].sort();
  // the answer, the runtime and the two shared definition files it refers to (common.v1: the request id; enums.v1: the response); no entry point and no contract of another audience
  assert.deepEqual(modules, ["_runtime.js", "approver.js", "common_v1.js", "confirmation_answer_v1.js", "enums_v1.js"]);
  for (const other of ["customer.js", "operator.js", "benchmark.js"]) {
    assert.ok(!modules.includes(other), `${other} is reachable from the approver entry point`);
    assert.ok(!closure(other).has("approver.js"), `approver.js is reachable from ${other}`);
  }
  for (const [audience, parsers] of Object.entries({ customer: CUSTOMER_PARSERS, operator: OPERATOR_PARSERS })) assert.ok("confirmation-answer.v1" in parsers, `${audience} does not export confirmation-answer.v1`);
  assert.ok(!("confirmation-answer.v1" in BENCHMARK_PARSERS), "the benchmark entry point holds the answer");
  for (const name of [...Object.keys(CUSTOMER_PARSERS), ...Object.keys(OPERATOR_PARSERS), ...Object.keys(BENCHMARK_PARSERS)].filter((n) => n !== "confirmation-answer.v1")) assert.ok(!(name in entry().PARSERS), `${name} is an approver contract`);
});

// A258, noema#760: no page reaches the approver entry point unless it is named here. The UI has no bundler: what a page imports (directly or through another module, in the built JS) is
// what ships with it. Today no page imports it (#758: no UI surface reads confirmation-answer.v1); an approver page that does names itself below, in a reviewed change.
const APPROVER_PAGES = [];
test("no module outside contract_types reaches the approver entry point, and the built page and its bundle do not hold it", () => {
  entry();
  const pages = readdirSync(BUILD).filter((f) => f.endsWith(".js"));
  assert.ok(pages.includes("main.js") && pages.includes("overlay.js"), "the scan sees the pages");
  for (const p of pages.filter((f) => !APPROVER_PAGES.includes(f))) {
    const graph = reach(dirReader(BUILD), [p]);
    assert.ok(!graph.reached.has("contract_types/approver.js"), `${p} reaches the approver entry point`);
    assert.deepEqual(graph.computed, [], `${p} imports a module by a computed name, which this test cannot follow`);
  }
  for (const f of readdirSync(fileURLToPath(new URL("../dist/", import.meta.url)), { recursive: true })) {
    assert.ok(!/approver/i.test(f), `dist/${f}`);
  }
});

test("the approver entry point and the answer import only the runtime and the shared definitions (no client, no fetch, no other contract)", () => {
  for (const f of ["approver.js", "confirmation_answer_v1.js"]) {
    const text = readFileSync(join(TYPES, f), "utf8");
    const imports = [...text.matchAll(/from "([^"]+)"/g)].map((m) => m[1]);
    assert.ok(imports.length > 0, `${f}: the scan sees its imports`);
    for (const i of imports) assert.ok(["./_runtime.js", "./confirmation_answer_v1.js", "./common_v1.js", "./enums_v1.js"].includes(i), `${f} imports ${i}`);
    assert.ok(!/\bfetch\b|XMLHttpRequest|WebSocket|EventSource/.test(text.replace(/\/\*\*[\s\S]*?\*\//g, "")), `${f} reaches the network`);
  }
});
