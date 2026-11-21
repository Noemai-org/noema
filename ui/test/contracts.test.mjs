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
import { parseReconstructionV3 } from "../build/js/contract_types/reconstruction_v3.js";

const FIXTURES = fileURLToPath(new URL("../../contracts/fixtures/", import.meta.url));
// Every invalid fixture of a contract the UI reads is refused by its generated parser: shape, bounds and the conditional rules (prefixItems, allOf with if/then/else, not,
// anyOf, contains), which the emitter compiles into checks (tools/codegen/ts.py; noema#682). The one exception is a fixture a browser cannot see:
//   record-event.v3 seq_is_a_float: JSON.parse reads 1.0 as 1, so no parser in a browser sees the float; the core and the validator refuse it.
//   clarification.v1 and overlay-card.v1 none_option_id_equals_an_option_id: a rule across two members that JSON Schema cannot say (tools/contracts/validate.py holds it as
//   `none_option_collisions`, and the core refuses it in trust/decide); a generated parser holds only schema keywords, so the UI does not (A275, noema#736).
const NOT_VISIBLE_TO_JSON_PARSE = new Set([
  "record-event.v3/seq_is_a_float.json",
  "clarification/none_option_id_equals_an_option_id.json",
  "overlay-card/none_option_id_equals_an_option_id.json",
]);

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

  test(`${name}: every invalid fixture is refused`, () => {
    if (!existsSync(invalid)) return;
    const expectations = JSON.parse(readFileSync(join(invalid, "expectations.json"), "utf8"));
    for (const [f, keyword] of Object.entries(expectations)) {
      if (f.includes("__") || NOT_VISIBLE_TO_JSON_PARSE.has(`${folder(name)}/${f}`)) continue;  // Def__case fixtures target a definition, not the root
      const doc = JSON.parse(readFileSync(join(invalid, f), "utf8"));
      assert.throws(() => PARSERS[name](doc), ContractError, `${name}/${f} (${keyword}) was accepted`);
    }
  });
}

// A275 (REQ-UI-002, REQ-AMB-004): the core mints the id of "None of these" and the UI submits it as option_id. The card carries it, the parser refuses a card without it or with a
// value that is not an option-ID form (lower case, digits and underscore, 1 to 32, and no more than four digits in all), so the UI never invents a sentinel. All data is SYNTHETIC.
test("a clarification card carries the core-minted id of None of these, on the card and in the stream", () => {
  const card = JSON.parse(readFileSync(join(FIXTURES, "valid", "overlay-card", "clarification_four_options.json"), "utf8"));
  const event = JSON.parse(readFileSync(join(FIXTURES, "valid", "sse-event", "clarification.json"), "utf8"));
  assert.equal(CUSTOMER_PARSERS["overlay-card.v1"](card).none_option_id, card.none_option_id);
  assert.equal(CUSTOMER_PARSERS["sse-event.v1"](event).card.none_option_id, event.card.none_option_id);
  const without = (c) => { const { none_option_id: _omitted, ...rest } = c; return rest; };
  assert.throws(() => CUSTOMER_PARSERS["overlay-card.v1"](without(card)), ContractError);
  assert.throws(() => CUSTOMER_PARSERS["sse-event.v1"]({ ...event, card: without(event.card) }), ContractError);
  for (const bad of ["None Of These", "none of these", "n".repeat(33), "", "none_12345", 5, null]) {
    assert.throws(() => CUSTOMER_PARSERS["overlay-card.v1"]({ ...card, none_option_id: bad }), ContractError, String(bad));
  }
});

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

// A258: the customer entry point holds the public contracts (the four it reads, the one it sends, message-request.v1) and errors.v3 and nothing that leads to the operator contracts (they carry tenant_id, events and hashes).
test("the customer entry point parses the public contracts only and imports no operator contract", () => {
  assert.deepEqual(Object.keys(CUSTOMER_PARSERS).sort(), ["customer-answer.v1", "errors.v3", "investigation-response.v1", "message-request.v1", "overlay-card.v1", "sse-event.v1"]);
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

// noema#682 (REQ-UI-003, REQ-AUD-003, INV-02): parseReconstructionV3 is the operator UI's validation boundary for a reconstruction. The twelve sections come in the order of
// REQ-AUD-003 (prefixItems) and the facts of a section follow its kind (allOf with if/then). The eight fixtures below were accepted by the parser merged in noema#615.
const RECON_REFUSED = [
  "sections_out_of_order.json",
  "decision_outcome_unknown_value.json",
  "execution_state_succeeded.json",
  "action_without_grant_state.json",
  "grant_state_unknown_value.json",
  "claim_without_bound_status.json",
  "claim_status_not_a_fact_status.json",
  "claim_text_empty.json",
];

for (const f of RECON_REFUSED) {
  test(`reconstruction.v3: parseReconstructionV3 refuses invalid/${f} (noema#682, REQ-AUD-003, INV-02)`, () => {
    assert.throws(() => parseReconstructionV3(read(`invalid/reconstruction.v3/${f}`)), ContractError);
  });
}

test("reconstruction.v3: parseReconstructionV3 accepts every valid fixture (noema#682, positive control)", () => {
  const valid = readdirSync(join(FIXTURES, "valid/reconstruction.v3")).filter((x) => x.endsWith(".json"));
  assert.ok(valid.length >= 4, "the valid reconstruction fixtures are there");
  for (const f of valid) assert.doesNotThrow(() => parseReconstructionV3(read(`valid/reconstruction.v3/${f}`)), f);
});

// A refusal must come from the rule that is meant. Each case starts from a valid reconstruction, changes one fact of one section, and expects the refusal to name that place.
// Sections with no committed invalid fixture are covered here, so dropping one `allOf` branch of the generated parser fails a test.
const section = (doc, kind) => doc.sections.find((s) => s.kind === kind);
const item = (doc, kind, facts, n = 0) => {
  const s = section(doc, kind);
  s.items[n] = { record_event_id: "recev_1001", object_ref: "obj_AB", basis: "RULE", facts };
  return doc;
};
const SECTION_FACTS = {
  "AMBIGUITY clarification_asked is a boolean": [(d) => item(d, "AMBIGUITY", { clarification_asked: "yes" }), "AMBIGUITY"],
  "RESOLUTION candidate_count is at most 20": [(d) => item(d, "RESOLUTION", { candidate_count: 21 }), "RESOLUTION"],
  "RESOLUTION candidate_count is not negative": [(d) => item(d, "RESOLUTION", { candidate_count: -1 }), "RESOLUTION"],
  "RESOLUTION resolved_target is a resource reference or null": [(d) => item(d, "RESOLUTION", { resolved_target: "not a reference" }), "RESOLUTION"],
  "OBSERVED_STATE classification is upper snake case": [(d) => item(d, "OBSERVED_STATE", { classification: "lower case" }), "OBSERVED_STATE"],
  "DECISION reason_codes is a list": [(d) => item(d, "DECISION", { decision_outcome: "ALLOW", reason_codes: "one" }), "DECISION"],
  "DECISION needs decision_outcome": [(d) => item(d, "DECISION", { reason_codes: [] }), "DECISION"],
  "ACTION needs capability_id": [(d) => item(d, "ACTION", { execution_state: "ACKNOWLEDGED", grant_state: "VALID" }), "ACTION"],
  "ACTION capability_id is a pack id": [(d) => item(d, "ACTION", { capability_id: "Not A Pack Id", execution_state: "ACKNOWLEDGED", grant_state: "VALID" }), "ACTION"],
  "ACTION attempt is positive": [(d) => item(d, "ACTION", { capability_id: "fin.cap.create_escalation_case", execution_state: "ACKNOWLEDGED", grant_state: "VALID", attempt: 0 }), "ACTION"],
  "VERIFICATION verdict is a verdict": [(d) => item(d, "VERIFICATION", { capability_id: "fin.cap.create_escalation_case", verdict: "MAYBE" }), "VERIFICATION"],
  "VERIFICATION capability_id is a pack id": [(d) => item(d, "VERIFICATION", { capability_id: "Not A Pack Id", verdict: "ACHIEVED" }), "VERIFICATION"],
  "EXPLANATION needs text": [(d) => item(d, "EXPLANATION", { bound_fact_status: "KNOWN" }), "EXPLANATION"],
  "EXPLANATION fact_id is not empty": [(d) => item(d, "EXPLANATION", { fact_id: "", text: "A case was opened.", bound_fact_status: "KNOWN" }), "EXPLANATION"],
  "EXPLANATION fact_id is at most 64 characters": [(d) => item(d, "EXPLANATION", { fact_id: "f".repeat(65), text: "A case was opened.", bound_fact_status: "KNOWN" }), "EXPLANATION"],
  "any facts hold at most 50 members": [(d) => item(d, "REQUEST", Object.fromEntries(Array.from({ length: 51 }, (_, i) => [`k${i}`, "v"]))), "REQUEST"],
  "EXPLANATION disclosed_refs are resource references": [(d) => item(d, "EXPLANATION", { text: "A case was opened.", bound_fact_status: "KNOWN", disclosed_refs: ["nope"] }), "EXPLANATION"],
};

for (const [rule, [mutate, kind]] of Object.entries(SECTION_FACTS)) {
  test(`reconstruction.v3: ${rule} (noema#682, REQ-AUD-003)`, () => {
    assert.doesNotThrow(() => parseReconstructionV3(read("valid/reconstruction.v3/full_write_case.json")), "the starting point is valid");
    const doc = mutate(read("valid/reconstruction.v3/full_write_case.json"));
    assert.throws(() => parseReconstructionV3(doc), (e) => e instanceof ContractError && e.path.startsWith("$.sections["), `${kind} was accepted`);
  });
}

test("reconstruction.v3: a section is where REQ-AUD-003 puts it, whichever two swap (noema#682)", () => {
  for (let i = 0; i < 11; i++) {
    const doc = read("valid/reconstruction.v3/full_write_case.json");
    [doc.sections[i], doc.sections[i + 1]] = [doc.sections[i + 1], doc.sections[i]];
    assert.throws(() => parseReconstructionV3(doc), ContractError, `sections ${i} and ${i + 1} swapped`);
  }
});

test("reconstruction.v3: a rule of one section kind does not apply to another (noema#682, positive control)", () => {
  const doc = read("valid/reconstruction.v3/full_write_case.json");
  item(doc, "REQUEST", { decision_outcome: "NOT_AN_OUTCOME" });  // REQUEST has no conditional rule on this member: accepted (positive control)
  assert.doesNotThrow(() => parseReconstructionV3(doc));
});

// A278: the facts of reconstruction.v3 are checked by value, under any key. The `oneOf` fixtures are not in the generic loop above (that keeps the bounds and the shape keywords), so
// the ones about a fact value are named here: the browser parser must refuse each, whatever the key.
test("reconstruction.v3: a fact value that is a number in disguise is refused under any key", () => {
  const names = ["request_id_uuid_of_31_digits_joined_by_dashes", "id_fact_holding_a_card_number", "typed_id_with_digits_only_in_its_body", "reference_fact_with_sixteen_digits_after_the_colon",
    "unlisted_key_with_a_card_number_in_a_sentence", "array_fact_item_with_a_card_number", "text_fact_with_five_joined_digits", "reason_codes_item_with_five_joined_digits"];
  for (const n of names) {
    const doc = JSON.parse(readFileSync(join(FIXTURES, "invalid", "reconstruction.v3", `${n}.json`), "utf8"));
    assert.throws(() => PARSERS["reconstruction.v3"](doc), ContractError, `${n} was accepted`);
  }
});
