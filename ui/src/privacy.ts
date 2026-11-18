// A run of digits is digits of any script (Unicode Nd) joined by any number of separators. The separators are those of the Record guard (`separators` in
// services/core/record/payloadguard, the source of truth; A259): every kind of space, dash and bracket, every format character and combining mark, the dot, slash, colon and underscore with
// their full-width forms, the minus sign and the middle dots. A comma ends a run. contracts/fixtures/digit_run_vectors.json holds the shared vectors that keep this and the guard together.
const SEPARATORS = String.raw`\t\n\f\r \x0b\x85\p{Z}\p{Cf}\p{M}\p{Pd}\p{Ps}\p{Pe}.\-/_:\u2212\u00b7\u30fb\uff0e\uff0f\uff1a\uff3f`;
const FIVE_DIGITS = new RegExp(String.raw`\p{Nd}(?:[${SEPARATORS}]*\p{Nd}){4}`, "u");

// An amount the core renders from integer minor units (A279, A270): the whole string is three capital letters, a space, an integer part of one to three digits and groups of three after
// commas, and two decimals or none, with at most nine digits before the point. `$` without the `m` flag is the end of the string, not a place before a final newline. Same pattern as the guard's.
const MONEY = /^[A-Z]{3} [0-9]{1,3}(?:,[0-9]{3})*(?:\.[0-9]{2})?$/;
const MAX_MONEY_INTEGER_DIGITS = 9;

function isMoney(text: string): boolean {
  if (!MONEY.test(text)) return false;
  const amount = text.slice("NGN ".length);
  const point = amount.indexOf(".");
  const integerPart = point === -1 ? amount : amount.slice(0, point);
  return integerPart.replace(/,/g, "").length <= MAX_MONEY_INTEGER_DIGITS;
}

// Test aid only: these checks cannot prove that arbitrary text is safe to show.
export function scanVisibleText(text: string): string[] {
  const findings: string[] = [];
  if (FIVE_DIGITS.test(text) && !isMoney(text)) {
    findings.push("digit-run");
  }
  if (/\bmodel confidence\b/iu.test(text)) findings.push("model-confidence");
  if (/\btraceback\s*\(most recent call last\)/iu.test(text)) findings.push("stack-trace");
  if (/\bpolicy threshold\b/iu.test(text)) findings.push("policy-threshold");
  if (/\braw evidence payload\b/iu.test(text)) findings.push("raw-evidence");
  if (/\binternal reason code\b/iu.test(text)) findings.push("internal-reason-code");
  if (/\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+\b/u.test(text)) findings.push("internal-reason-code");
  if (/\b(?:tenant_id|principal_id|decision_id|binding_hash)\b/iu.test(text)) findings.push("internal-field");
  return findings;
}

// Scan the whole rendered subtree, including visually hidden accessible text.
// Do not print the text: it may contain information that should never be logged.
export function scanDom(root: Element): string[] {
  const findings = scanVisibleText(root.textContent ?? "");
  for (const element of [root, ...Array.from(root.querySelectorAll("*"))]) {
    for (const attribute of Array.from(element.attributes)) {
      findings.push(...scanVisibleText(attribute.value));
    }
    if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement || element instanceof HTMLSelectElement) {
      findings.push(...scanVisibleText(element.value));
    }
  }
  return findings;
}
