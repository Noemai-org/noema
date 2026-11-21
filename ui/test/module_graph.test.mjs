// EL's independent read on noema#798 (A258, A433): the isolation test followed only `from "./name"` imports, so a bare side-effect import, a dynamic import() or a nested module that imports
// the approver entry point passed every test. These tests pin what the module-graph helper must follow. The sources are SYNTHETIC built modules.
import test from "node:test";
import assert from "node:assert/strict";
import { mapReader, reach } from "./support/module_graph.mjs";

const APPROVER = "contract_types/approver.js";
const graph = (modules, entry = "main.js") => reach(mapReader({ [APPROVER]: "export const a = 1;", "other.js": "export const o = 1;", ...modules }), [entry]);
const reaches = (modules, entry) => graph(modules, entry).reached.has(APPROVER);

test("a static import and a re-export reach the approver entry point (controls: these were already followed)", () => {
  assert.ok(reaches({ "main.js": 'import { a } from "./contract_types/approver.js";' }));
  assert.ok(reaches({ "main.js": 'export * from "./contract_types/approver.js";' }));
  assert.ok(reaches({ "main.js": 'export { a } from "./contract_types/approver.js";' }));
  assert.ok(reaches({ "main.js": 'import {\n  a,\n  b,\n} from "./contract_types/approver.js";' }), "an import split over lines");
});

test("a bare side-effect import reaches it, in either quote", () => {
  assert.ok(reaches({ "main.js": 'import "./contract_types/approver.js";' }));
  assert.ok(reaches({ "main.js": "import './contract_types/approver.js';" }));
  assert.ok(reaches({ "main.js": 'import   "./contract_types/approver.js"\n' }));
});

test("a dynamic import() with a literal reaches it, in any quote", () => {
  assert.ok(reaches({ "main.js": 'const m = await import("./contract_types/approver.js");' }));
  assert.ok(reaches({ "main.js": "const m = await import('./contract_types/approver.js');" }));
  assert.ok(reaches({ "main.js": "const m = await import(`./contract_types/approver.js`);" }));
  assert.ok(reaches({ "main.js": 'import("./contract_types/approver.js").then((m) => m);' }));
});

test("a nested module reaches it through ../, and the root reaches the nested module by a side-effect import (EL's mutation)", () => {
  assert.ok(reaches({ "main.js": 'import "./nested/wrapper.js";', "nested/wrapper.js": 'import "../contract_types/approver.js";' }));
  assert.ok(reaches({ "main.js": 'await import("./nested/wrapper.js");', "nested/wrapper.js": 'export * from "../contract_types/approver.js";' }));
  assert.ok(reaches({ "main.js": 'import "./a/b/c.js";', "a/b/c.js": 'import "../../contract_types/approver.js";' }), "two levels down");
});

test("a computed import() cannot be followed, so it is reported and the test fails closed", () => {
  const g = graph({ "main.js": 'const name = "approver"; await import(`./contract_types/${name}.js`);' });
  assert.deepEqual(g.computed, ["main.js"]);
  assert.deepEqual(graph({ "main.js": "await import(pick());" }).computed, ["main.js"]);
  assert.deepEqual(graph({ "main.js": 'await import("./other.js");' }).computed, [], "a literal import() is not computed");
});

test("a module that does not import it does not reach it, whatever else it imports (negative controls)", () => {
  assert.ok(!reaches({ "main.js": 'import { o } from "./other.js"; import "./other.js"; await import("./other.js");' }));
  assert.ok(!reaches({ "main.js": "export const x = 1;" }));
  assert.deepEqual(graph({ "main.js": 'import "./missing.js";' }).missing, ["missing.js"], "a module that is not in the build is reported, not skipped");
});
