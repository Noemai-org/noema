import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { CustomerAnswer } from "../build/js/customer-answer.js";

const fixture = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/customer-answer/${name}.json`, import.meta.url), "utf8"));

test("REQ-UI-001: customer answer displays the core's verification and case reference without deriving them", async () => {
  const supplied = await fixture("case_opened_verified");
  const without = await fixture("not_yet_known");
  // Neither customer-answer fixture carries a verdict list.
  assert.equal("verdicts" in supplied, false);
  const verified = renderToStaticMarkup(createElement(CustomerAnswer, { answer: supplied }));
  assert.match(verified, />Verified</i);
  assert.ok(verified.includes(supplied.case_ref_masked));
  const sameCaseUnverified = renderToStaticMarkup(createElement(CustomerAnswer, {
    answer: { ...supplied, done: { ...supplied.done, verified: false } },
  }));
  assert.doesNotMatch(sameCaseUnverified, />Verified</i);
  assert.ok(sameCaseUnverified.includes(supplied.case_ref_masked));
  const unverified = renderToStaticMarkup(createElement(CustomerAnswer, { answer: without }));
  assert.doesNotMatch(unverified, />Verified</i);
  assert.equal("case_ref_masked" in without, false);
  assert.ok(!unverified.includes(supplied.case_ref_masked));
});
