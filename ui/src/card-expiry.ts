// The contract is a UTC timestamp. Date.parse alone normalizes impossible dates.
export function cardExpiry(value: string, now: number): "active" | "expired" | "unavailable" {
  const instant = Date.parse(value);
  if (!Number.isFinite(instant) || !Number.isFinite(now)) return "unavailable";
  const date = new Date(instant);
  const parts = [date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate(), date.getUTCHours(), date.getUTCMinutes(), date.getUTCSeconds()];
  const expected = [value.slice(0, 4), value.slice(5, 7), value.slice(8, 10), value.slice(11, 13), value.slice(14, 16), value.slice(17, 19)].map(Number);
  if (parts.some((part, index) => part !== expected[index])) return "unavailable";
  return instant <= now ? "expired" : "active";
}
