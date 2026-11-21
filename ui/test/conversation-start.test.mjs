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
