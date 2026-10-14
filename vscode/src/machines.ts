import {
  ask,
  finished,
  find,
  type Host,
  hosts,
  type Load,
  type Settled,
  withLoad,
} from "./launcher";
import { held } from "./loading";

// Machines is every host ade can put a session on, and whether it is reachable.
// No tree any more: the picker reads this, and the connecting state this window
// holds has to outlive the picker closing.
export class Machines {
  private working = new Map<string, string>();
  // The launcher's in-flight work as of the last read.
  private was = new Map<string, string>();
  private fetch = held<Host[]>(() => this.read(), []);

  private async read(): Promise<Host[]> {
    const launcher = await find();
    if (!launcher) return [];
    // Asked beside the list: a reading that fails leaves the list as it was.
    const [listed, loads] = await Promise.all([
      hosts(launcher).catch(() => []),
      ask<{ hosts: Load[] | null }>(launcher, "/api/hosts/load").catch(() => ({
        hosts: null,
      })),
    ]);
    return withLoad(listed, loads.hosts ?? []);
  }

  // The launcher only says "connecting" once it has started; the picker has to
  // say it from the click, which is where the wait begins.
  begin(name: string, verb: string): void {
    this.working.set(name, verb);
  }

  end(name: string): void {
    this.working.delete(name);
  }

  get doing(): Map<string, string> {
    return this.working;
  }

  // The window that asked did not wait, so this is where it learns.
  settled(): Settled[] {
    const { settled, working } = finished(this.was, this.fetch.now);
    this.was = working;
    return settled;
  }

  // Dropped rather than reloaded: see Fetch.stale.
  stale(): void {
    this.fetch.stale();
  }

  async refresh(): Promise<Host[]> {
    this.fetch.reload();
    return this.fetch.children();
  }

  list(): Promise<Host[]> {
    return this.fetch.children();
  }
}
