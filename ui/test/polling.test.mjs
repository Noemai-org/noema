import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { pollInvestigation } from "../build/js/polling.js";

// Local contract server, not the core. Inputs and responses are SYNTHETIC.
const fixture = async (name) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/investigation-response/${name}.json`, import.meta.url), "utf8"));
async function withServer(handler, run) {
  const address = Object.values(networkInterfaces()).flat().find((x) => x?.internal && x.family === "IPv4")?.address;
  assert.ok(address);
  const server = createServer(handler);
  server.listen(0, address);
  await once(server, "listening");
  try { return await run(`http://${address}:${server.address().port}`); }
  finally { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); }
}
const input = (baseUrl, onState) => ({ baseUrl, investigationId: "inv_SyntheticA", sessionToken: "synthetic-session", maxResponseBytes: 16384, maxPollingMs: 15000, onState }); // Synthetic test limits only.

test("REQ-UI-001 A384: poll real states at one second and stop after the supplied answer", async () => {
  const start = await fixture("understanding");
  const end = await fixture("completed_with_answer");
  const times = [], seen = [];
  await withServer((req, res) => {
    times.push(Date.now());
    assert.equal(req.url, "/v1/investigations/inv_SyntheticA");
    assert.equal(req.headers.authorization, "Bearer synthetic-session");
    assert.equal(req.headers.cookie, undefined);
    res.writeHead(200, { "content-type": "application/json" }).end(JSON.stringify(times.length === 1 ? start : end));
  }, async (baseUrl) => {
    assert.equal(await pollInvestigation(input(baseUrl, (state) => seen.push(state))), undefined);
  });
  assert.deepEqual(seen, [start, end]);
  assert.equal(times.length, 2);
  assert.ok(times[1] - times[0] >= 900);
});

test("REQ-UI-001: a host polling deadline stops an in-flight request with safe copy", async () => {
  const controller = new AbortController();
  await withServer((_req, _res) => {}, async (baseUrl) => {
    const config = { ...input(baseUrl, () => assert.fail("no state was sent")), maxPollingMs: 100, signal: controller.signal };
    try {
      const result = await Promise.race([
        pollInvestigation(config),
        new Promise((_, reject) => setTimeout(() => reject(new Error("polling deadline was not enforced")), 700)),
      ]);
      assert.equal(result, "Connection lost.");
    } finally { controller.abort(); }
  });
});

test("REQ-UI-001: missing host polling deadline fails closed before a request", async () => {
  let calls = 0;
  await withServer((_req, res) => { calls++; res.writeHead(503).end(); }, async (baseUrl) => {
    const config = input(baseUrl, () => assert.fail("no state was sent"));
    delete config.maxPollingMs;
    assert.equal(await pollInvestigation(config), "Connection lost.");
  });
  assert.equal(calls, 0);
});

test("REQ-UI-001 A384: real connection drop refetches, then retries at 1/2/4 seconds and reports fixed copy", async () => {
  const times = [], seen = [];
  await withServer((req, res) => {
    times.push(Date.now());
    if (times.length === 1) req.socket.destroy();
    else res.writeHead(503, { "content-type": "application/json" }).end('{"error":"PRIVATE_SERVER_TEXT"}');
  }, async (baseUrl) => {
    assert.equal(await pollInvestigation(input(baseUrl, (state) => seen.push(state))), "Connection lost.");
  });
  assert.deepEqual(seen, []);
  assert.equal(times.length, 5); // initial, immediate refetch, three scheduled retries
  for (const [index, delay] of [[2, 1000], [3, 2000], [4, 4000]]) assert.ok(times[index] - times[index - 1] >= delay - 100);
});
