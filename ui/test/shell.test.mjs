import test from "node:test";
import assert from "node:assert/strict";
import { SHELL_STATUS, SHELL_TITLE } from "../build/js/shell.js";

test("shell exposes a title and a plain-language status", () => {
  assert.equal(SHELL_TITLE, "Noema");
  assert.equal(SHELL_STATUS, "Not yet available");
});
