import test from "node:test";
import assert from "node:assert/strict";
import { progressCopy } from "../build/js/progress-copy.js";

test("REQ-UI-001: the six public stages have plain language and unknown codes are refused", () => {
  for (const value of ["understanding", "needs_input", "needs_confirmation", "working", "answered", "unavailable"]) {
    const text = progressCopy(value);
    assert.equal(typeof text, "string");
    assert.ok(text.length > 0);
    assert.doesNotMatch(text, /understanding|needs_input|needs_confirmation|working|answered|unavailable/i);
  }
  assert.equal(progressCopy("REJECTED_AT_GATE"), null);
  assert.equal(progressCopy("__proto__"), null);
});
