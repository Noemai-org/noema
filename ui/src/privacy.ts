// Test aid only: these checks cannot prove that arbitrary text is safe to show.
export function scanVisibleText(text: string): string[] {
  const findings: string[] = [];
  if (/\d{5,}/u.test(text)) findings.push("digit-run");
  if (/\bmodel confidence\b/iu.test(text)) findings.push("model-confidence");
  if (/\btraceback\s*\(most recent call last\)/iu.test(text)) findings.push("stack-trace");
  if (/\bpolicy threshold\b/iu.test(text)) findings.push("policy-threshold");
  if (/\braw evidence payload\b/iu.test(text)) findings.push("raw-evidence");
  if (/\binternal reason code\b/iu.test(text)) findings.push("internal-reason-code");
  return findings;
}

// Scan the whole rendered subtree, including visually hidden accessible text.
// Do not print the text: it may contain information that should never be logged.
export function scanDom(root: Element): string[] {
  return scanVisibleText(root.textContent ?? "");
}
