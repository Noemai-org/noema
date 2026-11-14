import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

test("REQ-UI-001: the overlay has one named palette and type scale", async () => {
  const css = await readFile(new URL("../src/tokens.css", import.meta.url), "utf8");
  assert.match(css, /--noema-color-surface:/u);
  assert.match(css, /--noema-color-text:/u);
  assert.match(css, /--noema-type-body:/u);
});
