// A read kept against the version it was taken at. The diff and the file
// tree both move only when that version does, and both are a round trip.

export class Kept<T> {
  private at = "";
  private value: T;
  private note = "";

  constructor(empty: T) {
    this.value = empty;
  }

  // good is the kept read while the version still stands. Nothing is
  // keyed to an empty version: that is "we do not know", not "unchanged".
  good(version: string): { rows: T; why: string } | undefined {
    if (version === "" || version !== this.at) return undefined;
    return { rows: this.value, why: this.note };
  }

  keep(version: string, value: T, why = ""): { rows: T; why: string } {
    this.at = version;
    this.value = value;
    this.note = why;
    return { rows: value, why };
  }

  // stale drops the version and keeps the value: a pass that could not read
  // asks again, and writing [] in left the cache answering empty for ever.
  stale(why: string): { rows: T; why: string } {
    this.at = "";
    return { rows: this.value, why };
  }

  get rows(): T {
    return this.value;
  }
}
