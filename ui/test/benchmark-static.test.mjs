import test from "node:test";
import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdtemp, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { scanScorecardText } from "../build/js/scorecard-scan.js";
const run = promisify(execFile);

test("A326: a bench-only static scorecard file renders exact aggregate data", async () => {
  const fixturePath = "../contracts/fixtures/valid/scorecard-view/two_cases_clean.json";
  const fixture = JSON.parse(await readFile(fixturePath, "utf8"));
  const { stdout } = await run(process.execPath, ["scripts/render-benchmark.mjs", fixturePath]);
  assert.ok(stdout.includes(String(fixture.tier0.gates[0].rate.exact_upper95)));
  assert.ok(stdout.indexOf("Tier 0") < stdout.indexOf("By family"));
  assert.deepEqual(scanScorecardText(stdout), []);
});

test("A326 INV-22: static rendering refuses a file with a case list", async () => {
  const temp = await mkdtemp(join(tmpdir(), "noema-scorecard-"));
  try {
    const fixture = JSON.parse(await readFile("../contracts/fixtures/valid/scorecard-view/two_cases_clean.json", "utf8"));
    fixture.failure_list = ["BEN-AMB-0001"];
    const file = join(temp, "unsafe.json");
    await writeFile(file, JSON.stringify(fixture));
    const { stdout } = await run(process.execPath, ["scripts/render-benchmark.mjs", file]);
    assert.match(stdout, /unavailable/i);
    assert.deepEqual(scanScorecardText(stdout), []);
  } finally { await rm(temp, { recursive: true, force: true }); }
});
