import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  AGENT,
  DIFF,
  SHELL,
  agentTab,
  diffTab,
  dismissable,
  newSessionPanel,
  held,
  idOf,
  inGroup,
  keepable,
  live,
  moved,
  opened,
  rebuilt,
  seated,
  settled,
  shellTab,
  shut,
  shutMany,
  standing,
  startIn,
  sweeping,
  trimmed,
  type Tab,
} from "./tabs";

const tab = (id: string): Tab => ({ id, title: id, icon: "file", what: { kind: "none" } });

describe("what a tab is named by", () => {
  it("is what it holds, so the same thing is the same tab", () => {
    expect(idOf({ panel: "ade.newSession" })).toBe("panel:ade.newSession");
    expect(idOf({ kind: "term", title: "Shell" })).toBe(SHELL);
    expect(idOf({ kind: "agent", title: "Agent" })).toBe(AGENT);
    expect(idOf({ kind: "doc", title: "My Day", body: "" })).toBe("doc:My Day");
  });

  // One Diff tab: the file it shows changes, the tab does not.
  it("names every diff the same, whichever file it shows", () => {
    const file = { path: "x.go", status: "modified", binary: false, additions: 1, deletions: 0 };
    expect(idOf({ kind: "hunks", title: "anything", file })).toBe(DIFF);
    expect(idOf({ kind: "diff", title: "Diff" })).toBe(DIFF);
  });
});

describe("opening", () => {
  it("adds one that is not open", () => {
    expect(opened([tab("a")], tab("b")).map((t) => t.id)).toEqual(["a", "b"]);
  });

  it("replaces the one already holding it, in place", () => {
    const again = { ...tab("a"), title: "read again" };
    const out = opened([tab("a"), tab("b")], again);
    expect(out.map((t) => t.id)).toEqual(["a", "b"]);
    expect(out[0].title).toBe("read again");
  });
});

describe("closing", () => {
  const three = [tab("a"), tab("b"), tab("c")];

  it("lands on the one to the right", () => {
    expect(shut(three, "b", "b")).toEqual({ tabs: [tab("a"), tab("c")], active: "c" });
  });

  it("lands on the last when the end one goes", () => {
    expect(shut(three, "c", "c").active).toBe("b");
  });

  it("leaves the active one alone when another closes", () => {
    expect(shut(three, "a", "c").active).toBe("c");
  });

  it("ends with nothing open", () => {
    expect(shut([tab("a")], "a", "a")).toEqual({ tabs: [], active: "" });
  });

  it("ignores one that is not open", () => {
    expect(shut(three, "nope", "a")).toEqual({ tabs: three, active: "a" });
  });
});

describe("closing a sweep of them", () => {
  const four = [tab("a"), tab("b"), tab("c"), tab("d")];

  it("takes the ones to one side of the one picked, in strip order", () => {
    expect(sweeping(four, "b", "right")).toEqual(["c", "d"]);
    expect(sweeping(four, "c", "left")).toEqual(["a", "b"]);
    expect(sweeping(four, "b", "others")).toEqual(["a", "c", "d"]);
    expect(sweeping(four, "b", "all")).toEqual(["a", "b", "c", "d"]);
  });

  it("takes none to the right of the last, or left of the first", () => {
    expect(sweeping(four, "d", "right")).toEqual([]);
    expect(sweeping(four, "a", "left")).toEqual([]);
  });

  // All four is the only sweep that means anything from a tab that went.
  it("takes nothing from a tab that is not open", () => {
    expect(sweeping(four, "nope", "right")).toEqual([]);
    expect(sweeping(four, "nope", "others")).toEqual([]);
    expect(sweeping(four, "nope", "all")).toEqual(["a", "b", "c", "d"]);
  });

  it("keeps the one picked, whichever was active", () => {
    const out = shutMany(four, sweeping(four, "b", "others"), "d");
    expect(out.tabs.map((t) => t.id)).toEqual(["b"]);
    expect(out.active).toBe("b");
    expect(out.gone).toEqual(["a", "c", "d"]);
  });

  it("leaves the active one in front when it survives the sweep", () => {
    const out = shutMany(four, sweeping(four, "b", "right"), "a");
    expect(out.tabs.map((t) => t.id)).toEqual(["a", "b"]);
    expect(out.active).toBe("a");
  });

  // Closing everything to the left of c while a was in front has to land
  // somewhere, and c is the one the sweep was named from.
  it("lands on a survivor when the active one is swept", () => {
    const out = shutMany(four, sweeping(four, "c", "left"), "a");
    expect(out.tabs.map((t) => t.id)).toEqual(["c", "d"]);
    expect(out.active).toBe("c");
  });

  it("ends with nothing open and nothing in front", () => {
    const out = shutMany(four, sweeping(four, "b", "all"), "b");
    expect(out).toEqual({ tabs: [], active: "", gone: ["a", "b", "c", "d"] });
  });

  it("reports only what closed, so no pane is left behind", () => {
    expect(shutMany(four, ["b", "nope"], "a").gone).toEqual(["b"]);
  });
});

describe("a pane that may not be rebuilt", () => {
  // An iframe reloads when it is moved and a terminal reconnects, so
  // those stay in the page and are hidden instead.
  it("is a panel or the terminal", () => {
    expect(live({ ...tab("x"), what: { panel: "ade.newSession" } })).toBe(true);
    expect(live({ ...tab("x"), what: { kind: "term", title: "Shell" } })).toBe(true);
    expect(live({ ...tab("x"), what: { kind: "doc", title: "d", body: "" } })).toBe(false);
  });
});

describe("what is showing", () => {
  it("is the active tab, or nothing", () => {
    expect(held([tab("a"), tab("b")], "b")?.id).toBe("b");
    expect(held([tab("a")], "gone")).toBeUndefined();
  });
});

describe("what is picked up again next time", () => {
  const panel: Tab = { id: "panel:ade.newSession", title: "New session", icon: "session", what: { panel: "ade.newSession" } };
  const doc: Tab = { id: "doc:#412", title: "#412", icon: "pr", what: { kind: "doc", title: "#412", body: "..." } };

  it("is a panel, which opens again from its id alone", () => {
    expect(keepable([panel, doc])).toEqual(["panel:ade.newSession"]);
  });

  // Kept so the half each of the three was in comes back with it.
  it("is the three every session has", () => {
    expect(keepable([agentTab(), diffTab(), shellTab(), doc])).toEqual([AGENT, DIFF, SHELL]);
  });

  it("leaves out a document that came from a one-off read", () => {
    expect(keepable([doc])).toEqual([]);
  });
});

describe("when a pane is built again", () => {
  const pane = (key: string, waiting = false) => ({ key, waiting });

  it("is when the session or the address it was built with has moved on", () => {
    expect(rebuilt(pane("s1:http://x"), "s2:http://x", false)).toBe(true);
    expect(rebuilt(pane("s1:http://x"), "s1:http://y", false)).toBe(true);
    expect(rebuilt(pane("s1:http://x"), "s1:http://x", false)).toBe(false);
  });

  // The bug this is here for: the pane said "starting ade…", the session
  // came up, and nothing in the key had changed — so it said it until the
  // tab was closed and opened again.
  it("is when one that could only say “starting ade…” can now draw the page", () => {
    expect(rebuilt(pane("s1:http://x", true), "s1:http://x", false)).toBe(true);
  });

  it("is not when a drawn page is asked to wait again: it would lose what was typed", () => {
    expect(rebuilt(pane("s1:http://x", false), "s1:http://x", true)).toBe(false);
  });

  it("is not while it is still waiting", () => {
    expect(rebuilt(pane("s1:http://x", true), "s1:http://x", true)).toBe(false);
  });
});

describe("the tab sets written down", () => {
  const set = (n: string) => ({ tabs: [`code:${n}.go`], active: `code:${n}.go` });

  it("puts the conversation just used first, so the oldest is what goes", () => {
    const kept = new Map([["a", set("a")], ["b", set("b")], ["c", set("c")]]);
    expect(trimmed(kept, "c", 2).map(([s]) => s)).toEqual(["c", "a"]);
    expect(trimmed(kept, "a").map(([s]) => s)).toEqual(["a", "b", "c"]);
  });

  it("writes the others down even when the one just used has nothing kept", () => {
    const kept = new Map([["a", set("a")]]);
    expect(trimmed(kept, "unknown").map(([s]) => s)).toEqual(["a"]);
    expect(trimmed(new Map(), "a")).toEqual([]);
  });
});

describe("what Escape closes", () => {
  const of = (what: Tab["what"]): Tab => ({ id: idOf(what), title: "t", icon: "file", what });

  it("leaves a terminal alone, because vim reads the key", () => {
    expect(dismissable(of({ kind: "term", title: "Shell" }))).toBe(false);
  });

  it("leaves a panel to close itself, and has nothing to close with no tab", () => {
    expect(dismissable(of({ panel: "ade.newSession" }))).toBe(false);
    expect(dismissable(undefined)).toBe(false);
  });

  it("closes a tab the window draws", () => {
    expect(dismissable(of({ kind: "doc", title: "My Day", body: "" }))).toBe(true);
  });
});

describe("which panels hold the New session screen", () => {
  it("knows the plain one and the one seeded from a space", () => {
    expect(newSessionPanel("ade.newSession")).toBe(true);
    expect(newSessionPanel("in:/code/ade")).toBe(true);
  });

  it("does not claim another panel", () => {
    for (const other of ["ade.openSettings", "todo:abc", "design:plan.md"]) {
      expect(newSessionPanel(other), other).toBe(false);
    }
  });
});

describe("the repository a New session starts in", () => {
  const space = (path?: string): Tab => ({
    id: "space:o/r",
    title: "o/r",
    icon: "space",
    what: {
      kind: "space",
      title: "o/r",
      seen: { space: { repo: "o/r", path, sessions: 0, here: true }, sessions: [] },
    },
  });

  it("is the workspace you ran it from", () => {
    expect(startIn(space("/code/ade"))).toBe("/code/ade");
  });

  // A space not cloned here has no checkout to tick.
  it("is nothing for another tab, or a workspace with no checkout", () => {
    expect(startIn(space(undefined))).toBe("");
    expect(startIn(tab("a"))).toBe("");
    expect(startIn(undefined)).toBe("");
  });
});

describe("the two halves of the editor area", () => {
  it("has every tab on the left until one is dragged right", () => {
    const tabs = [tab("a"), tab("b")];
    expect(inGroup(tabs, 0).map((t) => t.id)).toEqual(["a", "b"]);
    expect(inGroup(tabs, 1)).toEqual([]);
  });

  it("moves a tab to the end of the half it is dropped in", () => {
    const tabs = moved([tab("a"), tab("b"), tab("c")], "a", 1);
    expect(inGroup(tabs, 0).map((t) => t.id)).toEqual(["b", "c"]);
    expect(inGroup(tabs, 1).map((t) => t.id)).toEqual(["a"]);
  });

  it("leaves a tab dropped back where it already is", () => {
    const tabs = [tab("a"), tab("b")];
    expect(moved(tabs, "a", 0)).toBe(tabs);
    expect(moved(tabs, "nope", 1)).toBe(tabs);
  });

  it("is a split only while both halves hold something", () => {
    expect(settled([tab("a"), tab("b")]).split).toBe(false);
    expect(settled(moved([tab("a"), tab("b")], "b", 1)).split).toBe(true);
  });

  // The last tab out of the left half: the right takes the whole area
  // back rather than leaving an empty column.
  it("closes the split when the left half empties, keeping the tabs", () => {
    const done = settled(moved([tab("a")], "a", 1));
    expect(done.split).toBe(false);
    expect(inGroup(done.tabs, 0).map((t) => t.id)).toEqual(["a"]);
  });
});

// The strip is rebuilt on every poll. A drag that captured the pointer
// on the tab lost it to the next redraw: the hint drew and the drop
// never landed, which only showed up on a drag slower than the poll.
describe("a tab drag outlives a redraw", () => {
  const src = readFileSync(join(__dirname, "app.ts"), "utf8");

  it("captures the pointer on the strip, which is built once", () => {
    expect(src).toContain("head.setPointerCapture(down.pointerId)");
    expect(src).not.toMatch(/now\.setPointerCapture/);
  });

  it("holds the strip still while a tab is being carried", () => {
    expect(src).toContain("if (!this.dragging) head.replaceChildren(...this.head())");
  });
});

describe("the three tabs every session has", () => {
  it("are put first, in their order, in a layout that has none", () => {
    expect(seated([tab("a")]).map((t) => t.id)).toEqual([AGENT, DIFF, SHELL, "a"]);
    expect(seated([]).map((t) => t.id)).toEqual([AGENT, DIFF, SHELL]);
  });

  // The Diff holds a file: seating again must not put the placeholder back.
  it("keep what each already holds", () => {
    const file = { path: "x.go", status: "modified", binary: false, additions: 1, deletions: 0 };
    const showing: Tab = { ...diffTab(), what: { kind: "hunks", title: "x.go", file } };
    const out = seated([tab("a"), shellTab(), showing]);
    expect(out.map((t) => t.id)).toEqual([AGENT, DIFF, SHELL, "a"]);
    expect(out[1]).toBe(showing);
  });

  it("are not added twice", () => {
    const once = seated([tab("a")]);
    expect(seated(once).map((t) => t.id)).toEqual(once.map((t) => t.id));
  });

  it("are the only standing ones", () => {
    for (const id of [AGENT, DIFF, SHELL]) expect(standing(id), id).toBe(true);
    for (const id of ["panel:ade.newSession", "doc:My Day", "rules", ""]) expect(standing(id), id).toBe(false);
  });

  it("are named by what they hold", () => {
    expect(idOf(agentTab().what)).toBe(AGENT);
    expect(idOf(diffTab().what)).toBe(DIFF);
    expect(idOf(shellTab().what)).toBe(SHELL);
  });

  it("are not closed by Escape", () => {
    for (const one of [agentTab(), diffTab(), shellTab()]) expect(dismissable(one), one.id).toBe(false);
  });

  // The diff is drawn, so it can be rebuilt; the other two would reconnect.
  it("hold a live thing when they are a terminal", () => {
    expect(live(agentTab())).toBe(true);
    expect(live(shellTab())).toBe(true);
    expect(live(diffTab())).toBe(false);
  });
});
