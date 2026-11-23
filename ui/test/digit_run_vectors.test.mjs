import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { scanVisibleText } from "../build/js/privacy.js";

// A259, A279/A270, REQ-UI-001: the UI scan gives the verdict of the one shared vector file, contracts/fixtures/digit_run_vectors.json, which the Record guard (the source of truth for the
// digit run) and the model service's redactor read as well. A string is refused when the scan reports "digit-run". All data is SYNTHETIC.
const file = JSON.parse(await readFile(new URL("../../contracts/fixtures/digit_run_vectors.json", import.meta.url), "utf8"));
const vectors = file.vectors;

test("the shared vectors are named once and say something", () => {
  assert.ok(vectors.length >= 100);
  const names = vectors.map((v) => v.name);
  assert.equal(new Set(names).size, names.length);
  for (const v of vectors) assert.ok(v.name && v.proves && v.group, v.name);
  const groups = new Set(vectors.map((v) => v.group));
  for (const g of ["length", "separator_joins", "ends_a_run", "separators_in_a_row", "scripts", "comma", "money_accepted", "money_plain", "money_refused"]) {
    assert.ok(groups.has(g), g);
  }
});

for (const v of vectors) {
  test(`REQ-UI-001: the scan gives the verdict of the shared vector ${v.name}`, () => {
    assert.equal(scanVisibleText(v.input).includes("digit-run"), v.refused, v.proves);
  });
}
