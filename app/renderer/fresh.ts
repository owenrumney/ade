// Every read the window makes, kept by age: a source is read because a view
// is looking at it, not because an event fired. See the design doc.

// NEED is the oldest a reading may be. The numbers sit with the source, so
// two views cannot disagree about them.
export const NEED = {
  agent: 1_000,
  diff: 2_000,
  ship: 5_000,
  pr: 30_000,
  // Repositories and branches: a person changes these, not a run.
  lists: 10_000,
} as const;

export type Need = keyof typeof NEED;

// needFor is a path's need. One with no row is read on every look.
export function needFor(path: string): number {
  const at = path.split("?")[0];
  if (at.startsWith("/api/pr") || at.startsWith("/api/checks")) return NEED.pr;
  if (at.startsWith("/api/repos") || at.startsWith("/api/branches")) {
    return NEED.lists;
  }
  if (at.startsWith("/api/state") || at.startsWith("/api/agent")) return NEED.agent;
  if (at.startsWith("/api/diff") || at.startsWith("/api/tree") || at.startsWith("/api/status")) {
    return NEED.diff;
  }
  return 0;
}

// Reading is what a view draws. Refreshing rather than empty: emptying first
// is why ade's panels flickered.
export interface Reading<T> {
  value: T;
  refreshing: boolean;
  at: number;
  error?: string;
}

interface Entry<T> {
  value: T;
  at: number;
  error?: string;
  need: number;
  // The fetch in flight, so a second asker waits on the first.
  reading?: Promise<T>;
  lookers: number;
}

export type Clock = () => number;

export class Sources {
  private held = new Map<string, Entry<unknown>>();
  private now: Clock;

  constructor(now: Clock = () => Date.now()) {
    this.now = now;
  }

  // watch is a reading of key. A value younger than its need is handed back
  // without a fetch; an older one is handed back while the fetch runs.
  watch<T>(key: string, read: () => Promise<T>, need: Need | number): Reading<T | undefined> {
    const held = (this.held.get(key) as Entry<T | undefined> | undefined) ?? {
      value: undefined,
      at: 0,
      need: 0,
      lookers: 0,
    };
    held.need = typeof need === "number" ? need : NEED[need];
    held.lookers++;
    this.held.set(key, held as Entry<unknown>);
    if (!held.reading && this.aged(held)) this.fetch(key, held, read);
    return {
      value: held.value,
      refreshing: held.reading !== undefined,
      at: held.at,
      error: held.error,
    };
  }

  // stale marks a source for re-reading without reading it.
  stale(key?: string): void {
    for (const [name, held] of this.held) {
      if (key === undefined || name === key || name.startsWith(`${key}:`)) held.at = 0;
    }
  }

  // forget drops a source whose key means nothing now: its session is gone.
  forget(key: string): void {
    for (const name of [...this.held.keys()]) {
      if (name === key || name.startsWith(`${key}:`)) this.held.delete(name);
    }
  }

  // looked ends a pass: what nothing looked at is dropped, so a closed panel
  // stops being read.
  looked(): void {
    for (const [name, held] of this.held) {
      if (held.lookers === 0 && !held.reading) this.held.delete(name);
      held.lookers = 0;
    }
  }

  // reading is what is held, for a draw that must not start a fetch.
  reading<T>(key: string, empty: T): Reading<T> {
    const held = this.held.get(key) as Entry<T> | undefined;
    if (!held) return { value: empty, refreshing: false, at: 0 };
    return {
      value: held.value,
      refreshing: held.reading !== undefined,
      at: held.at,
      error: held.error,
    };
  }

  get keys(): string[] {
    return [...this.held.keys()];
  }

  async settled(): Promise<void> {
    for (;;) {
      const flight = [...this.held.values()].map((h) => h.reading).filter(Boolean);
      if (flight.length === 0) return;
      await Promise.allSettled(flight);
    }
  }

  private aged(held: Entry<unknown>): boolean {
    return held.at === 0 || this.now() - held.at >= held.need;
  }

  private fetch<T>(key: string, held: Entry<T>, read: () => Promise<T>): void {
    const flight = read()
      .then((value) => {
        held.value = value;
        held.error = undefined;
        held.at = this.now();
        return value;
      })
      .catch((e: Error) => {
        // The value stands: a failed read is said, not drawn as empty.
        held.error = e.message;
        held.at = this.now();
        return held.value;
      })
      .finally(() => {
        if (held.reading === flight) held.reading = undefined;
      });
    held.reading = flight;
  }
}
