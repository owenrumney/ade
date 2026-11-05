import { isChat } from "./dock";
import type { View } from "./model";

// What is open in the editor area. A tab is named by what it holds, so
// opening the same file twice is the same tab: the id is the identity.

// Which half of the editor area a tab sits in. There is one until a tab
// is dragged to the right, which is what opens the split.
export type Group = 0 | 1;

export interface Tab {
  id: string;
  title: string;
  icon: string;
  // A hosted panel, or a view the window draws.
  what: { panel: string } | View;
  group?: Group;
}

export function groupOf(tab: Tab): Group {
  return tab.group ?? 0;
}

export function inGroup(tabs: Tab[], group: Group): Tab[] {
  return tabs.filter((t) => groupOf(t) === group);
}

// moved puts a tab in the other half, last in that half's strip.
export function moved(tabs: Tab[], id: string, group: Group): Tab[] {
  const at = tabs.findIndex((t) => t.id === id);
  if (at < 0 || groupOf(tabs[at]) === group) return tabs;
  return [...tabs.filter((t) => t.id !== id), { ...tabs[at], group }];
}

// settled is the split closing itself: an empty right half is no split,
// and an empty left one is filled by what the right was holding.
export function settled(tabs: Tab[]): { tabs: Tab[]; split: boolean } {
  if (inGroup(tabs, 1).length === 0) return { tabs, split: false };
  if (inGroup(tabs, 0).length > 0) return { tabs, split: true };
  return { tabs: tabs.map((t) => ({ ...t, group: 0 as Group })), split: false };
}

// The agent's tab, and the pane the server runs its interface in.
export const AGENT = "agent";

export function agentTab(): Tab {
  return { id: AGENT, title: "Agent", icon: "session", what: { kind: "agent", title: "Agent" } };
}

// withAgent puts the agent first in a session's tabs when it is not there.
export function withAgent(tabs: Tab[]): Tab[] {
  return tabs.some((t) => t.id === AGENT) ? tabs : [agentTab(), ...tabs];
}

// The panel ids that hold the New session screen, seeded or not.
export function newSessionPanel(panel: string): boolean {
  return panel === "ade.newSession" || panel.startsWith("todo:") || panel.startsWith("in:");
}

// startIn is the checkout a New session run from this tab should open with
// ticked. Only a workspace names one; everything else leaves the pick empty.
export function startIn(tab: Tab | undefined): string {
  const what = tab?.what;
  if (!what || "panel" in what || what.kind !== "space") return "";
  return what.seen.space.path ?? "";
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
    case "agent":
      return AGENT;
    case "rules":
      return "rules";
    case "space":
      return `space:${what.seen.space.repo}`;
    case "doc":
    case "pr":
    case "person":
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

// A sweep closes more than one tab, named from the one picked.
export type Sweep = "others" | "right" | "left" | "all";

// sweeping is which tabs a sweep takes, in strip order.
export function sweeping(tabs: Tab[], id: string, which: Sweep): string[] {
  if (which === "all") return tabs.map((t) => t.id);
  const at = tabs.findIndex((t) => t.id === id);
  if (at < 0) return [];
  if (which === "others") return tabs.filter((t) => t.id !== id).map((t) => t.id);
  const rest = which === "right" ? tabs.slice(at + 1) : tabs.slice(0, at);
  return rest.map((t) => t.id);
}

// shutMany closes a set, one at a time, so where it lands is the rule
// `shut` already has. `gone` is what actually closed: a pane is kept by
// id and the closing side has to drop every one of them.
export function shutMany(
  tabs: Tab[],
  ids: string[],
  active: string,
): { tabs: Tab[]; active: string; gone: string[] } {
  let out = { tabs, active };
  const gone: string[] = [];
  for (const id of ids) {
    const before = out.tabs.length;
    out = shut(out.tabs, id, out.active);
    if (out.tabs.length < before) gone.push(id);
  }
  return { ...out, gone };
}

// held is the tab that is showing, which is the active one or nothing.
export function held(tabs: Tab[], active: string): Tab | undefined {
  return tabs.find((t) => t.id === active);
}

// A tab holds a live thing when moving it would break it: an iframe
// reloads, a terminal reconnects. Those are hidden, not rebuilt.
export function live(tab: Tab): boolean {
  return "panel" in tab.what || tab.what.kind === "term" || tab.what.kind === "agent";
}

// Whether Escape closes this tab. A panel closes itself, and a terminal
// reads the key: vim, less and fzf all want it.
export function dismissable(tab: Tab | undefined): boolean {
  if (!tab || "panel" in tab.what) return false;
  return tab.what.kind !== "term" && tab.what.kind !== "agent";
}

// Pane is a built tab. `waiting` is one that could only say why it
// cannot draw its page yet.
export interface Pane {
  key: string;
  waiting: boolean;
}

// rebuilt: the key moved, or it was waiting and the page can now be
// drawn. Never the other way, which would lose what was being typed.
export function rebuilt(held: Pane, key: string, waiting: boolean): boolean {
  return held.key !== key || (held.waiting && !waiting);
}

// Layout is what one conversation had open: tab ids, and which was in
// front. Ids, because a tab is rebuilt from its id and nothing else.
// `right` is the other half of a split, and `at` is where it divides.
export interface Layout {
  tabs: string[];
  active: string;
  right?: string[];
  rightOn?: string;
  at?: number;
}

// trimmed is the sets as they are written down: the one just used
// first, the oldest dropped. localStorage is not a filing cabinet.
export function trimmed(kept: Map<string, Layout>, first: string, max = 20): [string, Layout][] {
  const out: [string, Layout][] = [];
  const one = kept.get(first);
  if (one) out.push([first, one]);
  for (const [session, held] of kept) {
    if (session !== first) out.push([session, held]);
  }
  return out.slice(0, max);
}

// What is worth remembering between runs: a tab the window can open
// again from its id alone, which a one-off read is not.
export function keepable(tabs: Tab[]): string[] {
  return tabs
    .filter((t) =>
      "panel" in t.what
        ? !isChat(t.what.panel)
        : t.what.kind === "term" ||
          t.what.kind === "agent" ||
          t.what.kind === "code" ||
          t.what.kind === "hunks",
    )
    .map((t) => t.id);
}

