import type { View } from "./model";

// What is open in the editor area. A tab is named by what it holds, so
// opening the same file twice is the same tab: the id is the identity.

export interface Tab {
  id: string;
  title: string;
  icon: string;
  // A hosted panel, or a view the window draws.
  what: { panel: string } | View;
}

export function idOf(what: Tab["what"]): string {
  if ("panel" in what) return `panel:${what.panel}`;
  switch (what.kind) {
    case "code":
      return `code:${what.title}`;
    case "hunks":
      return `hunks:${what.file.path}`;
    case "term":
      return "term";
    case "rules":
      return "rules";
    case "doc":
      return `doc:${what.title}`;
    default:
      return "none";
  }
}

// opened adds a tab, or brings forward the one already holding it. The
// newer content wins.
export function opened(tabs: Tab[], tab: Tab): Tab[] {
  const held = tabs.findIndex((t) => t.id === tab.id);
  if (held < 0) return [...tabs, tab];
  const out = [...tabs];
  out[held] = tab;
  return out;
}

// shut closes one and says what to land on: the tab to its right, or
// the last one, which is where an editor leaves you.
export function shut(tabs: Tab[], id: string, active: string): { tabs: Tab[]; active: string } {
  const at = tabs.findIndex((t) => t.id === id);
  if (at < 0) return { tabs, active };
  const out = tabs.filter((t) => t.id !== id);
  if (active !== id) return { tabs: out, active };
  const next = out[at] ?? out[out.length - 1];
  return { tabs: out, active: next?.id ?? "" };
}

// held is the tab that is showing, which is the active one or nothing.
export function held(tabs: Tab[], active: string): Tab | undefined {
  return tabs.find((t) => t.id === active);
}

// A tab holds a live thing when moving it would break it: an iframe
// reloads, a terminal reconnects. Those are hidden, not rebuilt.
export function live(tab: Tab): boolean {
  return "panel" in tab.what || tab.what.kind === "term";
}

// What is worth remembering between runs: a tab the window can open
// again from its id alone, which a one-off read is not.
export function keepable(tabs: Tab[]): string[] {
  return tabs
    .filter(
      (t) =>
        "panel" in t.what ||
        t.what.kind === "term" ||
        t.what.kind === "code" ||
        t.what.kind === "hunks",
    )
    .map((t) => t.id);
}

