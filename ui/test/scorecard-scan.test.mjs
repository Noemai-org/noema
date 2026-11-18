import test from "node:test";
import assert from "node:assert/strict";
import { scanScorecardText } from "../build/js/scorecard-scan.js";

test("A311: scorecard scan permits exact numeric evidence", () => {
  assert.deepEqual(scanScorecardText("Exact upper bound: 0.776394; 34500 ms"), []);
});

test("A311 INV-22: scorecard scan detects benchmark and decision case identifiers", () => {
  for (const value of ["BEN-AMB-0001", "BDC-ABC-123456"]) {
    assert.ok(scanScorecardText(`Example ${value} hidden`).includes("case-id"));
  }
});
