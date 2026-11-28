// Test aid for the benchmark inspector, not a substitute for authorization or
// the closed scorecard-view schema. Patterns come from the two case contracts.
export function scanScorecardText(text: string): string[] {
  return /(?:BEN-[A-Z]{2,5}|BDC-[A-Z]{3})-[0-9]{4,6}/u.test(text) ? ["case-id"] : [];
}

// Include hidden text, attributes and current form values; never log the content.
export function scanScorecardDom(root: Element): string[] {
  const findings = scanScorecardText(root.textContent ?? "");
  for (const element of [root, ...Array.from(root.querySelectorAll("*"))]) {
    for (const attribute of Array.from(element.attributes)) findings.push(...scanScorecardText(attribute.value));
    if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement || element instanceof HTMLSelectElement) {
      findings.push(...scanScorecardText(element.value));
    }
  }
  return findings;
}
