// A tree's contents, loaded when first asked for. VS Code draws progress
// while getChildren is pending and none for a field filled in beforehand.
export type Around = <R>(work: () => Promise<R>) => Promise<R>;

export class Fetch<T> {
  private held: T;
  private pending?: Promise<T>;

  constructor(
    private load: () => Promise<T>,
    private empty: T,
    private around: Around = (work) => work(),
  ) {
    this.held = empty;
  }

  // now is what was last loaded, for a caller that cannot wait — a title-bar
  // command deciding whether to show itself.
  get now(): T {
    return this.held;
  }

  children(): Promise<T> {
    this.pending ??= this.run();
    return this.pending;
  }

  // Starts a read without waiting: the caller tells the view straight away,
  // so it re-asks and gets the pending promise.
  reload(): void {
    this.pending = this.run();
  }

  async again(): Promise<void> {
    this.reload();
    await this.pending;
  }

  // stale drops the cache without reading. The next children() does the read,
  // so a view nobody has opened yet loads while they are watching it.
  stale(): void {
    this.pending = undefined;
  }

  // put is a redraw of data already here; clear is losing the server.
  // Neither makes a request.
  put(value: T): void {
    this.held = value;
    this.pending = Promise.resolve(value);
  }

  clear(): void {
    this.put(this.empty);
  }

  private async run(): Promise<T> {
    this.held = await this.around(this.load);
    return this.held;
  }
}
