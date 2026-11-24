import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { readdir } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { CustomerAnswer } from "../build/js/customer-answer.js";
import { parseCustomerAnswerV1 } from "../build/js/contract_types/customer_answer_v1.js";
import { scanVisibleText } from "../build/js/privacy.js";

const fixture = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/customer-answer/${name}.json`, import.meta.url), "utf8"));

test("A570 REQ-UI-001: customer answer never renders a verification label", async () => {
  const supplied = parseCustomerAnswerV1(await fixture("case_opened_verified"));
  const without = parseCustomerAnswerV1(await fixture("not_yet_known"));
  assert.equal("verdicts" in supplied, false);
  const verified = renderToStaticMarkup(createElement(CustomerAnswer, { answer: supplied }));
  assert.deepEqual(scanVisibleText(verified), []);
  assert.doesNotMatch(verified, />Verified</i);
  assert.ok(verified.includes(supplied.case_ref_masked));
  const sameCaseUnverified = renderToStaticMarkup(createElement(CustomerAnswer, {
    answer: { ...supplied, done: { ...supplied.done, verified: false } },
  }));
  assert.deepEqual(scanVisibleText(sameCaseUnverified), []);
  assert.doesNotMatch(sameCaseUnverified, />Verified</i);
  assert.ok(sameCaseUnverified.includes(supplied.case_ref_masked));
  const unverified = renderToStaticMarkup(createElement(CustomerAnswer, { answer: without }));
  assert.deepEqual(scanVisibleText(unverified), []);
  assert.doesNotMatch(unverified, />Verified</i);
  assert.equal("case_ref_masked" in without, false);
  assert.ok(!unverified.includes(supplied.case_ref_masked));
});

test("REQ-UI-001: invalid private fields and digit fixtures are rejected before render", async () => {
  const directory = new URL("../../contracts/fixtures/invalid/customer-answer/", import.meta.url);
  const names = (await readdir(directory)).filter((name) => name.endsWith("_in_found.json"));
  names.push("confidence_member.json", "reason_codes_member.json");
  for (const name of names) {
    const value = JSON.parse(await readFile(new URL(name, directory), "utf8"));
    assert.throws(() => parseCustomerAnswerV1(value), undefined, name);
    const html = renderToStaticMarkup(createElement(CustomerAnswer, { answer: value }));
    assert.match(html, /This result is unavailable/i, name);
    assert.doesNotMatch(html, />Verified</i, name);
    assert.deepEqual(scanVisibleText(html), [], name);
  }
});
