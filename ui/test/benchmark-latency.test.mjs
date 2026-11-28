import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { BenchmarkLatency } from "../build/js/benchmark-latency.js";
const fixture = JSON.parse(await readFile(new URL("../../contracts/fixtures/valid/scorecard-view/two_cases_clean.json", import.meta.url), "utf8"));
const render = (scorecard) => renderToStaticMarkup(createElement(BenchmarkLatency, { scorecard }));
test("REQ-UI-004: synthetic latency uses exact scores for each named path", () => {
  const html = render(fixture);
  for (const [path, latency] of Object.entries(fixture.tier3.latency_by_path)) {
    assert.ok(html.includes(path.toLowerCase().replaceAll("_", " ")));
    for (const value of [latency.n, latency.p50_ms, latency.p95_ms, latency.p99_ms]) {
      if (value !== null) assert.ok(html.includes(String(value)));
    }
  }
  assert.doesNotMatch(html, /BEN-[A-Z]{2,5}-[0-9]{4,6}/u);
});
test("REQ-UI-004: labelled latency values stay with their paths, including missing values", () => {
  const synthetic = structuredClone(fixture);
  synthetic.tier3.latency_by_path = {
    CLARIFICATION: { n: 7, p50_ms: 100, p95_ms: 200, p99_ms: 300 },
    CONFIRMATION: { n: 3, p50_ms: 400, p95_ms: null, p99_ms: 600 },
  };
  const expectLabels = (html) => {
    const text = html.replace(/<[^>]*>/gu, "");
    assert.match(text, /clarification: Count: 7; median: 100 ms; 95th percentile: 200 ms; 99th percentile: 300 ms/u);
    assert.match(text, /confirmation: Count: 3; median: 400 ms; 95th percentile: Not available ms; 99th percentile: 600 ms/u);
  };
  expectLabels(render(synthetic));
  const mutant = structuredClone(synthetic);
  [mutant.tier3.latency_by_path.CLARIFICATION.p50_ms, mutant.tier3.latency_by_path.CLARIFICATION.p95_ms] = [200, 100];
  assert.throws(() => expectLabels(render(mutant)), assert.AssertionError);
});
test("REQ-UI-004 INV-22: a case list cannot be rendered as latency", () => {
  const html = render({ ...fixture, failure_list: ["BEN-AMB-0001"] });
  assert.match(html, /unavailable/i);
  assert.doesNotMatch(html, /BEN-AMB-0001/);
});
test("A298: latency cannot display an integer JavaScript cannot represent exactly", () => {
  const unsafe = structuredClone(fixture);
  unsafe.tier3.latency_by_path.CLARIFICATION.p95_ms = Number.MAX_SAFE_INTEGER + 1;
  const html = render(unsafe);
  assert.match(html, /unavailable/i);
  assert.doesNotMatch(html, /9007199254740992/);
});
