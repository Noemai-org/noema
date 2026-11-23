import { test as base, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { networkInterfaces } from 'node:os';
import { once } from 'node:events';
import { readFile } from 'node:fs/promises';
import { scanVisibleText } from '../build/js/privacy.js';
import { assertAccessible } from './flow-a11y.mjs';

// Local contract server, not the core. All inputs are synthetic.
const test = base.extend({ page: async ({ page }, use) => {
  try { await use(page); } finally {
    try { const fields = await page.evaluate(() => {
      const root = document.body;
      const values = [root.textContent ?? ''];
      for (const element of [root, ...root.querySelectorAll('*')]) {
        for (const attribute of element.attributes) values.push(attribute.value);
        if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement || element instanceof HTMLSelectElement) values.push(element.value);
      }
      return values;
    });
    expect(fields.flatMap(scanVisibleText)).toEqual([]); }
    finally { await assertAccessible(page); }
  }
} });

test('REQ-UI-001: a host-supplied investigation displays only the core answer', async ({ page }) => {
  const address = Object.values(networkInterfaces()).flat().find((x) => x?.internal && x.family === 'IPv4')?.address;
  expect(address).toBeTruthy();
  const answer = await readFile('../contracts/fixtures/valid/investigation-response/completed_with_answer.json');
  const files = new Map([['/', ['dist/index.html', 'text/html']], ['/tokens.css', ['dist/tokens.css', 'text/css']], ['/app.js', ['dist/app.js', 'text/javascript']]]);
  let calls = 0;
  const server = createServer(async (req, res) => {
    if (req.url === '/v1/investigations/inv_SyntheticA') {
      calls++;
      if (req.headers.authorization !== 'Bearer synthetic-session' || req.headers.cookie) { res.writeHead(401).end(); return; }
      res.writeHead(200, { 'content-type': 'application/json' }).end(answer); return;
    }
    const entry = files.get(req.url);
    if (!entry) { res.writeHead(404).end(); return; }
    try { const data = await readFile(entry[0]); res.writeHead(200, { 'content-type': entry[1] }).end(data); }
    catch { res.writeHead(404).end(); }
  });
  server.listen(0, address); await once(server, 'listening');
  const baseUrl = `http://${address}:${server.address().port}`;
  try {
    await page.goto(baseUrl);
    await page.evaluate(async (baseUrl) => {
      const client = await import('/app.js');
      client.configureOverlay({ baseUrl, sessionToken: 'synthetic-session', investigationId: 'inv_SyntheticA', maxResponseBytes: 16384, maxPollingMs: 15000 });
    }, baseUrl);
    await expect(page.getByRole('region', { name: 'Answer' })).toContainText('I opened a case with the payments team.');
    await expect(page.getByRole('button', { name: 'Send' })).toBeDisabled();
    expect(calls).toBe(1);
  } finally { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); }
});

test('A479: live overlay refuses inconsistent conversation ids before polling', async ({ page }) => {
  const address = Object.values(networkInterfaces()).flat().find((x) => x?.internal && x.family === 'IPv4')?.address;
  const response = JSON.parse(await readFile('../contracts/fixtures/valid/conversation-response/accepted.json', 'utf8'));
  response.conversation_ref = 'inv_Different';
  const files = new Map([['/', ['dist/index.html', 'text/html']], ['/tokens.css', ['dist/tokens.css', 'text/css']], ['/app.js', ['dist/app.js', 'text/javascript']]]);
  let posts = 0, polls = 0;
  const server = createServer(async (req, res) => {
    if (req.url === '/v1/conversations' && req.method === 'POST') { posts++; req.resume(); res.writeHead(202, { 'content-type': 'application/json' }).end(JSON.stringify(response)); return; }
    if (req.url.startsWith('/v1/investigations/')) { polls++; res.writeHead(500).end(); return; }
    const entry = files.get(req.url); if (!entry) { res.writeHead(404).end(); return; }
    res.writeHead(200, { 'content-type': entry[1] }).end(await readFile(entry[0]));
  });
  server.listen(0, address); await once(server, 'listening');
  const baseUrl = `http://${address}:${server.address().port}`;
  try {
    await page.goto(baseUrl);
    await page.evaluate(async (url) => (await import('/app.js')).configureOverlay({ baseUrl: url, sessionToken: 'synthetic-session', maxPollingMs: 15000 }), baseUrl);
    await page.getByLabel('Your message').fill('Help me');
    await page.getByRole('button', { name: 'Send' }).click();
    await expect(page.getByText('Connection lost.')).toBeVisible();
    expect(posts).toBe(1); expect(polls).toBe(0);
  } finally { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); }
});

test('REQ-UI-001: a valid conversation starts polling and shows the supplied answer', async ({ page }) => {
  const address = Object.values(networkInterfaces()).flat().find((x) => x?.internal && x.family === 'IPv4')?.address;
  const conversation = JSON.parse(await readFile('../contracts/fixtures/valid/conversation-response/accepted.json', 'utf8'));
  const answer = await readFile('../contracts/fixtures/valid/investigation-response/completed_with_answer.json');
  const files = new Map([['/', ['dist/index.html', 'text/html']], ['/tokens.css', ['dist/tokens.css', 'text/css']], ['/app.js', ['dist/app.js', 'text/javascript']]]);
  let posts = 0, polls = 0;
  const server = createServer(async (req, res) => {
    if (req.url === '/v1/conversations' && req.method === 'POST') { posts++; req.resume(); res.writeHead(202, { 'content-type': 'application/json' }).end(JSON.stringify(conversation)); return; }
    if (req.url === `/v1/investigations/${conversation.investigation_id}`) { polls++; res.writeHead(200, { 'content-type': 'application/json' }).end(answer); return; }
    const entry = files.get(req.url); if (!entry) { res.writeHead(404).end(); return; }
    res.writeHead(200, { 'content-type': entry[1] }).end(await readFile(entry[0]));
  });
  server.listen(0, address); await once(server, 'listening');
  const baseUrl = `http://${address}:${server.address().port}`;
  try {
    await page.goto(baseUrl);
    await page.evaluate(async (url) => (await import('/app.js')).configureOverlay({ baseUrl: url, sessionToken: 'synthetic-session', maxPollingMs: 15000 }), baseUrl);
    await page.getByLabel('Your message').fill('Help me');
    await page.getByRole('button', { name: 'Send' }).click();
    await expect(page.getByRole('region', { name: 'Answer' })).toContainText('I opened a case with the payments team.');
    expect(posts).toBe(1); expect(polls).toBe(1);
  } finally { server.closeAllConnections(); await new Promise((resolve) => server.close(resolve)); }
});
