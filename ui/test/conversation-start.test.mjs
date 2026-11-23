import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { startConversationAndPoll } from "../build/js/conversation-start.js";

// Local contract server, not the core. The valid bodies come from #815 and main.
const fixture = async (path) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/${path}.json`, import.meta.url), "utf8"));
const otherId = "inv_01ARZ3NDEKTSV4RRFFQ69G5FAW";
async function runServer(handler, run) {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  assert.ok(address);
  const server = createServer(handler);
  server.listen(0, address);
  await once(server, "listening");
  try { return await run(`http://${address}:${server.address().port}`); }
  finally { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); }
}

for (const mismatch of ["conversation", "stream", "correlation"]) {
  test(`A479: refuse inconsistent ${mismatch} ID before any polling request`, async () => {
    const body = await fixture("conversation-response/accepted");
    if (mismatch === "conversation") body.conversation_ref = otherId;
    if (mismatch === "stream") body.stream_url = `/v1/conversations/${otherId}/events`;
    if (mismatch === "correlation") body.correlation_id = `corr_${otherId.slice(4)}`;
    const request = await fixture("message-request/start_text");
    let posts = 0;
    let polls = 0;
    await runServer((req, res) => {
      if (req.method === "POST") { posts++; res.writeHead(202, { "content-type": "application/json" }).end(JSON.stringify(body)); }
      else { polls++; res.writeHead(500).end(); }
    }, async (baseUrl) => {
      const result = await startConversationAndPoll({ baseUrl, sessionToken: "synthetic-session", request, maxPollingMs: 1000, onState: () => assert.fail("inconsistent state was shown") });
      assert.equal(result, "Connection lost.");
    });
    assert.equal(posts, 1);
    assert.equal(polls, 0);
  });
}

test("A479: a consistent core response can poll its exact investigation", async () => {
  const body = await fixture("conversation-response/accepted");
  const terminal = await fixture("investigation-response/completed_with_answer");
  const request = await fixture("message-request/start_text");
  const seen = [];
  const paths = [];
  await runServer((req, res) => {
    paths.push(`${req.method} ${req.url}`);
    res.writeHead(req.method === "POST" ? 202 : 200, { "content-type": "application/json" }).end(JSON.stringify(req.method === "POST" ? body : terminal));
  }, async (baseUrl) => {
    assert.equal(await startConversationAndPoll({ baseUrl, sessionToken: "synthetic-session", request, maxPollingMs: 1000, onState: (state) => seen.push(state) }), undefined);
  });
  assert.deepEqual(paths, ["POST /v1/conversations", `GET /v1/investigations/${body.investigation_id}`]);
  assert.deepEqual(seen, [terminal]);
});

test("A479: missing host polling duration sends no request", async () => {
  const request = await fixture("message-request/start_text");
  let requests = 0;
  await runServer((_req, res) => { requests++; res.writeHead(500).end(); }, async (baseUrl) => {
    assert.equal(await startConversationAndPoll({ baseUrl, sessionToken: "synthetic-session", request, onState: () => assert.fail("no state") }), "Connection lost.");
  });
  assert.equal(requests, 0);
});


test("A516: the initial POST deadline covers a server that never sends response headers", async () => {
  const request = await fixture("message-request/start_text");
  let posts = 0;
  await runServer((req, res) => {
    if (req.method === "POST") {
      posts++;
      req.resume();
      // Deliberately keep the connection open without response headers.
    } else {
      res.writeHead(500).end();
    }
  }, async (baseUrl) => {
    const started = Date.now();
    const result = await startConversationAndPoll({ baseUrl, sessionToken: "synthetic-session", request, maxPollingMs: 60, onState: () => assert.fail("a timed-out POST must not poll") });
    assert.equal(result, "Connection lost.");
    assert.ok(Date.now() - started < 1500, "the POST must be bounded by its host deadline");
  });
  assert.equal(posts, 1, "a timed-out POST is never retried");
});

test("A516: the initial POST deadline also covers a response body that never finishes", async () => {
  const request = await fixture("message-request/start_text");
  let posts = 0;
  await runServer((req, res) => {
    if (req.method === "POST") {
      posts++;
      req.resume();
      res.writeHead(202, { "content-type": "application/json" });
      res.write("{");
      // Headers arrived, but the body deliberately remains incomplete.
    } else {
      res.writeHead(500).end();
    }
  }, async (baseUrl) => {
    const started = Date.now();
    const result = await startConversationAndPoll({ baseUrl, sessionToken: "synthetic-session", request, maxPollingMs: 60, onState: () => assert.fail("an incomplete POST body must not poll") });
    assert.equal(result, "Connection lost.");
    assert.ok(Date.now() - started < 1500, "body consumption must share the POST deadline");
  });
  assert.equal(posts, 1, "a timed-out POST is never retried");
});
