import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { networkInterfaces } from "node:os";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { reconnectOnce } from "../build/js/reconnect.js";

// Local contract server, not the core. All tokens, identifiers and fixtures are SYNTHETIC.
const id = "inv_SyntheticA";
const limit = 16 * 1024; // Synthetic test limit, not a production policy.
const fixture = async (path) => JSON.parse(await readFile(new URL(`../../contracts/fixtures/valid/${path}.json`, import.meta.url), "utf8"));

test("REQ-UI-001: on a real stream drop refetches state before reconnecting, never rendering the partial answer", async () => {
  const address = Object.values(networkInterfaces()).flat().find((entry) => entry?.internal && entry.family === "IPv4")?.address;
  assert.ok(address);
  const state = await fixture("investigation-response/understanding");
  const answer = await fixture("sse-event/answer");
  let firstStream;
  let streamCount = 0;
  let started;
  const firstStarted = new Promise((resolve) => { started = resolve; });
  const trace = [];
  const server = createServer((request, response) => {
    if (request.url === "/drop") {
      assert.ok(firstStream);
      firstStream.destroy();
      response.writeHead(204).end();
      return;
    }
    trace.push({ path: request.url, auth: request.headers.authorization, cookie: request.headers.cookie });
    if (request.url === `/v1/investigations/${id}`) {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify(state));
    } else {
      streamCount++;
      response.writeHead(200, { "content-type": "text/event-stream" });
      if (streamCount === 1) {
        firstStream = response;
        response.flushHeaders();
        response.write(`event: answer\ndata: ${JSON.stringify(answer).slice(0, 30)}`);
        started();
      } else response.end(`event: answer\ndata: ${JSON.stringify(answer)}\n\n`);
    }
  });
  server.listen(0, address);
  await once(server, "listening");
  try {
    const baseUrl = `http://${address}:${server.address().port}`;
    const events = [];
    const states = [];
    const pending = reconnectOnce({ baseUrl, conversationRef: id, investigationId: id, sessionToken: "synthetic-session", maxFrameBytes: limit, maxResponseBytes: limit, onEvent: (value) => events.push(value), onState: (value) => states.push(value) });
    await firstStarted;
    await fetch(`${baseUrl}/drop`);
    await pending;
    assert.deepEqual(states, [state]);
    assert.deepEqual(events, [answer]);
    assert.deepEqual(trace.map((entry) => entry.path), [`/v1/conversations/${id}/events`, `/v1/investigations/${id}`, `/v1/conversations/${id}/events`]);
    assert.ok(trace.every((entry) => entry.auth === "Bearer synthetic-session" && entry.cookie === undefined));
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
});
