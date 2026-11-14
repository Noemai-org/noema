import test from "node:test";
import assert from "node:assert/strict";
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

test("REQ-UI-001: detects never-shown internal material", () => {
  assert.notDeepEqual(scanVisibleText("Model confidence: 0.9"), []);
  assert.notDeepEqual(scanVisibleText("Traceback (most recent call last)"), []);
  assert.notDeepEqual(scanVisibleText("Policy threshold: 0.8"), []);
  assert.notDeepEqual(scanVisibleText("Raw evidence payload"), []);
  assert.notDeepEqual(scanVisibleText("Internal reason code"), []);
});
