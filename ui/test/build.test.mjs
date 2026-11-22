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

test("REQ-UI-001: built shell declares a restrictive content security policy", async () => {
  const markup = await readFile(new URL("../dist/index.html", import.meta.url), "utf8");
  const match = markup.match(/<meta\s+http-equiv="Content-Security-Policy"\s+content="([^"]+)"\s*\/>/iu);
  assert.ok(match, "built shell must declare a CSP");
  for (const directive of ["default-src 'none'", "script-src 'self'", "style-src 'self'", "connect-src 'self'", "base-uri 'none'", "form-action 'none'"]) {
    assert.ok(match[1].split(";").map((part) => part.trim()).includes(directive), `missing ${directive}`);
  }
  assert.doesNotMatch(match[1], /unsafe-inline|unsafe-eval|\*/u);
});

test("REQ-UI-001: UI server sends frame-ancestors as an HTTP CSP header", async () => {
  const config = await readFile(new URL("../nginx.conf", import.meta.url), "utf8");
  assert.match(config, /add_header\s+Content-Security-Policy\s+"frame-ancestors 'none'"\s+always\s*;/u);
});
