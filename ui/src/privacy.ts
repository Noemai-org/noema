// Test aid only: these checks cannot prove that arbitrary text is safe to show.
export function scanVisibleText(text: string): string[] {
  const findings: string[] = [];
  // Match the wide separator categories in tools/contracts/digit_run.py and the public schemas.
  if (/\p{Nd}(?:[\s\u0085\p{Z}\p{Cf}\p{M}\p{Pd}\p{Ps}\p{Pe}.\-/_:\u2212\u00b7\u30fb\uff0e\uff0f\uff1a\uff3f]*\p{Nd}){4}/u.test(text)) {
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
