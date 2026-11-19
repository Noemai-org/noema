import test from "node:test";
import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { EvidencePanel } from "../build/js/evidence-panel.js";
import { scanVisibleText } from "../build/js/privacy.js";

const load = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/reconstruction.v3/${name}.json`, import.meta.url), "utf8"));

test("REQ-UI-003: every committed synthetic reconstruction renders without exposing references", async () => {
  const root = new URL("../../contracts/fixtures/valid/reconstruction.v3/", import.meta.url);
  const files = (await readdir(root)).filter((file) => file.endsWith(".json"));
  assert.ok(files.length > 0);
  for (const file of files) {
    const value = JSON.parse(await readFile(new URL(file, root), "utf8"));
    const html = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: value }));
    assert.match(html, /<h2>Record<\/h2>/, file);
    assert.doesNotMatch(html, /This evidence is unavailable/, file);
    assert.ok(!html.includes(value.tenant_id), file);
    assert.ok(!html.includes(value.correlation_id), file);
    assert.ok(!html.includes(value.chain.head_hash), file);
    assert.deepEqual(scanVisibleText(html), [], file);
  }
});

test("REQ-UI-003 POC-FR-17: the ops evidence view presents the supplied record-chain status", async () => {
  const value = await load("full_write_case");
  const html = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: value }));
  assert.deepEqual(scanVisibleText(html), []);
  assert.match(html, /record/i);
  assert.match(html, /verified/i);
  assert.ok(!html.includes(value.tenant_id));
  assert.ok(!html.includes(value.chain.head_hash));
});

test("REQ-UI-003: an unverified chain is refused by the reconstruction contract", async () => {
  const value = await load("no_events");
  const verified = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: value }));
  const unverified = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: { ...value, chain: { ...value.chain, verified: false } } }));
  assert.deepEqual(scanVisibleText(verified), []);
  assert.deepEqual(scanVisibleText(unverified), []);
  assert.match(verified, />Verified</i);
  assert.doesNotMatch(unverified, />Verified</i);
  assert.match(unverified, /This evidence is unavailable/i);
});

test("REQ-UI-003 POC-FR-17: reconstruction sections and masked explanation are presented", async () => {
  const value = await load("full_write_case");
  const html = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: value }));
  for (const heading of ["Who", "Request", "Clarification", "Resolution", "Observed state", "Policy", "Authority", "Decision", "Action", "Downstream result", "Verification", "Explanation"]) {
    assert.ok(html.includes(`>${heading}</h3>`), heading);
  }
  const explanation = value.sections.find((section) => section.kind === "EXPLANATION").items[0].facts.text;
  assert.ok(html.includes(explanation));
  assert.ok(!html.includes(value.sections.find((section) => section.kind === "RESOLUTION").items[0].facts.resolved_target));
  assert.deepEqual(scanVisibleText(html), []);
});

test("REQ-UI-003: unsafe explanation digits cause refusal before rendering", async () => {
  const value = await load("full_write_case");
  value.sections.find((section) => section.kind === "EXPLANATION").items[0].facts.text = "Account 12345";
  const html = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: value }));
  assert.match(html, /This evidence is unavailable/i);
  assert.ok(!html.includes("12345"));
  assert.deepEqual(scanVisibleText(html), []);
});

test("REQ-UI-003: malformed conditional reconstruction fixtures are never presented as verified", async () => {
  const root = new URL("../../contracts/fixtures/invalid/reconstruction.v3/", import.meta.url);
  const names = ["sections_out_of_order.json", "decision_outcome_unknown_value.json", "execution_state_succeeded.json", "action_without_grant_state.json", "grant_state_unknown_value.json", "claim_without_bound_status.json", "claim_status_not_a_fact_status.json", "claim_text_empty.json"];
  for (const name of names) {
    const value = JSON.parse(await readFile(new URL(name, root), "utf8"));
    const html = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: value }));
    assert.match(html, /This evidence is unavailable/i, name);
    assert.doesNotMatch(html, />Verified</i, name);
    assert.deepEqual(scanVisibleText(html), [], name);
  }
});
