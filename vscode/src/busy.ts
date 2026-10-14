// Busy is one indicator for however many reads are in flight. Counted rather
// than one each: eight views waking at once would otherwise stack eight.
// show takes a promise, not a stop callback: the promise exists before show
// is called, so a fast read cannot leave an undrawn indicator spinning.
export class Busy {
  private out = 0;
  private done?: () => void;

  constructor(private show: (until: Promise<void>) => void) {}

  get working(): boolean {
    return this.out > 0;
  }

  async around<T>(work: () => Promise<T>): Promise<T> {
    if (this.out++ === 0) {
      const until = new Promise<void>((ok) => (this.done = ok));
      this.show(until);
    }
    try {
      return await work();
    } finally {
      // In a finally, or one failed read leaves it spinning for the session.
      if (--this.out === 0) {
        this.done?.();
        this.done = undefined;
      }
    }
  }
}

// Around is a wrapper that shows something while work runs.
export type Around = <R>(work: () => Promise<R>) => Promise<R>;

// The shared indicator outside, the view's own inside. Here rather than in
// loading.ts, which imports vscode, so the wiring itself can be tested.
export function indicator(busy: Busy, view: Around): Around {
  return (work) => busy.around(() => view(work));
}
