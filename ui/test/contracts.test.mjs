// REQ-UI-001, REQ-API-001, AGENT-15: the TypeScript parsers generated from the contracts (tools/codegen/ts.py) accept every valid fixture and refuse every invalid
// fixture whose failing rule a parser holds. The UI receives untrusted JSON; a parser is its only way in. All fixture data is SYNTHETIC.
import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { ContractError, PARSERS } from "../build/js/contract_types/operator.js";
import { PARSERS as CUSTOMER_PARSERS } from "../build/js/contract_types/customer.js";
import { arr, bool, closed, int, lit, num, obj, oneOfLit, req, str, union } from "../build/js/contract_types/_runtime.js";

const FIXTURES = fileURLToPath(new URL("../../contracts/fixtures/", import.meta.url));
// The rules a generated parser holds beyond shape: the bounds of a string, a number and a list. Conditional rules (anyOf over constraints, if/then, not, contains) stay with the
// JSON Schema validator, as in the Go and Pydantic output, so a fixture refused only by such a rule is not asserted here.
const BOUNDS = new Set(["pattern", "minLength", "maxLength", "minimum", "maximum", "minItems", "maxItems"]);
// The invalid fixtures the generated types must refuse for their shape, derived from the validator (tools/codegen/shape_refusals.py).
const SHAPE = JSON.parse(readFileSync(fileURLToPath(new URL("../../tools/codegen/shape_refusals.json", import.meta.url)), "utf8"));
// Invalid fixtures whose bound sits under an if/then rule, so a parser cannot hold it:
//   clarification: minItems 2 applies only in ATTRIBUTE mode; noema-resolution: minItems 1 applies only when verified is true;
//   reconstruction.v3: the EXPLANATION section's items need a claim text of at least one character, a bound that sits in that section's allOf (a fact key of the section), not in the shape.
const CONDITIONAL = new Set(["clarification/attribute_mode_one_option.json", "noema-resolution/verified_with_no_verdicts.json", "reconstruction.v3/claim_text_empty.json", "record-event.v3/seq_is_a_float.json"]);  // seq_is_a_float: JSON.parse reads 1.0 as 1, so a browser cannot see the float; the core and the validator refuse it

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

// REQ-UI-001, INV-02 (the Security Reviewer's blocking finding on noema#615). The digit class of the public text patterns holds astral-plane digits
// (mathematical, Adlam, segmented): they match only under the `u` flag, so a generator change that dropped the flag for one pattern would let a long run of
// them through. The fixtures hold none, so these cases run the generated parsers on them.
const ASTRAL_RUNS = {
  mathematical: "\u{1D7CF}\u{1D7D0}\u{1D7D1}\u{1D7D2}\u{1D7D3}",   // MATHEMATICAL DOUBLE-STRUCK DIGIT ONE to FIVE
  adlam: "\u{1E950}\u{1E951}\u{1E952}\u{1E953}\u{1E954}",          // ADLAM DIGIT ZERO to FOUR
  segmented: "\u{1FBF0}\u{1FBF1}\u{1FBF2}\u{1FBF3}\u{1FBF4}",      // SEGMENTED DIGIT ZERO to FOUR
};
const FOUR_ASTRAL = "\u{1D7CF}\u{1D7D0}\u{1D7D1}\u{1D7D2}";
const read = (rel) => JSON.parse(readFileSync(join(FIXTURES, rel), "utf8"));
const withMessage = (text) => { const d = read("valid/errors.v3/dependency_unavailable_retryable.json"); d.error.message = text; return d; };
const withDetail = (text) => { const d = read("valid/errors.v3/input_invalid_with_details.json"); d.error.details[0].issue = text; return d; };
const withFound = (text) => { const d = read("valid/customer-answer/not_yet_known.json"); d.found = [text]; return d; };
const SURFACES = { "errors.v3 message": [withMessage, "errors.v3"], "errors.v3 details issue": [withDetail, "errors.v3"], "customer-answer found": [withFound, "customer-answer.v1"] };

for (const [surface, [build, root]] of Object.entries(SURFACES)) {
  for (const [kind, run] of Object.entries(ASTRAL_RUNS)) {
    test(`${surface}: a run of five ${kind} digits is refused (REQ-UI-001, INV-02)`, () => {
      assert.throws(() => PARSERS[root](build(`Reference ${run} please`)), ContractError);
    });
  }
  test(`${surface}: an astral run split by the core's separators is refused (REQ-UI-001, INV-02)`, () => {
    const split = [...ASTRAL_RUNS.mathematical].join(" - ");
    assert.throws(() => PARSERS[root](build(`Reference ${split} please`)), ContractError);
  });
  test(`${surface}: four astral digits pass, and so does text with none (positive control)`, () => {
    assert.doesNotThrow(() => PARSERS[root](build(`Reference ${FOUR_ASTRAL} please`)));
    assert.doesNotThrow(() => PARSERS[root](build("Reference only")));
  });
}

// A258: the customer entry point holds the four public contracts and errors.v3 and nothing that leads to the operator contracts (they carry tenant_id, events and hashes).
test("the customer entry point parses the public contracts only and imports no operator contract", () => {
  assert.deepEqual(Object.keys(CUSTOMER_PARSERS).sort(), ["customer-answer.v1", "errors.v3", "investigation-response.v1", "overlay-card.v1", "sse-event.v1"]);
  const dir = fileURLToPath(new URL("../build/js/contract_types/", import.meta.url));
  const seen = new Set();
  const todo = ["customer.js"];
  while (todo.length) {
    const f = todo.pop();
    if (seen.has(f)) continue;
    seen.add(f);
    for (const m of readFileSync(join(dir, f), "utf8").matchAll(/from "\.\/([a-z0-9_]+\.js)"/g)) todo.push(m[1]);
  }
  for (const operator of ["reconstruction_v3.js", "record_event_v3.js", "operator.js"]) assert.ok(!seen.has(operator), `${operator} is reachable from the customer entry point`);
  assert.ok(Object.keys(PARSERS).includes("reconstruction.v3") && Object.keys(PARSERS).includes("record-event.v3"));
});
