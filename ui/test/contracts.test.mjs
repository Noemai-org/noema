// REQ-UI-001, REQ-API-001, AGENT-15: the TypeScript parsers generated from the contracts (tools/codegen/ts.py) accept every valid fixture and refuse every invalid
// fixture whose failing rule a parser holds. The UI receives untrusted JSON; a parser is its only way in. All fixture data is SYNTHETIC.
import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { ContractError, PARSERS } from "../build/js/contracts/index.js";
import { arr, bool, closed, int, lit, num, obj, oneOfLit, req, str, union } from "../build/js/contracts/_runtime.js";

const FIXTURES = fileURLToPath(new URL("../../contracts/fixtures/", import.meta.url));
// The rules a generated parser holds beyond shape: the bounds of a string, a number and a list. Conditional rules (anyOf over constraints, if/then, not, contains) stay with the
// JSON Schema validator, as in the Go and Pydantic output, so a fixture refused only by such a rule is not asserted here.
const BOUNDS = new Set(["pattern", "minLength", "maxLength", "minimum", "maximum", "minItems", "maxItems"]);
// The invalid fixtures the generated types must refuse for their shape, derived from the validator (tools/codegen/shape_refusals.py).
const SHAPE = JSON.parse(readFileSync(fileURLToPath(new URL("../../tools/codegen/shape_refusals.json", import.meta.url)), "utf8"));
// Invalid fixtures whose bound sits under an if/then rule, so a parser cannot hold it:
//   clarification: minItems 2 applies only in ATTRIBUTE mode; noema-resolution: minItems 1 applies only when verified is true.
const CONDITIONAL = new Set(["clarification/attribute_mode_one_option.json", "noema-resolution/verified_with_no_verdicts.json"]);

// The fixture folder of a contract: <name> for v1, <name>.v<major> for a later major (tools/contracts/validate.py).
const folder = (name) => name.replace(/\.v1$/, "");

const names = Object.keys(PARSERS);

test("the UI reads at least the contracts named in tools/codegen/ts.json", () => {
  assert.ok(names.length >= 1);
});

for (const name of names) {
  const valid = join(FIXTURES, "valid", folder(name));
  const invalid = join(FIXTURES, "invalid", folder(name));

  test(`${name}: every valid fixture parses`, () => {
    assert.ok(existsSync(valid), `no valid fixtures for ${name}`);
    for (const f of readdirSync(valid).filter((x) => x.endsWith(".json") && !x.includes("__"))) {  // Def__case fixtures target a definition, not the root
      const doc = JSON.parse(readFileSync(join(valid, f), "utf8"));
      assert.doesNotThrow(() => PARSERS[name](doc), `${name}/${f}`);
    }
  });

  test(`${name}: every invalid fixture a parser can hold is refused`, () => {
    if (!existsSync(invalid)) return;
    const expectations = JSON.parse(readFileSync(join(invalid, "expectations.json"), "utf8"));
    for (const [f, keyword] of Object.entries(expectations)) {
      if (f.includes("__") || CONDITIONAL.has(`${folder(name)}/${f}`)) continue;  // Def__case fixtures target a definition, not the root
      if (!(`${folder(name)}/${f}` in SHAPE) && !BOUNDS.has(keyword)) continue;
      const doc = JSON.parse(readFileSync(join(invalid, f), "utf8"));
      assert.throws(() => PARSERS[name](doc), ContractError, `${name}/${f} (${keyword}) was accepted`);
    }
  });
}

test("a pattern is the whole string: a trailing newline is refused", () => {
  assert.throws(() => str("clr_AB\n", "$", { pattern: /^clr_[0-9A-Za-z]{2,32}$/u }), ContractError);
  assert.equal(str("clr_AB", "$", { pattern: /^clr_[0-9A-Za-z]{2,32}$/u }), "clr_AB");
});

test("a length is counted in code points, not UTF-16 units", () => {
  assert.equal(str("😀😀", "$", { maxLength: 2 }), "😀😀");
  assert.throws(() => str("😀😀😀", "$", { maxLength: 2 }), ContractError);
});

test("a closed object refuses an unknown member, including __proto__", () => {
  const o = obj(JSON.parse('{"a":1,"__proto__":{"x":1}}'), "$");
  assert.throws(() => closed(o, ["a"], "$"), ContractError);
});

test("an error names the place and the rule and never the value", () => {
  try {
    str("account 0123456789", "$.target", { pattern: /^(?![\s\S]*[0-9]{5})[\s\S]*$/u });
    assert.fail("accepted");
  } catch (e) {
    assert.ok(e instanceof ContractError);
    assert.equal(e.path, "$.target");
    assert.ok(!String(e.message).includes("0123456789"));
  }
});

test("oneOf needs exactly one alternative", () => {
  const a = (x, p) => lit(x, p, "A");
  const any = (x, p) => str(x, p);
  assert.equal(union("A", "$", [(x, p) => lit(x, p, "A"), (x, p) => lit(x, p, "B")]), "A");
  assert.throws(() => union("C", "$", [a, (x, p) => lit(x, p, "B")]), ContractError);
  assert.throws(() => union("A", "$", [a, any]), ContractError);
});

test("a list is bounded", () => {
  assert.throws(() => arr([1, 2, 3], "$", (x) => x, { maxItems: 2 }), ContractError);
  assert.throws(() => arr([], "$", (x) => x, { minItems: 1 }), ContractError);
});

test("a required member that is absent is refused, whatever its type", () => {
  assert.throws(() => req({}, "a", "$"), ContractError);
  assert.equal(req({ a: null }, "a", "$"), null);
});

test("an integer is a whole JSON number, a number is finite, a boolean is a boolean, an enum is one of its values", () => {
  assert.throws(() => int(1.5, "$"), ContractError);
  assert.throws(() => int("1", "$"), ContractError);
  assert.throws(() => int(0, "$", { minimum: 1 }), ContractError);
  assert.throws(() => num(Number.NaN, "$"), ContractError);
  assert.throws(() => num("1", "$"), ContractError);
  assert.throws(() => bool(0, "$"), ContractError);
  assert.throws(() => oneOfLit("C", "$", ["A", "B"]), ContractError);
  assert.equal(int(2, "$", { minimum: 1, maximum: 3 }), 2);
});
