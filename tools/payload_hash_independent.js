#!/usr/bin/env node
// An independent implementation of payload_hash (PERD v1.17 §32.1): SHA-256 of the UTF-8 bytes of the RFC 8785 form of the payload.
// It shares no code with the Go writer (services/core/record/chain, platform/crypto) or the Python reference (tools/jcs_ref.py).
// RFC 8785 takes its string and number serialisation from ECMAScript, so JSON.stringify is the specification's own text for those;
// object members are sorted here by UTF-16 code unit, which is JavaScript's default string order. Integers only: any other number,
// and any integer beyond 2^53-1, is refused. Standard library only (node:crypto, node:fs); not run in CI, run it by hand:
//
//   node tools/payload_hash_independent.js compute '{"a":1}'                  prints the canonical text, its bytes in hex and the hash
//   node tools/payload_hash_independent.js check [contracts/fixtures/golden_vectors.json]
//                                                                              recomputes every payload_hash_vectors entry and every
//                                                                              payload_rejected_vectors entry; exit 1 on any difference
'use strict';
const crypto = require('node:crypto');
const fs = require('node:fs');

const MAX_SAFE = Number.MAX_SAFE_INTEGER; // 2^53-1

function canon(v) {
  if (v === null || typeof v === 'boolean' || typeof v === 'string') return JSON.stringify(v);
  if (typeof v === 'number') {
    if (!Number.isInteger(v) || Math.abs(v) > MAX_SAFE) throw new Error(`number ${v} is not an integer within 2^53-1`);
    return JSON.stringify(v); // -0 prints as 0
  }
  if (Array.isArray(v)) return '[' + v.map(canon).join(',') + ']';
  return '{' + Object.keys(v).sort().map((k) => JSON.stringify(k) + ':' + canon(v[k])).join(',') + '}';
}

// JSON.parse reads 1.0 and 1e3 as the integers 1 and 1000, which JCS would print as 1 and 1000: that is not the number the writer
// was given. The raw text is therefore checked for a fraction or an exponent outside strings before it is parsed.
function numbersAreIntegerLiterals(text) {
  let outside = '';
  let inString = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (inString) { if (c === '\\') i++; else if (c === '"') inString = false; continue; }
    if (c === '"') { inString = true; continue; }
    outside += c;
  }
  return !/[0-9][.eE]/.test(outside); // true, false and null hold no digits, so only a number can match
}

function compute(text) {
  if (!numbersAreIntegerLiterals(text)) throw new Error('a number with a fraction or an exponent');
  const canonical = canon(JSON.parse(text));
  const bytes = Buffer.from(canonical, 'utf8');
  return { input_bytes_utf8: canonical, input_bytes_hex: bytes.toString('hex'), expected_hash: 'sha256:' + crypto.createHash('sha256').update(bytes).digest('hex') };
}

const [mode, arg] = process.argv.slice(2);
if (mode === 'compute') {
  console.log(JSON.stringify(compute(arg), null, 1));
} else if (mode === 'check') {
  const path = arg || 'contracts/fixtures/golden_vectors.json';
  const golden = JSON.parse(fs.readFileSync(path, 'utf8'));
  let bad = 0;
  for (const v of golden.payload_hash_vectors || []) {
    const got = compute(v.payload_json);
    for (const field of Object.keys(got)) {
      if (got[field] !== v[field]) { console.log(`${v.name}: ${field} differs: ${got[field]} vs ${v[field]}`); bad++; }
    }
  }
  for (const v of golden.payload_rejected_vectors || []) {
    try { compute(v.payload_json); console.log(`${v.name}: must be rejected`); bad++; } catch (e) { /* rejected, as required */ }
  }
  const n = (golden.payload_hash_vectors || []).length + (golden.payload_rejected_vectors || []).length;
  console.log(bad === 0 ? `payload vectors agree (${n})` : `${bad} difference(s)`);
  process.exit(bad === 0 ? 0 : 1);
} else {
  console.error('usage: payload_hash_independent.js compute <json> | check [golden_vectors.json]');
  process.exit(2);
}
