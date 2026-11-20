// A187/A207: these are labels for backend-issued public stages, not UI-computed states.
export function progressCopy(stage: unknown): string | null {
  switch (stage) {
    case "understanding": return "We're reading your request.";
    case "needs_input": return "We need more information from you.";
    case "needs_confirmation": return "Please review the action.";
    case "working": return "We're checking the details.";
    case "answered": return "This request has ended.";
    case "unavailable": return "We can't continue right now.";
    default: return null;
  }
}
