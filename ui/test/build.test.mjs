import test from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";

test("REQ-UI-001: privacy test helper is not shipped in the public bundle", async () => {
  const root = new URL("../dist/", import.meta.url);
  const files = await readdir(root, { recursive: true, withFileTypes: true });
  assert.ok(files.every((file) => !file.name.includes("privacy")));
  const markup = await readFile(new URL("index.html", root), "utf8");
  assert.doesNotMatch(markup, /scanVisibleText|scanDom/u);
});
