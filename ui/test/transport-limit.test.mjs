import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { networkInterfaces } from 'node:os';
import { once } from 'node:events';
import { RESPONSE_FRAME_LIMIT_BYTES } from '../build/js/transport-limits.js';
import { fetchInvestigation } from '../build/js/investigation-client.js';
import { readEventStream } from '../build/js/sse-reader.js';

test('A446: response and frame bound is exactly 65536 bytes', () => {
  assert.equal(RESPONSE_FRAME_LIMIT_BYTES, 65536);
});

test('A446: caller cannot increase the investigation response or SSE frame bound', async () => {
  const address = Object.values(networkInterfaces()).flat().find(x => x?.internal && x.family === 'IPv4')?.address;
  assert.ok(address);
  const answer = await readFile(new URL('../../contracts/fixtures/valid/investigation-response/understanding.json', import.meta.url), 'utf8');
  const server = createServer((request, response) => {
    if (request.url === '/events') response.writeHead(200, {'content-type':'text/event-stream'}).end(': ' + 'x'.repeat(65536) + '\n\n');
    else response.writeHead(200, {'content-type':'application/json'}).end(answer + ' '.repeat(65536));
  });
  server.listen(0, address); await once(server, 'listening');
  const baseUrl = `http://${address}:${server.address().port}`;
  try {
    await assert.rejects(fetchInvestigation({ baseUrl, investigationId:'inv_SyntheticA', sessionToken:'synthetic', maxResponseBytes: 131072 }));
    const events = [];
    await assert.rejects(readEventStream(await fetch(`${baseUrl}/events`), e => events.push(e), 131072));
    assert.deepEqual(events, []);
  } finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
});
