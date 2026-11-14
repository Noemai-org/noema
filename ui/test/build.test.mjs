import test from "node:test";
import assert from "node:assert/strict";
import { access } from "node:fs/promises";

test("REQ-UI-001: privacy test helper is not shipped in the public bundle", async () => {
  await assert.rejects(access(new URL("../dist/js/privacy.js", import.meta.url)), { code: "ENOENT" });
});
