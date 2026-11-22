import AxeBuilder from "@axe-core/playwright";

// Local test aid: do not log page content or axe's element excerpts.
export async function assertAccessible(page) {
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"])
    .analyze();
  if (result.violations.length) throw new Error("Automated accessibility violations found.");
}
