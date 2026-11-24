import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import * as customer from "../build/js/contract_types/customer.js";

test("A433: customer entry validates the SELF confirmation answer contract", async () => {
  assert.equal(typeof customer.parseConfirmationAnswerV1, "function", "customer confirmation-answer parser is missing");
  for (const name of ["accept", "decline"]) {
    const file = new URL(`../../contracts/fixtures/valid/confirmation-answer/${name}.json`, import.meta.url);
    const answer = JSON.parse(await readFile(file, "utf8"));
    assert.deepEqual(customer.parseConfirmationAnswerV1(answer), answer);
  }
  assert.throws(() => customer.parseConfirmationAnswerV1({ request_id: "cfr_2Q8", response: "ACCEPT", principal_id: "synthetic" }));
});
