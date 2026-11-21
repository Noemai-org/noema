import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { customerImportViolations } from "../test-support/customer-import-boundary.mjs";

// Synthetic module graphs: detect indirect imports without shipping test-only code.
async function graph(files, run) {
  const root = await mkdtemp(join(tmpdir(), "noema-ui-graph-"));
  try {
    for (const [name, source] of Object.entries(files)) {
      const file = join(root, name);
      await mkdir(join(file, ".."), { recursive: true });
      await writeFile(file, source);
    }
    return await run(root);
  } finally { await rm(root, { recursive: true, force: true }); }
}

test("A433: nested side-effect imports cannot reach the approver entry", async () => {
  await graph({ "main.ts": 'import "./nested/wrapper.js";', "nested/wrapper.ts": 'import "../contract_types/approver.js";', "contract_types/approver.ts": "export {};" }, async (root) => {
    assert.notDeepEqual(await customerImportViolations(root, ["main.ts"]), []);
  });
});

test("A433: dynamic imports and re-exports cannot reach the approver entry", async () => {
  await graph({ "main.ts": 'import("./nested/wrapper.js");', "nested/wrapper.ts": 'export * from "../contract_types/approver.js";', "contract_types/approver.ts": "export {};" }, async (root) => {
    assert.notDeepEqual(await customerImportViolations(root, ["main.ts"]), []);
  });
});

test("A433: a customer graph using the customer entry passes", async () => {
  await graph({ "main.ts": 'import "./contract_types/customer.js";', "contract_types/customer.ts": "export {};" }, async (root) => {
    assert.deepEqual(await customerImportViolations(root, ["main.ts"]), []);
  });
});

test("A433: a computed dynamic import fails closed", async () => {
  await graph({ "main.ts": 'const path = "./nested/wrapper.js"; import(path);' }, async (root) => {
    assert.notDeepEqual(await customerImportViolations(root, ["main.ts"]), []);
  });
});

test("A433: root-relative and URL imports fail closed", async () => {
  for (const specifier of ["/contract_types/approver.js", "https://example.invalid/approver.js"]) {
    await graph({ "main.ts": `import(${JSON.stringify(specifier)});` }, async (root) => {
      assert.notDeepEqual(await customerImportViolations(root, ["main.ts"]), []);
    });
  }
});

test("A433: every current customer source module stays outside the approver entry", async () => {
  const root = fileURLToPath(new URL("../src/", import.meta.url));
  const sources = (await readdir(root, { recursive: true, withFileTypes: true }))
    .filter((entry) => entry.isFile() && entry.name.endsWith(".ts"))
    .map((entry) => join(entry.parentPath, entry.name).slice(root.length))
    .filter((path) => !path.startsWith("contract_types/"));
  assert.ok(sources.includes("main.ts"));
  assert.deepEqual(await customerImportViolations(root, sources), []);
});
