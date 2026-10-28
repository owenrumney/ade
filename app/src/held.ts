// The last rows each part of the gather answered with: a read that times
// out once is drawn as the list it had, not as an empty place with an error.

export interface Held {
  // key names the part, at the checkout it was read for: a failed read
  // must not hand one session's list to another.
  <T>(key: string, at: string, read: Promise<T[]>, trouble: Record<string, string>): Promise<T[]>;
}

export function keeper(why: (e: Error) => string = (e) => e.message): Held {
  const held = new Map<string, unknown[]>();
  return async <T>(
    key: string,
    at: string,
    read: Promise<T[]>,
    trouble: Record<string, string>,
  ): Promise<T[]> => {
    try {
      const rows = (await read) ?? [];
      held.set(`${key}:${at}`, rows);
      return rows;
    } catch (e) {
      trouble[key] = why(e as Error);
      return (held.get(`${key}:${at}`) as T[] | undefined) ?? [];
    }
  };
}
