import test from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { scanVisibleText } from "../build/js/privacy.js";

test("REQ-UI-001: refuses a visible digit run longer than four", () => {
  assert.deepEqual(scanVisibleText("Account ending 1234"), []);
  assert.notDeepEqual(scanVisibleText("Account 12345"), []);
  assert.notDeepEqual(scanVisibleText("Account 12-34 5"), []);
  assert.deepEqual(scanVisibleText("Account 12-34"), []);
  assert.notDeepEqual(scanVisibleText("12--345"), []);
  assert.notDeepEqual(scanVisibleText("١٢٣٤٥"), []);
  assert.notDeepEqual(scanVisibleText("１２\u200b３/４:５"), []);
});

test("REQ-UI-001: the scanner catches every Unicode separator rejected by the shared customer-answer digit pattern", async () => {
  const schema = JSON.parse(await readFile(new URL("../../contracts/schemas/customer-answer.v1.json", import.meta.url), "utf8"));
  const contract = new RegExp(schema.properties.found.items.pattern, "u");
  for (let codePoint = 0; codePoint <= 0x10ffff; codePoint++) {
    if (codePoint >= 0xd800 && codePoint <= 0xdfff) continue;
    const sample = `1${String.fromCodePoint(codePoint)}2345`;
    const expected = !contract.test(sample);
    if (expected) assert.ok(scanVisibleText(sample).includes("digit-run"), `digit-run missed at U+${codePoint.toString(16).toUpperCase()}`);
  }
  for (const sample of ["1234", "1234,5", "1234x5"]) {
    assert.equal(contract.test(sample), true);
    assert.ok(!scanVisibleText(sample).includes("digit-run"));
  }
});

test("REQ-UI-001: every shared invalid digit fixture is detected", async () => {
  const directory = new URL("../../contracts/fixtures/invalid/customer-answer/", import.meta.url);
  const files = (await readdir(directory)).filter((name) => name.endsWith("_in_found.json"));
  assert.ok(files.length >= 12);
  for (const file of files) {
    const fixture = JSON.parse(await readFile(new URL(file, directory), "utf8"));
    assert.ok(fixture.found.some((text) => scanVisibleText(text).includes("digit-run")), file);
  }
});

test("REQ-UI-001: internal field names and reason codes are never shown", () => {
  for (const text of ["tenant_id", "REJECTED_AT_GATE", "NO_PERMITTING_RULE"]) {
    assert.notDeepEqual(scanVisibleText(text), []);
  }
});

test("REQ-UI-001: detects never-shown internal material", () => {
  assert.notDeepEqual(scanVisibleText("Model confidence: 0.9"), []);
  assert.notDeepEqual(scanVisibleText("Traceback (most recent call last)"), []);
  assert.notDeepEqual(scanVisibleText("Policy threshold: 0.8"), []);
  assert.notDeepEqual(scanVisibleText("Raw evidence payload"), []);
  assert.notDeepEqual(scanVisibleText("Internal reason code"), []);
});
