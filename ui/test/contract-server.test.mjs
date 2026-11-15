import test from "node:test";
import assert from "node:assert/strict";
import { request } from "node:http";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { networkInterfaces, tmpdir } from "node:os";
import { join } from "node:path";
import { startContractServer } from "../test-support/contract-server.mjs";

// Local contract server, not the core. All fixture data is synthetic.
async function withServer(scenario, run) {
  const dir = await mkdtemp(join(tmpdir(), "noema-contract-"));
  const socketPath = join(dir, "server.sock");
  const server = await startContractServer({ socketPath, scenario });
  try { await run(socketPath); } finally {
    await new Promise((resolve, reject) => server.close((err) => err ? reject(err) : resolve()));
    await rm(dir, { recursive: true, force: true });
  }
}

function get(socketPath, path) {
  return new Promise((resolve, reject) => {
    const req = request({ socketPath, path }, (response) => {
      let body = "";
      response.setEncoding("utf8");
      response.on("data", (chunk) => { body += chunk; });
      response.on("end", () => resolve({ status: response.statusCode, body, headers: response.headers }));
      response.on("error", reject);
      response.on("aborted", () => reject(new Error("stream aborted")));
    });
    req.on("error", reject);
    req.end();
  });
}

test("REQ-API-001: local contract server returns investigation and SSE fixtures", async () => {
  await withServer("normal", async (socket) => {
    const state = await get(socket, "/v1/investigations/inv_synthetic");
    assert.equal(state.status, 200);
    const expected = JSON.parse(await readFile(new URL("../../contracts/fixtures/valid/investigation-response/understanding.json", import.meta.url), "utf8"));
    assert.deepEqual(JSON.parse(state.body), expected);
    const events = await get(socket, "/v1/conversations/conv_synthetic/events");
    assert.match(events.headers["content-type"], /^text\/event-stream/u);
    assert.match(events.body, /^event: progress\ndata: /u);
    const payload = JSON.parse(events.body.split("\n")[1].slice("data: ".length));
    const progress = JSON.parse(await readFile(new URL("../../contracts/fixtures/valid/sse-event/progress.json", import.meta.url), "utf8"));
    assert.deepEqual(payload, progress);
  });
});

test("REQ-UI-001: real 5xx and stream close are observable", async () => {
  await withServer("error", async (socket) => {
    const response = await get(socket, "/v1/investigations/inv_synthetic");
    assert.equal(response.status, 503);
    assert.equal(JSON.parse(response.body).error.code, "DEPENDENCY_UNAVAILABLE");
  });
  await withServer("drop", async (socket) => {
    await assert.rejects(get(socket, "/v1/conversations/conv_synthetic/events"));
  });
});

test("REQ-UI-001: browser transport binds only a local interface", async () => {
  const server = await startContractServer({ loopback: true, scenario: "normal" });
  try {
    const address = server.address();
    assert.equal(typeof address, "object");
    const local = Object.values(networkInterfaces()).flat().find((item) => item?.address === address.address);
    assert.equal(local?.internal, true);
    const response = await fetch(`http://${address.address}:${address.port}/v1/investigations/inv_synthetic`);
    assert.equal(response.status, 200);
    assert.equal((await response.json()).state, "understanding");
  } finally { await new Promise((resolve, reject) => server.close((err) => err ? reject(err) : resolve())); }
});
