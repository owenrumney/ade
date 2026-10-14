import * as vscode from "vscode";
import type { Server } from "./discover";
import { find as findLauncher, waiting } from "./launcher";
import { badge } from "./waiting";

// How often the launcher is asked what is waiting. Its own answer is cached
// for 15s, so anything faster is a request that reads the same cache.
const every = 15000;

// Watch is the count of sessions waiting on you, and only that. The status bar
// rather than a view badge: it has to survive an empty sidebar.
export class Watch implements vscode.Disposable {
  private timer?: NodeJS.Timeout;
  private stopped = false;
  private item = vscode.window.createStatusBarItem(
    "ade.waiting",
    vscode.StatusBarAlignment.Left,
    0,
  );

  constructor() {
    this.item.name = "ade: sessions waiting";
    this.item.command = "ade.findSession";
  }

  start(): void {
    void this.tick();
  }

  dispose(): void {
    this.stopped = true;
    if (this.timer) clearTimeout(this.timer);
    this.item.dispose();
  }

  private async tick(): Promise<void> {
    if (this.stopped) return;
    try {
      const launcher = await findLauncher();
      await (launcher ? this.read(launcher) : this.quiet());
    } catch {
      // A launcher that is down is not news: it comes back or it does not.
    }
    if (!this.stopped) this.timer = setTimeout(() => void this.tick(), every);
  }

  private async quiet(): Promise<void> {
    this.item.hide();
  }

  private async read(launcher: Server): Promise<void> {
    const count = badge(await waiting(launcher));
    if (!count) {
      this.item.hide();
      return;
    }
    this.item.text = `$(bell-dot) ${count.value}`;
    this.item.tooltip = count.tooltip;
    this.item.show();
  }
}
