import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { parseVerifiedResolution } from "../build/js/verified.js";

const load = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/noema-resolution/${name}.json`, import.meta.url), "utf8"));

test("REQ-UI-001: verified requires a nonempty set of achieved verdicts", async () => {
  const good = await load("verified_case_opened");
  assert.equal(parseVerifiedResolution(good).verified, true);
  assert.throws(() => parseVerifiedResolution({ ...good, verified: false }));
  const none = await load("unverified_no_verdicts");
  assert.equal(parseVerifiedResolution(none).verified, false);
  assert.throws(() => parseVerifiedResolution({ ...none, verified: true }));
  const mixed = await load("mixed_verdicts_unverified");
  assert.throws(() => parseVerifiedResolution({ ...mixed, verified: true }));
  assert.equal(parseVerifiedResolution(mixed).verified, false);
});
