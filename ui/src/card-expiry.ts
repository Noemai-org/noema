// Validate the contract's UTC instant without comparing it to the device clock.
// Expiry is authoritative only when the core evaluates the request.
export function isCanonicalInstant(value: string): boolean {
  const instant = Date.parse(value);
  if (!Number.isFinite(instant)) return false;
  const date = new Date(instant);
  const parts = [date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate(), date.getUTCHours(), date.getUTCMinutes(), date.getUTCSeconds()];
  const expected = [value.slice(0, 4), value.slice(5, 7), value.slice(8, 10), value.slice(11, 13), value.slice(14, 16), value.slice(17, 19)].map(Number);
  return !parts.some((part, index) => part !== expected[index]);
}
