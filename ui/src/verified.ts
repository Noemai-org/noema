import { parseNoemaResolutionV1, type NoemaResolutionV1 } from "./contract_types/noema_resolution_v1.js";

// The generated parser checks shape but does not enforce conditional schema rules.
export function parseVerifiedResolution(value: unknown): NoemaResolutionV1 {
  const resolution = parseNoemaResolutionV1(value);
  const allAchieved = resolution.verdicts.length > 0 && resolution.verdicts.every((item) => item.verdict === "ACHIEVED");
  if (resolution.verified !== allAchieved) throw new Error("Invalid resolution");
  return resolution;
}
