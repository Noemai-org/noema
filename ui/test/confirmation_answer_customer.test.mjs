// A433 (noema#798; REQ-UI-001, REQ-TRU-009, REQ-SEC-001, INV-12, INV-25): a customer answers a SELF confirmation through the customer entry point, which exports confirmation-answer.v1; the UI
// refuses an answer whose request_id is not the id in the path (the route refuses it too); no page reaches the approver entry point, directly or through another module. All data is SYNTHETIC.
import test from "node:test";
import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { dirReader, modulesOf, reach } from "./support/module_graph.mjs";
import { ContractError, PARSERS as CUSTOMER_PARSERS } from "../build/js/contract_types/customer.js";

const FIXTURES = fileURLToPath(new URL("../../contracts/fixtures/", import.meta.url));
const BUILD = fileURLToPath(new URL("../build/js/", import.meta.url));
const VALID = join(FIXTURES, "valid", "confirmation-answer");
const INVALID = join(FIXTURES, "invalid", "confirmation-answer");

// Loaded without a static import so that a missing module is an assertion failure of the test that needs it, not an error of the file.
const confirmation = await import("../build/js/confirmation.js").catch(() => null);
const forPath = () => {
  assert.ok(confirmation !== null, "ui/src/confirmation.ts does not exist: the UI has no check of the path id against the body's request_id");
  return confirmation.parseConfirmationAnswerForPath;
};
const valid = (f) => JSON.parse(readFileSync(join(VALID, f), "utf8"));
const validFiles = () => readdirSync(VALID).filter((f) => f.endsWith(".json"));

test("the customer entry point exports confirmation-answer.v1: every valid fixture parses and every invalid one is refused", () => {
  const parse = CUSTOMER_PARSERS["confirmation-answer.v1"];
  assert.equal(typeof parse, "function", "the customer entry point does not export confirmation-answer.v1 (A433)");
  for (const f of validFiles()) assert.deepEqual(parse(valid(f)), valid(f), f);
  const expectations = JSON.parse(readFileSync(join(INVALID, "expectations.json"), "utf8"));
  for (const [f, keyword] of Object.entries(expectations)) {
    assert.throws(() => parse(JSON.parse(readFileSync(join(INVALID, f), "utf8"))), ContractError, `confirmation-answer/${f} (${keyword}) was accepted`);
  }
});

test("an answer is accepted for the request id of the path and refused for any other id, whatever the response", () => {
  const parse = forPath();
  for (const f of validFiles()) {
    const body = valid(f);
    assert.deepEqual(parse(body.request_id, body), body, f);
    for (const other of ["cfr_OTHER000000001", "cfr_" + "a".repeat(26), "", "cfr_", body.request_id + "x", " " + body.request_id, body.request_id.toLowerCase() === body.request_id ? body.request_id.toUpperCase() : body.request_id.toLowerCase()]) {
      if (other === body.request_id) continue;
      assert.throws(() => parse(other, body), ContractError, `${f}: path ${JSON.stringify(other)} with body ${body.request_id} was accepted`);
    }
  }
});

test("a path id that is not a request id, or a body that is not an answer, is refused whatever it says", () => {
  const parse = forPath();
  const body = valid(validFiles()[0]);
  for (const bad of [undefined, null, 5, {}, [], "x"]) assert.throws(() => parse(body.request_id, bad), ContractError);
  for (const bad of [undefined, null, 5, {}, []]) assert.throws(() => parse(bad, body), ContractError);
  assert.throws(() => parse(body.request_id, { ...body, approver_id: "usr_x" }), ContractError, "a member that names an identity is refused (INV-12)");
});

test("the answer helper imports only the customer entry point and the runtime: no approver module, no network", () => {
  assert.ok(existsSync(join(BUILD, "confirmation.js")), "ui/src/confirmation.ts is not built");
  const text = readFileSync(join(BUILD, "confirmation.js"), "utf8");
  const imports = [...text.matchAll(/from "([^"]+)"/g)].map((m) => m[1]);
  assert.ok(imports.length > 0, "the scan sees its imports");
  for (const i of imports) assert.ok(["./contract_types/customer.js", "./contract_types/_runtime.js"].includes(i), `confirmation.js imports ${i}`);
  assert.ok(!/\bfetch\b|XMLHttpRequest|WebSocket|EventSource/.test(text.replace(/\/\*\*[\s\S]*?\*\//g, "")), "confirmation.js reaches the network");
});

// Transitive: every module of the build outside contract_types, followed through its relative imports, never reaches the approver entry point or its module; the scan sees the answer helper.
test("no module of the build reaches the approver entry point, directly or through another module (A433)", () => {
  const modules = readdirSync(BUILD).filter((f) => f.endsWith(".js"));
  assert.ok(modules.includes("main.js") && modules.includes("overlay.js"), "the scan sees the pages");
  assert.ok(modules.includes("confirmation.js"), "the scan sees the answer helper (ui/src/confirmation.ts)");
  for (const m of modulesOf(BUILD).filter((f) => f !== "contract_types/approver.js")) {
    const graph = reach(dirReader(BUILD), [m]);
    for (const r of graph.reached) assert.ok(!r.endsWith("approver.js"), `${m} reaches ${r}`);
    assert.deepEqual(graph.computed, [], `${m} imports a module by a computed name, which this test cannot follow`);
  }
});

// EL's independent read on noema#798: the route pins the request id with \Acfr_...\z (a trailing newline never matches); a ^...$ pattern in another language would let one through. The UI parser and
// its comparison must refuse every trailing or leading line break and control character too, whether it sits in the path id, in the body, or in both.
test("a path id or a body request id with a trailing or leading line break is refused, whatever the other side says", () => {
  const parse = forPath();
  const body = valid(validFiles()[0]);
  for (const tail of ["\n", "\r\n", "\r", "\u0085", " ", " ", "\0", "\v", "\f", " ", "\t"]) {
    const label = JSON.stringify(tail);
    assert.throws(() => parse(body.request_id + tail, body), ContractError, `path id with trailing ${label} was accepted`);
    assert.throws(() => parse(tail + body.request_id, body), ContractError, `path id with leading ${label} was accepted`);
    const dirty = { ...body, request_id: body.request_id + tail };
    assert.throws(() => parse(dirty.request_id, dirty), ContractError, `a body request id with trailing ${label} was accepted, even with the same path id`);
    assert.throws(() => parse(body.request_id, dirty), ContractError, `a body request id with trailing ${label} was accepted for the clean path id`);
  }
});
