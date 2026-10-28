// How long the page waits before trying its first read again. It never
// stops trying: a chat that gave up stays blank until the tab is rebuilt.

export const FIRST = 1_000;
export const LONGEST = 15_000;

// delayFor doubles up to LONGEST, so a server that takes a minute to come
// up costs seven reads rather than sixty.
export function delayFor(tries: number): number {
  if (tries < 1) return FIRST;
  return Math.min(FIRST * 2 ** (tries - 1), LONGEST);
}
