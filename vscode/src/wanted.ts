// Opening a session or a review opens its checkout, restarting the extension,
// so what to land on is written down rather than held in a variable.
export const WANTED = "ade.wanted";

export interface Wanted {
  path: string;
  // A review to show, the conversation, or both. A conversation may name the
  // session to resume first; without one the chat opens on whatever is there.
  number?: number;
  chat?: boolean;
  session?: string;
  agent?: string;
  model?: string;

  // When it was left. A note is for the window coming up, not for whatever
  // opens that checkout next week.
  at?: number;
}

// stale is how long a note is worth acting on. Long enough for a window to
// start, short enough that nobody is surprised by one later.
export const STALE_MS = 2 * 60 * 1000;

// Store is the slice of vscode.Memento this needs, so the handover is
// testable without an editor.
export interface Store {
  get<T>(key: string): T | undefined;
  update(key: string, value: unknown): Thenable<void>;
}

export async function want(
  store: Store | undefined,
  wanted: Wanted,
  now = Date.now(),
): Promise<void> {
  await store?.update(WANTED, { ...wanted, at: now });
}

// Taken once, by the window it names. The writer is still running, so clearing
// on a mismatch would let it eat its own note; age retires one nobody claims.
export async function take(
  store: Store | undefined,
  root: string,
  now = Date.now(),
): Promise<Wanted | undefined> {
  const held = store?.get<Wanted>(WANTED);
  if (!held) return undefined;
  const old = now - (held.at ?? 0) > STALE_MS;
  if (held.path !== root) {
    if (old) await store?.update(WANTED, undefined);
    return undefined;
  }
  await store?.update(WANTED, undefined);
  return old ? undefined : held;
}
