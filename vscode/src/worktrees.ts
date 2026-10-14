import type { Client, Tree } from "./client";
import { held } from "./loading";

// Found is the repository's own checkout and its linked worktrees, kept apart
// because listed() pairs them and only the picker needs them paired.
export interface Found {
  main?: Tree;
  linked: Tree[];
}

// Worktrees is every checkout of the repository and the state of each. No tree
// any more: the picker reads this.
export class Worktrees {
  private client?: Client;
  private fetch = held<Found>(() => this.read(), { linked: [] });

  serving(client: Client | undefined): void {
    this.client = client;
    if (client) this.fetch.stale();
    else this.fetch.clear();
  }

  // A scan runs git in every worktree, so the poll only marks the list old.
  stale(): void {
    this.fetch.stale();
  }

  async refresh(): Promise<Found> {
    this.fetch.reload();
    return this.fetch.children();
  }

  // A failure in either read leaves that half out, not the whole list.
  private async read(): Promise<Found> {
    const client = this.client;
    if (!client) return { linked: [] };
    const [linked, main] = await Promise.all([
      client.worktrees().catch(() => []),
      client
        .switchable()
        .then((s) => s.worktrees.find((t) => t.main))
        .catch(() => undefined),
    ]);
    return { main, linked };
  }
}

// treeOf is which worktree a row's menu meant; Sessions passes its own node.
export function treeOf(node: unknown): Tree | undefined {
  const t = node as Tree | undefined;
  return typeof t?.path === "string" && typeof t?.name === "string"
    ? t
    : undefined;
}
