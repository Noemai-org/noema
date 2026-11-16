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
