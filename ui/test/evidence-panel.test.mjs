import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { EvidencePanel } from "../build/js/evidence-panel.js";

const load = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/reconstruction.v3/${name}.json`, import.meta.url), "utf8"));

test("REQ-UI-003 POC-FR-17: the ops evidence view presents the supplied record-chain status", async () => {
  const value = await load("full_write_case");
  const html = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: value }));
  assert.match(html, /record/i);
  assert.match(html, /verified/i);
  assert.ok(!html.includes(value.tenant_id));
  assert.ok(!html.includes(value.chain.head_hash));
});

test("REQ-UI-003: the chain badge follows the supplied verification value", async () => {
  const value = await load("no_events");
  const verified = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: value }));
  const unverified = renderToStaticMarkup(createElement(EvidencePanel, { reconstruction: { ...value, chain: { ...value.chain, verified: false } } }));
  assert.match(verified, />Verified</i);
  assert.doesNotMatch(unverified, />Verified</i);
});
