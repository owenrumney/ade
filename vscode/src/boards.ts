import type { Client, Issue } from "./client";
import { rankIssue } from "./issues";
import { held } from "./loading";

// Issues is the Linear tickets assigned to you. The board is the checkout
// server's, so it follows the window rather than the machine.
export class Issues {
  private why = "";
  private client?: Client;
  private fetch = held<Issue[]>(() => this.load(), []);

  serving(client: Client | undefined): void {
    this.client = client;
    if (client) this.fetch.stale();
    else this.fetch.clear();
  }

  private async load(): Promise<Issue[]> {
    if (!this.client) {
      this.why = "";
      return [];
    }
    const got = await this.client.issues().catch((err: unknown) => ({
      issues: [],
      error: String(err),
    }));
    this.why = got.error ?? "";
    return [...(got.issues ?? [])].sort(rankIssue);
  }

  async refresh(): Promise<Issue[]> {
    this.fetch.reload();
    return this.fetch.children();
  }

  // A Linear that answered with an error is not an empty backlog, and saying
  // "no issues" when the key is missing would be a lie.
  get error(): string {
    return this.why;
  }

  getChildren(): Promise<Issue[]> {
    return this.fetch.children();
  }
}
